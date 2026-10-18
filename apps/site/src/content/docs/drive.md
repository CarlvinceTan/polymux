---
title: Drive
slug: drive
description: Attach files in native conversations and understand where server-side file access happens.
section: Using Polymux
sectionOrder: 2
order: 3
published: true
---

# Drive

The native app sends files through conversation attachments. It does not currently include the older **Drive** file-browser workspace or **Settings → Drive** storage manager.

## Attach a file

Open a conversation and choose **Add attachment** (+) beside the message field. On macOS, you can also drag local files into the selected chat. Check the attachment preview and upload state before sending.

If an upload fails, retry it or remove it from the composer. Selecting a file does not prove that it reached the server.

## App files and server files

The agent executes on the connected server. Its filesystem is not automatically your device's filesystem. Attaching a document sends it to that server; typing a path from your phone or another Mac does not make the file available there.

Cloud storage access requires tools configured on the server. Polymux has no separate cloud-storage account: the service connection belongs to you. See [Skills and MCP](/docs/skills-and-mcp/).

Review the server and model provider you are sending to before attaching sensitive files.

## Older Electron releases

The earlier Drive UI supported storage sources and a tabbed file browser. Its storage-priority and per-chat folder instructions are not native-app controls. Check [Releases](/releases/) for your build rather than following an older screenshot.
