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

- **Client:** macOS 27 and Xcode 27.
- **Server:** Go 1.27.4, Git, and a C compiler. Xcode’s command-line tools provide the compiler on macOS.

## The Mac app

The Mac app carries its own copy of the server. On a Mac with no Polymux yet, it starts that server at login and connects to it, with nothing else to install. Turn it off under **Settings › Network › This Mac**.

If you join a network from a new install, the app retires its own server while it's still empty. If this Mac's server has chats on it, the app asks whether to keep the two networks separate or merge them.

To change which Mac is the server, join its network from a new install, open the network in **Settings › Network**, and choose **Move server**. Chats, bots, the vault and your devices go with it. Logins for tools on the old Mac, and files outside Polymux's data folder, stay behind.

To combine two networks, open the one you're giving up in **Settings › Network** and choose **Merge network**. Its chats, bots and vault come to the other network, and its server stops. Bots start a new working context and keep their chats and memory, schedules arrive paused, and plugins stay behind. The merge screen lists which devices gain or lose access first.

## Get the source

Clone the native development branch, `dev`, on each Mac you are installing on:

```sh
git clone --branch dev https://github.com/CarlvinceTan/polymux.git
cd polymux
```

Run the following commands from that folder. Client commands use a local development signature.

## One Mac

Install both parts together:

```sh
POLYMUX_SIGN_IDENTITY="-" POLYMUX_TEAM_ID="" \
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
POLYMUX_SIGN_IDENTITY="-" POLYMUX_TEAM_ID="" \
  ./scripts/install.sh app
```

Open Polymux, then pair it with your server.

## Pair another Mac

1. On an already connected client, open **Settings → Network**, choose the network, press **+** beside Devices, and copy its pairing link.
2. On the new client, open **Settings → Network**, press **+** beside Networks, paste the link into **Join with a link**, and press **Return**.

For a server-only installation, generate a link on the server Mac instead. Replace `SERVER_ADDRESS` with the address printed by the installer:

```sh
~/.local/bin/polymux pair --url "SERVER_ADDRESS"
```

Pairing links contain credentials. Share them privately, and use the complete link rather than just the server address.

## If something is not working

- **Cannot connect:** check that the server is running and try a fresh pairing link. Other devices connect through an encrypted tunnel, or through a transport plugin such as Tailscale. If the tunnel is turned off on the server, install a transport plugin.
- **Chats open but replies fail:** check your provider and **Default model** in **Settings → Models**.

To update, pull the source version you want and rerun the relevant install command. Release builds check for updates automatically; use **Polymux → Check for Updates…** to check now. Next, [choose a model](/docs/models-and-agents/) and [start a conversation](/docs/).
