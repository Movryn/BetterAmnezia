# Attack Surface

_This is an evolving document, describing currently known attack surface, a few mitigations, and several open questions. This is a work in progress. We document our current understanding with the intent of improving both our understanding and our security posture over time._

WireGuard for Windows consists of four components: a kernel driver, and three separate interacting userspace parts.

### WireGuardNT

WireGuardNT is a kernel driver. It exposes:

  - A miniport driver to the ndis stack, meaning any process on the system that can access the network stack in a reasonable way can send and receive packets, hitting those related ndis handlers.
  - A UDP port parsing WireGuard packets.
  - There are also various ndis OID calls, accessible to certain users, which hit further code.
  - A PNP and Close notifier added to the NDIS device file.
  - IOCTLs are added to the NDIS device file, and those IOCTLs are restricted to `O:SYD:P(A;;FA;;;SY)(A;;FA;;;BA)S:(ML;;NWNRNX;;;HI)`. The IOCTL allows userspace to get and set configuration, adapter state, and read log messages from a ring buffer.

### Tunnel Service

The tunnel service is a userspace service running as Local System, responsible for creating WireGuardNT adapters and configuring them. It exposes:

  - A global mutex is used for WireGuardNT interface creation, with the same DACL as the pipe, but first CreatePrivateNamespace is called with a "Local System" SID.
  - After some initial setup, it uses `AdjustTokenPrivileges` to remove all privileges, except for `SeLoadDriverPrivilege`, so that it can remove the interface when shutting down. This latter point is rather unfortunate, as `SeLoadDriverPrivilege` can be used for all sorts of interesting escalation. Future work includes forking an additional process or the like so that we can drop this from the main tunnel process.

### Manager Service

The manager service is a userspace service running as Local System, responsible for starting and stopping tunnel services, and ensuring a UI program with certain handles is available to Administrators. It exposes:

  - Extensive IPC using unnamed pipes, inherited by the UI process.
  - A readable `CreateFileMapping` handle to a binary ringlog shared by all services, inherited by the UI process.
  - It listens for service changes in tunnel services according to the string prefix "BetterAmneziaTunnel$".
  - It manages DPAPI-encrypted configuration files in `C:\Program Files\BetterAmnezia\Data`, which is created with `O:SYG:SYD:PAI(A;OICI;FA;;;SY)(A;OICI;FA;;;BA)`, and makes some effort to enforce good configuration filenames.
  - The actual DPAPI-encrypted configuration files are created with `O:SYG:SYD:PAI(A;;FA;;;SY)(A;;SD;;;BA)`.
  - It uses `WTSEnumerateSessions` and `WTSSESSION_NOTIFICATION` to walk through each available session. It then uses `WTSQueryUserToken` to get the token belonging to each session and then determines whether or not it is an administrator token. To determine that, it calls `CheckTokenMembership(CreateWellKnownSid(WinBuiltinAdministratorsSid))` on a duplicated impersonation token, as well as and calling `GetTokenInformation(TokenElevation)` on it. If either of these are false, then it fetched the linked token using `GetTokenInformation(TokenLinkedToken)` and queries the same. Only then does it spawn the UI process as that the elevated user token, passing it three unnamed pipe handles for IPC and the log mapping handle, as described above.
  - In the event that the administrator has set `HKLM\Software\AmneziaWG\LimitedOperatorUI` to 1, sessions are started for users that are a member of group S-1-5-32-556 (determined sing `CheckTokenMembership(CreateWellKnownSid(WinBuiltinNetworkConfigurationOperatorsSid))` on it and its linked token), with a more limited IPC interface, in which these non-admin users are denied private keys and tunnel editing rights. (This means users can potentially DoS the IPC server by draining notifications too slowly, or exhausting memory of the manager by spawning too many watcher go routines, or by sending garbage data that Go's `gob` decoder isn't expecting.)

### UI

The UI is a process running for each user who is in the Administrators group (per the above), running with the elevated high integrity linked token. It exposes:

  - Since the UI process is executed with an elevated token, it runs at high integrity and should be immune to various shatter attacks, modulo the great variety of clever bypasses in the latest Windows release.
  - It uses `AdjustTokenPrivileges` to remove all privileges.
  - It renders highlighted config files to a msftedit.dll control, which typically is capable of all sorts of OLE and RTF nastiness that we make some attempt to avoid.

