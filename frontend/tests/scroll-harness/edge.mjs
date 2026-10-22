import { chromium } from 'playwright';
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
await page.goto('http://127.0.0.1:5199/tests/scroll-harness/');
await page.waitForFunction(() => !!window.__setFrame && !!document.querySelector('.term-content'));
await page.evaluate(() => window.__setFrame(
  ['short', 'x'.repeat(500) + 'END', 'also short'].join('\n'), 'plain'));
await page.waitForTimeout(400);
const probe = await page.evaluate(() => {
  const sc = document.querySelector('.term-content');
  sc.scrollLeft = sc.scrollWidth; // max
  sc.dispatchEvent(new Event('scroll'));
  const row = [...document.querySelectorAll('.ansi-line')].find((r) => r.textContent.includes('END'));
  const rr = row.getBoundingClientRect();
  const scr = sc.getBoundingClientRect();
  return {
    scrollLeft: sc.scrollLeft, scrollWidth: sc.scrollWidth, clientW: sc.clientWidth,
    maxLeft: sc.scrollWidth - sc.clientWidth,
    rowRight: rr.right, scRight: scr.right,
    // Is the last glyph inside the viewport?
    endVisible: rr.right <= scr.right + 0.5,
    lastChars: row.textContent.slice(-6),
    padding: getComputedStyle(sc).paddingRight,
  };
});
console.log(JSON.stringify(probe, null, 2));
await browser.close();
