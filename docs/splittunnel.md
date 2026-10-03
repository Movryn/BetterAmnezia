# Split Tunneling

Split tunneling decides which traffic uses a tunnel. Rules are stored per tunnel and are applied by the tunnel service when the tunnel connects. Saving new rules for a connected tunnel reconnects it.

Rules live in `%ProgramFiles%\AmneziaWG\Data\Extras\<tunnel>.split.json`, which only SYSTEM and Administrators can read. The file is JSON and can be edited by hand while the tunnel is stopped:

```json
{
  "mode": "exclude",
  "ips": ["192.168.1.0/24", "1.2.3.4"],
  "domains": ["*.youtube.com", "*domain.com", "=bank.example"],
  "apps": [
    { "path": "C:\\Games\\game.exe", "action": "bypass" },
    { "path": "C:\\Program Files\\qBittorrent", "folder": true, "action": "vpnonly" }
  ],
  "allowLan": true,
  "dns": {
    "upstreams": ["https://cloudflare-dns.com/dns-query#1.1.1.1,1.0.0.1"],
    "blockOtherDns": true,
    "excludedViaSystemDns": false
  },
  "proxy": { "socks5": true, "socksPort": 1080, "http": false }
}
```

## Modes

| Mode      | Effect |
|-----------|--------|
| `off`     | Routes exactly what `AllowedIPs` says. App rules, DNS and proxy settings still apply. |
| `exclude` | Everything the tunnel normally carries goes through it, except the listed addresses and domains. They are routed through the regular default gateway, and the kill switch of full-tunnel configurations lets them pass. |
| `include` | Only the listed addresses and domains go through the tunnel. The tunnel no longer takes the default route and the kill switch is off. The peer's `AllowedIPs` must still cover the included destinations. |

`allowLan` (exclude mode) keeps private ranges (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`, link-local, multicast and their IPv6 equivalents) outside the tunnel and lets them through the kill switch.

## Addresses

Single addresses (`1.1.1.1`, `2001:db8::1`) and CIDR ranges (`10.0.0.0/8`, `2001:db8::/32`). Overlapping entries are merged.

## Domains

| Rule              | Matches |
|-------------------|---------|
| `example.com`     | `example.com` and every subdomain |
| `*.example.com`   | `example.com` and every subdomain |
| `=example.com`    | only `example.com` |
| `*example.com`    | every name ending in `example.com`, e.g. `myexample.com`, `a.example.com` |
| `api.*.example.com` | glob; `*` matches any characters, dots included |

Domain rules work through a DNS forwarder that the tunnel service runs on a loopback address (`127.0.0.53`, falling back to other `127.x` addresses). The tunnel's DNS setting points Windows at it. When an answer for a matching name (or any CNAME in the answer) comes back, the forwarder installs host routes for the returned addresses *before* it replies, so the first connection already takes the right path. Learned routes stay until the tunnel disconnects; at most 16384 addresses are kept, the least recently seen are dropped first.

While domain rules exist, plain DNS to any other server is blocked, so applications cannot bypass the forwarder with their own resolver. Browsers using their own encrypted DNS ("secure DNS" in Chrome, Edge and Firefox) are not affected by this; turn that feature off or use the local proxy for them.

In include mode, names that do not match are resolved by the regular network's DNS servers (unless custom upstreams are set), and AAAA answers for matched names are suppressed when the tunnel has no IPv6 address so that applications cannot leave over IPv6.

## DNS upstreams

The forwarder uses the tunnel's `DNS` servers unless upstreams are configured:

| Form | Transport |
|------|-----------|
| `1.1.1.1`, `1.1.1.1:5353`, `[2606:4700::1111]:53` | plain DNS (UDP, TCP on truncation) |
| `tls://dns.quad9.net#9.9.9.9` | DNS over TLS (port 853) |
| `https://cloudflare-dns.com/dns-query#1.1.1.1` | DNS over HTTPS |

The part after `#` lists bootstrap addresses; without it the host name is resolved once when the tunnel starts. `dns.force` runs the forwarder even without domain rules (to use encrypted DNS through the tunnel), and `dns.blockOtherDns` blocks other DNS servers in every mode. `dns.excludedViaSystemDns` resolves excluded names with the regular network's DNS servers, which gives CDNs a better idea of where you are.

## Applications

| Action    | Effect |
|-----------|--------|
| `vpnonly` | The application may only use the tunnel interface (loopback stays allowed). A per-app kill switch. |
| `block`   | The application may not use the network at all while the tunnel is up. |
| `bypass`  | The application's traffic avoids the tunnel. Requires the split tunnel driver, see below. |

A rule with `"folder": true` applies to every `.exe` inside the folder and its subfolders (up to 4096 programs), as found when the tunnel connects. `"enabled": false` keeps a rule without applying it.

`vpnonly` and `block` are WFP filters keyed on the executable path and need nothing else.

### App bypass and the split tunnel driver

Windows routes by destination, not by program. Keeping a single application out of a VPN requires a kernel driver that redirects that program's sockets. BetterAmnezia uses the open source [Mullvad split tunnel driver](https://github.com/mullvad/win-split-tunnel) (dual licensed GPL-3.0-or-later / MPL-2.0). It is **not** bundled. To enable `bypass` rules:

1. Obtain a signed `mullvad-split-tunnel.sys` that matches your architecture (x64 or ARM64). The Mullvad VPN app ships one in its installation directory.
2. Put it next to `amneziawg.exe`, or choose it under *Settings → Split tunnel driver*.

The tunnel service installs it as the kernel service `AmneziaWGSplitTunnel` when a tunnel with bypass rules connects, registers the process tree and the tunnel/internet addresses, and resets it when the tunnel disconnects. Driver versions 1.3 and later place their filters in the tunnel's own WFP sublayer, so bypassed apps also pass the kill switch; older drivers work only for tunnels without the kill switch. Do not use it while the Mullvad VPN app itself is running.

Without the driver, `bypass` rules are logged and ignored; the UI shows a warning.

## Local proxy

The tunnel can expose itself as a SOCKS5 (default port 1080) and/or HTTP proxy (default port 8118, supports `CONNECT`), listening on `127.0.0.1` or, if chosen, on all interfaces. Connections made through the proxy are bound to the tunnel address and host names are resolved through the tunnel's DNS, so applications configured to use the proxy go through the tunnel even when the rest of the system does not (for example in include mode). Optional username/password authentication applies to both.

In include mode a default route with metric 4096 is added to the tunnel interface so that these bound sockets have a route; normal traffic keeps using the regular default route.
