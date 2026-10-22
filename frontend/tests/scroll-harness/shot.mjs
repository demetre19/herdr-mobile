import { chromium } from 'playwright';
const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
await page.goto('http://127.0.0.1:5199/tests/scroll-harness/');
await page.waitForFunction(() => !!window.__setFrame && !!document.querySelector('.term-content'));
// Mixed realistic content: ANSI colors, a separator (─── run = separator
// token?), wide box table, spinner-ish mutation.
await page.evaluate(() => {
  const box = [
    '\x1b[32m✓\x1b[0m build finished',
    '┌──────────┬───────────────┬────────┐',
    '│ package  │ version       │ status │',
    '├──────────┼───────────────┼────────┤',
    '│ relay    │ 1.0.21        │ ok     │',
    '│ frontend │ 1.0.21-harness│ ok     │',
    '└──────────┴───────────────┴────────┘',
    '\x1b[1m$ git status\x1b[0m',
    'On branch main — 12/12 tests passed',
    '─'.repeat(80),
    ...Array.from({ length: 60 }, (_, i) => `log ${i} ${'▧'.repeat(i % 9)}`),
  ];
  window.__setFrame(box.join('\n'), 'ansi');
});
await page.waitForTimeout(400);
// Scroll to top to see the box.
await page.evaluate(() => {
  const sc = document.querySelector('.term-content');
  sc.scrollTop = 0; sc.dispatchEvent(new Event('scroll'));
});
await page.waitForTimeout(400);
await page.screenshot({ path: '/tmp/term-top.png' });
await page.evaluate(() => document.querySelector('.jump-bottom').click());
await page.waitForTimeout(400);
await page.screenshot({ path: '/tmp/term-bottom.png' });
console.log('shots written');
await browser.close();
