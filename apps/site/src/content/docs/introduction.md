---
title: Introduction
slug: introduction
description: Meet the native Polymux app for conversations with your agents on macOS and iOS.
section: Getting started
sectionOrder: 1
order: 1
published: true
---

# Introduction

Polymux is an open-source native app for talking to AI agents. The SwiftUI app connects to a Go agent server, which runs the agents and stores their conversations.

These guides describe the **native app in the current source tree**. Older Electron releases have a different interface and feature set. Check [Installation](/docs/installation/) before choosing a build; a download from the older release line will not match these instructions.

## In the native app

- Find conversations in the sidebar, with search, unread and active filters, and sorting.
- Talk to an individual agent or create a group with several agents.
- Give each agent a name, role, role description, and avatar.
- Send attachments, reply to messages, and search within a conversation.
- Review agent activity and answer approval requests in the chat.
- Configure appearance, the server connection, voice, and notifications in Settings.

The app does not currently expose the older tabbed Hub, Drive, Browser, or Tasks workspace. Those older screenshots are not a guide to the native interface.

## App and server

The app keeps a local conversation cache; agent execution, tool access, and authoritative history belong to the connected server. A remote server is not the same as running everything on your phone or Mac. A URL alone does not configure remote authentication; see [Installation](/docs/installation/) for the current client-setup limitations.

The current native targets require **macOS 27 or iOS 27**. Windows and Linux packages in the older Electron release line are separate from these native targets.

Start with [Quickstart](/docs/quickstart/), then [Conversations](/docs/workspace/).
