// Package auth implements password hashing, opaque device tokens and
// HS256 JWT session tokens using only the standard library.
package auth

import (
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const pbkdf2Iterations = 600_000

// HashPassword returns "pbkdf2-sha256$<iter>$<salt>$<hash>".
func HashPassword(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iterations, 32)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("pbkdf2-sha256$%d$%s$%s", pbkdf2Iterations,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

// CheckPassword reports whether password matches a HashPassword result.
func CheckPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" {
		return false
	}
	iter, err := strconv.Atoi(parts[1])
	if err != nil || iter < 1 {
		return false
	}
	salt, err1 := base64.RawStdEncoding.DecodeString(parts[2])
	want, err2 := base64.RawStdEncoding.DecodeString(parts[3])
	if err1 != nil || err2 != nil {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, password, salt, iter, len(want))
	if err != nil {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

// NewToken returns a random 256-bit token, hex encoded.
func NewToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err) // crypto/rand never fails on supported platforms
	}
	return hex.EncodeToString(b)
}

// NewID returns a short random identifier with a prefix, e.g. "dev_3f9a...".
func NewID(prefix string) string {
	b := make([]byte, 9)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return prefix + "_" + hex.EncodeToString(b)
}

// HashToken is how opaque tokens are stored at rest.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// TokenMatches compares a presented token with a stored HashToken value.
func TokenMatches(storedHash, token string) bool {
	return subtle.ConstantTimeCompare([]byte(storedHash), []byte(HashToken(token))) == 1
}

// Claims carried in device session tokens.
type Claims struct {
	Subject   string `json:"sub"` // device ID
	AccountID string `json:"acc"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

var (
	ErrTokenInvalid = errors.New("invalid token")
	ErrTokenExpired = errors.New("token expired")
)

var jwtHeader = base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))

// Signer issues and verifies HS256 JWTs.
type Signer struct {
	key []byte
	now func() time.Time
}

func NewSigner(key []byte) *Signer {
	return &Signer{key: key, now: time.Now}
}

func (s *Signer) Issue(deviceID, accountID string, ttl time.Duration) (string, time.Time, error) {
	now := s.now()
	exp := now.Add(ttl)
	payload, err := json.Marshal(Claims{
		Subject: deviceID, AccountID: accountID,
		IssuedAt: now.Unix(), ExpiresAt: exp.Unix(),
	})
	if err != nil {
		return "", time.Time{}, err
	}
	unsigned := jwtHeader + "." + base64.RawURLEncoding.EncodeToString(payload)
	return unsigned + "." + s.sign(unsigned), exp, nil
}

func (s *Signer) Verify(token string) (*Claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != jwtHeader {
		return nil, ErrTokenInvalid
	}
	want := s.sign(parts[0] + "." + parts[1])
	if !hmac.Equal([]byte(want), []byte(parts[2])) {
		return nil, ErrTokenInvalid
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrTokenInvalid
	}
	var c Claims
	if err := json.Unmarshal(raw, &c); err != nil || c.Subject == "" {
		return nil, ErrTokenInvalid
	}
	if s.now().Unix() >= c.ExpiresAt {
		return nil, ErrTokenExpired
	}
	return &c, nil
}

func (s *Signer) sign(data string) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(data))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}
