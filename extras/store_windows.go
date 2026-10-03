/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package extras

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/amnezia-vpn/amneziawg-windows/v3/conf"

	"github.com/amnezia-vpn/amneziawg-windows-client/splittunnel"
)

var storeLock sync.Mutex

// Dir returns the directory holding extras. It lives inside the data
// directory, so it inherits the SYSTEM/Administrators-only ACL.
func Dir() (string, error) {
	root, err := conf.RootDirectory(true)
	if err != nil {
		return "", err
	}
	d := filepath.Join(root, "Extras")
	if err := os.Mkdir(d, 0700); err != nil && !os.IsExist(err) {
		return "", err
	}
	return d, nil
}

func splitPath(tunnelName string) (string, error) {
	if !conf.TunnelNameIsValid(tunnelName) {
		return "", errors.New("Tunnel name is not valid")
	}
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, tunnelName+".split.json"), nil
}

// writeFileAtomic writes via a temporary file so readers never see partial
// content.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// LoadSplit returns the split tunneling rules of a tunnel; a missing file
// yields the default (off) configuration.
func LoadSplit(tunnelName string) (*splittunnel.Config, error) {
	p, err := splitPath(tunnelName)
	if err != nil {
		return nil, err
	}
	storeLock.Lock()
	data, err := os.ReadFile(p)
	storeLock.Unlock()
	if errors.Is(err, os.ErrNotExist) {
		return splittunnel.Parse(nil)
	}
	if err != nil {
		return nil, err
	}
	return splittunnel.Parse(data)
}

// SaveSplit validates and stores split tunneling rules.
func SaveSplit(tunnelName string, c *splittunnel.Config) error {
	if err := c.Validate(); err != nil {
		return err
	}
	p, err := splitPath(tunnelName)
	if err != nil {
		return err
	}
	data, err := c.Marshal()
	if err != nil {
		return err
	}
	storeLock.Lock()
	defer storeLock.Unlock()
	return writeFileAtomic(p, data)
}

// DeleteTunnel removes all extras of a tunnel.
func DeleteTunnel(tunnelName string) error {
	p, err := splitPath(tunnelName)
	if err != nil {
		return err
	}
	storeLock.Lock()
	defer storeLock.Unlock()
	err = os.Remove(p)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// RenameTunnel moves extras along with a renamed tunnel.
func RenameTunnel(oldName, newName string) error {
	from, err := splitPath(oldName)
	if err != nil {
		return err
	}
	to, err := splitPath(newName)
	if err != nil {
		return err
	}
	storeLock.Lock()
	defer storeLock.Unlock()
	err = os.Rename(from, to)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func settingsPath() (string, error) {
	d, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "settings.json"), nil
}

// LoadSettings reads the global settings.
func LoadSettings() (*Settings, error) {
	p, err := settingsPath()
	if err != nil {
		return nil, err
	}
	storeLock.Lock()
	data, err := os.ReadFile(p)
	storeLock.Unlock()
	if errors.Is(err, os.ErrNotExist) {
		return DefaultSettings(), nil
	}
	if err != nil {
		return nil, err
	}
	return ParseSettings(data)
}

// SaveSettings stores the global settings.
func SaveSettings(s *Settings) error {
	p, err := settingsPath()
	if err != nil {
		return err
	}
	data, err := s.Marshal()
	if err != nil {
		return err
	}
	storeLock.Lock()
	defer storeLock.Unlock()
	return writeFileAtomic(p, data)
}
