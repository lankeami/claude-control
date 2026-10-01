/**
 * Sidebar Section Filter — shared filter-as-you-type helpers for the right
 * sidebar sections (Files, Skills, Issues, Pull Requests).
 *
 * Dual-mode: ESM exports for tests/Node.js, browser globals for app.js.
 *
 * Each section keeps its own independent filter string, so narrowing one
 * section never affects the others (see createSectionFilters).
 */

/**
 * Case-insensitive substring filter.
 * An empty/null query returns the original array untouched.
 */
export function filterByText(items, query, textFn) {
  if (!query) return items;
  const q = String(query).toLowerCase();
  const fn = textFn || ((x) => x);
  return items.filter((item) => String(fn(item) || '').toLowerCase().includes(q));
}

/**
 * Filter a file tree into a flat list of visible nodes.
 *
 * With no query, mirrors the default walk: a dir's children are visible only
 * when the dir is open. With a query, matches node names case-insensitively
 * and always includes the ancestor dirs of a match (force-expanded), so
 * matches inside collapsed dirs stay reachable.
 */
export function filterFileTree(tree, query) {
  const out = [];
  const items = tree || [];
  const q = String(query || '').toLowerCase();

  if (!q) {
    const walk = (nodes) => {
      for (const node of nodes) {
        out.push(node);
        if (node.isDir && node.open && node.children) walk(node.children);
      }
    };
    walk(items);
    return out;
  }

  const matches = (node) => String(node.name || '').toLowerCase().includes(q);
  const subtreeHasMatch = (node) => {
    if (matches(node)) return true;
    if (node.isDir && node.children) return node.children.some(subtreeHasMatch);
    return false;
  };
  const walk = (nodes) => {
    for (const node of nodes) {
      if (node.isDir) {
        if (subtreeHasMatch(node)) {
          out.push(node);
          if (node.children) walk(node.children);
        }
      } else if (matches(node)) {
        out.push(node);
      }
    }
  };
  walk(items);
  return out;
}

/**
 * One independent filter string per right-sidebar section.
 */
export function createSectionFilters() {
  return { files: '', skills: '', issues: '', pulls: '' };
}

/**
 * Apply a single section's filter to its items; other sections' filter
 * strings are never consulted.
 */
export function applySectionFilter(filters, section, items, textFn) {
  const query = filters ? filters[section] : '';
  return filterByText(items, query, textFn);
}

// Browser globals for app.js (which is not a module).
if (typeof window !== 'undefined') {
  window._ccFilterByText = filterByText;
  window._ccFilterFileTree = filterFileTree;
}
