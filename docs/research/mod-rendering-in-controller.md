# Research: Claude Code Mod Rendering in Controller Web UI

**Date:** 2026-10-13
**Issue:** #309
**Author:** Jay Chinthrajah

## Background

Claude Code mods (plugins) render rich UI — panes, bands (above-prompt rows), status line entries, and toasts — using a surface-specific element system. The mod's `register(on, options)` function hooks events like `ui.render` to draw component trees from elements resolved via `$.ui.resolve(e)`, which returns surface-appropriate constructors (`Box`, `Text`, `Button`, etc.) for `terminal`, `desktop`, `vscode`, or `mobile`.

Users controlling sessions through the Controller web UI see **none of this** — only the chat transcript. This document researches how mod rendering works internally and evaluates approaches to bring mod UI into the Controller.

## Mod Internals: How UI Rendering Works

### Component Model

- Mods register `ui.render` hooks matched to specific components: `Pane` (side panels), `AbovePrompt` (bands), `UserMessage`, `CommandOutput`, etc.
- Each hook receives `($, e, next)` where `e` contains `component`, `surface`, `requestId`, `props`, and `viewport`.
- The hook returns a JSX tree built from `$.ui.resolve(e)` elements — **these differ per surface**. Terminal has `Raster`/`Image`; mobile lacks `Input`/`Select`/`Client`; desktop has `Svg`.
- The engine validates the tree against the surface's element table. Invalid trees are rejected (engine draws its own fallback).

### State Management

- Mods use `$.state` (session-scoped reactive atoms) for UI-driving data. Declared in a `types/index.d.ts` contract.
- `atom(ref, initial)` creates a reactive value; `read($, atom)` in a render hook subscribes that instance; `update($, atom, fn)` triggers a redraw of readers.
- `$.store` persists values across sessions.
- State changes trigger `$.ui.invalidate` implicitly — only the affected render sites redraw.

### What Gets Serialized

- **Component trees are NOT serialized to JSON.** They are rendered in-process by the engine per-surface. There is no wire format for the UI tree.
- **Transcript JSONL** contains `ui_log` entries for hook failures/refusals, but not rendered output.
- **`$.state` values** are held in-memory by the engine. No external API exposes them.
- **`$.ui.status(text)`** sets a status line string — just a text value, not a component tree.
- **`$.ui.toast(text)`** emits a transient notification — just a text value.

### Key Constraint

There is **no `web` surface** in the current plugin system. Mods cannot target a browser directly. The engine renders per-surface internally — there is no interception point to capture the rendered output as HTML or structured data for relay.

## Rendering Approaches

### Approach 1: Data-Only Bridge via `$.state` Exposure

**Concept:** Don't render mod component trees at all. Instead, expose mod state data (`$.state` atom values) through a new channel. The Controller web UI renders its own HTML based on the raw data, not the mod's component tree.

