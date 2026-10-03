export interface TerminalMenuAction {
  label: string;
  keys: string[];
  cancel: boolean;
}

export interface TerminalMenu {
  signature: string;
  title: string;
  actions: TerminalMenuAction[];
}

const KEY_NAME: Record<string, string> = {
  enter: 'Enter',
  return: 'Enter',
  esc: 'Escape',
  escape: 'Escape',
  tab: 'Tab',
  space: ' ',
  spacebar: ' ',
  '↑': 'Up',
  '⇧': 'Up',
  '⬆': 'Up',
  '↓': 'Down',
  '⇩': 'Down',
  '⬇': 'Down',
  '←': 'Left',
  '⇦': 'Left',
  '⬅': 'Left',
  '→': 'Right',
  '⇨': 'Right',
  '➡': 'Right',
  '⬕': 'Right',
  y: 'y',
  n: 'n',
};

const ARROW_KEYS: Record<string, true> = { Left: true, Up: true, Down: true, Right: true };
const CANCEL_LABELS: Record<string, true> = { Cancel: true, Close: true, Quit: true, Back: true };
const ARROW_CHARS = '↑⇧⬆↓⇩⬇←⇦⬅→⇨➡⬕';
const ARROW_CHAR_CLASS = `[${ARROW_CHARS}]`;
const ARROW_VERB: Record<string, string> = { Left: 'left', Up: 'up', Down: 'down', Right: 'right' };

const DIRECTIONAL_ACTION_ORDER: Record<string, number> = {
  Left: 0,
  Up: 1,
  Down: 2,
  Right: 3,
  Enter: 4,
  Escape: 5,
};

const ACTION_LABEL: Record<string, string> = {
  accept: 'Accept',
  back: 'Back',
  cancel: 'Cancel',
  choose: 'Choose',
  clear: 'Clear',
  close: 'Close',
  confirm: 'Confirm',
  continue: 'Continue',
  deny: 'Deny',
  down: 'Down',
  edit: 'Edit',
  left: 'Left',
  move: 'Move',
  navigate: 'Navigate',
  next: 'Next',
  no: 'No',
  previous: 'Previous',
  quit: 'Quit',
  right: 'Right',
  select: 'Select',
  submit: 'Submit',
  toggle: 'Toggle',
  up: 'Up',
  yes: 'Yes',
};

const KEY_TOKEN = String.raw`(?:Ctrl\+[A-Za-z]|Alt\+[A-Za-z]|Shift\+[A-Za-z]|Enter|Return|Esc(?:ape)?|Tab|Space(?:bar)?|${ARROW_CHAR_CLASS})`;
const VERB_TOKEN = Object.keys(ACTION_LABEL).join('|');
const SINGLE_HINT = new RegExp(`(${KEY_TOKEN})\\s*(?:to|:|=|-)?\\s*(${VERB_TOKEN})\\b`, 'giu');
const PAIRED_ARROWS = new RegExp(`(${ARROW_CHAR_CLASS})\\s*[/|]\\s*(${ARROW_CHAR_CLASS})\\s*(?:to|:|=|-)?\\s*(navigate|move|select|choose|previous|next)?`, 'giu');
const YES_NO = /\b(?:press\s+)?([yn])\s*[/|]\s*([yn])\b/iu;
const EXPLICIT_LETTER = /\b([yn])\s*(?:to|:|=|-)+\s*(yes|no|accept|deny|confirm|cancel)\b/giu;
const FILTER_FOOTER = /(?:^|\n)\s*type\s+to\s+filter[\s•·|]+enter\s+to\s+select(?:[\s•·|]+(?:tab\s+to\s+edit|esc\s+to\s+(?:clear|close)))*\s*$/iu;

function normalizeKey(value: string): string {
  const lower = value.toLocaleLowerCase();
  if (KEY_NAME[lower]) return KEY_NAME[lower];
  const modifier = value.match(/^(Ctrl|Alt|Shift)\+([A-Za-z])$/iu);
  if (modifier) return `${modifier[1][0].toUpperCase()}${modifier[1].slice(1).toLowerCase()}+${modifier[2].toUpperCase()}`;
  return '';
}

