---
title: Installation
slug: installation
description: Choose between native macOS and iOS builds and older Electron desktop releases.
section: Getting started
sectionOrder: 1
order: 3
published: true
---

# Installation

Polymux has two distinct app generations. These docs cover the native SwiftUI app and Go server in the current repository, not the earlier Electron workspace.

## Check the build you are installing

The website's download buttons currently select packages from the **Electron release line**. A newer native build may have a separate release tag. Inspect the assets and notes on [GitHub Releases](https://github.com/CarlvinceTan/polymux/releases) rather than assuming the main download button installs the interface shown here.

| App generation | Targets | Interface |
| --- | --- | --- |
| Native source | macOS 27 and iOS 27 | Agent conversations, groups, and native settings |
| Earlier Electron releases | macOS, Windows, and Linux packages | Tabbed workspace with Hub, Drive, and Browser |

The native Xcode project's version is 0.4.0. That source version is not a promise that a signed installer or iOS distribution is available for every target. Historical release notes describe their own build, not current native feature coverage.

## Connect to a server

The native app needs a running Go agent server. After opening the app, use **Settings → Agent server** to enter its URL. The server must be reachable, authenticated, and configured with model access for agent replies.

For local macOS development with the default server data directory, the app discovers the server's owner-only local credential. The default backend address is `http://127.0.0.1:7337`. Backend routes require Bearer authentication, including history and streams.

Changing the URL alone does not configure remote authentication. The native Settings panel has no general remote chat-token entry field. Remote and iOS deployment require an authenticated client setup beyond this local quickstart.

Use a trusted network and the authentication setup appropriate to your deployment. Do not expose an unauthenticated agent server to the public internet. See [Quickstart](/docs/quickstart/).

## macOS permissions

Notifications, microphone input, and speech recognition have separate permission requirements. Enable only the features you intend to use. The native app does not require the older Hub's iMessage database access to show agent conversations.

A denied permission can be changed in **System Settings → Privacy & Security**. Restart Polymux after enabling access if macOS does not apply it immediately.

## Updates

Check the release notes and distribution channel for the build you installed. The native Settings panel does not have the older **General → Check for updates** screen. Do not treat an Electron updater as a native-app upgrade path.

## Build from source

The [public repository](https://github.com/CarlvinceTan/polymux) contains the app under `apps/Polymux`, the server under `apps/server`, and shared Swift packages under `packages`.

Native builds need Xcode 27 and the matching SDKs. The server module requires Go 1.27.1. Configure [model access](/docs/models-and-agents/) first, then start a local development server from the repository root:

```sh
cd apps/server
go run -tags sqlite_fts5 ./cmd/server -listen 127.0.0.1:7337 -device-listen 127.0.0.1:7339 -no-mdns
```

This example keeps both listeners on loopback. The server reads its process environment; it does not automatically load a `.env` file. Its optional host controller is a separate unauthenticated interface and must stay on loopback.

Build the `Polymux` scheme in `apps/Polymux/Polymux.xcodeproj`. Device installation and signed distribution require suitable Apple signing configuration.