### BetterAmnezia additions

  - **Tunnel service, split tunneling.** When a tunnel uses domain rules or custom DNS, the tunnel service listens for DNS on a loopback address (`127.0.0.53`, or another `127.x` address) over UDP and TCP and forwards queries to the configured upstreams; answers for matching names cause routes to be added. Any local process can query it. When the local proxy is enabled, the service listens for SOCKS5 and/or HTTP proxy clients on `127.0.0.1` (or on all interfaces if the user chose so), optionally with a username and password, and opens TCP connections through the tunnel on their behalf; with "local network" selected, other machines on the LAN can use the tunnel through it. Split tunneling rules are read from `Data\Extras`, which inherits the Data directory's SYSTEM/Administrators-only ACL.
  - **Tunnel service, split tunnel driver.** If a tunnel has "bypass" app rules and a `mullvad-split-tunnel.sys` is present next to the executable or configured in the settings, the tunnel service (before dropping privileges) installs and starts it as the kernel driver service `BetterAmneziaSplitTunnel`. The driver path in the settings file can therefore only be set by administrators.
  - **Manager service.** Extension operations travel over the existing IPC as an operation name plus JSON. Reading split tunneling rules is allowed for limited operators (proxy passwords are removed); writing settings or rules, renaming and quitting require the elevated token. The manager may install a lockdown WFP session (block everything except loopback, DHCP, NDP, the AmneziaWG executable and optionally the LAN) while no tunnel is up; it is dynamic, so it disappears if the manager exits. It also sends ICMP echo requests from tunnel addresses for the optional health check, re-resolves endpoint host names and updates running tunnels through their UAPI pipes, and queries the WLAN API for the current SSID.
  - **New UI.** The interface is a WebView2 control in the UI process, which keeps only `SeChangeNotifyPrivilege` (needed by WebView2) when dropping privileges. The page is loaded from a string embedded in the executable with a restrictive Content Security Policy, and talks to Go through a fixed set of bridge methods; navigation, downloads and developer tools are disabled. WebView2 user data lives in `%LOCALAPPDATA%\BetterAmnezia\WebView2`, interface preferences in `%LOCALAPPDATA%\BetterAmnezia\ui.json`.
  - **Remote control.** The UI window accepts `WM_COPYDATA` messages from lower integrity processes (via `ChangeWindowMessageFilterEx`) carrying `connect`, `disconnect`, `toggle` or `disconnectall` commands, sent by `betteramnezia.exe /connect NAME` and friends. They are ignored unless an administrator enabled remote control in the service settings (stored in `Data\Extras\settings.json`, so a non-elevated process cannot turn it on), and they can only start or stop existing tunnels.

### Updates

A server hosts the result of `b2sum -l 256 *.msi > list && signify -S -e -s release.sec -m list && upload ./list.sec`, with the private key stored on an HSM. The MSIs in that list are only the latest ones available, and filenames fit the form `wireguard-${arch}-${version}.msi`. The updater, running as part of the manager service, downloads this list over TLS and verifies the signify Ed25519 signature of it. If it validates, then it finds the first MSI in it for its architecture that has a greater version. It then downloads this MSI from a predefined URL to a randomly generated (256-bits) file name inside `C:\Windows\Temp` with permissions of `O:SYD:PAI(A;;FA;;;SY)(A;;FR;;;BA)`, scheduled to be cleaned up at next boot via `MoveFileEx(MOVEFILE_DELAY_UNTIL_REBOOT)`, and verifies the BLAKE2b-256 signature. If it validates, then it calls `WinTrustVerify(WINTRUST_ACTION_GENERIC_VERIFY_V2, WTD_REVOKE_WHOLECHAIN)` on the MSI. If it validates, then it executes the installer with `msiexec.exe /qb!- /i`, using the elevated token linked to the IPC UI session that requested the update. Because `msiexec` requires exclusive access to the file, the file handle is closed in between the completion of downloading and the commencement of `msiexec`. Hopefully the permissions of `C:\Windows\Temp` are good enough that an attacker can't replace the MSI from beneath us.
