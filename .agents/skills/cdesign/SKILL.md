---
name: cdesign
description: General UI design rules for responsive interactions, balanced layouts, consistent components, native platform fit, avoiding generic AI-looking UI, readable text, accessibility, and contrast. Read before creating, changing, or reviewing components, styling, layout, colour, or motion, alongside the project's own design rules.
---

# Cdesign

Apply these reusable UI rules alongside the project's design rules. Explicit project-specific rules take precedence where they differ.

## Responsiveness

- Every interaction responds immediately: visible feedback in the same frame, state change in under 50 ms.
- No network or disk calls on the main thread.
- Use spinners only for work that is expected to take long. Never block the UI on one.

## Status and errors

- Say what happened in plain words. Use user-facing names, not implementation terms. Never show raw system error text.
- Empty states and placeholders in an otherwise empty area are centred horizontally and vertically within the full available container, including when text wraps. Status messages are inline when content surrounds them.
- Verify empty-state alignment in the rendered UI against the container boundaries; centring a wrapper does not centre children aligned to its leading edge.

## Components

- Reuse shared components; don't add screen-specific interaction patterns.
- Inline actions must look interactive at rest. Use a visible native button style when text beside a value could be mistaken for a value or input hint. Familiar action icons, such as plus and refresh, can be borderless and omit visible text; retain accessible labels and hover hints.
- Keep field labels visible when useful, and put entry hints inside empty inputs. Give hints a distinct secondary text style that still meets contrast requirements; replace the hint with the entered value.
- For credential management, use named entries with masked identifiers and explicit add and remove controls. Keep naming separate from changing the secret, and make the supported credential lifecycle clear.

## Layout

- Balance content and controls across the available space within each section or area of focus. When there is room, avoid clustering everything on the left while leaving the right unused; keep related elements clearly grouped rather than forcing symmetry or adding filler.
- In settings rows, place the option name on the left and its controls or actions on the right. At narrow widths, stack them when needed to preserve readability and usability.
- Preserve the resource hierarchy: keep primary settings on the owning resource's main view, with related credentials or accounts listed within that context. Child editors must not take over the owner's settings.

## Consistency

- Use one solid theme and style throughout the interface: shared tokens for colour, type, spacing, and radius. No one-off values.
- Before designing an unfamiliar interaction, inspect established software with the same task. Carry over the useful behaviour and states, then use the project's components; state any parts of the reference that could not be verified.

## Native platforms

- Use the platform's controls, navigation and transitions. A reinvented bar, back gesture or switch reads as a ported website.
- Use semantic system colours and system materials; they adapt to appearance and contrast settings. No raw hex or hand-rolled blur.
- Use SF Symbols, in one consistent weight. Don't mix icon families.
- On iPhone: lay out inside the safe area, keep edge-swipe back working, use text styles that follow Dynamic Type, and give every tappable control at least 44×44 pt.
- Nothing depends on hover where touch is possible.
- Honour Reduce Motion: crossfade instead of parallax and large slides.

## Generic AI patterns

These are defaults to reject, unless the brief asks for one.

- No gradient text, coloured glows, or glass and blur as decoration.
- No emoji or Unicode glyphs in place of icons.
- No small uppercase label above a heading; the heading carries itself.
- No grid of identical icon, heading and text cards as the page structure, and no nested cards.
- No hero-metric template: a big number, a small label and supporting stats.
- No coloured side stripe on cards, list items or callouts.
- Declare elevation once: a border or a shadow, not both.
- Use the product's own words. No hype such as "seamless" or "supercharge", and no invented claims.

## Text

- When adjacent labels use different font sizes, align their text baselines (for example, a name and smaller role). Check the rendered glyphs together; vertically centred text boxes can leave the smaller label visibly too high. Prefer native baseline alignment over a fixed offset.
- Text never overflows its surface or pushes other elements out of place. Every text that can grow has a truncation or wrapping plan.
- In single-line rows, secondary text truncates first; the primary label and trailing fixed elements (time, badges, status dots) keep their size.
- Content text (messages, quotes, descriptions) wraps; it never clips.
- A label or editable field that can't show all of its text ends in an ellipsis at rest, never a mid-word clip.
- Truncated text keeps its full string in the accessibility label.
- Verify every text surface with the longest realistic content at the narrowest supported width.

## Contrast

- Meet WCAG AA: text at least 4.5:1; large text (18pt, or 14pt bold) and meaningful non-text elements (icons, indicators, control outlines) at least 3:1.
- Verify in both light and dark mode, in every state (resting, selected, focused, hovered, disabled, placeholder, secondary).
- Content on an accent or filled surface uses that surface's paired on-colour.
- Verify from the rendered result (screenshot sampling or the platform's accessibility inspector), not token values.
