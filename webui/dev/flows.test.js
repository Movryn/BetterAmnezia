// UI flow tests against the mock bridge. Run with: node webui/dev/flows.test.js
// (needs the "playwright" npm package; set PLAYWRIGHT_MODULE to its path if global).
const { chromium } = require(process.env.PLAYWRIGHT_MODULE || 'playwright');
const assert = (c, m) => { if (!c) { throw new Error('ASSERT: ' + m); } else console.log('ok -', m); };
(async () => {
  const browser = await chromium.launch();
  const page = await browser.newPage({ viewport: { width: 1180, height: 760 } });
  const errors = [];
  page.on('pageerror', e => errors.push(e.message));
  page.on('console', m => { if (m.type() === 'error') errors.push(m.text()); });
  // Record bridge calls.
  await page.addInitScript(() => { window.__calls = []; });
  await page.goto('file://' + require('path').resolve(__dirname, 'index.html'));
  await page.evaluate(() => { const post = window.__host.post; window.__host.post = raw => { window.__calls.push(JSON.parse(raw)); post(raw); }; });
  await page.waitForTimeout(1500);
  const calls = m => page.evaluate(m => window.__calls.filter(c => c.method === m), m);

  // Toggle a tunnel via switch.
  await page.click('.tunnel-item:nth-child(3) .switch');
  await page.waitForTimeout(300);
  assert((await calls('tunnels.start')).some(c => c.params.name === 'tokyo-wg'), 'switch starts tunnel');
  await page.waitForTimeout(900);
  assert(await page.locator('.tunnel-item:nth-child(3) .dot.started').count() === 1, 'state change event updates list');

  // Select and connect via space
  await page.click('.tunnel-item:nth-child(2)');
  await page.waitForTimeout(300);
  assert((await page.textContent('.hero-title')) === 'home-lab', 'selecting shows detail');
  await page.locator('body').press('Space');
  await page.waitForTimeout(300);
  // Focus is on the item, so space toggles via item handler
  assert((await calls('tunnels.start')).some(c => c.params.name === 'home-lab'), 'space toggles selected tunnel');

  // Search filter
  await page.fill('#tunnel-search', 'tok');
  await page.waitForTimeout(200);
  assert(await page.locator('.tunnel-item').count() === 1, 'search filters list');
  await page.fill('#tunnel-search', '');

  // Split page: add IPs, domain with wildcard, preset, save.
  await page.click('.tunnel-item:nth-child(1)');
  await page.waitForTimeout(200);
  await page.keyboard.press('Control+2');
  await page.waitForTimeout(1200);
  assert(await page.locator('.mode-card.active').textContent().then(t => t.includes('Exclude')), 'split loads exclude mode');
  const ipInput = page.locator('.add-row input').nth(0);
  await ipInput.fill('10.0.0.0/8, 8.8.8.8');
  await ipInput.press('Enter');
  await page.waitForTimeout(200);
  assert(await page.locator('.chip:has-text("10.0.0.0/8")').count() === 1, 'add multiple IPs at once');
  await ipInput.fill('999.1.1.1');
  await ipInput.press('Enter');
  await page.waitForTimeout(200);
  assert(await page.locator('.add-row input.invalid').count() === 1, 'invalid IP rejected');
  const domInput = page.locator('.add-row input').nth(1);
  await domInput.fill('*example.org');
  await domInput.press('Enter');
  await page.waitForTimeout(200);
  assert(await page.locator('.chip:has-text("*example.org")').count() === 1, 'wildcard domain added');
  await page.click('.preset:has-text("Telegram")');
  await page.waitForTimeout(200);
  assert(await page.locator('.chip:has-text("149.154.160.0/20")').count() === 1, 'preset adds IP ranges too');
  assert(await page.locator('#savebar.show').count() === 1, 'save bar appears when dirty');
  await page.click('#savebar .btn.primary');
  await page.waitForTimeout(400);
  const sets = await calls('split.set');
  assert(sets.length === 1 && sets[0].params.config.domains.includes('*example.org') && sets[0].params.config.ips.includes('8.8.8.8'), 'split.set sends edited config');
  assert(await page.locator('#savebar.show').count() === 0, 'save bar hides after save');

  // Mode switch to include & app action change
  await page.click('.mode-card:has-text("Include only")');
  await page.waitForTimeout(200);
  await page.selectOption('.app-row select >> nth=0', 'vpnonly');
  await page.waitForTimeout(200);
  assert(await page.locator('#savebar.show').count() === 1, 'mode change marks dirty');
  // Navigate away triggers confirm
  await page.keyboard.press('Control+1');
  await page.waitForTimeout(300);
  assert(await page.locator('.modal h2:has-text("Discard changes?")').count() === 1, 'leaving with unsaved changes asks');
  await page.click('.modal .btn.primary');
  await page.waitForTimeout(400);
  assert(await page.locator('.page-title:has-text("Tunnels")').count() === 1, 'discard navigates');

  // Editor: kill switch toggle edits AllowedIPs
  await page.click('.tunnel-item:nth-child(1)');
  await page.waitForTimeout(300);
  await page.click('.hero-actions .btn:has-text("Edit")');
  await page.waitForTimeout(800);
  await page.click('.modal input[type=checkbox]');
  await page.waitForTimeout(200);
  const text = await page.inputValue('.modal textarea');
  assert(/AllowedIPs = 0\.0\.0\.0\/1, 128\.0\.0\.0\/1, ::\/1, 8000::\/1/.test(text), 'kill switch off rewrites AllowedIPs');
  await page.keyboard.press('Control+s');
  await page.waitForTimeout(400);
  assert((await calls('tunnels.save')).length === 1, 'Ctrl+S saves');

  // Import text
  await page.keyboard.press('Control+i');
  await page.waitForTimeout(400);
  await page.click('.tabs button:has-text("Paste text")');
  await page.fill('.modal textarea', 'vpn://abc');
  await page.click('.modal-foot .btn.primary');
  await page.waitForTimeout(300);
  assert((await calls('tunnels.importText')).length === 1, 'import text calls bridge');

  // Settings theme + accent
  await page.keyboard.press('Control+4');
  await page.waitForTimeout(600);
  await page.click('.theme-opt:has-text("AMOLED")');
  await page.waitForTimeout(200);
  assert(await page.evaluate(() => document.documentElement.dataset.theme) === 'amoled', 'AMOLED theme applies');
  await page.click('.swatch >> nth=3');
  await page.waitForTimeout(200);
  assert((await page.evaluate(() => getComputedStyle(document.documentElement).getPropertyValue('--accent'))).trim() === '#10b981', 'accent applies');
  // Toggle lockdown -> settings.set debounced
  await page.click('.row:has-text("Lockdown mode") .switch');
  await page.waitForTimeout(700);
  const ss = await calls('settings.set');
  assert(ss.length >= 1 && ss[ss.length - 1].params.lockdown.enabled === true, 'lockdown toggle saves settings');

  // Auto-tunnel: add trusted SSID
  await page.keyboard.press('Control+3');
  await page.waitForTimeout(500);
  await page.fill('input[placeholder="Network name (SSID)"] >> nth=0', 'Library*');
  await page.keyboard.press('Enter');
  await page.waitForTimeout(700);
  const ss2 = await calls('settings.set');
  assert(ss2[ss2.length - 1].params.autoTunnel.trustedSsids.includes('Library*'), 'trusted SSID saved');

  // Logs filter
  await page.keyboard.press('Control+5');
  await page.waitForTimeout(1500);
  await page.fill('.page-body input[placeholder="Filter"]', 'youtube');
  await page.waitForTimeout(600);
  assert(await page.locator('.log-line').count() === 1 && await page.locator('.log-line mark').count() === 1, 'log filter highlights matches');

  // Russian language switch
  await page.keyboard.press('Control+4');
  await page.waitForTimeout(400);
  await page.selectOption('.row:has-text("Language") select', 'ru');
  await page.waitForTimeout(400);
  assert(await page.locator('.page-title:has-text("Настройки")').count() === 1, 'language switch re-renders in Russian');

  assert(errors.length === 0, 'no page errors ' + errors.join('; '));
  await browser.close();
})().catch(e => { console.error(e.message); process.exit(1); });
