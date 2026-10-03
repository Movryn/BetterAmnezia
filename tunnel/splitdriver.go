/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package tunnel

import (
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/amnezia-vpn/amneziawg-windows/v3/tunnel/winipcfg"

	"github.com/amnezia-vpn/amneziawg-windows-client/extras"
	"github.com/amnezia-vpn/amneziawg-windows-client/tunnel/firewall"
)

// This file talks to the Mullvad split tunnel driver
// (https://github.com/mullvad/win-split-tunnel, GPL-3.0 or MPL-2.0), which
// keeps the traffic of selected applications out of the tunnel. The driver is
// optional and not shipped with this program; see docs/splittunnel.md.

const (
	splitDriverDevice      = `\\.\MULLVADSPLITTUNNEL`
	splitDriverServiceName = "AmneziaWGSplitTunnel"
	splitDriverFileName    = "mullvad-split-tunnel.sys"

	stDeviceType = 0x8000

	stStateStarted     = 1
	stStateInitialized = 2
	stStateReady       = 3
	stStateEngaged     = 4
	stStateZombie      = 5
)

func ctlCode(function, method uint32) uint32 {
	return stDeviceType<<16 | function<<2 | method
}

var (
	ioctlInitialize        = ctlCode(1, 0)
	ioctlRegisterProcesses = ctlCode(3, 0)
	ioctlRegisterIPs       = ctlCode(4, 0)
	ioctlSetConfiguration  = ctlCode(6, 0)
	ioctlClearConfig       = ctlCode(8, 3)
	ioctlGetState          = ctlCode(9, 0)
	ioctlReset             = ctlCode(11, 3)
)

type splitDriver struct {
	handle windows.Handle
	luid   winipcfg.LUID
	apps   []string
	tunV4  netip.Addr
	tunV6  netip.Addr
}

func (d *splitDriver) ioctl(code uint32, in []byte, out []byte) (uint32, error) {
	var inPtr, outPtr *byte
	if len(in) > 0 {
		inPtr = &in[0]
	}
	if len(out) > 0 {
		outPtr = &out[0]
	}
	var returned uint32
	err := windows.DeviceIoControl(d.handle, code, inPtr, uint32(len(in)), outPtr, uint32(len(out)), &returned, nil)
	return returned, err
}

func (d *splitDriver) state() (uint64, error) {
	out := make([]byte, 8)
	if _, err := d.ioctl(ioctlGetState, nil, out); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(out), nil
}

