# UI interaction coverage

Tracks every user flow against the UI quality bar. A row is done only when it was exercised in the running app.

Status: `done` verified in the app, `flow` covered by the named E2E flow, `todo` not yet verified, `gap` could not be verified (reason given).

The bar for every row: answers in the same frame, no blank frame, no movement the user did not cause, smooth motion, touches only what it changes, failure shows one quiet line.

## Sidebar

| Flow | Checked | Status | Evidence |
| --- | --- | --- | --- |
| Launch with cached chats | First frame shows chats and open chat | done | `relaunch.js`; first frame reads the chat from cache before the window shows |
| Launch without a splash | Shell opens as soon as the session restores, without an animation delay | flow | `fixture-launch.js` |
| Launch with nothing cached | Quiet connecting state, no empty-account flash | flow | `empty-groups.js`, `fixture-startup.js`; chats wait for the session to restore |
| Launch offline | Cached content, one status | flow | `fixture-status.js` |
| Hover across rows | Only touched rows redraw, highlight follows pointer | done | Real pointer, 72 chats: shell 2, pane 6 for 90 moves |
| Scroll with a still pointer | No highlight trail, window does not redraw | done | Shell 0 during scroll; `PointerIntent` ignores rows sliding under the pointer |
| Hover then click a chat | Messages in first frame | done | Frame recording: no blank frame |
| Click a chat never opened | No empty flash | flow | `ChatWarmTests`: it starts from the sidebar preview, then fills in. Seen on iPhone, opening at the end |
| Arrow keys through chats | Neighbours ready, no wait | flow | Neighbours warm on selection; not timed by frame |
| Collapse and reveal sidebar | Button follows edge, smooth | done | `fixture-sidebar-reveal.js`; Toggle Sidebar (⌃⌘S) checked both ways with real keys |
| Drag divider, snap closed | Follows pointer | done | Real pointer: widens, snaps closed past half, Show sidebar restores it |
| Sections: create, rename, cancel | Inline edit, no jump | flow | `sections.js`, `section-bot.js` |
| Sections: collapse, reorder, delete | Rows animate, no flash | done | Collapse and expand animate over 12 to 13 frames on a real server; `sections.js` |
| Pin, unpin, reorder pins | Row moves once | flow | `fixture-sidebar.js`, `fixture-markers.js`; pin uses the same animated change |
| Drag chat to section or pin | Target shows, drops once | done | Real pointer: moves and pins once. The Pinned zone floats, so the list no longer shifts under the drag |
| Drag file across chats | Crossing opens nothing, resting opens | done | Real file from Finder: a 900 ms crossing opens nothing; resting 350 ms highlights and opens the row |
| Drop file on chat | Attaches to that chat | done | Real file from Finder: opens that chat with the file in its composer |
| Right-click row and section | Native menu, submenu safe path | flow | `sections.js`; native menus keep their own safe path to submenus |
| Delete chat, bot, section | Leaves at once, rolls back on failure | flow | `sections.js`, `provider-removal.js` |
| New bot, new group | Row and chat appear at once | flow | `fixture-new-bot.js`, `fixture-new-group.js`, `group.js` |

## Chat

