# Hub

Hub lets you read and send messages and mail from Polymux. `polymux-hub` (`plugins/hub/internal/hub`) runs Tuwunel on loopback with federation off, one account, and the mautrix bridges as children. The app only talks to `polymux-hub`, never to Matrix. Mail uses IMAP and SMTP directly, always over TLS. Contact links and broadcast lists stay on the server.

Routes, relative to `/plugins/hub/api`: `status`, `settings`, `chats?since=`, `chats/{id}/messages`, `chats/{id}/members`, `chats/{id}/name`, `stickers`, `media`, `platforms/{id}/logins`, `people`, `chats` (POST starts one), `links`, `broadcasts`, `mail`, `mail/oauth/{provider}`.

## Chats

- **Caching**: a chat's latest page is saved as it loads, and opens from there before refreshing. `AppSession.prepareHub` readies recent chats, faces and images at launch. Reading a page never marks it read; only opening a chat does.
- **Who sent what**: a linked account can write from a bridge ghost as well as the Hub user. WhatsApp's newer chats use a hidden ID that only the bridge's device record holds, so Hub reads it from `bridge.db`, read-only. A direct chat names the other side after the chat, whichever ID they wrote from.
- **Spaces**: a chat lists every Space it's in, read from either side of the link. `HubSpaces` folds a Space into one row with its chats behind it. A bridge's account-wide filtering Space is never listed, so its chats stay at the top level.
- **Search**: the app searches one chat at a time, paging back to its start. `HubSearch` splits results into messages, media, files and links.
- **Mentions**: a draft holds plain text. `HubMentionText` rebuilds the mentions from it at send, and each travels as a handle plus a Matrix ID, which is what lets a bridge make a native mention.
- **Rename**: this writes the room's name, and the bridge carries it to networks that allow it. WeChat groups rename through WeChat itself: see below.
- **Stickers**: the picker offers the last 40 seen on the sync stream. Sending one writes `m.sticker`.
- **Reactions**: each reaction lists who made it, shown on hover or long press, and a tap changes it straight away.
- **Read marks**: the sidecar keeps each person's latest `m.read` receipt from the sync stream and puts their avatar on the newest message they've reached, not on their own. Networks whose bridges send no receipts show none.
- **Voice notes**: recorded as AAC and sent as audio with the voice flag and duration. Bridges for networks that need another format convert it, which needs `ffmpeg` beside them; Hub's package doesn't carry it.
- **Incognito**: the sidecar keeps this, so it covers every device. While it's on, opening a chat sends no read receipt and leaves its unread count. Mail still marks messages read.

## Mail credentials

Passwords, refresh tokens and OAuth clients are sealed with AES-256-GCM before they reach `mail.json`, each bound to its record. The key is in the Keychain on a Mac and in an owner-only `credentials.key` elsewhere, as the server's vault does it. A locked Keychain stops mail until it's unlocked; Hub never replaces a key it can't read.

Gmail and Outlook sign in with OAuth. The sidecar makes the sign-in, keeps the PKCE verifier and redeems the code, then uses XOAUTH2 for IMAP and SMTP. The app only opens the browser and catches the redirect on `http://127.0.0.1:47666/mail/callback`, since the server may be another machine. Register that address with the provider, and enter the client in Accounts the first time.

## iMessage and WeChat

`mautrix-imessage` reads the Messages database on the Mac running Hub, so that machine needs Full Disk Access. It has no login API and an older config (`imessage.go`), and it's pinned to a commit because upstream tags no releases.

WeChat has no mautrix bridge. `wechat.go` is an appservice inside `polymux-hub` that carries messages between the homeserver and `wechat-bridge`, a relay installed on the Mac that drives the desktop app. It needs that relay, and nothing runs until someone connects WeChat.

`wechat-use` 1.19 targets a dedicated WeChat 4.1.9 copy with its own login, named in `~/.wx-rs/managed-wechat.json`, and Hub follows that copy. One account can't stay signed in on the copy and the installed app together. WeChat gives no delivery receipt, so `submitted_unconfirmed` counts as sent and nothing resends. A send is refused while the copy is the frontmost app.

- **Received**: text and stickers. A sticker is downloaded from WeChat's CDN and checked against its digest, and other media shows by name.
- **Sent**: text, plus pictures through `wechat-use send --image`. A sticker arrives as a picture of itself, because a real one needs WeChat's private sender.
- **Group rename**: `wechat_native.go` attaches LLDB for one call and runs WeChat's own `ModifyChatRoomName` (`wechat_rename.py`). The address only works on one build, so `wechatBuilds` lists each by image UUID, and the button stays hidden on any other. To add a build, find the function that loads the `ModifyChatRoomNa` literal, confirm its callers pass `(service, recipient, name)`, use the address its callers branch to, then test it live. WeChat must be open and allow debugging. The name is read back when WeChat's database can be read. This also runs for an adopted bridge, where Hub keeps the new name itself in `names.json`, because the bridge owns the room.
- **Real stickers**: not sent. WeChat's send path takes C++ objects and callbacks that its UI builds, so unlike rename there's no plain function to call.

`status.textOnly` lists platforms that only carry text, so the app offers nothing else there. An adopted WeChat bridge drops anything but text without saying so, so it's one of them unless the Mac has the relay's CLI. With the CLI, Hub sends a picture or sticker to WeChat itself, then writes it to the room, so the thread never shows what didn't go. WeChat takes pictures only, never voice notes or other files.

Hub can use a homeserver and bridges that are already running. With a `homeserver.json` in its data folder, naming `url`, `userID`, `tokenFile` and `bridges`, it starts no processes of its own, keeps accounts linked there, and doesn't join rooms itself. Its package then needs only `polymux-hub`, and Hub's own iMessage and WeChat bridges stay off. The exceptions are for WeChat: a group rename someone asks for briefly runs LLDB, and a picture goes through `wechat-use`.

Tuwunel publishes Linux builds only, so `plugins/hub/prepare.sh <target> <folder>` fetches or builds Tuwunel and the bridges for one target, and the release workflow runs it before packaging. To build a package locally: `plugins/hub/prepare.sh darwin-arm64 .build/hub-binaries`, then `POLYMUX_PLUGIN_BINARIES=.build/hub-binaries scripts/package.sh hub`.
