// Arrow pad E2E: the ↔ button is a drag-pad — swipe locks a direction and
// sends that arrow; holding repeats every 200ms; release stops. Tap still
// opens the popup, whose buttons share the same hold-repeat.
//   bun run vite --port 5199 --strictPort --host 127.0.0.1 &
//   bun tests/scroll-harness/arrows.mjs
import { chromium } from 'playwright';

const URL = 'http://127.0.0.1:5199/tests/scroll-harness/';
const results = [];
const check = (name, ok, detail = '') => {
  results.push({ name, ok });
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${detail ? `  (${detail})` : ''}`);
};

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 390, height: 844 }, hasTouch: true });
page.on('console', (m) => { if (m.type() === 'error') console.log('[page]', m.text()); });
await page.goto(URL);
await page.waitForFunction(() => !!window.__setFrame && !!document.querySelector('.term-content'));
await page.evaluate(() => window.__setFrame('ready', 'plain'));

// Spy on outgoing key sends.
await page.evaluate(() => {
  const store = window.__storeForTest;
  window.__sent = [];
  window.__storeForTestSend = store.sendToAgent.bind(store);
  store.sendToAgent = async (agent, command) => {
    if (command && command.type === 'send_keys') window.__sent.push(...command.keys);
    return { ok: true };
  };
});
const sent = () => page.evaluate(() => window.__sent.map((k) => k.toLowerCase()));
const reset = () => page.evaluate(() => { window.__sent = []; });

const padSelector = '.term-keys .arrow-menu button';
const pad = () => page.locator(padSelector).first();

async function padBox() {
  return await pad().evaluate((el) => {
    const r = el.getBoundingClientRect();
    return { x: r.x + r.width / 2, y: r.y + r.height / 2, w: r.width, h: r.height };
  });
}

// Playwright mouse events land on the element under the point; our pad uses
// pointer capture, so raw coordinates are honest. touchscreen taps fire
// pointer events too, but mouse keeps this deterministic.
async function dragHold(dx, dy, holdMs) {
  const { x, y } = await padBox();
  await page.mouse.move(x, y);
  await page.mouse.down();
  await page.mouse.move(x + dx, y + dy, { steps: 3 });
  await page.waitForTimeout(holdMs);
  await page.mouse.up();
  await page.waitForTimeout(250); // release: next 200ms tick must NOT fire
}

// --- 1. Swipe down = Down arrow, held = repeats ~200ms ---------------------
await reset();
await dragHold(0, 40, 550);
let keys = await sent();
check('swipe down sends Down', keys.length > 0 && keys.every((k) => k === 'down'), `sent=${JSON.stringify(keys)}`);
check('held ~550ms repeats ≥3 times (200ms cadence)', keys.length >= 3, `count=${keys.length}`);

// --- 2. Release stops: one poll later, no new sends ------------------------
const countAfterRelease = keys.length;
await page.waitForTimeout(450);
keys = await sent();
check('release stops repeat', keys.length === countAfterRelease, `count=${keys.length}`);

// --- 3. Swipe left = Left arrow --------------------------------------------
await reset();
await dragHold(-40, 0, 50);
keys = await sent();
check('swipe left sends Left', keys.length > 0 && keys.every((k) => k === 'left'), `sent=${JSON.stringify(keys)}`);

// --- 4. Tap (no drag) opens the popup, does not send -----------------------
await reset();
{
  const { x, y } = await padBox();
  await page.mouse.move(x, y);
  await page.mouse.down();
  await page.waitForTimeout(60);
  await page.mouse.up();
  await page.waitForTimeout(100);
}
keys = await sent();
check('tap sends no arrow', keys.length === 0, `sent=${JSON.stringify(keys)}`);
const popupOpen = await page.evaluate(() => !!document.querySelector('.arrow-popup'));
check('tap opens arrow popup', popupOpen);

// --- 5. Popup arrows also hold-repeat --------------------------------------
if (popupOpen) {
  await reset();
  const downBtn = page.locator('.arrow-popup button[aria-label^="Down"]');
  const box = await downBtn.boundingBox();
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.waitForTimeout(550);
  await page.mouse.up();
  await page.waitForTimeout(250);
  keys = await sent();
  check('popup down hold repeats', keys.length >= 2 && keys.every((k) => k === 'down'), `sent=${JSON.stringify(keys)}`);
  await page.waitForTimeout(300);
  check('popup release stops repeat', (await sent()).length === keys.length);
}

// --- 6. Swipe right + up = right (dominant axis wins) -----------------------
await reset();
await dragHold(40, 8, 50);
keys = await sent();
check('mostly-horizontal swipe sends Right', keys.length > 0 && keys.every((k) => k === 'right'), `sent=${JSON.stringify(keys)}`);

// --- 7. Detected-menu arrows hold-repeat ----------------------------------
// Feed a footer with arrow hints so the menu bar appears (4 buttons ↑↓←→).
await page.evaluate(() => window.__setFrame(
  Array.from({ length: 60 }, (_, i) => `menu line ${i}`).join('\n')
    + '\nPick a row\n↑/↓ select  ←/→ move'
, 'plain'));
await page.waitForSelector('.generic-menu-actions .button', { timeout: 3000 });
await reset();
{
  // Hold the menu's Down arrow for ~600ms; release must stop.
  const btn = page.locator('.generic-menu-actions .button[data-keys="Down"]').first();
  const box = await btn.boundingBox();
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.waitForTimeout(600);
  await page.mouse.up();
  await page.waitForTimeout(250);
}
keys = await sent();
check('menu Down hold repeats', keys.length >= 3 && keys.every((k) => k === 'down'), `sent=${JSON.stringify(keys)}`);
await page.waitForTimeout(300);
check('menu release stops repeat', (await sent()).length === keys.length);

// --- 8. Floating ↔ stays the wide-view toggle, never an arrow pad --------
{
  await reset();
  const floatPad = page.locator('.terminal-copy .button[aria-label^="Arrow keys"]');
  check('no floating arrow pad (wide toggle stays wide)', !(await floatPad.count()), 'no Arrow-keys button in toolbar');
  const wideBtn = page.locator('.terminal-copy .button[aria-label="Toggle wide view"]').first();
  const wideVisible = await wideBtn.isVisible().catch(() => false);
  if (wideVisible) {
    const box = await wideBtn.boundingBox();
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
    await page.mouse.down();
    await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2 - 40, { steps: 3 });
    await page.mouse.up();
    keys = await sent();
    check('wide toggle drag sends no arrows', keys.length === 0, `sent=${JSON.stringify(keys)}`);
  }
}

await browser.close();
const fails = results.filter((r) => !r.ok).length;
console.log(`\n${results.length - fails}/${results.length} checks passed`);
process.exit(fails ? 1 : 0);
