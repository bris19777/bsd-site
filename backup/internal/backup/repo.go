// Package backup is the client backup engine: scanning, change detection,
// content-addressed deduplicated uploads, snapshots, retention and restore.
package backup

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"bsd-backup/internal/crypt"
	"bsd-backup/internal/storage"
)

var (
	ErrWrongPassphrase = errors.New("backup: encryption passphrase does not match this repository")
	ErrNeedPassphrase  = errors.New("backup: repository is encrypted but no passphrase is configured")
)

// RepoConfig is stored unencrypted as the "config" object; it holds what is
// needed to derive and verify the key, never the key itself.
type RepoConfig struct {
	Version    int       `json:"version"`
	RepoID     string    `json:"repo_id"`
	Created    time.Time `json:"created"`
	DeviceID   string    `json:"device_id"`
	Hostname   string    `json:"hostname"`
	Encrypted  bool      `json:"encrypted"`
	KDF        string    `json:"kdf,omitempty"`
	Iterations int       `json:"iterations,omitempty"`
	Salt       []byte    `json:"salt,omitempty"`
	KeyCheck   []byte    `json:"key_check,omitempty"`
}

type Repo struct {
	B      storage.Backend
	Keys   *crypt.Keys // nil when unencrypted
	Config RepoConfig
	// Warning is set when the policy asks for a different encryption mode
	// than the repository was created with (the repository's mode wins).
	Warning string
}

type RepoOptions struct {
	Passphrase string
	Encrypt    bool // used only when creating the repository
	DeviceID   string
	Hostname   string
}

// OpenRepo opens the repository on b, creating it on first use.
func OpenRepo(ctx context.Context, b storage.Backend, o RepoOptions) (*Repo, error) {
	r := &Repo{B: b}
	rc, err := b.Get(ctx, storage.Object{Key: "config"})
	switch {
	case errors.Is(err, storage.ErrNotExist):
		if err := r.create(ctx, o); err != nil {
			return nil, err
		}
		return r, nil
	case err != nil:
		return nil, fmt.Errorf("read repository config: %w", err)
	}
	raw, err := io.ReadAll(io.LimitReader(rc, 1<<20))
	rc.Close()
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, &r.Config); err != nil {
		return nil, fmt.Errorf("parse repository config: %w", err)
	}
	if r.Config.Encrypted {
		if o.Passphrase == "" {
			return nil, ErrNeedPassphrase
		}
		master, err := crypt.KeyFromPassphrase(o.Passphrase, r.Config.Salt, r.Config.Iterations)
		if err != nil {
			return nil, err
		}
		if !hmac.Equal(crypt.KeyCheck(master), r.Config.KeyCheck) {
			return nil, ErrWrongPassphrase
		}
		r.Keys = crypt.DeriveKeys(master)
	}
	if o.Encrypt != r.Config.Encrypted {
		r.Warning = fmt.Sprintf("policy encryption=%v but repository was created with encryption=%v; keeping the repository's setting",
			o.Encrypt, r.Config.Encrypted)
	}
	return r, nil
}

func (r *Repo) create(ctx context.Context, o RepoOptions) error {
	id := make([]byte, 16)
	rand.Read(id)
	r.Config = RepoConfig{
		Version: 1, RepoID: hex.EncodeToString(id), Created: time.Now().UTC(),
		DeviceID: o.DeviceID, Hostname: o.Hostname, Encrypted: o.Encrypt,
	}
	if o.Encrypt {
		if o.Passphrase == "" {
			return ErrNeedPassphrase
		}
		salt := make([]byte, 16)
		rand.Read(salt)
		master, err := crypt.KeyFromPassphrase(o.Passphrase, salt, crypt.KDFIterations)
		if err != nil {
			return err
		}
		r.Config.KDF, r.Config.Iterations, r.Config.Salt = "pbkdf2-sha256", crypt.KDFIterations, salt
		r.Config.KeyCheck = crypt.KeyCheck(master)
		r.Keys = crypt.DeriveKeys(master)
	}
	raw, _ := json.MarshalIndent(r.Config, "", "  ")
	_, err := r.B.Put(ctx, "config", bytes.NewReader(raw))
	return err
}

// Manifest lists every file in one snapshot.
type Manifest struct {
	ID       string         `json:"id"`
	Time     time.Time      `json:"time"`
	DeviceID string         `json:"device_id"`
	Hostname string         `json:"hostname"`
	Sources  []string       `json:"sources"`
	Partial  bool           `json:"partial,omitempty"`
	Files    []ManifestFile `json:"files"`
}

type ManifestFile struct {
	Path    string `json:"p"`
	Size    int64  `json:"s"`
	ModTime int64  `json:"m"` // Unix nanoseconds
	Hash    string `json:"h"` // hex SHA-256 of plaintext
	Blob    string `json:"b"`
}

// SnapshotInfo is a snapshot as seen in storage listings.
type SnapshotInfo struct {
	ID   string         `json:"id"`
	Time time.Time      `json:"time"`
	Obj  storage.Object `json:"obj"`
}

const snapshotTimeLayout = "20060102T150405Z"

func newSnapshotID(t time.Time) string {
	b := make([]byte, 3)
	rand.Read(b)
	return t.UTC().Format(snapshotTimeLayout) + "-" + hex.EncodeToString(b)
}

func snapshotTime(id string) (time.Time, bool) {
	ts, _, _ := strings.Cut(id, "-")
	t, err := time.Parse(snapshotTimeLayout, ts)
	return t, err == nil
}

// ListSnapshots returns the repository's snapshots, newest first.
func (r *Repo) ListSnapshots(ctx context.Context) ([]SnapshotInfo, error) {
	objs, err := r.B.List(ctx, "snapshots")
	if err != nil {
		return nil, err
	}
	var out []SnapshotInfo
	for _, o := range objs {
		id := strings.TrimPrefix(o.Key, "snapshots/")
		if t, ok := snapshotTime(id); ok {
			out = append(out, SnapshotInfo{ID: id, Time: t, Obj: o})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Time.After(out[j].Time) })
	return out, nil
}

func (r *Repo) encodeManifest(m *Manifest) ([]byte, error) {
	var buf bytes.Buffer
	w, err := crypt.NewWriter(&buf, r.Keys, true)
	if err != nil {
		return nil, err
	}
	if err := json.NewEncoder(w).Encode(m); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (r *Repo) decodeManifest(rd io.Reader) (*Manifest, error) {
	dec, err := crypt.NewReader(rd, r.Keys)
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.NewDecoder(dec).Decode(&m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	return &m, nil
}

// LoadManifest downloads and decodes one snapshot manifest.
func (r *Repo) LoadManifest(ctx context.Context, s SnapshotInfo) (*Manifest, error) {
	rc, err := r.B.Get(ctx, s.Obj)
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return r.decodeManifest(rc)
}
