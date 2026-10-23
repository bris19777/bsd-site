package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeDrive implements the slice of the Drive v3 API the backend uses.
// When flaky is set, every other chunk PUT stores only its first 256 KiB
// and answers 503, exercising the resume path.
type fakeDrive struct {
	t        *testing.T
	mu       sync.Mutex
	files    map[string]*fakeFile
	sessions map[string]*fakeSession
	next     int
	flaky    bool
	puts     int
	srv      *httptest.Server
}

type fakeFile struct {
	id, name, parent, mime string
	data                   []byte
}

type fakeSession struct {
	name, parent string
	data         []byte
}

var (
	reName   = regexp.MustCompile(`name = '((?:\\'|[^'])*)'`)
	reParent = regexp.MustCompile(`'([^']*)' in parents`)
)

func newFakeDrive(t *testing.T) *fakeDrive {
	f := &fakeDrive{t: t, files: map[string]*fakeFile{}, sessions: map[string]*fakeSession{}}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeDrive) id() string { f.next++; return fmt.Sprintf("id%d", f.next) }

func (f *fakeDrive) serve(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer tok" {
		http.Error(w, "no auth", http.StatusUnauthorized)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	path := r.URL.Path
	switch {
	case r.Method == http.MethodGet && path == "/drive/v3/files":
		q := r.URL.Query().Get("q")
		var name string
		if m := reName.FindStringSubmatch(q); m != nil {
			name = strings.ReplaceAll(m[1], `\'`, `'`)
		}
		parent := reParent.FindStringSubmatch(q)[1]
		var out []map[string]string
		for _, file := range f.files {
			if file.parent == parent && (name == "" || file.name == name) {
				out = append(out, map[string]string{"id": file.id, "name": file.name, "size": strconv.Itoa(len(file.data))})
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"files": out})
	case r.Method == http.MethodPost && path == "/drive/v3/files":
		var meta struct {
			Name     string   `json:"name"`
			MimeType string   `json:"mimeType"`
			Parents  []string `json:"parents"`
		}
		json.NewDecoder(r.Body).Decode(&meta)
		file := &fakeFile{id: f.id(), name: meta.Name, parent: meta.Parents[0], mime: meta.MimeType}
		f.files[file.id] = file
		json.NewEncoder(w).Encode(map[string]string{"id": file.id, "name": file.name})
	case r.Method == http.MethodPost && path == "/upload/drive/v3/files":
		var meta struct {
			Name    string   `json:"name"`
			Parents []string `json:"parents"`
		}
		json.NewDecoder(r.Body).Decode(&meta)
		sid := f.id()
		f.sessions[sid] = &fakeSession{name: meta.Name, parent: meta.Parents[0]}
		w.Header().Set("Location", f.srv.URL+"/session/"+sid)
	case r.Method == http.MethodPut && strings.HasPrefix(path, "/session/"):
		f.put(w, r, strings.TrimPrefix(path, "/session/"))
	case r.Method == http.MethodGet && strings.HasPrefix(path, "/drive/v3/files/"):
		file := f.files[strings.TrimPrefix(path, "/drive/v3/files/")]
		if file == nil {
			http.NotFound(w, r)
			return
		}
		w.Write(file.data)
	case r.Method == http.MethodDelete && strings.HasPrefix(path, "/drive/v3/files/"):
		id := strings.TrimPrefix(path, "/drive/v3/files/")
		if f.files[id] == nil {
			http.NotFound(w, r)
			return
		}
		delete(f.files, id)
		w.WriteHeader(http.StatusNoContent)
	default:
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL)
		http.Error(w, "unexpected", http.StatusBadRequest)
	}
}

var reRange = regexp.MustCompile(`^bytes (?:(\d+)-(\d+)|\*)/(\d+|\*)$`)

