package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"bsd-backup/internal/api"
)

const adminKey = "test-admin-key-0123456789"

type harness struct {
	t   *testing.T
	srv *httptest.Server
	s   *Server
}

func newHarness(t *testing.T) *harness {
	store, err := OpenStore(filepath.Join(t.TempDir(), "store.json"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(Config{AdminKey: adminKey}, store, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, s: s, srv: httptest.NewServer(s.Handler())}
	t.Cleanup(h.srv.Close)
	return h
}

func (h *harness) req(method, path, bearer string, in, out any) int {
	h.t.Helper()
	var body io.Reader
	if in != nil {
		b, _ := json.Marshal(in)
		body = bytes.NewReader(b)
	}
	r, _ := http.NewRequest(method, h.srv.URL+path, body)
	if bearer != "" {
		r.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(r)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

var testPolicy = &api.Policy{
	Backend: api.BackendNAS,
	NAS:     &api.NASConf{Path: `\\nas.tailnet.ts.net\backups`, Username: "u", Password: "secret-pw"},
	Sources: []api.Source{{Path: `C:\Users\*\Documents`}},
}

func (h *harness) createAccount(quotaBytes int64) accountView {
	var acc accountView
	code := h.req("POST", "/api/v1/admin/accounts", adminKey, map[string]any{
		"name": "Acme", "username": "acme", "password": "correct-horse-battery",
		"quota_bytes": quotaBytes, "policy": testPolicy,
	}, &acc)
	if code != http.StatusCreated {
		h.t.Fatalf("create account: %d", code)
	}
	return acc
}

func (h *harness) enrollAndToken() (api.EnrollResponse, string) {
	var enr api.EnrollResponse
	if code := h.req("POST", "/api/v1/auth/enroll", "", api.EnrollRequest{Username: "acme", Password: "correct-horse-battery", Hostname: "PC1"}, &enr); code != 200 {
		h.t.Fatalf("enroll: %d", code)
	}
	var tok api.TokenResponse
	if code := h.req("POST", "/api/v1/auth/token", "", api.TokenRequest{DeviceID: enr.DeviceID, DeviceToken: enr.DeviceToken}, &tok); code != 200 {
		h.t.Fatalf("token: %d", code)
	}
	return enr, tok.AccessToken
}

func TestSyncDecisions(t *testing.T) {
	h := newHarness(t)
	acc := h.createAccount(1000)
	if acc.Policy.NAS.Password != "********" {
		t.Fatal("admin API must not echo the NAS password")
	}
	_, jwt := h.enrollAndToken()

	var sr api.SyncResponse
	h.req("POST", "/api/v1/agent/sync", jwt, api.SyncRequest{State: api.StateIdle, StoredBytes: 400}, &sr)
	if !sr.BackupAllowed || sr.Policy.NAS.Password != "secret-pw" || sr.UsedBytes != 400 {
		t.Fatalf("active account: %+v", sr)
	}

	// Usage reaches quota → suspended.
	h.req("POST", "/api/v1/agent/sync", jwt, api.SyncRequest{StoredBytes: 1000}, &sr)
	if sr.BackupAllowed || sr.Reason != api.ReasonQuotaExceeded {
		t.Fatalf("over quota: %+v", sr)
	}

	// Raise quota, then deactivate.
	h.req("PATCH", "/api/v1/admin/accounts/"+acc.ID, adminKey, map[string]any{"quota_gb": 1}, nil)
	h.req("POST", "/api/v1/agent/sync", jwt, api.SyncRequest{StoredBytes: 1000}, &sr)
	if !sr.BackupAllowed {
		t.Fatalf("after quota raise: %+v", sr)
	}
	h.req("PATCH", "/api/v1/admin/accounts/"+acc.ID, adminKey, map[string]any{"subscription": "inactive"}, nil)
	h.req("POST", "/api/v1/agent/sync", jwt, api.SyncRequest{}, &sr)
	if sr.BackupAllowed || sr.Reason != api.ReasonInactive {
		t.Fatalf("inactive: %+v", sr)
	}
}

func TestBackupNowAndReports(t *testing.T) {
	h := newHarness(t)
	h.createAccount(0)
	enr, jwt := h.enrollAndToken()
	h.req("POST", "/api/v1/admin/devices/"+enr.DeviceID+"/backup-now", adminKey, nil, nil)
	var sr api.SyncResponse
	h.req("POST", "/api/v1/agent/sync", jwt, api.SyncRequest{}, &sr)
	if !sr.RunNow {
		t.Fatal("run_now not delivered")
	}
	var sr2 api.SyncResponse
	h.req("POST", "/api/v1/agent/sync", jwt, api.SyncRequest{}, &sr2)
	if sr2.RunNow {
		t.Fatal("run_now must be delivered once")
	}
	h.req("POST", "/api/v1/agent/report", jwt, api.ReportRequest{RunID: "r1", Phase: api.PhaseCompleted,
		Stats: api.RunStats{FilesScanned: 10, StoredBytes: 4242}}, nil)
	h.req("POST", "/api/v1/agent/logs", jwt, api.LogsRequest{Entries: []api.LogEntry{{Level: "WARN", Message: "locked file"}}}, nil)
	var dv deviceView
	h.req("GET", "/api/v1/admin/devices/"+enr.DeviceID, adminKey, nil, &dv)
	if dv.StoredBytes != 4242 || dv.LastBackupAt == nil || len(dv.Events) < 3 {
		t.Fatalf("device view: %+v", dv)
	}
}

func TestAuthFailures(t *testing.T) {
	h := newHarness(t)
	h.createAccount(0)
	if code := h.req("GET", "/api/v1/admin/accounts", "wrong", nil, nil); code != 401 {
		t.Fatalf("bad admin key: %d", code)
	}
	if code := h.req("POST", "/api/v1/agent/sync", "not-a-jwt", api.SyncRequest{}, nil); code != 401 {
		t.Fatalf("bad jwt: %d", code)
	}
	// Brute force protection.
	for i := 0; i < maxFailures; i++ {
		h.req("POST", "/api/v1/auth/enroll", "", api.EnrollRequest{Username: "acme", Password: "nope"}, nil)
	}
	if code := h.req("POST", "/api/v1/auth/enroll", "", api.EnrollRequest{Username: "acme", Password: "correct-horse-battery"}, nil); code != 429 {
		t.Fatalf("expected lockout, got %d", code)
	}
}

func TestRevokedDevice(t *testing.T) {
	h := newHarness(t)
	h.createAccount(0)
	enr, jwt := h.enrollAndToken()
	h.req("POST", "/api/v1/admin/devices/"+enr.DeviceID+"/revoke", adminKey, nil, nil)
	var e api.ErrorResponse
	if code := h.req("POST", "/api/v1/agent/sync", jwt, api.SyncRequest{}, &e); code != 403 || e.Error != api.ReasonDeviceRevoked {
		t.Fatalf("sync after revoke: %d %+v", code, e)
	}
	if code := h.req("POST", "/api/v1/auth/token", "", api.TokenRequest{DeviceID: enr.DeviceID, DeviceToken: enr.DeviceToken}, nil); code != 403 {
		t.Fatalf("token after revoke: %d", code)
	}
}

func TestStorageTokenBrokered(t *testing.T) {
	google := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		if r.Form.Get("refresh_token") != "rt" {
			http.Error(w, "bad", 400)
			return
		}
		w.Write([]byte(`{"access_token":"ya29.test","expires_in":3600}`))
	}))
	defer google.Close()
	h := newHarness(t)
	acc := h.createAccount(0)
	h.req("PATCH", "/api/v1/admin/accounts/"+acc.ID, adminKey, map[string]any{
		"policy":       api.Policy{Backend: api.BackendGDrive, Sources: []api.Source{{Path: `C:\Data`}}},
		"gdrive_creds": GDriveCreds{OAuth: &OAuthCreds{ClientID: "c", ClientSecret: "s", RefreshToken: "rt", TokenURI: google.URL}},
	}, nil)
	_, jwt := h.enrollAndToken()
	var tok api.StorageTokenResponse
	if code := h.req("POST", "/api/v1/agent/storage-token", jwt, api.StorageTokenRequest{Purpose: "backup"}, &tok); code != 200 || tok.AccessToken != "ya29.test" {
		t.Fatalf("storage token: %d %+v", code, tok)
	}
	var got accountView
	h.req("GET", "/api/v1/admin/accounts/"+acc.ID, adminKey, nil, &got)
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "rt") && strings.Contains(string(raw), `"refresh_token"`) {
		t.Fatal("admin API leaked the refresh token")
	}
	// Inactive: backups denied, restores allowed only if configured.
	h.req("PATCH", "/api/v1/admin/accounts/"+acc.ID, adminKey, map[string]any{"subscription": "inactive"}, nil)
	if code := h.req("POST", "/api/v1/agent/storage-token", jwt, api.StorageTokenRequest{Purpose: "backup"}, nil); code != 403 {
		t.Fatalf("inactive backup token: %d", code)
	}
	if code := h.req("POST", "/api/v1/agent/storage-token", jwt, api.StorageTokenRequest{Purpose: "restore"}, nil); code != 403 {
		t.Fatalf("inactive restore token with AllowRestoreWhenInactive=false: %d", code)
	}
}

func TestPolicyValidation(t *testing.T) {
	h := newHarness(t)
	acc := h.createAccount(0)
	var e api.ErrorResponse
	code := h.req("PATCH", "/api/v1/admin/accounts/"+acc.ID, adminKey, map[string]any{
		"policy": api.Policy{Backend: "ftp", Sources: []api.Source{{Path: "C:\\"}}},
	}, &e)
	if code != 400 {
		t.Fatalf("invalid backend accepted: %d", code)
	}
	var got accountView
	h.req("GET", "/api/v1/admin/accounts/"+acc.ID, adminKey, nil, &got)
	if got.Policy.Backend != api.BackendNAS {
		t.Fatal("rejected PATCH must not change the account")
	}
}
