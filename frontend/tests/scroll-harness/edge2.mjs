import { chromium, devices } from 'playwright';
const browser = await chromium.launch();
const ctx = await browser.newContext({ ...devices['Pixel 7'], hasTouch: true });
const page = await ctx.newPage();
await page.goto('http://127.0.0.1:5199/tests/scroll-harness/');
await page.waitForFunction(() => !!window.__setFrame && !!document.querySelector('.term-content'));

// Force the phone's real condition: measured cell width emitted.
await page.evaluate(() => window.__setFrame(
  ['plain row ' + 'x'.repeat(80),
   '\x1b[32mcolored row\x1b[0m ' + 'y'.repeat(80),
   '┌' + '─'.repeat(78) + '┐',
   'row with last char marker END'].join('\n'), 'ansi'));
await page.waitForTimeout(400);
const probe = await page.evaluate(() => {
  const sc = document.querySelector('.term-content');
  // Measure the real monospace advance vs the probe's.
  const meas = document.createElement('span');
  meas.style.cssText = 'visibility:hidden;white-space:pre';
  meas.textContent = 'x'.repeat(100);
  sc.appendChild(meas);
  const textW = meas.getBoundingClientRect().width / 100;
  const probeW = document.querySelector('.term-content > span').getBoundingClientRect().width;
  meas.remove();
  // Set the var like the component would.
  sc.style.setProperty('--terminal-cell-width', `${textW}px`);
  const rows = [...document.querySelectorAll('.ansi-line')];
  return {
    textAdvancePerChar: textW,
    rows: rows.map((r) => ({
      text: r.textContent.slice(0, 20),
      len: r.textContent.length,
      scrollW: r.scrollWidth,   // what overflow:hidden might clip
      rectW: r.getBoundingClientRect().width,
      innerSpanW: r.firstElementChild ? r.firstElementChild.scrollWidth : null,
    })),
    scScrollW: sc.scrollWidth,
  };
});
console.log(JSON.stringify(probe, null, 2));
// Now pan all the way right and check last glyph rect.
const pan = await page.evaluate(() => {
  const sc = document.querySelector('.term-content');
  sc.scrollLeft = sc.scrollWidth;
  const rows = [...document.querySelectorAll('.ansi-line')];
  const widest = rows.reduce((a, b) => (a.scrollWidth > b.scrollWidth ? a : b));
  const last = widest.textContent.slice(-3);
  const rr = widest.getBoundingClientRect();
  const scr = sc.getBoundingClientRect();
  return { last, rowRight: rr.right, scRight: scr.right, gap: scr.right - rr.right };
});
console.log(JSON.stringify(pan, null, 2));
await browser.close();
