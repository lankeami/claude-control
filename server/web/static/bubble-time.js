/**
 * Bubble Time — formats chat message timestamps as absolute local time
 * with seconds (e.g. "12:06:32 PM") instead of relative text that goes
 * stale ("just now", "3m ago").
 *
 * Dual-mode: ESM export for tests/Node.js, browser global for app.js.
 */
export function formatAbsoluteTime(dateStr) {
  if (!dateStr || typeof dateStr !== 'string') return '';
  // Server timestamps may lack a timezone suffix; treat bare ISO datetimes
  // as UTC (same normalization as formatRelativeTime in app.js).
  const normalized = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}(:\d{2}(\.\d+)?)?$/.test(dateStr)
    ? dateStr + 'Z'
    : dateStr;
  const date = new Date(normalized);
  if (isNaN(date.getTime())) return '';
  return date.toLocaleTimeString(undefined, {
    hour: 'numeric',
    minute: '2-digit',
    second: '2-digit',
  });
}

// Browser global — allows app.js (loaded as a regular script) to call this
// function after this module has been loaded via <script type="module">.
if (typeof window !== 'undefined') {
  window._ccFormatAbsoluteTime = formatAbsoluteTime;
}