| Flow | Checked | Status | Evidence |
| --- | --- | --- | --- |
| Reopen a recent chat | Instant, same scroll edge | flow | `fixture-chat-place.js`: same place when left partway up, the end when left there. Frame recording: swaps in one frame. Kept for the session, not across relaunch |
| Scroll transcript | Pane does not redraw per frame | done | Bubbles no longer redraw on pane updates; pane still updates for scroll position |
| Load earlier messages | Position holds | flow | `fixture-history.js` |
| Date pill while scrolling | Fades, no layout change | flow | `fixture-history.js` |
| Scroll to latest button | Appears once, smooth scroll | flow | `fixture-scroll-latest.js` |
| Jump to replied message | Scrolls, glows, settles | flow | `replies.js`, `fixture-replies.js` |
| Search in chat | Opens and closes without shifting | done | `search.js`; Find in Chat (⌘F) checked with real keys |
| Send text | Bubble at once, composer resets | flow | `chat.js` |
| Send and get a reply at the end of a chat | Chat only moves up, once per message | done | 60 fps window capture on a real server, macOS: +52 pt per message, no downward frame; iPhone compiled but not exercised |
| Send with attachments | Bubble at once, progress ring | flow | `fixture-media.js` |
| Reply and cancel | Preview fades, chat holds position | flow | `replies.js` |
| React, emoji picker | Optimistic, opens fast | flow | `replies.js`, `fixture-reaction-menu.js`; catalogue decodes at launch |
| Thinking dots with the window unfocused | Keep moving while a bot works | done | Six captures of an unfocused window all differ; they were identical before. About 12 to 15% CPU in Debug while working |
| Avatar interrupted mid-move | Finishes when the window returns | flow | `interrupted()` in `RenderChecks.swift`, which fails without the fix |
| Avatar restyled while resting | Finishes the morph, or lands at once if nothing can draw it | flow | `restyled()` in `RenderChecks.swift`, which also covers coming back from thinking |
| Every avatar in a group stack | Gap wraps the front avatar and never stalls the app | flow | `meshStacks()` and `gapPlacement()` in `RenderChecks.swift`; the iPhone mask was checked by hand |
| Avatar working dots in an unfocused window | Keep animating | done | Six captures while unfocused all differed; they were identical before the fix |
| Typing and working row | No flicker on switch | flow | `fixture-actions.js`, `fixture-tasks.js`; uses the server connection, so switching chats no longer drops it |
| A bot's tasks | Show their state only, don't open, stay out of the chat list | flow | `fixture-task-threads.js` on macOS; iPhone compiled but not exercised |
| Streaming reply | Sidebar preview stable | flow | `fixture-sidebar-preview.js` |
| Failed reply warning | One quiet bubble | flow | `fixture-reply-error.js`, `fixture-reply-recovered.js` |
| Replaced model warning | Says why another model replied | flow | `fixture-reply-notice.js` |
| Reasoning levels | Picker offers only the model's levels | flow | `fixture-model-levels.js` |
| Unsent and not sent | Inline, delete works | flow | `fixture-unsent.js` |
| Images load | Space reserved, nothing jumps | done | New uploads record their size and the bubble holds that space; older images still size on load |
| Video, audio, file, HTML cards | Stable size, quiet failure | flow | `fixture-media.js`, `fixture-cards.js`, `fixture-card-cases.js` |
| Choice, credential, vault cards | Answer once, no shift | flow | `fixture-cards.js`, `fixture-credentials.js`, `vault.js` |
| Drop, paste, attach | Previews animate in | flow | `fixture-composer-layout.js` |
| Attachment menu | Opens above + from its leading edge, Esc keeps the draft, Emoji inserts at the caret | flow | `fixture-attachment-menu.js` |
| Dictation and voice | Controls swap smoothly, hesitations leave the draft, group chats offer no speech conversation | flow | `fixture-dictation.js`, `fixture-dictation-text.js`, `voice-model.js`, `fixture-panel-close.js`; a live voice call was not tested |
| Mentions list | Opens above field, keyboard works | flow | `fixture-mentions.js`, new |

## Panels and menus

