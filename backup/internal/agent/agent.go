package agent

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"sync"
	"time"

	"bsd-backup/internal/api"
	"bsd-backup/internal/auth"
	"bsd-backup/internal/backup"
	"bsd-backup/internal/storage"
)

// errSuspended is the cancellation cause when the server (or the offline
// grace rule) stops a running backup.
type errSuspended struct{ reason string }

func (e errSuspended) Error() string { return "backup suspended: " + e.reason }

var errShutdown = errors.New("service stopping")

const (
	defaultInterval = 60 * time.Minute
	retryAfterFail  = 15 * time.Minute
	activePoll      = 60 * time.Second // heartbeat cadence while a backup runs
)

type Agent struct {
	paths   Paths
	cfg     Config
	secrets Secrets
	client  *Client
	log     *slog.Logger
	logs    *logBuffer

	mu          sync.Mutex
	state       string
	lastErr     string
	stored      int64
	lastBackup  *time.Time
	lastAttempt time.Time
	lastFailed  bool
	cancelRun   context.CancelCauseFunc
	runDone     chan struct{}
	lastPrune   time.Time
}

func New(paths Paths, log *slog.Logger, logs *logBuffer) (*Agent, error) {
	cfg, sec, err := paths.LoadConfig()
	if err != nil {
		return nil, err
	}
	a := &Agent{
		paths: paths, cfg: cfg, secrets: sec, log: log, logs: logs, state: api.StateIdle,
		client: NewClient(cfg.ServerURL, cfg.DeviceID, sec.DeviceToken),
	}
	if idx, err := backup.LoadIndex(paths.State()); err == nil {
		a.stored = idx.StoredBytes()
		a.lastBackup = idx.LastBackup()
	}
	return a, nil
}

// Run is the service body: heartbeat, enforce, schedule, until ctx ends.
func (a *Agent) Run(ctx context.Context) error {
	lowerPriority()
	a.log.Info("agent started", "version", Version, "device", a.cfg.DeviceID, "server", a.cfg.ServerURL)
	for {
		decision := a.syncOnce(ctx)
		a.enforce(decision)
		if decision.BackupAllowed && a.due(decision) {
			a.startBackup(ctx, decision)
		} else if decision.Reason == api.ReasonQuotaExceeded {
			a.maybePrune(ctx, decision)
		}

		wait := time.Duration(max(decision.PollSeconds, 30)) * time.Second
		a.mu.Lock()
		running := a.runDone
		a.mu.Unlock()
		if running != nil {
			wait = min(wait, activePoll)
		}
		select {
		case <-ctx.Done():
			a.stopRun(errShutdown)
			a.log.Info("agent stopped")
			return nil
		case <-time.After(wait):
		case <-chanOrNil(running): // report state promptly when a run ends
		}
	}
}

func chanOrNil(c chan struct{}) <-chan struct{} {
	if c == nil {
		return nil
	}
	return c
}

// syncOnce sends a heartbeat and returns the server's decision, falling
// back to the cached decision while offline within the grace period.
func (a *Agent) syncOnce(ctx context.Context) api.SyncResponse {
	a.mu.Lock()
	req := api.SyncRequest{
		Hostname: hostname(), OS: runtime.GOOS + "/" + runtime.GOARCH, AgentVersion: Version,
		State: a.state, StoredBytes: a.stored, LastBackupAt: a.lastBackup, LastError: a.lastErr,
	}
	a.mu.Unlock()

	sctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	resp, err := a.client.Sync(sctx, req)
	if err == nil {
		a.paths.saveCache(CachedSync{Resp: resp, At: time.Now()})
		a.flushLogs(sctx)
		return resp
	}
	if errors.Is(err, ErrRevoked) {
		a.log.Error("server rejected this device", "err", err)
		return api.SyncResponse{Reason: api.ReasonDeviceRevoked, PollSeconds: 3600}
	}
	a.log.Warn("cannot reach management server", "err", err)
	cached, _ := a.paths.loadCache()
	grace := time.Duration(a.cfg.OfflineGraceHours) * time.Hour
	if cached == nil || time.Since(cached.At) > grace {
		return api.SyncResponse{Reason: "server_unreachable", PollSeconds: 300}
	}
	d := cached.Resp
	d.RunNow = false
	d.PollSeconds = min(max(d.PollSeconds, 30), 300)
	return d
}

// enforce stops a running backup at once when backups are not allowed.
func (a *Agent) enforce(d api.SyncResponse) {
	if d.BackupAllowed {
		a.mu.Lock()
		if a.state == api.StateSuspended {
			a.state = api.StateIdle
			a.log.Info("backups resumed")
		}
		a.mu.Unlock()
		return
	}
	a.mu.Lock()
	was := a.state
	if a.runDone == nil {
		a.state = api.StateSuspended
	}
	a.mu.Unlock()
	if was != api.StateSuspended {
		a.log.Warn("backups suspended", "reason", d.Reason)
	}
	a.stopRun(errSuspended{d.Reason})
}

