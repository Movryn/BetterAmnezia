/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package webui

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/amnezia-vpn/amneziawg-windows-client/ghupdate"
	"github.com/amnezia-vpn/amneziawg-windows-client/services"
	"github.com/amnezia-vpn/amneziawg-windows-client/version"
)

// updates tracks the newest release found on GitHub and an installer
// download in progress.
type updates struct {
	b *bridge

	mu          sync.Mutex
	latest      *ghupdate.Release
	checking    bool
	checkedAt   time.Time
	err         string
	installing  bool
	downloaded  int64
	total       int64
	lastEmitted time.Time
}

func userAgent() string {
	return "BetterAmnezia/" + version.Number + " (" + version.OsName() + "; " + version.Arch() + ")"
}

// installedWithMSI reports whether this executable is the one the installer
// put into Program Files; portable copies are updated by hand.
func installedWithMSI() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	dir, err := services.InstallDirectory()
	if err != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(filepath.Dir(exe)), filepath.Clean(dir))
}

func (u *updates) status() map[string]any {
	u.mu.Lock()
	defer u.mu.Unlock()
	s := map[string]any{
		"current":    version.Number,
		"checking":   u.checking,
		"installing": u.installing,
		"downloaded": u.downloaded,
		"total":      u.total,
		"canInstall": installedWithMSI(),
		"error":      u.err,
	}
	if !u.checkedAt.IsZero() {
		s["checkedAt"] = u.checkedAt.Unix()
	}
	if r := u.latest; r != nil {
		s["latest"] = r.Version()
		s["available"] = ghupdate.Newer(r.Version(), version.Number)
		s["notes"] = r.Notes
		s["url"] = r.PageURL
	}
	return s
}

func (u *updates) emit(prompt bool) {
	s := u.status()
	s["prompt"] = prompt
	u.b.emit("update", s)
}

// check asks GitHub for the newest release. With prompt set (the automatic
// check at start-up) a new version is announced to the user, unless they
// turned the reminder off.
func (u *updates) check(prompt bool) {
	u.mu.Lock()
	if u.checking {
		u.mu.Unlock()
		return
	}
	u.checking = true
	u.err = ""
	u.mu.Unlock()
	u.emit(false)

	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	r, err := ghupdate.Latest(ctx, userAgent())

	u.mu.Lock()
	u.checking = false
	u.checkedAt = time.Now()
	if err != nil {
		u.err = err.Error()
		log.Printf("Update check failed: %v", err)
	} else {
		u.latest = r
	}
	u.mu.Unlock()

	available := err == nil && ghupdate.Newer(r.Version(), version.Number)
	prompt = prompt && available && loadPrefs().UpdatePrompt
	u.emit(prompt)
	if prompt && !u.b.w.visible() {
		u.b.w.notifyAlways("BetterAmnezia "+r.Version(), tr("A new version is available. Click to see what's new."))
	}
}

// startupCheck runs the automatic check shortly after start-up.
func (u *updates) startupCheck() {
	if !loadPrefs().CheckUpdates {
		return
	}
	go func() {
		// Let the tunnels and the page settle first.
		time.Sleep(8 * time.Second)
		u.check(true)
	}()
}

// installerPath creates an empty file only SYSTEM and Administrators can
// write, so nothing unprivileged can swap the installer between the
// checksum test and msiexec.
func installerPath(name string) (string, error) {
	var rnd [8]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", err
	}
	windir, err := windows.GetWindowsDirectory()
	if err != nil {
		return "", err
	}
	path := filepath.Join(windir, "Temp", "betteramnezia-update-"+hex.EncodeToString(rnd[:])+"-"+name)
	sd, err := windows.SecurityDescriptorFromString("D:PAI(A;;FA;;;SY)(A;;FA;;;BA)")
	if err != nil {
		return "", err
	}
	sa := &windows.SecurityAttributes{Length: uint32(unsafe.Sizeof(windows.SecurityAttributes{})), SecurityDescriptor: sd}
	path16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(path16, windows.GENERIC_WRITE, 0, sa, windows.CREATE_NEW, windows.FILE_ATTRIBUTE_TEMPORARY, 0)
	if err != nil {
		return "", err
	}
	windows.CloseHandle(h)
	// Clean up on the next reboot in any case.
	windows.MoveFileEx(path16, nil, windows.MOVEFILE_DELAY_UNTIL_REBOOT)
	return path, nil
}

// install downloads the installer of the newest release, verifies it and
// starts it. The installer closes BetterAmnezia, upgrades it and starts the
// new version.
func (u *updates) install() error {
	if !installedWithMSI() {
		return errors.New(tr("This copy was not installed with the installer. Download the new version from the release page."))
	}
	u.mu.Lock()
	r := u.latest
	if u.installing {
		u.mu.Unlock()
		return errors.New(tr("The update is already being downloaded."))
	}
	if r == nil || !ghupdate.Newer(r.Version(), version.Number) {
		u.mu.Unlock()
		return errors.New(tr("No update is available."))
	}
	asset := r.Installer(version.Arch())
	if asset == nil {
		u.mu.Unlock()
		return errors.New(tr("The release has no installer for this computer."))
	}
	u.installing = true
	u.downloaded, u.total = 0, asset.Size
	u.err = ""
	u.mu.Unlock()
	u.emit(false)

	go func() {
		err := u.downloadAndRun(r, asset)
		u.mu.Lock()
		u.installing = false
		if err != nil {
			u.err = err.Error()
		}
		u.mu.Unlock()
		if err != nil {
			log.Printf("Update failed: %v", err)
		}
		u.emit(false)
	}()
	return nil
}

func (u *updates) downloadAndRun(r *ghupdate.Release, asset *ghupdate.Asset) error {
	path, err := installerPath(asset.Name)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	err = r.Download(ctx, asset, path, userAgent(), func(done, total int64) {
		u.mu.Lock()
		u.downloaded, u.total = done, total
		emit := time.Since(u.lastEmitted) > 200*time.Millisecond
		if emit {
			u.lastEmitted = time.Now()
		}
		u.mu.Unlock()
		if emit {
			u.emit(false)
		}
	})
	if err != nil {
		return err
	}
	system32, err := windows.GetSystemDirectory()
	if err != nil {
		return err
	}
	cmd := exec.Command(filepath.Join(system32, "msiexec.exe"), "/i", path, "/qb!-", "/norestart")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
	if err := cmd.Start(); err != nil {
		return err
	}
	log.Printf("Started installer for version %s", r.Version())
	// msiexec outlives us: the installer stops BetterAmnezia and starts the
	// new version when it is done.
	cmd.Process.Release()
	return nil
}

func (b *bridge) updateStatus(json.RawMessage) (any, error) { return b.updates.status(), nil }

func (b *bridge) updateCheck(json.RawMessage) (any, error) {
	b.updates.check(false)
	return b.updates.status(), nil
}

func (b *bridge) updateInstall(json.RawMessage) (any, error) {
	if err := requireAdmin(); err != nil {
		return nil, err
	}
	return nil, b.updates.install()
}
