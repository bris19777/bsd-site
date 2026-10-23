// Package storage abstracts the destinations agents write to directly:
// Google Drive (REST API) and an SMB/NAS share reached over Tailscale.
//
// A repository holds three kinds of objects, addressed by key:
//
//	config              repository parameters (salt, key check)
//	blobs/<id>          file contents, named by content hash
//	snapshots/<id>      one manifest per backup run
package storage

import (
	"context"
	"errors"
	"io"
	"strings"
)

var ErrNotExist = errors.New("storage: object does not exist")

// Object describes a stored object. ID is backend specific (the Drive file
// ID); it may be empty, in which case backends look the object up by Key.
type Object struct {
	Key  string `json:"key"`
	ID   string `json:"id,omitempty"`
	Size int64  `json:"size"`
}

type Backend interface {
	// Put stores r under key, replacing any existing object.
	Put(ctx context.Context, key string, r io.Reader) (Object, error)
	Get(ctx context.Context, obj Object) (io.ReadCloser, error)
	// Delete removes obj; deleting a missing object is not an error.
	Delete(ctx context.Context, obj Object) error
	// List returns every object under a top-level prefix ("blobs", "snapshots").
	List(ctx context.Context, prefix string) ([]Object, error)
	// Describe names the destination for logs, e.g. a path or folder ID.
	Describe() string
}

// splitKey returns ("", "config") for "config" and ("blobs", "abc") for
// "blobs/abc".
func splitKey(key string) (dir, name string) {
	if i := strings.IndexByte(key, '/'); i >= 0 {
		return key[:i], key[i+1:]
	}
	return "", key
}

func validKey(key string) bool {
	dir, name := splitKey(key)
	if name == "" || strings.ContainsAny(name, `/\:*?"<>|`) || name == "." || name == ".." {
		return false
	}
	return dir == "" || dir == "blobs" || dir == "snapshots"
}
