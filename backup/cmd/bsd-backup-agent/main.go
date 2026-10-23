// Command bsd-backup-agent is the Windows backup client. Typical install,
// from an elevated prompt:
//
//	bsd-backup-agent enroll -server https://backup.example.com -username acme
//	bsd-backup-agent install
//	bsd-backup-agent start
//
// When started by the Service Control Manager it runs as the
// "BSDBackupAgent" service.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"golang.org/x/term"

	"bsd-backup/internal/agent"
	"bsd-backup/internal/api"
	"bsd-backup/internal/backup"
)

const usage = `bsd-backup-agent %s

Usage: bsd-backup-agent [-data-dir DIR] <command> [flags]

Setup (run as Administrator):
  enroll             register this PC with the management server
  install            install the Windows service (auto-start)
  uninstall          remove the Windows service
  start | stop       start or stop the service

Operation:
  run                run the agent in the foreground (console)
  backup-now         run one backup immediately in the foreground
  status             show enrollment, policy and last backup
  snapshots          list snapshots in the storage repository
  restore            restore files from a snapshot
  show-recovery-key  print the encryption passphrase (keep it safe!)
  version
`

func main() {
	dataDir := flag.String("data-dir", agent.DefaultDataDir(), "agent data directory")
	flag.Usage = func() { fmt.Fprintf(os.Stderr, usage, agent.Version) }
	flag.Parse()
	paths := agent.Paths{Dir: *dataDir}

	if isWindowsService() {
		if err := runService(paths); err != nil {
			os.Exit(1)
		}
		return
	}

	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(2)
	}
	cmd, rest := args[0], args[1:]
	var err error
	switch cmd {
	case "enroll":
		err = cmdEnroll(paths, rest)
	case "install":
		err = installService(*dataDir)
	case "uninstall":
		err = uninstallService()
	case "start":
		err = startService()
	case "stop":
		err = stopService()
	case "service": // what the SCM launches; also usable in the foreground
		err = runService(paths)
	case "run":
		err = cmdRun(paths)
	case "backup-now":
		err = cmdBackupNow(paths)
	case "status":
		err = cmdStatus(paths)
	case "snapshots":
		err = cmdSnapshots(paths)
	case "restore":
		err = cmdRestore(paths, rest)
	case "show-recovery-key":
		err = cmdShowKey(paths)
	case "version":
		fmt.Println(agent.Version)
	default:
		flag.Usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func signalContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func consoleAgent(paths agent.Paths) (*agent.Agent, error) {
	log, buf, err := agent.NewLogger(paths.Logs(), os.Stderr)
	if err != nil {
		return nil, err
	}
	return agent.New(paths, log, buf)
}

func cmdEnroll(paths agent.Paths, args []string) error {
	fs := flag.NewFlagSet("enroll", flag.ExitOnError)
	server := fs.String("server", "", "management server URL (https://...)")
	username := fs.String("username", "", "account username")
	password := fs.String("password", "", "account password (default: $BSD_BACKUP_PASSWORD or prompt)")
	passphrase := fs.String("passphrase", "", "encryption passphrase (default: $BSD_BACKUP_PASSPHRASE or a generated recovery key)")
	fs.Parse(args)
	if *server == "" || *username == "" {
		return errors.New("enroll needs -server and -username")
	}
	if err := agent.CheckServerURL(*server); err != nil {
		return err
	}
	pw := firstNonEmpty(*password, os.Getenv("BSD_BACKUP_PASSWORD"))
	if pw == "" {
		var err error
		if pw, err = prompt("Password for " + *username + ": "); err != nil {
			return err
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	host, _ := os.Hostname()
	resp, err := agent.Enroll(ctx, *server, api.EnrollRequest{Username: *username, Password: pw, Hostname: host, OS: "windows"})
	if err != nil {
		return err
	}
	pass := firstNonEmpty(*passphrase, os.Getenv("BSD_BACKUP_PASSPHRASE"))
	generated := pass == ""
	if generated {
		pass = agent.NewRecoveryKey()
	}
	cfg := agent.Config{ServerURL: *server, DeviceID: resp.DeviceID, AccountID: resp.AccountID, OfflineGraceHours: 72}
	if err := paths.SaveConfig(cfg, agent.Secrets{DeviceToken: resp.DeviceToken, Passphrase: pass}); err != nil {
		return err
	}
	fmt.Printf("Enrolled as device %s (account %s).\n", resp.DeviceID, resp.AccountID)
	if generated {
		fmt.Printf("\nENCRYPTION RECOVERY KEY: %s\n\n", pass)
		fmt.Println("Store this key somewhere safe (not on this PC). If encryption is enabled and")
		fmt.Println("this PC is lost, the backup cannot be restored without it.")
	}
	return nil
}

func cmdRun(paths agent.Paths) error {
	a, err := consoleAgent(paths)
	if err != nil {
		return err
	}
	ctx, cancel := signalContext()
	defer cancel()
	return a.Run(ctx)
}

func cmdBackupNow(paths agent.Paths) error {
	a, err := consoleAgent(paths)
	if err != nil {
		return err
	}
	ctx, cancel := signalContext()
	defer cancel()
	return a.RunOnce(ctx)
}

func cmdStatus(paths agent.Paths) error {
	st, err := agent.ReadStatus(paths)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintf(w, "Server:\t%s\n", st.Config.ServerURL)
	fmt.Fprintf(w, "Device:\t%s\n", st.Config.DeviceID)
	if c := st.Cached; c != nil {
		fmt.Fprintf(w, "Subscription:\t%s (checked %s)\n", c.Resp.Subscription, c.At.Local().Format(time.DateTime))
		allowed := "yes"
		if !c.Resp.BackupAllowed {
			allowed = "no — " + c.Resp.Reason
		}
		fmt.Fprintf(w, "Backups allowed:\t%s\n", allowed)
		quota := "unlimited"
		if c.Resp.QuotaBytes > 0 {
			quota = gb(c.Resp.QuotaBytes)
		}
		fmt.Fprintf(w, "Account usage:\t%s of %s\n", gb(c.Resp.UsedBytes), quota)
		if p := c.Resp.Policy; p != nil {
			fmt.Fprintf(w, "Backend:\t%s\n", p.Backend)
			for _, s := range p.Sources {
				fmt.Fprintf(w, "Source:\t%s\n", s.Path)
			}
		}
	}
	fmt.Fprintf(w, "This PC stores:\t%s in %d snapshot(s)\n", gb(st.Stored), st.Snapshots)
	if st.LastBackup != nil {
		fmt.Fprintf(w, "Last backup:\t%s\n", st.LastBackup.Local().Format(time.DateTime))
	} else {
		fmt.Fprintf(w, "Last backup:\tnever\n")
	}
	return w.Flush()
}

func cmdSnapshots(paths agent.Paths) error {
	a, err := consoleAgent(paths)
	if err != nil {
		return err
	}
	ctx, cancel := signalContext()
	defer cancel()
	repo, err := a.OpenRepo(ctx, "restore")
	if err != nil {
		return err
	}
	snaps, err := repo.ListSnapshots(ctx)
	if err != nil {
		return err
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tTIME")
	for _, s := range snaps {
		fmt.Fprintf(w, "%s\t%s\n", s.ID, s.Time.Local().Format(time.DateTime))
	}
	return w.Flush()
}

func cmdRestore(paths agent.Paths, args []string) error {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	snap := fs.String("snapshot", "latest", "snapshot ID or 'latest'")
	target := fs.String("target", "", "directory to restore into (required)")
	prefix := fs.String("path", "", `only restore files under this original path, e.g. C:\Users\dana\Documents`)
	fs.Parse(args)
	if *target == "" {
		return errors.New("restore needs -target")
	}
	a, err := consoleAgent(paths)
	if err != nil {
		return err
	}
	ctx, cancel := signalContext()
	defer cancel()
	repo, err := a.OpenRepo(ctx, "restore")
	if err != nil {
		return err
	}
	log, _, _ := agent.NewLogger(paths.Logs(), os.Stderr)
	st, err := backup.Restore(ctx, repo, backup.RestoreOptions{Snapshot: *snap, Target: *target, Prefix: *prefix, Log: log})
	fmt.Printf("Restored %d file(s), %s, from snapshot %s into %s\n", st.Files, gb(st.Bytes), st.Snapshot, *target)
	return err
}

func cmdShowKey(paths agent.Paths) error {
	_, sec, err := paths.LoadConfig()
	if err != nil {
		return err
	}
	fmt.Println(sec.Passphrase)
	return nil
}

func prompt(label string) (string, error) {
	fmt.Fprint(os.Stderr, label)
	if term.IsTerminal(int(os.Stdin.Fd())) {
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimRight(line, "\r\n"), err
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

func gb(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.2f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
}
