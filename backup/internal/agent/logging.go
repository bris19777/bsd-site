package agent

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"bsd-backup/internal/api"
)

const (
	maxLogFile    = 10 << 20
	maxShipBuffer = 500
)

// NewLogger writes text logs to <dir>/agent.log (rotated at 10 MiB, one
// old file kept) and, if console is non-nil, to it as well. Warnings and
// errors are also buffered for shipping to the management server.
func NewLogger(dir string, console io.Writer) (*slog.Logger, *logBuffer, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, nil, err
	}
	rw := &rotatingFile{path: filepath.Join(dir, "agent.log")}
	var w io.Writer = rw
	if console != nil {
		w = io.MultiWriter(rw, console)
	}
	buf := &logBuffer{}
	h := &shipHandler{Handler: slog.NewTextHandler(w, nil), buf: buf}
	return slog.New(h), buf, nil
}

type logBuffer struct {
	mu      sync.Mutex
	entries []api.LogEntry
}

func (b *logBuffer) add(e api.LogEntry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries = append(b.entries, e)
	if n := len(b.entries); n > maxShipBuffer {
		b.entries = b.entries[n-maxShipBuffer:]
	}
}

func (b *logBuffer) drain() []api.LogEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := b.entries
	b.entries = nil
	return out
}

func (b *logBuffer) requeue(e []api.LogEntry) {
	b.mu.Lock()
	b.entries = append(e, b.entries...)
	if n := len(b.entries); n > maxShipBuffer {
		b.entries = b.entries[n-maxShipBuffer:]
	}
	b.mu.Unlock()
}

type shipHandler struct {
	slog.Handler
	buf   *logBuffer
	attrs []slog.Attr
}

func (h *shipHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Level >= slog.LevelWarn {
		msg := r.Message
		for _, a := range h.attrs {
			msg += " " + a.String()
		}
		r.Attrs(func(a slog.Attr) bool {
			msg += " " + a.String()
			return true
		})
		h.buf.add(api.LogEntry{Time: r.Time.UTC(), Level: r.Level.String(), Message: msg})
	}
	return h.Handler.Handle(ctx, r)
}

func (h *shipHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &shipHandler{Handler: h.Handler.WithAttrs(attrs), buf: h.buf, attrs: append(append([]slog.Attr{}, h.attrs...), attrs...)}
}

func (h *shipHandler) WithGroup(name string) slog.Handler {
	return &shipHandler{Handler: h.Handler.WithGroup(name), buf: h.buf, attrs: h.attrs}
}

type rotatingFile struct {
	mu   sync.Mutex
	path string
	f    *os.File
	size int64
}

func (r *rotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil || r.size+int64(len(p)) > maxLogFile {
		if r.f != nil {
			r.f.Close()
			os.Rename(r.path, r.path+".1")
		}
		f, err := os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
		if err != nil {
			return 0, err
		}
		info, _ := f.Stat()
		r.f, r.size = f, 0
		if info != nil {
			r.size = info.Size()
		}
		if r.size+int64(len(p)) > maxLogFile && r.size > 0 {
			r.f.Close()
			os.Rename(r.path, r.path+".1")
			if r.f, err = os.OpenFile(r.path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600); err != nil {
				return 0, err
			}
			r.size = 0
		}
	}
	n, err := r.f.Write(p)
	r.size += int64(n)
	return n, err
}
