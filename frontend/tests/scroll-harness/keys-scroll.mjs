// Touch-pan probe for .term-keys: does a horizontal finger drag starting ON
// a button scroll the row? Uses CDP Input.dispatchTouchEvent — real touch.
import { chromium } from 'playwright';

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 390, height: 844 }, hasTouch: true });
await page.goto('http://127.0.0.1:5199/tests/scroll-harness/');
await page.waitForSelector('.term-content');
await page.evaluate(() => window.__setFrame('ready', 'plain'));
await page.evaluate(() => {
  const store = window.__storeForTest;
  window.__sent = [];
  store.sendToAgent = async (agent, command) => {
    if (command && command.type === 'send_keys') window.__sent.push(...command.keys);
    return { ok: true };
  };
});
await page.evaluate(() => { document.documentElement.dataset.interfaceSize = 'large'; });
await page.waitForTimeout(300);

const before = await page.evaluate(() => {
  const el = document.querySelector('.term-keys');
  return { sw: el.scrollWidth, cw: el.clientWidth, sl: el.scrollLeft };
});
console.log('before:', JSON.stringify(before));

// Find a NON-arrow-pad button center to start the pan (F keys).
const pt = await page.evaluate(() => {
  const btn = [...document.querySelectorAll('.term-keys .button')].find(b => !b.closest('.arrow-menu'));
  const r = btn.getBoundingClientRect();
  return { x: r.x + r.width / 2, y: r.y + r.height / 2, label: btn.textContent || btn.getAttribute('aria-label') };
});
console.log('pan starts on:', JSON.stringify(pt));

const cdp = await page.context().newCDPSession(page);
await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x: pt.x, y: pt.y, id: 1 }] });
for (let i = 1; i <= 10; i++) {
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: pt.x - i * 30, y: pt.y, id: 1 }] });
  await page.waitForTimeout(20);
}
await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
await page.waitForTimeout(300);

const after = await page.evaluate(() => {
  const el = document.querySelector('.term-keys');
  const btn = el.querySelector('.button');
  const pad = el.querySelector('.arrow-menu .button');
  return {
    sl: el.scrollLeft,
    btnTA: getComputedStyle(btn).touchAction,
    padTA: getComputedStyle(pad).touchAction,
    bgAttachment: getComputedStyle(el).backgroundAttachment,
  };
});
console.log('after pan on button:', JSON.stringify(after));
await page.evaluate(() => { document.querySelector('.term-keys').scrollLeft = 0; });
await page.screenshot({ path: '/tmp/keys-row.png', clip: { x: 0, y: 780, width: 390, height: 64 } });

// Same pan starting on the ↔ arrow pad.
const padPt = await page.evaluate(() => {
  const btn = document.querySelector('.term-keys .arrow-menu .button');
  const r = btn.getBoundingClientRect();
  return { x: r.x + r.width / 2, y: r.y + r.height / 2 };
});
await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ x: padPt.x, y: padPt.y, id: 1 }] });
for (let i = 1; i <= 6; i++) {
  await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ x: padPt.x + i * 20, y: padPt.y, id: 1 }] });
  await page.waitForTimeout(20);
}
await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
await page.waitForTimeout(300);
const padAfter = await page.evaluate(() => {
  const el = document.querySelector('.term-keys');
  const sent = window.__sent || [];
  return { sl: el.scrollLeft, sent };
});
console.log('after pan on pad:', JSON.stringify(padAfter));
await browser.close();
