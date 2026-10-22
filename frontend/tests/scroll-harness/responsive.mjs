import { chromium, devices } from 'playwright';
import assert from 'node:assert/strict';
const browser = await chromium.launch();
const context = await browser.newContext(devices['Pixel 7']);
const page = await context.newPage();
page.on('pageerror', error => { throw error; });
await page.addInitScript(() => window.addEventListener('unhandledrejection', event => { throw event.reason; }));
await page.goto('http://127.0.0.1:5199/tests/scroll-harness/');
await page.waitForFunction(() => window.__setFrame);
await page.evaluate(() => {
  document.body.dataset.view = 'terminal';
  const app = document.querySelector('#app'); app.className = 'app-shell';
  const layout = document.createElement('div'); layout.className = 'terminal-layout';
  app.append(layout); layout.append(document.querySelector('.terminal-view'));
});
const text = '│ prefix section 👩🏽‍💻 中文 e\u0301 headings match the rest of the output '.repeat(4) + 'LAST';
const ansi = '\x1b[1;36m' + text + '\x1b[0m';
const metrics = () => page.evaluate(() => {
  const sc = document.querySelector('.term-content');
  const style = getComputedStyle(sc);
  const rows = [...document.querySelectorAll('[data-terminal-row]')];
  return { width: sc.clientWidth, scroll: sc.scrollWidth, left: sc.scrollLeft,
    available: sc.clientWidth - parseFloat(style.paddingLeft) - parseFloat(style.paddingRight),
    rows: rows.map(row => ({ text: row.textContent, width: row.getBoundingClientRect().width })) };
});
const cdp = await context.newCDPSession(page);
async function pan() {
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x: 340, y: 200 }] });
  for (let step = 1; step <= 10; step++) {
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: 340 - step * 28, y: 200 }] });
    await page.waitForTimeout(30);
  }
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
  await page.waitForTimeout(300);
}
await page.evaluate(ansi => window.__setFrame(ansi), ansi);
for (const size of ['compact', 'regular', 'large']) {
  await page.evaluate(async size => { const { setInterfaceSize } = await import('/src/lib/preferences.ts'); setInterfaceSize(size); }, size);
  await page.waitForTimeout(400);
  const normal = await metrics();
  console.log(size, 'normal', JSON.stringify(normal));
  // Do not send another frame: interface changes must reflow cached output.
  assert.equal(normal.rows.map(row => row.text).join(''), text);
  assert(normal.rows.every(row => row.width <= normal.available + 1), 'normal row clips');
  assert(normal.scroll <= normal.width + 1, 'normal mode must not pan sideways');
  await page.click('[aria-label="Toggle wide view"]');
  await page.waitForTimeout(400);
  const wide = await metrics();
  console.log(size, 'wide cached', JSON.stringify(wide));
  assert.equal(wide.rows.length, 1, 'cached frame must unwrap without new output');
  assert(wide.scroll > wide.width + 100, 'wide frame must overflow horizontally');
  await pan();
  assert((await metrics()).left > 100, 'touch must pan wide ANSI output');
  await page.evaluate(() => { const sc = document.querySelector('.term-content'); sc.scrollLeft = sc.scrollWidth; });
  const endVisible = await page.evaluate(() => {
    const sc = document.querySelector('.term-content');
    const row = document.querySelector('[data-terminal-row]');
    const range = document.createRange(); range.selectNodeContents(row);
    return range.getBoundingClientRect().right <= sc.getBoundingClientRect().right;
  });
  assert(endVisible, 'last characters must be reachable');
  await page.screenshot({ path: `/tmp/herdr-${size}-wide.png` });
  await page.click('[aria-label="Toggle wide view"]');
  await page.waitForTimeout(400);
  assert((await metrics()).rows.length > 1, 'same frame must wrap again');
  await page.screenshot({ path: `/tmp/herdr-${size}-normal.png` });
}
if (process.env.HERDR_FRAME_CAPTURE) {
const { readFile } = await import('node:fs/promises');
const live = (await readFile(process.env.HERDR_FRAME_CAPTURE, 'utf8')).split('\n').slice(0, 18).join('\n');
await page.evaluate(live => window.__setFrame(live), live);
await page.waitForTimeout(400);
const expected = live.replace(/\x1b\[[0-9;?]*[ -/]*[@-~]/g, '').replace(/\r/g, '').split('\n').map(line => line.trimEnd()).join('');
assert.equal((await metrics()).rows.map(row => row.text).join('').replace(/\s/g, ''), expected.replace(/\s/g, ''), 'live ANSI capture must preserve complete non-whitespace text');
await page.click('[aria-label="Toggle wide view"]');
await page.waitForTimeout(400);
await pan();
assert((await metrics()).left > 100, 'captured live output must pan');
await page.screenshot({ path: '/tmp/herdr-live-wide.png' });
console.log('PASS live ANSI capture text reconstruction and touch pan');
}
await page.screenshot({ path: '/tmp/herdr-responsive.png' });
await browser.close();
