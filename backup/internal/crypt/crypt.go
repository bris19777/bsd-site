// Package crypt implements the on-storage object format: an optional gzip
// layer followed by optional AES-256-GCM in fixed-size authenticated
// segments, so arbitrarily large files stream with constant memory.
//
//	"BSDB" | version(1) | flags(1) | [noncePrefix(7)] | segments...
//
// Each segment holds up to 64 KiB of plaintext plus a 16-byte tag. Its
// nonce is noncePrefix || counter(4, big endian) || lastFlag(1), so
// reordering, dropping or truncating segments fails authentication.
package crypt

import (
	"bufio"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
)

const (
	version     = 1
	flagGzip    = 1 << 0
	flagAESGCM  = 1 << 1
	segmentSize = 64 << 10
	tagSize     = 16
	prefixSize  = 7
	KeySize     = 32

	// KDFIterations is the PBKDF2-SHA256 work factor for passphrases.
	KDFIterations = 600_000
)

var magic = [4]byte{'B', 'S', 'D', 'B'}

var (
	ErrFormat    = errors.New("crypt: not a BSD backup object")
	ErrAuth      = errors.New("crypt: authentication failed (wrong key or corrupted data)")
	ErrNeedKey   = errors.New("crypt: object is encrypted but no key is configured")
	ErrTruncated = errors.New("crypt: object is truncated")
)

// Keys are derived from one master key so a single secret protects both
// content (EncKey) and blob names (IDKey).
type Keys struct {
	EncKey []byte
	IDKey  []byte
}

func DeriveKeys(master []byte) *Keys {
	return &Keys{EncKey: label(master, "bsd-backup enc"), IDKey: label(master, "bsd-backup id")}
}

// KeyCheck lets a repository verify a passphrase without storing it.
func KeyCheck(master []byte) []byte { return label(master, "bsd-backup key check") }

func label(key []byte, l string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(l))
	return m.Sum(nil)
}

// KeyFromPassphrase stretches a passphrase into a master key.
func KeyFromPassphrase(passphrase string, salt []byte, iterations int) ([]byte, error) {
	return pbkdf2.Key(sha256.New, passphrase, salt, iterations, KeySize)
}

// BlobID names content. Without encryption it is the plaintext SHA-256;
// with encryption it is keyed so storage never learns content hashes.
func BlobID(keys *Keys, plainSHA256 []byte) string {
	if keys == nil {
		return hex.EncodeToString(plainSHA256)
	}
	return hex.EncodeToString(label(keys.IDKey, string(plainSHA256)))
}

// NewWriter returns a writer that encodes into w. Close must be called to
// flush the final segment; it does not close w.
func NewWriter(w io.Writer, keys *Keys, compress bool) (io.WriteCloser, error) {
	header := []byte{magic[0], magic[1], magic[2], magic[3], version, 0}
	if compress {
		header[5] |= flagGzip
	}
	var sink io.WriteCloser = nopCloser{w}
	if keys != nil {
		header[5] |= flagAESGCM
		prefix := make([]byte, prefixSize)
		if _, err := rand.Read(prefix); err != nil {
			return nil, err
		}
		aead, err := newAEAD(keys.EncKey)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(append(append([]byte{}, header...), prefix...)); err != nil {
			return nil, err
		}
		sink = &sealWriter{w: w, aead: aead, prefix: prefix, aad: header}
	} else if _, err := w.Write(header); err != nil {
		return nil, err
	}
	if !compress {
		return sink, nil
	}
	gz, err := gzip.NewWriterLevel(sink, gzip.DefaultCompression)
	if err != nil {
		return nil, err
	}
	return &chainCloser{Writer: gz, closers: []io.Closer{gz, sink}}, nil
}