func (a *Agent) stopRun(cause error) {
	a.mu.Lock()
	cancel, done := a.cancelRun, a.runDone
	a.mu.Unlock()
	if cancel == nil {
		return
	}
	cancel(cause)
	<-done
}

func (a *Agent) due(d api.SyncResponse) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.runDone != nil || d.Policy == nil {
		return false
	}
	if d.RunNow {
		return true
	}
	interval := time.Duration(d.Policy.Sched.IntervalMinutes) * time.Minute
	if interval <= 0 {
		interval = defaultInterval
	}
	if a.lastFailed {
		interval = min(interval, retryAfterFail)
	}
	if !a.lastAttempt.IsZero() && time.Since(a.lastAttempt) < interval {
		return false
	}
	if a.lastAttempt.IsZero() && a.lastBackup != nil && time.Since(*a.lastBackup) < interval {
		return false
	}
	return inWindow(time.Now(), d.Policy.Sched.WindowStart, d.Policy.Sched.WindowEnd)
}

// inWindow reports whether t's local clock time is within [start, end),
// handling windows that wrap past midnight. Empty bounds mean always.
func inWindow(t time.Time, start, end string) bool {
	if start == "" || end == "" {
		return true
	}
	s, err1 := time.Parse("15:04", start)
	e, err2 := time.Parse("15:04", end)
	if err1 != nil || err2 != nil {
		return true
	}
	now := t.Hour()*60 + t.Minute()
	sm, em := s.Hour()*60+s.Minute(), e.Hour()*60+e.Minute()
	if sm <= em {
		return now >= sm && now < em
	}
	return now >= sm || now < em
}

func (a *Agent) startBackup(parent context.Context, d api.SyncResponse) {
	ctx, cancel := context.WithCancelCause(parent)
	done := make(chan struct{})
	a.mu.Lock()
	a.cancelRun, a.runDone = cancel, done
	a.state = api.StateBackingUp
	a.lastAttempt = time.Now()
	a.mu.Unlock()
	go func() {
		defer close(done)
		err := a.runBackup(ctx, d)
		a.mu.Lock()
		a.cancelRun, a.runDone = nil, nil
		a.lastFailed = err != nil
		switch {
		case err == nil:
			a.state, a.lastErr = api.StateIdle, ""
		case errors.As(context.Cause(ctx), new(errSuspended)) || errors.Is(err, backup.ErrQuotaExceeded):
			a.state = api.StateSuspended
		case errors.Is(context.Cause(ctx), errShutdown):
			a.state = api.StateIdle
		default:
			a.state, a.lastErr = api.StateError, err.Error()
		}
		a.mu.Unlock()
		cancel(nil)
	}()
}

// RunOnce performs a single backup in the foreground (CLI "backup-now").
func (a *Agent) RunOnce(ctx context.Context) error {
	d := a.syncOnce(ctx)
	if !d.BackupAllowed {
		return fmt.Errorf("backup not allowed: %s", d.Reason)
	}
	return a.runBackup(ctx, d)
}

func (a *Agent) runBackup(ctx context.Context, d api.SyncResponse) error {
	runID := auth.NewID("run")
	report := func(phase string, st api.RunStats, msg string) {
		rctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := a.client.Report(rctx, api.ReportRequest{RunID: runID, Phase: phase, Stats: st, Message: msg, Time: time.Now().UTC()}); err != nil {
			a.log.Warn("send report", "err", err)
		}
	}
	release, err := acquireRunLock(a.paths.State())
	if err != nil {
		a.log.Warn("backup skipped", "err", err)
		return err
	}
	defer release()
	report(api.PhaseStarted, api.RunStats{}, "")
	a.log.Info("backup started", "run", runID, "backend", d.Policy.Backend)

	repo, err := a.openRepo(ctx, d.Policy, "backup")
	if err != nil {
		a.log.Error("open storage", "err", err)
		report(api.PhaseFailed, api.RunStats{}, err.Error())
		return err
	}
	idx, err := backup.LoadIndex(a.paths.State())
	if err != nil {
		report(api.PhaseFailed, api.RunStats{}, err.Error())
		return err
	}
	quotaLeft := int64(-1)
	if d.QuotaBytes > 0 {
		quotaLeft = max(d.QuotaBytes-d.UsedBytes, 0)
	}
	p := d.Policy
	st, err := backup.Run(ctx, repo, idx, backup.Options{
		Sources: p.Sources, Exclude: p.Exclude, Compress: p.Options.Compress,
		MaxFileSize: p.Options.MaxFileSizeMB << 20, BandwidthBps: int64(p.Options.BandwidthKBps) << 10,
		Retention: p.Keep, StateDir: a.paths.Dir, DeviceID: a.cfg.DeviceID, Hostname: hostname(),
		QuotaRemaining: quotaLeft, Log: a.log,
		Progress: func(s api.RunStats) {
			a.mu.Lock()
			a.stored = s.StoredBytes
			a.mu.Unlock()
			report(api.PhaseProgress, s, "")
		},
	})
	a.mu.Lock()
	a.stored = idx.StoredBytes()
	a.lastBackup = idx.LastBackup()
	a.mu.Unlock()

	var suspended errSuspended
	switch {
	case err == nil:
		a.log.Info("backup completed", "run", runID, "files", st.FilesScanned, "changed", st.FilesChanged,
			"failed", st.FilesFailed, "uploaded", st.BytesUploaded, "stored", st.StoredBytes)
		msg := ""
		if st.FilesFailed > 0 {
			msg = fmt.Sprintf("%d file(s) could not be read (see logs)", st.FilesFailed)
		}
		report(api.PhaseCompleted, st, msg)
	case errors.Is(err, backup.ErrQuotaExceeded):
		a.log.Warn("storage quota reached; partial snapshot saved", "run", runID)
		report(api.PhaseSuspended, st, api.ReasonQuotaExceeded)
	case errors.As(context.Cause(ctx), &suspended):
		a.log.Warn("backup interrupted", "run", runID, "reason", suspended.reason)
		report(api.PhaseSuspended, st, suspended.reason)
	case errors.Is(context.Cause(ctx), errShutdown):
		a.log.Info("backup interrupted by service stop", "run", runID)
		report(api.PhaseSuspended, st, "service stopped")
	default:
		a.log.Error("backup failed", "run", runID, "err", err)
		report(api.PhaseFailed, st, err.Error())
	}
	return err
}

