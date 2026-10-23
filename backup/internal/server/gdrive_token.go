package server

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	defaultTokenURI = "https://oauth2.googleapis.com/token"
	defaultScope    = "https://www.googleapis.com/auth/drive"
)

// tokenMinter exchanges an account's stored Google credentials for access
// tokens, caching each until shortly before it expires.
type tokenMinter struct {
	client *http.Client
	mu     sync.Mutex
	cache  map[string]cachedToken // by account ID
}

type cachedToken struct {
	token string
	exp   time.Time
	key   string // fingerprint of the creds that produced it
}

func newTokenMinter(c *http.Client) *tokenMinter {
	return &tokenMinter{client: c, cache: map[string]cachedToken{}}
}

func (m *tokenMinter) Token(ctx context.Context, accountID string, c *GDriveCreds) (string, time.Time, error) {
	fp := credsFingerprint(c)
	m.mu.Lock()
	if t, ok := m.cache[accountID]; ok && t.key == fp && time.Until(t.exp) > 5*time.Minute {
		m.mu.Unlock()
		return t.token, t.exp, nil
	}
	m.mu.Unlock()

	var form url.Values
	var tokenURI string
	scope := c.Scope
	if scope == "" {
		scope = defaultScope
	}
	switch {
	case c.OAuth != nil:
		tokenURI = orDefault(c.OAuth.TokenURI, defaultTokenURI)
		form = url.Values{
			"grant_type":    {"refresh_token"},
			"client_id":     {c.OAuth.ClientID},
			"client_secret": {c.OAuth.ClientSecret},
			"refresh_token": {c.OAuth.RefreshToken},
		}
	case c.ServiceAccount != nil:
		tokenURI = orDefault(c.ServiceAccount.TokenURI, defaultTokenURI)
		assertion, err := serviceAccountJWT(c.ServiceAccount, tokenURI, scope)
		if err != nil {
			return "", time.Time{}, err
		}
		form = url.Values{
			"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"},
			"assertion":  {assertion},
		}
	default:
		return "", time.Time{}, errors.New("account has no Google Drive credentials")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", time.Time{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := m.client.Do(req)
	if err != nil {
		return "", time.Time{}, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", time.Time{}, fmt.Errorf("google token endpoint: %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tr); err != nil || tr.AccessToken == "" {
		return "", time.Time{}, fmt.Errorf("google token endpoint: unexpected response")
	}
	exp := time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	m.mu.Lock()
	m.cache[accountID] = cachedToken{token: tr.AccessToken, exp: exp, key: fp}
	m.mu.Unlock()
	return tr.AccessToken, exp, nil
}

func serviceAccountJWT(sa *ServiceAccountCreds, aud, scope string) (string, error) {
	block, _ := pem.Decode([]byte(sa.PrivateKey))
	if block == nil {
		return "", errors.New("service account private_key is not PEM")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		if k, err2 := x509.ParsePKCS1PrivateKey(block.Bytes); err2 == nil {
			parsed = k
		} else {
			return "", fmt.Errorf("parse service account key: %w", err)
		}
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return "", errors.New("service account key is not RSA")
	}
	now := time.Now()
	claims := map[string]any{
		"iss": sa.ClientEmail, "scope": scope, "aud": aud,
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	}
	if sa.Subject != "" {
		claims["sub"] = sa.Subject
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	payload, _ := json.Marshal(claims)
	unsigned := header + "." + base64.RawURLEncoding.EncodeToString(payload)
	sum := sha256.Sum256([]byte(unsigned))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func credsFingerprint(c *GDriveCreds) string {
	raw, _ := json.Marshal(c)
	sum := sha256.Sum256(raw)
	return base64.RawStdEncoding.EncodeToString(sum[:8])
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