// NewReader decodes an object written by NewWriter. keys may be nil for
// unencrypted repositories.
func NewReader(r io.Reader, keys *Keys) (io.Reader, error) {
	var header [6]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, ErrFormat
	}
	if [4]byte(header[:4]) != magic || header[4] != version {
		return nil, ErrFormat
	}
	flags := header[5]
	var out io.Reader = r
	if flags&flagAESGCM != 0 {
		if keys == nil {
			return nil, ErrNeedKey
		}
		prefix := make([]byte, prefixSize)
		if _, err := io.ReadFull(r, prefix); err != nil {
			return nil, ErrTruncated
		}
		aead, err := newAEAD(keys.EncKey)
		if err != nil {
			return nil, err
		}
		out = &openReader{r: bufio.NewReaderSize(r, segmentSize+tagSize+1), aead: aead,
			prefix: prefix, aad: append([]byte{}, header[:]...)}
	}
	if flags&flagGzip != 0 {
		gz, err := gzip.NewReader(out)
		if err != nil {
			if errors.Is(err, ErrAuth) || errors.Is(err, ErrTruncated) {
				return nil, err
			}
			return nil, fmt.Errorf("crypt: gzip: %w", err)
		}
		out = gz
	}
	return out, nil
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func nonce(prefix []byte, counter uint32, last bool) []byte {
	n := make([]byte, 12)
	copy(n, prefix)
	binary.BigEndian.PutUint32(n[prefixSize:], counter)
	if last {
		n[11] = 1
	}
	return n
}

type sealWriter struct {
	w       io.Writer
	aead    cipher.AEAD
	prefix  []byte
	aad     []byte
	buf     []byte
	counter uint32
	closed  bool
}

func (s *sealWriter) Write(p []byte) (int, error) {
	s.buf = append(s.buf, p...)
	// Keep at least one byte back: only Close knows which segment is last.
	for len(s.buf) > segmentSize {
		if err := s.seal(s.buf[:segmentSize], false); err != nil {
			return 0, err
		}
		s.buf = append(s.buf[:0], s.buf[segmentSize:]...)
	}
	return len(p), nil
}

func (s *sealWriter) seal(plain []byte, last bool) error {
	if s.counter == ^uint32(0) {
		return errors.New("crypt: object too large")
	}
	ct := s.aead.Seal(nil, nonce(s.prefix, s.counter, last), plain, s.aad)
	s.counter++
	_, err := s.w.Write(ct)
	return err
}

func (s *sealWriter) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	return s.seal(s.buf, true)
}

type openReader struct {
	r       *bufio.Reader
	aead    cipher.AEAD
	prefix  []byte
	aad     []byte
	counter uint32
	plain   []byte
	done    bool
	seg     []byte
}

func (o *openReader) Read(p []byte) (int, error) {
	for len(o.plain) == 0 {
		if o.done {
			return 0, io.EOF
		}
		if err := o.next(); err != nil {
			return 0, err
		}
	}
	n := copy(p, o.plain)
	o.plain = o.plain[n:]
	return n, nil
}

func (o *openReader) next() error {
	if o.seg == nil {
		o.seg = make([]byte, segmentSize+tagSize)
	}
	n, err := io.ReadFull(o.r, o.seg)
	last := false
	switch {
	case err == io.EOF || (err == io.ErrUnexpectedEOF && n < tagSize):
		return ErrTruncated
	case err == io.ErrUnexpectedEOF:
		last = true
	case err != nil:
		return err
	default:
		if _, perr := o.r.Peek(1); perr == io.EOF {
			last = true
		} else if perr != nil {
			return perr
		}
	}
	plain, aerr := o.aead.Open(o.seg[:0:0], nonce(o.prefix, o.counter, last), o.seg[:n], o.aad)
	if aerr != nil {
		// Data that ends on a segment sealed as non-final means the tail
		// was cut off at a segment boundary.
		if last {
			if _, e := o.aead.Open(nil, nonce(o.prefix, o.counter, false), o.seg[:n], o.aad); e == nil {
				return ErrTruncated
			}
		}
		return ErrAuth
	}
	o.counter++
	o.plain = plain
	o.done = last
	return nil
}

type nopCloser struct{ io.Writer }

func (nopCloser) Close() error { return nil }

type chainCloser struct {
	io.Writer
	closers []io.Closer
}

func (c *chainCloser) Close() error {
	for _, cl := range c.closers {
		if err := cl.Close(); err != nil {
			return err
		}
	}
	return nil
}
