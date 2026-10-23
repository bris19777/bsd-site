package backup

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bsd-backup/internal/api"
	"bsd-backup/internal/storage"
)

type fixture struct {
	t     *testing.T
	src   string
	state string
	b     storage.Backend
	now   time.Time
}

func newFixture(t *testing.T) *fixture {
	ctx := context.Background()
	b, err := storage.OpenNAS(ctx, storage.NASOptions{Path: t.TempDir(), RepoName: "dev_test"})
	if err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, src: t.TempDir(), state: t.TempDir(), b: b,
		now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
}

func (f *fixture) write(rel string, data []byte) {
	p := filepath.Join(f.src, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		f.t.Fatal(err)
	}
	// Distinct mtimes so change detection is exercised deterministically.
	f.now = f.now.Add(time.Minute)
	os.Chtimes(p, f.now, f.now)
}

func (f *fixture) run(encrypt bool, keep api.Retention, quota int64) (api.RunStats, *Repo, error) {
	ctx := context.Background()
	repo, err := OpenRepo(ctx, f.b, RepoOptions{Encrypt: encrypt, Passphrase: "correct horse", DeviceID: "dev_test"})
	if err != nil {
		f.t.Fatal(err)
	}
	idx, err := LoadIndex(f.state)
	if err != nil {
		f.t.Fatal(err)
	}
	f.now = f.now.Add(time.Hour)
	now := f.now
	st, err := Run(ctx, repo, idx, Options{
		Sources: []api.Source{{Path: f.src}}, Exclude: []string{"**/cache/**"},
		Compress: true, Retention: keep, StateDir: f.state, DeviceID: "dev_test",
		QuotaRemaining: quota, Now: func() time.Time { return now },
	})
	return st, repo, err
}

func random(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func TestIncrementalBackupAndRestore(t *testing.T) {
	for _, encrypt := range []bool{false, true} {
		t.Run(map[bool]string{false: "plain", true: "encrypted"}[encrypt], func(t *testing.T) {
			f := newFixture(t)
			big := random(300 << 10)
			f.write("docs/a.txt", []byte("hello"))
			f.write("docs/b.bin", big)
			f.write("docs/copy-of-b.bin", big) // dedup: same content, one blob
			f.write("cache/skip.txt", []byte("excluded"))
			f.write("x.tmp", []byte("excluded by default"))

			st, _, err := f.run(encrypt, api.Retention{}, -1)
			if err != nil {
				t.Fatal(err)
			}
			if st.FilesScanned != 3 || st.FilesChanged != 3 || st.BytesUploaded == 0 {
				t.Fatalf("first run: %+v", st)
			}
			blobs, _ := f.b.List(context.Background(), "blobs")
			if len(blobs) != 2 {
				t.Fatalf("want 2 deduplicated blobs, got %d", len(blobs))
			}

			// Nothing changed: no uploads.
			st, _, err = f.run(encrypt, api.Retention{}, -1)
			if err != nil || st.FilesChanged != 0 || st.BytesUploaded != 0 {
				t.Fatalf("second run: %+v %v", st, err)
			}

			f.write("docs/a.txt", []byte("hello, world"))
			st, repo, err := f.run(encrypt, api.Retention{}, -1)
			if err != nil || st.FilesChanged != 1 {
				t.Fatalf("third run: %+v %v", st, err)
			}

			target := t.TempDir()
			rs, err := Restore(context.Background(), repo, RestoreOptions{Snapshot: "latest", Target: target})
			if err != nil || rs.Files != 3 {
				t.Fatalf("restore: %+v %v", rs, err)
			}
			restored := filepath.Join(target, relativeRestorePath(filepath.Join(f.src, "docs/a.txt")))
			if got, _ := os.ReadFile(restored); string(got) != "hello, world" {
				t.Fatalf("restored a.txt = %q", got)
			}
			got, _ := os.ReadFile(filepath.Join(target, relativeRestorePath(filepath.Join(f.src, "docs/copy-of-b.bin"))))
			if !bytes.Equal(got, big) {
				t.Fatal("restored copy-of-b.bin differs")
			}
		})
	}
}

func TestRetentionDeletesUnreferencedBlobs(t *testing.T) {
	f := newFixture(t)
	keep := api.Retention{KeepLast: 2}
	for i := 0; i < 4; i++ {
		f.write("f.txt", random(1000)) // new content each run
		if _, _, err := f.run(false, keep, -1); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	snaps, _ := f.b.List(ctx, "snapshots")
	blobs, _ := f.b.List(ctx, "blobs")
	if len(snaps) != 2 || len(blobs) != 2 {
		t.Fatalf("after retention: %d snapshots, %d blobs; want 2 and 2", len(snaps), len(blobs))
	}
	// Both kept snapshots must still restore completely.
	repo, _ := OpenRepo(ctx, f.b, RepoOptions{})
	list, _ := repo.ListSnapshots(ctx)
	for _, s := range list {
		if _, err := Restore(ctx, repo, RestoreOptions{Snapshot: s.ID, Target: t.TempDir()}); err != nil {
			t.Fatalf("restore %s: %v", s.ID, err)
		}
	}
}

func TestQuotaStopsUploads(t *testing.T) {
	f := newFixture(t)
	f.write("a.bin", random(10_000))
	f.write("b.bin", random(10_000))
	st, _, err := f.run(false, api.Retention{}, 15_000)
	if !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("want ErrQuotaExceeded, got %v", err)
	}
	if st.FilesScanned != 2 || st.BytesUploaded == 0 || st.BytesUploaded > 15_000 {
		t.Fatalf("stats: %+v", st)
	}
	snaps, _ := f.b.List(context.Background(), "snapshots")
	if len(snaps) != 1 {
		t.Fatalf("want one partial snapshot, got %d", len(snaps))
	}
}

func TestWrongPassphrase(t *testing.T) {
	f := newFixture(t)
	f.write("a", []byte("x"))
	if _, _, err := f.run(true, api.Retention{}, -1); err != nil {
		t.Fatal(err)
	}
	_, err := OpenRepo(context.Background(), f.b, RepoOptions{Passphrase: "wrong"})
	if !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("got %v", err)
	}
	_, err = OpenRepo(context.Background(), f.b, RepoOptions{})
	if !errors.Is(err, ErrNeedPassphrase) {
		t.Fatalf("got %v", err)
	}
}

