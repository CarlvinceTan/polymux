<p align="center">
  <img src="apps/tauri/src-tauri/icons/128x128@2x.png" alt="Polymux" width="128">
</p>

<h1 align="center">Polymux</h1>

<p align="center">
  <strong>Native conversations with your AI agents on macOS.</strong>
</p>

Polymux is an extensible personal assistant with a native Tauri app and your
own Go server. You create each agent yourself and give it its own profile,
instructions, skills and MCP connections. Its backend capabilities are being
separated into Go packages that other projects can use without running Polymux.

- **Conversations:** chats, groups, attachments, message search and voice.
- **Agents:** custom profiles and avatars; nothing is prebuilt.
- **Memory:** agent-and-conversation-scoped memory stored in SQLite on the server.
- **Server:** runs the agents, stores history, connects tools, and runs schedules.

The app is a client only. Install the server CLI on a Mac or Linux machine, pair
the app with it, and start talking.

## Repository

- `apps/tauri`: the Tauri app (Svelte UI, Rust backend, macOS).
- `apps/server`: the Go agent server and CLI.
- `apps/site`: the website at [polymux.com](https://polymux.com).
- `packages`: reusable Go backend modules.
- `docs`: [developer guides](docs/README.md).

## Backend packages

The standalone Go modules below can be imported by another backend. They do not
import the Polymux server or its private API types; Polymux uses the same public
APIs as other consumers.

| Go package | Purpose |
| --- | --- |
| [`runtime`](packages/runtime/) | Persistent agent sessions, streaming, tools, skills, MCP and model providers, built on PiG. |
| [`memory`](packages/memory/) | Persistent documents isolated by owner and scope, with revision-safe updates and deletion. |
| [`vault`](packages/vault/) | Encrypted SQLite vault, credential imports, TOTP and origin-bound credential use. |
| [`search`](packages/search/) | Hybrid ranking, pluggable embeddings, FTS5 query escaping and media extraction. |

See [backend package usage and boundaries](docs/packages.md) for imports and
executable examples. The server supplies Polymux's API, policy and product tools
around these packages.

## Requirements

**App**

- macOS 13 or later.
- Node.js 22, Rust stable, and Xcode’s command-line tools, to build from source.
- Dictation uses macOS speech recognition. Cleaning up dictated text needs Apple Intelligence.

**Server**

- macOS or Linux, with Go 1.27 and a C compiler (SQLite and sqlite-vec build through cgo).
- Build and test with the `sqlite_fts5` tag (the Makefile sets it).
- `ffmpeg` and `ffprobe` for thumbnails and video search.
- `git` for coding agents.

**Optional**

- `llama-server` (llama.cpp 0.7 or newer) with the EmbeddingGemma 2 GGUF for semantic search. Set `POLYMUX_LLAMA` and `POLYMUX_EMBED_MODEL`. Without it, search is keyword-only.
- The EmbeddingGemma 2 multimodal projector (`POLYMUX_EMBED_MMPROJ`) to search images, audio and video by content.
- Tailscale to reach the server over your tailnet.
- `dumbpipe` for pairing from other networks. It is downloaded on first use if it isn't on your PATH.

## Development

```sh
# Server
cd apps/server
make test
make run
```

Run `scripts/run.sh` for an isolated app and server, or `scripts/check.sh` to compile without launching.

## Sponsors

<p align="left">
  <img src="docs/assets/flarehq-sponsor.svg" alt="FlareHQ" width="240">
</p>
