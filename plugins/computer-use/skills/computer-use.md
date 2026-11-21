---
name: computer-use
description: See and operate apps and web pages on this server's computer with Computer Use, in the background, without taking the user's focus.
---

# Computer Use

`computer_use__call` runs one cua-driver tool on the computer this Polymux server runs on. `computer_use__describe` lists supported tools and their arguments. Each action asks for approval.

Both are reached through codemode: `await tools.computer_use__call({tool: "list_windows", args: {app: "Safari"}})`. A screenshot comes back as an image block in `content`; pass it to `image()` to see it. The text block is a summary written for you; `structuredContent` holds the same answer as data (such as `windows[].pid` and `window_id`) for the script to use. Chain a read, an action and a re-read in one script when you don't need to look in between.

- **Find before you open.** If the Context plugin is on, read `context__summary` or `context__windows` first and work in the window that's already open. Otherwise `list_windows` with `app`, or `launch_app`, gives the `pid` and `window_id`.
- **Read narrowly.** `get_window_state` with a `query` returns the matching part of the accessibility tree and a screenshot. Re-read with `since: "latest"` after acting.
- **Act by element.** A tree row `[N]` is the `element_token` `<snapshot_id>:N`. Use pixel `x`,`y` only for what's missing from the tree.
- **Verify after acting.** Re-read the same window or tab after the approved action. Scripts and nested driver steps are not exposed.
- **Browser pages**: `get_browser_state` binds an exact native window and reads its page and tabs. If setup is needed, `browser_prepare` with `allow_launch: true` starts a separate browser profile after approval. Existing profiles also require the driver's own authorization. Use the resulting tab and target IDs with `browser_navigate`, `browser_click` and `browser_type`.
- **Stay out of the way.** Act in the background. Don't bring apps to the front, quit apps or close windows you didn't open unless the user asks.
- **Ask first** before sending, buying, deleting or anything that leaves the computer. Screenshots can show private content: capture only what the task needs.

If a call says cua-driver isn't running or lacks permission, tell the user to open CuaDriver.app on that computer and grant Accessibility and Screen Recording.
