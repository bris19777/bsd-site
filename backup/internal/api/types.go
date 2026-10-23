// Package api defines the JSON wire types shared by the management server
// and the Windows agent. Every request/response body on /api/v1 is one of
// these types.
package api

import "time"

// Subscription states.
const (
	SubscriptionActive   = "active"
	SubscriptionInactive = "inactive"
)

// Storage backends.
const (
	BackendGDrive = "gdrive"
	BackendNAS    = "nas"
)

// Reasons the server gives for BackupAllowed=false.
const (
	ReasonInactive      = "subscription_inactive"
	ReasonQuotaExceeded = "quota_exceeded"
	ReasonNoPolicy      = "no_backend_configured"
	ReasonDeviceRevoked = "device_revoked"
)

// Agent states reported in heartbeats.
const (
	StateIdle      = "idle"
	StateBackingUp = "backing_up"
	StateSuspended = "suspended"
	StateError     = "error"
)

// ---- authentication ----

// EnrollRequest registers a new device under an account using the account's
// username/password. It is the only call that ever sends the password.
type EnrollRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Hostname string `json:"hostname"`
	OS       string `json:"os"`
}

// EnrollResponse carries the long-lived device credential. The agent stores
// DeviceToken with DPAPI; the server only keeps its SHA-256.
type EnrollResponse struct {
	DeviceID    string `json:"device_id"`
	DeviceToken string `json:"device_token"`
	AccountID   string `json:"account_id"`
}

// TokenRequest exchanges the device credential for a short-lived JWT.
type TokenRequest struct {
	DeviceID    string `json:"device_id"`
	DeviceToken string `json:"device_token"`
}

type TokenResponse struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// ---- policy ----

// Policy is the backup configuration the server pushes to agents.
type Policy struct {
	Backend string       `json:"backend"` // BackendGDrive | BackendNAS
	GDrive  *GDriveConf  `json:"gdrive,omitempty"`
	NAS     *NASConf     `json:"nas,omitempty"`
	Sources []Source     `json:"sources"`
	Exclude []string     `json:"exclude,omitempty"` // applied to every source
	Sched   Schedule     `json:"schedule"`
	Keep    Retention    `json:"retention"`
	Options BackupOption `json:"options"`
}

// GDriveConf locates the repository on Google Drive. Credentials never reach
// the agent: it asks the server for short-lived access tokens instead.
type GDriveConf struct {
	// RootFolderID is a folder (or Shared Drive) ID. Empty means the agent
	// creates/uses a "BSD Backup" folder in the account's My Drive.
	RootFolderID string `json:"root_folder_id,omitempty"`
}

// NASConf points at an SMB share reachable over Tailscale, e.g.
// \\nas.tailnet-abc.ts.net\backups. On non-Windows agents Path is a
// mounted directory.
type NASConf struct {
	Path     string `json:"path"`
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

// Source is one directory tree to back up.
type Source struct {
	Path    string   `json:"path"`
	Include []string `json:"include,omitempty"` // empty = everything
	Exclude []string `json:"exclude,omitempty"`
}

// Schedule controls when automatic backups run.
type Schedule struct {
	IntervalMinutes int `json:"interval_minutes"` // 0 = default (60)
	// Optional daily window in local time, "HH:MM". A window that wraps
	// midnight ("22:00"–"06:00") is allowed. Empty = any time.
	WindowStart string `json:"window_start,omitempty"`
	WindowEnd   string `json:"window_end,omitempty"`
}

// Retention: a snapshot is kept if it is among the newest KeepLast, or
// younger than KeepDays. Both zero = keep everything.
type Retention struct {
	KeepLast int `json:"keep_last"`
	KeepDays int `json:"keep_days"`
}

type BackupOption struct {
	Compress      bool  `json:"compress"`
	Encrypt       bool  `json:"encrypt"`
	BandwidthKBps int   `json:"bandwidth_kbps,omitempty"` // 0 = unlimited
	MaxFileSizeMB int64 `json:"max_file_size_mb,omitempty"`
}

// ---- sync / heartbeat ----

// SyncRequest is the agent's periodic heartbeat. The response tells the
// agent whether it may back up and with which policy.
type SyncRequest struct {
	Hostname     string     `json:"hostname"`
	OS           string     `json:"os"`
	AgentVersion string     `json:"agent_version"`
	State        string     `json:"state"`
	StoredBytes  int64      `json:"stored_bytes"`
	LastBackupAt *time.Time `json:"last_backup_at,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
}

type SyncResponse struct {
	Subscription  string    `json:"subscription"`
	QuotaBytes    int64     `json:"quota_bytes"` // 0 = unlimited
	UsedBytes     int64     `json:"used_bytes"`  // whole account, all devices
	BackupAllowed bool      `json:"backup_allowed"`
	Reason        string    `json:"reason,omitempty"`
	Policy        *Policy   `json:"policy,omitempty"`
	PollSeconds   int       `json:"poll_seconds"`
	RunNow        bool      `json:"run_now,omitempty"`
	ServerTime    time.Time `json:"server_time"`
}

// ---- telemetry ----

// Report phases.
const (
	PhaseStarted   = "started"
	PhaseProgress  = "progress"
	PhaseCompleted = "completed"
	PhaseFailed    = "failed"
	PhaseSuspended = "suspended"
)

type RunStats struct {
	FilesScanned   int64  `json:"files_scanned"`
	FilesChanged   int64  `json:"files_changed"`
	FilesFailed    int64  `json:"files_failed"`
	BytesScanned   int64  `json:"bytes_scanned"`
	BytesUploaded  int64  `json:"bytes_uploaded"` // after compression/encryption
	BlobsDeleted   int64  `json:"blobs_deleted"`
	StoredBytes    int64  `json:"stored_bytes"` // whole repository after the run
	SnapshotID     string `json:"snapshot_id,omitempty"`
	DurationMillis int64  `json:"duration_ms"`
}

type ReportRequest struct {
	RunID   string    `json:"run_id"`
	Phase   string    `json:"phase"`
	Stats   RunStats  `json:"stats"`
	Message string    `json:"message,omitempty"`
	Time    time.Time `json:"time"`
}

type LogEntry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
}

type LogsRequest struct {
	Entries []LogEntry `json:"entries"`
}

// StorageTokenRequest asks for a Google Drive access token.
// Purpose is "backup" or "restore"; restores may be allowed while the
// subscription is inactive so customers can always retrieve their data.
type StorageTokenRequest struct {
	Purpose string `json:"purpose"`
}

type StorageTokenResponse struct {
	AccessToken string    `json:"access_token"`
	ExpiresAt   time.Time `json:"expires_at"`
}

// ErrorResponse is returned with every non-2xx status.
type ErrorResponse struct {
	Error string `json:"error"`
}