// splitDriverPath finds the driver binary: the configured path, then next to
// our executable.
func splitDriverPath() (string, error) {
	if s, err := extras.LoadSettings(); err == nil && s.SplitDriverPath != "" {
		if _, err := os.Stat(s.SplitDriverPath); err == nil {
			return s.SplitDriverPath, nil
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	p := filepath.Join(filepath.Dir(exe), splitDriverFileName)
	if _, err := os.Stat(p); err != nil {
		return "", fmt.Errorf("%s not found next to the program and no driver path configured", splitDriverFileName)
	}
	return p, nil
}

func openSplitDevice() (windows.Handle, error) {
	name, _ := windows.UTF16PtrFromString(splitDriverDevice)
	return windows.CreateFile(name, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil, windows.OPEN_EXISTING, 0, 0)
}

// loadSplitDriver installs and starts the driver service when needed.
func loadSplitDriver() (windows.Handle, error) {
	if h, err := openSplitDevice(); err == nil {
		return h, nil
	}
	path, err := splitDriverPath()
	if err != nil {
		return 0, err
	}
	m, err := mgr.Connect()
	if err != nil {
		return 0, err
	}
	defer m.Disconnect()
	service, err := m.OpenService(splitDriverServiceName)
	if err != nil {
		service, err = m.CreateService(splitDriverServiceName, path, mgr.Config{
			ServiceType:  windows.SERVICE_KERNEL_DRIVER,
			StartType:    mgr.StartManual,
			ErrorControl: mgr.ErrorNormal,
			DisplayName:  "AmneziaWG split tunnel driver",
		})
		if err != nil {
			return 0, fmt.Errorf("unable to install split tunnel driver: %w", err)
		}
	} else if cfg, err := service.Config(); err == nil && !strings.EqualFold(cfg.BinaryPathName, path) && !strings.HasSuffix(strings.ToLower(cfg.BinaryPathName), strings.ToLower(path)) {
		cfg.BinaryPathName = path
		service.UpdateConfig(cfg)
	}
	defer service.Close()
	if err := service.Start(); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
		return 0, fmt.Errorf("unable to start split tunnel driver: %w", err)
	}
	for i := 0; i < 50; i++ {
		if status, err := service.Query(); err == nil && status.State == svc.Running {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	return openSplitDevice()
}

// startDriver loads the driver if any application should bypass the
// tunnel. It runs before the firewall is enabled.
func (s *splitRuntime) startDriver() error {
	apps := s.bypassApps()
	if len(apps) == 0 {
		return nil
	}
	if unsafe.Sizeof(uintptr(0)) != 8 {
		return errors.New("the split tunnel driver requires 64-bit Windows")
	}
	h, err := loadSplitDriver()
	if err != nil {
		return err
	}
	d := &splitDriver{handle: h, tunV4: s.tunV4, tunV6: s.tunV6}
	for _, a := range apps {
		dev, err := devicePath(a)
		if err != nil {
			log.Printf("Split tunneling: skipping %s: %v", a, err)
			continue
		}
		d.apps = append(d.apps, dev)
	}
	if len(d.apps) == 0 {
		windows.CloseHandle(h)
		return errors.New("none of the bypass applications could be resolved")
	}
	st, err := d.state()
	if err != nil {
		windows.CloseHandle(h)
		return err
	}
	if st != stStateStarted {
		// Left over from an earlier run that did not shut down cleanly.
		if _, err := d.ioctl(ioctlReset, nil, nil); err != nil {
			windows.CloseHandle(h)
			return fmt.Errorf("unable to reset split tunnel driver (state %d): %w", st, err)
		}
	}
	s.driver = d
	return nil
}

// engageDriver configures the driver once the tunnel and firewall are up.
func (s *splitRuntime) engageDriver() {
	d := s.driver
	if d == nil {
		return
	}
	d.luid = s.luid
	if err := d.engage(); err != nil {
		log.Printf("Split tunneling: unable to engage split tunnel driver, bypass rules are inactive: %v", err)
		d.Close()
		s.driver = nil
		return
	}
	log.Printf("Split tunneling: driver engaged for %d applications", len(d.apps))
	s.watchDefaultRoutes()
}

func (d *splitDriver) engage() error {
	key, ok := firewall.SublayerKey()
	if !ok {
		return errors.New("firewall sublayer unavailable")
	}
	guids := make([]byte, 32)
	putGUID(guids[0:16], key)
	putGUID(guids[16:32], key)
	if _, err := d.ioctl(ioctlInitialize, guids, nil); err != nil {
		// Drivers before 1.3 take no input.
		if _, err2 := d.ioctl(ioctlInitialize, nil, nil); err2 != nil {
			return fmt.Errorf("initialize: %w", err)
		}
		log.Println("Split tunneling: old split tunnel driver; it may not be able to pass the kill switch")
	}
	procs, err := processDiscoveryPayload()
	if err != nil {
		return fmt.Errorf("process discovery: %w", err)
	}
	if _, err := d.ioctl(ioctlRegisterProcesses, procs, nil); err != nil {
		return fmt.Errorf("register processes: %w", err)
	}
	if _, err := d.ioctl(ioctlSetConfiguration, configurationPayload(d.apps), nil); err != nil {
		return fmt.Errorf("set configuration: %w", err)
	}
	return d.registerIPs()
}

// registerIPs tells the driver the tunnel and internet addresses. It is
// called again when the default route moves.
func (d *splitDriver) registerIPs() error {
	payload := make([]byte, 40)
	if d.tunV4.IsValid() {
		a := d.tunV4.As4()
		copy(payload[0:4], a[:])
	}
	if a, ok := internetAddress(windows.AF_INET, d.luid); ok {
		b := a.As4()
		copy(payload[4:8], b[:])
	}
	if d.tunV6.IsValid() {
		a := d.tunV6.As16()
		copy(payload[8:24], a[:])
	}
	if a, ok := internetAddress(windows.AF_INET6, d.luid); ok {
		b := a.As16()
		copy(payload[24:40], b[:])
	}
	_, err := d.ioctl(ioctlRegisterIPs, payload, nil)
	if err != nil {
		return fmt.Errorf("register IP addresses: %w", err)
	}
	return nil
}

func (d *splitDriver) Close() {
	if d.handle == 0 {
		return
	}
	if _, err := d.ioctl(ioctlReset, nil, nil); err != nil {
		d.ioctl(ioctlClearConfig, nil, nil)
	}
	windows.CloseHandle(d.handle)
	d.handle = 0
}

func putGUID(b []byte, g windows.GUID) {
	binary.LittleEndian.PutUint32(b[0:4], g.Data1)
	binary.LittleEndian.PutUint16(b[4:6], g.Data2)
	binary.LittleEndian.PutUint16(b[6:8], g.Data3)
	copy(b[8:16], g.Data4[:])
}

// internetAddress returns the first unicast address of the interface that
// carries the best non-tunnel default route.
func internetAddress(family winipcfg.AddressFamily, tunnel winipcfg.LUID) (netip.Addr, bool) {
	gw, ok := bestPhysicalDefault(family, tunnel)
	if !ok {
		return netip.Addr{}, false
	}
	rows, err := winipcfg.GetUnicastIPAddressTable(family)
	if err != nil {
		return netip.Addr{}, false
	}
	for i := range rows {
		if rows[i].InterfaceLUID != gw.luid {
			continue
		}
		a, ok := netip.AddrFromSlice(rows[i].Address.IP())
		if !ok {
			continue
		}
		a = a.Unmap()
		if a.IsLinkLocalUnicast() {
			continue
		}
		return a, true
	}
	return netip.Addr{}, false
}

// devicePath converts C:\dir\app.exe to \device\harddiskvolume3\dir\app.exe,
// the form the driver matches on.
func devicePath(path string) (string, error) {
	path = filepath.Clean(path)
	if len(path) < 3 || path[1] != ':' {
		return "", errors.New("path must start with a drive letter")
	}
	drive, _ := windows.UTF16PtrFromString(path[:2])
	buf := make([]uint16, 1024)
	n, err := windows.QueryDosDevice(drive, &buf[0], uint32(len(buf)))
	if err != nil || n == 0 {
		return "", fmt.Errorf("unable to resolve drive %s: %w", path[:2], err)
	}
	return strings.ToLower(windows.UTF16ToString(buf) + path[2:]), nil
}

func utf16Bytes(s string) []byte {
	u := windows.StringToUTF16(s)
	u = u[:len(u)-1]
	b := make([]byte, 2*len(u))
	for i, c := range u {
		binary.LittleEndian.PutUint16(b[2*i:], c)
	}
	return b
}

// configurationPayload builds ST_CONFIGURATION_HEADER, entries and strings.
func configurationPayload(apps []string) []byte {
	const headerSize, entrySize = 16, 16
	var strs [][]byte
	total := headerSize + entrySize*len(apps)
	for _, a := range apps {
		b := utf16Bytes(a)
		strs = append(strs, b)
		total += len(b)
	}
	buf := make([]byte, total)
	binary.LittleEndian.PutUint64(buf[0:], uint64(len(apps)))
	binary.LittleEndian.PutUint64(buf[8:], uint64(total))
	strBase := headerSize + entrySize*len(apps)
	offset := 0
	for i, b := range strs {
		e := buf[headerSize+entrySize*i:]
		binary.LittleEndian.PutUint64(e[0:], uint64(offset))
		binary.LittleEndian.PutUint16(e[8:], uint16(len(b)))
		copy(buf[strBase+offset:], b)
		offset += len(b)
	}
	return buf
}

type discoveredProcess struct {
	pid, ppid uint32
	created   uint64
	path      string
}

// processDiscoveryPayload builds ST_PROCESS_DISCOVERY_HEADER and entries for
// every process in the system.
func processDiscoveryPayload() ([]byte, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	entry := windows.ProcessEntry32{Size: uint32(unsafe.Sizeof(windows.ProcessEntry32{}))}
	if err := windows.Process32First(snap, &entry); err != nil {
		return nil, err
	}
	procs := make(map[uint32]*discoveredProcess)
	for {
		p := &discoveredProcess{pid: entry.ProcessID, ppid: entry.ParentProcessID}
		if h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, entry.ProcessID); err == nil {
			var creation, dummy windows.Filetime
			if windows.GetProcessTimes(h, &creation, &dummy, &dummy, &dummy) == nil {
				p.created = uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime)
			}
			buf := make([]uint16, 1024)
			if n, err := getProcessImageFileName(h, buf); err == nil {
				p.path = strings.ToLower(windows.UTF16ToString(buf[:n]))
			}
			windows.CloseHandle(h)
			procs[p.pid] = p
		}
		if err := windows.Process32Next(snap, &entry); err != nil {
			break
		}
	}
	// Detect PID reuse: a parent must be older than its child.
	for _, p := range procs {
		parent, ok := procs[p.ppid]
		if !ok || parent.created == 0 || parent.created >= p.created {
			p.ppid = 0
		}
	}
	const headerSize, entrySize = 16, 32
	total := headerSize + entrySize*len(procs)
	var strs [][]byte
	list := make([]*discoveredProcess, 0, len(procs))
	for _, p := range procs {
		list = append(list, p)
		b := utf16Bytes(p.path)
		strs = append(strs, b)
		total += len(b)
	}
	buf := make([]byte, total)
	binary.LittleEndian.PutUint64(buf[0:], uint64(len(list)))
	binary.LittleEndian.PutUint64(buf[8:], uint64(total))
	strBase := headerSize + entrySize*len(list)
	offset := 0
	for i, p := range list {
		e := buf[headerSize+entrySize*i:]
		binary.LittleEndian.PutUint64(e[0:], uint64(p.pid))
		binary.LittleEndian.PutUint64(e[8:], uint64(p.ppid))
		if len(strs[i]) > 0 {
			binary.LittleEndian.PutUint64(e[16:], uint64(offset))
			binary.LittleEndian.PutUint16(e[24:], uint16(len(strs[i])))
			copy(buf[strBase+offset:], strs[i])
			offset += len(strs[i])
		}
	}
	return buf, nil
}

var (
	modpsapi                     = windows.NewLazySystemDLL("psapi.dll")
	procGetProcessImageFileNameW = modpsapi.NewProc("GetProcessImageFileNameW")
)

func getProcessImageFileName(h windows.Handle, buf []uint16) (int, error) {
	r, _, err := procGetProcessImageFileNameW.Call(uintptr(h), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if r == 0 {
		return 0, err
	}
	return int(r), nil
}
