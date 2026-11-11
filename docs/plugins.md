# Plugins

The app and server ship no plugin code. A plugin is a versioned package listed in an index file, and it's installed only when someone asks for that version. The model follows [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness/blob/master/docs/user/guide/plugin-market.md): one JSON index, an explicit confirmation showing the source, and pinned versions.

## Three kinds

| | Global | Scoped | Both |
| --- | --- | --- | --- |
| Applies to | Everything | The chats it's active in, direct or group | Everything, with a button in the chats it's active in |
| UI | A switcher entry that replaces the sidebar and content | A button left of the call button, opening the side panel; optional | A row above the chats that fills the content beside them, plus the Scoped button |
| Example | Hub | A chat's notes | Graph Visualiser |

The switcher only appears once a Global plugin can be shown or a second server is known, and always keeps Chats. With more than one server it lists each server's Chats, named after the server, and its Global plugins, and picking one moves the app to that server. iPhone's icons show the server's initial instead, or a number when initials clash. It's the native tab bar on iPhone and sits at the foot of the sidebar elsewhere. The sidebar's Search and + buttons stay in place and act on the plugin on show. Unread badges on the switcher can be turned off in Settings. Scoped buttons share one layout, and each chat shows only those active in it. On iPhone they collapse into a menu. The call button, the sidebar button and the side panel's close button belong to the app.

## How it fits together

- **Index**: [`plugins/index.json`](../plugins/index.json) on `dev`, served at `https://polymux.com/plugins/index.json`. The server reads it from `POLYMUX_PLUGIN_INDEX`, an `index-url` file in its plugins folder, or that default address. The last good copy is kept for offline use.
- **Website**: `apps/site` fetches the same index when it builds and lists it at `/plugins/`, so a new release shows there at the next site build. A failed fetch stops the build. `POLYMUX_SITE_REGISTRY` points a build at a local index instead.
- **Package**: a `.tgz` with a `plugin.json` at its root. The package decides what a plugin is, not its index row. Downloads are checked against the index's SHA-256 before unpacking.
- **Install state and layout**: on the server (`internal/plugins`, `plugins` tables), shared by every client.
- **Service**: a package may run one process. The server supervises it, gives it a port and token, and forwards `/plugins/{name}/api/*` to it through the normal device authentication. It gets its own folder for data, and uninstalling removes that folder.
- **Screens**: plugin UI ships in the app, because iOS can't load native code. A package names a `surface`, and `PluginSurface.swift` maps that name to views. An app without that surface shows Needs update. The exception is `web`: a Scoped package ships its own page, which fills the side panel; see [Web pages](#web-pages).
- **Skills**: `skills` in `plugin.json` lists Markdown skill files, and `plugins/scripts/package.sh` ships them. A Global or both plugin's apply to every chat, and a Scoped plugin's to its active chats. They reach an agent when its session next opens.

## Capabilities and settings

Everything beyond `kind` is opt-in. A plugin may have a `surface`, a `service`, `skills`, `settings`, and `provides`, which names what the service offers Polymux. The capabilities are `transport`, `gate` and `tools`; each needs a `service`.

`settings` lists options as `key`, `title`, `type` (`bool` or `text`) and `default`. The server keeps the values in the plugin's data folder (`GET`/`PUT /plugins/{name}/settings`), points the service at that file with `POLYMUX_PLUGIN_SETTINGS`, and restarts the service on a change.

## Transports

A transport plugin makes this server reachable from other networks, so a phone needs nothing beyond the plugin. The server gives its service two loopback addresses and asks where they ended up:

- `POLYMUX_PUBLISH_API`: the plain-HTTP API listener. The transport terminates TLS for it.
- `POLYMUX_PUBLISH_DEVICE`: the pinned-TLS device listener. The transport must pass it through untouched, or the pinned fingerprint no longer matches.
- `GET /transport` on the service answers `{state, message, api, device}`, with `state` one of `starting`, `ready` or `error`. A ready `api` goes in invite links; a ready `device` goes in device pairing links.

`GET /transports` on the server lists every transport's report. [`tailscale`](../plugins/tailscale) is the first: it runs `tailscale serve` for both listeners and reports the MagicDNS addresses. Its HTTPS needs certificates enabled once in the Tailscale admin console.

## Tools

