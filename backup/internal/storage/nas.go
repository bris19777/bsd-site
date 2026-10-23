package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// NASOptions configures the NAS backend. Path is a UNC path such as
// \\nas.tailnet-abc.ts.net\backups (Windows) or a mounted directory.
type NASOptions struct {
	Path     string
	Username string
	Password string
	RepoName string // per-device subdirectory
}

// NAS stores objects as files. Blobs are sharded by the first two hex
// characters of their ID to keep directories small.
type NAS struct {
	root string
}

// OpenNAS checks that the share is reachable (over Tailscale for remote
// hosts), connects with credentials if given, and creates the repository
// directories.
func OpenNAS(ctx context.Context, o NASOptions) (*NAS, error) {
	if o.Path == "" || o.RepoName == "" {
		return nil, errors.New("nas: path and repo name are required")
	}
	if host := uncHost(o.Path); host != "" {
		if err := probeSMB(ctx, host); err != nil {
			return nil, fmt.Errorf("nas: %s is unreachable (is Tailscale connected on this PC?): %w", host, err)
		}
		if o.Username != "" {
			if err := connectShare(uncShare(o.Path), o.Username, o.Password); err != nil {
				return nil, fmt.Errorf("nas: connect %s: %w", uncShare(o.Path), err)
			}
		}
	}
	n := &NAS{root: filepath.Join(o.Path, o.RepoName)}
	for _, d := range []string{"blobs", "snapshots"} {
		if err := os.MkdirAll(filepath.Join(n.root, d), 0o700); err != nil {
			return nil, fmt.Errorf("nas: %w", err)
		}
	}
	return n, nil
}

func (n *NAS) Describe() string { return n.root }

func (n *NAS) path(key string) (string, error) {
	if !validKey(key) {
		return "", fmt.Errorf("nas: invalid key %q", key)
	}
	dir, name := splitKey(key)
	if dir == "blobs" && len(name) > 2 {
		return filepath.Join(n.root, dir, name[:2], name), nil
	}
	return filepath.Join(n.root, dir, name), nil
}

func (n *NAS) Put(ctx context.Context, key string, r io.Reader) (Object, error) {
	p, err := n.path(key)
	if err != nil {
		return Object{}, err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return Object{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(p), ".upload-*")
	if err != nil {
		return Object{}, err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	size, err := io.Copy(tmp, ctxReader{ctx, r})
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return Object{}, err
	}
	if err := os.Rename(tmp.Name(), p); err != nil {
		return Object{}, err
	}
	return Object{Key: key, Size: size}, nil
}

func (n *NAS) Get(_ context.Context, obj Object) (io.ReadCloser, error) {
	p, err := n.path(obj.Key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ErrNotExist
	}
	return f, err
}

func (n *NAS) Delete(_ context.Context, obj Object) error {
	p, err := n.path(obj.Key)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

func (n *NAS) List(ctx context.Context, prefix string) ([]Object, error) {
	var out []Object
	base := filepath.Join(n.root, prefix)
	err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if d.IsDir() || strings.HasPrefix(d.Name(), ".upload-") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		out = append(out, Object{Key: prefix + "/" + d.Name(), Size: info.Size()})
		return nil
	})
	return out, err
}

// uncHost returns "nas" for \\nas\share\dir, or "" for a local path.
func uncHost(p string) string {
	p = strings.ReplaceAll(p, "/", `\`)
	if !strings.HasPrefix(p, `\\`) {
		return ""
	}
	host, _, _ := strings.Cut(p[2:], `\`)
	return host
}

// uncShare returns \\nas\share for \\nas\share\dir.
func uncShare(p string) string {
	p = strings.ReplaceAll(p, "/", `\`)
	parts := strings.SplitN(p[2:], `\`, 3)
	if len(parts) < 2 {
		return p
	}
	return `\\` + parts[0] + `\` + parts[1]
}

// probeSMB confirms TCP 445 on the NAS answers. Over Tailscale this fails
// fast when the tailnet is down instead of hanging inside the SMB client.
func probeSMB(ctx context.Context, host string) error {
	d := net.Dialer{Timeout: 5 * time.Second}
	c, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, "445"))
	if err != nil {
		return err
	}
	return c.Close()
}

// ctxReader stops a copy when the context is cancelled, so suspending a
// backup interrupts a large file promptly.
type ctxReader struct {
	ctx context.Context
	r   io.Reader
}

func (c ctxReader) Read(p []byte) (int, error) {
	if err := c.ctx.Err(); err != nil {
		return 0, err
	}
	return c.r.Read(p)
}
