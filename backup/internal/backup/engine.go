package backup

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bsd-backup/internal/api"
	"bsd-backup/internal/crypt"
	"bsd-backup/internal/storage"
)

var (
	ErrQuotaExceeded     = errors.New("backup: storage quota exceeded")
	errChangedDuringRead = errors.New("file changed while it was being backed up; will retry next run")
)

type Options struct {
	Sources      []api.Source
	Exclude      []string // global, on top of DefaultExcludes
	Compress     bool
	MaxFileSize  int64 // bytes; 0 = no limit
	BandwidthBps int64 // 0 = unlimited
	Retention    api.Retention
	StateDir     string // never backed up
	DeviceID     string
	Hostname     string
	// QuotaRemaining is how many more bytes the account may store; < 0
	// means unlimited. The run stops (with a partial snapshot) before an
	// upload would cross it.
	QuotaRemaining int64
	Progress       func(api.RunStats) // called roughly every ProgressEvery
	ProgressEvery  time.Duration
	Log            *slog.Logger
	Now            func() time.Time
}

type engine struct {
	o         Options
	repo      *Repo
	idx       *Index
	exclude   *Matcher
	limiter   *Limiter
	stats     api.RunStats
	manifest  *Manifest
	seen      map[string]bool
	quotaHit  bool
	lastProg  time.Time
	sinceSave int
}

// Run performs one backup: scan, upload new content, write a snapshot
// manifest, then apply retention. On cancellation (suspension, service
// stop) it saves the index and returns ctx.Err() without a snapshot; blobs
// already uploaded are reused next time.
func Run(ctx context.Context, repo *Repo, idx *Index, o Options) (api.RunStats, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Log == nil {
		o.Log = slog.New(slog.DiscardHandler)
	}
	if o.ProgressEvery == 0 {
		o.ProgressEvery = 30 * time.Second
	}
	start := o.Now()
	e := &engine{
		o: o, repo: repo, idx: idx, limiter: NewLimiter(o.BandwidthBps),
		exclude: NewMatcher(append(append([]string{}, DefaultExcludes...), o.Exclude...)),
		seen:    map[string]bool{}, lastProg: start,
		manifest: &Manifest{
			ID: newSnapshotID(start), Time: start.UTC(), DeviceID: o.DeviceID, Hostname: o.Hostname,
		},
	}
	finish := func(err error) (api.RunStats, error) {
		if serr := idx.Save(); serr != nil && err == nil {
			err = fmt.Errorf("save index: %w", serr)
		}
		e.stats.StoredBytes = idx.StoredBytes()
		e.stats.DurationMillis = o.Now().Sub(start).Milliseconds()
		return e.stats, err
	}

	if err := idx.sync(ctx, repo); err != nil {
		return finish(fmt.Errorf("read repository: %w", err))
	}
	for _, src := range o.Sources {
		roots, err := expandSource(src.Path)
		if err != nil || len(roots) == 0 {
			o.Log.Warn("source not found", "path", src.Path, "err", err)
			e.stats.FilesFailed++
			continue
		}
		include := NewMatcher(src.Include)
		exclude := NewMatcher(src.Exclude)
		for _, root := range roots {
			e.manifest.Sources = append(e.manifest.Sources, root)
			if err := e.walk(ctx, root, include, exclude); err != nil {
				return finish(err)
			}
			if e.quotaHit {
				break
			}
		}
		if e.quotaHit {
			break
		}
	}
	if ctx.Err() != nil {
		return finish(ctx.Err())
	}

	// Files that disappeared stay in older snapshots but leave the index.
	if !e.quotaHit {
		for p := range idx.Files {
			if !e.seen[p] {
				delete(idx.Files, p)
			}
		}
	}
	if err := e.writeSnapshot(ctx); err != nil {
		return finish(fmt.Errorf("write snapshot: %w", err))
	}
	if e.quotaHit {
		return finish(ErrQuotaExceeded)
	}
	deleted, err := ApplyRetention(ctx, repo, idx, o.Retention, o.Now(), o.Log)
	e.stats.BlobsDeleted = deleted
	if err != nil {
		o.Log.Warn("retention", "err", err)
	}
	return finish(nil)
}

