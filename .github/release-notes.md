## Download

| File | Use it if |
|------|-----------|
| `betteramnezia-amd64-<version>.msi` | Most PCs (64-bit Intel/AMD). Installs to Program Files, adds a Start menu entry. |
| `betteramnezia-arm64-<version>.msi` | Windows on ARM (Snapdragon laptops). |
| `betteramnezia-x86-<version>.msi` | 32-bit Windows. |
| `BetterAmnezia-<version>-<arch>-portable.zip` | No installer: unzip anywhere and run `betteramnezia.exe` (keep `wintun.dll` next to it). |

Run the installer or `betteramnezia.exe` and approve the administrator prompt; BetterAmnezia then lives in the system tray. Requires Windows 10 or 11; the modern interface needs the WebView2 runtime, which is preinstalled on current Windows.

The builds are not code-signed, so Windows SmartScreen may warn about an unknown publisher: choose **More info → Run anyway**. Verify downloads with `SHA256SUMS.txt`.

BetterAmnezia installs alongside the official AmneziaWG client, WireGuard and AmneziaVPN without conflicts: it has its own services, data folder and installer. It does not ship `awg.exe`.

## Highlights

- New interface with System, Light, Dark and AMOLED themes, accent colours, English and Russian.
- Split tunneling per tunnel: apps and folders, IPs/CIDRs, domains with wildcards (`*.example.com`, `*example.com`), include/exclude modes, presets, encrypted DNS, local SOCKS5/HTTP proxy.
- Auto-tunneling by Wi-Fi network/Ethernet, health monitor with auto-restart, dynamic DNS endpoint updates, deferred endpoint resolution, lockdown kill switch, CLI remote control.
- Import `.conf`, `.zip` and AmneziaVPN `vpn://` keys; share as QR code or `vpn://` key.

App bypass ("Bypass VPN" rules) needs the separate Mullvad split tunnel driver; see `docs/splittunnel.md`.
