/* Development mock of the Go bridge, used to preview and test the UI in a
   regular browser: open webui/dev/index.html. Not shipped in the binary. */
(function () {
  const params = new URLSearchParams(location.search);
  const empty = params.has("empty");
  const now = () => Date.now();
  const tunnels = empty ? [] : [
    { name: "amsterdam-awg", state: "started", addresses: ["10.8.1.2/32", "fd00::2/128"], dns: ["1.1.1.1", "1.0.0.1"], endpoints: ["vpn-ams.example.net:51820"], allowedIps: ["0.0.0.0/0", "::/0"], listenPort: 0, mtu: 1280, peers: 1, awg: true, fullTunnel: true, publicKey: "x5Sgp4Kh9t1pKmM6qOQm0aPp8m0sWRGPl4H8gXy9vHw=" },
    { name: "home-lab", state: "stopped", addresses: ["192.168.77.5/24"], dns: [], endpoints: ["home.dyn.example.org:51820"], allowedIps: ["192.168.0.0/16"], listenPort: 0, mtu: 0, peers: 1, awg: false, fullTunnel: false, publicKey: "Zk2q9bJYbDiQWb9Y6g2Q5qk9m7Gv0wqM8cWcJfE2uGk=" },
    { name: "tokyo-wg", state: "stopped", addresses: ["10.66.0.12/32"], dns: ["9.9.9.9"], endpoints: ["203.0.113.40:443"], allowedIps: ["0.0.0.0/0"], listenPort: 0, mtu: 1420, peers: 2, awg: true, fullTunnel: true, publicKey: "A1b2C3d4E5f6G7h8I9j0K1l2M3n4O5p6Q7r8S9t0U1v=" }
  ];
  const splits = {
    "amsterdam-awg": { mode: "exclude", ips: ["192.168.1.0/24", "1.2.3.4"], domains: ["*.youtube.com", "*domain.com", "=bank.example"], apps: [
      { path: "C:\\Program Files\\Steam\\steam.exe", action: "bypass" },
      { path: "C:\\Users\\me\\AppData\\Roaming\\Telegram Desktop\\Telegram.exe", action: "vpnonly" },
      { path: "D:\\Games", folder: true, action: "bypass", enabled: false }
    ], allowLan: true, dns: { upstreams: ["https://cloudflare-dns.com/dns-query#1.1.1.1,1.0.0.1"], blockOtherDns: true }, proxy: { socks5: true, socksPort: 1080 } }
  };
  let settings = {
    autoTunnel: { enabled: true, tunnel: "amsterdam-awg", trustedSsids: ["HomeNet", "Office-*"], onUntrustedWifi: "connect", onEthernet: "none", onOther: "none", rules: [{ ssid: "Cafe *", action: "connect", tunnel: "tokyo-wg" }], debounceSeconds: 3 },
    health: { enabled: true, handshakeTimeoutSeconds: 180, pingTarget: "", pingIntervalSeconds: 30, pingFailuresForRestart: 3, restartCooldownSeconds: 60 },
    dynamicDns: { enabled: true, intervalSeconds: 300 },
    lockdown: { enabled: false, allowLan: true },
    remoteControl: false
  };
  let prefs = { theme: params.get("theme") || "dark", accent: params.get("accent") || "#7c5cff", language: params.get("lang") || "en", closeToTray: true, startMinimized: true, notifications: true, compact: false, windowSize: "remember" };
  let rx = 120e6, tx = 9e6;
  let logs = [];
  let cursor = 0;
  const logLines = [
    "[MGR] Starting BetterAmnezia/3.1.1 (Windows 11 23H2; amd64)",
    "[MGR] Network changed: wifi \"CafeCentral\" (Wi-Fi)",
    "[MGR] Auto-tunnel: connecting amsterdam-awg (untrusted Wi-Fi CafeCentral)",
    "[TUN] [amsterdam-awg] Split tunneling: mode=exclude, 2 address rules, 3 domain rules, 3 app rules",
    "[TUN] [amsterdam-awg] Split tunneling: DNS forwarder listening on 127.0.0.53",
    "[TUN] [amsterdam-awg] Creating Wintun interface",
    "[TUN] [amsterdam-awg] Startup complete",
    "[TUN] [amsterdam-awg] Split tunneling: www.youtube.com -> [142.250.74.14] (exclude)",
    "[MGR] [home-lab] Unable to resolve endpoint: timeout",
    "[GUI] UI started"
  ];
  logLines.forEach((l, i) => logs.push({ t: now() - (logLines.length - i) * 4000, text: l }));

  function emit(event, data) {
    setTimeout(() => window.__bridge && window.__bridge.event({ event, data }), 0);
  }

  const handlers = {
    "prefs.get": () => prefs,
    "prefs.set": p => (prefs = Object.assign(prefs, p)),
    "app.info": () => ({ version: "1.0.0", isAdmin: !params.has("readonly"), official: true, updateState: params.has("update") ? 1 : 0, arch: "amd64", os: "Windows 11 Pro 23H2" }),
    "tunnels.list": () => tunnels.map(t => Object.assign({}, t)),
    "tunnels.get": p => ({ summary: tunnels.find(t => t.name === p.name), text: `[Interface]\nPrivateKey = yAnz5TF+lXXJte14tji3zlMNq+hd2rYUIgJBgB3fBmk=\nAddress = 10.8.1.2/32, fd00::2/128\nDNS = 1.1.1.1, 1.0.0.1\nMTU = 1280\nJc = 4\nJmin = 40\nJmax = 70\nS1 = 86\nS2 = 112\nH1 = 1854962112\nH2 = 902311045\nH3 = 1720419830\nH4 = 2084519734\n\n# Amsterdam\n[Peer]\nPublicKey = xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=\nPresharedKey = 4kJ2pJ7rZ7Yfz2zKZKk9QKj1Q0b0u0+8bX5wq1i3l2Y=\nAllowedIPs = 0.0.0.0/0, ::/0\nEndpoint = vpn-ams.example.net:51820\nPersistentKeepalive = 25\n` }),
    "tunnels.runtime": p => {
      rx += 2e5 + Math.random() * 3.5e6;
      tx += 3e4 + Math.random() * 4e5;
      return { rx, tx, lastHandshake: now() - 41000, listenPort: 51904, now: now(), peers: [{ publicKey: "xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=", endpoint: "198.51.100.23:51820", rx, tx, lastHandshake: now() - 41000, keepalive: "25" }] };
    },
    "tunnels.start": p => setState(p.name, "started"),
    "tunnels.stop": p => setState(p.name, "stopped"),
    "tunnels.toggle": p => setState(p.name, tunnels.find(t => t.name === p.name).state === "started" ? "stopped" : "started"),
    "tunnels.validate": p => /\[Interface\]/i.test(p.text) ? { ok: true } : { ok: false, error: "Line 1: Missing [Interface] section" },
    "tunnels.template": () => ({ name: "new-tunnel", text: "[Interface]\nPrivateKey = cGFzc3dvcmRwYXNzd29yZHBhc3N3b3JkcGFzc3dvcmQ=\nAddress = \nDNS = 1.1.1.1\n\n[Peer]\nPublicKey = \nAllowedIPs = 0.0.0.0/0, ::/0\nEndpoint = \n" }),
    "tunnels.save": p => ({ name: p.name, restarted: false }),
    "tunnels.shareKey": p => ({ key: "vpn://AAAB2Xjaq1YqLs7PS0lNUbJSKs9MTlWyUlBQ8E0szs8DAAHqB_8", text: "[Interface]\nPrivateKey = …\n" }),
    "tunnels.qr": () => ({ svg: '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 29 29" shape-rendering="crispEdges"><rect width="100%" height="100%" fill="#fff"/><path fill="#000" d="M4 4h7v7h-7zM18 4h7v7h-7zM4 18h7v7h-7zM13 13h3v3h-3zM14 4h1v6h-1zM18 14h2v6h-2zM13 20h4v1h-4zM21 21h3v3h-3z"/></svg>' }),
    "keys.public": () => ({ public: "x5Sgp4Kh9t1pKmM6qOQm0aPp8m0sWRGPl4H8gXy9vHw=" }),
    "keys.generate": () => ({ private: "aGVsbG93b3JsZGhlbGxvd29ybGRoZWxsb3dvcmxkMTI=", public: "cHVibGljcHVibGljcHVibGljcHVibGljcHVibGljMTI=" }),
    "split.get": p => JSON.parse(JSON.stringify(splits[p.name] || { mode: "off", dns: {}, proxy: {} })),
    "split.validate": () => ({ ok: true }),
    "split.set": p => { splits[p.name] = p.config; return { restarted: true }; },
    "settings.get": () => JSON.parse(JSON.stringify(settings)),
    "settings.set": s => (settings = s),
    "status.get": () => ({ lockdown: settings.lockdown.enabled, network: { kind: "wifi", ssid: "CafeCentral", name: "Wi-Fi" }, health: { "amsterdam-awg": { healthy: true, restarts: 1 } }, driverAvailable: params.has("driver") }),
    "apps.describe": p => p.paths.map(path => ({ path, name: path.split("\\").pop().replace(/\.exe$/i, "") })),
    "apps.running": () => ["C:\\Program Files\\Mozilla Firefox\\firefox.exe", "C:\\Program Files\\Google\\Chrome\\Application\\chrome.exe", "C:\\Users\\me\\AppData\\Local\\Discord\\app-1.0\\Discord.exe", "C:\\Program Files\\qBittorrent\\qbittorrent.exe"].map(path => ({ path, name: path.split("\\").pop().replace(/\.exe$/i, "") })),
    "log.follow": p => {
      const out = logs.slice(p.cursor == null ? 0 : p.cursor);
      return { lines: out, cursor: logs.length };
    },
    "clipboard.read": () => ({ text: "" }),
    "clipboard.write": () => null,
    "tunnels.importText": () => ({ imported: ["imported"], errors: [] })
  };

  function setState(name, s) {
    const t = tunnels.find(t => t.name === name);
    const mid = s === "started" ? "starting" : "stopping";
    t.state = mid;
    emit("tunnelChange", { name, state: mid, global: mid });
    setTimeout(() => { t.state = s; emit("tunnelChange", { name, state: s, global: s }); }, 700);
    return null;
  }

  setInterval(() => {
    logs.push({ t: now(), text: "[TUN] [amsterdam-awg] Keepalive packet sent" });
  }, 3000);

  window.__host = {
    post(raw) {
      const msg = JSON.parse(raw);
      const fn = handlers[msg.method];
      setTimeout(() => {
        try {
          const result = fn ? fn(msg.params || {}) : null;
          window.__bridge.reply({ id: msg.id, result: result === undefined ? null : result });
        } catch (e) {
          window.__bridge.reply({ id: msg.id, error: String(e) });
        }
      }, 20);
    }
  };
})();