func TestIndexRebuildAvoidsReupload(t *testing.T) {
	f := newFixture(t)
	f.write("a.bin", random(50_000))
	if _, _, err := f.run(false, api.Retention{}, -1); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(f.state) // e.g. agent reinstalled
	st, _, err := f.run(false, api.Retention{}, -1)
	if err != nil || st.BytesUploaded != 0 {
		t.Fatalf("rebuilt index should reuse stored blobs: %+v %v", st, err)
	}
}

func TestCancelledRunWritesNoSnapshot(t *testing.T) {
	f := newFixture(t)
	f.write("a.bin", random(1000))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	repo, _ := OpenRepo(context.Background(), f.b, RepoOptions{})
	idx, _ := LoadIndex(f.state)
	_, err := Run(ctx, repo, idx, Options{Sources: []api.Source{{Path: f.src}}, QuotaRemaining: -1})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
	snaps, _ := f.b.List(context.Background(), "snapshots")
	if len(snaps) != 0 {
		t.Fatal("cancelled run must not write a snapshot")
	}
}

func TestMatcher(t *testing.T) {
	m := NewMatcher([]string{"*.tmp", "**/node_modules/**", "/data/skip/*.log"})
	cases := map[string]bool{
		"/a/b/file.tmp":             true,
		"/a/b/file.txt":             false,
		"/proj/node_modules/x/y.js": true,
		"/data/skip/a.log":          true,
		"/data/skip/sub/a.log":      false,
		"/data/keep/a.log":          false,
	}
	for p, want := range cases {
		if got := m.Match(p); got != want {
			t.Errorf("Match(%q) = %v, want %v", p, got, want)
		}
	}
	if !m.MatchDir("/proj/node_modules") {
		t.Error("MatchDir should skip node_modules")
	}
}

func TestRelativeRestorePath(t *testing.T) {
	if got := relativeRestorePath("/home/a/b.txt"); got != filepath.FromSlash("home/a/b.txt") {
		t.Fatalf("got %q", got)
	}
}
