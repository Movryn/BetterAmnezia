# BetterAmnezia

A better [AmneziaWG](https://amnezia.org/) client for Windows. It keeps the solid core of the official AmneziaWG/WireGuard for Windows client (Wintun, the tunnel service, the encrypted configuration store, the kill switch) and adds a modern interface and the features people kept asking for, many of them modeled on [WG Tunnel](https://github.com/wgtunnel/android) for Android.

## Features

**Interface**
- New WebView2 interface with System, Light, Dark and true-black AMOLED themes, accent colours, compact mode, English and Russian.
- Live throughput chart, transfer totals, handshake age, endpoint and per-peer details.
- Editor with syntax highlighting for WireGuard and AmneziaWG parameters, live validation, key generation, a kill switch toggle and a generator for AmneziaWG obfuscation parameters.
- Import `.conf`, `.zip` and AmneziaVPN `vpn://` keys from files, the clipboard or by dropping files on the window. Export single tunnels or everything as a ZIP. Share as a `vpn://` key or QR code for phones.
- Rename, duplicate, delete and restart tunnels; tray menu with per-tunnel toggles and notifications.
- The classic interface is still available (Settings → Advanced) and is used automatically when the WebView2 runtime is missing, e.g. on Windows 7.

**Split tunneling** (per tunnel; see [`docs/splittunnel.md`](docs/splittunnel.md))
- *Exclude* mode (everything through the VPN except…) and *include* mode (only … through the VPN).
- IP addresses and CIDR ranges, IPv4 and IPv6.
- Domains with wildcards: `example.com`, `*.example.com`, `*example.com`, `api.*.example.com`, `=exact.example.com`. One-click presets for YouTube, Discord, Telegram, Instagram, X, ChatGPT, Claude, Netflix, Spotify, Twitch, Steam and LinkedIn.
- Applications and whole folders: *VPN only* (per-app kill switch), *Block*, and *Bypass VPN* (with the optional split tunnel driver).
- Keep the LAN outside the VPN.
- DNS forwarder with plain DNS, DNS over TLS and DNS over HTTPS upstreams and leak blocking.
- Local SOCKS5 and HTTP proxy bound to the tunnel, with optional authentication.

**Automation and reliability**
- Auto-tunneling: connect or disconnect depending on the network: trusted Wi-Fi networks, untrusted Wi-Fi, Ethernet, other networks, and per-SSID rules with wildcards.
- Health monitor: restarts a tunnel that keeps sending without receiving anything or completing a handshake, optionally also based on pings through the tunnel.
- Dynamic DNS: re-resolves endpoint host names and moves running tunnels to the new address without reconnecting.
- Deferred endpoint bootstrapping: when an endpoint host name cannot be resolved at connect time (no network yet, DNS blocked), the tunnel still comes up and the endpoint is filled in as soon as it resolves.
- Lockdown mode: blocks all traffic while no tunnel is connected, optionally allowing the LAN.
- Remote control for scripts and automation tools, without elevation (enable it under Settings → Remote control):

  ```text
  amneziawg.exe /connect <tunnel>
  amneziawg.exe /disconnect <tunnel>
  amneziawg.exe /toggle <tunnel>
  amneziawg.exe /disconnectall
  ```

  Exit codes: 0 success, 1 failure, 2 remote control disabled, 3 BetterAmnezia not running.

## Requirements

Windows 10 or 11 with the [WebView2 runtime](https://developer.microsoft.com/microsoft-edge/webview2/) (preinstalled on current Windows). The classic interface works without it.

## Building

See [`docs/buildrun.md`](docs/buildrun.md). In short, on Windows run `build.bat`; on Linux run `make`.

The interface lives in [`webui/assets`](webui/assets) as plain HTML, CSS and JavaScript and is embedded into the binary. To work on it without Windows, open [`webui/dev/index.html`](webui/dev/index.html) in a browser; it uses a mock of the Go bridge. [`webui/dev/flows.test.js`](webui/dev/flows.test.js) runs the main UI flows with Playwright.

Go packages with platform independent logic have unit tests that run anywhere:

```text
go test ./splittunnel ./extras ./importer
```

## Documentation

- [`splittunnel.md`](docs/splittunnel.md) &ndash; How split tunneling, the DNS forwarder, app rules and the local proxy work.
- [`adminregistry.md`](docs/adminregistry.md) &ndash; Registry keys for administrators.
- [`attacksurface.md`](docs/attacksurface.md) &ndash; Security design of the components.
- [`buildrun.md`](docs/buildrun.md) &ndash; Building, localizing, running and developing.
- [`enterprise.md`](docs/enterprise.md) &ndash; Enterprise deployment notes.
- [`netquirk.md`](docs/netquirk.md) &ndash; Networking quirks and kill switch semantics.
- [`userregistry.md`](docs/userregistry.md) &ndash; Registry keys for users.

## License

This repository is MIT-licensed. The optional split tunnel driver is a separate program by Mullvad VPN AB, licensed under GPL-3.0-or-later or MPL-2.0, and is not part of this repository.

```text
Copyright (C) 2018-2022 WireGuard LLC. All Rights Reserved.

Permission is hereby granted, free of charge, to any person obtaining a
copy of this software and associated documentation files (the "Software"),
to deal in the Software without restriction, including without limitation
the rights to use, copy, modify, merge, publish, distribute, sublicense,
and/or sell copies of the Software, and to permit persons to whom the
Software is furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in
all copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER
DEALINGS IN THE SOFTWARE.
```