**How it would work:**
1. Add a hook or transcript-tailing filter that captures `$.state` changes for registered mod atoms
2. Forward state snapshots as a new SSE event type (`mod_state`) alongside existing transcript events
3. The Controller web UI maintains an Alpine.js reactive store of mod state
4. Custom HTML templates in the Controller render the data (e.g., a token counter mod's `{ inputTokens, outputTokens, cost }` renders as a styled bar)

**Pros:**
- No dependency on mod component trees — works even if the mod UI changes completely
- Clean separation: mods own their data, Controller owns its presentation
- Straightforward SSE integration with existing `Broadcaster` pipeline
- Can render data more richly than terminal constraints allow (charts, interactive tables)
- No engine modifications needed — only Controller-side changes

**Cons:**
- Requires per-mod rendering templates in the Controller (not generic)
- Won't display arbitrary mod UIs — only mods the Controller knows about
- Mod authors would need to document their state schema for Controller compatibility
- Data-only: loses any interactive elements (buttons, inputs) the mod provides

**Feasibility:** High — can be prototyped entirely within the Controller codebase.

### Approach 2: Transcript/Telemetry Tailing with HTML Approximation

**Concept:** Monitor the Claude Code transcript JSONL and hook events for mod-related entries. Build HTML approximations of mod output from whatever structured data appears in the transcript.

**How it would work:**
1. The interactive backend already tails transcript JSONL via `transcript.go`
2. Extend the tailer to recognize mod-related entries (tool registrations, status line updates, toast notifications, hook failures)
3. Forward these as SSE events to the web UI
4. Build a small HTML component library mirroring common mod primitives (Box → div, Text → span, progress bars, tables)
5. Render approximations of mod output from the available data

**Pros:**
- Uses existing infrastructure (transcript tailing is already in place)
- No engine modifications needed
- Can capture status line and toast notifications immediately
- Provides some visibility even for unknown mods (at least shows activity)

**Cons:**
- Transcript doesn't contain rendered UI trees — only log lines about hook failures/refusals
- Very lossy: most mod visual output never appears in the transcript
- Status line text is the only reliably available mod output
- Can't reconstruct pane or band content from transcript data alone
- Approximations may be misleading if they don't match actual mod output

**Feasibility:** Medium — easy to implement for status/toast, but fundamentally limited for pane/band content.

### Approach 3: New SSE Event Channel with Mod State Protocol

**Concept:** Define a lightweight protocol where the Controller injects hooks (via the generated settings file for managed sessions) that capture mod state changes and forward them through the existing hook-signal mechanism.

**How it would work:**
1. Add `state.set` hooks in the generated settings file that fire `hook-signal --event mod_state_update` with the state key and value
2. Add a new `/api/sessions/{id}/mod-state` endpoint that collects state updates
3. Forward state updates to the web UI via the existing SSE channel as `mod_state` events
4. Build a mod panel renderer in the web UI that displays state data in named slots

**Pros:**
- Captures actual mod state changes in real time
- Works with the existing hook-signal infrastructure
- Mods don't need to be modified — `state.set` is a hookable event
- Can throttle/debounce updates server-side
- Generic: works for any mod that uses `$.state`

**Cons:**
- `state.set` hooks via settings-file classic hooks may not capture `$.state` atom changes (those flow through the function hooks system, not classic hooks)
- Adds complexity to the already-complex generated settings file
- State values may be large or opaque without the mod's rendering context
- Still no component trees — only raw data

**Feasibility:** Medium — depends on whether `state.set` is hookable from classic hooks. If not, falls back to Approach 1.

### Approach 4: Plugin-Authored Web Adapter

**Concept:** Create a companion Claude Code mod that acts as a bridge — it hooks `ui.render` for known components, serializes the element tree to a JSON format, and exposes it via `$.process.run` or a local HTTP server that the Controller can poll.

**How it would work:**
1. Write a "controller-bridge" mod that hooks `ui.render` for `Pane` and `AbovePrompt`
2. In the hook, call `next(e)` to get the rendered tree from other mods, then serialize the result to JSON
3. Write the JSON to a known file path or expose via a local HTTP endpoint (using `$.process.spawn`)
4. The Controller's Go server polls this endpoint/file and forwards data via SSE
5. The web UI maps the serialized tree to HTML elements

**Pros:**
- Captures actual rendered component trees from other mods
- Generic: works for any mod's UI output
- The bridge mod can run as a regular plugin alongside other mods
- Could support interactive elements by relaying button presses back through the bridge

**Cons:**
- A `ui.render` hook that calls `next(e)` gets the tree from below — but the tree is surface-specific JSX, not a serializable structure. **The tree may not be JSON-serializable.**
- Adds a dependency: the bridge mod must be installed for Controller mod visibility
- Performance overhead of serializing and polling every render
- Complex: essentially building a remote rendering protocol
- The engine validates trees per-surface — a hook intercepting for relay purposes may conflict with the surface's element table

**Feasibility:** Low — the fundamental problem is that component trees are in-process JSX objects, not serializable data. This approach would require engine-level changes to support tree serialization.

## Recommendation

**Approach 1 (Data-Only Bridge) for the prototype, with Approach 2 (Transcript Tailing) for status/toast.**

Rationale:
1. **Pragmatic:** We can't access mod component trees from outside the engine — they're in-process JSX, not serialized data. Any approach requiring tree access needs engine changes we can't make.
2. **Incremental:** Start with the data that IS accessible (status line text, toast text, and structured state data from known mods) and render Controller-native HTML.
3. **Existing infrastructure:** The SSE `Broadcaster` and transcript tailing are already in place. Adding a new event type is straightforward.
4. **Good enough:** The highest-value mod outputs (token dashboards, cost trackers, agent status) are data-driven. A data-only approach covers the 80% case. Interactive mod elements (buttons, inputs) are lower priority for remote users who primarily monitor.

### Prototype Plan

For this prototype, we will:
1. Add a new SSE event type `mod_state` that carries mod panel data
2. Build a **mod bar** in the Controller header area with named panel slots
3. Build a **side pane** area alongside the chat view for larger mod panels
4. Create a mock mod data flow (server-side mock → SSE → web UI) to prove the pipeline works
5. Design the panel slot system so real mod data can plug in later

The prototype does NOT attempt to:
- Render arbitrary mod component trees
- Support interactive mod elements (buttons, inputs)
- Modify the Claude Code engine or plugin system
- Require mods to be modified for Controller compatibility

## Appendix: Mod UI Surfaces Reference

| Surface | Where | Elements | Notes |
|---------|-------|----------|-------|
| `terminal` | CLI terminal | Box, Text, Button, Input, Select, Client, Raster, Image, Markdown | Primary surface; has `Raster`/`Image` unique to it |
| `desktop` | Desktop app Code tab | Box, Text, Button, Input, Select, Svg, Markdown | Has `Svg`; no `Raster`/`Image`/`Client` |
| `vscode` | VS Code extension | Box, Text, Button, Input, Select, Markdown | No `Client`/`Svg`/`Raster`/`Image` |
| `mobile` | Mobile app | Box, Text, Button, Svg, Markdown | No `Input`/`Select`/`Client`/`Raster`/`Image` |
| `web` | **Does not exist** | N/A | Not a recognized surface in the plugin system |

## Appendix: Mod State Flow (Current vs. Proposed)

### Current (No Visibility)
```
Mod → $.state.set() → Engine renders per-surface → Terminal/Desktop shows UI
                                                  → Controller sees nothing
```

### Proposed (Data Bridge)
```
Mod → $.state.set() → Engine renders per-surface → Terminal/Desktop shows UI
  ↓
Server captures state data (via hook-signal or transcript)
  ↓
SSE mod_state event → Controller web UI → Alpine.js renders HTML panels
```
