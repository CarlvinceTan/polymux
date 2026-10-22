# Polymux — Gap Plan

What Polymux is still missing compared with three reference products, and where
Polymux already covers the same need in its own way.

Sources (read 22 Oct 2026):

- **Grok Bot**: [b-nnett/grok-bot-0.18-reconstructed](https://github.com/b-nnett/grok-bot-0.18-reconstructed), a reconstruction of the shipped 0.18 macOS app.
- **Rakazo**: [elie222/rakazo](https://github.com/elie222/rakazo), an open-source Grok Bot alternative.
- **OpenBot**: [CopilotKit/OpenBot](https://github.com/CopilotKit/OpenBot), self-hosted "AI coworkers". This is not [ashhart/OpenBot](https://github.com/ashhart/OpenBot), a separate, small Grok Bot reimplementation.

The references guide behaviour, not UI parity. New behaviour reuses Polymux's
shell, bubbles, cards and panels (see `DESIGN.md`). Every item must also pass
`docs/architecture.md`: one self-hosted server, clients only, nothing hosted or
paid in the core path, and no telemetry.

**Legend:** ✅ has · ◐ partial or different · — none · ? unverified, seen as a module name only.
**Priority:** P1 next, P2 soon, P3 later; `?` after a priority means it waits on a decision.

## 1. Conversation UX

| Feature | Grok Bot | Rakazo | OpenBot | Polymux today | Gap / next step | Pri |
| --- | --- | --- | --- | --- | --- | --- |
| Rich inline output | — | ◐ `render_plot`, mermaid | ✅ governed components and playground | ✅ `show_html` card, diff card | Add built-in mermaid and chart helpers on top of `show_html`, so bots don't write raw HTML for common visuals | P2 |
| Choice / question widget | ✅ widget (≤6 options, custom answer, dismiss on move-on) | ✅ answerable ask | ◐ | ✅ `ask_user`, `ChoiceCard` | Add an auto-dismiss mode for low-stakes questions | P3 |
| Emoji reactions (bot ↔ user) | ✅ `React` tapback tool | ✅ message reactions | — | — | Reactions on bubbles, plus an agent reaction tool that is used sparingly | P2 |
| Reply / quote a message | ✅ branched replies | ✅ replies, quotes | ◐ | ✅ reply preview, jump to message | — | — |
| Tool-activity display | ◐ tool-result cards | ✅ optional card: last six steps, collapses to count and duration | ✅ Activity tab with outputs | ◐ activity / typing animation | Optional, expandable "what it ran" card per run | P2 |
| Timeline events in thread | ✅ renamed, channel connected, routine changed | ◐ | ✅ | ◐ notices | Show system events for routine and profile changes inline | P3 |
| Slash commands / skill picker in composer | ? | ✅ slash and @mentions | ✅ `/skill` | ◐ @mentions in groups | `/` picker for skills and routines | P3 |
| Hidden / archived chats | ✅ hidden chats | ✅ `archive_bot` | ✅ hide or pin per user | ◐ pins, sections | Archive or hide a bot without deleting it | P3 |
| Time separators, scroll-by-date | ? | ✅ | ? | ✅ `ScrollDate` | — | — |
| Error trays (deduped failure banners) | ✅ trays service | ◐ run errors | ◐ | ? | Check how failed runs surface; dedupe repeated failures | P3 |

## 2. Agents and fleet

| Feature | Grok Bot | Rakazo | OpenBot | Polymux today | Gap / next step | Pri |
| --- | --- | --- | --- | --- | --- | --- |
| Create / edit agents from chat | ✅ create and edit agent tools | ✅ `spawn_bot`, `update_bot` | ✅ | ✅ `create_agent`, `update_agent` | — | — |
| Clone / duplicate an agent | ✅ agent clone | ✅ duplicate | — | — | Duplicate a bot's configuration only, without history, memory or credentials | P3 |
| Agent sets its own avatar | ✅ `avatar.set` / `clear` | ✅ avatar motion and shapes | ◐ seed | ◐ avatar editor (user only) | Let an agent pick or regenerate its avatar | P3 |
| First-run intro | ✅ kickstart prompt | ✅ intro turn that restates its role | ◐ | ✅ intro turn on create; user messages queue behind it (one job per bot); focus question renders through `ChoiceCard` | — | — |
| Fleet management (bot manages bots it created) | ✅ | ✅ opt-in per bot; user-started runs only | — | ◐ section tools, group tools | Per-bot opt-in switch; deny on runs started by routines or peers | P2 |
| Subagents (short-lived) | ✅ Task + Check / Message / Stop | ✅ `run_subagent` | — | ◐ `spawn_task`; `manage_subagents` lists status/output and stops children | Add steering for running children | P2 |
| Peer messaging / handoff | ✅ `SendToAgent` | ✅ `message_bot`, `handoff_to_bot` | ✅ `ask_agent` | ✅ `message_agent`, `handoff_agent` | — | — |
| Groups with multiple bots | ✅ group chat | ✅ groups | ✅ channels | ✅ groups, lead agent, turn-taking | — | — |
| Org chart / relationship view | ✅ org-chart graph | — | — | — | Read-only map of bots and who delegates to whom | P3 |
| Per-bot model and thinking level | ✅ model catalog with params | ✅ per bot and per routine | ✅ | ◐ provider catalogue | Per-routine model override; expose a thinking level | P2 |
| Bring your own agent endpoint | ◐ router: Cursor, Claude Code, Codex, OpenRouter | ✅ through Pi | ✅ any AG-UI endpoint | ◐ API providers only | Route turns to a local Claude Code / Codex login, or an external agent endpoint | P3 |
| Usage and cost tracking | ✅ local usage totals | ◐ | ◐ | — | Per-bot and per-routine token totals from provider usage | P2 |

## 3. Memory and knowledge

| Feature | Grok Bot | Rakazo | OpenBot | Polymux today | Gap / next step | Pri |
| --- | --- | --- | --- | --- | --- | --- |
| Per-agent memory | ✅ `memory.write` / `forget` | ✅ save / recall / forget | ✅ | ✅ `save_memory`, `recall_memory`, `forget_memory` | — | — |
| History retrieval | ✅ | ✅ `search_history`, `read_history` | ✅ | ✅ `recall_history` | — | — |
| Shared memory across bots | ✅ user memory + projects (create / join / leave), per-bot shards | ✅ `save_shared_memory`, brain | ✅ `/capabilities/memory` | ✅ shared user memory (`shared: true`) and projects (`manage_projects`, `project: <slug>`); per-bot shards, tagged with the bot that learned each fact, precedence own > project > shared | — | — |
| Memory synthesis / consolidation | ✅ daily synthesis service with verification | ✅ `brain_propose` | ✅ automatic learning (hosted) | ✅ daily upkeep inside background reviews (merge, forget superseded, promote to profile); tiers profile / log / note, notes fade after 30 days | User-visible diff of consolidation changes | P3 |
| User-visible memory page | ? | ✅ | ✅ | ? | Browse and edit what a bot remembers, in the profile | P2 |
| Bot proposes instruction changes | ◐ | ✅ `propose_instructions_update` | — | — | Bot suggests an edit to its own instructions; user approves it in a card | P2 |
| Image context from earlier turns | ? | ✅ budgeted recent images | ? | ? | Verify that earlier attachments reach later turns | P3 |

## 4. Routines, triggers and proactivity

| Feature | Grok Bot | Rakazo | OpenBot | Polymux today | Gap / next step | Pri |
| --- | --- | --- | --- | --- | --- | --- |
| Schedules (cron, interval, one-off) | ✅ cron, `@every` | ✅ plain language | ✅ 15-minute floor, cap of 20 | ✅ `manage_routines`: at, cron, interval | — | — |
| Content watches | ? finite watches | — | — | ✅ watch (model-free change detection) | Polymux's model-free version is unique | — |
| Routine editor UI | ✅ schedule editor, run history | ✅ | ✅ `/routines` | ◐ list in profile; run history through API and `manage_routines` | Show run history and edit timing in the UI | P2 |
| Event triggers: GitHub | ✅ PR, review, CI events | ✅ | — | ◐ generic `POST /triggers/{id}/events` | GitHub adapter (webhook or poll) mapped onto triggers | P2 |
| Event triggers: Slack / Linear / Sentry / PagerDuty | ✅ | ◐ | — | — | Adapters only after messaging channels land | P3 |
| Inbound webhooks | ? | ✅ | — | ✅ trigger endpoint (tailnet) | — | — |
| Failure fatigue | ? | ✅ stuck-work | ✅ one message, auto-off after 10 failures | ✅ first-failure notice, pause and notify after ten failures; success or explicit resume resets streak | — | — |
| Multi-step continuation | ◐ | ✅ | ✅ responsibilities | ✅ `manage_tasks` checkpoints, `track_work` | — | — |

## 5. Computer, browser and tools

| Feature | Grok Bot | Rakazo | OpenBot | Polymux today | Gap / next step | Pri |
| --- | --- | --- | --- | --- | --- | --- |
| Sandboxed computer per bot | ✅ remote box or local Docker | ✅ Team and Private computers | ✅ container per bot | ◐ per-conversation git worktrees on the trusted host | Optional local Docker sandbox per bot (no hosted provider). Decision needed, see below | P1? |
| Shell and files | ✅ Shell, Read, AwaitShell | ✅ | ✅ | ✅ read, write, edit, bash, find, ls | — | — |
| Run on the user's computer | ✅ ExternalShell | ✅ trusted local computer | — | ✅ paired devices, `run_device` over SSH | — | — |
| Browser automation | ✅ ~15 `browser_*` tools, CDP, tabs | ✅ `browser_act` / `snapshot` | ✅ governed per-bot Chromium | ◐ add-on: any browser MCP the user registers (its `browser_*` tools are dropped in native-only mode) | Stays an add-on by decision; no browser built into Polymux | — |
| GUI computer use | ✅ Computer tool, computer-use subagent | ✅ `computer_act` / `observe` | ◐ | ◐ Cua on paired Mac (device capability) | Expose it consistently; screenshot results in chat | P2 |
| Live screen + takeover | ✅ VNC view, hand off for login / 2FA | ✅ `request_takeover`, live desktop | ✅ take the wheel | — | Live view of the bot's browser or desktop; user takes control and hands it back | P2 |
| Copy files to / from the sandbox | ✅ CopyToBox / CopyFromBox | ✅ | ✅ | ✅ attachments in, `attach_file` out | — | — |
| Teach by recording | ✅ teach recording → skill | ✅ teach playbook | ✅ demonstrations | — | Record a demonstration and turn it into a skill | P3 |
| Web search / fetch | ◐ via plugins | ✅ `web_search`, `web_fetch` | ✅ Parallel / Exa / Firecrawl | ◐ add-on: any search/fetch MCP the user registers (e.g. Firecrawl, a SearXNG MCP); `http_request` built in | Stays an add-on by decision | — |
| Image / video generation | ? | ✅ | — | ✅ `generate_image`, `generate_video` | — | — |
| Cloud coding agents | ✅ CloudAgent (Cursor) | ✅ `cloud_agent_*` | — | — | Launch a local Claude Code / Codex run and track it (no hosted dependency) | P3 |

## 6. Integrations and skills

| Feature | Grok Bot | Rakazo | OpenBot | Polymux today | Gap / next step | Pri |
| --- | --- | --- | --- | --- | --- | --- |
| MCP servers | ✅ add / remove / restart / status | ✅ HTTPS and stdio, OAuth | ✅ governed | ✅ `manage_integrations`, scoped per agent, code mode | — | — |
| MCP OAuth (incl. no-DCR clients) | ✅ AuthenticateMcpServer | ✅ | ✅ | ◐ token store | Verify the OAuth flow end to end; manual client ID for providers without DCR | P2 |
| Per-connector custom instructions | ✅ SetMcpInstructions | ? | ◐ | — | Instructions attached to a connection | P3 |
| Plugin marketplace / catalogue | ✅ Search / Install plugins | ✅ Composio / Pipedream (optional) | ✅ catalogue | — | Curated local catalogue of MCP setups (no account required) | P3 |
| Skills: create / edit | ✅ workflows (`workflow.write`) | ✅ `skill_create` / `update` / `delete` | ✅ personal and deployment skills | ◐ trusted Markdown skill paths | Let an agent write and update its own skills | P2 |
| Secrets requested in chat | ✅ secret request | ✅ `request_secret`, origin-bound | ✅ vault | ✅ `request_secret`, `CredentialCard`, `http_request` | — | — |
| Site logins / saved passwords | ✅ 1Password bridge | ✅ `site_login` | ✅ `/settings/passwords` | — | Depends on the browser (section 5) | P3 |

## 7. Messaging channels and reach

| Feature | Grok Bot | Rakazo | OpenBot | Polymux today | Gap / next step | Pri |
| --- | --- | --- | --- | --- | --- | --- |
| Telegram / WhatsApp / Slack DMs | ◐ Slack and Discord "coming soon" | ✅ Telegram, WhatsApp, Slack, iMessage / SMS | ✅ Slack, Teams, SMS | — | One adapter (Telegram first: free, no business account) bound to a bot's thread | P2 |
| Push notifications | ✅ OS notifications | ✅ | ✅ | ✅ APNs | — | — |
| Deep links | ✅ | ? | ? | ◐ link opens Settings | Links to a bot, chat or message | P3 |
| Web client | — | ✅ | ✅ | — | Out of scope for now; architecture allows it | P3 |
| Android | — | ✅ | — | — | Not planned | — |

## 8. Voice

| Feature | Grok Bot | Rakazo | OpenBot | Polymux today | Gap / next step | Pri |
| --- | --- | --- | --- | --- | --- | --- |
| Dictation | ? | ✅ hold to talk | ✅ | ✅ on-device, Apple Intelligence polish | — | — |
| Spoken replies / calls | ? | ✅ half-duplex calls, barge-in | ✅ live calls | ✅ Apple voice, live (realtime) mode, private consultation | — | — |
| Bot calls the user | — | ✅ `call_user` | — | — | Bot can start a call for urgent items | P3 |

## 9. Safety, approvals and trust

| Feature | Grok Bot | Rakazo | OpenBot | Polymux today | Gap / next step | Pri |
| --- | --- | --- | --- | --- | --- | --- |
| Per-action approval policy | ✅ always / ask / never per local action | ✅ action approval with effect keys | ✅ CEL policy, fails closed | ◐ routine proposals need approval; otherwise unrestricted by design (old D40) | Ask / allow / never per tool class (shell, write, external effects), shown as a card. Decision needed, see below | P1? |
| Auto-review of actions | ✅ allow / block instruction lists | ✅ auto review (asks when unsure) | ◐ | — | Optional reviewer that approves routine-safe calls | P3 |
| Untrusted-content tagging | ✅ spotlight tags on tool results | ✅ untrusted envelope | ✅ untrusted envelope | ✅ marked envelopes for external tool, web, device and peer content, with embedded tags defused | — | — |
| Action audit log | ✅ action audit | ◐ | ✅ `/admin/audit` | ◐ activity records | Readable per-bot log of external effects | P3 |
| Content encryption at rest | — | ✅ | ✅ credentials | ◐ secrets in Keychain | Optional; SQLite stays an open format | P3 |
| Restricted egress for sandboxes | — | ✅ | ✅ | — | Depends on the sandbox (section 5) | P3 |

## Polymux-only (keep)

| Feature | Notes |
| --- | --- |
| Inline HTML cards (`show_html`) and diff cards | The rich-output path; extend it instead of adding components |
| Semantic search over messages, images, audio and video | EmbeddingGemma 2, sqlite-vec, `media-text` |
| Content watches | Model-free change detection that wakes a bot only on change |
| MCP code mode | Bots call MCP tools from code instead of one call at a time |
| Device pairing | Bonjour, Tailscale, dumbpipe; SSH `run_device` |
| Native SwiftUI on macOS and iOS | Dictation polish, panels, avatar kit |
| Export / import | `polymux export` / `import` |

## Not planned (conflicts with architecture)

| Feature | Seen in | Why not |
| --- | --- | --- |
| SSO / SAML / OIDC, people admin, team publication | OpenBot, Rakazo | Single-owner, self-hosted server |
| Hosted learning containers | OpenBot (CopilotKit Intelligence) | Requires a hosted service |
| Composio / Pipedream as the core integration path | Rakazo, OpenBot | Requires an account; MCP covers it |
| Required hosted sandboxes (E2B, Daytona, Railway) | Rakazo | Paid or hosted; a local Docker option only |
| Telemetry / analytics | Grok Bot, OpenBot | No telemetry without opt-in |

## Decisions needed

The previous `PLAN.md` (removed in `14caf86`) recorded choices that some gaps reverse. That plan predates the pivot, so confirm each one before building:

- **Approvals.** D40: permissions come from the user's requests and learned preferences, with no mandatory approval UI. All three references gate actions. Keep D40, or add an opt-in ask / allow / never policy?
- **Sandboxes.** Per-agent compute sandboxes were deferred "until separately designed". Is a local Docker sandbox now in scope?
- **Screens.** The old plan excluded a computer viewer, integration marketplace, and dedicated memory and routine screens. That affects live screen, catalogue, memory page and routine editor.

## Suggested order

1. **P1**
   - Done: shared memory. Web search, fetch and the browser stay user-configured MCP add-ons.
   - If the decisions above allow: an approval policy and a local Docker sandbox.
2. **P2**
   - Add reactions and the tool-activity card.
   - Add steering for running subagents; listing and stopping are implemented.
   - Add a routine run-history UI; history APIs and failure fatigue are implemented.
   - Add the GitHub trigger.
   - Add usage tracking.
   - Let agents write their own skills.
   - Add the Telegram channel.
   - Add live screen and takeover.
   - Done: untrusted-content tagging for external tool, device, peer, watch and trigger content.
3. **P3** Everything else, re-checked against the architecture rules before starting.
