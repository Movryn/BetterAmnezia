/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2026 BetterAmnezia contributors. All Rights Reserved.
 */

package manager

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"golang.org/x/sys/windows"

	"github.com/amnezia-vpn/amneziawg-windows/v3/conf"

	"github.com/amnezia-vpn/amneziawg-windows-client/extras"
	"github.com/amnezia-vpn/amneziawg-windows-client/splittunnel"
)

// The extension channel carries BetterAmnezia features over the existing
// manager IPC: an operation name plus a JSON payload in each direction.

type TunnelRef struct {
	Tunnel string `json:"tunnel"`
}

type SplitSetRequest struct {
	Tunnel string              `json:"tunnel"`
	Config *splittunnel.Config `json:"config"`
}

type SplitSetResponse struct {
	Restarted bool `json:"restarted"`
}

type RenameRequest struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type StatusResponse struct {
	Lockdown        bool                    `json:"lockdown"`
	Network         extras.NetworkState     `json:"network"`
	Health          map[string]HealthStatus `json:"health"`
	DriverAvailable bool                    `json:"driverAvailable"`
}

var errUnknownOp = errors.New("Unknown operation")

func (s *ManagerService) requireAdmin() error {
	if s.elevatedToken == 0 {
		return windows.ERROR_ACCESS_DENIED
	}
	return nil
}

// Ext dispatches an extension operation.
func (s *ManagerService) Ext(op string, payload []byte) ([]byte, error) {
	switch op {
	case "settings.get":
		return json.Marshal(currentSettings())
	case "settings.set":
		if err := s.requireAdmin(); err != nil {
			return nil, err
		}
		settings, err := extras.ParseSettings(payload)
		if err != nil {
			return nil, err
		}
		if err := applySettings(settings); err != nil {
			return nil, err
		}
		return json.Marshal(currentSettings())
	case "split.get":
		var req TunnelRef
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, err
		}
		c, err := extras.LoadSplit(req.Tunnel)
		if err != nil {
			return nil, err
		}
		if s.elevatedToken == 0 {
			c.Proxy.Password = ""
		}
		return json.Marshal(c)
	case "split.set":
		if err := s.requireAdmin(); err != nil {
			return nil, err
		}
		var req SplitSetRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, err
		}
		if req.Config == nil {
			return nil, errors.New("Missing configuration")
		}
		if req.Config.Mode == "" {
			req.Config.Mode = splittunnel.ModeOff
		}
		if err := extras.SaveSplit(req.Tunnel, req.Config); err != nil {
			return nil, err
		}
		resp := SplitSetResponse{}
		if state, err := s.State(req.Tunnel); err == nil && state == TunnelStarted {
			resp.Restarted = true
			go func() {
				if err := restartTunnel(s, req.Tunnel, "split tunneling rules changed"); err != nil {
					log.Printf("[%s] Unable to restart tunnel: %v", req.Tunnel, err)
				}
			}()
		}
		return json.Marshal(resp)
	case "tunnel.rename":
		if err := s.requireAdmin(); err != nil {
			return nil, err
		}
		var req RenameRequest
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, err
		}
		return nil, s.rename(req.From, req.To)
	case "tunnel.restart":
		var req TunnelRef
		if err := json.Unmarshal(payload, &req); err != nil {
			return nil, err
		}
		return nil, restartTunnel(s, req.Tunnel, "requested by user")
	case "status":
		return json.Marshal(StatusResponse{
			Lockdown:        lockdownActive(),
			Network:         currentNetwork(),
			Health:          healthSnapshot(),
			DriverAvailable: splitDriverAvailable(),
		})
	}
	return nil, fmt.Errorf("%w: %s", errUnknownOp, op)
}

func (s *ManagerService) rename(from, to string) error {
	if from == to {
		return nil
	}
	if !conf.TunnelNameIsValid(to) {
		return errors.New("Tunnel name is not valid")
	}
	if _, err := conf.LoadFromName(to); err == nil {
		return errors.New("Another tunnel already exists with that name")
	}
	c, err := conf.LoadFromName(from)
	if err != nil {
		return err
	}
	wasRunning := false
	if state, err := s.State(from); err == nil && (state == TunnelStarted || state == TunnelStarting) {
		wasRunning = true
		if err := s.Stop(from); err != nil {
			return err
		}
		s.WaitForStop(from)
	}
	c.Name = to
	if err := c.Save(false); err != nil {
		return err
	}
	if err := conf.DeleteName(from); err != nil {
		return err
	}
	if err := extras.RenameTunnel(from, to); err != nil {
		log.Printf("[%s] Unable to move split tunneling rules: %v", to, err)
	}
	if wasRunning {
		return s.Start(to)
	}
	return nil
}

// IPCServerNotifyExt sends an extension event to every UI.
func IPCServerNotifyExt(event string, v any) {
	payload, err := json.Marshal(v)
	if err != nil {
		return
	}
	notifyAll(ExtNotificationType, false, event, payload)
}

type ExtEventCallback struct {
	cb func(event string, payload []byte)
}

var extEventCallbacks = make(map[*ExtEventCallback]bool)

// IPCClientRegisterExtEvent subscribes to extension events.
func IPCClientRegisterExtEvent(cb func(event string, payload []byte)) *ExtEventCallback {
	s := &ExtEventCallback{cb}
	extEventCallbacks[s] = true
	return s
}

func (cb *ExtEventCallback) Unregister() {
	delete(extEventCallbacks, cb)
}

// IPCClientExtRaw performs an extension call with raw JSON.
func IPCClientExtRaw(op string, payload []byte) (resp []byte, err error) {
	rpcMutex.Lock()
	defer rpcMutex.Unlock()

	err = rpcEncoder.Encode(ExtMethodType)
	if err != nil {
		return
	}
	err = rpcEncoder.Encode(op)
	if err != nil {
		return
	}
	if payload == nil {
		payload = []byte{}
	}
	err = rpcEncoder.Encode(payload)
	if err != nil {
		return
	}
	err = rpcDecoder.Decode(&resp)
	if err != nil {
		return
	}
	err = rpcDecodeError()
	return
}

// IPCClientExt performs an extension call, encoding req and decoding into
// resp when it is not nil.
func IPCClientExt(op string, req, resp any) error {
	var payload []byte
	if req != nil {
		var err error
		payload, err = json.Marshal(req)
		if err != nil {
			return err
		}
	}
	out, err := IPCClientExtRaw(op, payload)
	if err != nil {
		return err
	}
	if resp != nil && len(out) > 0 {
		return json.Unmarshal(out, resp)
	}
	return nil
}
