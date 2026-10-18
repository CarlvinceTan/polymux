---
title: Skills and MCP
slug: skills-and-mcp
description: Extend Polymux with reusable skill instructions and Model Context Protocol servers.
section: Customising Polymux
sectionOrder: 3
order: 2
published: true
---

# Skills and MCP

Skills provide instructions for a kind of work. Model Context Protocol (MCP) connections supply external tools. In the native app, both are configured for agents on the **server**, not through the older Electron settings pages.

## Skills

An agent definition can reference trusted Markdown skill files using absolute paths on the server. Registering a path from your app device does not copy that file to a remote server.

A skill commonly uses a `SKILL.md` entrypoint with supporting references or scripts. The server loads the configured Markdown instructions with size limits; do not assume that every directory used by another agent app is automatically discovered.

Only register skills you trust. Their instructions can guide tool use on the server and connected computers. There is no native **Settings → Skills** browser at present.

## MCP servers

The server supports local stdio connections (`command`, `args`, and optional `env`) and remote Streamable HTTP connections. A stdio process runs on the server computer, not on the device displaying the chat.

## Make an integration change

Ask the agent in a **direct conversation** to inspect or manage its integrations. Its `manage_integrations` tool can persist supported MCP, skill, and CLI configuration. Give it the exact trusted service or file you intend to use, and review the proposed access before authorising setup.

Saved definition changes take effect on the next turn. The startup `POLYMUX_AGENTS` definitions file is not a live settings file, and existing stored definitions take precedence. There is no generic watcher for the older `~/.polymux/mcp.json` configuration.

## Permissions

MCP connections default to consequential-action approval classification unless an operator marks them read-only. That classification is a trust decision, not proof that a tool cannot write. Keep credentials out of prompts and source files, and review an integration's own account and permission requirements.
