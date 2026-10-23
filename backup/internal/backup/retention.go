package backup

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"bsd-backup/internal/api"
	"bsd-backup/internal/storage"
)

// ApplyRetention deletes snapshots the policy no longer keeps, then deletes
// blobs no kept snapshot references (including orphans from interrupted
// runs). It returns the number of blobs deleted. If any kept manifest
// cannot be read, no blob is deleted: an unreadable manifest must never
// cause data loss.
func ApplyRetention(ctx context.Context, repo *Repo, idx *Index, keep api.Retention, now time.Time, log *slog.Logger) (int64, error) {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	snaps := append([]SnapshotState(nil), idx.Snapshots...)
	sort.Slice(snaps, func(i, j int) bool { return snaps[i].Time.After(snaps[j].Time) })

	var kept, drop []SnapshotState
	for i, s := range snaps {
		keepIt := i == 0 || (keep.KeepLast == 0 && keep.KeepDays == 0) ||
			i < keep.KeepLast ||
			(keep.KeepDays > 0 && now.Sub(s.Time) < time.Duration(keep.KeepDays)*24*time.Hour)
		if keepIt {
			kept = append(kept, s)
		} else {
			drop = append(drop, s)
		}
	}

	referenced := map[string]bool{}
	for _, s := range kept {
		m, err := loadCachedManifest(ctx, repo, idx, s)
		if err != nil {
			return 0, fmt.Errorf("read snapshot %s, skipping cleanup: %w", s.ID, err)
		}
		for _, f := range m.Files {
			referenced[f.Blob] = true
		}
	}

	// Snapshots first, then blobs: a crash in between leaves only orphans,
	// which the next run removes.
	for _, s := range drop {
		if err := repo.B.Delete(ctx, storage.Object{Key: "snapshots/" + s.ID, ID: s.RemoteID}); err != nil {
			return 0, fmt.Errorf("delete snapshot %s: %w", s.ID, err)
		}
		os.Remove(idx.manifestPath(s.ID))
		log.Info("snapshot expired", "snapshot", s.ID)
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Time.Before(kept[j].Time) })
	idx.Snapshots = kept

	var deleted int64
	for id, b := range idx.Blobs {
		if referenced[id] {
			continue
		}
		if err := repo.B.Delete(ctx, storage.Object{Key: "blobs/" + id, ID: b.RemoteID}); err != nil {
			idx.Save()
			return deleted, fmt.Errorf("delete blob: %w", err)
		}
		delete(idx.Blobs, id)
		deleted++
	}
	return deleted, idx.Save()
}

func loadCachedManifest(ctx context.Context, repo *Repo, idx *Index, s SnapshotState) (*Manifest, error) {
	raw, err := os.ReadFile(idx.manifestPath(s.ID))
	if err == nil {
		if m, derr := repo.decodeManifest(bytes.NewReader(raw)); derr == nil {
			return m, nil
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	m, err := repo.LoadManifest(ctx, SnapshotInfo{ID: s.ID, Time: s.Time,
		Obj: storage.Object{Key: "snapshots/" + s.ID, ID: s.RemoteID}})
	if err != nil {
		return nil, err
	}
	if raw, err := repo.encodeManifest(m); err == nil {
		os.MkdirAll(filepath.Dir(idx.manifestPath(s.ID)), 0o700)
		os.WriteFile(idx.manifestPath(s.ID), raw, 0o600)
	}
	return m, nil
}
