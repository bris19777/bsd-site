package server

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"bsd-backup/internal/api"
	"bsd-backup/internal/auth"
)

// accountView is what the admin API returns: secrets are never echoed.
type accountView struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Username     string      `json:"username"`
	Subscription string      `json:"subscription"`
	QuotaBytes   int64       `json:"quota_bytes"`
	UsedBytes    int64       `json:"used_bytes"`
	Policy       *api.Policy `json:"policy,omitempty"`
	GDriveAuth   string      `json:"gdrive_auth,omitempty"` // "oauth" | "service_account"
	Devices      int         `json:"devices"`
	CreatedAt    time.Time   `json:"created_at"`
}

type deviceView struct {
	ID           string             `json:"id"`
	AccountID    string             `json:"account_id"`
	Hostname     string             `json:"hostname"`
	OS           string             `json:"os"`
	AgentVersion string             `json:"agent_version"`
	State        string             `json:"state"`
	Online       bool               `json:"online"`
	StoredBytes  int64              `json:"stored_bytes"`
	LastSeen     time.Time          `json:"last_seen"`
	LastBackupAt *time.Time         `json:"last_backup_at,omitempty"`
	LastRun      *api.ReportRequest `json:"last_run,omitempty"`
	LastError    string             `json:"last_error,omitempty"`
	Revoked      bool               `json:"revoked,omitempty"`
	EnrolledAt   time.Time          `json:"enrolled_at"`
	Events       []Event            `json:"events,omitempty"`
}

// accountInput is the body of POST (all required fields) and PATCH (any
// subset) on /admin/accounts.
type accountInput struct {
	Name         *string      `json:"name"`
	Username     *string      `json:"username"`
	Password     *string      `json:"password"`
	Subscription *string      `json:"subscription"`
	QuotaBytes   *int64       `json:"quota_bytes"`
	QuotaGB      *float64     `json:"quota_gb"` // convenience alternative to quota_bytes
	Policy       *api.Policy  `json:"policy"`
	GDriveCreds  *GDriveCreds `json:"gdrive_creds"`
}