// expandSource resolves environment variables and glob patterns, so a
// policy can say C:\Users\*\Documents to cover every profile.
func expandSource(p string) ([]string, error) {
	p = filepath.Clean(os.ExpandEnv(p))
	if !strings.ContainsAny(p, "*?[") {
		if _, err := os.Stat(p); err != nil {
			return nil, err
		}
		return []string{p}, nil
	}
	return filepath.Glob(p)
}

func (e *engine) walk(ctx context.Context, root string, include, exclude *Matcher) error {
	stateDir := filepath.Clean(e.o.StateDir)
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if e.quotaHit {
			return fs.SkipAll
		}
		if err != nil {
			e.o.Log.Warn("cannot read", "path", path, "err", err)
			e.stats.FilesFailed++
			return nil
		}
		if d.IsDir() {
			if path != root && (d.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 ||
				(e.o.StateDir != "" && path == stateDir) ||
				e.exclude.MatchDir(path) || exclude.MatchDir(path)) {
				return fs.SkipDir
			}
			return nil
		}
		if !d.Type().IsRegular() || e.exclude.Match(path) || exclude.Match(path) {
			return nil
		}
		if !include.Empty() {
			rel, _ := filepath.Rel(root, path)
			if !include.Match(path) && !include.Match(rel) {
				return nil
			}
		}
		info, err := d.Info()
		if err != nil {
			e.stats.FilesFailed++
			return nil
		}
		if e.o.MaxFileSize > 0 && info.Size() > e.o.MaxFileSize {
			return nil
		}
		if err := e.file(ctx, path, info); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if errors.Is(err, ErrQuotaExceeded) {
				e.quotaHit = true
				return fs.SkipAll
			}
			e.o.Log.Warn("backup failed", "path", path, "err", err)
			e.stats.FilesFailed++
		}
		e.progress()
		return nil
	})
}

func (e *engine) file(ctx context.Context, path string, info fs.FileInfo) error {
	e.stats.FilesScanned++
	e.stats.BytesScanned += info.Size()
	size, mtime := info.Size(), info.ModTime().UnixNano()

	prev, known := e.idx.Files[path]
	var sum []byte
	if known && prev.Size == size && prev.ModTime == mtime && prev.Hash != "" {
		sum, _ = hex.DecodeString(prev.Hash)
	}
	// Trust a cached hash only while its blob is stored; before uploading,
	// always hash the bytes actually on disk.
	if sum == nil || !e.hasBlob(sum) {
		var err error
		if sum, err = hashFile(ctx, path); err != nil {
			return err
		}
	}
	hashHex := hex.EncodeToString(sum)
	blob := crypt.BlobID(e.repo.Keys, sum)
	if !known || prev.Hash != hashHex {
		e.stats.FilesChanged++
	}

	if _, stored := e.idx.Blobs[blob]; !stored {
		if e.o.QuotaRemaining >= 0 && e.stats.BytesUploaded+size > e.o.QuotaRemaining {
			return ErrQuotaExceeded
		}
		obj, err := e.uploadWithRetry(ctx, path, sum, blob)
		if err != nil {
			return err
		}
		e.idx.Blobs[blob] = BlobState{Size: obj.Size, RemoteID: obj.ID, Uploaded: e.o.Now().UTC()}
		e.stats.BytesUploaded += obj.Size
		if e.sinceSave++; e.sinceSave >= 50 {
			e.sinceSave = 0
			e.idx.Save()
		}
	}

	e.idx.Files[path] = FileState{Size: size, ModTime: mtime, Hash: hashHex}
	e.seen[path] = true
	e.manifest.Files = append(e.manifest.Files, ManifestFile{
		Path: path, Size: size, ModTime: mtime, Hash: hashHex, Blob: blob,
	})
	return nil
}

func (e *engine) hasBlob(sum []byte) bool {
	_, ok := e.idx.Blobs[crypt.BlobID(e.repo.Keys, sum)]
	return ok
}

