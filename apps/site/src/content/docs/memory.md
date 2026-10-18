---
title: Memory
slug: memory
description: Understand agent memory stored on the server and how it differs from conversation history.
section: Customising Polymux
sectionOrder: 3
order: 3
published: true
---

# Memory

The current server gives agents explicit memory tools for saving, recalling, and forgetting useful notes. Memory is scoped to an **agent and conversation**, not a single shared notebook for every agent or chat.

## What is stored

Memory is stored in SQLite under `memory/<agentID>/memory.sqlite` in the server data directory. It is not the Markdown vault described by older Electron documentation.

The server retrieves relevant notes within a bounded context budget. Delegated work does not automatically receive the parent conversation's private memory.

## Ask the agent

You can ask an agent to save a useful preference or recall what it has stored for this conversation. Check its result rather than assuming a request to “remember” always succeeded.

Forgetting an active memory does **not** delete the original chat message, database backups, or every trace of the content. Manage those separately if you need data removal.

## Where “local” means

Memory belongs to the server. If the server runs on another computer, memory is stored there, not only on your phone or Mac. Model providers may receive recalled content as part of an agent request.

There is no native **Settings → Memory** or **ComputerHistory** panel at present. Do not treat older screen-history retention or capture instructions as a description of the native app. See [Privacy and permissions](/docs/privacy-and-permissions/).
