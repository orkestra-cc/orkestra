// markdownToolbar — the pure text transforms behind MarkdownField's toolbar.
// Kept free of React/DOM so the wrap/prefix rules are unit-testable on their
// own; the component only supplies the textarea's current selection and
// writes back the returned text + selection.

export type MarkdownAction =
  'bold' | 'italic' | 'heading' | 'link' | 'list' | 'quote';

export interface TextSelection {
  start: number;
  end: number;
}

export interface ToolbarResult extends TextSelection {
  text: string;
}

const LINE_PREFIX: Partial<Record<MarkdownAction, string>> = {
  heading: '## ',
  list: '- ',
  quote: '> '
};

const WRAP: Partial<Record<MarkdownAction, string>> = {
  bold: '**',
  italic: '_'
};

const LINK_URL_PLACEHOLDER = 'url';

const wrap = (text: string, marker: string, sel: TextSelection) => {
  const inner = text.slice(sel.start, sel.end);
  const next =
    text.slice(0, sel.start) + marker + inner + marker + text.slice(sel.end);
  return {
    text: next,
    start: sel.start + marker.length,
    end: sel.end + marker.length
  };
};

const link = (text: string, sel: TextSelection): ToolbarResult => {
  const label = text.slice(sel.start, sel.end);
  const inserted = `[${label}](${LINK_URL_PLACEHOLDER})`;
  const urlStart = sel.start + label.length + 3; // "[" + label + "]("
  return {
    text: text.slice(0, sel.start) + inserted + text.slice(sel.end),
    start: urlStart,
    end: urlStart + LINK_URL_PLACEHOLDER.length
  };
};

// Prefix (or, when every touched line already carries it, un-prefix) each
// line the selection spans. A collapsed selection means "the cursor's line".
const prefixLines = (
  text: string,
  prefix: string,
  sel: TextSelection
): ToolbarResult => {
  const blockStart = text.lastIndexOf('\n', sel.start - 1) + 1;
  const nlAfter = text.indexOf('\n', sel.end);
  const blockEnd = nlAfter === -1 ? text.length : nlAfter;
  const lines = text.slice(blockStart, blockEnd).split('\n');
  const allPrefixed = lines.every(l => l.startsWith(prefix));
  const nextLines = allPrefixed
    ? lines.map(l => l.slice(prefix.length))
    : lines.map(l => prefix + l);
  const block = nextLines.join('\n');
  const next = text.slice(0, blockStart) + block + text.slice(blockEnd);
  // A real selection grows to cover the whole (re)prefixed block, so a
  // second click toggles exactly what the first one touched; a bare cursor
  // just rides along with its own line.
  if (sel.start !== sel.end) {
    return { text: next, start: blockStart, end: blockStart + block.length };
  }
  const shift = allPrefixed ? -prefix.length : prefix.length;
  const caret = Math.max(blockStart, sel.start + shift);
  return { text: next, start: caret, end: caret };
};

export const applyMarkdownAction = (
  text: string,
  action: MarkdownAction,
  sel: TextSelection
): ToolbarResult => {
  if (action === 'link') return link(text, sel);
  const marker = WRAP[action];
  if (marker) return wrap(text, marker, sel);
  return prefixLines(text, LINE_PREFIX[action] as string, sel);
};
