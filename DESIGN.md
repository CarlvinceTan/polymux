# Polymux UI

Polymux-specific requirements for all UI work. Apply unless instructed otherwise.
Read and follow the [cdesign skill](.agents/skills/cdesign/SKILL.md) for general UI rules before any component, style, layout, colour, or motion work. The rules below take precedence where they differ.

## Responsiveness

- Update the UI optimistically. Run backend work in the background; on failure, roll back and follow Status and errors.

## Data

- Cache aggressively; show cached content first, then refresh.
- Everything previously loaded stays viewable offline.

## Status and errors

- Status and error wording must be true on every platform. Name what the user sees (chats, not sidebar).
- A lost connection shows one quiet app-wide status, not an error per screen. Keep cached content visible; reconnect and refresh automatically.
- A failed action rolls back and shows one short, quiet line beside what failed. Retry automatically when safe; the line clears itself once resolved.
- No retry buttons, and no alerts for recoverable failures.
- Placeholders and empty states are centred.
- Placeholders and empty states name the state only ("No providers yet."); they don't explain it.

## Components

- Prefer native platform controls over custom ones, unless instructed otherwise.
- Visible button labels are one word ("Send", "Save"). Use two only when one word would be ambiguous.

## Text

- Settings use clear labels without extra descriptions or explanatory text unless the user explicitly asks for them.
- No `fixedSize` on text inside a flexible row or panel.
