// Feeds a captured live hc observer frame sequence into the real
// TerminalView and measures scroll jumps — reproduces the phone jank.
import { chromium } from 'playwright';
import fs from 'node:fs';

const URL = 'http://127.0.0.1:5199/tests/scroll-harness/';
const frames = fs.readFileSync('/tmp/frames.txt', 'utf8').split('\x00FRAME\x00\n').slice(1);

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
page.on('console', (m) => { if (m.type() === 'error') console.log('[page]', m.text()); });
await page.goto(URL);
await page.waitForFunction(() => !!window.__setFrame && !!document.querySelector('.term-content'));

const geo = () => page.evaluate(() => {
  const sc = document.querySelector('.term-content');
  return { top: sc.scrollTop, h: sc.scrollHeight, c: sc.clientHeight,
    bottom: sc.scrollHeight - sc.scrollTop - sc.clientHeight,
    rows: Number(document.querySelector('.term-screen').dataset.terminalRowCount) };
});

// Feed frames like watch ticks at 300ms — record geometry after each.
const track = [];
for (const f of frames) {
  await page.evaluate((c) => window.__setFrame(c, 'ansi'), f);
  await page.waitForTimeout(120);
  track.push(await geo());
}
console.log('tick  scrollTop  scrollH  rows');
track.forEach((g, i) => console.log(String(i).padStart(4), String(g.top.toFixed(0)).padStart(8),
  String(g.h).padStart(8), String(g.rows).padStart(6)));
const tops = track.map(g => g.top);
const heights = track.map(g => g.h);
console.log('\nscrollTop min/max:', Math.min(...tops).toFixed(0), '/', Math.max(...tops).toFixed(0),
  ' range:', (Math.max(...tops) - Math.min(...tops)).toFixed(0));
console.log('scrollHeight min/max:', Math.min(...heights), '/', Math.max(...heights),
  ' range:', Math.max(...heights) - Math.min(...heights));
await browser.close();
