# BSD Managed Backup

A managed, client-side backup system for Windows PCs:

- **`bsd-backup-server`**: the cloud Central Management Server. It handles authentication, subscriptions and quotas, the backup policy, telemetry, and Google Drive token brokering.
- **`bsd-backup-agent.exe`**: a native Windows Service on each PC. It backs files up **directly** to Google Drive or to a NAS reached over Tailscale. Backup data never passes through the management server.

Everything is written in Go with no runtime dependencies. The agent is one static `.exe` and the server is one static binary.

```
                         ┌──────────────────────────────────────┐
                         │   Central Management Server (cloud)  │
                         │  auth · subscriptions · quotas       │
                         │  policy engine · telemetry           │
                         │  Google token broker                 │
                         └──────▲──────────────▲────────────────┘
       enroll / JWT / policy /  │              │ admin API (curl, scripts, future UI)
       heartbeat / reports /    │ HTTPS        │
       logs / Drive tokens      │              │
┌───────────────────────────────┴───┐
│ Windows PC: BSDBackupAgent service│        payload goes direct, never via server
│  DPAPI-protected credentials      │──────────────────────────┐
│  scan → hash → dedup → gzip →     │                          │
│  AES-256-GCM → throttle → upload  │                          ▼
└───────────────────────────────────┘    ┌──────────────────────────┬────────────────────────────┐
                                         │ Google Drive (REST v3,   │ NAS SMB share via Tailscale│
                                         │ resumable chunked upload)│ \\nas.tailnet.ts.net\share │
                                         └──────────────────────────┴────────────────────────────┘
```

## Contents

| Path | What it is |
|---|---|
| `cmd/bsd-backup-server` | Server entry point (flags, TLS, graceful shutdown) |
| `cmd/bsd-backup-agent` | Agent CLI and Windows Service wrapper (`service_windows.go`) |
| `internal/api` | JSON wire types shared by both sides |
| `internal/auth` | PBKDF2 password hashes, device tokens, HS256 JWTs |
| `internal/server` | HTTP handlers, JSON-file store, subscription/quota decisions, Google token minting |
| `internal/agent` | Enrollment, DPAPI secret storage, sync loop, enforcement, scheduling, log shipping |
| `internal/backup` | Engine: scan, change detection, dedup, snapshots, retention/GC, restore |
| `internal/crypt` | Object format: gzip + streaming AES-256-GCM |
| `internal/storage` | `Backend` interface with Google Drive and NAS implementations |
| `deploy/` | Dockerfile, systemd unit, Caddyfile, `install-agent.ps1` |
| `examples/` | Account and policy JSON for the admin API |

## How it works

### Authentication

1. **Enroll (once per PC).** The installer runs `enroll` with the account's username and password. The server checks the PBKDF2-SHA256 hash and creates a device record. It returns a random 256-bit **device token** and stores only that token's SHA-256. Five failed attempts per IP+username lock enrollment for 15 minutes.
2. **Session.** The agent exchanges the device token for a **15-minute JWT** (HS256) and renews it automatically. The password is never stored on the PC.
3. **Local secrets.** The device token, the encryption passphrase and the cached policy are sealed with **machine-scope DPAPI**. They live in `%ProgramData%\BSDBackup`, whose ACL allows only SYSTEM and Administrators with inheritance off. That is required because the installer (an admin) and the service (LocalSystem) are different accounts.
4. **Revocation.** `POST /admin/devices/{id}/revoke` invalidates a device at once. Its next heartbeat or token renewal fails and it suspends.

### Subscriptions, quotas and enforcement

The agent sends a heartbeat (`/agent/sync`) every `poll-seconds`, 5 minutes by default. While a backup is running it sends one every 60 seconds. The server replies with `backup_allowed` plus a reason:

| Server state | Agent behaviour |
|---|---|
| `subscription_inactive` | Cancels any running backup **immediately**: the upload stops mid-stream and no snapshot is published. Google Drive tokens are refused. |
| `quota_exceeded` (account usage ≥ quota) | No new backups. Retention still runs once a day so expired snapshots free space. |
| active, under quota | Backs up. During a run the agent stops *before* an upload would cross the remaining quota and saves a partial snapshot. |
| server unreachable | Keeps working on the cached policy for `offline_grace_hours` (72 h by default), then suspends until it can confirm the subscription. |

Usage is the sum of `stored_bytes` across the account's devices. Each agent reports its repository size after every run and in progress reports.

### Backup engine

