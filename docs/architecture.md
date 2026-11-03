# Architecture

Requirements that every design and dependency choice must satisfy.

## Server

- A server is the single source of truth for its own agents, conversations, memory, schedules and tool connections.
- The server is a standalone Go binary with a CLI. It runs as its own process, independently of any GUI.
- All state lives on the server (SQLite). Clients hold no authoritative data and may cache only.
- Pairing and administration happen through the API, from any client or the CLI.

## Network

A network is one server and its devices. A device can be on several networks, each with its own entry in the switcher, and each server decides who it lets in.

- A network's name lives on its server, so every device shows the same one and it survives a move. The first device to reach an unnamed server gives it that device's next free number, as Network 1, Network 2 and so on. Anyone on the network can rename it.
- Joining a network from a Mac with its own server retires that server while it's empty. One with data stays, and the app asks whether to keep the two separate or merge them. A merge is never automatic, because the invite may be someone else's.
- Removing a device from a server is reversible and stays in force until it's allowed back.
- Moving or merging links the two servers first. A linked server is a device limited to `/network/peer/*`, so it can't read this server's chats.
- Moving a server hands its whole data folder to an empty server. The source seals itself and exits, so a worker copies data at rest. The copy is sealed to the target, along with the Keychain keys the folder doesn't hold, and stays aside until a commit; the target takes over when it next starts. Identity, tokens and the tunnel secret move with the data, so devices keep working. Two servers never share an identity: any failure before a commit restarts the source, and a commit whose answer is lost leaves the source down until someone checks.
- Merging brings one server's data into another, using the same sealed stream. The receiver seals, imports in one transaction, and restarts; the source retires, keeping its copy until someone deletes it. Every table and data folder has a rule, and a test fails on one without, so a new table forces a decision (`internal/store/merge.go`, `internal/server/merge.go`).
- A merge skips `durable.db`, whose IDs come from one server's counter and sit inside its records, so bots keep their chats and memory but start a new working context. Schedules arrive paused, same-named bots both stay, tagged with their server, and the receiver's device list stands.

Code: `internal/network`, `internal/device/network.go`, `internal/server/peers.go`, `internal/server/move.go`, `internal/server/merge.go`, `internal/store/merge.go`, `cmd/polymux/move.go`, and `SessionPool.swift` in the app.

## Bot prompts

A bot's prompt is built in layers, in this order. A layer only adds to the ones before it.

| Layer | Holds | Where | Editable |
| --- | --- | --- | --- |
| Core | Identity and how to treat data, for any host of the library | `CorePrompt` in `packages/runtime` | No |
| System | Polymux policy and required features, such as HTML cards. Tools add their own rules, so a rule only appears when its tool does | `buildPrompt` in `internal/agentruntime` | No |
| Everything else | Skills, and notes for the current turn | `skillPrompt`, and the turn input | By adding skills |
| Instructions | How the bot talks and any custom behaviour | The Instructions field, or `DefaultPrompt` when blank | Yes, by the user or a bot |

- Instructions go last so they have the final say.
- Skills load on demand: the prompt lists each by name and description, and a bot reads one from its file, or with `read_skill` when it has no file access, only when a task needs it. The card reference is the exception for bots without file access: cards are frequent, and a cached prompt costs less than a load per card.
- In a direct chat `send_to_user` is the bot's voice, and each message is one bubble. A reminder goes with every model request. Text written after the last message stays private, so a bot ends with `NO_RESPONSE`. A bot that skips the tool, or does more work and ends in plain text, still reaches the user: its text is delivered with one bubble per paragraph, including what it wrote beside its tool calls, such as a quick answer given mid-run.
- A message sent while a bot is working steers its run: it joins the bot's next model request with a note to answer and carry on, and the run keeps its place. A message from another bot stays its own queued turn, because its answer returns as a receipt.
- In a group chat, a message addressed to nobody first goes to a router: one short model call that picks the member whose role fits it, so the others skip a turn. When the router can't tell, every member takes a turn and speaks only if its role fits best. Either way one bot answers each request, not every bot that could.
- A bot takes one turn at a time in each conversation. Its task threads, delegated tasks and voice consultations have sessions of their own, so they run alongside its main chat, within the server's limit on turns at once. Turns a user is waiting on go first, and one slot is kept for them, so a reply never queues behind background work.
- A long command or codemode script steps aside when a message is waiting: it keeps running in the background and its result arrives as a new message. A script that has run a few seconds also stops at its next tool call, handing back what finished, so a change of mind lands before more tools run.
- A blank field follows `DefaultPrompt`, and clients are shown that text. Saving it unchanged keeps the field blank, so a bot only stops following the default once its instructions are edited.

## Bot quality

How a bot's handling of work is judged. A change to how bots run, delegate or schedule work must hold or improve each of these.

- Time to completion: the time from when a task is given to when its result reaches the user, averaged over tasks. It matters more than overall throughput, but never at the expense of the quality of the reply or the work.
- Responsiveness: the user never waits long for a first reply, even while the bot is busy; a quick "ok, on it" beats silence.
- Conversational quality: the bot talks like a person, including when it reports results: as long as the content needs, without filler.
- Throughput: when requests keep arriving before earlier ones finish, every one still gets done, none dropped or duplicated.
- Accuracy and recall over long contexts: facts and preferences given earlier still shape later work, however much has happened since.
- Long horizons: long, many-step work stays on track and finishes.
- Changing minds: a changed or cancelled request is followed promptly, and effects already made are undone or adjusted.
- Active guidance: constraints the user adds while work runs are applied to the result.
- Proactivity: a bot raises what the user would want to know, such as a conflict or a due reminder, and stays quiet about what they wouldn't.

## Clients

- Every GUI (macOS, iOS, web, or any future one) is a client only.
- No GUI embeds the server. The Mac app carries a copy and has launchd run it, so it outlives the app. A new install starts it; an update never does. Joining a network retires it while it's empty; a server with data stays until someone merges it or turns it off.
- Clients talk to the server through one documented API (`contracts`). No client gets a private or privileged path.
- Any client can be removed or replaced without affecting the server or other clients.
- Clients are interchangeable: the same server works with any number of them at once.

## Cost and accounts

- Free to run. Nothing in the core path requires a paid service, subscription or licence.
- No required third-party accounts. Core features must not depend on signing up for any service (cloud, auth, analytics, push relay, hosting, etc.).
- No Polymux-operated account or cloud service is required. The user's own server is the only backend.
- Optional integrations (e.g. a model provider the user chooses to connect) must be opt-in, replaceable, and never block core use.

## Ownership and privacy

- The user owns their data. It stays on their machine unless they explicitly send it elsewhere.
- No telemetry or analytics without explicit opt-in.
- Data is stored in open, documented formats and can be exported.

## Dependencies

- Prefer the standard library and platform frameworks. Add a dependency only when it clearly earns its place.
- Dependencies must be open source and usable without an account or key.
- The server must run with no network access to Polymux-controlled services.

## Platforms

- The server runs on macOS and Linux.
- Clients are native to their platform.
