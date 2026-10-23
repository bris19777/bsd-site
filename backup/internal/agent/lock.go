package agent

import (
	"errors"
	"os"
	"path/filepath"
)

var ErrBusy = errors.New("another backup is running on this PC (service or CLI)")

// acquireRunLock takes an OS-level exclusive lock on <state>/run.lock. The
// OS drops it if the process dies, so a crash never leaves a stale lock.
func acquireRunLock(stateDir string) (release func(), err error) {
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(filepath.Join(stateDir, "run.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := lockFile(f); err != nil {
		f.Close()
		return nil, ErrBusy
	}
	return func() { f.Close() }, nil
}
