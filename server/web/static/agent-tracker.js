// Pure state-transformation functions for agent/tool invocation tracking.
// All functions treat the invocations array as immutable — they return a new array.

/**
 * Applies a tool_use SSE event: completes any currently-running invocation,
 * then appends a new running entry.
 * @param {Array} invocations - current invocations array
 * @param {Object} block - tool_use block from SSE (id, name)
 * @param {string} label - human-readable label (from extractToolContext)
 * @param {number} [now] - current timestamp (injectable for tests)
 * @returns {Array} new invocations array
 */
export function applyToolUse(invocations, block, label, now = Date.now()) {
    const updated = invocations.map(inv => {
        if (inv.status !== 'running') return inv;
        return {
            ...inv,
            status: 'completed',
            endTime: now,
            duration: Math.round((now - inv.startTime) / 1000) + 's',
        };
    });
    return [
        ...updated,
        {
            id: block.id || ('inv-' + now),
            name: block.name || 'Tool',
            label,
            status: 'running',
            startTime: now,
            endTime: null,
            duration: null,
            lastOutput: null,
        },
    ];
}

/**
 * Applies a tool_result SSE event: completes the last running entry and
 * captures a snippet of the output.
 * @param {Array} invocations
 * @param {Object} data - tool_result data (may have .content)
 * @param {number} [now]
 * @returns {Array} new invocations array
 */
export function applyToolResult(invocations, data, now = Date.now()) {
    let found = false;
    const updated = [...invocations].reverse().map(inv => {
        if (!found && inv.status === 'running') {
            found = true;
            let lastOutput = null;
            if (data && data.content) {
                let text = '';
                if (typeof data.content === 'string') {
                    text = data.content;
                } else if (Array.isArray(data.content)) {
                    const textBlock = data.content.find(b => b.type === 'text');
                    if (textBlock) text = textBlock.text || '';
                }
                if (text) lastOutput = text.substring(0, 120);
            }
            return {
                ...inv,
                status: 'completed',
                endTime: now,
                duration: Math.round((now - inv.startTime) / 1000) + 's',
                lastOutput,
            };
        }
        return inv;
    }).reverse();
    return updated;
}

/**
 * Marks all remaining running entries as completed (called on session done).
 * @param {Array} invocations
 * @param {number} [now]
 * @returns {Array} new invocations array
 */
export function finalizeAll(invocations, now = Date.now()) {
    return invocations.map(inv => {
        if (inv.status !== 'running') return inv;
        return {
            ...inv,
            status: 'completed',
            endTime: now,
            duration: Math.round((now - inv.startTime) / 1000) + 's',
        };
    });
}

/**
 * Extracts a short human-readable label from a tool block.
 * @param {Object} block - tool block with .name and .input
 * @returns {string}
 */
export function extractToolContext(block) {
    const name = block.name || 'Tool';
    const input = block.input || {};
    let context = '';
    if (input.file_path) {
        const parts = input.file_path.split('/');
        context = parts[parts.length - 1];
    } else if (input.command) {
        context = input.command.substring(0, 30);
    } else if (input.pattern) {
        context = input.pattern.substring(0, 30);
    }
    const full = context ? `${name} ${context}` : name;
    return full.length > 40 ? full.substring(0, 37) + '...' : full;
}

// Browser global — allows app.js (loaded as a regular script) to call these
// functions after this module has been loaded via <script type="module">.
if (typeof window !== 'undefined') {
    window._ccAgentTracker = { applyToolUse, applyToolResult, finalizeAll, extractToolContext };
}
