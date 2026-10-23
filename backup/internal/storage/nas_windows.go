//go:build windows

package storage

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	mpr                    = windows.NewLazySystemDLL("mpr.dll")
	procWNetAddConnection2 = mpr.NewProc("WNetAddConnection2W")
)

const (
	resourceTypeDisk       = 0x1
	errorAlreadyAssigned   = 85
	errorSessionCredential = 1219 // already connected with other credentials
)

type netResource struct {
	Scope       uint32
	Type        uint32
	DisplayType uint32
	Usage       uint32
	LocalName   *uint16
	RemoteName  *uint16
	Comment     *uint16
	Provider    *uint16
}

// connectShare authenticates the service's logon session to the SMB share
// without mapping a drive letter (services cannot see users' mapped drives).
func connectShare(share, user, password string) error {
	remote, err := syscall.UTF16PtrFromString(share)
	if err != nil {
		return err
	}
	u, err := syscall.UTF16PtrFromString(user)
	if err != nil {
		return err
	}
	p, err := syscall.UTF16PtrFromString(password)
	if err != nil {
		return err
	}
	nr := netResource{Type: resourceTypeDisk, RemoteName: remote}
	r, _, _ := procWNetAddConnection2.Call(uintptr(unsafe.Pointer(&nr)),
		uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(u)), 0)
	switch r {
	case 0, errorAlreadyAssigned, errorSessionCredential:
		return nil
	default:
		return syscall.Errno(r)
	}
}