| Flow | Checked | Status | Evidence |
| --- | --- | --- | --- |
| Open and close any panel | Fade and settle, focus returns | done | Frame recording: 10 frame fade and settle, no pop; `fixture-panel-close.js` |
| Search everything | Still pointer never steals selection, Return opens highlight | flow | `fixture-search-keys.js`, new; `search.js` |
| Search arrow keys past the fold | Selection stays in view | flow | `fixture-search-keys.js`, new |
| Dropdowns | Opens on the selected item, room-aware, hover never scrolls | flow | `fixture-dropdowns.js`, `settings-menus.js` |
| Model picker | Same as dropdowns | flow | `model-picker.js`, `model-label.js` |
| Bot fallbacks | Saves each change, survives relaunch, Off stops the reply | flow | `fallbacks.js` |
| Voice fallbacks | Saves a custom order and voice, survives relaunch | flow | `voice-fallbacks.js` |
| Image and video models | Saves the mode, survives relaunch | flow | `media-models.js` |
| Profile panel | Slides, stays across chats, resize | done | Continuous open and close: about 55 to 60 fps in Release, 20 to 30 fps in Debug |
| Group editor | Tiles, remove on hover | flow | `group.js` |
| Avatar editor | Smooth preview | flow | `fixture-avatars.js`, `fixture-avatar-picker.js` |
| Settings pages | Switch instantly, cached | flow | `settings.js` |
| Providers | List, add, reorder, remove | flow | `providers.js`, `provider-keys.js`, `provider-order.js`, `provider-picker.js` |
| Devices | A network's page lists them; loading, failed, pair | flow | `fixture-devices.js`, `-slow`, `-failed`, `fixture-pairing.js` |
| Networks | Join a second, switch, each keeps its place, numbered names, rename, leave | flow | `servers.js`, `fixture-servers.js` |
| This Mac's server | New install, relaunch, off and on, join keeps it or retires it, or merges it from the join screen | flow | `fresh.js`, `join-empty.js`, `join-keeps.js`, `join-merge.js` |
| First-run welcome | Shown once on a new install, steps to permissions counted from the server, Settings reopens them | flow | `fresh.js`, `fixture-onboarding.js` |
| Move | Preview, move, app follows, old copy, a new server beside it | flow | `move.js` |
| Merge | Preview, merge, both sets of bots, same names told apart, old server goes | flow | `merge.js` |
| Vault | Locked, unlocked, import | flow | `vault.js`, `vault_browser.js`, `vault_kdbx.js`, `vault_passkey.js`, `vault_values.js` |
| Updates | Check and states | flow | `fixture-updates.js` |

## States

| Flow | Checked | Status | Evidence |
| --- | --- | --- | --- |
| Connection lost and back | One status, clears itself | done | `fixture-status.js`. A frozen server only shows after the 20 s request timeout |
| Empty chats, sections, search, models | Centred, names the state | flow | `fixture-profile-empty.js`, `fixture-providers.js`; wording unified |
| Failed action in each store | Rolls back, one line, clears | flow | Package tests; sidebar notes now float over the list |
| Reduce Motion | Nothing animates | gap | No launch animation remains. Other transitions go through `Motion`; not run on macOS, where it is a system setting |
| Light and dark | Fills and fades match | flow | Tints use the adaptive primary colour; `fixture-site.js` runs dark, most flows light |
| iPhone | Pages, sheets, no hover state | done | Simulator: list, first and repeat chat opens, profile, New Bot sheet, dark mode. Keyboard stays down, chats open at their end |

## Performance budget

Measured with `--verify-fixture --verify-load --verify-renders` (72 chats, Debug build).

| Gesture | Before | After |
| --- | --- | --- |
| 90 pointer moves over rows | shell 36, pane 37 | shell 2, pane 6 |
| Sidebar scroll | shell 41-99, pane 44-105 | shell 0, pane 3-4 |
| Scroll an empty chat | shell 40-42, pane 65-67 | shell 2, pane 30 |
| Open a chat with 100 unsynced events | shell about 100, 3 s per click | shell 6, under 1 s per click including the test driver |
| Idle CPU, memory on an empty chat | 1.5%, 94 MB | under 2% with brief spikes |
| Memory with 12 chats kept | not measured | 191 to 223 MB in Debug, flat across repeated switching |
| Release build, idle with six chats | not measured | 81 MB, 1 to 2% CPU |

Release builds have no fixture, so the render counts are Debug numbers.
