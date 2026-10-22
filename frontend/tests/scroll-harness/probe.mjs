import { chromium } from 'playwright';
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
await page.goto('http://127.0.0.1:5199/tests/scroll-harness/');
await page.waitForFunction(() => !!window.__setFrame && !!document.querySelector('.term-content'));
await page.evaluate(() => window.__setFrame(
  ['short', 'x'.repeat(500), 'also short'].join('\n'), 'plain'));
await page.waitForTimeout(400);
const probe = await page.evaluate(() => {
  const sc = document.querySelector('.term-content');
  const screen = document.querySelector('.term-screen');
  const rows = [...document.querySelectorAll('.ansi-line')];
  const cs = getComputedStyle(sc);
  const ss = getComputedStyle(screen);
  return {
    scScrollW: sc.scrollWidth, scClientW: sc.clientWidth, scOverflowX: cs.overflowX,
    screenW: ss.width, screenDisplay: ss.display,
    rows: rows.map((r) => {
      const rs = getComputedStyle(r);
      const inner = r.firstElementChild;
      return {
        w: rs.width, ov: rs.overflow, ws: rs.whiteSpace, h: rs.height,
        innerW: inner ? getComputedStyle(inner).width : null,
        innerTag: inner?.tagName, rect: r.getBoundingClientRect().width,
      };
    }),
  };
});
console.log(JSON.stringify(probe, null, 2));
await browser.close();
