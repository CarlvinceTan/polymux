---
title: Troubleshooting
slug: troubleshooting
description: Diagnose native app connections, missing replies, upload failures, and differences from older screenshots.
section: Help
sectionOrder: 4
order: 2
published: true
---

# Troubleshooting

Start by identifying whether the problem belongs to the app, the server, the model provider, or a tool. Changing unrelated permissions or provider keys can hide the original failure.

## The app shows “Connect to Polymux”

Use **Connection settings** and check the **Agent server** address. Include the scheme and port, confirm the server is running, and make sure this device can reach its network. On a phone, `localhost` does not refer to your development Mac.

Cached conversations are not proof of a live connection. Watch for connection errors and retry after restoring access. If the server requires authentication, check the deployment's authentication setup rather than pasting a secret into a chat or URL.

## Chats load but the agent does not reply

Inspect the conversation's activity and error state. A run may be waiting for your answer to an approval card. Otherwise, check the server's model configuration and logs for authentication errors, rate limits, unavailable models, or tool failures.

The native app does not have **Settings → Providers** or **Models**. Those older instructions are not a route to configuring the Go server.

## An attachment did not upload

Check the failed attachment in the composer. Retry after restoring the connection, or remove it and choose the file again. A filename in the composer does not mean the upload succeeded. A local path typed into a message is not an upload to a remote server.

## The interface does not match a screenshot

Check your app generation. Earlier Electron releases show a tabbed Hub, Drive, Browser, and Tasks workspace. Current native builds show agent conversations and groups. Read [Installation](/docs/installation/) before treating a missing older settings page as a bug.

## Voice is unavailable

Check Voice in Settings and its error state. Change only the microphone or speech permission needed for the feature, using the operating system's settings; a server or model problem is not fixed by granting broader device access.

## Still stuck

Check the [GitHub issues](https://github.com/CarlvinceTan/polymux/issues) for a matching report. When opening a new issue, include the Polymux version, operating system, the affected view or provider, and the exact error text. Remove keys, tokens, personal messages, and other private data first.