func (s *Server) handleListAccounts(w http.ResponseWriter, r *http.Request) {
	var out []accountView
	s.store.View(func(d *storeData) error {
		for _, a := range d.Accounts {
			out = append(out, s.viewAccount(d, a))
		}
		return nil
	})
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetAccount(w http.ResponseWriter, r *http.Request) {
	var out accountView
	err := s.store.View(func(d *storeData) error {
		a := d.Accounts[r.PathValue("id")]
		if a == nil {
			return ErrNotFound
		}
		out = s.viewAccount(d, a)
		return nil
	})
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCreateAccount(w http.ResponseWriter, r *http.Request) {
	var in accountInput
	if !readJSON(w, r, &in) {
		return
	}
	if in.Username == nil || in.Password == nil || *in.Username == "" {
		writeErr(w, http.StatusBadRequest, "username and password are required")
		return
	}
	acc := &Account{
		ID: auth.NewID("acc"), Subscription: api.SubscriptionActive, CreatedAt: s.now(),
	}
	var out accountView
	err := s.store.Update(func(d *storeData) error {
		if err := s.applyAccountInput(d, acc, &in); err != nil {
			return err
		}
		d.Accounts[acc.ID] = acc
		out = s.viewAccount(d, acc)
		return nil
	})
	if err != nil {
		s.badInput(w, err)
		return
	}
	s.log.Info("account created", "account", acc.ID, "username", acc.Username)
	writeJSON(w, http.StatusCreated, out)
}

func (s *Server) handlePatchAccount(w http.ResponseWriter, r *http.Request) {
	var in accountInput
	if !readJSON(w, r, &in) {
		return
	}
	var out accountView
	err := s.store.Update(func(d *storeData) error {
		acc := d.Accounts[r.PathValue("id")]
		if acc == nil {
			return ErrNotFound
		}
		// Validate on a copy so a bad PATCH changes nothing.
		next := *acc
		if err := s.applyAccountInput(d, &next, &in); err != nil {
			return err
		}
		*acc = next
		out = s.viewAccount(d, acc)
		return nil
	})
	if err != nil {
		s.badInput(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

type inputError struct{ msg string }

func (e inputError) Error() string { return e.msg }

func (s *Server) badInput(w http.ResponseWriter, err error) {
	var ie inputError
	if errors.As(err, &ie) {
		writeErr(w, http.StatusBadRequest, ie.msg)
		return
	}
	s.internal(w, err)
}

func (s *Server) applyAccountInput(d *storeData, acc *Account, in *accountInput) error {
	if in.Name != nil {
		acc.Name = *in.Name
	}
	if in.Username != nil && *in.Username != acc.Username {
		if other := d.accountByUsername(*in.Username); other != nil && other.ID != acc.ID {
			return inputError{"username already in use"}
		}
		acc.Username = *in.Username
	}
	if in.Password != nil {
		if len(*in.Password) < 10 {
			return inputError{"password must be at least 10 characters"}
		}
		h, err := auth.HashPassword(*in.Password)
		if err != nil {
			return err
		}
		acc.PasswordHash = h
	}
	if in.Subscription != nil {
		switch *in.Subscription {
		case api.SubscriptionActive, api.SubscriptionInactive:
			acc.Subscription = *in.Subscription
		default:
			return inputError{"subscription must be active or inactive"}
		}
	}
	if in.QuotaBytes != nil {
		acc.QuotaBytes = *in.QuotaBytes
	}
	if in.QuotaGB != nil {
		acc.QuotaBytes = int64(*in.QuotaGB * (1 << 30))
	}
	if acc.QuotaBytes < 0 {
		return inputError{"quota must not be negative"}
	}
	if in.GDriveCreds != nil {
		if err := validateCreds(in.GDriveCreds); err != nil {
			return err
		}
		acc.GDriveCreds = in.GDriveCreds
	}
	if in.Policy != nil {
		if err := validatePolicy(in.Policy); err != nil {
			return err
		}
		acc.Policy = in.Policy
	}
	return nil
}

func validatePolicy(p *api.Policy) error {
	switch p.Backend {
	case api.BackendGDrive:
		if p.GDrive == nil {
			p.GDrive = &api.GDriveConf{}
		}
	case api.BackendNAS:
		if p.NAS == nil || p.NAS.Path == "" {
			return inputError{"nas backend requires nas.path"}
		}
	default:
		return inputError{"policy.backend must be gdrive or nas"}
	}
	if len(p.Sources) == 0 {
		return inputError{"policy.sources must list at least one path"}
	}
	for _, src := range p.Sources {
		if strings.TrimSpace(src.Path) == "" {
			return inputError{"policy.sources contains an empty path"}
		}
	}
	if p.Sched.IntervalMinutes < 0 || p.Keep.KeepLast < 0 || p.Keep.KeepDays < 0 {
		return inputError{"schedule and retention values must not be negative"}
	}
	for _, hm := range []string{p.Sched.WindowStart, p.Sched.WindowEnd} {
		if hm == "" {
			continue
		}
		if _, err := time.Parse("15:04", hm); err != nil {
			return inputError{fmt.Sprintf("invalid schedule window time %q (want HH:MM)", hm)}
		}
	}
	if (p.Sched.WindowStart == "") != (p.Sched.WindowEnd == "") {
		return inputError{"schedule window needs both window_start and window_end"}
	}
	return nil
}

func validateCreds(c *GDriveCreds) error {
	switch {
	case c.OAuth != nil && c.ServiceAccount != nil:
		return inputError{"gdrive_creds: set oauth or service_account, not both"}
	case c.OAuth != nil:
		if c.OAuth.ClientID == "" || c.OAuth.ClientSecret == "" || c.OAuth.RefreshToken == "" {
			return inputError{"gdrive_creds.oauth needs client_id, client_secret and refresh_token"}
		}
	case c.ServiceAccount != nil:
		if c.ServiceAccount.ClientEmail == "" || c.ServiceAccount.PrivateKey == "" {
			return inputError{"gdrive_creds.service_account needs client_email and private_key"}
		}
	default:
		return inputError{"gdrive_creds: set oauth or service_account"}
	}
	return nil
}

func (s *Server) viewAccount(d *storeData, a *Account) accountView {
	v := accountView{
		ID: a.ID, Name: a.Name, Username: a.Username, Subscription: a.Subscription,
		QuotaBytes: a.QuotaBytes, UsedBytes: d.accountUsage(a.ID),
		Devices: len(d.devicesOf(a.ID)), CreatedAt: a.CreatedAt,
	}
	if a.Policy != nil {
		p := *a.Policy
		if p.NAS != nil {
			nas := *p.NAS
			if nas.Password != "" {
				nas.Password = "********"
			}
			p.NAS = &nas
		}
		v.Policy = &p
	}
	if a.GDriveCreds != nil {
		if a.GDriveCreds.OAuth != nil {
			v.GDriveAuth = "oauth"
		} else {
			v.GDriveAuth = "service_account"
		}
	}
	return v
}

func (s *Server) viewDevice(dev *Device, withEvents bool) deviceView {
	v := deviceView{
		ID: dev.ID, AccountID: dev.AccountID, Hostname: dev.Hostname, OS: dev.OS,
		AgentVersion: dev.AgentVersion, State: dev.State, StoredBytes: dev.StoredBytes,
		LastSeen: dev.LastSeen, LastBackupAt: dev.LastBackupAt, LastRun: dev.LastRun,
		LastError: dev.LastError, Revoked: dev.Revoked, EnrolledAt: dev.EnrolledAt,
		Online: s.now().Sub(dev.LastSeen) < 3*time.Duration(s.cfg.PollSeconds)*time.Second,
	}
	if withEvents {
		v.Events = dev.Events
	}
	return v
}

func (s *Server) handleListDevices(w http.ResponseWriter, r *http.Request) {
	out := []deviceView{}
	s.store.View(func(d *storeData) error {
		for _, dev := range d.devicesOf(r.URL.Query().Get("account")) {
			out = append(out, s.viewDevice(dev, false))
		}
		return nil
	})
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleGetDevice(w http.ResponseWriter, r *http.Request) {
	var out deviceView
	err := s.store.View(func(d *storeData) error {
		dev := d.Devices[r.PathValue("id")]
		if dev == nil {
			return ErrNotFound
		}
		out = s.viewDevice(dev, true)
		return nil
	})
	if err != nil {
		s.internal(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleBackupNow(w http.ResponseWriter, r *http.Request) {
	s.deviceAction(w, r, func(dev *Device) {
		dev.RunNow = true
		dev.addEvent(Event{Time: s.now(), Kind: "admin", Message: "backup requested by admin"})
	})
}

func (s *Server) handleRevoke(w http.ResponseWriter, r *http.Request) {
	s.deviceAction(w, r, func(dev *Device) {
		dev.Revoked = true
		dev.addEvent(Event{Time: s.now(), Kind: "admin", Message: "device revoked by admin"})
	})
}

func (s *Server) deviceAction(w http.ResponseWriter, r *http.Request, fn func(*Device)) {
	err := s.store.Update(func(d *storeData) error {
		dev := d.Devices[r.PathValue("id")]
		if dev == nil {
			return ErrNotFound
		}
		fn(dev)
		return nil
	})
	if err != nil {
		s.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
