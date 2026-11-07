# HTML Cards

Use semantic HTML and the preloaded kit first. Write content and behaviour; the kit supplies typography, spacing, surfaces, controls, focus states, responsive layouts, and light/dark colours. No imports, framework, CSS reset, or styling setup is needed. Keep ordinary controls on the kit defaults so their colours, typography, and surfaces stay cohesive with the chat pane. Custom HTML, CSS, SVG, and canvas remain available for layouts and visualisations the kit doesn't cover.

Start with `<div class="card">…</div>`: it supplies padding and vertical spacing, with no outer border, background, radius, or shadow. The message bubble owns the outside: it is the card's background, shape and edge, so never add an outer border, background, shadow or wrapping `.panel`; `.panel` is only for a distinct group inside the card. Put each inline `<script>` after the markup it uses. IDs are local to this card and must be unique. External resources, navigation, and network requests are blocked.

The kit uses borderless controls and groups with quiet fills and spacing, matching the chat pane. Use these defaults for ordinary UI. Focus rings and validation cues appear only when needed; use `hr` when a section genuinely needs a separator.

## Keep it focused

Use the smallest layout that answers the question, and make it useful on first render, before any input. Show only what explains the answer: don't invent scores, status cards, filters, or fact grids to fill space, and add only the controls the task needs.

| Content | Layout |
| --- | --- |
| One-off data | The chart or table first, with values on it and no metric row above |
| A few headline numbers | Two to four `.tile`s in a `.grid` |
| Simulation or explainer | One main visual, compact controls, and one line of detail |
| Step-through | One visual that updates with Back and Next, not every step side by side |
| Sequences or parallel work | Aligned lanes on one time axis, with waits and totals marked on it |
| Comparing several measures | Small multiples on a shared scale, all visible rather than behind a toggle |
| Part of a whole | One stacked chart, by period when time matters |

## Compose

| Need | Markup or class |
| --- | --- |
| Title and context | `.header` with a `div` containing `h3` and `p.muted`, plus an optional `.badge` |
| Vertical sections | `.stack`; `.stack.compact` for closer spacing |
| Inline controls | `.row` wraps; add `.between` to separate ends, `.grow` to let a child fill remaining space |
| Responsive columns | `.grid` fits columns of at least 140px and collapses automatically on narrow screens |
| Grouped surface | `.panel`, optionally `.stack`; avoid nesting several surfaces |
| Actions | `.actions` containing ordinary `button`s; `.primary` for the main action, `.danger` for a destructive action, `disabled` when unavailable |
| Status | `.badge` with `.good`, `.warn`, `.bad`, or `.info`; include a word so meaning doesn't depend on colour |
| Inline feedback | `.notice` with an optional status class; `role="status"` for updates or `role="alert"` for an action failure |
| Metrics | `.grid` of `.tile`s containing `.label`, `.value`, and optional `.delta.up` or `.delta.down` |
| Rows / checklist | `.list` containing `.item`s; a text `div` grows, `small` supplies secondary text; use `label.item` for a checkbox row |
| Key/value facts | `dl.facts` containing `<div><dt>Label</dt><dd>Value</dd></div>` |
| Table | `.table` around `table`; use `thead`, `th scope="col"`, and `.numeric` on numeric cells; wide content scrolls within the wrapper |
| Empty state | `.empty` with a heading naming the state, optionally `.actions` |
| Pending work | `<span class="loading" role="status">Working</span>`; honours Reduce Motion; use only while work is pending |

Use h1–h4, p, small, ul/ol, hr, details/summary, code, pre, and kbd directly. Buttons have one-word labels where clear ("Save", "Send"). Text wraps by default. Keep labels visible; don't use a placeholder as the only label.

For a timeline, use `ol.steps` with `li data-state="done"`, `"active"`, or `"failed"`; other items are pending. Include a written state and optional `small`.

## Controls

```html
<div class="field">
  <label for="name">Name</label>
  <input id="name" placeholder="Project name" aria-describedby="hint">
  <small id="hint">Shown in the report.</small>
</div>
<label class="item"><input type="checkbox" class="switch" checked><div>Notify<small>When the task finishes</small></div></label>
<div class="segmented" aria-label="Period" data-value="week">
  <button value="day">Day</button><button value="week">Week</button>
</div>
```

