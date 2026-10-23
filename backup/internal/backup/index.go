package backup

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Index is the agent's local record of what is already in the repository.
// It makes runs incremental (unchanged size+mtime means no re-hash) and
// keeps the storage IDs of uploaded objects. It can always be rebuilt
// from the repository, so losing it costs time, not data.
type Index struct {
	RepoID    string               `json:"repo_id"`
	Files     map[string]FileState `json:"files"`
	Blobs     map[string]BlobState `json:"blobs"`
	Snapshots []SnapshotState      `json:"snapshots"`

	dir string
}

type FileState struct {
	Size    int64  `json:"s"`
	ModTime int64  `json:"m"`
	Hash    string `json:"h"`
}

type BlobState struct {
	Size     int64     `json:"s"`            // stored (compressed/encrypted) size
	RemoteID string    `json:"id,omitempty"` // e.g. Google Drive file ID
	Uploaded time.Time `json:"t"`
}

type SnapshotState struct {
	ID       string    `json:"id"`
	Time     time.Time `json:"time"`
	RemoteID string    `json:"remote_id,omitempty"`
	Size     int64     `json:"size"`
	Files    int       `json:"files"`
	Partial  bool      `json:"partial,omitempty"`
}

func LoadIndex(stateDir string) (*Index, error) {
	idx := &Index{dir: stateDir}
	raw, err := os.ReadFile(filepath.Join(stateDir, "index.json"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	if err == nil {
		if jerr := json.Unmarshal(raw, idx); jerr != nil {
			// A corrupt index is rebuilt from the repository, not fatal.
			idx = &Index{dir: stateDir}
		}
	}
	idx.ensure()
	return idx, nil
}

func (x *Index) ensure() {
	if x.Files == nil {
		x.Files = map[string]FileState{}
	}
	if x.Blobs == nil {
		x.Blobs = map[string]BlobState{}
	}
}

func (x *Index) Save() error {
	if err := os.MkdirAll(x.dir, 0o700); err != nil {
		return err
	}
	raw, err := json.Marshal(x)
	if err != nil {
		return err
	}
	p := filepath.Join(x.dir, "index.json")
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// StoredBytes is the repository's total size as far as this agent knows.
func (x *Index) StoredBytes() int64 {
	var n int64
	for _, b := range x.Blobs {
		n += b.Size
	}
	for _, s := range x.Snapshots {
		n += s.Size
	}
	return n
}

func (x *Index) LastBackup() *time.Time {
	for i := len(x.Snapshots) - 1; i >= 0; i-- {
		if !x.Snapshots[i].Partial {
			t := x.Snapshots[i].Time
			return &t
		}
	}
	return nil
}

// sync aligns the index with the repository. When the repository changed
// identity (new destination, re-created repo) or the index is empty, blob
// and snapshot lists are rebuilt from storage listings so nothing that is
// already stored gets uploaded again.
func (x *Index) sync(ctx context.Context, r *Repo) error {
	if x.RepoID == r.Config.RepoID && (len(x.Blobs) > 0 || len(x.Snapshots) > 0) {
		return nil
	}
	fresh := &Index{dir: x.dir, RepoID: r.Config.RepoID}
	fresh.ensure()
	if x.RepoID == r.Config.RepoID {
		fresh.Files = x.Files
	}
	blobs, err := r.B.List(ctx, "blobs")
	if err != nil {
		return err
	}
	for _, o := range blobs {
		fresh.Blobs[strings.TrimPrefix(o.Key, "blobs/")] = BlobState{Size: o.Size, RemoteID: o.ID}
	}
	snaps, err := r.ListSnapshots(ctx)
	if err != nil {
		return err
	}
	for _, s := range snaps {
		fresh.Snapshots = append(fresh.Snapshots, SnapshotState{ID: s.ID, Time: s.Time, RemoteID: s.Obj.ID, Size: s.Obj.Size})
	}
	sort.Slice(fresh.Snapshots, func(i, j int) bool { return fresh.Snapshots[i].Time.Before(fresh.Snapshots[j].Time) })
	// Kept file hashes are safe: the engine re-uploads any whose blob is
	// not in the rebuilt blob list.
	*x = *fresh
	return x.Save()
}

func (x *Index) manifestPath(id string) string {
	return filepath.Join(x.dir, "manifests", id)
}