// maybePrune applies retention at most daily while over quota, so expired
// snapshots free space even though new backups are blocked.
func (a *Agent) maybePrune(ctx context.Context, d api.SyncResponse) {
	if d.Policy == nil || (d.Policy.Keep.KeepLast == 0 && d.Policy.Keep.KeepDays == 0) || time.Since(a.lastPrune) < 24*time.Hour {
		return
	}
	a.lastPrune = time.Now()
	release, err := acquireRunLock(a.paths.State())
	if err != nil {
		return
	}
	defer release()
	repo, err := a.openRepo(ctx, d.Policy, "backup")
	if err != nil {
		return
	}
	idx, err := backup.LoadIndex(a.paths.State())
	if err != nil {
		return
	}
	n, err := backup.ApplyRetention(ctx, repo, idx, d.Policy.Keep, time.Now(), a.log)
	a.log.Info("retention while over quota", "blobs_deleted", n, "err", err)
	a.mu.Lock()
	a.stored = idx.StoredBytes()
	a.mu.Unlock()
}

// OpenRepo opens the policy's repository; purpose is "backup" or "restore".
func (a *Agent) OpenRepo(ctx context.Context, purpose string) (*backup.Repo, error) {
	d := a.syncOnce(ctx)
	if d.Policy == nil {
		return nil, fmt.Errorf("no policy available (%s)", d.Reason)
	}
	return a.openRepo(ctx, d.Policy, purpose)
}

func (a *Agent) openRepo(ctx context.Context, p *api.Policy, purpose string) (*backup.Repo, error) {
	var b storage.Backend
	var err error
	switch p.Backend {
	case api.BackendGDrive:
		root := ""
		if p.GDrive != nil {
			root = p.GDrive.RootFolderID
		}
		b, err = storage.OpenGDrive(ctx, storage.GDriveOptions{
			Tokens: &driveTokens{c: a.client, purpose: purpose}, RootFolderID: root, RepoName: a.cfg.DeviceID,
		})
	case api.BackendNAS:
		if p.NAS == nil {
			return nil, errors.New("policy has no NAS settings")
		}
		b, err = storage.OpenNAS(ctx, storage.NASOptions{
			Path: p.NAS.Path, Username: p.NAS.Username, Password: p.NAS.Password, RepoName: a.cfg.DeviceID,
		})
	default:
		return nil, fmt.Errorf("unknown backend %q", p.Backend)
	}
	if err != nil {
		return nil, err
	}
	repo, err := backup.OpenRepo(ctx, b, backup.RepoOptions{
		Passphrase: a.secrets.Passphrase, Encrypt: p.Options.Encrypt,
		DeviceID: a.cfg.DeviceID, Hostname: hostname(),
	})
	if err != nil {
		return nil, err
	}
	if repo.Warning != "" {
		a.log.Warn(repo.Warning)
	}
	return repo, nil
}

func (a *Agent) flushLogs(ctx context.Context) {
	if a.logs == nil {
		return
	}
	entries := a.logs.drain()
	if len(entries) == 0 {
		return
	}
	if err := a.client.Logs(ctx, entries); err != nil {
		a.logs.requeue(entries)
	}
}

func hostname() string {
	h, _ := os.Hostname()
	return h
}

// Status is a snapshot of the agent's view for the CLI.
type Status struct {
	Config     Config
	Cached     *CachedSync
	Stored     int64
	LastBackup *time.Time
	Snapshots  int
}

func ReadStatus(paths Paths) (Status, error) {
	cfg, _, err := paths.LoadConfig()
	if err != nil {
		return Status{}, err
	}
	st := Status{Config: cfg}
	st.Cached, _ = paths.loadCache()
	if idx, err := backup.LoadIndex(paths.State()); err == nil {
		st.Stored, st.LastBackup, st.Snapshots = idx.StoredBytes(), idx.LastBackup(), len(idx.Snapshots)
	}
	return st, nil
}
