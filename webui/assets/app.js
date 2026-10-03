/* BetterAmnezia UI. Plain DOM, no framework: each page is a function that
   builds its elements; state changes re-render the affected page. */
(() => {
  "use strict";

  // ------------------------------------------------------------------
  // DOM helpers
  // ------------------------------------------------------------------

  function h(tag, props, ...children) {
    const el = document.createElement(tag);
    if (props) {
      for (const [k, v] of Object.entries(props)) {
        if (v == null || v === false) continue;
        if (k === "class") el.className = v;
        else if (k === "style" && typeof v === "object") Object.assign(el.style, v);
        else if (k.startsWith("on") && typeof v === "function") el.addEventListener(k.slice(2).toLowerCase(), v);
        else if (k === "html") el.innerHTML = v;
        else if (k === "value") el.value = v;
        else if (k === "checked") el.checked = !!v;
        else if (k === "ref") v(el);
        else if (v === true) el.setAttribute(k, "");
        else el.setAttribute(k, v);
      }
    }
    append(el, children);
    return el;
  }

  function append(el, children) {
    if (!Array.isArray(children)) children = [children];
    for (const c of children) {
      if (c == null || c === false) continue;
      if (Array.isArray(c)) append(el, c);
      else if (c instanceof Node) el.appendChild(c);
      else el.appendChild(document.createTextNode(String(c)));
    }
  }

  const ICONS = {
    shield: '<path d="M12 3l7 3v6c0 4.4-3 8.3-7 9-4-.7-7-4.6-7-9V6z"/><path d="M9 12l2 2 4-4"/>',
    logo: '<path d="M12 2.8l7.6 3.2v5.8c0 4.9-3.2 9-7.6 10.2-4.4-1.2-7.6-5.3-7.6-10.2V6z"/><path d="M8.3 15.6L12 7.4l3.7 8.2M9.6 12.8h4.8"/>',
    list: '<path d="M8 6h12M8 12h12M8 18h12"/><circle cx="4" cy="6" r="1"/><circle cx="4" cy="12" r="1"/><circle cx="4" cy="18" r="1"/>',
    split: '<path d="M4 12h6l4-6h6M14 6l-2-2M20 6l-2 2M10 12l4 6h6M20 18l-2-2M20 18l-2 2"/>',
    wifi: '<path d="M2 8.5a15 15 0 0120 0M5 12a10 10 0 0114 0M8.5 15.5a5 5 0 017 0"/><circle cx="12" cy="19" r="1"/>',
    ethernet: '<rect x="4" y="6" width="16" height="12" rx="2"/><path d="M8 18v-4M12 18v-4M16 18v-4M9 6v3h6V6"/>',
    globe: '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3c2.5 3 2.5 15 0 18M12 3c-2.5 3-2.5 15 0 18"/>',
    gear: '<circle cx="12" cy="12" r="3"/><path d="M19.4 15a1.7 1.7 0 00.3 1.8l.1.1a2 2 0 11-2.8 2.8l-.1-.1a1.7 1.7 0 00-1.8-.3 1.7 1.7 0 00-1 1.5V21a2 2 0 11-4 0v-.1a1.7 1.7 0 00-1.1-1.5 1.7 1.7 0 00-1.8.3l-.1.1a2 2 0 11-2.8-2.8l.1-.1a1.7 1.7 0 00.3-1.8 1.7 1.7 0 00-1.5-1H3a2 2 0 110-4h.1a1.7 1.7 0 001.5-1.1 1.7 1.7 0 00-.3-1.8l-.1-.1a2 2 0 112.8-2.8l.1.1a1.7 1.7 0 001.8.3H9a1.7 1.7 0 001-1.5V3a2 2 0 114 0v.1a1.7 1.7 0 001 1.5 1.7 1.7 0 001.8-.3l.1-.1a2 2 0 112.8 2.8l-.1.1a1.7 1.7 0 00-.3 1.8V9a1.7 1.7 0 001.5 1H21a2 2 0 110 4h-.1a1.7 1.7 0 00-1.5 1z"/>',
    terminal: '<rect x="3" y="4" width="18" height="16" rx="2"/><path d="M7 9l3 3-3 3M13 15h4"/>',
    info: '<circle cx="12" cy="12" r="9"/><path d="M12 11v5M12 8h.01"/>',
    plus: '<path d="M12 5v14M5 12h14"/>',
    power: '<path d="M12 3v9"/><path d="M6.3 7.3a8 8 0 1011.4 0"/>',
    edit: '<path d="M4 20h4L19 9l-4-4L4 16z"/><path d="M13.5 6.5l4 4"/>',
    share: '<circle cx="18" cy="5" r="2.5"/><circle cx="6" cy="12" r="2.5"/><circle cx="18" cy="19" r="2.5"/><path d="M8.2 10.8l7.6-4.6M8.2 13.2l7.6 4.6"/>',
    more: '<circle cx="5" cy="12" r="1.2"/><circle cx="12" cy="12" r="1.2"/><circle cx="19" cy="12" r="1.2"/>',
    download: '<path d="M12 4v12M7 11l5 5 5-5M5 20h14"/>',
    upload: '<path d="M12 20V8M7 13l5-5 5 5M5 4h14"/>',
    handshake: '<path d="M12 8v4l3 2"/><circle cx="12" cy="12" r="9"/>',
    server: '<rect x="4" y="4" width="16" height="7" rx="1.5"/><rect x="4" y="13" width="16" height="7" rx="1.5"/><path d="M8 7.5h.01M8 16.5h.01"/>',
    copy: '<rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V6a2 2 0 00-2-2H6a2 2 0 00-2 2v8a2 2 0 002 2h2"/>',
    trash: '<path d="M4 7h16M10 11v6M14 11v6M6 7l1 13h10l1-13M9 7V4h6v3"/>',
    file: '<path d="M14 3H6a2 2 0 00-2 2v14a2 2 0 002 2h12a2 2 0 002-2V9z"/><path d="M14 3v6h6"/>',
    folder: '<path d="M3 7a2 2 0 012-2h4l2 2h8a2 2 0 012 2v8a2 2 0 01-2 2H5a2 2 0 01-2-2z"/>',
    app: '<rect x="4" y="4" width="16" height="16" rx="3"/><path d="M4 9h16"/>',
    clipboard: '<rect x="6" y="4" width="12" height="17" rx="2"/><path d="M9 4V3h6v1M9 10h6M9 14h6"/>',
    key: '<circle cx="8" cy="15" r="4"/><path d="M11 12l9-9M17 6l3 3M15 8l2 2"/>',
    check: '<path d="M5 12l5 5L20 7"/>',
    x: '<path d="M6 6l12 12M18 6L6 18"/>',
    alert: '<path d="M12 3l9.5 17h-19z"/><path d="M12 10v4M12 17h.01"/>',
    error: '<circle cx="12" cy="12" r="9"/><path d="M9 9l6 6M15 9l-6 6"/>',
    search: '<circle cx="11" cy="11" r="7"/><path d="M20 20l-4-4"/>',
    refresh: '<path d="M20 11a8 8 0 10-2.3 5.7"/><path d="M20 4v7h-7"/>',
    zip: '<path d="M14 3H6a2 2 0 00-2 2v14a2 2 0 002 2h12a2 2 0 002-2V9z"/><path d="M14 3v6h6M10 5h2M10 8h2M10 11h2M10 14h2v3h-2z"/>',
    qr: '<rect x="4" y="4" width="6" height="6"/><rect x="14" y="4" width="6" height="6"/><rect x="4" y="14" width="6" height="6"/><path d="M14 14h2v2h-2zM18 18h2v2h-2zM14 18h2M18 14h2"/>',
    lock: '<rect x="5" y="11" width="14" height="9" rx="2"/><path d="M8 11V8a4 4 0 018 0v3"/>',
    bolt: '<path d="M13 3L5 13h6l-1 8 8-10h-6z"/>',
    heart: '<path d="M12 20s-7-4.5-7-10a4 4 0 017-2.6A4 4 0 0119 10c0 5.5-7 10-7 10z"/>',
    route: '<circle cx="6" cy="18" r="2"/><circle cx="18" cy="6" r="2"/><path d="M8 18h7a3 3 0 000-6H9a3 3 0 010-6h7"/>',
    dns: '<path d="M4 7h16M4 12h16M4 17h10"/><circle cx="18" cy="17" r="2"/>',
    proxy: '<path d="M4 12h4l3-6 2 12 3-6h4"/>',
    eye: '<path d="M2 12s3.5-7 10-7 10 7 10 7-3.5 7-10 7S2 12 2 12z"/><circle cx="12" cy="12" r="3"/>',
    palette: '<path d="M12 3a9 9 0 100 18c1 0 1.5-.8 1.2-1.6-.4-1 .2-2.4 1.6-2.4H17a4 4 0 004-4c0-5.5-4-10-9-10z"/><circle cx="7.5" cy="11" r="1"/><circle cx="10.5" cy="7" r="1"/><circle cx="15" cy="7.5" r="1"/>',
    play: '<path d="M7 4l13 8-13 8z"/>',
    ext: '<path d="M14 4h6v6M20 4l-9 9M18 14v5a1 1 0 01-1 1H5a1 1 0 01-1-1V7a1 1 0 011-1h5"/>',
    save: '<path d="M5 3h11l4 4v12a2 2 0 01-2 2H6a2 2 0 01-2-2V5a2 2 0 012-2z"/><path d="M8 3v5h7V3M8 21v-7h8v7"/>',
    sparkle: '<path d="M12 3l1.8 5.2L19 10l-5.2 1.8L12 17l-1.8-5.2L5 10l5.2-1.8z"/>',
    pause: '<path d="M8 5v14M16 5v14"/>'
  };

  function icon(name, extraClass) {
    const span = document.createElement("span");
    span.style.display = "contents";
    span.innerHTML = '<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"' + (extraClass ? ' class="' + extraClass + '"' : "") + ">" + (ICONS[name] || ICONS.info) + "</svg>";
    return span.firstChild;
  }

  // ------------------------------------------------------------------
  // Bridge to Go
  // ------------------------------------------------------------------

  // The host installs __host before the page loads; fall back to the
  // WebView2 channel directly in case that script was not registered yet.
  if (!window.__host && window.chrome && window.chrome.webview) {
    window.__host = { post: m => window.chrome.webview.postMessage(m) };
  }

  const pending = new Map();
  const listeners = new Map();
  let seq = 0;

  window.__bridge = {
    reply(msg) {
      const p = pending.get(msg.id);
      if (!p) return;
      pending.delete(msg.id);
      if (msg.error) p.reject(new Error(msg.error));
      else p.resolve(msg.result);
    },
    event(msg) {
      (listeners.get(msg.event) || []).forEach(fn => {
        try { fn(msg.data); } catch (e) { console.error(e); }
      });
    }
  };

  function call(method, params) {
    return new Promise((resolve, reject) => {
      const id = ++seq;
      pending.set(id, { resolve, reject });
      try {
        window.__host.post(JSON.stringify({ id, method, params: params || {} }));
      } catch (e) {
        pending.delete(id);
        reject(e);
      }
    });
  }

  function on(event, fn) {
    if (!listeners.has(event)) listeners.set(event, []);
    listeners.get(event).push(fn);
  }

  // ------------------------------------------------------------------
  // Formatting
  // ------------------------------------------------------------------

  function fmtBytes(n) {
    if (!n) return "0 B";
    const u = ["B", "KiB", "MiB", "GiB", "TiB"];
    let i = 0;
    while (n >= 1024 && i < u.length - 1) { n /= 1024; i++; }
    return (i === 0 ? n.toFixed(0) : n < 10 ? n.toFixed(2) : n < 100 ? n.toFixed(1) : n.toFixed(0)) + " " + u[i];
  }

  function fmtRate(bps) {
    if (!bps || bps < 1) return "0 B/s";
    return fmtBytes(bps) + "/s";
  }

  function fmtAgo(ms) {
    if (!ms) return t("never");
    const s = Math.max(0, Math.round((Date.now() - ms) / 1000));
    if (s < 3) return t("just now");
    if (s < 120) return t("{0}s ago", s);
    if (s < 7200) return t("{0}m ago", Math.round(s / 60));
    return t("{0}h ago", Math.round(s / 3600));
  }

  function fmtDuration(ms) {
    const s = Math.floor(ms / 1000);
    const hh = Math.floor(s / 3600), mm = Math.floor(s % 3600 / 60), ss = s % 60;
    const pad = n => String(n).padStart(2, "0");
    return (hh ? hh + ":" : "") + pad(mm) + ":" + pad(ss);
  }

  const clone = v => JSON.parse(JSON.stringify(v));
  const same = (a, b) => JSON.stringify(a) === JSON.stringify(b);

  function debounce(fn, ms) {
    let timer;
    return (...a) => { clearTimeout(timer); timer = setTimeout(() => fn(...a), ms); };
  }

  // ------------------------------------------------------------------
  // State
  // ------------------------------------------------------------------

  const state = {
    info: { version: "", isAdmin: true, updateState: 0 },
    prefs: { theme: "system", accent: "#7c5cff", language: "auto", closeToTray: true, startMinimized: true, notifications: true, remoteControl: false, compact: false },
    tunnels: [],
    loaded: false,
    selected: null,
    page: "tunnels",
    search: "",
    status: { lockdown: false, network: { kind: "none" }, health: {}, driverAvailable: false },
    settings: null,
    runtime: {},
    history: {},
    since: {},
    splitCache: {},
    busy: {},
    update: null
  };

  const PAGES = [
    { id: "tunnels", icon: "list", label: "Tunnels" },
    { id: "split", icon: "split", label: "Split tunneling" },
    { id: "auto", icon: "wifi", label: "Auto-tunnel" },
    { id: "settings", icon: "gear", label: "Settings" },
    { id: "logs", icon: "terminal", label: "Logs" },
    { id: "about", icon: "info", label: "About" }
  ];

  // ------------------------------------------------------------------
  // Theme
  // ------------------------------------------------------------------

  const lightQuery = window.matchMedia("(prefers-color-scheme: light)");

  function applyPrefs() {
    const p = state.prefs;
    let theme = p.theme;
    if (theme === "system") theme = lightQuery.matches ? "light" : "dark";
    document.documentElement.dataset.theme = theme;
    document.documentElement.style.setProperty("--accent", p.accent || "#7c5cff");
    document.documentElement.style.setProperty("--accent-contrast", contrastText(p.accent || "#7c5cff"));
    document.body.classList.toggle("compact", !!p.compact);
    i18n.set(p.language);
  }

  function contrastText(hex) {
    const m = /^#?([0-9a-f]{6})$/i.exec(hex || "");
    if (!m) return "#fff";
    const n = parseInt(m[1], 16);
    const r = n >> 16 & 255, g = n >> 8 & 255, b = n & 255;
    return (0.299 * r + 0.587 * g + 0.114 * b) > 170 ? "#111" : "#fff";
  }

  lightQuery.addEventListener("change", () => { applyPrefs(); });

  async function setPrefs(patch) {
    Object.assign(state.prefs, patch);
    applyPrefs();
    try {
      state.prefs = await call("prefs.set", state.prefs);
    } catch (e) { toastError(e); }
    render();
  }

  // ------------------------------------------------------------------
  // Toasts, menus, modals
  // ------------------------------------------------------------------

  function toast(kind, title, message, ms) {
    const el = h("div", { class: "toast " + kind },
      icon(kind === "ok" ? "check" : kind === "bad" ? "error" : "info"),
      h("div", null, h("div", { class: "t" }, title), message ? h("div", { class: "m" }, message) : null));
    document.getElementById("toasts").appendChild(el);
    setTimeout(() => { el.style.transition = "opacity .2s"; el.style.opacity = "0"; setTimeout(() => el.remove(), 220); }, ms || (kind === "bad" ? 7000 : 3000));
  }

  function toastError(e, title) {
    toast("bad", title || t("Something went wrong"), (e && e.message) || String(e));
  }

  let openMenuEl = null;
  function closeMenu() {
    if (openMenuEl) { openMenuEl.remove(); openMenuEl = null; }
  }
  document.addEventListener("mousedown", e => {
    if (openMenuEl && !openMenuEl.contains(e.target)) closeMenu();
  });

  function showMenu(anchor, items) {
    closeMenu();
    const menu = h("div", { class: "menu", role: "menu" });
    items.forEach(it => {
      if (!it) return;
      if (it === "-") { menu.appendChild(h("hr")); return; }
      menu.appendChild(h("button", {
        class: it.danger ? "danger" : null,
        disabled: it.disabled,
        onclick: () => { closeMenu(); it.action(); }
      }, it.icon ? icon(it.icon) : null, it.label));
    });
    document.body.appendChild(menu);
    const r = anchor.getBoundingClientRect();
    const mw = menu.offsetWidth, mh = menu.offsetHeight;
    let left = Math.min(r.right - mw, window.innerWidth - mw - 8);
    left = Math.max(8, left);
    let top = r.bottom + 6;
    if (top + mh > window.innerHeight - 8) top = Math.max(8, r.top - mh - 6);
    menu.style.left = left + "px";
    menu.style.top = top + "px";
    openMenuEl = menu;
  }

  const modalStack = [];

  function modal({ title, body, foot, size, onClose, dismissable = true }) {
    const box = h("div", { class: "modal" + (size ? " " + size : ""), role: "dialog", "aria-modal": "true" },
      h("div", { class: "modal-head" }, h("h2", null, title),
        dismissable ? h("button", { class: "btn ghost icon small", title: t("Close"), onclick: () => close() }, icon("x")) : null),
      h("div", { class: "modal-body" }, body),
      foot ? h("div", { class: "modal-foot" }, foot) : null);
    const scrim = h("div", { class: "scrim", onmousedown: e => { if (e.target === scrim && dismissable) close(); } }, box);
    document.getElementById("overlays").appendChild(scrim);
    const entry = { scrim, close, dismissable };
    modalStack.push(entry);
    function close(result) {
      if (!scrim.isConnected) return;
      if (onClose && onClose(result) === false) return;
      scrim.remove();
      const i = modalStack.indexOf(entry);
      if (i >= 0) modalStack.splice(i, 1);
    }
    setTimeout(() => {
      if (box.contains(document.activeElement)) return;
      const f = box.querySelector("[autofocus]") || box.querySelector("input,textarea,button.primary");
      if (f) f.focus({ preventScroll: true });
    }, 30);
    return { close, box };
  }

  function confirmDialog({ title, message, ok, danger }) {
    return new Promise(resolve => {
      let done = false;
      const m = modal({
        title,
        body: h("p", { class: "muted", style: { margin: "4px 0 6px" } }, message),
        foot: [
          h("button", { class: "btn", onclick: () => m.close() }, t("Cancel")),
          h("button", { class: "btn primary" + (danger ? " danger solid" : ""), autofocus: true, onclick: () => { done = true; m.close(); } }, ok || t("Yes"))
        ],
        onClose: () => resolve(done)
      });
    });
  }

  function promptDialog({ title, label, value, ok, validate }) {
    return new Promise(resolve => {
      let result = null;
      let input;
      const err = h("div", { class: "editor-status bad" });
      const submit = () => {
        const v = input.value.trim();
        const problem = validate ? validate(v) : null;
        if (problem) { err.textContent = problem; input.classList.add("invalid"); return; }
        result = v;
        m.close();
      };
      const m = modal({
        title,
        body: h("div", { class: "vstack" },
          h("label", { class: "muted" }, label),
          input = h("input", { class: "input", value: value || "", autofocus: true, onkeydown: e => { if (e.key === "Enter") submit(); } }),
          err),
        foot: [h("button", { class: "btn", onclick: () => m.close() }, t("Cancel")), h("button", { class: "btn primary", onclick: submit }, ok || t("Save"))],
        onClose: () => resolve(result)
      });
      setTimeout(() => { input.focus(); input.select(); }, 40);
    });
  }

  function switchEl(checked, onchange, opts = {}) {
    return h("label", { class: "switch" + (opts.busy ? " busy" : ""), title: opts.title, onclick: e => e.stopPropagation() },
      h("input", { type: "checkbox", checked, disabled: opts.disabled, "aria-label": opts.label || opts.title, onchange: e => onchange(e.target.checked, e) }),
      h("span", { class: "track" }), h("span", { class: "thumb" }));
  }

  function selectEl(value, options, onchange, attrs = {}) {
    return h("select", Object.assign({ class: "select", onchange: e => onchange(e.target.value) }, attrs),
      options.map(o => h("option", { value: o.value, selected: o.value === value ? true : null }, o.label)));
  }

  function settingRow(name, desc, control, opts = {}) {
    return h("div", { class: "row" + (opts.sub ? " sub" : "") + (opts.disabled ? " disabled" : "") },
      h("div", { class: "label" }, h("div", { class: "name" }, name), desc ? h("div", { class: "desc" }, desc) : null),
      h("div", { class: "control" }, control));
  }

  function numberInput(value, min, max, onchange, width) {
    const el = h("input", {
      class: "input", type: "number", min, max, value, style: { width: (width || 90) + "px" },
      onchange: e => {
        const n = parseInt(e.target.value, 10);
        if (isNaN(n) || n < min || n > max) {
          e.target.classList.add("invalid");
          toast("bad", t("Value must be between {0} and {1}.", min, max));
          return;
        }
        e.target.classList.remove("invalid");
        onchange(n);
      }
    });
    return el;
  }

  async function copyText(text) {
    try {
      await call("clipboard.write", { text });
      toast("ok", t("Copied"));
    } catch (e) { toastError(e); }
  }

  function copyButton(text) {
    return h("button", { class: "btn ghost icon small copy-btn", title: t("Copy"), onclick: e => { e.stopPropagation(); copyText(text); } }, icon("copy"));
  }

  // ------------------------------------------------------------------
  // Data loading
  // ------------------------------------------------------------------

  async function loadTunnels() {
    try {
      state.tunnels = await call("tunnels.list");
    } catch (e) {
      toastError(e);
    }
    state.loaded = true;
    if (!state.selected || !state.tunnels.find(x => x.name === state.selected)) {
      const active = state.tunnels.find(x => x.state === "started");
      const last = state.tunnels.find(x => x.name === state.prefs.lastTunnel);
      state.selected = (active || last || state.tunnels[0] || {}).name || null;
    }
    render();
  }

  async function loadStatus() {
    try {
      state.status = await call("status.get");
    } catch (e) { /* the manager may be busy */ }
  }

  async function loadSettings() {
    try {
      state.settings = await call("settings.get");
    } catch (e) { toastError(e); }
  }

  const saveSettingsSoon = debounce(async () => {
    try {
      state.settings = await call("settings.set", state.settings);
      flashSaved();
      loadStatus().then(renderSidebar);
    } catch (e) {
      toastError(e);
      await loadSettings();
      render();
    }
  }, 350);

  function updateSettings(mutator) {
    if (!state.settings) return;
    mutator(state.settings);
    saveSettingsSoon();
    render();
  }

  function flashSaved() {
    const el = document.getElementById("saved-flash");
    if (!el) return;
    el.classList.remove("hidden");
    clearTimeout(flashSaved.timer);
    flashSaved.timer = setTimeout(() => el.classList.add("hidden"), 1600);
  }

  function tunnelByName(name) {
    return state.tunnels.find(x => x.name === name);
  }

  async function getSplit(name, force) {
    if (!force && state.splitCache[name]) return state.splitCache[name];
    const c = await call("split.get", { name });
    state.splitCache[name] = c;
    return c;
  }

  // ------------------------------------------------------------------
  // Shell
  // ------------------------------------------------------------------

  function navigate(page) {
    if (state.page === page) return;
    if (state.page === "split" && splitPage.dirty() && !splitPage.confirmLeave(() => { state.page = page; render(); })) return;
    state.page = page;
    render();
  }

  function renderSidebar() {
    const side = document.getElementById("sidebar");
    side.innerHTML = "";
    const active = state.tunnels.filter(x => x.state === "started");
    const transitional = state.tunnels.find(x => x.state === "starting" || x.state === "stopping");
    let dotClass = "", title = t("Not connected"), sub = state.status.lockdown ? t("Lockdown mode") : "";
    if (transitional) {
      dotClass = transitional.state;
      title = transitional.state === "starting" ? t("Connecting…") : t("Disconnecting…");
      sub = transitional.name;
    } else if (active.length) {
      dotClass = "started";
      title = active.length === 1 ? active[0].name : t("{0} active", active.length);
      sub = t("Connected");
    }
    append(side, [
      h("div", { class: "brand" },
        h("div", { class: "brand-mark" }, icon("logo")),
        h("div", { class: "brand-text" }, h("div", { class: "brand-name" }, "BetterAmnezia"), h("div", { class: "brand-sub" }, "AmneziaWG " + (state.info.version || "")))),
      PAGES.map(p => h("button", {
        class: "nav-item" + (state.page === p.id ? " active" : ""),
        title: t(p.label),
        onclick: () => navigate(p.id)
      }, icon(p.icon), h("span", null, t(p.label)),
        p.id === "about" && state.info.updateState === 1 ? h("span", { class: "badge accent" }, "1") : null,
        p.id === "auto" && state.settings && state.settings.autoTunnel.enabled ? h("span", { class: "badge ok" }, "ON") : null)),
      h("div", { class: "sidebar-spacer" }),
      state.status.lockdown ? h("div", { class: "callout warn", style: { marginBottom: "8px", padding: "10px" } }, icon("lock"), h("span", null, t("Lockdown mode"))) : null,
      h("div", { class: "status-card", onclick: () => { if (active[0]) state.selected = active[0].name; navigate("tunnels"); render(); } },
        h("span", { class: "dot " + dotClass }),
        h("div", null, h("div", { class: "title" }, title), sub ? h("div", { class: "sub" }, sub) : null))
    ]);
  }

  let pageScroll = {};

  function render() {
    renderSidebar();
    const main = document.getElementById("main");
    const prevBody = main.querySelector("[data-scroll]");
    if (prevBody) pageScroll[prevBody.dataset.scroll] = prevBody.scrollTop;
    const pageFn = { tunnels: tunnelsPage, split: splitPage.render, auto: autoPage, settings: settingsPage, logs: logsPage.render, about: aboutPage }[state.page];
    const content = pageFn();
    main.innerHTML = "";
    append(main, content);
    main.querySelectorAll("[data-scroll]").forEach(el => {
      if (pageScroll[el.dataset.scroll] != null) el.scrollTop = pageScroll[el.dataset.scroll];
    });
  }

  function pageHeader(title, sub, actions) {
    return h("div", { class: "page-header" },
      h("div", null, h("h1", { class: "page-title" }, title), sub ? h("div", { class: "page-sub" }, sub) : null),
      h("div", { class: "spacer" }),
      h("span", { id: "saved-flash", class: "badge ok hidden" }, icon("check"), t("Saved")),
      actions);
  }

  function readOnlyNotice() {
    return state.info.isAdmin ? null : h("div", { class: "callout warn", style: { marginBottom: "16px" } }, icon("lock"), h("div", null, t("Read-only: administrator rights are needed to change tunnels.")));
  }

  // ------------------------------------------------------------------
  // Tunnels page
  // ------------------------------------------------------------------

  function stateLabel(s) {
    return { started: t("Connected"), stopped: t("Disconnected"), starting: t("Connecting…"), stopping: t("Disconnecting…") }[s] || s;
  }

  async function toggleTunnel(name) {
    const tun = tunnelByName(name);
    if (!tun) return;
    try {
      if (tun.state === "started" || tun.state === "starting") await call("tunnels.stop", { name });
      else await call("tunnels.start", { name });
    } catch (e) { toastError(e, name); }
  }

  function addMenuItems() {
    return [
      { icon: "file", label: t("Import from file…"), action: () => importDialog("file") },
      { icon: "clipboard", label: t("Paste config or vpn:// key…"), action: () => importDialog("text") },
      { icon: "plus", label: t("Create empty tunnel"), action: () => editTunnel(null) },
      "-",
      { icon: "zip", label: t("Export all to ZIP…"), disabled: !state.tunnels.length, action: () => exportZip([]) }
    ];
  }

  function tunnelsPage() {
    if (state.loaded && !state.tunnels.length) {
      return [
        pageHeader(t("Tunnels"), null, null),
        h("div", { class: "page-body" },
          readOnlyNotice(),
          h("div", { class: "empty", style: { marginTop: "6vh" } },
            h("div", { class: "art" }, icon("shield")),
            h("h2", null, t("No tunnels yet")),
            h("p", null, t("Import a .conf or .zip file, paste an AmneziaVPN vpn:// key, or drop files anywhere in this window.")),
            h("div", { class: "hstack" },
              h("button", { class: "btn primary", disabled: !state.info.isAdmin, onclick: () => importDialog("file") }, icon("file"), t("Import tunnel")),
              h("button", { class: "btn", disabled: !state.info.isAdmin, onclick: () => importDialog("text") }, icon("key"), t("Paste key")),
              h("button", { class: "btn ghost", disabled: !state.info.isAdmin, onclick: () => editTunnel(null) }, icon("plus"), t("Create empty tunnel")))))
      ];
    }
    const q = state.search.trim().toLowerCase();
    const list = state.tunnels.filter(x => !q || x.name.toLowerCase().includes(q) || (x.endpoints || []).join(" ").toLowerCase().includes(q));
    let addBtn;
    return [
      pageHeader(t("Tunnels"), null,
        addBtn = h("button", { class: "btn primary", disabled: !state.info.isAdmin, onclick: () => showMenu(addBtn, addMenuItems()) }, icon("plus"), t("Add tunnel"))),
      h("div", { class: "page-body flush" },
        h("div", { class: "tunnels" },
          h("div", { class: "tunnel-list" },
            h("div", { class: "tools" },
              h("div", { class: "search" }, icon("search"),
                h("input", {
                  class: "input", id: "tunnel-search", placeholder: t("Search tunnels"), value: state.search,
                  oninput: e => { state.search = e.target.value; renderTunnelItems(); }
                }))),
            h("div", { class: "tunnel-items", id: "tunnel-items", "data-scroll": "tunnel-items" }, tunnelItems(list))),
          h("div", { class: "detail", id: "detail", "data-scroll": "detail" }, tunnelDetail())))
    ];
  }

  function tunnelItems(list) {
    if (!list.length) return h("div", { class: "muted", style: { padding: "12px" } }, state.loaded ? t("No tunnels match your search.") : t("Loading…"));
    return list.map(tun => h("div", {
      class: "tunnel-item" + (tun.name === state.selected ? " selected" : ""),
      tabindex: "0",
      onclick: () => { selectTunnel(tun.name); },
      onkeydown: e => { if (e.key === "Enter") selectTunnel(tun.name); if (e.key === " ") { e.preventDefault(); toggleTunnel(tun.name); } }
    },
      h("span", { class: "dot " + (tun.error ? "error" : tun.state) }),
      h("div", { class: "meta" },
        h("div", { class: "name" }, tun.name),
        h("div", { class: "sub" }, tun.state === "started" || tun.state === "starting" || tun.state === "stopping" ? stateLabel(tun.state) : ((tun.endpoints || [])[0] || (tun.addresses || [])[0] || ""))),
      switchEl(tun.state === "started" || tun.state === "starting", () => toggleTunnel(tun.name), { busy: tun.state === "starting" || tun.state === "stopping", label: tun.name })));
  }

  function renderTunnelItems() {
    const el = document.getElementById("tunnel-items");
    if (!el) return;
    const q = state.search.trim().toLowerCase();
    el.innerHTML = "";
    append(el, tunnelItems(state.tunnels.filter(x => !q || x.name.toLowerCase().includes(q) || (x.endpoints || []).join(" ").toLowerCase().includes(q))));
  }

  function selectTunnel(name) {
    state.selected = name;
    state.prefs.lastTunnel = name;
    call("prefs.set", { lastTunnel: name }).catch(() => {});
    renderTunnelItems();
    renderDetail();
    pollRuntime();
  }

  function renderDetail() {
    const el = document.getElementById("detail");
    if (!el) return;
    el.innerHTML = "";
    append(el, tunnelDetail());
  }

  function tunnelDetail() {
    const tun = tunnelByName(state.selected);
    if (!tun) return h("div", { class: "empty" }, h("div", { class: "art" }, icon("list")), h("h2", null, t("Select a tunnel")));
    const on = tun.state === "started";
    const busy = tun.state === "starting" || tun.state === "stopping";
    const rt = state.runtime[tun.name];
    const split = state.splitCache[tun.name];
    if (!split) getSplit(tun.name).then(() => { if (state.selected === tun.name) renderDetail(); }).catch(() => {});
    const health = state.status.health && state.status.health[tun.name];
    let moreBtn;
    const since = state.since[tun.name];
    return [
      readOnlyNotice(),
      h("div", { class: "card hero" + (on ? " on" : "") },
        h("button", {
          class: "power" + (on ? " on" : "") + (busy ? " busy" : ""),
          title: on ? t("Disconnect") : t("Connect"),
          onclick: () => toggleTunnel(tun.name)
        }, icon("power")),
        h("div", { style: { minWidth: 0 } },
          h("h2", { class: "hero-title" }, tun.name),
          h("div", { class: "hero-state" },
            h("span", { class: "dot " + (tun.error ? "error" : tun.state) }),
            h("span", null, stateLabel(tun.state)),
            on && since ? h("span", { class: "muted", id: "uptime" }, "· " + fmtDuration(Date.now() - since)) : null,
            h("span", { class: "badge " + (tun.awg ? "accent" : "") }, tun.awg ? t("AmneziaWG obfuscation") : t("WireGuard")),
            h("span", { class: "badge" }, tun.fullTunnel ? t("Full tunnel") : t("Partial")),
            split && split.mode !== "off" ? h("span", { class: "badge accent" }, t("Split: {0}", split.mode === "include" ? t("Include only") : t("Exclude"))) : null,
            on && health ? h("span", { class: "badge " + (health.healthy ? "ok" : "bad") }, health.healthy ? t("Healthy") : t("Unhealthy"), health.restarts ? " · " + t("Restarted {0}×", health.restarts) : "") : null),
          tun.error ? h("div", { class: "callout bad", style: { marginTop: "10px" } }, icon("error"), h("span", { class: "selectable" }, tun.error)) : null,
          h("div", { class: "hero-actions" },
            h("button", { class: "btn" + (on ? "" : " primary"), onclick: () => toggleTunnel(tun.name), disabled: busy }, icon("power"), on ? t("Disconnect") : t("Connect")),
            h("button", { class: "btn", disabled: !state.info.isAdmin, onclick: () => editTunnel(tun.name) }, icon("edit"), t("Edit")),
            h("button", { class: "btn", onclick: () => { splitPage.open(tun.name); } }, icon("split"), t("Split tunneling")),
            h("button", { class: "btn", disabled: !state.info.isAdmin, onclick: () => shareDialog(tun.name) }, icon("share"), t("Share")),
            moreBtn = h("button", {
              class: "btn icon", title: t("More"), onclick: () => showMenu(moreBtn, [
                { icon: "file", label: t("Export") + " (.conf)", disabled: !state.info.isAdmin, action: () => exportConf(tun.name) },
                { icon: "copy", label: t("Duplicate"), disabled: !state.info.isAdmin, action: () => duplicateTunnel(tun.name) },
                { icon: "edit", label: t("Rename"), disabled: !state.info.isAdmin, action: () => renameTunnel(tun.name) },
                on ? { icon: "refresh", label: t("Restart"), action: () => call("tunnels.restart", { name: tun.name }).catch(toastError) } : null,
                "-",
                { icon: "trash", label: t("Delete"), danger: true, disabled: !state.info.isAdmin, action: () => deleteTunnel(tun.name) }
              ])
            }, icon("more"))))),
      h("div", { class: "stats" },
        statCard("download", t("Download"), "stat-rx", on && rt ? fmtRate(rt.rxRate) : "—", "stat-rx-total", on && rt ? fmtBytes(rt.rx) + " " + t("received") : ""),
        statCard("upload", t("Upload"), "stat-tx", on && rt ? fmtRate(rt.txRate) : "—", "stat-tx-total", on && rt ? fmtBytes(rt.tx) + " " + t("sent") : ""),
        statCard("handshake", t("Last handshake"), "stat-hs", on && rt ? fmtAgo(rt.lastHandshake) : "—", null, ""),
        statCard("server", t("Endpoint"), "stat-ep", (rt && on && rt.peers[0] && rt.peers[0].endpoint) || (tun.endpoints || [])[0] || "—", null, "", true)),
      on ? h("div", { class: "card", style: { marginTop: "16px" } },
        h("div", { class: "card-head" }, h("h3", null, t("Throughput")), h("span", { class: "sub" }, t("last 2 minutes")), h("div", { class: "spacer" }),
          h("div", { class: "legend" }, h("span", null, h("i", { style: { background: "var(--accent)" } }), t("Download")), h("span", null, h("i", { style: { background: "var(--info)" } }), t("Upload")))),
        h("div", { class: "card-body", id: "chart" }, chart(tun.name))) : null,
      h("div", { class: "grid-2", style: { marginTop: "16px" } },
        h("div", { class: "card" },
          h("div", { class: "card-head" }, h("h3", null, t("Interface"))),
          h("div", { class: "card-body" }, h("div", { class: "kv" },
            kv(t("Public key"), tun.publicKey, true),
            kv(t("Addresses"), (tun.addresses || []).join(", ")),
            kv(t("DNS servers"), (tun.dns || []).join(", ")),
            kv(t("Listen port"), (rt && on && rt.listenPort) || tun.listenPort || t("automatic")),
            tun.mtu ? kv("MTU", tun.mtu) : null))),
        h("div", { class: "card" },
          h("div", { class: "card-head" }, h("h3", null, t("Peers")), h("span", { class: "badge" }, String(tun.peers || 0))),
          h("div", null, peerList(tun, on ? rt : null))))
    ];
  }

  function statCard(ic, label, id, value, subId, sub, small) {
    return h("div", { class: "stat" },
      h("div", { class: "k" }, icon(ic), label),
      h("div", { class: "v" + (small ? " small" : ""), id, title: value }, value),
      h("div", { class: "s", id: subId }, sub || " "));
  }

  function kv(k, v, copy) {
    if (v == null || v === "") v = "—";
    return [h("div", { class: "k" }, k), h("div", { class: "v" + (copy ? " mono" : "") }, String(v), copy && v !== "—" ? copyButton(String(v)) : null)];
  }

  function peerList(tun, rt) {
    const peers = rt && rt.peers && rt.peers.length ? rt.peers : null;
    if (!peers) {
      return h("div", { class: "card-body" }, h("div", { class: "kv" },
        kv(t("Endpoint"), (tun.endpoints || []).join(", ")),
        kv(t("Allowed IPs"), (tun.allowedIps || []).join(", "))));
    }
    return peers.map(p => h("div", { class: "peer" },
      h("div", { class: "peer-head" }, h("span", { class: "mono ellipsis grow", title: p.publicKey }, p.publicKey), copyButton(p.publicKey)),
      h("div", { class: "muted", style: { fontSize: "12.5px", marginTop: "4px" } },
        [p.endpoint, fmtAgo(p.lastHandshake), "↓ " + fmtBytes(p.rx), "↑ " + fmtBytes(p.tx), p.keepalive ? t("Persistent keepalive") + " " + p.keepalive + "s" : null].filter(Boolean).join(" · "))));
  }

  function chart(name) {
    const hist = state.history[name] || [];
    const W = 600, H = 120;
    const svg = document.createElementNS("http://www.w3.org/2000/svg", "svg");
    svg.setAttribute("viewBox", `0 0 ${W} ${H}`);
    svg.setAttribute("preserveAspectRatio", "none");
    svg.setAttribute("class", "chart");
    const pts = hist.slice(-120);
    const n = Math.min(120, Math.max(30, pts.length));
    const max = Math.max(1024, ...pts.map(p => Math.max(p.rx, p.tx)));
    const xy = (i, v) => [(W * (i + (n - pts.length))) / (n - 1), H - 4 - (H - 10) * (v / max)];
    const path = key => pts.map((p, i) => { const [x, y] = xy(i, p[key]); return (i ? "L" : "M") + x.toFixed(1) + " " + y.toFixed(1); }).join(" ");
    if (pts.length > 1) {
      const area = document.createElementNS(svg.namespaceURI, "path");
      const first = xy(0, 0)[0], last = xy(pts.length - 1, 0)[0];
      area.setAttribute("d", path("rx") + ` L${last} ${H} L${first} ${H} Z`);
      area.setAttribute("class", "rx-area");
      svg.appendChild(area);
      for (const key of ["tx", "rx"]) {
        const p = document.createElementNS(svg.namespaceURI, "path");
        p.setAttribute("d", path(key));
        p.setAttribute("class", key);
        svg.appendChild(p);
      }
    }
    return svg;
  }

  // Runtime polling for the selected tunnel.
  async function pollRuntime() {
    const tun = tunnelByName(state.selected);
    if (!tun || tun.state !== "started" || state.page !== "tunnels") return;
    try {
      const rt = await call("tunnels.runtime", { name: tun.name });
      const prev = state.runtime[tun.name];
      const dt = prev ? Math.max(0.2, (rt.now - prev.now) / 1000) : 0;
      rt.rxRate = prev && rt.rx >= prev.rx ? (rt.rx - prev.rx) / dt : 0;
      rt.txRate = prev && rt.tx >= prev.tx ? (rt.tx - prev.tx) / dt : 0;
      state.runtime[tun.name] = rt;
      if (prev) {
        const hist = state.history[tun.name] || (state.history[tun.name] = []);
        hist.push({ rx: rt.rxRate, tx: rt.txRate });
        if (hist.length > 120) hist.splice(0, hist.length - 120);
      }
      if (!state.since[tun.name] && rt.lastHandshake) state.since[tun.name] = Math.min(Date.now(), rt.lastHandshake);
      updateLiveStats(tun.name, !prev);
    } catch (e) { /* tunnel may be going down */ }
  }

  function updateLiveStats(name, full) {
    if (state.selected !== name || state.page !== "tunnels") return;
    if (full || !document.getElementById("chart")) { renderDetail(); return; }
    const rt = state.runtime[name];
    const set = (id, v) => { const el = document.getElementById(id); if (el) { el.textContent = v; el.title = v; } };
    set("stat-rx", fmtRate(rt.rxRate));
    set("stat-tx", fmtRate(rt.txRate));
    set("stat-rx-total", fmtBytes(rt.rx) + " " + t("received"));
    set("stat-tx-total", fmtBytes(rt.tx) + " " + t("sent"));
    set("stat-hs", fmtAgo(rt.lastHandshake));
    if (rt.peers[0] && rt.peers[0].endpoint) set("stat-ep", rt.peers[0].endpoint);
    if (state.since[name]) set("uptime", "· " + fmtDuration(Date.now() - state.since[name]));
    const c = document.getElementById("chart");
    if (c) { c.innerHTML = ""; c.appendChild(chart(name)); }
  }

  async function deleteTunnel(name) {
    const ok = await confirmDialog({ title: t("Delete tunnel?"), message: t("“{0}” will be removed from this computer. This cannot be undone.", name), ok: t("Delete"), danger: true });
    if (!ok) return;
    try {
      await call("tunnels.delete", { names: [name] });
      delete state.splitCache[name];
      state.selected = null;
      await loadTunnels();
    } catch (e) { toastError(e); }
  }

  async function renameTunnel(name) {
    const to = await promptDialog({ title: t("Rename tunnel"), label: t("New name"), value: name, validate: validName });
    if (!to || to === name) return;
    try {
      await call("tunnels.rename", { from: name, to });
      state.selected = to;
      state.splitCache[to] = state.splitCache[name];
      delete state.splitCache[name];
      await loadTunnels();
    } catch (e) { toastError(e); }
  }

  function validName(v) {
    if (!/^[a-zA-Z0-9_=+.-]{1,32}$/.test(v)) return t("Invalid: {0}", "a-z 0-9 _ = + . - (≤32)");
    return null;
  }

  async function duplicateTunnel(name) {
    try {
      const r = await call("tunnels.duplicate", { name });
      state.selected = r.name;
      await loadTunnels();
    } catch (e) { toastError(e); }
  }

  async function exportConf(name) {
    try {
      const r = await call("tunnels.exportConf", { name });
      if (r) toast("ok", t("Exported to {0}", r.path));
    } catch (e) { toastError(e); }
  }

  async function exportZip(names) {
    try {
      const r = await call("tunnels.exportZip", { names });
      if (r) toast("ok", t("Exported to {0}", r.path));
    } catch (e) { toastError(e); }
  }

  // ------------------------------------------------------------------
  // Import
  // ------------------------------------------------------------------

  function reportImport(r) {
    if (!r) return;
    if (r.imported && r.imported.length) {
      toast("ok", t("Imported {0}", r.imported.join(", ")));
      state.selected = r.imported[0];
    }
    if (r.errors && r.errors.length) {
      toast("bad", r.imported && r.imported.length ? t("Some tunnels could not be imported") : t("Import failed"), r.errors.join("\n"));
    }
    loadTunnels();
  }

  function readFiles(files) {
    return Promise.all(Array.from(files).map(f => new Promise((resolve, reject) => {
      const reader = new FileReader();
      reader.onload = () => resolve({ name: f.name, data: String(reader.result).split(",")[1] || "" });
      reader.onerror = () => reject(reader.error);
      reader.readAsDataURL(f);
    })));
  }

  async function importFileList(files) {
    if (!files || !files.length) return;
    try {
      reportImport(await call("tunnels.importData", { files: await readFiles(files) }));
    } catch (e) { toastError(e, t("Import failed")); }
  }

  function importDialog(tab) {
    if (!state.info.isAdmin) return;
    let current = tab || "file";
    let textArea, nameInput;
    const body = h("div");
    const draw = () => {
      body.innerHTML = "";
      append(body, [
        h("div", { class: "tabs" },
          h("button", { class: current === "file" ? "active" : "", onclick: () => { current = "file"; draw(); } }, t("From file")),
          h("button", { class: current === "text" ? "active" : "", onclick: () => { current = "text"; draw(); } }, t("Paste text"))),
        current === "file" ? fileTab() : textTab()
      ]);
    };
    const fileTab = () => {
      const zone = h("div", { class: "dropzone" },
        icon("file"),
        h("div", { style: { color: "var(--text)", fontWeight: 600 } }, t("Drop .conf, .zip or .vpn files here")),
        h("div", null, t("or")),
        h("button", { class: "btn primary", onclick: async () => { try { const r = await call("tunnels.importFiles"); if (r) { m.close(); reportImport(r); } } catch (e) { toastError(e); } } }, t("Browse…")));
      zone.addEventListener("dragover", e => { e.preventDefault(); e.stopPropagation(); zone.classList.add("over"); });
      zone.addEventListener("dragleave", () => zone.classList.remove("over"));
      zone.addEventListener("drop", e => { e.preventDefault(); e.stopPropagation(); zone.classList.remove("over"); m.close(); importFileList(e.dataTransfer.files); });
      return zone;
    };
    const textTab = () => h("div", { class: "vstack" },
      h("div", { class: "muted" }, t("Paste a WireGuard/AmneziaWG configuration or one or more AmneziaVPN vpn:// keys.")),
      textArea = h("textarea", { class: "textarea", rows: "11", spellcheck: "false", placeholder: "[Interface]\nPrivateKey = …\n\nvpn://…", autofocus: true }),
      h("div", { class: "hstack" },
        nameInput = h("input", { class: "input grow", placeholder: t("Name (optional)") }),
        h("button", { class: "btn", onclick: async () => { try { textArea.value = (await call("clipboard.read")).text || ""; } catch (e) { toastError(e); } } }, icon("clipboard"), t("Paste from clipboard"))));
    const m = modal({
      title: t("Import tunnels"),
      body,
      size: "wide",
      foot: [
        h("button", { class: "btn", onclick: () => m.close() }, t("Cancel")),
        h("button", {
          class: "btn primary", onclick: async () => {
            if (current !== "text") return;
            const text = textArea.value.trim();
            if (!text) return;
            try {
              const r = await call("tunnels.importText", { name: nameInput.value.trim(), text });
              m.close();
              reportImport(r);
            } catch (e) { toastError(e, t("Import failed")); }
          }
        }, t("Import"))
      ]
    });
    draw();
  }

  // Drop files anywhere to import.
  let dragDepth = 0, dropOverlay = null;
  window.addEventListener("dragenter", e => {
    if (!e.dataTransfer || !Array.from(e.dataTransfer.types || []).includes("Files")) return;
    e.preventDefault();
    dragDepth++;
    if (!dropOverlay && state.info.isAdmin && !modalStack.length) {
      dropOverlay = h("div", { class: "drop-overlay" }, t("Drop to import tunnels"));
      document.body.appendChild(dropOverlay);
    }
  });
  window.addEventListener("dragleave", () => {
    dragDepth = Math.max(0, dragDepth - 1);
    if (!dragDepth && dropOverlay) { dropOverlay.remove(); dropOverlay = null; }
  });
  window.addEventListener("dragover", e => e.preventDefault());
  window.addEventListener("drop", e => {
    e.preventDefault();
    dragDepth = 0;
    if (dropOverlay) { dropOverlay.remove(); dropOverlay = null; }
    if (state.info.isAdmin && !modalStack.length && e.dataTransfer && e.dataTransfer.files.length) importFileList(e.dataTransfer.files);
  });

  // ------------------------------------------------------------------
  // Editor
  // ------------------------------------------------------------------

  const AWG_KEYS = new Set(["jc", "jmin", "jmax", "s1", "s2", "s3", "s4", "h1", "h2", "h3", "h4", "i1", "i2", "i3", "i4", "i5", "j1", "j2", "j3", "itime"]);
  const SECRET_KEYS = new Set(["privatekey", "presharedkey"]);

  function escapeHTML(s) {
    return s.replace(/[&<>"]/g, c => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]);
  }

  function highlight(text) {
    return text.split("\n").map(line => {
      const trimmed = line.trim();
      if (trimmed.startsWith("#")) return '<span class="tok-comment">' + escapeHTML(line) + "</span>";
      if (/^\s*\[[^\]]*\]\s*$/.test(line)) return '<span class="tok-section">' + escapeHTML(line) + "</span>";
      const m = /^(\s*)([^=#]+?)(\s*=\s*)([^#]*)(#.*)?$/.exec(line);
      if (!m) return escapeHTML(line);
      const key = m[2].trim().toLowerCase();
      const cls = AWG_KEYS.has(key) ? "tok-awg" : "tok-key";
      const vcls = SECRET_KEYS.has(key) ? "tok-secret" : "tok-value";
      return escapeHTML(m[1]) + '<span class="' + cls + '">' + escapeHTML(m[2]) + '</span><span class="tok-eq">' + escapeHTML(m[3]) + '</span><span class="' + vcls + '">' + escapeHTML(m[4]) + "</span>" + (m[5] ? '<span class="tok-comment">' + escapeHTML(m[5]) + "</span>" : "");
    }).join("\n") + "\n";
  }

  // Kill switch helpers mirror the classic UI: a single peer with 0.0.0.0/0
  // turns the firewall on; 0.0.0.0/1 + 128.0.0.0/1 routes everything
  // without it.
  function killSwitchState(text) {
    const peers = (text.match(/^\s*\[peer\]\s*$/gim) || []).length;
    if (peers !== 1) return null;
    const m = /^\s*allowedips\s*=\s*(.*)$/im.exec(text);
    if (!m) return null;
    const ips = m[1].split(",").map(s => s.trim());
    if (ips.includes("0.0.0.0/0")) return true;
    if (ips.includes("0.0.0.0/1") && ips.includes("128.0.0.0/1")) return false;
    return null;
  }

  function setKillSwitch(text, on) {
    return text.replace(/^(\s*allowedips\s*=\s*)(.*)$/im, (all, pre, list) => {
      let ips = list.split(",").map(s => s.trim()).filter(Boolean);
      const swap = (from, to) => {
        const idx = ips.findIndex(x => from.includes(x));
        ips = ips.filter(x => !from.includes(x));
        ips.splice(idx < 0 ? ips.length : idx, 0, ...to);
      };
      if (on) {
        if (ips.includes("0.0.0.0/1")) swap(["0.0.0.0/1", "128.0.0.0/1"], ["0.0.0.0/0"]);
        if (ips.includes("::/1")) swap(["::/1", "8000::/1"], ["::/0"]);
      } else {
        if (ips.includes("0.0.0.0/0")) swap(["0.0.0.0/0"], ["0.0.0.0/1", "128.0.0.0/1"]);
        if (ips.includes("::/0")) swap(["::/0"], ["::/1", "8000::/1"]);
      }
      return pre + ips.join(", ");
    });
  }

  async function editTunnel(name) {
    if (!state.info.isAdmin) return;
    let original = "", origName = name || "";
    try {
      if (name) original = (await call("tunnels.get", { name })).text;
      else {
        const tpl = await call("tunnels.template");
        original = tpl.text;
        origName = "";
        name = tpl.name;
      }
    } catch (e) { toastError(e); return; }
    const running = name && origName && ["started", "starting"].includes((tunnelByName(origName) || {}).state);

    let textarea, pre, gutter, statusEl, pubEl, killEl, nameInput;
    const sync = () => {
      pre.innerHTML = highlight(textarea.value);
      const lines = textarea.value.split("\n").length;
      gutter.textContent = Array.from({ length: lines }, (_, i) => i + 1).join("\n");
      textarea.style.height = "0";
      textarea.style.height = Math.max(textarea.scrollHeight, textarea.parentElement.clientHeight) + "px";
      textarea.style.width = "0";
      textarea.style.width = Math.max(textarea.scrollWidth, textarea.parentElement.clientWidth) + "px";
      const ks = killSwitchState(textarea.value);
      killEl.querySelector("input").checked = ks === true;
      killEl.querySelector("input").disabled = ks === null;
      validateSoon();
      derivePublicSoon();
    };
    const validateSoon = debounce(async () => {
      statusEl.className = "editor-status";
      statusEl.textContent = t("Checking…");
      try {
        const r = await call("tunnels.validate", { name: nameInput.value.trim(), text: textarea.value });
        statusEl.innerHTML = "";
        if (r.ok) { statusEl.className = "editor-status ok"; append(statusEl, [icon("check"), t("Configuration is valid")]); }
        else { statusEl.className = "editor-status bad"; append(statusEl, [icon("error"), h("span", { class: "selectable" }, r.error)]); }
      } catch (e) { statusEl.textContent = e.message; }
    }, 350);
    const derivePublicSoon = debounce(async () => {
      const m = /^\s*privatekey\s*=\s*(\S+)/im.exec(textarea.value);
      if (!m) { pubEl.textContent = "—"; return; }
      try { pubEl.textContent = (await call("keys.public", { private: m[1] })).public; }
      catch (e) { pubEl.textContent = "—"; }
    }, 300);

    const insert = snippet => {
      const v = textarea.value.replace(/\s*$/, "");
      textarea.value = v + "\n\n" + snippet + "\n";
      sync();
      textarea.focus();
    };

    const editorEl = h("div", { class: "editor" },
      gutter = h("div", { class: "gutter" }, "1"),
      h("div", { class: "surface", onscroll: e => { gutter.scrollTop = e.target.scrollTop; } },
        pre = h("pre", { "aria-hidden": "true" }),
        textarea = h("textarea", {
          spellcheck: "false", autocapitalize: "off", autocomplete: "off", wrap: "off",
          oninput: sync,
          onkeydown: e => {
            if (e.key === "Tab") {
              e.preventDefault();
              const s = textarea.selectionStart;
              textarea.setRangeText("    ", s, textarea.selectionEnd, "end");
              sync();
            }
          }
        })));
    textarea.value = original;

    const body = h("div", { style: { display: "grid", gridTemplateColumns: "1fr 260px", gap: "18px", height: "100%" } },
      h("div", { class: "vstack", style: { minHeight: 0, height: "100%" } },
        h("label", { class: "muted" }, t("Name")),
        nameInput = h("input", { class: "input", value: name, maxlength: "32", oninput: validateSoon }),
        h("label", { class: "muted", style: { marginTop: "6px" } }, t("Configuration")),
        editorEl,
        statusEl = h("div", { class: "editor-status" })),
      h("div", { class: "vstack" },
        h("div", { class: "card", style: { padding: "14px" } },
          h("div", { class: "muted", style: { fontSize: "12px" } }, t("Public key")),
          h("div", { class: "hstack" }, pubEl = h("div", { class: "mono grow", style: { wordBreak: "break-all" } }, "—"),
            h("button", { class: "btn ghost icon small", title: t("Copy"), onclick: () => pubEl.textContent !== "—" && copyText(pubEl.textContent) }, icon("copy"))),
          h("button", {
            class: "btn small", style: { marginTop: "10px", width: "100%" }, onclick: async () => {
              if (!await confirmDialog({ title: t("New keypair"), message: t("Generate a new private key? The server must be updated with the new public key."), ok: t("New keypair") })) return;
              const k = await call("keys.generate");
              if (/^\s*privatekey\s*=.*$/im.test(textarea.value)) textarea.value = textarea.value.replace(/^(\s*privatekey\s*=\s*).*$/im, "$1" + k.private);
              else textarea.value = textarea.value.replace(/^(\s*\[interface\]\s*)$/im, "$1\nPrivateKey = " + k.private);
              sync();
            }
          }, icon("key"), t("New keypair"))),
        h("div", { class: "card", style: { padding: "14px" } },
          killEl = h("label", { class: "hstack", style: { alignItems: "flex-start", cursor: "pointer" } },
            h("input", { type: "checkbox", style: { marginTop: "3px" }, onchange: e => { textarea.value = setKillSwitch(textarea.value, e.target.checked); sync(); } }),
            h("span", null, t("Block untunneled traffic (kill switch)"), h("div", { class: "muted", style: { fontSize: "12px" } }, t("Only available for a single peer routing 0.0.0.0/0."))))),
        h("div", { class: "card", style: { padding: "14px" } },
          h("div", { class: "muted", style: { fontSize: "12px", marginBottom: "8px" } }, t("Insert")),
          h("div", { class: "vstack" },
            h("button", { class: "btn small", onclick: () => insert("[Peer]\nPublicKey = \nAllowedIPs = 0.0.0.0/0, ::/0\nEndpoint = \nPersistentKeepalive = 25") }, icon("plus"), t("Peer section")),
            h("button", {
              class: "btn small", onclick: () => {
                if (/^\s*jc\s*=/im.test(textarea.value)) return;
                const rnd = (a, b) => a + Math.floor(Math.random() * (b - a + 1));
                const block = `Jc = ${rnd(3, 6)}\nJmin = 40\nJmax = 70\nS1 = ${rnd(15, 150)}\nS2 = ${rnd(15, 150)}\nH1 = ${rnd(5, 2147483647)}\nH2 = ${rnd(5, 2147483647)}\nH3 = ${rnd(5, 2147483647)}\nH4 = ${rnd(5, 2147483647)}`;
                textarea.value = textarea.value.replace(/^(\s*\[peer\]\s*)$/im, block + "\n\n$1");
                if (!/^\s*jc\s*=/im.test(textarea.value)) textarea.value += "\n" + block;
                sync();
              }
            }, icon("sparkle"), t("AmneziaWG parameters")))),
        running ? h("div", { class: "callout" }, icon("info"), h("span", null, t("The tunnel is running and will reconnect to apply changes."))) : null));

    let saving = false;
    const save = async () => {
      if (saving) return;
      const newName = nameInput.value.trim();
      const problem = validName(newName);
      if (problem) { nameInput.classList.add("invalid"); toast("bad", problem); return; }
      saving = true;
      try {
        const r = await call("tunnels.save", { name: newName, originalName: origName, text: textarea.value });
        original = textarea.value;
        origName = newName;
        m.close("saved");
        toast("ok", t("Saved"));
        state.selected = r.name;
        delete state.splitCache[r.name];
        loadTunnels();
      } catch (e) { toastError(e); } finally { saving = false; }
    };

    const m = modal({
      title: origName ? t("Edit tunnel") : t("New tunnel"),
      body,
      size: "full",
      foot: [h("div", { class: "spacer" }), h("span", { class: "muted", style: { fontSize: "12px" } }, h("span", { class: "kbd" }, "Ctrl"), " + ", h("span", { class: "kbd" }, "S")),
        h("button", { class: "btn", onclick: () => m.close() }, t("Cancel")),
        h("button", { class: "btn primary", onclick: save }, icon("save"), t("Save"))],
      onClose: result => {
        if (result === "saved" || (textarea.value === original && nameInput.value.trim() === (origName || name))) return true;
        confirmDialog({ title: t("Discard changes?"), message: t("You have unsaved changes."), ok: t("Discard"), danger: true }).then(ok => {
          if (ok) { original = textarea.value; origName = nameInput.value.trim(); name = origName; m.close("saved"); }
        });
        return false;
      }
    });
    m.box.querySelector(".modal-body").style.overflow = "hidden";
    m.box.addEventListener("keydown", e => {
      if (e.ctrlKey && e.key.toLowerCase() === "s") { e.preventDefault(); save(); }
    });
    requestAnimationFrame(() => {
      sync();
      textarea.focus({ preventScroll: true });
      textarea.setSelectionRange(0, 0);
      const surface = textarea.parentElement;
      setTimeout(() => { surface.scrollTop = 0; surface.scrollLeft = 0; gutter.scrollTop = 0; }, 60);
    });
  }

  // ------------------------------------------------------------------
  // Share
  // ------------------------------------------------------------------

  async function shareDialog(name) {
    let format = "vpn";
    let data;
    try { data = await call("tunnels.shareKey", { name }); } catch (e) { toastError(e); return; }
    const body = h("div");
    const draw = async () => {
      body.innerHTML = "";
      const text = format === "vpn" ? data.key : data.text;
      const qrBox = h("div", { class: "qr" }, h("div", { class: "skeleton", style: { width: "100%", height: "100%" } }));
      append(body, [
        h("div", { style: { display: "flex", justifyContent: "center", marginBottom: "14px" } },
          h("div", { class: "segmented" },
            h("button", { class: format === "vpn" ? "active" : "", onclick: () => { format = "vpn"; draw(); } }, icon("key"), t("vpn:// key")),
            h("button", { class: format === "conf" ? "active" : "", onclick: () => { format = "conf"; draw(); } }, icon("file"), t("Config file")))),
        qrBox,
        h("p", { class: "muted", style: { textAlign: "center", fontSize: "12.5px", margin: "12px 0" } }, t("Scan with AmneziaVPN, AmneziaWG or WG Tunnel on your phone. Anyone with this code can use the tunnel.")),
        h("textarea", { class: "textarea", rows: format === "vpn" ? "3" : "8", readonly: true, spellcheck: "false", style: { width: "100%" } }, text)
      ]);
      try {
        const qr = await call("tunnels.qr", { name, format });
        qrBox.innerHTML = qr.svg;
      } catch (e) {
        qrBox.innerHTML = "";
        qrBox.style.background = "transparent";
        append(qrBox, h("div", { class: "callout warn" }, icon("alert"), h("span", null, e.message)));
      }
    };
    const m = modal({
      title: t("Share tunnel") + " · " + name,
      body,
      foot: [
        h("button", { class: "btn", onclick: () => exportConf(name) }, icon("save"), t("Save as file…")),
        h("div", { class: "spacer" }),
        h("button", { class: "btn primary", onclick: () => copyText(format === "vpn" ? data.key : data.text) }, icon("copy"), format === "vpn" ? t("Copy key") : t("Copy config"))
      ]
    });
    draw();
  }

  // ------------------------------------------------------------------
  // Split tunneling page
  // ------------------------------------------------------------------

  const DOMAIN_PRESETS = [
    { name: "YouTube", domains: ["*.youtube.com", "*.googlevideo.com", "*.ytimg.com", "*.ggpht.com", "youtu.be", "*.youtube-nocookie.com", "*.youtubei.googleapis.com"] },
    { name: "Discord", domains: ["*.discord.com", "*.discord.gg", "*.discordapp.com", "*.discordapp.net", "*.discord.media", "*.discordcdn.com"] },
    { name: "Telegram", domains: ["*.telegram.org", "*.t.me", "*.telegra.ph", "*.telesco.pe", "*.tdesktop.com"], ips: ["91.108.4.0/22", "91.108.8.0/22", "91.108.12.0/22", "91.108.16.0/22", "91.108.56.0/22", "149.154.160.0/20", "95.161.64.0/20", "2001:67c:4e8::/48", "2001:b28:f23d::/48", "2001:b28:f23f::/48"] },
    { name: "Instagram", domains: ["*.instagram.com", "*.cdninstagram.com", "*.fbcdn.net", "*.facebook.com", "*.fbsbx.com"] },
    { name: "X / Twitter", domains: ["*.x.com", "*.twitter.com", "*.twimg.com", "*.t.co"] },
    { name: "ChatGPT", domains: ["*.openai.com", "*.chatgpt.com", "*.oaistatic.com", "*.oaiusercontent.com"] },
    { name: "Claude", domains: ["*.claude.ai", "*.anthropic.com"] },
    { name: "Netflix", domains: ["*.netflix.com", "*.nflxvideo.net", "*.nflximg.net", "*.nflxext.com", "*.nflxso.net"] },
    { name: "Spotify", domains: ["*.spotify.com", "*.scdn.co", "*.spotifycdn.com"] },
    { name: "Twitch", domains: ["*.twitch.tv", "*.ttvnw.net", "*.jtvnw.net", "*.twitchcdn.net"] },
    { name: "Steam", domains: ["*.steampowered.com", "*.steamcommunity.com", "*.steamstatic.com", "*.steamcontent.com", "*.steamserver.net"] },
    { name: "LinkedIn", domains: ["*.linkedin.com", "*.licdn.com"] }
  ];

  const DNS_PRESETS = [
    { name: "Cloudflare DoH", value: "https://cloudflare-dns.com/dns-query#1.1.1.1,1.0.0.1" },
    { name: "Google DoH", value: "https://dns.google/dns-query#8.8.8.8,8.8.4.4" },
    { name: "Quad9 DoT", value: "tls://dns.quad9.net#9.9.9.9,149.112.112.112" },
    { name: "AdGuard DoH", value: "https://dns.adguard-dns.com/dns-query#94.140.14.14,94.140.15.15" },
    { name: "Cloudflare", value: "1.1.1.1" }
  ];

  function isValidPrefix(s) {
    s = s.trim();
    const [addr, bits] = s.split("/");
    const v4 = /^(25[0-5]|2[0-4]\d|1?\d?\d)(\.(25[0-5]|2[0-4]\d|1?\d?\d)){3}$/.test(addr);
    const v6 = !v4 && /^[0-9a-f:.]+$/i.test(addr.replace(/^\[|\]$/g, "")) && addr.includes(":");
    if (!v4 && !v6) return false;
    if (bits === undefined) return true;
    const n = Number(bits);
    return /^\d+$/.test(bits) && n >= 0 && n <= (v4 ? 32 : 128);
  }

  function isValidDomain(s) {
    s = s.trim().toLowerCase().replace(/\.$/, "");
    if (!s || s === "*" || s === "=") return false;
    s = s.replace(/^=/, "");
    return s.split(".").every(l => l.length > 0 && /^[a-z0-9_*\-\u0080-￿]+$/.test(l));
  }

  const splitPage = (() => {
    let tunnelName = null, original = null, draft = null, loading = false, error = null;
    const appMeta = {};

    function dirty() { return draft && original && !same(draft, original); }

    function confirmLeave(proceed) {
      confirmDialog({ title: t("Discard changes?"), message: t("You have unsaved changes."), ok: t("Discard"), danger: true }).then(ok => {
        if (ok) { draft = clone(original); proceed(); }
      });
      return false;
    }

    async function load(name) {
      tunnelName = name;
      loading = true;
      error = null;
      draft = original = null;
      render();
      try {
        const c = await getSplit(name, true);
        c.ips = c.ips || [];
        c.domains = c.domains || [];
        c.apps = c.apps || [];
        c.dns = c.dns || {};
        c.dns.upstreams = c.dns.upstreams || [];
        c.proxy = c.proxy || {};
        original = c;
        draft = clone(c);
        describeApps(c.apps.map(a => a.path));
      } catch (e) { error = e.message; }
      loading = false;
      if (state.page === "split") render();
    }

    async function describeApps(paths) {
      const missing = paths.filter(p => !appMeta[p.toLowerCase()]);
      if (!missing.length) return;
      let infos;
      try {
        infos = await call("apps.describe", { paths: missing });
      } catch (e) { return; /* icons are cosmetic */ }
      infos.forEach(i => { appMeta[i.path.toLowerCase()] = i; });
      if (state.page === "split") renderAppsCard();
    }

    function open(name) {
      if (state.page === "split" && tunnelName === name) return;
      const go = () => { state.page = "split"; load(name); };
      if (dirty() && tunnelName !== name) { confirmLeave(go); return; }
      go();
    }

    function changed() {
      const bar = document.getElementById("savebar");
      if (bar) bar.classList.toggle("show", !!dirty());
    }

    async function save() {
      try {
        const v = await call("split.validate", { name: tunnelName, config: draft });
        if (!v.ok) { toast("bad", t("Invalid: {0}", v.error)); return; }
        const r = await call("split.set", { name: tunnelName, config: draft });
        original = clone(draft);
        state.splitCache[tunnelName] = clone(draft);
        toast("ok", t("Split tunneling saved"), r && r.restarted ? t("Reconnecting {0} to apply.", tunnelName) : null);
        render();
      } catch (e) { toastError(e); }
    }

    function renderPage() {
      if (!state.tunnels.length) {
        return [pageHeader(t("Split tunneling"), t("Choose which apps, addresses and websites use the tunnel.")),
          h("div", { class: "page-body" }, h("div", { class: "empty" }, h("div", { class: "art" }, icon("split")), h("p", null, t("Create a tunnel first."))))];
      }
      if (!tunnelName || !tunnelByName(tunnelName)) {
        const pick = tunnelByName(state.selected) ? state.selected : state.tunnels[0].name;
        setTimeout(() => load(pick));
        return [pageHeader(t("Split tunneling")), h("div", { class: "page-body" }, t("Loading…"))];
      }
      const header = pageHeader(t("Split tunneling"), t("Choose which apps, addresses and websites use the tunnel."),
        h("div", { class: "hstack" }, h("span", { class: "muted" }, t("Tunnel")),
          selectEl(tunnelName, state.tunnels.map(x => ({ value: x.name, label: x.name })), v => open(v), { style: { minWidth: "180px" } })));
      if (loading || !draft) return [header, h("div", { class: "page-body" }, error ? h("div", { class: "callout bad" }, icon("error"), error) : t("Loading…"))];
      const ro = !state.info.isAdmin;
      return [header, h("div", { class: "page-body", "data-scroll": "split" },
        readOnlyNotice(),
        modeCards(),
        h("div", { id: "apps-card", style: { marginTop: "16px" } }, appsCard()),
        h("div", { class: "section-title" }, draft.mode === "include" ? t("Sent through the VPN") : draft.mode === "exclude" ? t("Kept outside the VPN") : t("Addresses and websites")),
        draft.mode === "off" ? h("div", { class: "callout" }, icon("info"), h("span", null, t("Pick “Exclude” or “Include only” above to route addresses and websites separately."))) : [ipsCard(), domainsCard()],
        h("div", { class: "section-title" }, t("DNS and proxy")),
        dnsCard(),
        proxyCard(),
        h("div", { class: "savebar" + (dirty() ? " show" : ""), id: "savebar" },
          h("div", { class: "msg" }, h("b", null, t("Unsaved changes")), " · ",
            ["started", "starting"].includes((tunnelByName(tunnelName) || {}).state) ? t("Saving will reconnect “{0}”.", tunnelName) : ""),
          h("button", { class: "btn", onclick: () => { draft = clone(original); render(); } }, t("Discard")),
          h("button", { class: "btn primary", disabled: ro, onclick: save }, icon("save"), t("Save"))))];
    }

    function modeCards() {
      const modes = [
        { id: "off", icon: "globe", title: t("Off"), desc: t("Route exactly what the tunnel configuration says.") },
        { id: "exclude", icon: "route", title: t("Exclude"), desc: t("Everything goes through the VPN except the addresses and sites below.") },
        { id: "include", icon: "split", title: t("Include only"), desc: t("Only the addresses and sites below go through the VPN.") }
      ];
      return h("div", { class: "mode-cards" }, modes.map(m => h("button", {
        class: "mode-card" + (draft.mode === m.id ? " active" : ""),
        onclick: () => { draft.mode = m.id; render(); }
      }, h("div", { class: "icon" }, icon(m.icon)), h("div", { class: "t" }, m.title), h("div", { class: "d" }, m.desc))));
    }

    function renderAppsCard() {
      const el = document.getElementById("apps-card");
      if (!el) return;
      el.innerHTML = "";
      append(el, appsCard());
    }

    function appsCard() {
      const needsDriver = draft.apps.some(a => a.action === "bypass" && a.enabled !== false) && !state.status.driverAvailable;
      return h("div", { class: "card" },
        h("div", { class: "card-head" },
          h("div", null, h("h3", null, t("Applications")), h("div", { class: "sub" }, t("Per-app rules work in every mode."))),
          h("div", { class: "spacer" }),
          h("button", { class: "btn small", onclick: pickRunning }, icon("play"), t("Running apps")),
          h("button", { class: "btn small", onclick: addFolder }, icon("folder"), t("Add folder")),
          h("button", { class: "btn small primary", onclick: addApps }, icon("plus"), t("Add app"))),
        needsDriver ? h("div", { class: "card-body", style: { paddingBottom: "6px" } }, h("div", { class: "callout warn" }, icon("alert"),
          h("div", null, h("b", null, t("Bypass needs the split tunnel driver")), h("div", null, t("Windows cannot route a single app around a VPN without a kernel driver. Place mullvad-split-tunnel.sys next to amneziawg.exe or pick it in Settings → Split tunnel driver. Without it, bypass rules are ignored; “VPN only” and “Block” always work."))))) : null,
        h("div", { style: { paddingTop: "8px" } },
          draft.apps.length ? draft.apps.map((a, i) => appRow(a, i)) : h("div", { class: "muted", style: { padding: "8px 18px 18px" } }, t("No app rules yet."))));
    }

    function appRow(a, i) {
      const meta = appMeta[a.path.toLowerCase()];
      const on = a.enabled !== false;
      return h("div", { class: "app-row" + (on ? "" : " off") },
        h("div", { class: "app-icon" }, meta && meta.icon ? h("img", { src: meta.icon, alt: "" }) : icon(a.folder ? "folder" : "app")),
        h("div", { class: "meta" },
          h("div", { class: "name" }, (meta && meta.name) || a.path.split(/[\\/]/).pop(), a.folder ? h("span", { class: "badge", style: { marginLeft: "8px" } }, t("Folder")) : null),
          h("div", { class: "path", title: a.path }, "‎" + a.path + (a.folder ? " — " + t("all programs inside") : ""))),
        selectEl(a.action, [
          { value: "bypass", label: t("Bypass VPN") },
          { value: "vpnonly", label: t("VPN only") },
          { value: "block", label: t("Block") }
        ], v => { a.action = v; renderAppsCard(); changed(); }, { style: { width: "160px" } }),
        switchEl(on, v => { a.enabled = v ? undefined : false; if (v) delete a.enabled; renderAppsCard(); changed(); }, { title: t("Enabled") }),
        h("button", { class: "btn ghost icon small", title: t("Remove"), onclick: () => { draft.apps.splice(i, 1); renderAppsCard(); changed(); } }, icon("trash")));
    }

    function addAppInfos(infos, folder) {
      for (const info of infos || []) {
        if (!info || !info.path) continue;
        appMeta[info.path.toLowerCase()] = info;
        if (draft.apps.some(a => a.path.toLowerCase() === info.path.toLowerCase())) continue;
        draft.apps.push({ path: info.path, folder: folder || undefined, action: draft.mode === "include" ? "vpnonly" : "bypass" });
      }
      renderAppsCard();
      changed();
    }

    async function addApps() {
      try { addAppInfos(await call("dialog.pickApps")); } catch (e) { toastError(e); }
    }

    async function addFolder() {
      try { const f = await call("dialog.pickFolder"); if (f) addAppInfos([f], true); } catch (e) { toastError(e); }
    }

    async function pickRunning() {
      const chosen = new Set();
      let all = [], q = "";
      const list = h("div", { class: "picker-list" }, h("div", { class: "muted", style: { padding: "14px" } }, t("Loading…")));
      const draw = () => {
        list.innerHTML = "";
        const items = all.filter(a => !q || a.name.toLowerCase().includes(q) || a.path.toLowerCase().includes(q));
        append(list, items.map(a => h("div", {
          class: "app-row" + (chosen.has(a.path) ? " checked" : ""),
          onclick: () => { chosen.has(a.path) ? chosen.delete(a.path) : chosen.add(a.path); draw(); addBtn.textContent = t("Add {0}", chosen.size); }
        }, h("span", { class: "check" }, icon("check")),
          h("div", { class: "app-icon" }, a.icon ? h("img", { src: a.icon, alt: "" }) : icon("app")),
          h("div", { class: "meta" }, h("div", { class: "name" }, a.name), h("div", { class: "path" }, "‎" + a.path)))));
      };
      let addBtn;
      const m = modal({
        title: t("Pick running apps"),
        size: "wide",
        body: h("div", null,
          h("div", { class: "search" }, icon("search"), h("input", { class: "input", placeholder: t("Search apps"), autofocus: true, oninput: e => { q = e.target.value.toLowerCase(); draw(); } })),
          list),
        foot: [h("button", { class: "btn", onclick: () => m.close() }, t("Cancel")),
          addBtn = h("button", { class: "btn primary", onclick: () => { addAppInfos(all.filter(a => chosen.has(a.path))); m.close(); } }, t("Add {0}", 0))]
      });
      try { all = await call("apps.running"); draw(); } catch (e) { toastError(e); }
    }

    function chipEditor({ items, validate, placeholder, onChange, mono = true }) {
      let input;
      const wrap = h("div");
      const draw = () => {
        wrap.innerHTML = "";
        append(wrap, [
          items.length ? h("div", { class: "chips", style: { padding: "4px 18px 14px" } }, items.map((v, i) => h("span", { class: "chip" + (validate(v) ? "" : " invalid"), title: v },
            h("span", null, v), h("button", { title: t("Remove"), onclick: () => { items.splice(i, 1); draw(); onChange(); } }, icon("x"))))) : null,
          h("div", { class: "add-row" },
            input = h("input", {
              class: "input" + (mono ? " mono" : ""), placeholder,
              onkeydown: e => { if (e.key === "Enter") { e.preventDefault(); add(); } },
              onpaste: e => {
                const text = (e.clipboardData || {}).getData ? e.clipboardData.getData("text") : "";
                if (/[\s,;]/.test(text.trim())) { e.preventDefault(); input.value = text; add(); }
              }
            }),
            h("button", { class: "btn", onclick: () => add() }, icon("plus"), t("Add")))
        ]);
      };
      const add = () => {
        const values = input.value.split(/[\s,;]+/).map(s => s.trim()).filter(Boolean);
        const bad = values.filter(v => !validate(v));
        if (bad.length) { input.classList.add("invalid"); toast("bad", t("Invalid: {0}", bad.join(", "))); return; }
        values.forEach(v => { if (!items.some(x => x.toLowerCase() === v.toLowerCase())) items.push(v); });
        input.value = "";
        input.classList.remove("invalid");
        draw();
        onChange();
        setTimeout(() => wrap.querySelector("input").focus());
      };
      draw();
      return wrap;
    }

    function ipsCard() {
      return h("div", { class: "card" },
        h("div", { class: "card-head" }, h("div", null, h("h3", null, t("IP addresses and ranges")), h("div", { class: "sub" }, t("Single addresses or CIDR ranges, IPv4 or IPv6.")))),
        chipEditor({ items: draft.ips, validate: isValidPrefix, placeholder: t("e.g. 192.168.1.0/24, 1.1.1.1, 2001:db8::/32"), onChange: changed }),
        draft.mode === "exclude" ? settingRow(t("Keep local network (LAN) outside the VPN"), t("Printers, NAS and other devices on your network stay reachable."),
          switchEl(!!draft.allowLan, v => { draft.allowLan = v || undefined; if (!v) delete draft.allowLan; changed(); })) : null);
    }

    function domainsCard() {
      const presetsEl = h("div", { class: "presets" });
      const drawPresets = () => {
        presetsEl.innerHTML = "";
        append(presetsEl, DOMAIN_PRESETS.map(p => {
          const added = p.domains.every(d => draft.domains.includes(d));
          return h("button", {
            class: "preset" + (added ? " added" : ""), onclick: () => {
              if (added) {
                draft.domains = draft.domains.filter(d => !p.domains.includes(d));
                if (p.ips) draft.ips = draft.ips.filter(d => !p.ips.includes(d));
              } else {
                p.domains.forEach(d => { if (!draft.domains.includes(d)) draft.domains.push(d); });
                (p.ips || []).forEach(d => { if (!draft.ips.includes(d)) draft.ips.push(d); });
              }
              render();
            }
          }, (added ? "✓ " : "+ ") + p.name);
        }));
      };
      drawPresets();
      return h("div", { class: "card" },
        h("div", { class: "card-head" }, h("div", null, h("h3", null, t("Domains")), h("div", { class: "sub" }, t("example.com also covers its subdomains. Wildcards: *.example.com, *example.com, api.*.example.com. Use =example.com for that name only.")))),
        chipEditor({ items: draft.domains, validate: isValidDomain, placeholder: t("e.g. *.youtube.com, *domain.com"), onChange: () => { changed(); drawPresets(); } }),
        h("div", { class: "card-body", style: { borderTop: "1px solid var(--border)", paddingTop: "14px" } },
          h("div", { class: "muted", style: { fontSize: "12px", marginBottom: "8px" } }, t("Quick add")),
          presetsEl,
          draft.domains.length ? h("div", { class: "callout", style: { marginTop: "14px" } }, icon("info"), h("div", null, t("Domain rules need the DNS forwarder; it starts automatically when the tunnel connects."), " ", t("Browsers with “secure DNS” (DoH) enabled bypass domain rules. Turn it off in the browser or use the local proxy."))) : null));
    }

    function dnsCard() {
      const custom = draft.dns.upstreams;
      const presets = h("div", { class: "presets", style: { padding: "0 18px 14px" } }, DNS_PRESETS.map(p => h("button", {
        class: "preset" + (custom.includes(p.value) ? " added" : ""),
        onclick: () => {
          const i = custom.indexOf(p.value);
          if (i >= 0) custom.splice(i, 1); else custom.push(p.value);
          render();
        }
      }, (custom.includes(p.value) ? "✓ " : "+ ") + p.name)));
      return h("div", { class: "card" },
        h("div", { class: "card-head" }, h("div", null, h("h3", null, t("Upstream servers")), h("div", { class: "sub" }, custom.length ? t("Encrypted DNS and custom servers. Supports 1.1.1.1, tls://dns.google#8.8.8.8 and https://cloudflare-dns.com/dns-query#1.1.1.1") : t("Use the tunnel's DNS servers")))),
        chipEditor({ items: custom, validate: v => /^(https:\/\/|tls:\/\/)/.test(v) || isValidPrefix(v.replace(/^udp:\/\//, "").replace(/:\d+$/, "").replace(/^\[|\]$/g, "")), placeholder: "https://… · tls://… · 1.1.1.1", onChange: () => render() }),
        presets,
        settingRow(t("Always use the DNS forwarder"), t("Also when no domain rules exist, e.g. for encrypted DNS."), switchEl(!!draft.dns.force, v => { draft.dns.force = v; changed(); })),
        settingRow(t("Block other DNS servers"), t("Stops apps from sidestepping the rules with their own DNS server."), switchEl(!!draft.dns.blockOtherDns, v => { draft.dns.blockOtherDns = v; changed(); })),
        draft.mode === "exclude" ? settingRow(t("Resolve excluded sites with regular DNS"), t("Gives excluded sites the same servers they would get without the VPN (better CDN routing)."), switchEl(!!draft.dns.excludedViaSystemDns, v => { draft.dns.excludedViaSystemDns = v; changed(); })) : null);
    }

    function proxyCard() {
      const p = draft.proxy;
      const any = p.socks5 || p.http;
      return h("div", { class: "card" },
        h("div", { class: "card-head" }, h("div", null, h("h3", null, t("Local proxy")), h("div", { class: "sub" }, t("Point individual apps (browsers, Telegram, torrent clients) at the tunnel without routing the whole system.")))),
        settingRow(t("SOCKS5 proxy"), (p.listen || "127.0.0.1") + ":" + (p.socksPort || 1080),
          h("div", { class: "hstack" }, numberInput(p.socksPort || 1080, 1, 65535, v => { p.socksPort = v; changed(); }), switchEl(!!p.socks5, v => { p.socks5 = v; render(); }))),
        settingRow(t("HTTP proxy"), (p.listen || "127.0.0.1") + ":" + (p.httpPort || 8118),
          h("div", { class: "hstack" }, numberInput(p.httpPort || 8118, 1, 65535, v => { p.httpPort = v; changed(); }), switchEl(!!p.http, v => { p.http = v; render(); }))),
        any ? [
          settingRow(t("Listen address"), null, selectEl(p.listen || "127.0.0.1", [
            { value: "127.0.0.1", label: t("This computer only (127.0.0.1)") },
            { value: "0.0.0.0", label: t("Local network (0.0.0.0)") }
          ], v => { p.listen = v === "127.0.0.1" ? undefined : v; if (!p.listen) delete p.listen; render(); })),
          settingRow(t("Username"), t("optional"), h("input", { class: "input", value: p.username || "", style: { width: "180px" }, oninput: e => { p.username = e.target.value || undefined; changed(); } })),
          settingRow(t("Password"), t("optional"), h("input", { class: "input", type: "password", value: p.password || "", style: { width: "180px" }, oninput: e => { p.password = e.target.value || undefined; changed(); } }))
        ] : null);
    }

    return { render: renderPage, open, dirty, confirmLeave };
  })();

  // ------------------------------------------------------------------
  // Auto-tunnel page
  // ------------------------------------------------------------------

  function actionOptions() {
    return [
      { value: "connect", label: t("Connect tunnel") },
      { value: "disconnect", label: t("Disconnect tunnels") },
      { value: "none", label: t("Do nothing") }
    ];
  }

  function autoPage() {
    const s = state.settings;
    if (!s) { loadSettings().then(render); return [pageHeader(t("Auto-tunnel")), h("div", { class: "page-body" }, t("Loading…"))]; }
    const a = s.autoTunnel;
    a.trustedSsids = a.trustedSsids || [];
    a.rules = a.rules || [];
    const ro = !state.info.isAdmin;
    const net = state.status.network || { kind: "none" };
    const tunnelOptions = [{ value: "", label: "—" }].concat(state.tunnels.map(x => ({ value: x.name, label: x.name })));
    let ssidInput;
    const netIcon = { wifi: "wifi", ethernet: "ethernet", other: "globe", none: "x" }[net.kind] || "globe";
    const netLabel = { wifi: t("Wi-Fi"), ethernet: t("Ethernet"), other: t("Other"), none: t("No network") }[net.kind] || net.kind;
    return [
      pageHeader(t("Auto-tunnel"), t("Connect and disconnect automatically based on the network you join."),
        switchEl(!!a.enabled, v => updateSettings(s => { s.autoTunnel.enabled = v; }), { disabled: ro || !state.tunnels.length, label: t("Enable auto-tunneling") })),
      h("div", { class: "page-body", "data-scroll": "auto" },
        readOnlyNotice(),
        !state.tunnels.length ? h("div", { class: "callout warn", style: { marginBottom: "16px" } }, icon("alert"), t("Auto-tunneling needs at least one tunnel.")) : null,
        h("div", { class: "card" },
          h("div", { class: "row" },
            h("div", { class: "app-icon", style: { width: "44px", height: "44px", borderRadius: "12px", background: "var(--accent-weak)", color: "var(--accent)" } }, icon(netIcon)),
            h("div", { class: "label" },
              h("div", { class: "muted", style: { fontSize: "12px" } }, t("Current network")),
              h("div", { class: "name", style: { fontSize: "16px" } }, net.kind === "wifi" ? (net.ssid || t("unknown network name")) : netLabel),
              h("div", { class: "desc" }, [netLabel, net.name].filter(Boolean).join(" · "))),
            net.kind === "wifi" && net.ssid ? h("button", {
              class: "btn small", disabled: ro || a.trustedSsids.includes(net.ssid),
              onclick: () => updateSettings(s => { s.autoTunnel.trustedSsids.push(net.ssid); })
            }, icon("shield"), t("Add current")) : null),
          net.kind === "wifi" && !net.ssid ? h("div", { class: "card-body" }, h("div", { class: "callout warn" }, icon("alert"), t("Windows did not report the Wi-Fi name. On Windows 11, allow location access for desktop apps (Settings → Privacy & security → Location)."))) : null),
        h("div", { class: "callout", style: { marginTop: "16px" } }, icon("info"), t("Rules run whenever the active network changes. Manual changes stay until the next network change.")),
        h("div", { class: "section-title" }, t("Settings")),
        h("div", { class: "card" },
          settingRow(t("Default tunnel"), t("Used by rules that don't name a tunnel."), selectEl(a.tunnel || "", tunnelOptions, v => updateSettings(s => { s.autoTunnel.tunnel = v; }), { disabled: ro, style: { minWidth: "200px" } })),
          settingRow(t("On untrusted Wi-Fi"), null, selectEl(a.onUntrustedWifi, actionOptions(), v => updateSettings(s => { s.autoTunnel.onUntrustedWifi = v; }), { disabled: ro })),
          settingRow(t("On Ethernet"), null, selectEl(a.onEthernet, actionOptions(), v => updateSettings(s => { s.autoTunnel.onEthernet = v; }), { disabled: ro })),
          settingRow(t("On other networks"), t("Mobile broadband, virtual adapters and similar."), selectEl(a.onOther, actionOptions(), v => updateSettings(s => { s.autoTunnel.onOther = v; }), { disabled: ro })),
          settingRow(t("Wait before acting"), t("Lets the network settle after a change."), h("div", { class: "hstack" }, numberInput(a.debounceSeconds || 0, 0, 60, v => updateSettings(s => { s.autoTunnel.debounceSeconds = v; })), h("span", { class: "muted" }, t("seconds"))))),
        h("div", { class: "section-title" }, t("Trusted Wi-Fi networks")),
        h("div", { class: "card" },
          h("div", { class: "card-head" }, h("div", { class: "sub" }, t("Tunnels disconnect on these networks. Wildcards allowed, e.g. Home*."))),
          a.trustedSsids.length ? h("div", { class: "chips", style: { padding: "4px 18px 14px" } }, a.trustedSsids.map((v, i) => h("span", { class: "chip" }, h("span", null, v),
            h("button", { disabled: ro, onclick: () => updateSettings(s => { s.autoTunnel.trustedSsids.splice(i, 1); }) }, icon("x"))))) : null,
          h("div", { class: "add-row" },
            ssidInput = h("input", { class: "input", placeholder: t("Network name (SSID)"), disabled: ro, onkeydown: e => { if (e.key === "Enter") addSsid(); } }),
            h("button", { class: "btn", disabled: ro, onclick: () => addSsid() }, icon("plus"), t("Add")))),
        h("div", { class: "section-title" }, t("Per-network rules")),
        h("div", { class: "card" },
          h("div", { class: "card-head" }, h("div", { class: "sub" }, t("Checked before everything else, top to bottom.")), h("div", { class: "spacer" }),
            h("button", { class: "btn small", disabled: ro, onclick: () => updateSettings(s => { s.autoTunnel.rules.push({ ssid: net.kind === "wifi" && net.ssid ? net.ssid : "", action: "connect", tunnel: "" }); }) }, icon("plus"), t("Add rule"))),
          a.rules.length ? a.rules.map((r, i) => h("div", { class: "app-row" },
            h("div", { class: "app-icon" }, icon("wifi")),
            h("input", { class: "input grow", value: r.ssid, placeholder: t("Network name (SSID)"), disabled: ro, onchange: e => updateSettings(s => { s.autoTunnel.rules[i].ssid = e.target.value.trim(); }) }),
            selectEl(r.action, actionOptions(), v => updateSettings(s => { s.autoTunnel.rules[i].action = v; }), { disabled: ro }),
            r.action === "connect" ? selectEl(r.tunnel || "", [{ value: "", label: t("default") }].concat(state.tunnels.map(x => ({ value: x.name, label: x.name }))), v => updateSettings(s => { s.autoTunnel.rules[i].tunnel = v; }), { disabled: ro }) : null,
            h("button", { class: "btn ghost icon small", disabled: ro, title: t("Remove"), onclick: () => updateSettings(s => { s.autoTunnel.rules.splice(i, 1); }) }, icon("trash"))))
            : h("div", { class: "muted", style: { padding: "4px 18px 18px" } }, t("No rules."))))
    ];
    function addSsid() {
      const v = ssidInput.value.trim();
      if (!v) return;
      updateSettings(s => { if (!s.autoTunnel.trustedSsids.includes(v)) s.autoTunnel.trustedSsids.push(v); });
    }
  }

  // ------------------------------------------------------------------
  // Settings page
  // ------------------------------------------------------------------

  const ACCENTS = ["#7c5cff", "#3b82f6", "#06b6d4", "#10b981", "#84cc16", "#fbb26a", "#f97316", "#ef4444", "#ec4899", "#a855f7"];

  function themePreview(id, label) {
    const vars = {
      light: { "--p-side": "#eceef4", "--p-main": "#ffffff", "--p-line": "#e3e5ed" },
      dark: { "--p-side": "#141720", "--p-main": "#181b25", "--p-line": "#272c3a" },
      amoled: { "--p-side": "#000", "--p-main": "#000", "--p-line": "#1c1c22" },
      system: { "--p-side": "linear-gradient(135deg,#eceef4 50%,#141720 50%)", "--p-main": "linear-gradient(135deg,#fff 50%,#181b25 50%)", "--p-line": "#8888" }
    }[id];
    const prev = h("div", { class: "theme-prev" }, h("i", { class: "p-side" }), h("div", { class: "p-main" }, h("b"), h("b"), h("b", { style: { width: "70%" } })));
    for (const [k, v] of Object.entries(vars)) prev.style.setProperty(k, v);
    return h("button", { class: "theme-opt" + (state.prefs.theme === id ? " active" : ""), onclick: () => setPrefs({ theme: id }) }, prev, label);
  }

  function settingsPage() {
    const s = state.settings;
    if (!s) loadSettings().then(() => { if (state.page === "settings") render(); });
    const ro = !state.info.isAdmin;
    const p = state.prefs;
    const hl = s && s.health, dd = s && s.dynamicDns, ld = s && s.lockdown;
    return [
      pageHeader(t("Settings")),
      h("div", { class: "page-body", "data-scroll": "settings" },
        h("div", { class: "section-title" }, t("Appearance")),
        h("div", { class: "card" },
          h("div", { class: "card-body", style: { paddingTop: "16px" } },
            h("div", { class: "themes" }, themePreview("system", t("System")), themePreview("light", t("Light")), themePreview("dark", t("Dark")), themePreview("amoled", t("AMOLED black"))))),
        h("div", { class: "card" },
          settingRow(t("Accent colour"), null, h("div", { class: "swatches" },
            ACCENTS.map(c => h("button", { class: "swatch" + (p.accent === c ? " active" : ""), style: { background: c }, title: c, onclick: () => setPrefs({ accent: c }) })),
            h("label", { class: "swatch custom" + (ACCENTS.includes(p.accent) ? "" : " active"), title: "Custom" }, h("input", { type: "color", value: p.accent, onchange: e => setPrefs({ accent: e.target.value }) })))),
          settingRow(t("Language"), null, selectEl(p.language || "auto", [{ value: "auto", label: t("Automatic") }, { value: "en", label: "English" }, { value: "ru", label: "Русский" }], v => setPrefs({ language: v }))),
          settingRow(t("Compact layout"), null, switchEl(!!p.compact, v => setPrefs({ compact: v })))),

        h("div", { class: "section-title" }, t("Behaviour")),
        h("div", { class: "card" },
          settingRow(t("Close button hides to the tray"), t("Otherwise it minimises to the taskbar. Tunnels keep running either way."), switchEl(!!p.closeToTray, v => setPrefs({ closeToTray: v }))),
          settingRow(t("Start hidden in the tray"), null, switchEl(!!p.startMinimized, v => setPrefs({ startMinimized: v }))),
          settingRow(t("Notifications"), t("Connections, errors, health restarts and auto-tunnel actions."), switchEl(!!p.notifications, v => setPrefs({ notifications: v })))),

        !s ? h("div", { class: "card", style: { marginTop: "16px", padding: "18px" } }, t("Loading…")) : [
          h("div", { class: "section-title" }, t("Kill switch")),
          h("div", { class: "card" },
            settingRow(t("Lockdown mode"), t("Block all internet traffic whenever no tunnel is connected."), switchEl(!!ld.enabled, v => updateSettings(s => { s.lockdown.enabled = v; }), { disabled: ro })),
            ld.enabled ? settingRow(t("Allow local network while locked down"), null, switchEl(!!ld.allowLan, v => updateSettings(s => { s.lockdown.allowLan = v; }), { disabled: ro }), { sub: true }) : null,
            h("div", { class: "card-body", style: { paddingTop: "12px" } },
              state.status.lockdown ? h("div", { class: "callout warn" }, icon("lock"), t("Lockdown is active: traffic is blocked until a tunnel connects.")) :
                h("div", { class: "muted", style: { fontSize: "12.5px" } }, t("Each full-tunnel configuration (AllowedIPs 0.0.0.0/0) already blocks leaks while it is connected.")))),

          h("div", { class: "section-title" }, t("Connection reliability")),
          h("div", { class: "card" },
            settingRow(t("Restart tunnels that stop working"), t("A tunnel that keeps sending without getting answers or a handshake is reconnected."), switchEl(!!hl.enabled, v => updateSettings(s => { s.health.enabled = v; }), { disabled: ro })),
            hl.enabled ? [
              settingRow(t("Handshake timeout"), null, h("div", { class: "hstack" }, numberInput(hl.handshakeTimeoutSeconds, 30, 3600, v => updateSettings(s => { s.health.handshakeTimeoutSeconds = v; })), h("span", { class: "muted" }, t("seconds"))), { sub: true }),
              settingRow(t("Ping target"), t("Optional host pinged through the tunnel, e.g. 1.1.1.1."), h("input", { class: "input mono", value: hl.pingTarget || "", placeholder: "1.1.1.1", style: { width: "180px" }, disabled: ro, onchange: e => updateSettings(s => { s.health.pingTarget = e.target.value.trim(); }) }), { sub: true }),
              hl.pingTarget ? settingRow(t("Ping interval"), null, h("div", { class: "hstack" }, numberInput(hl.pingIntervalSeconds, 5, 3600, v => updateSettings(s => { s.health.pingIntervalSeconds = v; })), h("span", { class: "muted" }, t("seconds"))), { sub: true }) : null,
              hl.pingTarget ? settingRow(t("Failed pings before restart"), null, numberInput(hl.pingFailuresForRestart, 1, 100, v => updateSettings(s => { s.health.pingFailuresForRestart = v; })), { sub: true }) : null
            ] : null,
            settingRow(t("Re-resolve endpoint host names"), t("Follows servers on dynamic DNS without reconnecting."), switchEl(!!dd.enabled, v => updateSettings(s => { s.dynamicDns.enabled = v; }), { disabled: ro })),
            dd.enabled ? [
              settingRow(t("Check every"), null, h("div", { class: "hstack" }, numberInput(dd.intervalSeconds, 30, 86400, v => updateSettings(s => { s.dynamicDns.intervalSeconds = v; })), h("span", { class: "muted" }, t("seconds"))), { sub: true }),
              settingRow(t("Prefer IPv6 endpoints"), null, switchEl(!!dd.preferIpv6, v => updateSettings(s => { s.dynamicDns.preferIpv6 = v; }), { disabled: ro }), { sub: true })
            ] : null),

          h("div", { class: "section-title" }, t("Split tunnel driver")),
          h("div", { class: "card" },
            h("div", { class: "card-body", style: { paddingTop: "16px" } },
              h("div", { class: "callout" + (state.status.driverAvailable ? "" : " warn") }, icon(state.status.driverAvailable ? "check" : "alert"),
                h("div", null, h("b", null, state.status.driverAvailable ? t("Driver found — apps can bypass the VPN.") : t("Driver not found — “Bypass VPN” app rules are inactive.")),
                  h("div", null, t("Uses the open-source Mullvad split tunnel driver (GPL-3.0/MPL-2.0). It is not bundled."))))),
            settingRow(t("Driver file"), s.splitDriverPath || "mullvad-split-tunnel.sys", h("div", { class: "hstack" },
              s.splitDriverPath ? h("button", { class: "btn small ghost", disabled: ro, onclick: () => updateSettings(s => { s.splitDriverPath = ""; }) }, t("Clear")) : null,
              h("button", { class: "btn small", disabled: ro, onclick: async () => { const r = await call("dialog.pickDriver").catch(toastError); if (r) updateSettings(s => { s.splitDriverPath = r.path; }); } }, t("Choose…")))))
        ],

        h("div", { class: "section-title" }, t("Remote control")),
        h("div", { class: "card" },
          settingRow(t("Allow command-line control"), null, switchEl(!!p.remoteControl, v => setPrefs({ remoteControl: v }))),
          h("div", { class: "card-body" },
            h("div", { class: "muted", style: { fontSize: "12.5px", marginBottom: "8px" } }, t("Lets scripts and automation tools connect tunnels without elevation:")),
            h("pre", { class: "mono", style: { margin: 0, padding: "10px 12px", background: "var(--code-bg)", border: "1px solid var(--border)", borderRadius: "8px", whiteSpace: "pre-wrap" } },
              "amneziawg.exe /connect <tunnel>\namneziawg.exe /disconnect <tunnel>\namneziawg.exe /toggle <tunnel>\namneziawg.exe /disconnectall"))),

        h("div", { class: "section-title" }, t("Advanced")),
        h("div", { class: "card" },
          settingRow(t("Use the classic interface"), null, h("button", { class: "btn small", onclick: async () => { if (await confirmDialog({ title: t("Use the classic interface"), message: t("Switch back to the original AmneziaWG window. You can return from its tray menu by deleting the preference."), ok: t("Switch") })) call("app.legacyUI").catch(toastError); } }, t("Switch"))),
          settingRow(t("Exit and stop all tunnels"), t("Stops the background service; tunnels disconnect."), h("button", {
            class: "btn small danger", disabled: ro, onclick: async () => {
              if (await confirmDialog({ title: t("Exit BetterAmnezia?"), message: t("All tunnels will be disconnected."), ok: t("Exit"), danger: true })) call("app.quit", { stopTunnels: true }).catch(toastError);
            }
          }, t("Exit")))))
    ];
  }

  // ------------------------------------------------------------------
  // Logs page
  // ------------------------------------------------------------------

  const logsPage = (() => {
    const lines = [];
    let cursor = null, follow = true, filter = "", errorsOnly = false, timer = null;
    const MAX = 5000;

    function classify(text) {
      const l = text.toLowerCase();
      if (/(error|fail|unable|cannot|denied|panic)/.test(l)) return "err";
      if (/(warn|retry|retrying|timeout|stale)/.test(l)) return "warn";
      return "";
    }

    function lineEl(l) {
      let tag = "", msg = l.text;
      const m = /^\[(\w{3})\]\s?(.*)$/s.exec(l.text);
      if (m) { tag = m[1]; msg = m[2]; }
      const d = new Date(l.t);
      const ts = d.toLocaleTimeString([], { hour12: false }) + "." + String(d.getMilliseconds()).padStart(3, "0");
      const msgEl = h("span", { class: "msg" });
      if (filter) {
        const lower = msg.toLowerCase(), f = filter.toLowerCase();
        let i = 0, j;
        while ((j = lower.indexOf(f, i)) >= 0) {
          append(msgEl, [msg.slice(i, j), h("mark", null, msg.slice(j, j + f.length))]);
          i = j + f.length;
        }
        append(msgEl, [msg.slice(i)]);
      } else msgEl.textContent = msg;
      return h("div", { class: "log-line " + classify(l.text) }, h("span", { class: "ts" }, ts), h("span", { class: "tag " + tag }, tag), msgEl);
    }

    function visible(l) {
      if (errorsOnly && classify(l.text) !== "err") return false;
      return !filter || l.text.toLowerCase().includes(filter.toLowerCase());
    }

    async function poll() {
      try {
        const r = await call("log.follow", { cursor });
        cursor = r.cursor;
        if (r.lines.length) {
          lines.push(...r.lines);
          if (lines.length > MAX) lines.splice(0, lines.length - MAX);
          const view = document.getElementById("log-view");
          if (view) {
            const frag = document.createDocumentFragment();
            r.lines.filter(visible).forEach(l => frag.appendChild(lineEl(l)));
            view.appendChild(frag);
            while (view.childElementCount > MAX) view.firstChild.remove();
            if (follow) view.scrollTop = view.scrollHeight;
            const c = document.getElementById("log-count");
            if (c) c.textContent = t("{0} lines", lines.length);
          }
        }
      } catch (e) { /* ignore */ }
    }

    function start() {
      if (timer) return;
      poll();
      timer = setInterval(() => { if (state.page === "logs") poll(); else { clearInterval(timer); timer = null; } }, 1000);
    }

    function renderPage() {
      setTimeout(start);
      const view = h("div", { class: "log-view", id: "log-view", onscroll: e => {
        const el = e.target;
        const atBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 30;
        if (follow !== atBottom) { follow = atBottom; const f = document.getElementById("log-follow"); if (f) f.checked = follow; }
      } }, lines.filter(visible).map(lineEl));
      setTimeout(() => { if (follow) view.scrollTop = view.scrollHeight; });
      return [
        pageHeader(t("Logs"), null, h("div", { class: "hstack" },
          h("span", { class: "muted", id: "log-count", style: { fontSize: "12.5px" } }, t("{0} lines", lines.length)),
          h("button", { class: "btn", onclick: () => copyText(lines.map(l => new Date(l.t).toISOString() + ": " + l.text).join("\n")) }, icon("copy"), t("Copy all")),
          h("button", { class: "btn", onclick: async () => { try { const r = await call("log.save"); if (r) toast("ok", t("Exported to {0}", r.path)); } catch (e) { toastError(e); } } }, icon("save"), t("Save…")))),
        h("div", { class: "page-body", style: { display: "flex", flexDirection: "column", gap: "12px", overflow: "hidden" } },
          h("div", { class: "hstack" },
            h("div", { class: "search grow" }, icon("search"), h("input", { class: "input", placeholder: t("Filter"), value: filter, oninput: debounce(e => { filter = e.target.value; render(); }, 200) })),
            h("label", { class: "hstack", style: { cursor: "pointer" } }, switchEl(errorsOnly, v => { errorsOnly = v; render(); }), h("span", null, t("Errors only"))),
            h("label", { class: "hstack", style: { cursor: "pointer" } }, h("span", { style: { display: "contents" }, ref: el => el.appendChild(switchEl(follow, v => { follow = v; if (v) { const lv = document.getElementById("log-view"); if (lv) lv.scrollTop = lv.scrollHeight; } })) }), h("span", null, t("Follow")))),
          h("div", { style: { flex: 1, minHeight: 0 } }, view))
      ];
    }

    return { render: renderPage };
  })();

  // ------------------------------------------------------------------
  // About page
  // ------------------------------------------------------------------

  function aboutPage() {
    const u = state.update;
    const updateRow = state.info.updateState === 1 ?
      h("div", { class: "callout", style: { marginTop: "16px" } }, icon("download"),
        h("div", { class: "grow" }, h("b", null, t("Update available")),
          u ? h("div", null, u.error ? u.error : u.activity + (u.total ? " · " + Math.round(100 * u.downloaded / u.total) + "%" : "")) : null),
        h("button", { class: "btn primary small", disabled: !state.info.isAdmin || (u && !u.error && !u.complete), onclick: () => call("update.start").catch(toastError) }, t("Update now")))
      : state.info.updateState === 2 ? h("div", { class: "muted", style: { marginTop: "12px" } }, t("Unofficial build — automatic updates are off.")) : null;
    return [
      pageHeader(t("About")),
      h("div", { class: "page-body" },
        h("div", { class: "card", style: { padding: "28px", display: "flex", gap: "22px", alignItems: "center" } },
          h("div", { class: "brand-mark", style: { width: "72px", height: "72px", borderRadius: "20px" } }, h("span", { style: { display: "contents" }, ref: el => { const s = icon("logo"); s.style.width = "40px"; s.style.height = "40px"; el.appendChild(s); } })),
          h("div", null,
            h("div", { style: { font: "600 26px/1.2 var(--font-display)" } }, "BetterAmnezia"),
            h("div", { class: "muted" }, t("A better AmneziaWG client for Windows.")),
            h("div", { class: "hstack", style: { marginTop: "10px" } },
              h("span", { class: "badge accent" }, t("Version") + " " + (state.info.version || "?")),
              state.info.arch ? h("span", { class: "badge" }, state.info.arch) : null,
              state.info.os ? h("span", { class: "badge" }, state.info.os) : null),
            updateRow)),
        h("div", { class: "grid-2", style: { marginTop: "16px" } },
          h("div", { class: "card" },
            h("div", { class: "card-head" }, h("h3", null, t("Keyboard shortcuts"))),
            h("div", { class: "card-body" }, h("div", { class: "kv text" },
              [h("div", { class: "k" }, h("span", { class: "kbd" }, "Ctrl"), " + ", h("span", { class: "kbd" }, "1…6")), h("div", { class: "v" }, PAGES.map(p => t(p.label)).join(" · "))],
              [h("div", { class: "k" }, h("span", { class: "kbd" }, "Ctrl"), " + ", h("span", { class: "kbd" }, "F")), h("div", { class: "v" }, t("Search"))],
              [h("div", { class: "k" }, h("span", { class: "kbd" }, "Ctrl"), " + ", h("span", { class: "kbd" }, "I")), h("div", { class: "v" }, t("Import tunnels"))],
              [h("div", { class: "k" }, h("span", { class: "kbd" }, "Space")), h("div", { class: "v" }, t("Toggle selected tunnel"))]))),
          h("div", { class: "card" },
            h("div", { class: "card-head" }, h("h3", null, t("About"))),
            h("div", { class: "card-body" },
              h("p", { class: "muted", style: { marginTop: 0 } }, t("Based on AmneziaWG for Windows and WireGuard for Windows. Split tunnel driver by Mullvad VPN (optional).")),
              h("div", { class: "vstack", style: { alignItems: "flex-start" } },
                h("a", { onclick: () => call("shell.openURL", { url: "https://github.com/Movryn/betteramnezia" }) }, t("Source code"), " ↗"),
                h("a", { onclick: () => call("shell.openURL", { url: "https://github.com/amnezia-vpn/amneziawg-windows-client" }) }, "AmneziaWG for Windows ↗"),
                h("a", { onclick: () => call("shell.openURL", { url: "https://github.com/mullvad/win-split-tunnel" }) }, "Mullvad split tunnel driver ↗"))))))
    ];
  }

  // ------------------------------------------------------------------
  // Events from the host
  // ------------------------------------------------------------------

  on("tunnelChange", d => {
    const tun = tunnelByName(d.name);
    if (!tun) { loadTunnels(); return; }
    const was = tun.state;
    tun.state = d.state;
    tun.error = d.error || "";
    if (d.state === "started" && was !== "started") { state.since[d.name] = Date.now(); state.history[d.name] = []; delete state.runtime[d.name]; }
    if (d.state === "stopped") { delete state.since[d.name]; delete state.runtime[d.name]; }
    if (d.error) toast("bad", d.name, d.error);
    if (state.page === "tunnels") { renderSidebar(); renderTunnelItems(); renderDetail(); }
    else renderSidebar();
    setTimeout(loadStatus, 1500);
  });
  on("tunnelsChange", () => loadTunnels());
  on("network", n => { state.status.network = n; if (state.page === "auto") render(); });
  on("lockdown", d => { state.status.lockdown = !!(d && d.active); renderSidebar(); if (state.page === "settings") render(); });
  on("health", d => { if (d && d.tunnel) { state.status.health[d.tunnel] = d.status; if (state.page === "tunnels") renderDetail(); } });
  on("settings", s => { state.settings = s; if (["auto", "settings"].includes(state.page)) render(); else renderSidebar(); });
  on("updateFound", d => { state.info.updateState = d.state; renderSidebar(); if (state.page === "about") render(); });
  on("updateProgress", d => { state.update = d; if (state.page === "about") render(); });
  on("navigate", d => { if (d && d.action === "import") importDialog("file"); });
  on("systemTheme", () => applyPrefs());
  on("raised", () => { loadTunnels(); loadStatus().then(renderSidebar); });

  // ------------------------------------------------------------------
  // Keyboard
  // ------------------------------------------------------------------

  document.addEventListener("keydown", e => {
    if (e.key === "Escape") {
      if (openMenuEl) { closeMenu(); return; }
      const top = modalStack[modalStack.length - 1];
      if (top && top.dismissable) top.close();
      return;
    }
    if (modalStack.length) return;
    const inField = /^(INPUT|TEXTAREA|SELECT)$/.test(document.activeElement && document.activeElement.tagName);
    if (e.ctrlKey && !e.shiftKey && !e.altKey) {
      const n = parseInt(e.key, 10);
      if (n >= 1 && n <= PAGES.length) { e.preventDefault(); navigate(PAGES[n - 1].id); return; }
      if (e.key.toLowerCase() === "f" && state.page === "tunnels") { e.preventDefault(); const s = document.getElementById("tunnel-search"); if (s) s.focus(); return; }
      if (e.key.toLowerCase() === "i") { e.preventDefault(); importDialog("file"); return; }
    }
    if (!inField && e.key === " " && state.page === "tunnels" && state.selected && !(document.activeElement && document.activeElement.closest(".tunnel-item"))) {
      e.preventDefault();
      toggleTunnel(state.selected);
    }
  });

  // ------------------------------------------------------------------
  // Start
  // ------------------------------------------------------------------

  async function start() {
    try { state.prefs = Object.assign(state.prefs, await call("prefs.get")); } catch (e) { /* defaults */ }
    applyPrefs();
    render();
    try { state.info = await call("app.info"); } catch (e) { /* ignore */ }
    await Promise.all([loadTunnels(), loadStatus(), loadSettings()]);
    render();
    setInterval(pollRuntime, 1000);
    setInterval(() => { loadStatus().then(() => { renderSidebar(); if (state.page === "auto") render(); }); }, 10000);
    setInterval(() => {
      if (state.page !== "tunnels") return;
      const tun = tunnelByName(state.selected);
      if (tun && tun.state !== "started") return;
      const hs = document.getElementById("stat-hs");
      const rt = tun && state.runtime[tun.name];
      if (hs && rt) hs.textContent = fmtAgo(rt.lastHandshake);
    }, 5000);
  }

  start();
})();
