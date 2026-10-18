---
name: polymux
description: Use this skill for any question about Polymux itself, including how Polymux works, using or configuring it, settings, integrations, device pairing, and troubleshooting.
---

# Polymux

Explain features using the user's current setup and exposed tools. Do not claim an integration or permission exists without checking.

- Chats keep conversations with individual agents; groups bring agents into a shared conversation. Route messages by stable agent IDs.
- Agents have configured identities, roles, models, and avatars. Help users find the relevant settings rather than inventing controls.
- Voice supports spoken interaction when configured; check audio devices, provider setup, and microphone permissions when it fails.
- Memory stores useful facts and preferences. Users can ask to recall, save, or forget memory; never store credentials.
- Routines schedule reminders or recurring work. Confirm time and timezone. Tasks checkpoint multi-step work; triggers connect external events to established tasks.
- Integrations include MCP configured through `~/.polymux/mcp.json` and trusted skill files registered through `manage_integrations`. Inspect existing setup first; changes apply on the next turn.
- Devices link with a single-use code from `polymux-server device pair -agent ID`, replacing ID with the agent's stable ID; `device list` and `device revoke` manage them. Linking is not authorization for unrelated actions.
- Settings cover the server connection, agents/models/providers, voice, integrations, and linked devices. Guide users from observed controls and configuration; never guess UI labels or expose credentials.

## Troubleshooting

1. Server reachable: check the configured address, whether the server is running, and connectivity from the affected device.
2. MCP unavailable: inspect configuration, command availability, authentication, and connection errors. Do not claim unavailable MCP tools worked.
3. Skill unreadable: check the registered path, file existence, readability, and valid text. Register trusted paths with `manage_integrations`.
4. Permissions: identify the missing device or OS permission and explain the normal user-controlled grant process. Never bypass approvals or change OS permissions yourself.
5. Logs: inspect relevant server and integration errors around the failure time. Scrub sensitive data before showing, copying, or sharing logs.

## Reports

Filing a bug report or sending feedback uses the bundled skill named report. Do not file an issue from this skill.
