---
title: Quickstart
slug: quickstart
description: Connect the native app to an agent server and start an individual or group conversation.
section: Getting started
sectionOrder: 1
order: 2
published: true
---

# Quickstart

You need a native Polymux build and a running, authenticated agent server. This quickstart uses the app and server on the same Mac with the default data directory, so local credentials can be discovered automatically. See [Installation](/docs/installation/) for source builds, older Electron downloads, and remote-authentication limitations.

## 1. Connect the app

Open **Settings** using the gear button. If the app cannot connect, use **Connection settings** on the connection screen.

Enter your server URL under **Agent server**, including its scheme and port. Use an address reachable from this device. `localhost` on an iPhone refers to the phone, not your Mac.

For the default local server, use `http://127.0.0.1:7337`. Do not paste a token into the URL.

The server address saves after a short pause or when you leave Settings. If connection fails, check the address, network access, and server state before changing model configuration.

## 2. Choose an agent

Select a conversation in the sidebar, or press **New chat** (+). Choose an agent and press **Create**. The plus button inside New Chat creates a new agent; its profile can be edited afterwards.

Choose several agents to create a group. You can give the group a name before creating it.

## 3. Start a conversation

Send a small request to check the connection and the server's model setup:

> Help me plan three priorities for today.

Follow the response and any activity or approval cards in the conversation. Available tools depend on the server's configuration; connecting the app does not configure every integration.

## 4. Make the agent yours

Click the agent's avatar or name at the top of the conversation to open **Edit profile**. Change its name, role, or role description, then press **Save**. Click the avatar in the profile to customise its appearance.

Next, read [Conversations](/docs/workspace/) for search, attachments, and groups, or [Models and agents](/docs/models-and-agents/) for the boundary between app settings and server configuration.
