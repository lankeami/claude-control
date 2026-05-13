/**
 * Option Selector — detects numbered/lettered choices in assistant messages
 * and returns structured option objects for rendering as clickable buttons.
 *
 * Dual-mode: ESM export for tests/Node.js, browser global for app.js.
 *
 * Detected patterns:
 *   Option A: Title  |  Option 1: Title  |  Option A (alone)
 *   A. Title         (capital letter + dot)
 *   1. Title         (number + dot)
 *   a) Title  |  A) Title  |  1) Title   (parenthesis)
 */
export function extractOptions(content) {
  if (!content) return [];
  const options = [];
  const lines = content.split('\n');

  // Strip markdown formatting from a line for clean matching
  const strip = (s) => s
    .replace(/^\s*#{1,6}\s*/, '')  // heading markers
    .replace(/\*\*/g, '')           // bold
    .replace(/\*/g, '')             // italic
    .replace(/^[-*]\s+/, '')        // list bullet
    .replace(/:$/, '')              // trailing colon
    .trim();

  for (const line of lines) {
    const c = strip(line);
    if (!c) continue;
    let m;

    // "Option A: Title", "Option B - Title", "Option A — Title" etc.
    m = c.match(/^option\s+([a-zA-Z]|\d+)\s*[:\-.—–]\s*(.+)/i);
    if (m) {
      const label = `Option ${m[1].toUpperCase()}`;
      const title = strip(m[2]);
      options.push({ label, text: title ? `${label}: ${title}` : label });
      continue;
    }

    // "Option A" alone (no separator or title)
    m = c.match(/^option\s+([a-zA-Z]|\d+)\s*$/i);
    if (m) {
      const label = `Option ${m[1].toUpperCase()}`;
      options.push({ label, text: label });
      continue;
    }

    // "A. Title" or "B. Title" (capital letter + dot — letter-choice style)
    m = c.match(/^([A-Z])\.\s+(.+)/);
    if (m) {
      const label = m[1];
      const title = strip(m[2]);
      options.push({ label, text: `${label}. ${title}` });
      continue;
    }

    // "A — Title" or "A – Title" (capital letter + em/en dash — Claude brainstorm style)
    m = c.match(/^([A-Z])\s+[—–]\s+(.+)/);
    if (m) {
      const label = m[1];
      const title = strip(m[2]);
      options.push({ label, text: `${label} — ${title}` });
      continue;
    }

    // "1. Title" / "2. Title" (numbered dot style)
    m = c.match(/^(\d+)\.\s+(.+)/);
    if (m) {
      const label = m[1];
      const title = strip(m[2]);
      options.push({ label, text: `${label}. ${title}` });
      continue;
    }

    // "a) Title" / "A) Title" / "1) Title" (parenthesis style)
    m = c.match(/^([a-zA-Z]|\d+)\)\s+(.+)/);
    if (m) {
      const label = m[1];
      const title = strip(m[2]);
      options.push({ label, text: `${label}) ${title}` });
      continue;
    }
  }

  return options.length >= 2 ? options : [];
}

// Browser global — allows app.js (loaded as a regular script) to call this
// function after this module has been loaded via <script type="module">.
if (typeof window !== 'undefined') {
  window._ccExtractOptions = extractOptions;
}
