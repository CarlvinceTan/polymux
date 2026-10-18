# Polymux UI

Core requirements for all UI work. Apply unless instructed otherwise.

## Responsiveness

- Every interaction responds immediately: visible feedback in the same frame, state change in under 50 ms.
- Update the UI optimistically. Run backend work in the background; on failure, roll back and show a clear, specific message (with retry where it makes sense).
- No network or disk calls on the main thread.
- Use spinners only for work that is expected to take long. Never block the UI on one.

## Data

- Cache aggressively; show cached content first, then refresh.
- Everything previously loaded stays viewable offline.

## Components

- Prefer native SwiftUI components over custom ones, unless instructed otherwise.
- Reuse shared app components; don't add screen-specific interaction patterns.
- Placeholders and empty states are centred.

## Consistency

- Use one solid theme and style across the app: shared tokens for colour, type, spacing, and radius. No one-off values.

## Contrast

- Meet WCAG AA: text at least 4.5:1; large text (18pt, or 14pt bold) and meaningful non-text elements (icons, indicators, control outlines) at least 3:1.
- Verify in both light and dark mode, in every state (resting, selected, focused, hovered, disabled, placeholder, secondary).
- Content on an accent or filled surface uses that surface's paired on-colour.
- Verify from the rendered result (screenshot sampling or Accessibility Inspector), not token values.
