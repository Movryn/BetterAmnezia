/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package webui

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/amnezia-vpn/amneziawg-windows/v3/conf"

	"github.com/amnezia-vpn/amneziawg-windows-client/extras"
	"github.com/amnezia-vpn/amneziawg-windows-client/importer"
	"github.com/amnezia-vpn/amneziawg-windows-client/manager"
	"github.com/amnezia-vpn/amneziawg-windows-client/ringlogger"
	"github.com/amnezia-vpn/amneziawg-windows-client/splittunnel"
	"github.com/amnezia-vpn/amneziawg-windows-client/updater"
	"github.com/amnezia-vpn/amneziawg-windows-client/version"
)

type request struct {
	ID     int64           `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type handler func(params json.RawMessage) (any, error)

type bridge struct {
	w        *window
	handlers map[string]handler

	quitManager       bool
	stopTunnelsOnQuit bool

	callbacks []interface{ Unregister() }
	stateMu   sync.Mutex
	states    map[string]manager.TunnelState
}

func newBridge(w *window) *bridge {
	b := &bridge{w: w, states: make(map[string]manager.TunnelState)}
	b.handlers = map[string]handler{
		"app.info":            b.appInfo,
		"app.quit":            b.appQuit,
		"app.legacyUI":        b.appLegacyUI,
		"window.hide":         func(json.RawMessage) (any, error) { w.dispatch(w.hide); return nil, nil },
		"window.minimize":     b.windowMinimize,
		"prefs.get":           func(json.RawMessage) (any, error) { return loadPrefs(), nil },
		"prefs.set":           b.prefsSet,
		"tunnels.list":        b.tunnelsList,
		"tunnels.get":         b.tunnelsGet,
		"tunnels.runtime":     b.tunnelsRuntime,
		"tunnels.start":       b.tunnelAction("start"),
		"tunnels.stop":        b.tunnelAction("stop"),
		"tunnels.toggle":      b.tunnelAction("toggle"),
		"tunnels.restart":     b.tunnelsRestart,
		"tunnels.save":        b.tunnelsSave,
		"tunnels.validate":    b.tunnelsValidate,
		"tunnels.delete":      b.tunnelsDelete,
		"tunnels.rename":      b.tunnelsRename,
		"tunnels.duplicate":   b.tunnelsDuplicate,
		"tunnels.template":    b.tunnelsTemplate,
		"tunnels.importText":  b.importText,
		"tunnels.importFiles": b.importFiles,
		"tunnels.importData":  b.importData,
		"tunnels.exportZip":   b.exportZip,
		"tunnels.exportConf":  b.exportConf,
		"tunnels.shareKey":    b.shareKey,
		"tunnels.qr":          b.qrCode,
		"keys.generate":       b.keysGenerate,
		"keys.public":         b.keysPublic,
		"split.get":           b.splitGet,
		"split.set":           b.splitSet,
		"split.validate":      b.splitValidate,
		"settings.get":        b.settingsGet,
		"settings.set":        b.settingsSet,
		"status.get":          b.statusGet,
		"dialog.pickApps":     b.pickApps,
		"dialog.pickFolder":   b.pickFolder,
		"dialog.pickDriver":   b.pickDriver,
		"apps.running":        b.appsRunning,
		"apps.describe":       b.appsDescribe,
		"log.follow":          b.logFollow,
		"log.save":            b.logSave,
		"clipboard.read":      b.clipboardRead,
		"clipboard.write":     b.clipboardWrite,
		"shell.openURL":       b.openURL,
		"update.state":        b.updateState,
		"update.start":        b.updateStart,
	}
	return b
}

// handleMessage receives a request from the page on the UI thread and runs
// it in the background.
func (b *bridge) handleMessage(message string) {
	var req request
	if err := json.Unmarshal([]byte(message), &req); err != nil || req.Method == "" {
		return
	}
	h, ok := b.handlers[req.Method]
	if !ok {
		b.reply(req.ID, nil, fmt.Errorf("unknown method %s", req.Method))
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("UI bridge panic in %s: %v", req.Method, r)
				b.reply(req.ID, nil, fmt.Errorf("internal error: %v", r))
			}
		}()
		result, err := h(req.Params)
		b.reply(req.ID, result, err)
	}()
}

func (b *bridge) reply(id int64, result any, err error) {
	resp := map[string]any{"id": id}
	if err != nil {
		resp["error"] = err.Error()
	} else {
		resp["result"] = result
	}
	data, mErr := json.Marshal(resp)
	if mErr != nil {
		data, _ = json.Marshal(map[string]any{"id": id, "error": mErr.Error()})
	}
	b.w.eval("window.__bridge&&window.__bridge.reply(" + string(data) + ")")
}

// emit sends an event to the page.
func (b *bridge) emit(event string, payload any) {
	data, err := json.Marshal(map[string]any{"event": event, "data": payload})
	if err != nil {
		return
	}
	b.w.eval("window.__bridge&&window.__bridge.event(" + string(data) + ")")
}

func decode[T any](params json.RawMessage) (T, error) {
	var v T
	if len(params) == 0 || string(params) == "null" {
		return v, nil
	}
	err := json.Unmarshal(params, &v)
	return v, err
}

type nameParams struct {
	Name string `json:"name"`
}

func requireAdmin() error {
	if !IsAdmin {
		return errors.New(tr("This action requires administrator rights."))
	}
	return nil
}

func stateName(s manager.TunnelState) string {
	switch s {
	case manager.TunnelStarted:
		return "started"
	case manager.TunnelStopped:
		return "stopped"
	case manager.TunnelStarting:
		return "starting"
	case manager.TunnelStopping:
		return "stopping"
	}
	return "unknown"
}

// subscribe forwards manager notifications to the page and the tray.
func (b *bridge) subscribe() {
	b.callbacks = append(b.callbacks,
		manager.IPCClientRegisterTunnelChange(func(t *manager.Tunnel, state, globalState manager.TunnelState, err error) {
			payload := map[string]any{"name": t.Name, "state": stateName(state), "global": stateName(globalState)}
			if err != nil {
				payload["error"] = err.Error()
			}
			b.emit("tunnelChange", payload)
			b.stateMu.Lock()
			prev, known := b.states[t.Name]
			b.states[t.Name] = state
			b.stateMu.Unlock()
			b.refreshTray(globalState)
			if err != nil {
				b.w.notify(t.Name, tr("Tunnel error: ")+err.Error())
			} else if known && prev != state {
				switch state {
				case manager.TunnelStarted:
					b.w.notify(t.Name, tr("Connected"))
				case manager.TunnelStopped:
					b.w.notify(t.Name, tr("Disconnected"))
				}
			}
		}),
		manager.IPCClientRegisterTunnelsChange(func() { b.emit("tunnelsChange", nil) }),
		manager.IPCClientRegisterManagerStopping(func() { b.w.dispatch(b.w.quit) }),
		manager.IPCClientRegisterUpdateFound(func(s manager.UpdateState) {
			b.emit("updateFound", map[string]any{"state": int(s)})
		}),
		manager.IPCClientRegisterUpdateProgress(func(dp updater.DownloadProgress) {
			p := map[string]any{"activity": dp.Activity, "downloaded": dp.BytesDownloaded, "total": dp.BytesTotal, "complete": dp.Complete}
			if dp.Error != nil {
				p["error"] = dp.Error.Error()
			}
			b.emit("updateProgress", p)
		}),
		manager.IPCClientRegisterExtEvent(func(event string, payload []byte) {
			var v any
			json.Unmarshal(payload, &v)
			b.emit(event, v)
			switch event {
			case "settings":
				var s extras.Settings
				if json.Unmarshal(payload, &s) == nil {
					remoteControlEnabled.Store(s.RemoteControl)
				}
			case "health":
				if m, ok := v.(map[string]any); ok {
					b.w.notify(fmt.Sprint(m["tunnel"]), tr("Restarting unhealthy tunnel: ")+fmt.Sprint(m["message"]))
				}
			case "autotunnel":
				if m, ok := v.(map[string]any); ok {
					b.w.notify(tr("Auto-tunnel"), fmt.Sprintf("%s %s (%s)", tr(fmt.Sprint(m["action"])), m["tunnel"], m["reason"]))
				}
			}
		}),
	)
	go func() {
		var s extras.Settings
		if manager.IPCClientExt("settings.get", nil, &s) == nil {
			remoteControlEnabled.Store(s.RemoteControl)
		}
		tunnels, err := manager.IPCClientTunnels()
		if err == nil {
			b.stateMu.Lock()
			for _, t := range tunnels {
				if s, err := t.State(); err == nil {
					b.states[t.Name] = s
				}
			}
			b.stateMu.Unlock()
		}
		if gs, err := manager.IPCClientGlobalState(); err == nil {
			b.refreshTray(gs)
		}
	}()
}

func (b *bridge) unsubscribe() {
	for _, cb := range b.callbacks {
		cb.Unregister()
	}
	b.callbacks = nil
}

func (b *bridge) refreshTray(globalState manager.TunnelState) {
	b.stateMu.Lock()
	var active []string
	for name, s := range b.states {
		if s == manager.TunnelStarted {
			active = append(active, name)
		}
	}
	b.stateMu.Unlock()
	sort.Strings(active)
	b.w.dispatch(func() {
		if b.w.tray != nil {
			b.w.tray.setState(globalState, active)
		}
	})
}

func (w *window) notify(title, text string) {
	if !loadPrefs().Notifications {
		return
	}
	w.dispatch(func() {
		if w.tray != nil {
			w.tray.balloon(title, text)
		}
	})
}

func (b *bridge) appInfo(json.RawMessage) (any, error) {
	updateState, _ := manager.IPCClientUpdateState()
	return map[string]any{
		"version":     version.Number,
		"isAdmin":     IsAdmin,
		"official":    version.IsRunningOfficialVersion(),
		"updateState": int(updateState),
		"arch":        version.Arch(),
		"os":          version.OsName(),
	}, nil
}

func (b *bridge) appQuit(params json.RawMessage) (any, error) {
	p, err := decode[struct {
		StopTunnels bool `json:"stopTunnels"`
	}](params)
	if err != nil {
		return nil, err
	}
	if err := requireAdmin(); err != nil {
		return nil, err
	}
	b.quitManager = true
	b.stopTunnelsOnQuit = p.StopTunnels
	b.w.dispatch(b.w.quit)
	return nil, nil
}

func (b *bridge) appLegacyUI(json.RawMessage) (any, error) {
	if err := SetLegacyUI(true); err != nil {
		return nil, err
	}
	// The manager relaunches the UI process, which then picks the classic UI.
	b.w.dispatch(b.w.quit)
	return nil, nil
}

func (b *bridge) windowMinimize(json.RawMessage) (any, error) {
	b.w.dispatch(func() {
		if b.w.tray != nil {
			b.w.hide()
		}
	})
	return nil, nil
}

func (b *bridge) prefsSet(params json.RawMessage) (any, error) {
	p := loadPrefs()
	oldSize := p.WindowSize
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, err
	}
	// The window rectangle is owned by the host, not the page.
	current := loadPrefs()
	p.Window, p.WindowMaximized = current.Window, current.WindowMaximized
	if _, ok := sizePresets[p.WindowSize]; !ok && p.WindowSize != sizeMax {
		p.WindowSize = sizeRemember
	}
	if err := savePrefs(p); err != nil {
		return nil, err
	}
	// Resize after saving: the resize stores the new rectangle itself.
	if p.WindowSize != oldSize {
		size := p.WindowSize
		b.w.dispatch(func() { b.w.applySizePreset(size) })
	}
	setLanguage(p.Language)
	theme := p.effectiveTheme()
	b.w.dispatch(func() { b.w.applyTheme(theme) })
	return p, nil
}

type tunnelDTO struct {
	Name       string   `json:"name"`
	State      string   `json:"state"`
	Addresses  []string `json:"addresses"`
	DNS        []string `json:"dns"`
	Endpoints  []string `json:"endpoints"`
	AllowedIPs []string `json:"allowedIps"`
	ListenPort uint16   `json:"listenPort"`
	MTU        uint16   `json:"mtu"`
	Peers      int      `json:"peers"`
	AWG        bool     `json:"awg"`
	FullTunnel bool     `json:"fullTunnel"`
	PublicKey  string   `json:"publicKey"`
	Error      string   `json:"error,omitempty"`
}

func summarize(name string, c *conf.Config, state manager.TunnelState) tunnelDTO {
	d := tunnelDTO{Name: name, State: stateName(state)}
	if c == nil {
		return d
	}
	for _, a := range c.Interface.Addresses {
		d.Addresses = append(d.Addresses, a.String())
	}
	for _, ip := range c.Interface.DNS {
		d.DNS = append(d.DNS, ip.String())
	}
	d.DNS = append(d.DNS, c.Interface.DNSSearch...)
	d.ListenPort = c.Interface.ListenPort
	d.MTU = c.Interface.MTU
	d.Peers = len(c.Peers)
	if !c.Interface.PrivateKey.IsZero() {
		d.PublicKey = c.Interface.PrivateKey.Public().String()
	}
	i := c.Interface
	d.AWG = i.JunkPacketCount != 0 || i.InitPacketJunkSize != 0 || i.ResponsePacketJunkSize != 0 || i.InitPacketMagicHeader != "" || len(i.IPackets) > 0
	for _, p := range c.Peers {
		if !p.Endpoint.IsEmpty() {
			d.Endpoints = append(d.Endpoints, p.Endpoint.String())
		}
		for _, a := range p.AllowedIPs {
			d.AllowedIPs = append(d.AllowedIPs, a.String())
			if a.Cidr == 0 {
				d.FullTunnel = true
			}
		}
	}
	return d
}

func (b *bridge) tunnelsList(json.RawMessage) (any, error) {
	tunnels, err := manager.IPCClientTunnels()
	if err != nil {
		return nil, err
	}
	sort.Slice(tunnels, func(i, j int) bool { return conf.TunnelNameIsLess(tunnels[i].Name, tunnels[j].Name) })
	out := make([]tunnelDTO, 0, len(tunnels))
	for _, t := range tunnels {
		state, _ := t.State()
		c, err := t.StoredConfig()
		d := summarize(t.Name, &c, state)
		if err != nil {
			d.Error = err.Error()
		}
		out = append(out, d)
	}
	return out, nil
}

func (b *bridge) tunnelsGet(params json.RawMessage) (any, error) {
	p, err := decode[nameParams](params)
	if err != nil {
		return nil, err
	}
	t := manager.Tunnel{Name: p.Name}
	c, err := t.StoredConfig()
	if err != nil {
		return nil, err
	}
	state, _ := t.State()
	return map[string]any{
		"summary": summarize(p.Name, &c, state),
		"text":    c.ToWgQuick(),
	}, nil
}

type peerStats struct {
	PublicKey     string `json:"publicKey"`
	Endpoint      string `json:"endpoint"`
	RxBytes       uint64 `json:"rx"`
	TxBytes       uint64 `json:"tx"`
	LastHandshake int64  `json:"lastHandshake"`
	Keepalive     string `json:"keepalive,omitempty"`
}

func (b *bridge) tunnelsRuntime(params json.RawMessage) (any, error) {
	p, err := decode[nameParams](params)
	if err != nil {
		return nil, err
	}
	t := manager.Tunnel{Name: p.Name}
	c, err := t.RuntimeConfig()
	if err != nil {
		return nil, err
	}
	var rx, tx uint64
	var latest int64
	peers := make([]peerStats, 0, len(c.Peers))
	for _, peer := range c.Peers {
		ps := peerStats{
			PublicKey: peer.PublicKey.String(),
			RxBytes:   uint64(peer.RxBytes),
			TxBytes:   uint64(peer.TxBytes),
			Keepalive: peer.PersistentKeepalive,
		}
		if !peer.Endpoint.IsEmpty() {
			ps.Endpoint = peer.Endpoint.String()
		}
		if !peer.LastHandshakeTime.IsEmpty() {
			ps.LastHandshake = time.Unix(0, 0).Add(time.Duration(peer.LastHandshakeTime)).UnixMilli()
			latest = max(latest, ps.LastHandshake)
		}
		rx += ps.RxBytes
		tx += ps.TxBytes
		peers = append(peers, ps)
	}
	return map[string]any{
		"rx":            rx,
		"tx":            tx,
		"lastHandshake": latest,
		"listenPort":    c.Interface.ListenPort,
		"peers":         peers,
		"now":           time.Now().UnixMilli(),
	}, nil
}

func (b *bridge) tunnelAction(action string) handler {
	return func(params json.RawMessage) (any, error) {
		p, err := decode[nameParams](params)
		if err != nil {
			return nil, err
		}
		t := manager.Tunnel{Name: p.Name}
		switch action {
		case "start":
			return nil, t.Start()
		case "stop":
			return nil, t.Stop()
		}
		_, err = t.Toggle()
		return nil, err
	}
}

func (b *bridge) tunnelsRestart(params json.RawMessage) (any, error) {
	p, err := decode[nameParams](params)
	if err != nil {
		return nil, err
	}
	return nil, manager.IPCClientExt("tunnel.restart", manager.TunnelRef{Tunnel: p.Name}, nil)
}

type saveParams struct {
	Name         string `json:"name"`
	OriginalName string `json:"originalName"`
	Text         string `json:"text"`
}

func parseConfig(name, text string) (*conf.Config, error) {
	if !conf.TunnelNameIsValid(name) {
		return nil, errors.New(tr("Invalid name: use up to 32 letters, digits and _=+.- characters."))
	}
	return conf.FromWgQuickWithUnknownEncoding(text, name)
}

func (b *bridge) tunnelsValidate(params json.RawMessage) (any, error) {
	p, err := decode[saveParams](params)
	if err != nil {
		return nil, err
	}
	c, err := parseConfig(p.Name, p.Text)
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}, nil
	}
	return map[string]any{"ok": true, "summary": summarize(p.Name, c, manager.TunnelStopped)}, nil
}

func (b *bridge) tunnelsSave(params json.RawMessage) (any, error) {
	if err := requireAdmin(); err != nil {
		return nil, err
	}
	p, err := decode[saveParams](params)
	if err != nil {
		return nil, err
	}
	c, err := parseConfig(p.Name, p.Text)
	if err != nil {
		return nil, err
	}
	wasRunning := false
	if p.OriginalName != "" {
		ot := manager.Tunnel{Name: p.OriginalName}
		if s, err := ot.State(); err == nil && (s == manager.TunnelStarted || s == manager.TunnelStarting) {
			wasRunning = true
		}
		if p.OriginalName != p.Name {
			if wasRunning {
				ot.Stop()
				ot.WaitForStop()
			}
			if err := manager.IPCClientExt("tunnel.rename", manager.RenameRequest{From: p.OriginalName, To: p.Name}, nil); err != nil {
				return nil, err
			}
		}
	} else if existing, err := (&manager.Tunnel{Name: p.Name}).StoredConfig(); err == nil && existing.Name != "" {
		return nil, fmt.Errorf(tr("A tunnel named %s already exists."), p.Name)
	}
	if _, err := manager.IPCClientNewTunnel(c); err != nil {
		return nil, err
	}
	if wasRunning {
		go func() {
			if err := manager.IPCClientExt("tunnel.restart", manager.TunnelRef{Tunnel: p.Name}, nil); err != nil {
				b.w.notify(p.Name, err.Error())
			}
		}()
	}
	return map[string]any{"name": p.Name, "restarted": wasRunning}, nil
}

func (b *bridge) tunnelsDelete(params json.RawMessage) (any, error) {
	if err := requireAdmin(); err != nil {
		return nil, err
	}
	p, err := decode[struct {
		Names []string `json:"names"`
	}](params)
	if err != nil {
		return nil, err
	}
	var errs []error
	for _, n := range p.Names {
		if err := (&manager.Tunnel{Name: n}).Delete(); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", n, err))
		}
	}
	return nil, errors.Join(errs...)
}

func (b *bridge) tunnelsRename(params json.RawMessage) (any, error) {
	if err := requireAdmin(); err != nil {
		return nil, err
	}
	p, err := decode[manager.RenameRequest](params)
	if err != nil {
		return nil, err
	}
	if !conf.TunnelNameIsValid(p.To) {
		return nil, errors.New(tr("Invalid name: use up to 32 letters, digits and _=+.- characters."))
	}
	return nil, manager.IPCClientExt("tunnel.rename", p, nil)
}

// uniqueName returns name or name-2, name-3… whichever is free.
func uniqueName(name string, taken map[string]bool) string {
	base := importer.SanitizeName(name)
	candidate := base
	for i := 2; taken[strings.ToLower(candidate)] || !conf.TunnelNameIsValid(candidate); i++ {
		suffix := fmt.Sprintf("-%d", i)
		trimmed := base
		if len(trimmed)+len(suffix) > 32 {
			trimmed = trimmed[:32-len(suffix)]
		}
		candidate = trimmed + suffix
		if i > 999 {
			break
		}
	}
	taken[strings.ToLower(candidate)] = true
	return candidate
}

func existingNames() map[string]bool {
	taken := make(map[string]bool)
	if tunnels, err := manager.IPCClientTunnels(); err == nil {
		for _, t := range tunnels {
			taken[strings.ToLower(t.Name)] = true
		}
	}
	return taken
}

func (b *bridge) tunnelsDuplicate(params json.RawMessage) (any, error) {
	if err := requireAdmin(); err != nil {
		return nil, err
	}
	p, err := decode[nameParams](params)
	if err != nil {
		return nil, err
	}
	src := manager.Tunnel{Name: p.Name}
	c, err := src.StoredConfig()
	if err != nil {
		return nil, err
	}
	c.Name = uniqueName(p.Name+"-copy", existingNames())
	if _, err := manager.IPCClientNewTunnel(&c); err != nil {
		return nil, err
	}
	if split, err := b.splitGet(params); err == nil {
		if cfg, ok := split.(*splittunnel.Config); ok && cfg.Active() {
			manager.IPCClientExt("split.set", manager.SplitSetRequest{Tunnel: c.Name, Config: cfg}, nil)
		}
	}
	return map[string]any{"name": c.Name}, nil
}

func (b *bridge) tunnelsTemplate(json.RawMessage) (any, error) {
	k, err := conf.NewPrivateKey()
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"name":      uniqueName("new-tunnel", existingNames()),
		"text":      "[Interface]\nPrivateKey = " + k.String() + "\nAddress = \nDNS = 1.1.1.1\n\n[Peer]\nPublicKey = \nAllowedIPs = 0.0.0.0/0, ::/0\nEndpoint = \n",
		"publicKey": k.Public().String(),
	}, nil
}

type importResult struct {
	Imported []string `json:"imported"`
	Errors   []string `json:"errors"`
}

// createImported validates and creates imported configs with unique names.
func createImported(configs []importer.Config) importResult {
	res := importResult{Imported: []string{}, Errors: []string{}}
	taken := existingNames()
	for _, ic := range configs {
		name := uniqueName(ic.Name, taken)
		c, err := conf.FromWgQuickWithUnknownEncoding(ic.Text, name)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", ic.Name, err))
			delete(taken, strings.ToLower(name))
			continue
		}
		if _, err := manager.IPCClientNewTunnel(c); err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		res.Imported = append(res.Imported, name)
	}
	return res
}

func (b *bridge) importText(params json.RawMessage) (any, error) {
	if err := requireAdmin(); err != nil {
		return nil, err
	}
	p, err := decode[struct {
		Name string `json:"name"`
		Text string `json:"text"`
	}](params)
	if err != nil {
		return nil, err
	}
	name := p.Name
	if name == "" {
		name = "imported"
	}
	configs, err := importer.FromText(name, p.Text)
	if err != nil {
		return nil, err
	}
	return createImported(configs), nil
}

func (b *bridge) importData(params json.RawMessage) (any, error) {
	if err := requireAdmin(); err != nil {
		return nil, err
	}
	p, err := decode[struct {
		Files []struct {
			Name string `json:"name"`
			Data string `json:"data"` // base64
		} `json:"files"`
	}](params)
	if err != nil {
		return nil, err
	}
	var all []importer.Config
	res := importResult{Imported: []string{}, Errors: []string{}}
	for _, f := range p.Files {
		data, err := base64.StdEncoding.DecodeString(f.Data)
		if err != nil {
			res.Errors = append(res.Errors, f.Name+": "+err.Error())
			continue
		}
		cs, err := importer.FromBytes(f.Name, data)
		if err != nil {
			res.Errors = append(res.Errors, f.Name+": "+err.Error())
			continue
		}
		all = append(all, cs...)
	}
	r := createImported(all)
	res.Imported = append(res.Imported, r.Imported...)
	res.Errors = append(res.Errors, r.Errors...)
	return res, nil
}

func (b *bridge) importFiles(json.RawMessage) (any, error) {
	if err := requireAdmin(); err != nil {
		return nil, err
	}
	var paths []string
	b.w.dispatchSync(func() {
		paths = openFileDialog(b.w.hwnd, tr("Import tunnels"), tr("Tunnel files")+" (*.conf, *.zip, *.txt, *.vpn)\x00*.conf;*.zip;*.txt;*.vpn\x00"+tr("All files")+"\x00*.*\x00", true)
	})
	if len(paths) == 0 {
		return nil, nil
	}
	res := importResult{Imported: []string{}, Errors: []string{}}
	var all []importer.Config
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			res.Errors = append(res.Errors, err.Error())
			continue
		}
		cs, err := importer.FromBytes(path, data)
		if err != nil {
			res.Errors = append(res.Errors, path+": "+err.Error())
			continue
		}
		all = append(all, cs...)
	}
	r := createImported(all)
	res.Imported = append(res.Imported, r.Imported...)
	res.Errors = append(res.Errors, r.Errors...)
	return res, nil
}

func storedTexts(names []string) ([]importer.Config, error) {
	var out []importer.Config
	for _, n := range names {
		c, err := (&manager.Tunnel{Name: n}).StoredConfig()
		if err != nil {
			return nil, err
		}
		out = append(out, importer.Config{Name: n, Text: c.ToWgQuick()})
	}
	return out, nil
}

func (b *bridge) exportZip(params json.RawMessage) (any, error) {
	if err := requireAdmin(); err != nil {
		return nil, err
	}
	p, err := decode[struct {
		Names []string `json:"names"`
	}](params)
	if err != nil {
		return nil, err
	}
	if len(p.Names) == 0 {
		tunnels, err := manager.IPCClientTunnels()
		if err != nil {
			return nil, err
		}
		for _, t := range tunnels {
			p.Names = append(p.Names, t.Name)
		}
	}
	configs, err := storedTexts(p.Names)
	if err != nil {
		return nil, err
	}
	data, err := importer.ToZip(configs)
	if err != nil {
		return nil, err
	}
	var path string
	b.w.dispatchSync(func() {
		path = saveFileDialog(b.w.hwnd, tr("Export tunnels"), "ZIP (*.zip)\x00*.zip\x00", "tunnels.zip", "zip")
	})
	if path == "" {
		return nil, nil
	}
	return map[string]string{"path": path}, os.WriteFile(path, data, 0600)
}

func (b *bridge) exportConf(params json.RawMessage) (any, error) {
	if err := requireAdmin(); err != nil {
		return nil, err
	}
	p, err := decode[nameParams](params)
	if err != nil {
		return nil, err
	}
	configs, err := storedTexts([]string{p.Name})
	if err != nil {
		return nil, err
	}
	var path string
	b.w.dispatchSync(func() {
		path = saveFileDialog(b.w.hwnd, tr("Export tunnel"), tr("Tunnel files")+" (*.conf)\x00*.conf\x00", p.Name+".conf", "conf")
	})
	if path == "" {
		return nil, nil
	}
	return map[string]string{"path": path}, os.WriteFile(path, []byte(configs[0].Text), 0600)
}

func (b *bridge) shareKey(params json.RawMessage) (any, error) {
	if err := requireAdmin(); err != nil {
		return nil, err
	}
	p, err := decode[nameParams](params)
	if err != nil {
		return nil, err
	}
	configs, err := storedTexts([]string{p.Name})
	if err != nil {
		return nil, err
	}
	key, err := importer.EncodeVPNKey(p.Name, configs[0].Text, "")
	if err != nil {
		return nil, err
	}
	return map[string]string{"key": key, "text": configs[0].Text}, nil
}

func (b *bridge) keysGenerate(json.RawMessage) (any, error) {
	k, err := conf.NewPrivateKey()
	if err != nil {
		return nil, err
	}
	return map[string]string{"private": k.String(), "public": k.Public().String()}, nil
}

func (b *bridge) keysPublic(params json.RawMessage) (any, error) {
	p, err := decode[struct {
		Private string `json:"private"`
	}](params)
	if err != nil {
		return nil, err
	}
	k, err := conf.NewPrivateKeyFromString(strings.TrimSpace(p.Private))
	if err != nil {
		return nil, err
	}
	return map[string]string{"public": k.Public().String()}, nil
}

func (b *bridge) splitGet(params json.RawMessage) (any, error) {
	p, err := decode[nameParams](params)
	if err != nil {
		return nil, err
	}
	var c splittunnel.Config
	if err := manager.IPCClientExt("split.get", manager.TunnelRef{Tunnel: p.Name}, &c); err != nil {
		return nil, err
	}
	if c.Mode == "" {
		c.Mode = splittunnel.ModeOff
	}
	return &c, nil
}

type splitParams struct {
	Name   string              `json:"name"`
	Config *splittunnel.Config `json:"config"`
}

func (b *bridge) splitValidate(params json.RawMessage) (any, error) {
	p, err := decode[splitParams](params)
	if err != nil {
		return nil, err
	}
	if p.Config == nil {
		return nil, errors.New("missing configuration")
	}
	if err := p.Config.Validate(); err != nil {
		return map[string]any{"ok": false, "error": err.Error()}, nil
	}
	return map[string]any{"ok": true}, nil
}

func (b *bridge) splitSet(params json.RawMessage) (any, error) {
	if err := requireAdmin(); err != nil {
		return nil, err
	}
	p, err := decode[splitParams](params)
	if err != nil {
		return nil, err
	}
	var resp manager.SplitSetResponse
	if err := manager.IPCClientExt("split.set", manager.SplitSetRequest{Tunnel: p.Name, Config: p.Config}, &resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func (b *bridge) settingsGet(json.RawMessage) (any, error) {
	var s extras.Settings
	if err := manager.IPCClientExt("settings.get", nil, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func (b *bridge) settingsSet(params json.RawMessage) (any, error) {
	if err := requireAdmin(); err != nil {
		return nil, err
	}
	var s extras.Settings
	resp, err := manager.IPCClientExtRaw("settings.set", params)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(resp, &s); err != nil {
		return nil, err
	}
	remoteControlEnabled.Store(s.RemoteControl)
	return &s, nil
}

func (b *bridge) statusGet(json.RawMessage) (any, error) {
	var s manager.StatusResponse
	if err := manager.IPCClientExt("status", nil, &s); err != nil {
		return nil, err
	}
	return s, nil
}

func (b *bridge) logFollow(params json.RawMessage) (any, error) {
	p, err := decode[struct {
		Cursor *uint32 `json:"cursor"`
	}](params)
	if err != nil {
		return nil, err
	}
	cursor := ringlogger.CursorAll
	if p.Cursor != nil {
		cursor = *p.Cursor
	}
	lines, next := ringlogger.Global.FollowFromCursor(cursor)
	type line struct {
		T    int64  `json:"t"`
		Text string `json:"text"`
	}
	out := make([]line, 0, len(lines))
	for _, l := range lines {
		out = append(out, line{T: l.Stamp.UnixMilli(), Text: l.Line})
	}
	return map[string]any{"lines": out, "cursor": next}, nil
}

func (b *bridge) logSave(json.RawMessage) (any, error) {
	var path string
	b.w.dispatchSync(func() {
		path = saveFileDialog(b.w.hwnd, tr("Save log"), tr("Text files")+" (*.txt)\x00*.txt\x00", "betteramnezia-log-"+time.Now().Format("2006-01-02T150405")+".txt", "txt")
	})
	if path == "" {
		return nil, nil
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if _, err := ringlogger.Global.WriteTo(f); err != nil {
		return nil, err
	}
	return map[string]string{"path": path}, nil
}

func (b *bridge) updateState(json.RawMessage) (any, error) {
	s, err := manager.IPCClientUpdateState()
	return map[string]int{"state": int(s)}, err
}

func (b *bridge) updateStart(json.RawMessage) (any, error) {
	if err := requireAdmin(); err != nil {
		return nil, err
	}
	return nil, manager.IPCClientUpdate()
}
