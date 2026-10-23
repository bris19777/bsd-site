// Package server is the cloud Central Management Server: authentication,
// subscriptions and quotas, the policy engine, telemetry, and Google Drive
// token brokering. Backup payloads never pass through it.
package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"bsd-backup/internal/api"
	"bsd-backup/internal/auth"
)

type Config struct {
	AdminKey                 string        // bearer key for /api/v1/admin
	TokenTTL                 time.Duration // device JWT lifetime
	PollSeconds              int           // heartbeat interval sent to agents
	AllowRestoreWhenInactive bool
	HTTPClient               *http.Client // for Google token calls; nil = default
}

type Server struct {
	cfg     Config
	store   *Store
	signer  *auth.Signer
	minter  *tokenMinter
	limiter *loginLimiter
	log     *slog.Logger
	now     func() time.Time
}

func New(cfg Config, store *Store, log *slog.Logger) (*Server, error) {
	if len(cfg.AdminKey) < 16 {
		return nil, errors.New("admin key must be at least 16 characters")
	}
	if cfg.TokenTTL == 0 {
		cfg.TokenTTL = 15 * time.Minute
	}
	if cfg.PollSeconds == 0 {
		cfg.PollSeconds = 300
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	var key []byte
	err := store.Update(func(d *storeData) error {
		if len(d.JWTKey) == 0 {
			d.JWTKey = make([]byte, 32)
			if _, err := rand.Read(d.JWTKey); err != nil {
				return err
			}
		}
		key = d.JWTKey
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &Server{
		cfg: cfg, store: store, signer: auth.NewSigner(key),
		minter: newTokenMinter(cfg.HTTPClient), limiter: newLoginLimiter(),
		log: log, now: time.Now,
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })

	mux.HandleFunc("POST /api/v1/auth/enroll", s.handleEnroll)
	mux.HandleFunc("POST /api/v1/auth/token", s.handleToken)

	mux.HandleFunc("POST /api/v1/agent/sync", s.device(s.handleSync))
	mux.HandleFunc("POST /api/v1/agent/report", s.device(s.handleReport))
	mux.HandleFunc("POST /api/v1/agent/logs", s.device(s.handleLogs))
	mux.HandleFunc("POST /api/v1/agent/storage-token", s.device(s.handleStorageToken))

	mux.HandleFunc("GET /api/v1/admin/accounts", s.admin(s.handleListAccounts))
	mux.HandleFunc("POST /api/v1/admin/accounts", s.admin(s.handleCreateAccount))
	mux.HandleFunc("GET /api/v1/admin/accounts/{id}", s.admin(s.handleGetAccount))
	mux.HandleFunc("PATCH /api/v1/admin/accounts/{id}", s.admin(s.handlePatchAccount))
	mux.HandleFunc("GET /api/v1/admin/devices", s.admin(s.handleListDevices))
	mux.HandleFunc("GET /api/v1/admin/devices/{id}", s.admin(s.handleGetDevice))
	mux.HandleFunc("POST /api/v1/admin/devices/{id}/backup-now", s.admin(s.handleBackupNow))
	mux.HandleFunc("POST /api/v1/admin/devices/{id}/revoke", s.admin(s.handleRevoke))
	return mux
}

// ---- middleware ----

type deviceCtx struct {
	deviceID  string
	accountID string
}

func (s *Server) device(h func(http.ResponseWriter, *http.Request, deviceCtx)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, ok := bearer(r)
		if !ok {
			writeErr(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		claims, err := s.signer.Verify(tok)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, err.Error())
			return
		}
		var revoked, exists bool
		s.store.View(func(d *storeData) error {
			dev := d.Devices[claims.Subject]
			exists = dev != nil && d.Accounts[dev.AccountID] != nil
			revoked = exists && dev.Revoked
			return nil
		})
		switch {
		case !exists:
			writeErr(w, http.StatusUnauthorized, "unknown device")
		case revoked:
			writeErr(w, http.StatusForbidden, api.ReasonDeviceRevoked)
		default:
			h(w, r, deviceCtx{deviceID: claims.Subject, accountID: claims.AccountID})
		}
	}
}

func (s *Server) admin(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok, ok := bearer(r)
		if !ok || subtle.ConstantTimeCompare([]byte(tok), []byte(s.cfg.AdminKey)) != 1 {
			writeErr(w, http.StatusUnauthorized, "admin key required")
			return
		}
		h(w, r)
	}
}

// ---- auth ----

