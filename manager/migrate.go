/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package manager

import (
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"

	"github.com/amnezia-vpn/amneziawg-windows/v3/conf"
)

// migrateFromSharedDirectory copies tunnels from %ProgramFiles%\AmneziaWG
// the first time this version runs. Earlier BetterAmnezia builds stored
// their data there, shared with the official client; the Extras folder only
// exists if BetterAmnezia was used, so official-only installs are left
// alone.
func migrateFromSharedDirectory() {
	root, err := conf.RootDirectory(true)
	if err != nil {
		return
	}
	newConfigs := filepath.Join(root, "Configurations")
	if _, err := os.Stat(newConfigs); err == nil {
		return
	}
	pf, err := windows.KnownFolderPath(windows.FOLDERID_ProgramFiles, windows.KF_FLAG_DEFAULT)
	if err != nil {
		return
	}
	old := filepath.Join(pf, "AmneziaWG", "Data")
	if _, err := os.Stat(filepath.Join(old, "Extras")); err != nil {
		return
	}
	if err := os.Mkdir(newConfigs, 0700); err != nil {
		return
	}
	n := copyMatching(filepath.Join(old, "Configurations"), newConfigs, func(name string) bool {
		return strings.HasSuffix(name, ".conf.dpapi")
	})
	newExtras := filepath.Join(root, "Extras")
	os.Mkdir(newExtras, 0700)
	copyMatching(filepath.Join(old, "Extras"), newExtras, func(name string) bool {
		return strings.HasSuffix(name, ".json")
	})
	log.Printf("Copied %d tunnels from the shared AmneziaWG data directory", n)
}

func copyMatching(from, to string, match func(string) bool) int {
	entries, err := os.ReadDir(from)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if !e.Type().IsRegular() || !match(e.Name()) {
			continue
		}
		src, err := os.Open(filepath.Join(from, e.Name()))
		if err != nil {
			continue
		}
		dst, err := os.OpenFile(filepath.Join(to, e.Name()), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err == nil {
			if _, err = io.Copy(dst, src); err == nil {
				n++
			}
			dst.Close()
		}
		src.Close()
	}
	return n
}