function addAction(actions: TerminalMenuAction[], key: string, verb: string) {
  const normalized = normalizeKey(key);
  const rawLabel = ACTION_LABEL[verb.toLocaleLowerCase()] || '';
  if (!normalized || !rawLabel || actions.some((action) => action.keys[0] === normalized)) return;
  const label = ARROW_KEYS[normalized] ? normalized : rawLabel;
  actions.push({
    label,
    keys: [normalized],
    cancel: CANCEL_LABELS[rawLabel] ?? false,
  });
}

function cleanTitle(value: string): string {
  return value
    .replace(/[│┃║]/gu, ' ')
    .replace(/^[\s┌┐└┘╭╮╰╯─━═*#>[\]-]+|[\s┌┐└┘╭╮╰╯─━═*#<[\]-]+$/gu, '')
    .replace(/\s+/g, ' ')
    .trim()
    .slice(0, 100);
}

export function terminalTextInputMode(value: string): 'submit' | 'filter' | null {
  const tail = value.replace(/\r\n?/g, '\n').split('\n').slice(-8).join('\n');
  if (FILTER_FOOTER.test(tail)) return 'filter';
  if (/\benter(?:\s+or\s+ctrl\+q)?\s+submit\b/iu.test(tail)) return 'submit';
  return null;
}

export function detectTerminalMenu(value: string): TerminalMenu | null {
  const lines = value.replace(/\r\n?/g, '\n').split('\n');
  while (lines.length && !lines.at(-1)?.trim()) lines.pop();
  const tail = lines.slice(-10);
  if (!tail.length) return null;
  const candidateLines = tail.slice(-4);
  const footer = candidateLines.join(' · ');
  const actions: TerminalMenuAction[] = [];

  let pairedArrowsMatched = false;
  for (const match of footer.matchAll(PAIRED_ARROWS)) {
    pairedArrowsMatched = true;
    const dir1 = normalizeKey(match[1]);
    const dir2 = normalizeKey(match[2]);
    addAction(actions, match[1], match[3] || ARROW_VERB[dir1] || 'select');
    addAction(actions, match[2], match[3] || ARROW_VERB[dir2] || 'select');
  }
  for (const match of footer.matchAll(SINGLE_HINT)) addAction(actions, match[1], match[2]);
  for (const match of footer.matchAll(EXPLICIT_LETTER)) addAction(actions, match[1], match[2]);
  const yesNo = footer.match(YES_NO);
  if (yesNo) {
    addAction(actions, yesNo[1], yesNo[1].toLocaleLowerCase() === 'y' ? 'yes' : 'no');
    addAction(actions, yesNo[2], yesNo[2].toLocaleLowerCase() === 'y' ? 'yes' : 'no');
  }

  const presentArrows = new Set(actions.map((action) => action.keys[0]).filter((key) => ARROW_KEYS[key]));
  // A paired-arrow hint means the TUI offers a D-pad; fill in the missing
  // directions so left/right are always reachable when up/down are.
  if (pairedArrowsMatched || presentArrows.size >= 2) {
    addAction(actions, '←', 'left');
    addAction(actions, '↑', 'up');
    addAction(actions, '↓', 'down');
    addAction(actions, '→', 'right');
  }
  if (actions.some((action) => ARROW_KEYS[action.keys[0]])) {
    actions.sort((left, right) => (
      (DIRECTIONAL_ACTION_ORDER[left.keys[0]] ?? 100)
      - (DIRECTIONAL_ACTION_ORDER[right.keys[0]] ?? 100)
    ));
  }

  if (!actions.length) return null;
  const hintIndex = tail.findIndex((line) => {
    SINGLE_HINT.lastIndex = 0;
    PAIRED_ARROWS.lastIndex = 0;
    EXPLICIT_LETTER.lastIndex = 0;
    return SINGLE_HINT.test(line) || PAIRED_ARROWS.test(line) || EXPLICIT_LETTER.test(line) || YES_NO.test(line);
  });
  if (hintIndex < Math.max(0, tail.length - 4)) return null;
  const titleCandidates = tail.slice(0, hintIndex).map(cleanTitle).filter(Boolean);
  const title = titleCandidates.at(-1) || 'Terminal menu';
  const signature = `${title}\u0000${candidateLines.join('\n')}\u0000${actions.map((action) => action.keys.join('+')).join(',')}`;
  return { signature, title, actions };
}
