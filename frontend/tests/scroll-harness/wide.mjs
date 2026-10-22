import { chromium } from 'playwright';
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
page.on('console', (m) => { if (m.type() === 'error') console.log('[page]', m.text()); });
await page.goto('http://127.0.0.1:5199/tests/scroll-harness/');
await page.waitForFunction(() => !!window.__setFrame && !!document.querySelector('.term-content'));
// 240-col snapshot like a wide-view leased pane.
await page.evaluate(() => {
  const lines = Array.from({ length: 200 }, (_, i) =>
    `row ${String(i).padStart(3, '0')} ` + 'data-'.repeat(38).slice(0, 220 - `row ${i} `.length));
  window.__setFrame(lines.join('\n'), 'plain');
});
await page.waitForTimeout(500);
const probe = await page.evaluate(() => {
  const sc = document.querySelector('.term-content');
  const screen = document.querySelector('.term-screen');
  const row = document.querySelector('.ansi-line');
  return {
    scScrollW: sc.scrollWidth, scClientW: sc.clientWidth,
    scScrollH: sc.scrollHeight, scClientH: sc.clientHeight,
    screenW: getComputedStyle(screen).width,
    contentVar: sc.style.getPropertyValue('--terminal-content-width'),
    rowW: getComputedStyle(row).width, rowText: row.textContent.length,
    styleAttr: sc.getAttribute('style'),
    touchAction: getComputedStyle(sc).touchAction,
    // What sits on top of the log center?
    topEl: document.elementFromPoint(195, 500)?.className,
  };
});
console.log(JSON.stringify(probe, null, 2));
// Try a real touch swipe.
await page.touchscreen.tap(195, 400);
const before = await page.evaluate(() => document.querySelector('.term-content').scrollTop);
await page.evaluate(() => { /* noop */ });
// Simulate touch drag vertically.
const sc = await page.$('.term-content');
const box = await sc.boundingBox();
await page.mouse.move(195, 600); await page.mouse.down();
await page.mouse.move(195, 300, { steps: 10 }); await page.mouse.up();
await page.waitForTimeout(300);
const after = await page.evaluate(() => document.querySelector('.term-content').scrollTop);
console.log('scrollTop before/after mouse-drag:', before, after);
await browser.close();
