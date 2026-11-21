---
name: plugin-creator
description: Build a Polymux plugin and install it here. Use when someone wants a side panel beside a chat, a custom screen, or new agent tools for this chat, without going through the Marketplace.
---

# Plugin creator

A plugin you build here is a folder on this server. It stays out of the Marketplace. Once the user confirms it, a button for it appears in this chat, left of the call button, and its page opens in the side panel.

## Steps

1. Make a folder in your workspace, with `plugin.json` at its root.
2. Add the page and, if it needs one, the service.
3. Find `install_plugin` with `tool_search` and call it with the folder's absolute path. It checks the plugin and posts a card asking the user. Finish your turn; their answer starts your next one.
4. To change a plugin, edit the folder, raise `version`, and call `install_plugin` again.

Only direct chats can install, and never a chat under a Permissions gate.

## plugin.json

```json
{
  "name": "notes",
  "title": "Notes",
  "description": "Notes for this chat.",
  "version": "0.1.0",
  "kind": "scoped",
  "symbol": "note.text",
  "selectedSymbol": "note.text.badge.plus",
  "surface": "web",
  "page": "ui/index.html"
}
```

- `name`: lowercase letters, digits and hyphens.
- `symbol`, `selectedSymbol`: SF Symbol names, unfilled for unselected and a different one for selected. Both are needed with a `surface`.
- `kind`: `scoped` for anything with a page. Leave out `surface` and `page` for a plugin that only adds tools or skills.
- `hideClose`: `true` removes the panel's close button. Then give the page its own way out: `polymux.close()`.
- `service`, `skills`, `settings`, `provides`, `tools`: optional, below.

Local plugins can't be transports or gates, and `web` is the only screen they can have.

## The page

The page fills the whole side panel, and you decide everything inside it. Polymux keeps the panel's edge, which the user can drag to resize or past half its width to collapse, and the close button unless you hide it.

- It's 300 to 480 points wide on a Mac and fills the screen on iPhone, where the system's Back button closes it, so make the layout fluid. Start the page with `<meta name="viewport" content="width=device-width,initial-scale=1">`, or it shows tiny on iPhone.
- Everything it loads sits in the page's folder, by relative path. It can also load from other sites.
- `fetch("api/anything")` reaches your service's `/anything`, through the app's own connection. The page never holds a credential, so never put a secret in it.
- `polymux.chat` is the chat's ID, also in the URL as `?chat=`. `polymux.close()` closes the panel.
- Browser storage is gone when the panel closes, so keep anything lasting in your service.
- Responses arrive whole, so streaming won't reach the page.
- `document.documentElement.dataset.theme` is `light` or `dark`, and a `polymux:theme` event fires when it changes. Follow it.
- Plugin pages can't reach the rest of the Polymux API.

## The service

`service` is the path of one executable in the folder. It may be a script, and the folder is copied as it is, so build it first. Polymux starts it and restarts it if it stops.

- Listen on `127.0.0.1:$POLYMUX_PLUGIN_PORT`. Requests carry `Authorization: Bearer $POLYMUX_PLUGIN_TOKEN`; refuse any others.
- Answer `GET /health` with 200, or the plugin shows as failed.
- Keep state in `$POLYMUX_PLUGIN_DATA`. `$POLYMUX_PLUGIN_ROOT` is your folder's installed copy.
- Replacing a plugin keeps its data. Uninstalling removes it.

## Tools and skills

- Tools: set `"provides": ["tools"]` and list each in `tools` with `name`, `description` and JSON Schema `parameters`. `POST /tools/{name}` receives `{chat, agent, args}` and answers `{text}`. Agents see `plugin_name__tool_name`.
- Skills: `skills` lists Markdown files in the folder. They apply in the chats the plugin is active in.
- Settings: `settings` lists `key`, `title`, `type` (`bool` or `text`) and `default`; the app shows them on the plugin's page in Settings.