func (s *Server) handleEnroll(w http.ResponseWriter, r *http.Request) {
	var req api.EnrollRequest
	if !readJSON(w, r, &req) {
		return
	}
	limKey := clientIP(r) + "|" + req.Username
	if !s.limiter.allow(limKey, s.now()) {
		writeErr(w, http.StatusTooManyRequests, "too many failed attempts; try again later")
		return
	}
	var resp api.EnrollResponse
	err := s.store.Update(func(d *storeData) error {
		acc := d.accountByUsername(req.Username)
		if acc == nil || !auth.CheckPassword(acc.PasswordHash, req.Password) {
			return errBadCredentials
		}
		token := auth.NewToken()
		dev := &Device{
			ID: auth.NewID("dev"), AccountID: acc.ID, TokenHash: auth.HashToken(token),
			Hostname: req.Hostname, OS: req.OS, State: api.StateIdle,
			EnrolledAt: s.now(), LastSeen: s.now(),
		}
		dev.addEvent(Event{Time: s.now(), Kind: "enroll", Message: "device enrolled from " + clientIP(r)})
		d.Devices[dev.ID] = dev
		resp = api.EnrollResponse{DeviceID: dev.ID, DeviceToken: token, AccountID: acc.ID}
		return nil
	})
	if errors.Is(err, errBadCredentials) {
		s.limiter.fail(limKey, s.now())
		writeErr(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if err != nil {
		s.internal(w, err)
		return
	}
	s.limiter.reset(limKey)
	s.log.Info("device enrolled", "device", resp.DeviceID, "account", resp.AccountID, "host", req.Hostname)
	writeJSON(w, http.StatusOK, resp)
}

var errBadCredentials = errors.New("bad credentials")

func (s *Server) handleToken(w http.ResponseWriter, r *http.Request) {
	var req api.TokenRequest
	if !readJSON(w, r, &req) {
		return
	}
	var accountID string
	var status int
	s.store.View(func(d *storeData) error {
		dev := d.Devices[req.DeviceID]
		switch {
		case dev == nil || !auth.TokenMatches(dev.TokenHash, req.DeviceToken):
			status = http.StatusUnauthorized
		case dev.Revoked:
			status = http.StatusForbidden
		default:
			accountID = dev.AccountID
		}
		return nil
	})
	switch status {
	case http.StatusUnauthorized:
		writeErr(w, status, "invalid device credentials")
		return
	case http.StatusForbidden:
		writeErr(w, status, api.ReasonDeviceRevoked)
		return
	}
	tok, exp, err := s.signer.Issue(req.DeviceID, accountID, s.cfg.TokenTTL)
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, api.TokenResponse{AccessToken: tok, ExpiresAt: exp})
}

// ---- agent ----

func (s *Server) handleSync(w http.ResponseWriter, r *http.Request, dc deviceCtx) {
	var req api.SyncRequest
	if !readJSON(w, r, &req) {
		return
	}
	var resp api.SyncResponse
	err := s.store.Update(func(d *storeData) error {
		dev, acc := d.Devices[dc.deviceID], d.Accounts[dc.accountID]
		if dev == nil || acc == nil {
			return ErrNotFound
		}
		dev.LastSeen = s.now()
		dev.Hostname = orDefault(req.Hostname, dev.Hostname)
		dev.OS = orDefault(req.OS, dev.OS)
		dev.AgentVersion = req.AgentVersion
		dev.State = req.State
		dev.StoredBytes = req.StoredBytes
		dev.LastError = req.LastError
		if req.LastBackupAt != nil {
			dev.LastBackupAt = req.LastBackupAt
		}
		resp = s.decide(d, acc)
		resp.RunNow = dev.RunNow && resp.BackupAllowed
		if resp.RunNow {
			dev.RunNow = false
		}
		return nil
	})
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// decide applies subscription and quota rules for an account.
func (s *Server) decide(d *storeData, acc *Account) api.SyncResponse {
	resp := api.SyncResponse{
		Subscription: acc.Subscription,
		QuotaBytes:   acc.QuotaBytes,
		UsedBytes:    d.accountUsage(acc.ID),
		Policy:       acc.Policy,
		PollSeconds:  s.cfg.PollSeconds,
		ServerTime:   s.now(),
	}
	switch {
	case acc.Subscription != api.SubscriptionActive:
		resp.Reason = api.ReasonInactive
	case acc.Policy == nil:
		resp.Reason = api.ReasonNoPolicy
	case acc.QuotaBytes > 0 && resp.UsedBytes >= acc.QuotaBytes:
		resp.Reason = api.ReasonQuotaExceeded
	default:
		resp.BackupAllowed = true
	}
	return resp
}

func (s *Server) handleReport(w http.ResponseWriter, r *http.Request, dc deviceCtx) {
	var req api.ReportRequest
	if !readJSON(w, r, &req) {
		return
	}
	err := s.store.Update(func(d *storeData) error {
		dev := d.Devices[dc.deviceID]
		if dev == nil {
			return ErrNotFound
		}
		dev.LastSeen = s.now()
		if req.Stats.StoredBytes > 0 {
			dev.StoredBytes = req.Stats.StoredBytes
		}
		level := "info"
		switch req.Phase {
		case api.PhaseCompleted:
			t := req.Time
			dev.LastBackupAt = &t
			dev.LastError = ""
			dev.State = api.StateIdle
		case api.PhaseFailed:
			level = "error"
			dev.LastError = req.Message
			dev.State = api.StateError
		case api.PhaseSuspended:
			level = "warn"
			dev.State = api.StateSuspended
		case api.PhaseStarted, api.PhaseProgress:
			dev.State = api.StateBackingUp
		}
		if req.Phase != api.PhaseProgress {
			rr := req
			dev.LastRun = &rr
			dev.addEvent(Event{Time: req.Time, Kind: "report", Level: level,
				Message: req.Phase + " " + req.RunID + summarize(req)})
		}
		return nil
	})
	if err != nil {
		s.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request, dc deviceCtx) {
	var req api.LogsRequest
	if !readJSON(w, r, &req) {
		return
	}
	err := s.store.Update(func(d *storeData) error {
		dev := d.Devices[dc.deviceID]
		if dev == nil {
			return ErrNotFound
		}
		for _, e := range req.Entries {
			dev.addEvent(Event{Time: e.Time, Kind: "log", Level: e.Level, Message: truncate(e.Message, 2000)})
		}
		return nil
	})
	if err != nil {
		s.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleStorageToken(w http.ResponseWriter, r *http.Request, dc deviceCtx) {
	var req api.StorageTokenRequest
	if !readJSON(w, r, &req) {
		return
	}
	var creds *GDriveCreds
	var denied string
	s.store.View(func(d *storeData) error {
		acc := d.Accounts[dc.accountID]
		switch {
		case acc == nil:
			denied = "unknown account"
		case acc.Policy == nil || acc.Policy.Backend != api.BackendGDrive || acc.GDriveCreds == nil:
			denied = "account is not configured for Google Drive"
		case acc.Subscription != api.SubscriptionActive &&
			!(req.Purpose == "restore" && s.cfg.AllowRestoreWhenInactive):
			denied = api.ReasonInactive
		default:
			c := *acc.GDriveCreds
			creds = &c
		}
		return nil
	})
	if denied != "" {
		writeErr(w, http.StatusForbidden, denied)
		return
	}
	tok, exp, err := s.minter.Token(r.Context(), dc.accountID, creds)
	if err != nil {
		s.log.Error("mint drive token", "account", dc.accountID, "err", err)
		writeErr(w, http.StatusBadGateway, "could not obtain Google Drive token")
		return
	}
	writeJSON(w, http.StatusOK, api.StorageTokenResponse{AccessToken: tok, ExpiresAt: exp})
}

// ---- helpers ----

func (s *Server) internal(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrNotFound) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	s.log.Error("request failed", "err", err)
	writeErr(w, http.StatusInternalServerError, "internal error")
}

func bearer(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	tok, ok := strings.CutPrefix(h, "Bearer ")
	return tok, ok && tok != ""
}

func readJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, api.ErrorResponse{Error: msg})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func summarize(r api.ReportRequest) string {
	var out string
	if r.Stats.FilesScanned > 0 || r.Phase == api.PhaseCompleted {
		out = fmt.Sprintf(" files=%d changed=%d failed=%d uploaded=%dB",
			r.Stats.FilesScanned, r.Stats.FilesChanged, r.Stats.FilesFailed, r.Stats.BytesUploaded)
	}
	if r.Message != "" {
		out += ": " + truncate(r.Message, 500)
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// loginLimiter blocks a client+username after repeated enrollment failures.
type loginLimiter struct {
	mu    sync.Mutex
	fails map[string][]time.Time
}

const (
	maxFailures   = 5
	failureWindow = 15 * time.Minute
)

func newLoginLimiter() *loginLimiter { return &loginLimiter{fails: map[string][]time.Time{}} }

func (l *loginLimiter) allow(key string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	recent := l.fails[key][:0]
	for _, t := range l.fails[key] {
		if now.Sub(t) < failureWindow {
			recent = append(recent, t)
		}
	}
	l.fails[key] = recent
	return len(recent) < maxFailures
}

func (l *loginLimiter) fail(key string, now time.Time) {
	l.mu.Lock()
	l.fails[key] = append(l.fails[key], now)
	l.mu.Unlock()
}

func (l *loginLimiter) reset(key string) {
	l.mu.Lock()
	delete(l.fails, key)
	l.mu.Unlock()
}
