import { chromium, devices } from 'playwright';
const browser = await chromium.launch();
const ctx = await browser.newContext({ ...devices['Pixel 7'], hasTouch: true });
const page = await ctx.newPage();
page.on('console', (m) => { if (m.type() === 'error') console.log('[page]', m.text()); });
await page.goto('http://127.0.0.1:5199/tests/scroll-harness/');
await page.waitForFunction(() => !!window.__setFrame && !!document.querySelector('.term-content'));
await page.evaluate(() => {
  const lines = Array.from({ length: 200 }, (_, i) =>
    `row ${String(i).padStart(3, '0')} ` + 'data-'.repeat(38).slice(0, 220 - `row ${i} `.length));
  window.__setFrame(lines.join('\n'), 'plain');
});
await page.waitForTimeout(500);

const geo = () => page.evaluate(() => {
  const sc = document.querySelector('.term-content');
  return { top: sc.scrollTop, left: sc.scrollLeft, sw: sc.scrollWidth, sh: sc.scrollHeight, cw: sc.clientWidth };
});

// Playwright lacks touch-drag; emulate with touch events via CDP.
const cdp = await ctx.newCDPSession(page);
async function touchDrag(x0, y0, x1, y1) {
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x: x0, y: y0 }] });
  for (let i = 1; i <= 8; i++) {
    await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: x0 + (x1 - x0) * i / 8, y: y0 + (y1 - y0) * i / 8 }] });
    await new Promise(r => setTimeout(r, 30));
  }
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
}
// First go mid-list, then drag DOWN to scroll up (finger down reveals
// earlier content).
await page.evaluate(() => {
  const sc = document.querySelector('.term-content');
  sc.scrollTop = 1200; sc.dispatchEvent(new Event('scroll'));
});
await page.waitForTimeout(300);
const v0 = await geo();
await touchDrag(195, 250, 195, 600);
await page.waitForTimeout(400);
const v1 = await geo();
console.log('vertical drag (scroll-up): scrollTop', v0.top, '->', v1.top, v1.top < v0.top ? 'SCROLLS' : 'STUCK');
await touchDrag(300, 400, 40, 400);
await page.waitForTimeout(400);
const v2 = await geo();
console.log('horizontal drag: scrollLeft', v1.left, '->', v2.left, v2.left > v1.left ? 'SCROLLS' : 'STUCK', 'scrollW', v2.sw);
await browser.close();