- Standard `input`, `select`, `textarea`, checkboxes, radios, `progress`, and `meter` are styled automatically. Use `fieldset` and `legend` for related radio choices. Use `input[type=range]` with a visible label and value; its fill follows its value, min, and max automatically.
- `.field` gives a label, control, and optional hint consistent spacing. For validation, set `aria-invalid="true"`, connect the message with `aria-describedby`, and use `<small class="error">…</small>`. Update the message and attribute together.
- `.segmented` takes direct button children, initializes automatically, skips disabled options, and supports arrow/Home/End keys. Read `group.value` or `group.dataset.value`; handle `change` with `event.detail.value`. Set `data-value` to select programmatically. Dynamically inserted groups and sliders initialize automatically too.
- Use local JS for immediate interaction. Keep real form controls, accessible names, and button semantics; don't replace them with clickable divs.
- Put a live value next to the control that changes it. Keep anything essential visible without hover, which phones don't have. Announce changing results with `role="status"` or `aria-live="polite"`, but not every animation frame.

## Visualisation

Chart.js 4 is preloaded as `Chart`. Put each canvas in `.chart` (220px) or `.chart.compact` (140px). Fonts, series colours, legends, tooltips, and theme updates are handled. Supply data and chart options, leaving colours unset unless they carry meaning. Give the canvas a text summary or accessible label. One y-axis per chart; multiple series need a legend. Prefer a metric or table for one or two numbers.

For diagrams, maps, simulations, games, and other custom visuals, use HTML/SVG/canvas/WebGL. The available tokens are `--text`, `--muted`, `--fill`, `--fill-strong`, `--separator`, `--outline`, `--raised`, `--surface`, `--accent`, `--on-accent`, `--good`, `--warn`, `--bad`, `--info`, and `--series-1` through `--series-8`. Spacing tokens `--space-1` through `--space-5` are 4, 8, 12, 16, and 24px; `--radius` and `--control-radius` match the kit. Use these instead of hard-coded colours and spacing. Canvas and WebGL can't read CSS variables: `polymuxColor('--text')` returns a token as `rgba()`, and the `polymux:theme` window event fires when the appearance changes, so redraw then.

Label axes and units, and put the values that matter on the chart. In custom SVG, label marks directly and add a legend only when that won't fit. On narrow widths, drop ticks and annotations rather than let labels overlap or clip.

Use one colour by default: `--series-1` for a single measure. Add other series colours only when they carry meaning, such as a category or status, and keep that meaning the same across the card. Don't give every item its own colour. Keep text, axes, and grid lines neutral.

## Size and scrolling

Omit width for automatic sizing up to 550px; `.card`, grids, and charts fill the available width. Height follows the content, so all of it shows and the card itself never scrolls. `height` sets a minimum for a canvas or game that needs a set size. Size content in px rather than `vh`, which follows the card and can keep it growing. Avoid `position: fixed`: it isn't counted in the card's height, so it can cover content or get cut off. Fit narrow phone widths; avoid fixed viewport sizes. Custom CSS is appropriate for the visualisation itself, but ordinary cards should need none.

Keep cards compact, ideally under 400px tall. When content runs longer, cut it down rather than boxing it in:

| Too much | Do this |
| --- | --- |
| Many rows | Show the rows that matter and a count of the rest, with `details` for the full list |
| Several topics | `.segmented` views showing one topic at a time |
| A long sequence | Paging with Back and Next buttons |
| Separate results | A second card |

Never put main content in a scrolling box. People miss what sits below the fold of a small box, and on a phone it fights the chat's own scrolling. Don't add `overflow:auto` or `overflow:scroll` with a `height` or `max-height` to a `.card`, `.stack`, `.panel`, `.list`, table, or other main content. Don't cut content off with fixed heights, `overflow:hidden`, or line clamps either.

Inner scrolling is only for a small part the card still makes sense without:

- Sideways: `.table` and `pre` already scroll wide content on their own, so add no overflow CSS.
- Up and down: only for secondary detail, such as a raw log or source excerpt, with the answer, summary, and actions outside it. Use one per card, never nested, and keep it under about 160px. The wheel stays in the box while the pointer is over it, so a large one traps the chat.

