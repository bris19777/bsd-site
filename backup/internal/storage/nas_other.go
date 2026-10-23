//go:build !windows

package storage

import "errors"

// connectShare is Windows-only; elsewhere mount the share (e.g. cifs over
// Tailscale) and point the policy at the mount point.
func connectShare(share, user, password string) error {
	return errors.New("SMB credentials are only supported on Windows; mount the share instead")
}
