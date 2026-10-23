package crypt

import (
	"bytes"
	"crypto/rand"
	"errors"
	"io"
	"testing"
)

func roundTrip(t *testing.T, keys *Keys, compress bool, plain []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w, err := NewWriter(&buf, keys, compress)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(plain); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := NewReader(bytes.NewReader(buf.Bytes()), keys)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, plain) {
		t.Fatalf("round trip mismatch: got %d bytes, want %d", len(got), len(plain))
	}
	return buf.Bytes()
}

func TestRoundTrip(t *testing.T) {
	master := make([]byte, KeySize)
	rand.Read(master)
	keys := DeriveKeys(master)
	random := make([]byte, 3*segmentSize+123)
	rand.Read(random)
	for _, size := range []int{0, 1, segmentSize - 1, segmentSize, segmentSize + 1, 2 * segmentSize, len(random)} {
		for _, k := range []*Keys{nil, keys} {
			for _, gz := range []bool{false, true} {
				roundTrip(t, k, gz, random[:size])
			}
		}
	}
}

func TestTamperAndTruncation(t *testing.T) {
	master := make([]byte, KeySize)
	rand.Read(master)
	keys := DeriveKeys(master)
	plain := make([]byte, 2*segmentSize+10)
	rand.Read(plain)
	enc := roundTrip(t, keys, false, plain)

	read := func(b []byte, k *Keys) error {
		r, err := NewReader(bytes.NewReader(b), k)
		if err != nil {
			return err
		}
		_, err = io.ReadAll(r)
		return err
	}

	flipped := append([]byte{}, enc...)
	flipped[len(flipped)/2] ^= 1
	if err := read(flipped, keys); !errors.Is(err, ErrAuth) {
		t.Fatalf("tampered: got %v, want ErrAuth", err)
	}
	// Cut exactly after the first full segment.
	boundary := 6 + prefixSize + segmentSize + tagSize
	if err := read(enc[:boundary], keys); !errors.Is(err, ErrTruncated) {
		t.Fatalf("truncated at boundary: got %v, want ErrTruncated", err)
	}
	if err := read(enc[:6+prefixSize], keys); !errors.Is(err, ErrTruncated) {
		t.Fatalf("truncated to header: got %v, want ErrTruncated", err)
	}
	other := make([]byte, KeySize)
	rand.Read(other)
	if err := read(enc, DeriveKeys(other)); !errors.Is(err, ErrAuth) {
		t.Fatalf("wrong key: got %v, want ErrAuth", err)
	}
	if err := read(enc, nil); !errors.Is(err, ErrNeedKey) {
		t.Fatalf("no key: got %v, want ErrNeedKey", err)
	}
}

func TestBlobIDKeyed(t *testing.T) {
	h := bytes.Repeat([]byte{7}, 32)
	if BlobID(nil, h) == BlobID(DeriveKeys([]byte("k")), h) {
		t.Fatal("keyed blob ID must differ from plain hash")
	}
}