## State and replies

`polymuxSend(textOrJSON)` sends a reply from a real click, keyboard, or change handler, with context linking it to this card. Objects arrive as JSON text. One send per gesture, under 16 KB; automatic, timer, and synthetic sends are ignored. Only send when the interaction calls for an agent response; calculations, filtering, and selections can stay local.

`polymuxSave(value)` retains up to 64 KB of JSON while switching chats or scrolling. Restore from `polymuxState`; this is UI state, not durable storage. Restore a segmented control's `dataset.value` before reading it.

## Advanced cards

A card is a sandboxed mini-app. Beyond the kit, build whatever the task needs: drawing pads, simulations, games, editors, 3D views, audio tools, or data explorers.

| Works | How |
| --- | --- |
| Graphics | Canvas 2D, WebGL/WebGL2, OffscreenCanvas, SVG; `canvas.toDataURL()` exports an image |
| Motion | `requestAnimationFrame`, CSS transitions and animations, `element.animate()` |
| Direct input | Pointer events for drawing and dragging (`touch-action: none` on the surface), keyboard on focusable elements |
| Sound and media | Web Audio, `<audio>`, and `<video>` with data: or blob: sources; start audio from a click or tap |
| Heavy work | Web Workers from a `blob:` URL of inline code, WebAssembly from an embedded base64 module, `eval` and `new Function` |
| Structure | Inline `<script type="module">`, scripts inserted at runtime, `<dialog>`, `popover`, data: fonts |

Blocked: network access (fetch, XHR, WebSocket, including from workers), external or imported scripts, styles, images and fonts, links and navigation, new windows, form submission, `alert`/`confirm`/`prompt`, browser storage and cookies (use `polymuxSave`), clipboard, fullscreen, file pickers, camera, microphone, location, WebGPU, and `crypto.subtle`.

Design around these limits:

- Everything ships in 1 MB of HTML, including data, images, fonts, and WebAssembly. Generate visuals with code rather than embedding large media.
- The card is at most 550 CSS px wide, narrower on phones, and should stay under about 400px tall. Size canvases from their container (`ResizeObserver`, `devicePixelRatio`); use paging, panning, or zooming for anything larger.
- The mouse wheel and trackpad scroll the chat over a card, except over a part that scrolls inside it, so pan and zoom with pointer drags or buttons.
- A shown card is fixed: it can't be changed or receive live agent activity afterwards. Show a new card for new results, and don't present simulated progress as actual agent activity.
- A card can reload when the user switches chats or scrolls away, restarting timers, animations, and in-memory data. Restore what matters from `polymuxState`.
- An uncaught error or rejected promise adds "Part of this card didn't work." below the card. Catch failures in risky code and show a useful fallback.
- Stop animation loops when nothing moves, and move long calculations into a worker so the card stays responsive.

## Before showing

A shown card can't be changed, so read it through first. Check that every script name is defined, every queried element exists, the main interaction updates what it should, and the layout holds at phone width.

## Complete example

This uses only kit classes, markup, and behaviour:

```html
<div class="card">
  <header class="header"><div><h3>Weekly focus</h3><p class="muted">Choose your target</p></div><span class="badge good">Ready</span></header>
  <div class="grid">
    <div class="tile"><span class="label">Planned</span><span id="total" class="value">4 h</span></div>
    <div class="tile"><span class="label">Completed</span><span class="value">3 h</span></div>
  </div>
  <div class="field"><label for="hours">Hours</label><input id="hours" type="range" min="0" max="12" value="4"></div>
  <div class="actions"><button class="primary" id="save">Save</button><span class="muted">Updates your plan</span></div>
</div>
<script>
const hours = document.getElementById('hours'), total = document.getElementById('total');
if (polymuxState?.hours != null) hours.value = polymuxState.hours;
const refresh = () => { total.textContent = hours.value + ' h'; polymuxSave({hours: Number(hours.value)}); };
hours.addEventListener('input', refresh);
document.getElementById('save').addEventListener('click', () => polymuxSend({action: 'plan', hours: Number(hours.value)}));
refresh();
</script>
```
