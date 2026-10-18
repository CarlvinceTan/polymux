---
title: Privacy and permissions
slug: privacy-and-permissions
description: Understand what stays on the app device, what reaches the agent server, and which permissions are optional.
section: Help
sectionOrder: 4
order: 1
published: true
---

# Privacy and permissions

Polymux has no separate hosted account to sign in to. The native app connects to an agent server you configure, and that server may use external models and services. Review all of those destinations before sharing sensitive work.

## App device and agent server

The app caches conversations on its device. The connected server stores authoritative conversation history and runs agents and tools. Messages and attachments travel to that server. If it runs on another computer, that data is not confined to the app device.

Hosted models and connected tools can receive task content according to their configuration and terms. A local app or server does not make a hosted model local. MCP connections and third-party service accounts belong to you, not a Polymux cloud account.

Read the full [privacy policy](/privacy-policy/) for the current product and browser-extension details.

## Credentials

Server-managed provider credentials use the server's credential storage. A remote server's credentials do not live in the app's Settings panel. Avoid placing secrets in prompts, screenshots, skill instructions, source files, or public repositories.

## Browser access

The older Electron extension's tab-inventory and tab-assignment model is not a native-app settings feature. Do not infer that your current browser tabs are shared because an older guide described them.

## Operating-system access

Microphone input, speech recognition, and notifications have different purposes. Enable only what you plan to use, and let the operating system handle its approval prompts. Text conversations do not require every optional permission.

## Consequential actions

Review approval and confirmation cards in the conversation before answering them. Installing a tool does not authorise every send, publication, payment, deletion, or account action it can perform. Tool policies depend on the server and integration; do not assume every consequential action has an automatic confirmation guard.
