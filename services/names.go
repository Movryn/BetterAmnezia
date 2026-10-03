/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package services

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/amnezia-vpn/amneziawg-windows/v3/conf"
)

// BetterAmnezia uses its own names for everything that is visible system
// wide, so it can be installed next to the official AmneziaWG client (or
// WireGuard) without the two fighting over services, pipes or files.
const (
	ProductName         = "BetterAmnezia"
	ManagerServiceName  = "BetterAmneziaManager"
	tunnelServicePrefix = "BetterAmneziaTunnel$"
	pipePrefix          = `\\.\pipe\ProtectedPrefix\Administrators\BetterAmnezia\`
	// SplitDriverServiceName is the kernel service for the optional split
	// tunnel driver.
	SplitDriverServiceName = "BetterAmneziaSplitTunnel"
)

// ServiceNameOfTunnel returns the Windows service name for a tunnel.
func ServiceNameOfTunnel(tunnelName string) (string, error) {
	if !conf.TunnelNameIsValid(tunnelName) {
		return "", errors.New("Tunnel name is not valid")
	}
	return tunnelServicePrefix + tunnelName, nil
}

// PipePathOfTunnel returns the UAPI named pipe of a tunnel.
func PipePathOfTunnel(tunnelName string) (string, error) {
	if !conf.TunnelNameIsValid(tunnelName) {
		return "", errors.New("Tunnel name is not valid")
	}
	return pipePrefix + tunnelName, nil
}

// IsTunnelServiceName reports whether a service belongs to one of our
// tunnels.
func IsTunnelServiceName(name string) bool {
	return strings.HasPrefix(name, tunnelServicePrefix)
}

func installRoot() (string, error) {
	pf, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return "", err
	}
	return filepath.Join(pf, ProductName), nil
}

// UseOwnDataDirectory points the configuration store, the log and the
// extras at %ProgramFiles%\BetterAmnezia\Data instead of the official
// client's folder. Processes running as SYSTEM create the directory with a
// SYSTEM/Administrators-only ACL; others only use the path.
func UseOwnDataDirectory(create bool) error {
	root, err := installRoot()
	if err != nil {
		return err
	}
	data := filepath.Join(root, "Data")
	if create {
		if err := createDataDirectory(root, data); err != nil {
			return err
		}
	}
	conf.PresetRootDirectory(data)
	return nil
}

// createDataDirectory mirrors conf.RootDirectory: it creates the directory
// with a protected ACL and makes sure it is a real directory at the expected
// place, not a reparse point planted by someone else.
func createDataDirectory(root, data string) error {
	root16, err := windows.UTF16PtrFromString(root)
	if err != nil {
		return err
	}
	// The root inherits its ACL from Program Files.
	err = windows.CreateDirectory(root16, nil)
	if err != nil && err != windows.ERROR_ALREADY_EXISTS {
		return err
	}
	sd, err := windows.SecurityDescriptorFromString("O:SYG:SYD:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)")
	if err != nil {
		return err
	}
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	data16, err := windows.UTF16PtrFromString(data)
	if err != nil {
		return err
	}
	var h windows.Handle
	for {
		err = windows.CreateDirectory(data16, sa)
		if err != nil && err != windows.ERROR_ALREADY_EXISTS {
			return err
		}
		h, err = windows.CreateFile(data16, windows.READ_CONTROL|windows.WRITE_OWNER|windows.WRITE_DAC, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT|windows.FILE_ATTRIBUTE_DIRECTORY, 0)
		if err != nil && err != windows.ERROR_FILE_NOT_FOUND {
			return err
		}
		if err == nil {
			break
		}
	}
	defer windows.CloseHandle(h)
	var fi windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(h, &fi); err != nil {
		return err
	}
	if fi.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 {
		return errors.New("Data directory is actually a file")
	}
	if fi.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		return errors.New("Data directory is reparse point")
	}
	buf := make([]uint16, windows.MAX_PATH+4)
	for {
		n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
		if err != nil {
			return err
		}
		if n < uint32(len(buf)) {
			break
		}
		buf = make([]uint16, n)
	}
	if !strings.EqualFold(`\\?\`+data, windows.UTF16ToString(buf)) {
		return errors.New("Data directory jumped to unexpected location")
	}
	return windows.SetKernelObjectSecurity(h, windows.DACL_SECURITY_INFORMATION|windows.GROUP_SECURITY_INFORMATION|windows.OWNER_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION, sd)
}

// DataDirectoryExists reports whether our data directory was set up before.
func DataDirectoryExists() bool {
	root, err := installRoot()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(root, "Data", "Configurations"))
	return err == nil
}