A `tools` plugin gives agents tools its service answers. `tools` in `plugin.json` lists each one's `name`, `description` and JSON Schema `parameters`.

- **Asking**: `POST /tools/{name}` on the service receives `{chat, agent, args}` and answers `{text}`, which goes back to the model marked as untrusted.
- **Which chats**: tools follow the same rule as skills, and reach an agent when its session next opens.
- **Names**: an agent sees `plugin_name__tool_name`, with the plugin's hyphens as underscores. Gates decide these calls like any other.

## Gates

A gate is a Scoped plugin that decides agent tool calls in its active chats. Polymux asks it from `beforeTool` (`internal/server/coordination.go`), which every tool call passes, MCP calls inside codemode and voice turns included.

- **Asking**: `POST /gate` on the service receives `{chat, agent, dir, tool, args}` and answers `{allow, reason}`. A refused call never runs, and `reason` goes back to the model as its result.
- **Which chats**: a run answers to its own chat and to the chats of whatever started it, so a child task, a peer request or a task thread stays under the rules of the chat it came from (`boundChats`).
- **Failing closed**: every gate has to allow a call. A gate that's stopped, failed or being reinstalled blocks calls in its chats. Only turning it off for a chat or uninstalling it lifts its rules, and agents have no tool for either.
- **This machine**: `http_request` can't reach this machine from a bound chat, since an agent could read the local token and change its rules through the API.
- **New bots**: a bound agent can't hire a bot with a first task, which would start with no parent run. It can hire one and message it, which keeps the rules.

### Permissions

[`permissions`](../plugins/permissions) is the first gate. Each chat gets a list of rules, edited in its side panel and kept in the plugin's data folder.

| Kind | Covers | Pattern |
| --- | --- | --- |
| `read` | `read`, `find`, `ls`, `grep`, `attach_file` | A path. `*` stays within a folder, `**` crosses them |
| `write` | `write`, `edit` | A path |
| `command` | `bash`, `run_device` | A command. `*` matches anything |
| `tool` | Every tool, by name | A name, such as `github__*` |

Deny beats allow. A kind with any allow rule becomes an allow list, so whatever they don't cover is blocked. An empty pattern covers everything of that kind.

- **Paths** resolve as the file tools do, then again with links followed, so `..` and symlinks don't get around a rule. No rule can open the plugin's own folder.
- **Commands** are parsed, and each command in a line has to pass on its own, nested ones included. With allow rules, anything that can't be judged without running it is blocked: a command name built from variables, a redirect to a file, or an assignment.

Bash runs unsandboxed as the server's user, so path rules only bind the file tools. While commands are unrestricted, a bot can still reach those files and even edit the rules, and the panel says so. Deny rules on commands are best effort, since `sh -c` can hide a name. An allow list is the firm option, but it's only as tight as its commands: allowing `python *` allows anything.

## Web pages

A Scoped plugin can set `"surface": "web"` and `"page": "ui/index.html"` instead of a surface built into the app. Its page fills the whole side panel, and whatever is inside is the plugin's business.

- **What the app keeps**: the panel's edge, which resizes it and collapses it past half its minimum width, and the close button. `"hideClose": true` removes the button, and the page closes the panel with `polymux.close()`. On iPhone the panel is a page, so the system's Back stays.
- **Reaching the server**: the page loads from `polymux-plugin://<name>/`, a scheme the app answers itself (`PluginWeb.swift`). Files come from `GET /plugins/{name}/ui/*`, only out of the folder holding the page, and `api/*` goes to the plugin's service. The page never holds the device's credential and can't reach other plugins or the rest of the API. Responses arrive whole, so streaming doesn't work.
- **In the page**: `polymux.chat`, `?chat=`, and `document.documentElement.dataset.theme` with a `polymux:theme` event. Storage is thrown away with the panel. Other sites can be loaded inside the page; following a link to one opens the browser.

## Plugins made by bots

A bot can build a plugin in a folder and install it without an index, so it never reaches the Marketplace. The bundled [`plugin-creator`](../apps/server/internal/skills/plugin-creator/SKILL.md) skill teaches the format.