func (f *fakeDrive) put(w http.ResponseWriter, r *http.Request, sid string) {
	s := f.sessions[sid]
	if s == nil {
		http.NotFound(w, r)
		return
	}
	m := reRange.FindStringSubmatch(r.Header.Get("Content-Range"))
	if m == nil {
		http.Error(w, "bad range "+r.Header.Get("Content-Range"), http.StatusBadRequest)
		return
	}
	body, _ := io.ReadAll(r.Body)
	if m[1] != "" {
		start, _ := strconv.Atoi(m[1])
		if start != len(s.data) {
			http.Error(w, fmt.Sprintf("range starts at %d, have %d", start, len(s.data)), http.StatusBadRequest)
			return
		}
		f.puts++
		if f.flaky && f.puts%2 == 1 && len(body) > 256<<10 {
			s.data = append(s.data, body[:256<<10]...)
			http.Error(w, "flaky", http.StatusServiceUnavailable)
			return
		}
		s.data = append(s.data, body...)
	}
	if m[3] != "*" {
		total, _ := strconv.Atoi(m[3])
		if total == len(s.data) {
			file := &fakeFile{id: f.id(), name: s.name, parent: s.parent, data: s.data}
			f.files[file.id] = file
			delete(f.sessions, sid)
			json.NewEncoder(w).Encode(map[string]string{"id": file.id, "name": file.name, "size": strconv.Itoa(len(file.data))})
			return
		}
	}
	if len(s.data) > 0 {
		w.Header().Set("Range", fmt.Sprintf("bytes=0-%d", len(s.data)-1))
	}
	w.WriteHeader(308)
}

type staticTokens struct{}

func (staticTokens) Token(context.Context, bool) (string, error) { return "tok", nil }

func openFake(t *testing.T, f *fakeDrive) *GDrive {
	g, err := OpenGDrive(context.Background(), GDriveOptions{
		Tokens: staticTokens{}, RepoName: "dev_1",
		APIBase: f.srv.URL + "/drive/v3", UploadBase: f.srv.URL + "/upload/drive/v3",
		ChunkSize: 512 << 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestGDriveRoundTrip(t *testing.T) {
	for _, flaky := range []bool{false, true} {
		t.Run(fmt.Sprintf("flaky=%v", flaky), func(t *testing.T) {
			fd := newFakeDrive(t)
			fd.flaky = flaky
			g := openFake(t, fd)
			ctx := context.Background()
			for _, size := range []int{0, 100, 512 << 10, 512<<10 + 1, 3*512<<10 + 7} {
				data := make([]byte, size)
				rand.Read(data)
				key := fmt.Sprintf("blobs/b%d", size)
				obj, err := g.Put(ctx, key, bytes.NewReader(data))
				if err != nil {
					t.Fatalf("put %d: %v", size, err)
				}
				if obj.Size != int64(size) || obj.ID == "" {
					t.Fatalf("put %d: got %+v", size, obj)
				}
				rc, err := g.Get(ctx, Object{Key: key}) // by name lookup
				if err != nil {
					t.Fatal(err)
				}
				got, _ := io.ReadAll(rc)
				rc.Close()
				if !bytes.Equal(got, data) {
					t.Fatalf("size %d: content mismatch (%d bytes back)", size, len(got))
				}
			}
			objs, err := g.List(ctx, "blobs")
			if err != nil || len(objs) != 5 {
				t.Fatalf("list: %v %d", err, len(objs))
			}
			if err := g.Delete(ctx, objs[0]); err != nil {
				t.Fatal(err)
			}
			if err := g.Delete(ctx, objs[0]); err != nil {
				t.Fatalf("second delete should be a no-op: %v", err)
			}
			if _, err := g.Get(ctx, Object{Key: "blobs/missing"}); err != ErrNotExist {
				t.Fatalf("missing: got %v", err)
			}
		})
	}
}

func TestGDriveConfigReplaced(t *testing.T) {
	fd := newFakeDrive(t)
	g := openFake(t, fd)
	ctx := context.Background()
	g.Put(ctx, "config", strings.NewReader("v1"))
	g.Put(ctx, "config", strings.NewReader("v2"))
	// Reopen so nothing is cached: the folder tree must be found again,
	// and only one config must exist.
	g2 := openFake(t, fd)
	rc, err := g2.Get(ctx, Object{Key: "config"})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(rc)
	if string(b) != "v2" {
		t.Fatalf("config = %q", b)
	}
	n := 0
	for _, f := range fd.files {
		if f.name == "config" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("%d config files", n)
	}
}
