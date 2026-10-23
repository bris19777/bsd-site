package agent

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"bsd-backup/internal/api"
	"bsd-backup/internal/backup"
	"bsd-backup/internal/server"
)

const adminKey = "e2e-admin-key-0123456789"

type e2e struct {
	t     *testing.T
	srv   *httptest.Server
	accID string
	src   string
	nas   string
	paths Paths
	agent *Agent
}

func setup(t *testing.T, policy api.Policy) *e2e {
	store, err := server.OpenStore(filepath.Join(t.TempDir(), "store.json"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := server.New(server.Config{AdminKey: adminKey, PollSeconds: 30}, store, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	e := &e2e{t: t, srv: httptest.NewServer(s.Handler()), src: t.TempDir(), nas: t.TempDir(),
		paths: Paths{Dir: t.TempDir()}}
	t.Cleanup(e.srv.Close)

	policy.NAS = &api.NASConf{Path: e.nas}
	policy.Sources = []api.Source{{Path: e.src}}
	var acc struct{ ID string }
	e.admin("POST", "/api/v1/admin/accounts", map[string]any{
		"username": "acme", "password": "correct-horse-battery", "policy": policy,
	}, &acc)
	e.accID = acc.ID

	ctx := context.Background()
	enr, err := Enroll(ctx, e.srv.URL, api.EnrollRequest{Username: "acme", Password: "correct-horse-battery", Hostname: "PC1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.paths.SaveConfig(Config{ServerURL: e.srv.URL, DeviceID: enr.DeviceID, AccountID: enr.AccountID},
		Secrets{DeviceToken: enr.DeviceToken, Passphrase: NewRecoveryKey()}); err != nil {
		t.Fatal(err)
	}
	log, buf, _ := NewLogger(e.paths.Logs(), nil)
	if e.agent, err = New(e.paths, log, buf); err != nil {
		t.Fatal(err)
	}
	return e
}

func (e *e2e) admin(method, path string, in, out any) {
	e.t.Helper()
	b, _ := json.Marshal(in)
	req, _ := http.NewRequest(method, e.srv.URL+path, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+adminKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		e.t.Fatalf("%s %s: %s", method, path, resp.Status)
	}
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
}

func (e *e2e) device() map[string]any {
	var devs []map[string]any
	e.admin("GET", "/api/v1/admin/devices", nil, &devs)
	if len(devs) != 1 {
		e.t.Fatalf("want 1 device, got %d", len(devs))
	}
	return devs[0]
}

func writeRandom(t *testing.T, path string, n int) {
	b := make([]byte, n)
	rand.Read(b)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEndToEndBackupRestore(t *testing.T) {
	e := setup(t, api.Policy{Backend: api.BackendNAS, Options: api.BackupOption{Compress: true, Encrypt: true}})
	writeRandom(t, filepath.Join(e.src, "a.bin"), 100_000)
	os.WriteFile(filepath.Join(e.src, "notes.txt"), []byte("quarterly numbers"), 0o644)

	ctx := context.Background()
	if err := e.agent.RunOnce(ctx); err != nil {
		t.Fatal(err)
	}
	dev := e.device()
	if dev["stored_bytes"].(float64) < 100_000 || dev["last_backup_at"] == nil {
		t.Fatalf("server did not record the backup: %v", dev)
	}

	repo, err := e.agent.OpenRepo(ctx, "restore")
	if err != nil {
		t.Fatal(err)
	}
	if !repo.Config.Encrypted {
		t.Fatal("policy asked for encryption")
	}
	target := t.TempDir()
	st, err := backup.Restore(ctx, repo, backup.RestoreOptions{Snapshot: "latest", Target: target})
	if err != nil || st.Files != 2 {
		t.Fatalf("restore: %+v %v", st, err)
	}

	// Deactivate: the agent must refuse to back up.
	e.admin("PATCH", "/api/v1/admin/accounts/"+e.accID, map[string]any{"subscription": "inactive"}, nil)
	if err := e.agent.RunOnce(ctx); err == nil {
		t.Fatal("backup must be refused for an inactive subscription")
	}
}

func TestSuspensionStopsRunningBackup(t *testing.T) {
	// 64 KiB/s throttle with ~2 MiB of incompressible data: the run would
	// take ~30 s, so it is certainly still going when we suspend it.
	e := setup(t, api.Policy{Backend: api.BackendNAS, Options: api.BackupOption{BandwidthKBps: 64}})
	for i := 0; i < 4; i++ {
		writeRandom(t, filepath.Join(e.src, "big"+string(rune('a'+i))+".bin"), 512<<10)
	}
	ctx := context.Background()
	d := e.agent.syncOnce(ctx)
	if !d.BackupAllowed {
		t.Fatalf("not allowed: %s", d.Reason)
	}
	e.agent.startBackup(ctx, d)
	time.Sleep(1500 * time.Millisecond)

	e.admin("PATCH", "/api/v1/admin/accounts/"+e.accID, map[string]any{"subscription": "inactive"}, nil)
	start := time.Now()
	d = e.agent.syncOnce(ctx)
	e.agent.enforce(d)
	if took := time.Since(start); took > 5*time.Second {
		t.Fatalf("suspension took %v", took)
	}
	if e.agent.state != api.StateSuspended {
		t.Fatalf("state = %s, want suspended", e.agent.state)
	}
	snaps, _ := os.ReadDir(filepath.Join(e.nas, e.agent.cfg.DeviceID, "snapshots"))
	if len(snaps) != 0 {
		t.Fatal("an interrupted backup must not publish a snapshot")
	}
	if dev := e.device(); dev["state"] != api.StateSuspended {
		t.Fatalf("server state = %v", dev["state"])
	}
}

func TestOfflineGrace(t *testing.T) {
	e := setup(t, api.Policy{Backend: api.BackendNAS})
	ctx := context.Background()
	if d := e.agent.syncOnce(ctx); !d.BackupAllowed {
		t.Fatal("expected allowed")
	}
	e.srv.Close() // server goes away
	if d := e.agent.syncOnce(ctx); !d.BackupAllowed {
		t.Fatal("within grace the cached decision should apply")
	}
	c, _ := e.paths.loadCache()
	c.At = time.Now().Add(-100 * time.Hour)
	e.paths.saveCache(*c)
	if d := e.agent.syncOnce(ctx); d.BackupAllowed {
		t.Fatal("after the grace period backups must be suspended")
	}
}

func TestInWindow(t *testing.T) {
	at := func(h, m int) time.Time { return time.Date(2026, 1, 1, h, m, 0, 0, time.Local) }
	cases := []struct {
		t          time.Time
		start, end string
		want       bool
	}{
		{at(12, 0), "", "", true},
		{at(12, 0), "09:00", "17:00", true},
		{at(18, 0), "09:00", "17:00", false},
		{at(23, 0), "22:00", "06:00", true},
		{at(3, 0), "22:00", "06:00", true},
		{at(12, 0), "22:00", "06:00", false},
	}
	for _, c := range cases {
		if got := inWindow(c.t, c.start, c.end); got != c.want {
			t.Errorf("inWindow(%s, %s-%s) = %v", c.t.Format("15:04"), c.start, c.end, got)
		}
	}
}
