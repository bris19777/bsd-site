// Package agent is the Windows client: enrollment, credential storage,
// policy sync with the management server, scheduling, enforcement of
// subscription/quota state, and running the backup engine.
package agent

import (
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bsd-backup/internal/api"
)

const Version = "1.0.0"

// Config is the non-secret part of the enrollment, stored as plain JSON.
type Config struct {
	ServerURL string `json:"server_url"`
	DeviceID  string `json:"device_id"`
	AccountID string `json:"account_id"`
	// OfflineGraceHours: how long the agent keeps backing up on its last
	// known policy while the server is unreachable. After that it suspends
	// until it can confirm the subscription again.
	OfflineGraceHours int `json:"offline_grace_hours"`
}

// Secrets are DPAPI-protected at rest on Windows.
type Secrets struct {
	DeviceToken string `json:"device_token"`
	Passphrase  string `json:"passphrase,omitempty"` // client-side encryption key material
}

// CachedSync is the last server decision, kept (protected, since the
// policy may carry NAS credentials) for offline operation.
type CachedSync struct {
	Resp api.SyncResponse `json:"resp"`
	At   time.Time        `json:"at"`
}

type Paths struct{ Dir string }

func (p Paths) config() string  { return filepath.Join(p.Dir, "agent.json") }
func (p Paths) secrets() string { return filepath.Join(p.Dir, "secrets.bin") }
func (p Paths) cache() string   { return filepath.Join(p.Dir, "policy.bin") }
func (p Paths) State() string   { return filepath.Join(p.Dir, "state") }
func (p Paths) Logs() string    { return filepath.Join(p.Dir, "logs") }

var ErrNotEnrolled = errors.New("agent is not enrolled; run: bsd-backup-agent enroll")

func (p Paths) LoadConfig() (Config, Secrets, error) {
	var c Config
	var s Secrets
	raw, err := os.ReadFile(p.config())
	if errors.Is(err, fs.ErrNotExist) {
		return c, s, ErrNotEnrolled
	}
	if err != nil {
		return c, s, err
	}
	if err := json.Unmarshal(raw, &c); err != nil {
		return c, s, err
	}
	if err := p.readProtected(p.secrets(), &s); err != nil {
		return c, s, err
	}
	if c.OfflineGraceHours == 0 {
		c.OfflineGraceHours = 72
	}
	return c, s, nil
}

func (p Paths) SaveConfig(c Config, s Secrets) error {
	if err := secureDir(p.Dir); err != nil {
		return err
	}
	raw, _ := json.MarshalIndent(c, "", "  ")
	if err := writeFileAtomic(p.config(), raw); err != nil {
		return err
	}
	return p.writeProtected(p.secrets(), s)
}

func (p Paths) loadCache() (*CachedSync, error) {
	var c CachedSync
	if err := p.readProtected(p.cache(), &c); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

func (p Paths) saveCache(c CachedSync) error { return p.writeProtected(p.cache(), c) }

func (p Paths) readProtected(path string, v any) error {
	sealed, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	raw, err := unprotect(sealed)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}

func (p Paths) writeProtected(path string, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	sealed, err := protect(raw)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, sealed)
}

func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// NewRecoveryKey returns a random 160-bit passphrase in groups of five,
// easy to read aloud or print, e.g. "ABCDE-FGHIJ-...".
func NewRecoveryKey() string {
	b := make([]byte, 20)
	rand.Read(b)
	s := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b)
	var groups []string
	for i := 0; i < len(s); i += 5 {
		groups = append(groups, s[i:min(i+5, len(s))])
	}
	return strings.Join(groups, "-")
}
