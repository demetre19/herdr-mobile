// Terminal scroll E2E driver: mounts the real TerminalView via the vite
// harness page and verifies the fixed-row contract. Run with the vite dev
// server on :5199:
//   bun run vite --port 5199 --strictPort --host 127.0.0.1 &
//   bun tests/scroll-harness/drive.mjs
import { chromium } from 'playwright';

const URL = 'http://127.0.0.1:5199/tests/scroll-harness/';
const results = [];
const check = (name, ok, detail = '') => {
  results.push({ name, ok });
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${detail ? `  (${detail})` : ''}`);
};

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
page.on('console', (m) => { if (m.type() === 'error') console.log('[page]', m.text()); });
await page.goto(URL);
await page.waitForFunction(() => !!window.__setFrame && !!document.querySelector('.term-content'));

// Feed an initial 120-line frame.
await page.evaluate(() => {
  const lines = Array.from({ length: 120 }, (_, i) => `line ${String(i).padStart(4, '0')} output text`);
  window.__setFrame(lines.join('\n'), 'plain');
});
await page.waitForFunction(() => {
  const sc = document.querySelector('.term-content');
  return sc && sc.scrollHeight > sc.clientHeight * 2;
});
await page.waitForTimeout(400);

const geo = () => page.evaluate(() => {
  const sc = document.querySelector('.term-content');
  return {
    top: sc.scrollTop, h: sc.scrollHeight, c: sc.clientHeight,
    bottom: sc.scrollHeight - sc.scrollTop - sc.clientHeight,
    jumpVisible: !!document.querySelector('.jump-bottom'),
    rowCount: Number(document.querySelector('.term-screen').dataset.terminalRowCount),
    rowH: document.querySelector('.ansi-line')?.getBoundingClientRect().height,
  };
});

// --- Test 1: pinned at bottom follows appended+cropped frames -------------
let g = await geo();
check('starts pinned at bottom', g.bottom < 2, `bottom=${g.bottom.toFixed(1)}`);
const h0 = g.h;
// Simulate relay snapshots: grow + front-crop (content beyond 1000 rows).
let allLines = Array.from({ length: 120 }, (_, i) => `line ${String(i).padStart(4, '0')} output text`);
for (let n = 0; n < 40; n++) {
  for (let j = 0; j < 6; j++) allLines.push(`burst ${n}.${j} ${'x'.repeat(20 + (j % 7) * 30)}`);
  const snapshot = allLines.slice(-300); // relay keeps 300-row history
  await page.evaluate((c) => window.__setFrame(c, 'plain'), snapshot.join('\n'));
  if (n % 10 === 0) await page.waitForTimeout(50);
}
await page.waitForTimeout(500);
g = await geo();
check('pinned follows 40 append+crop frames', g.bottom < 2, `bottom=${g.bottom.toFixed(1)}`);
check('scrollback retained locally (>300 rows)', g.rowCount > 300, `rows=${g.rowCount}`);

// --- Test 2: reading mode — tracked row must not move ---------------------
await page.evaluate(() => {
  const sc = document.querySelector('.term-content');
  // Scroll well clear of the 48px bottom magnet without overshooting the
  // document — a clamped scroll lands at the bottom and keeps the pin.
  sc.scrollTop = sc.scrollHeight - sc.clientHeight - 300;
  sc.dispatchEvent(new Event('scroll'));
});
await page.waitForTimeout(400);
const trackStart = await page.evaluate(() => {
  const sc = document.querySelector('.term-content');
  const top = sc.getBoundingClientRect().top;
  const el = [...document.querySelectorAll('[data-terminal-row]')]
    .find((r) => r.getBoundingClientRect().bottom > top + 2);
  return { y: el?.getBoundingClientRect().top ?? null, text: el?.textContent };
});
let maxDrift = 0;
let missing = 0;
for (let n = 0; n < 60; n++) {
  for (let j = 0; j < 4; j++) allLines.push(`stream ${n}.${j} tail ${'y'.repeat((j % 5) * 40)}`);
  const snapshot = allLines.slice(-300);
  await page.evaluate((c) => window.__setFrame(c, 'plain'), snapshot.join('\n'));
  await page.waitForTimeout(30);
  const y = await page.evaluate((text) => {
    const el = [...document.querySelectorAll('[data-terminal-row]')]
      .find((r) => r.textContent === text);
    return el?.getBoundingClientRect().top ?? null;
  }, trackStart.text);
  if (y === null) missing += 1;
  else maxDrift = Math.max(maxDrift, Math.abs(y - trackStart.y));
}
g = await geo();
check('tracked row stays mounted while reading', missing === 0, `missing=${missing}`);
check('tracked row stable while streaming', maxDrift <= 1.5, `maxDrift=${maxDrift.toFixed(2)}px`);
check('not pinned while reading', g.bottom > 48, `bottom=${g.bottom.toFixed(1)}`);
check('jump-to-latest button visible', g.jumpVisible === true);

// --- Test 3: scroll-up unpin, jump-to-bottom repin ------------------------
await page.evaluate(() => document.querySelector('.jump-bottom')?.click());
await page.waitForTimeout(300);
g = await geo();
check('jump button re-pins to bottom', g.bottom < 2, `bottom=${g.bottom.toFixed(1)}`);
for (let n = 0; n < 20; n++) {
  for (let j = 0; j < 4; j++) allLines.push(`post-repin ${n}.${j}`);
  await page.evaluate((c) => window.__setFrame(c, 'plain'), allLines.slice(-300).join('\n'));
}
await page.waitForTimeout(400);
g = await geo();
check('pin holds after repin + more output', g.bottom < 2, `bottom=${g.bottom.toFixed(1)}`);

// --- Test 4: fixed rows, long lines wrap at the pane width ----------------
// Regular view wraps a long line into extra fixed-height rows — no
// horizontal scroll. Wide-view horizontal pan is covered by wide2.mjs.
await page.evaluate(() => window.__setFrame(
  ['short', 'x'.repeat(500), 'also short'].join('\n'), 'plain'));
await page.waitForTimeout(300);
const fixed = await page.evaluate(() => {
  const rows = [...document.querySelectorAll('.ansi-line')];
  const heights = new Set(rows.map((r) => Math.round(r.getBoundingClientRect().height * 100)));
  const sc = document.querySelector('.term-content');
  return { heights: [...heights], canScrollX: sc.scrollWidth > sc.clientWidth,
    scrollW: sc.scrollWidth, clientW: sc.clientWidth,
    xRows: rows.filter((r) => /^x+$/.test(r.textContent)).length };
});
check('all rows exactly one height', fixed.heights.length === 1, `heights=${fixed.heights}`);
check('long line wraps into extra rows', fixed.xRows >= 8, `xRows=${fixed.xRows}`);
check('wrapped view does not scroll horizontally', !fixed.canScrollX, `w=${fixed.scrollW}/${fixed.clientW}`);

// --- Test 5: scroll-up during stream frees the pin, then re-pin -----------
// Reseed tall content (the 3-line frame left the view at the top) and pin.
await page.evaluate((c) => window.__setFrame(c, 'plain'), allLines.slice(-300).join('\n'));
await page.waitForTimeout(300);
await page.evaluate(() => {
  const sc = document.querySelector('.term-content');
  sc.scrollTop = 1500;
  sc.dispatchEvent(new Event('scroll'));
});
await page.waitForTimeout(200);
for (let n = 0; n < 10; n++) {
  allLines.push(`after-unpin ${n}`);
  await page.evaluate((c) => window.__setFrame(c, 'plain'), allLines.slice(-300).join('\n'));
}
await page.waitForTimeout(300);
const pos = await page.evaluate(() => document.querySelector('.term-content').scrollTop);
for (let n = 0; n < 10; n++) {
  allLines.push(`after-unpin-2 ${n}`);
  await page.evaluate((c) => window.__setFrame(c, 'plain'), allLines.slice(-300).join('\n'));
}
await page.waitForTimeout(300);
const pos2 = await page.evaluate(() => document.querySelector('.term-content').scrollTop);
check('unpinned view does not move under streaming', Math.abs(pos2 - pos) < 2, `Δ=${Math.abs(pos2 - pos).toFixed(2)}`);

await browser.close();
const failed = results.filter((r) => !r.ok);
console.log(`\n${results.length - failed.length}/${results.length} passed`);
process.exit(failed.length ? 1 : 0);
