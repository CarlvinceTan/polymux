# Architecture

Requirements that every design and dependency choice must satisfy.

## Server

- There is exactly one server. It is the single source of truth for agents, conversations, memory, schedules and tool connections.
- The server is a standalone Go binary with a CLI. It is installed and run independently of any GUI.
- All state lives on the server (SQLite). Clients hold no authoritative data and may cache only.
- Pairing and administration happen through the CLI.

## Clients

- Every GUI (macOS, iOS, web, or any future one) is a client only.
- No GUI bundles, embeds, launches or manages the server.
- Clients talk to the server through one documented API (`contracts`). No client gets a private or privileged path.
- Any client can be removed or replaced without affecting the server or other clients.
- Clients are interchangeable: the same server works with any number of them at once.

## Cost and accounts

- Free to run. Nothing in the core path requires a paid service, subscription or licence.
- No required third-party accounts. Core features must not depend on signing up for any service (cloud, auth, analytics, push relay, hosting, etc.).
- No Polymux-operated account or cloud service is required. The user's own server is the only backend.
- Optional integrations (e.g. a model provider the user chooses to connect) must be opt-in, replaceable, and never block core use.

## Ownership and privacy

- The user owns their data. It stays on their machine unless they explicitly send it elsewhere.
- No telemetry or analytics without explicit opt-in.
- Data is stored in open, documented formats and can be exported.

## Dependencies

- Prefer the standard library and platform frameworks. Add a dependency only when it clearly earns its place.
- Dependencies must be open source and usable without an account or key.
- The server must run with no network access to Polymux-controlled services.

## Platforms

- The server runs on macOS and Linux.
- Clients are native to their platform.