- **Change detection.** Files are walked per source; sources may contain wildcards and environment variables, e.g. `C:\Users\*\Documents`. If a file's size and mtime match the local index, its hash is reused. Otherwise it is re-hashed with SHA-256. The engine always hashes the bytes on disk before uploading.
- **Dedup and incremental.** Content is stored once per unique hash as `blobs/<id>`. Unchanged and duplicate files cost nothing. Each run writes one manifest, `snapshots/<UTC time>-<rand>`, listing every file, so each snapshot can be restored on its own while storage holds only the differences.
- **Pipeline.** file → gzip (skipped for already-compressed types such as jpg, mp4, zip and docx) → AES-256-GCM in 64 KiB authenticated segments → token-bucket throttle → backend. The file is re-hashed during upload; if it changed mid-upload, the blob is discarded and retried next run.
- **Encryption** (`options.encrypt`). A master key is derived from the passphrase with PBKDF2 (600k iterations). From it come separate content and blob-name keys, so storage learns neither the content nor its hashes. The repository `config` stores only a salt and a key check. If no passphrase is given at enrollment, a **recovery key** is generated and printed once. Keep it off the PC: without it an encrypted backup cannot be restored.
- **Retention.** A snapshot is kept if it is among the newest `keep_last` or younger than `keep_days`. Then every blob no kept snapshot references is deleted, including orphans from interrupted runs. If any kept manifest cannot be read, nothing is deleted.
- **Default excludes.** pagefile, hiberfil, swapfile, `$Recycle.Bin`, System Volume Information, Temp and INetCache, `*.tmp`, Office `~$` lock files, Thumbs.db. The agent's own data directory is never backed up.
- **Being a good citizen.** The service runs at below-normal CPU priority and honours `bandwidth_kbps`. An OS-level lock stops a console `backup-now` from racing the service.

### Storage backends

**Google Drive.** Layout: `<root>/<device-id>/{config, blobs/, snapshots/}`.
- Uploads use the **resumable upload protocol** in 8 MiB chunks. After a network failure the agent asks Drive how many bytes it committed and resumes from there. It retries 429, 5xx and rate-limit 403s with backoff.
- **Google credentials never reach the PC.** The server holds an OAuth refresh token or a service-account key and gives agents 1-hour access tokens through `/agent/storage-token`. Deactivating a customer therefore also cuts their Drive access within the hour.
- Drive file IDs are recorded in the local index, as the spec requires.
- All calls set `supportsAllDrives`, so Shared Drives work.

**NAS over Tailscale.** The policy gives a UNC path such as `\\office-nas.tailnet-abc.ts.net\backups`, optionally with SMB credentials.
- Before each run the agent probes TCP 445. If the tailnet is down it fails fast with "is Tailscale connected on this PC?" instead of hanging.
- It authenticates the share with `WNetAddConnection2`. No drive letter is involved: services cannot see users' mapped drives.
- Blobs are sharded as `blobs/ab/abcdef…`, and every write goes to a temp file followed by a rename.

## Running the server

```bash
cd backup
go build -o bsd-backup-server ./cmd/bsd-backup-server
export BSD_ADMIN_KEY="$(openssl rand -hex 24)"     # keep this secret
./bsd-backup-server -addr 127.0.0.1:8080 -data /var/lib/bsd-backup/store.json
```

Put it behind TLS: `deploy/Caddyfile` gives automatic Let's Encrypt. Alternatively pass `-tls-cert`/`-tls-key`. Agents refuse non-HTTPS servers except on localhost. `deploy/bsd-backup-server.service` is a hardened systemd unit and `deploy/Dockerfile` builds a distroless image.

| Flag | Default | Meaning |
|---|---|---|
| `-addr` | `:8080` | listen address |
| `-data` | `data/store.json` | JSON data file (mode 0600; back it up) |
| `-poll-seconds` | `300` | heartbeat interval sent to agents |
| `-token-ttl` | `15m` | device JWT lifetime |
| `-allow-restore-when-inactive` | `true` | inactive customers can still *restore* from Google Drive |

### Admin API

Every call sends `Authorization: Bearer $BSD_ADMIN_KEY`. Secrets (passwords, refresh tokens, keys) are accepted but never echoed back.

```bash
H="Authorization: Bearer $BSD_ADMIN_KEY"; S=https://backup.example.com/api/v1/admin

curl -H "$H" $S/accounts -d @examples/account-nas.json                 # create (see examples/)
curl -H "$H" $S/accounts                                                # list with usage
curl -H "$H" -X PATCH $S/accounts/acc_… -d '{"subscription":"inactive"}'  # suspend customer
curl -H "$H" -X PATCH $S/accounts/acc_… -d '{"quota_gb":1000}'            # change quota
curl -H "$H" -X PATCH $S/accounts/acc_… -d @new-policy.json             # {"policy":{…}}
curl -H "$H" "$S/devices?account=acc_…"                                 # status, online, last run
curl -H "$H" $S/devices/dev_…                                           # + last 200 events/logs
curl -H "$H" -X POST $S/devices/dev_…/backup-now                        # run at next heartbeat
curl -H "$H" -X POST $S/devices/dev_…/revoke
```