- **Installing**: `install_plugin` checks the folder and copies it to `.pending/`, then posts a card. Only a person answering that card installs it (`internal/plugins/local.go`, `internal/server/plugin_install.go`), and it turns on in that chat. The card's install answer is out of reach of `ask_user`.
- **Limits**: direct chats only, never a chat under a gate. A local plugin is Scoped or tools-only, can't be a transport or a gate, can't use a built-in surface, and can't replace a plugin from an index.
- **Source**: shows as `Made by <bot>` under Installed. A plugin's service runs as the server's user, so the card says so. A bot with a shell can already do what a plugin can; the card is consent, not a sandbox.

## plugin.json

```json
{
  "name": "hub",
  "title": "Hub",
  "description": "Your messages and mail in one place.",
  "symbol": "tray.2",
  "selectedSymbol": "tray.2.fill",
  "version": "0.1.1",
  "kind": "global",
  "surface": "hub",
  "engines": { "plugins": 3 },
  "service": "bin/polymux-hub",
  "skills": ["skills/email.md"],
  "provides": ["tools"],
  "tools": [{ "name": "email_accounts", "description": "List the user's mailboxes.", "parameters": { "type": "object", "properties": {} } }]
}
```

A transport adds `"provides": ["transport"]` and, if it has options, `"settings": [{ "key": "https-port", "title": "HTTPS port", "type": "text", "default": "8443" }]`; see the Tailscale plugin's [`plugin.json`](../plugins/tailscale/plugin.json).

`symbol` and `selectedSymbol` are the SF Symbol's unfilled and filled forms, for unselected and selected, and they're the only UI a plugin sets. A plugin with a `surface` must set both, or the install is refused. `engines.plugins` is the plugin API version it needs (`plugins.APIVersion`).

## Activity

`GET /work` lists the newest runs and handoffs across every chat, leaving out tool output. It only reads, so a plugin's screens can follow what's happening without marking anything read or waking a bot.

## Official plugins

[`plugins/`](../plugins/) holds the official plugins as one Go module, kept out of `go.work` so a plugin's dependencies can't change the server's build. Its [README](../plugins/README.md) covers packaging and releasing.

## Graph Visualiser

[`graph-visualiser`](../plugins/graph-visualiser) shows work as a map: who's working, where, on what, and what's passing between them. It's a view: nothing on it starts or changes work.

- **Data**: `WorkNode` and `WorkWire` in the schema are all it reads. A node's `kind` decides how it's drawn ([`WorkKind`](../packages/PolymuxKit/Sources/PolymuxKit/WorkKind.swift)), and an unknown kind still draws as a plain item, so new kinds can ship without breaking older apps. A wire's `carries` says what moves along it, and a node's `preview` names a picture of it.
- **Sources**: the app builds bots, handoffs, delegated tasks and tool use from `GET /work` (`WorkGraphBuilder`). The service adds repositories and GitHub, read with `git` and `gh`, and whatever agents report through its `report_work` tool. It serves previews as small JPEGs from `GET /preview`, so the app never reads a reported path itself.
- **The map** ([`WorkWorld`](../packages/PolymuxKit/Sources/PolymuxKit/WorkWorld.swift)): places become stations, and every bot's Terminal is the one Terminal, so a station stays put. Whoever has an active wire to a station stands there; everyone else rests at home. What a station holds becomes its items.
- **Districts**: stations that belong together share an islet with its own kind of ground. What explicitly holds a station comes first: a machine and what's on it, a pipeline and its steps. The rest group by what they are, as one Tools district until there are more than six, then by kind.
- **Pipelines**: a node of kind `pipeline` holds `step` nodes joined by wires. Steps stand in that order along a road and wear their number, and a step holds what it made.
- **Shape**: wide views put districts around home; a chat's side panel or a phone lays them out as a trail down the screen and opens on where the work is.
- **Drawing** (`apps/Polymux/Sources/Graph/World*.swift`): the ground and every model are drawn at the map's size, so they stay sharp at any zoom. `WorldModel` holds one model per kind of station.
- **Diagram**: the same graph as boxes and wires is one toolbar button away. [`GraphKit`](../packages/GraphKit) lays it out and routes its wires.
- **In a chat**: it shows that chat's bots and work, and the environment only where a report links to it.

## Hub

Hub, your messages and mail in one place, is the first plugin. [`plugins/hub`](../plugins/hub) holds its service and its documentation. Its screens ship in the app (`surface: "hub"`), its routes sit under `/plugins/hub/api`, and `contracts/generate.py` writes its wire types from the shared schema.
