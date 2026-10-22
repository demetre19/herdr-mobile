import { chromium } from 'playwright';
const b = await chromium.launch();
const p = await b.newPage({ viewport: { width: 390, height: 844 } });
await p.goto('http://127.0.0.1:5199/tests/scroll-harness/', { waitUntil: 'load' });
await p.waitForFunction(() => !!window.__setFrame);
const allLines = Array.from({ length: 120 }, (_, i) => `line ${String(i).padStart(4, '0')} output text`);
await p.evaluate((c) => window.__setFrame(c, 'plain'), allLines.join('\n'));
await p.waitForTimeout(500);
console.log(await p.evaluate(() => {
  const sc = document.querySelector('.term-content');
  const screen = document.querySelector('.term-screen');
  return { scrollH: sc.scrollHeight, clientH: sc.clientHeight, scrollTop: sc.scrollTop,
    rowCount: screen?.dataset?.terminalRowCount,
    domLines: document.querySelectorAll('.ansi-line').length,
    spacers: [...document.querySelectorAll('.terminal-virtual-spacer')].map(s => s.getBoundingClientRect().height),
    screenH: screen?.scrollHeight, origin: screen?.offsetTop,
    firstRowTop: document.querySelector('.ansi-line')?.getBoundingClientRect().top,
    scRectTop: sc.getBoundingClientRect().top };
}));
await b.close();
