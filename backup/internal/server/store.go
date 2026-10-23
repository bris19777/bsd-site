package server

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"bsd-backup/internal/api"
)

// maxEvents bounds the telemetry kept per device.
const maxEvents = 200

var ErrNotFound = errors.New("not found")

// Account is a customer: one set of credentials, one subscription, one
// quota and one policy shared by all of its devices.
type Account struct {
	ID           string       `json:"id"`
	Name         string       `json:"name"`
	Username     string       `json:"username"`
	PasswordHash string       `json:"password_hash"`
	Subscription string       `json:"subscription"` // api.SubscriptionActive | Inactive
	QuotaBytes   int64        `json:"quota_bytes"`  // 0 = unlimited
	Policy       *api.Policy  `json:"policy,omitempty"`
	GDriveCreds  *GDriveCreds `json:"gdrive_creds,omitempty"`
	CreatedAt    time.Time    `json:"created_at"`
}

// GDriveCreds are held only by the server, which mints short-lived access
// tokens for agents. Exactly one of OAuth or ServiceAccount is set.
type GDriveCreds struct {
	OAuth          *OAuthCreds          `json:"oauth,omitempty"`
	ServiceAccount *ServiceAccountCreds `json:"service_account,omitempty"`
	Scope          string               `json:"scope,omitempty"` // default drive
}

type OAuthCreds struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
	TokenURI     string `json:"token_uri,omitempty"`
}

// ServiceAccountCreds mirrors the fields of a Google service-account JSON key.
type ServiceAccountCreds struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"` // PEM
	TokenURI    string `json:"token_uri,omitempty"`
	Subject     string `json:"subject,omitempty"` // domain-wide delegation user
}

type Device struct {
	ID           string             `json:"id"`
	AccountID    string             `json:"account_id"`
	TokenHash    string             `json:"token_hash"`
	Hostname     string             `json:"hostname"`
	OS           string             `json:"os"`
	AgentVersion string             `json:"agent_version"`
	State        string             `json:"state"`
	StoredBytes  int64              `json:"stored_bytes"`
	LastSeen     time.Time          `json:"last_seen"`
	LastBackupAt *time.Time         `json:"last_backup_at,omitempty"`
	LastRun      *api.ReportRequest `json:"last_run,omitempty"`
	LastError    string             `json:"last_error,omitempty"`
	RunNow       bool               `json:"run_now,omitempty"`
	Revoked      bool               `json:"revoked,omitempty"`
	EnrolledAt   time.Time          `json:"enrolled_at"`
	Events       []Event            `json:"events,omitempty"`
}

// Event is one telemetry record (report or log line) kept for the admin view.
type Event struct {
	Time    time.Time `json:"time"`
	Kind    string    `json:"kind"` // "report" | "log" | "enroll" | "admin"
	Level   string    `json:"level,omitempty"`
	Message string    `json:"message"`
}

type storeData struct {
	JWTKey   []byte              `json:"jwt_key"`
	Accounts map[string]*Account `json:"accounts"`
	Devices  map[string]*Device  `json:"devices"`
}

// Store is a JSON-file database guarded by a mutex. It is sized for an MSP
// with hundreds of devices; swap it for Postgres behind the same methods
// when that stops being true.
type Store struct {
	mu   sync.Mutex
	path string
	d    storeData
}

func OpenStore(path string) (*Store, error) {
	s := &Store{path: path, d: storeData{
		Accounts: map[string]*Account{}, Devices: map[string]*Device{},
	}}
	raw, err := os.ReadFile(path)
	switch {
	case errors.Is(err, os.ErrNotExist):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(raw, &s.d); err != nil {
			return nil, err
		}
	}
	if s.d.Accounts == nil {
		s.d.Accounts = map[string]*Account{}
	}
	if s.d.Devices == nil {
		s.d.Devices = map[string]*Device{}
	}
	return s, nil
}

// Update runs fn under the lock and persists the result if fn succeeds.
func (s *Store) Update(fn func(d *storeData) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fn(&s.d); err != nil {
		return err
	}
	return s.saveLocked()
}

// View runs fn under the lock without persisting.
func (s *Store) View(fn func(d *storeData) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(&s.d)
}

func (s *Store) saveLocked() error {
	raw, err := json.MarshalIndent(&s.d, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (d *storeData) accountByUsername(username string) *Account {
	for _, a := range d.Accounts {
		if a.Username == username {
			return a
		}
	}
	return nil
}

// accountUsage sums the stored bytes of every non-revoked device.
func (d *storeData) accountUsage(accountID string) int64 {
	var used int64
	for _, dev := range d.Devices {
		if dev.AccountID == accountID && !dev.Revoked {
			used += dev.StoredBytes
		}
	}
	return used
}

func (d *storeData) devicesOf(accountID string) []*Device {
	var out []*Device
	for _, dev := range d.Devices {
		if accountID == "" || dev.AccountID == accountID {
			out = append(out, dev)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Hostname < out[j].Hostname })
	return out
}

func (dev *Device) addEvent(e Event) {
	dev.Events = append(dev.Events, e)
	if n := len(dev.Events); n > maxEvents {
		dev.Events = append([]Event(nil), dev.Events[n-maxEvents:]...)
	}
}
