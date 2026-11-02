---
title: Install
slug: installation
description: Install the server and client on one Mac, or pair clients on other Macs.
section: Guides
sectionOrder: 1
order: 2
published: true
---

# Install

Native downloads are published on the [Releases page](/releases/). Until a release is available, installation builds from source. Older Electron downloads are a different app.

## Requirements

- **Client:** macOS 13 or later.
- **Server:** Go 1.27.3, Git, and a C compiler. Xcode’s command-line tools provide the compiler on macOS.
- **Building from source:** Node.js 22, Rust stable, and Xcode’s command-line tools.

## Get the source

Clone the native development branch, `dev`, on each Mac you are installing on:

```sh
git clone --branch dev https://github.com/CarlvinceTan/polymux.git
cd polymux
```

Run the following commands from that folder. Local client builds are ad-hoc signed; release builds are Developer ID signed and notarized.

## One Mac

Install both parts together:

```sh
./scripts/install.sh both
```

This installs the app in Applications, starts the server at login, and connects the app to it.

## Server

For a Mac that only needs to run your agents:

```sh
./scripts/install.sh server
```

Keep this Mac awake and connected when you want your agents available.

## Client

For another Mac that only needs the chat app:

```sh
./scripts/install.sh app
```

Open Polymux, then pair it with your server.

## Pair another Mac

1. On an already connected client, open **Settings → Devices → Add device** and copy its pairing link.
2. On the new client, open the same screen, paste the link into **Join with a link**, and press **Return**.

For a server-only installation, generate a link on the server Mac instead. Replace `SERVER_ADDRESS` with the address printed by the installer:

```sh
~/.local/bin/polymux pair --url "SERVER_ADDRESS"
```

Pairing links contain credentials. Share them privately, and use the complete link rather than just the server address.

## If something is not working

- **Cannot connect:** check that the server is running and try a fresh pairing link. Other devices connect through an encrypted tunnel, or over Tailscale when the server runs with `-tailscale`. If the tunnel is turned off on the server, use Tailscale.
- **Chats open but replies fail:** check your provider and **Default model** in **Settings → Models**.

To update, pull the source version you want and rerun the relevant install command. Release builds check for updates automatically; use **Polymux → Check for Updates…** to check now. Next, [choose a model](/docs/models-and-agents/) and [start a conversation](/docs/).
