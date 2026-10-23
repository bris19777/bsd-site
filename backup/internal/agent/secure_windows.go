//go:build windows

package agent

import (
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

// DefaultDataDir is %ProgramData%\BSDBackup.
func DefaultDataDir() string {
	pd := os.Getenv("ProgramData")
	if pd == "" {
		pd = `C:\ProgramData`
	}
	return filepath.Join(pd, "BSDBackup")
}

// The installer (an administrator) and the service (LocalSystem) are
// different accounts, so secrets use machine-scope DPAPI and the data
// directory is locked to SYSTEM and Administrators. Other local users can
// neither read the files nor, therefore, decrypt them.
var entropy = []byte("bsd-backup agent secrets v1")

func protect(plain []byte) ([]byte, error) {
	return dpapi(plain, true)
}

func unprotect(sealed []byte) ([]byte, error) {
	return dpapi(sealed, false)
}

func dpapi(in []byte, seal bool) ([]byte, error) {
	if len(in) == 0 {
		return nil, nil
	}
	inBlob := windows.DataBlob{Size: uint32(len(in)), Data: &in[0]}
	entBlob := windows.DataBlob{Size: uint32(len(entropy)), Data: &entropy[0]}
	var out windows.DataBlob
	flags := uint32(windows.CRYPTPROTECT_UI_FORBIDDEN | windows.CRYPTPROTECT_LOCAL_MACHINE)
	var err error
	if seal {
		err = windows.CryptProtectData(&inBlob, nil, &entBlob, 0, nil, flags, &out)
	} else {
		err = windows.CryptUnprotectData(&inBlob, nil, &entBlob, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out)
	}
	if err != nil {
		return nil, err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return append([]byte(nil), unsafe.Slice(out.Data, out.Size)...), nil
}

// secureDir creates dir with an ACL granting full control to SYSTEM and
// Administrators only, with inheritance disabled.
func secureDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("D:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	if err != nil {
		return err
	}
	dacl, _, err := sd.DACL()
	if err != nil {
		return err
	}
	return windows.SetNamedSecurityInfo(dir, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, dacl, nil)
}

// lowerPriority keeps backups from competing with the user's foreground work.
func lowerPriority() {
	windows.SetPriorityClass(windows.CurrentProcess(), windows.BELOW_NORMAL_PRIORITY_CLASS)
}
