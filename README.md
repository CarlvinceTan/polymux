<p align="center">
  <img src="apps/Polymux/Assets/AppIcon.png" alt="Polymux" width="128">
</p>

<h1 align="center">Polymux</h1>

<p align="center">
  <strong>Native conversations with your AI agents on macOS and iOS.</strong>
</p>

Polymux is a SwiftUI app for individual and group conversations with agents
running on your own Go server. You create each agent yourself and give it its own
profile, instructions, skills and MCP connections.

- **Conversations:** chats, groups, attachments, message search and voice.
- **Agents:** custom profiles and avatars; nothing is prebuilt.
- **Memory:** agent-and-conversation-scoped memory stored in SQLite on the server.
- **Server:** runs the agents, stores history, connects tools, and runs schedules.

The app is a client only. Install the server CLI on a Mac or Linux machine, pair
the app with it, and start talking.

## Repository

- `apps/Polymux`: the SwiftUI app (macOS 27 and iOS 27).
- `apps/server`: the Go agent server and CLI.
- `apps/site`: the website at [polymux.com](https://polymux.com).
- `packages`: Swift libraries used by the app.
- `docs`: specs and roadmap.

## Requirements

**App**

- macOS 27 or iOS 27.
- Xcode with the Swift 6 toolchain, to build from source.
- Dictation uses on-device speech recognition. Cleaning up dictated text needs Apple Intelligence.

**Server**

- macOS or Linux, with Go 1.27 and a C compiler (SQLite and sqlite-vec build through cgo).
- Build and test with the `sqlite_fts5` tag (the Makefile sets it).
- `ffmpeg` and `ffprobe` for thumbnails and video search.
- `git` for coding agents.

**Optional**

- `llama-server` (llama.cpp 0.7 or newer) with the EmbeddingGemma 2 GGUF for semantic search. Set `POLYMUX_LLAMA` and `POLYMUX_EMBED_MODEL`. Without it, search is keyword-only.
- The EmbeddingGemma 2 multimodal projector (`POLYMUX_EMBED_MMPROJ`) to search images, audio and video by content.
- `media-text` (`make tools`, macOS only) to extract text from documents and media (`POLYMUX_EXTRACTOR`).
- Tailscale to reach the server over your tailnet.
- `dumbpipe` for pairing from other networks. It is downloaded on first use if it isn't on your PATH.

## Development

```sh
# Server
cd apps/server
make test
make run
```

Open `apps/Polymux/Polymux.xcodeproj` in Xcode to build the app.

## Sponsors

<p align="left">
  <img src="docs/assets/flarehq-sponsor.svg" alt="FlareHQ" width="240">
</p>
