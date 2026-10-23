//go:build windows

package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/eventlog"
	"golang.org/x/sys/windows/svc/mgr"

	"bsd-backup/internal/agent"
)

const (
	serviceName = "BSDBackupAgent"
	displayName = "BSD Backup Agent"
	description = "Managed backup of this PC to Google Drive or a NAS, configured by the BSD management server."
)

func isWindowsService() bool {
	ok, _ := svc.IsWindowsService()
	return ok
}

type handler struct{ paths agent.Paths }

func runService(paths agent.Paths) error {
	if !isWindowsService() {
		return cmdRun(paths) // "service" typed at a console: just run in the foreground
	}
	return svc.Run(serviceName, &handler{paths})
}

func (h *handler) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}
	elog, _ := eventlog.Open(serviceName)
	if elog != nil {
		defer elog.Close()
	}
	fail := func(err error) (bool, uint32) {
		if elog != nil {
			elog.Error(1, "BSD Backup Agent cannot start: "+err.Error())
		}
		return true, 1
	}
	log, buf, err := agent.NewLogger(h.paths.Logs(), nil)
	if err != nil {
		return fail(err)
	}
	a, err := agent.New(h.paths, log, buf)
	if err != nil {
		log.Error("startup", "err", err)
		return fail(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()
	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for {
		select {
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending, WaitHint: 30000}
				cancel()
				select {
				case <-done:
				case <-time.After(25 * time.Second):
					log.Warn("backup did not stop within 25s; exiting anyway")
				}
				return false, 0
			}
		case err := <-done:
			cancel()
			if err != nil {
				return fail(err)
			}
			return false, 0
		}
	}
}

func installService(dataDir string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return fmt.Errorf("connect to service manager (run as Administrator): %w", err)
	}
	defer m.Disconnect()
	if s, err := m.OpenService(serviceName); err == nil {
		s.Close()
		return fmt.Errorf("service %s already installed", serviceName)
	}
	s, err := m.CreateService(serviceName, exe, mgr.Config{
		DisplayName: displayName, Description: description,
		StartType: mgr.StartAutomatic, DelayedAutoStart: true,
	}, "-data-dir", dataDir, "service")
	if err != nil {
		return err
	}
	defer s.Close()
	// Restart on crash: 1 min, 1 min, then 5 min; reset the count daily.
	s.SetRecoveryActions([]mgr.RecoveryAction{
		{Type: mgr.ServiceRestart, Delay: time.Minute},
		{Type: mgr.ServiceRestart, Delay: time.Minute},
		{Type: mgr.ServiceRestart, Delay: 5 * time.Minute},
	}, 86400)
	eventlog.InstallAsEventCreate(serviceName, eventlog.Error|eventlog.Warning|eventlog.Info)
	fmt.Printf("Installed service %s. Start it with: bsd-backup-agent start\n", serviceName)
	return nil
}

func uninstallService() error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %s is not installed", serviceName)
	}
	defer s.Close()
	s.Control(svc.Stop)
	if err := s.Delete(); err != nil {
		return err
	}
	eventlog.Remove(serviceName)
	fmt.Println("Service removed. Data in", agent.DefaultDataDir(), "was kept.")
	return nil
}

func startService() error {
	return withService(func(s *mgr.Service) error { return s.Start() })
}

func stopService() error {
	return withService(func(s *mgr.Service) error {
		_, err := s.Control(svc.Stop)
		return err
	})
}

func withService(fn func(*mgr.Service) error) error {
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(serviceName)
	if err != nil {
		return fmt.Errorf("service %s is not installed", serviceName)
	}
	defer s.Close()
	return fn(s)
}
