
import { chromium } from 'playwright';
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 390, height: 844 }, hasTouch: true });
await page.goto('http://127.0.0.1:5199/tests/scroll-harness/');
await page.waitForSelector('.term-content');
await page.evaluate(() => window.__setFrame('ready', 'plain'));
await page.evaluate(() => { document.documentElement.dataset.interfaceSize = 'large'; });
await page.waitForTimeout(300);
const m = await page.evaluate(() => {
  const el = document.querySelector('.term-keys');
  const cs = getComputedStyle(el);
  return { sw: el.scrollWidth, cw: el.clientWidth, sl: el.scrollLeft, overflowX: cs.overflowX, overflow: cs.overflow };
});
console.log(JSON.stringify(m));
// attempt scroll
const can = await page.evaluate(() => {
  const el = document.querySelector('.term-keys');
  el.scrollLeft = 9999;
  const v = el.scrollLeft;
  return { after: v };
});
console.log(JSON.stringify(can));
await browser.close();
