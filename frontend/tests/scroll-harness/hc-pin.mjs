// HcView workspace-pin E2E: mounts the real HcView via the vite harness and
// verifies pin buttons, the Pinned section, machine-group headings, and
// unpin. Run with the vite dev server on :5199:
//   bun run vite --port 5199 --strictPort --host 127.0.0.1 &
//   bun tests/scroll-harness/hc-pin.mjs
import { chromium } from 'playwright';

const URL = 'http://127.0.0.1:5199/tests/scroll-harness/hc.html';
const results = [];
const check = (name, ok, detail = '') => {
  results.push({ name, ok });
  console.log(`${ok ? 'PASS' : 'FAIL'}  ${name}${detail ? `  (${detail})` : ''}`);
};

const browser = await chromium.launch();
const page = await browser.newPage({ viewport: { width: 390, height: 844 } });
page.on('console', (m) => { if (m.type() === 'error') console.log('[page]', m.text()); });
await page.goto(URL);
await page.waitForSelector('.workspace-card summary');
await page.waitForTimeout(300);

// 1) Regular space summaries expose a pin button.
const pinCount = await page.locator('.workspace-card summary .workspace-pin:not(.pinned)').count();
check('space summaries show pin button', pinCount >= 3, `count=${pinCount}`);

// 2) Remote machine spaces sit under an @<machine> heading — but ONLY
// when the block has visible spaces. DESKTOP's inventory mirrors
// macbookpro-codex/t147 as a qualified terminal; the dedupe hides that
// card (LAPTOP published the same address first), so DESKTOP's remote
// block is empty and must not emit a heading.
const headings = await page.locator('.machine-heading').allTextContents();
check('remote machine heading rendered', headings.some((t) => t.includes('@macbookpro-codex')), headings.join(','));
check('heading count is 1 (empty block suppressed)', headings.filter((t) => t.includes('@')).length === 1, headings.join(','));

// 3) No Pinned section yet.
check('no pinned section initially', await page.locator('section[aria-label="Pinned"]').count() === 0);

// 4) Pin the local space — it moves to the top Pinned section.
await page.locator('.workspace-card summary', { hasText: 'herdr-mobile' }).locator('.workspace-pin').click();
await page.waitForSelector('section[aria-label="Pinned"] .workspace-card');
const pinnedCards = await page.locator('section[aria-label="Pinned"] .workspace-card summary .agent-project').allTextContents();
check('pinned space moved to Pinned section', pinnedCards.some((t) => t.includes('herdr-mobile')), pinnedCards.join(','));

// 5) The pinned space vanished from its relay section.
const sectionCards = await page.locator('section:not([aria-label="Pinned"]) .workspace-card summary .agent-project').allTextContents();
check('pinned space removed from regular section', !sectionCards.some((t) => t.includes('herdr-mobile')), sectionCards.join(','));

// 6) Pin the remote space too; Pinned order follows click order.
await page.locator('.workspace-card summary', { hasText: 'of-machinery' }).locator('.workspace-pin').click();
await page.waitForFunction(() => document.querySelectorAll('section[aria-label="Pinned"] .workspace-card').length === 2);
const order = await page.locator('section[aria-label="Pinned"] .workspace-card summary .agent-project').allTextContents();
check('pin order preserved', order[0].includes('herdr-mobile') && order[1].includes('of-machinery'), order.join(','));

// 7) Unpin returns the space to its section.
await page.locator('section[aria-label="Pinned"] .workspace-card summary', { hasText: 'of-machinery' }).locator('.workspace-pin').click();
await page.waitForFunction(() => document.querySelectorAll('section[aria-label="Pinned"] .workspace-card').length === 1);
const restored = await page.locator('section:not([aria-label="Pinned"]) .workspace-card summary .agent-project').allTextContents();
check('unpinned space restored to section', restored.some((t) => t.includes('of-machinery')), restored.join(','));

await page.screenshot({ path: 'tests/scroll-harness/hc-pin.png', fullPage: true });

const failed = results.filter((r) => !r.ok);
console.log(`\n${results.length - failed.length}/${results.length} checks passed`);
await browser.close();
process.exit(failed.length ? 1 : 0);
