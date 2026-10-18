---
title: Hub
slug: hub
description: Understand the difference between native agent conversations and the older Electron Hub.
section: Using Polymux
sectionOrder: 2
order: 2
published: true
---

# Hub

The native app's conversation list is for **Polymux agents and groups**. It is not the messaging and email inbox shown in older Electron screenshots.

## Native app

There is no **Settings → Hub** connection screen or unified third-party inbox in the native app. Adding an agent does not sign in to WhatsApp, Telegram, email, or another messaging service.

An agent may use messaging or email tools configured on its server. Whether it can read or send depends on those tools and the accounts you explicitly connect there. Do not assume an account is connected because its service appeared on the old website.

See [Skills and MCP](/docs/skills-and-mcp/) for server tools and [Privacy and permissions](/docs/privacy-and-permissions/) before granting account access. Polymux has no separate hosted account to sign in to; any connected service retains its own sign-in and terms.

## Older Electron releases

Earlier desktop releases included a Hub workspace and service-specific bridge setup. That interface and its Incognito Mode instructions do not apply to the native app. Consult the notes for your exact build in [Releases](/releases/).
