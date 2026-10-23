//go:build !windows

package agent

import "os"

// DefaultDataDir is $BSD_BACKUP_HOME or /var/lib/bsd-backup. Non-Windows
// builds exist for development and testing.
func DefaultDataDir() string {
	if d := os.Getenv("BSD_BACKUP_HOME"); d != "" {
		return d
	}
	return "/var/lib/bsd-backup"
}

// Without DPAPI, secrets rely on file permissions (0600 in a 0700 dir).
func protect(plain []byte) ([]byte, error)    { return plain, nil }
func unprotect(sealed []byte) ([]byte, error) { return sealed, nil }

func secureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.Chmod(dir, 0o700)
}

func lowerPriority() {}
