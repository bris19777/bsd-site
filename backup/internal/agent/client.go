package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"bsd-backup/internal/api"
)

// ErrRevoked means the server rejected this device's credentials for good.
var ErrRevoked = errors.New("device credentials revoked or invalid; re-enroll this PC")

type APIError struct {
	Status int
	Msg    string
}

func (e *APIError) Error() string { return fmt.Sprintf("server: %d %s", e.Status, e.Msg) }

// Client talks to the management server. It caches the device JWT and
// renews it with the device token when it expires or is rejected.
type Client struct {
	base        string
	http        *http.Client
	deviceID    string
	deviceToken string

	mu  sync.Mutex
	jwt string
	exp time.Time
}

func NewClient(base, deviceID, deviceToken string) *Client {
	return &Client{
		base: strings.TrimRight(base, "/"), http: &http.Client{Timeout: 60 * time.Second},
		deviceID: deviceID, deviceToken: deviceToken,
	}
}

// CheckServerURL requires HTTPS except for loopback addresses (testing).
func CheckServerURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("invalid server URL %q", raw)
	}
	host := u.Hostname()
	if u.Scheme != "https" && !(u.Scheme == "http" && (host == "localhost" || host == "127.0.0.1" || host == "::1")) {
		return errors.New("server URL must use https://")
	}
	return nil
}

// Enroll registers this PC with the account's username and password.
func Enroll(ctx context.Context, base string, req api.EnrollRequest) (api.EnrollResponse, error) {
	var resp api.EnrollResponse
	c := NewClient(base, "", "")
	err := c.do(ctx, "/api/v1/auth/enroll", "", req, &resp)
	return resp, err
}

func (c *Client) token(ctx context.Context, force bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !force && c.jwt != "" && time.Until(c.exp) > time.Minute {
		return c.jwt, nil
	}
	var resp api.TokenResponse
	err := c.do(ctx, "/api/v1/auth/token", "", api.TokenRequest{DeviceID: c.deviceID, DeviceToken: c.deviceToken}, &resp)
	var ae *APIError
	if errors.As(err, &ae) && (ae.Status == http.StatusUnauthorized || ae.Status == http.StatusForbidden) {
		return "", ErrRevoked
	}
	if err != nil {
		return "", err
	}
	c.jwt, c.exp = resp.AccessToken, resp.ExpiresAt
	return c.jwt, nil
}

// call performs an authenticated request, renewing the JWT once on 401.
func (c *Client) call(ctx context.Context, path string, in, out any) error {
	for attempt := 0; ; attempt++ {
		tok, err := c.token(ctx, attempt > 0)
		if err != nil {
			return err
		}
		err = c.do(ctx, path, tok, in, out)
		var ae *APIError
		if errors.As(err, &ae) {
			switch {
			case ae.Status == http.StatusUnauthorized && attempt == 0:
				continue
			case ae.Status == http.StatusForbidden && ae.Msg == api.ReasonDeviceRevoked:
				return ErrRevoked
			}
		}
		return err
	}
}

func (c *Client) do(ctx context.Context, path, bearer string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "bsd-backup-agent/"+Version)
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode >= 300 {
		var e api.ErrorResponse
		json.Unmarshal(raw, &e)
		if e.Error == "" {
			e.Error = resp.Status
		}
		return &APIError{Status: resp.StatusCode, Msg: e.Error}
	}
	if out == nil || len(raw) == 0 {
		return nil
	}
	return json.Unmarshal(raw, out)
}

func (c *Client) Sync(ctx context.Context, req api.SyncRequest) (api.SyncResponse, error) {
	var resp api.SyncResponse
	err := c.call(ctx, "/api/v1/agent/sync", req, &resp)
	return resp, err
}

func (c *Client) Report(ctx context.Context, req api.ReportRequest) error {
	return c.call(ctx, "/api/v1/agent/report", req, nil)
}

func (c *Client) Logs(ctx context.Context, entries []api.LogEntry) error {
	return c.call(ctx, "/api/v1/agent/logs", api.LogsRequest{Entries: entries}, nil)
}

func (c *Client) StorageToken(ctx context.Context, purpose string) (api.StorageTokenResponse, error) {
	var resp api.StorageTokenResponse
	err := c.call(ctx, "/api/v1/agent/storage-token", api.StorageTokenRequest{Purpose: purpose}, &resp)
	return resp, err
}

// driveTokens adapts the server's storage-token endpoint to
// storage.TokenSource, caching each token until shortly before expiry.
type driveTokens struct {
	c       *Client
	purpose string
	mu      sync.Mutex
	tok     string
	exp     time.Time
}

func (d *driveTokens) Token(ctx context.Context, force bool) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !force && d.tok != "" && time.Until(d.exp) > 2*time.Minute {
		return d.tok, nil
	}
	resp, err := d.c.StorageToken(ctx, d.purpose)
	if err != nil {
		return "", fmt.Errorf("get Google Drive token from server: %w", err)
	}
	d.tok, d.exp = resp.AccessToken, resp.ExpiresAt
	return d.tok, nil
}