Agent-facing endpoints, all `POST`: `/api/v1/auth/enroll` and `/api/v1/auth/token`, plus the following, which require the device JWT: `/api/v1/agent/sync`, `/report`, `/logs` and `/storage-token`. The bodies are defined in `internal/api/types.go`.

### Google Drive setup

Use a dedicated Google account or Shared Drive per customer. A Drive access token is scoped to the whole drive it authorizes.

- **OAuth (personal Google accounts).** Create an OAuth client (Desktop type) in Google Cloud and enable the Drive API. Run the consent flow once with scope `https://www.googleapis.com/auth/drive` to get a refresh token. Store `client_id`, `client_secret` and `refresh_token` under `gdrive_creds.oauth`.
- **Service account (Workspace).** Service accounts have no My Drive quota, so add the service account as a member of a **Shared Drive**. Store `client_email` and `private_key` under `gdrive_creds.service_account`, and set `policy.gdrive.root_folder_id` to the Shared Drive ID. To use domain-wide delegation instead, set `subject` to the user to impersonate.
- Leaving `root_folder_id` empty makes the agent use a "BSD Backup" folder in My Drive.

### NAS over Tailscale setup

1. Install Tailscale on the NAS and on each PC, in the same tailnet. On PCs, enable "Run unattended" so the tunnel is up without anyone logged in, because the service runs at boot.
2. Create an SMB share and a share user with access to it.
3. Use the NAS's MagicDNS name in the policy: `"nas": {"path": "\\\\nas.tailnet-xyz.ts.net\\backups", "username": "…", "password": "…"}`.
4. Use Tailscale ACLs to limit customer PCs to port 445 on the NAS. Each device writes to its own `<device-id>` folder.

## Installing the agent

Build it on any OS:

```bash
cd backup
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bsd-backup-agent.exe ./cmd/bsd-backup-agent
```

On the PC, open an elevated PowerShell in the folder that contains the exe and `deploy/install-agent.ps1`:

```powershell
.\install-agent.ps1 -Server https://backup.example.com -Username acme
```

The script copies the exe to `C:\Program Files\BSDBackup`, prompts for the password and enrolls. It then installs `BSDBackupAgent` with delayed automatic start and restart-on-failure, and starts it. To do the same by hand: `enroll`, `install`, `start`.

Day-to-day commands (run as Administrator):

```
bsd-backup-agent status                       # subscription, quota, backend, last backup
bsd-backup-agent backup-now                   # foreground run with console log
bsd-backup-agent snapshots
bsd-backup-agent restore -target D:\Restore -path "C:\Users\dana\Documents"   # -snapshot <id> for older
bsd-backup-agent show-recovery-key
```

Logs go to `%ProgramData%\BSDBackup\logs\agent.log`, rotated at 10 MiB. Warnings and errors are also shipped to the server, and service start failures go to the Windows Event Log. Restores recreate the original layout under the target, for example `C:\Users\dana\x.docx` becomes `D:\Restore\C\Users\dana\x.docx`. Every restored file is checked against its recorded SHA-256.

## Development

```bash
cd backup
go test -race ./...                # unit + end-to-end tests (server, agent, engine, fake Drive)
GOOS=windows go vet ./...          # checks the Windows-only code (service, DPAPI, SMB, locks)
```

The tests cover:
- crypto tamper and truncation detection
- Drive resumable uploads against a fake Drive that drops chunks
- incremental and dedup behaviour, retention GC, quota stops, wrong passphrase, rebuilding the index after a reinstall
- the server's quota and subscription decisions, revocation, the enrollment lockout, secret redaction and token brokering
- a full agent↔server run, including suspending a backup mid-upload and the offline grace period

The agent also builds on Linux and macOS for development. There, secrets rely on file permissions instead of DPAPI, and the NAS path must be a mounted directory.

## Known limitations and next steps

- **Open/locked files.** Files held exclusively, such as Outlook `.pst` files while Outlook runs or databases, are skipped and reported. A **VSS snapshot** before each run is the main next step for Windows.
- **Change detection** uses scheduled scans with size and mtime plus hashing. A `ReadDirectoryChangesW` watcher could trigger near-real-time runs.
- **Server store** is a single JSON file, which suits hundreds of devices. Move to Postgres behind the same `Store` methods for more. Google and NAS credentials in it are protected only by file permissions; add KMS encryption at rest for production.
- **No admin web UI yet.** The JSON admin API is complete enough to put one on top.
- **Shipping.** Sign `bsd-backup-agent.exe` with a code-signing certificate and package it as an MSI for GPO/Intune deployment.
