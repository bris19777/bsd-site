//go:build !windows

package main

import (
	"errors"

	"bsd-backup/internal/agent"
)

// Non-Windows builds are for development: there is no service manager
// integration, "service" simply runs in the foreground (use systemd).
var errWindowsOnly = errors.New("service management is only available on Windows")

func isWindowsService() bool              { return false }
func runService(paths agent.Paths) error  { return cmdRun(paths) }
func installService(dataDir string) error { return errWindowsOnly }
func uninstallService() error             { return errWindowsOnly }
func startService() error                 { return errWindowsOnly }
func stopService() error                  { return errWindowsOnly }
