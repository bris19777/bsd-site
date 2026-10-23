package backup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"bsd-backup/internal/crypt"
	"bsd-backup/internal/storage"
)

type RestoreOptions struct {
	Snapshot string // snapshot ID or "latest"
	Target   string // directory to restore into
	Prefix   string // only paths starting with this (case-insensitive on Windows)
	Log      *slog.Logger
}

type RestoreStats struct {
	Snapshot string
	Files    int
	Bytes    int64
	Failed   int
}

// FindSnapshot resolves an ID or "latest" (newest complete snapshot).
func (r *Repo) FindSnapshot(ctx context.Context, id string) (SnapshotInfo, error) {
	snaps, err := r.ListSnapshots(ctx)
	if err != nil {
		return SnapshotInfo{}, err
	}
	for _, s := range snaps {
		if s.ID == id {
			return s, nil
		}
	}
	if id == "" || id == "latest" {
		for _, s := range snaps {
			if m, err := r.LoadManifest(ctx, s); err == nil && !m.Partial {
				return s, nil
			}
		}
		if len(snaps) > 0 {
			return snaps[0], nil
		}
	}
	return SnapshotInfo{}, fmt.Errorf("snapshot %q not found", id)
}

// Restore writes a snapshot's files under Target, recreating the original
// layout (C:\Users\a\x.txt -> <Target>\C\Users\a\x.txt). Every file is
// verified against its recorded SHA-256 before it is moved into place.
func Restore(ctx context.Context, r *Repo, o RestoreOptions) (RestoreStats, error) {
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	snap, err := r.FindSnapshot(ctx, o.Snapshot)
	if err != nil {
		return RestoreStats{}, err
	}
	m, err := r.LoadManifest(ctx, snap)
	if err != nil {
		return RestoreStats{}, err
	}
	st := RestoreStats{Snapshot: snap.ID}
	prefix := normPath(o.Prefix)
	for _, f := range m.Files {
		if ctx.Err() != nil {
			return st, ctx.Err()
		}
		if prefix != "" && !strings.HasPrefix(normPath(f.Path), prefix) {
			continue
		}
		dest := filepath.Join(o.Target, relativeRestorePath(f.Path))
		if err := restoreFile(ctx, r, f, dest); err != nil {
			o.Log.Warn("restore failed", "path", f.Path, "err", err)
			st.Failed++
			continue
		}
		st.Files++
		st.Bytes += f.Size
	}
	if st.Failed > 0 {
		return st, fmt.Errorf("%d file(s) could not be restored", st.Failed)
	}
	return st, nil
}

func restoreFile(ctx context.Context, r *Repo, f ManifestFile, dest string) error {
	rc, err := r.B.Get(ctx, storage.Object{Key: "blobs/" + f.Blob})
	if err != nil {
		return err
	}
	defer rc.Close()
	plain, err := crypt.NewReader(rc, r.Keys)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".restore-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(tmp, h), plain)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	if hex.EncodeToString(h.Sum(nil)) != f.Hash {
		return errors.New("content does not match recorded hash")
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return err
	}
	mt := time.Unix(0, f.ModTime)
	return os.Chtimes(dest, mt, mt)
}

// relativeRestorePath turns an absolute source path into a relative one:
// C:\a\b -> C\a\b, \\srv\share\a -> UNC\srv\share\a, /home/a -> home/a.
func relativeRestorePath(p string) string {
	vol := filepath.VolumeName(p)
	rest := strings.TrimLeft(p[len(vol):], `\/`)
	switch {
	case vol == "":
	case strings.HasPrefix(vol, `\\`):
		vol = filepath.Join("UNC", strings.TrimLeft(vol, `\`))
	default:
		vol = strings.TrimSuffix(vol, ":")
	}
	if runtime.GOOS != "windows" {
		rest = strings.ReplaceAll(rest, `\`, "/")
	}
	return filepath.Join(vol, filepath.FromSlash(rest))
}