func (e *engine) uploadWithRetry(ctx context.Context, path string, sum []byte, blob string) (storage.Object, error) {
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		obj, err := e.upload(ctx, path, sum, blob)
		if err == nil || errors.Is(err, errChangedDuringRead) || ctx.Err() != nil || errors.Is(err, fs.ErrNotExist) {
			return obj, err
		}
		lastErr = err
		e.o.Log.Warn("upload failed, retrying", "path", path, "attempt", attempt+1, "err", err)
		select {
		case <-ctx.Done():
			return storage.Object{}, ctx.Err()
		case <-time.After(time.Duration(attempt+1) * 5 * time.Second):
		}
	}
	return storage.Object{}, lastErr
}

// upload streams file -> (gzip) -> (AES-GCM) -> throttle -> backend, and
// re-hashes on the way so a file modified mid-upload is never recorded
// under the wrong content ID.
func (e *engine) upload(ctx context.Context, path string, want []byte, blob string) (storage.Object, error) {
	f, err := os.Open(path)
	if err != nil {
		return storage.Object{}, err
	}
	defer f.Close()

	h := sha256.New()
	pr, pw := io.Pipe()
	done := make(chan error, 1)
	go func() {
		w, err := crypt.NewWriter(pw, e.repo.Keys, e.o.Compress && compressible(path))
		if err == nil {
			_, err = io.Copy(w, io.TeeReader(f, h))
			if cerr := w.Close(); err == nil {
				err = cerr
			}
		}
		pw.CloseWithError(err)
		done <- err
	}()
	obj, err := e.repo.B.Put(ctx, "blobs/"+blob, e.limiter.Reader(ctx, pr))
	pr.CloseWithError(errors.New("upload aborted")) // unblock the encoder if Put stopped early
	encErr := <-done
	if err == nil && encErr != nil {
		err = encErr
	}
	if err != nil {
		return storage.Object{}, err
	}
	if !bytes.Equal(h.Sum(nil), want) {
		e.repo.B.Delete(ctx, obj)
		return storage.Object{}, errChangedDuringRead
	}
	return obj, nil
}

func (e *engine) writeSnapshot(ctx context.Context) error {
	e.manifest.Partial = e.quotaHit
	raw, err := e.repo.encodeManifest(e.manifest)
	if err != nil {
		return err
	}
	obj, err := e.repo.B.Put(ctx, "snapshots/"+e.manifest.ID, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(e.idx.manifestPath(e.manifest.ID)), 0o700); err == nil {
		os.WriteFile(e.idx.manifestPath(e.manifest.ID), raw, 0o600)
	}
	e.idx.Snapshots = append(e.idx.Snapshots, SnapshotState{
		ID: e.manifest.ID, Time: e.manifest.Time, RemoteID: obj.ID, Size: obj.Size,
		Files: len(e.manifest.Files), Partial: e.manifest.Partial,
	})
	e.stats.SnapshotID = e.manifest.ID
	return e.idx.Save()
}

func (e *engine) progress() {
	if e.o.Progress == nil || e.o.Now().Sub(e.lastProg) < e.o.ProgressEvery {
		return
	}
	e.lastProg = e.o.Now()
	s := e.stats
	s.StoredBytes = e.idx.StoredBytes()
	e.o.Progress(s)
}

func hashFile(ctx context.Context, path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	h := sha256.New()
	if err := copyCtx(ctx, h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

func copyCtx(ctx context.Context, h hash.Hash, r io.Reader) error {
	buf := make([]byte, 1<<20)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, err := r.Read(buf)
		h.Write(buf[:n])
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

var incompressibleExt = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".gif": true, ".webp": true, ".heic": true, ".avif": true,
	".mp4": true, ".mov": true, ".mkv": true, ".avi": true, ".webm": true, ".mp3": true, ".m4a": true,
	".aac": true, ".flac": true, ".ogg": true, ".zip": true, ".7z": true, ".rar": true, ".gz": true,
	".bz2": true, ".xz": true, ".zst": true, ".docx": true, ".xlsx": true, ".pptx": true, ".cab": true,
}

func compressible(path string) bool {
	return !incompressibleExt[strings.ToLower(filepath.Ext(path))]
}
