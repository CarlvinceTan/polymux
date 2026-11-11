# Backend packages

Polymux's reusable building blocks are Go backend packages: agent runtime, memory, vault and search. Other projects can import these capabilities into their own backends; Polymux's server uses the same packages. Product-specific routes, policy, agent tools, private cards and chat storage stay in the server.

## Go modules

Each directory has its own `go.mod` and can be imported without `apps/server`:

```go
import (
    "github.com/CarlvinceTan/polymux/packages/runtime"
    "github.com/CarlvinceTan/polymux/packages/memory"
    "github.com/CarlvinceTan/polymux/packages/search"
    "github.com/CarlvinceTan/polymux/packages/vault"
)
```

All four use Go 1.27.4. The repository's `go.work` connects them to the server; the server also has relative replacements so it builds with `GOWORK=off` from this checkout. No package release is published by this extraction. After the source is pushed, consumers can pin a commit or a module-specific release tag.

For a local consumer beside a checkout named `polymux`, use a local replacement:

```sh
go mod edit -replace=github.com/CarlvinceTan/polymux/packages/vault=../polymux/packages/vault
go get github.com/CarlvinceTan/polymux/packages/vault@v0.0.0
```

Use the same pattern for the other modules. Here `v0.0.0` is a local placeholder, not a published release. All modules use the repository's [Apache-2.0 license](../LICENSE).

### Agent runtime

[`runtime`](../packages/runtime/) runs persistent sessions on PiG: streamed text and tool events, submission replay, model-provider routing and retries, skills, MCP and optional detached Git worktrees. It owns neutral input and usage types rather than importing the app's API models.

PiG supplies codemode execution, nested-call validation, the native SQLite adapter and the decoded-history cache. The runtime supplies host hooks and result mapping around those APIs, plus conversation cleanup and provider policy.

Start with [`ExampleNewDurable`](../packages/runtime/example_test.go). Open storage with `NewDurableStorage`, construct the runtime, and call `Open` and `Send`. Retain `Session.Path()` to reopen a conversation. Close sessions and the runtime before closing storage. The example uses a local model endpoint and runs without paid credentials or a Polymux server.

Hosts supply credentials through provider configuration and may add PiG tools through `Definition.ExtraTools`. `CorePrompt` is the package-level core: identity, handling of quoted and external data, and the tool contract, with no Polymux product policy. `Client.BuildPrompt` supplies host policy above it; `Client.InstallSkills` supplies bundled skill files. The default prompt and skill loader do not install Polymux's product guides. Hosts authorize tools, MCP connections, attachment paths and shell access; the runtime is not an execution sandbox.

Polymux's [`internal/agentruntime`](../apps/server/internal/agentruntime/) adapts its saved agent definitions, messages and usage to the library and supplies its policy and bundled skills. Existing conversation identifiers, `durable.db` records and provider namespaces retain their format.

### Memory

[`memory`](../packages/memory/) stores documents separately by owner and conversation scope. `New` opens stores lazily under a caller-selected directory. `Save` returns a revision; updates and `Forget` require that revision and return `ErrConflict` for stale writes. `Delete` removes an owner's database; `Close` releases open stores.

See [`ExampleNew`](../packages/memory/example_test.go) for saving, reopening and detecting a conflicting edit. Hosts authenticate access to owner/scope identifiers and decide which documents enter an agent's context. Polymux keeps its memory tools, scope selection and retrieval policy in the server. The existing per-owner `memory.sqlite` format and document identifiers are unchanged.

### Vault

[`vault`](../packages/vault/) owns its domain types, the KeePass (KDBX 4) file it stores them in, revision checks, merging, the optional sync location, imports, TOTP and origin-bound credential operations. It is pure Go and needs no database. Future format changes belong to this module, not the HTTP adapter.

Start with the executable [`ExampleOpen`](../packages/vault/example_test.go):

1. Choose a folder for the embedded vault (`vault.kdbx`) and its per-host settings (`config.json`).
2. Implement `vault.Keys` with secure unattended storage for the vault password. The example's memory-only keys are for demonstrations, not persistent use.
3. Call `vault.Open`; the vault is created on first use with a random password.
4. Use `vault.Item`, `Save`, `List` and the credential-use operations. `SetSync` adds a sync location.

**Authorization belongs to the host.** Data methods are trusted APIs: `Get`, `Save`, `Values` and `File` do not authenticate devices themselves. Authenticate devices before management operations; expose blank replacement fields instead of saved values. Only expose value-free metadata, references and receipts to models. This is not a sandbox against an agent with unrestricted host access.

Polymux supplies the macOS Keychain/Linux key-file implementation in [`vault_keys.go`](../apps/server/internal/server/vault_keys.go), maps domain types to the generated API contract in [`vault_types.go`](../apps/server/internal/server/vault_types.go), and keeps its routes and tools in `internal/server`. Chromium filling/passkeys require an explicitly configured loopback debugging endpoint; the library does not launch or configure a browser. See [Vault](vault.md) for the security boundary and storage format.

### Search

[`search`](../packages/search/) owns reciprocal-rank fusion, deterministic tie-breaking, lexical fallback, optional embedding requests and normalization, FTS5 query escaping and media extraction. It has no dependency on Polymux or SQLite and needs no paid service. Keyword search does not require an embedding backend.

Implement `search.Source` for your corpus: `Lexical` returns relevance-ordered candidates for a raw query; `Vector` returns ordered candidates for the supplied embedding/model key. Your adapter owns indexing, persistence, filtering and access scope. `search.Engine.Search` combines both lists, returns at most 40 hits and reports whether semantic retrieval succeeded. A nil embedder, an embedding error or a vector-query error leaves lexical retrieval available; lexical errors are returned. `search.Start` builds that optional backend from a local `llama-server`, a remote embeddings URL, and a `media-text` extractor. With none of those set, it returns a zero `Backend` and keyword search stands on its own.

See the executable [`ExampleEngine_Search`](../packages/search/example_test.go) for a minimal standalone corpus. Use `search.Query` only when your lexical backend needs escaped FTS5 syntax. `HTTPEmbedder` supports EmbeddingGemma-compatible endpoints; supply positive dimensions and an HTTP client with a timeout. Optional video embedding needs `ffmpeg`/`ffprobe`; non-text extraction can use `SubprocessExtractor` with the `media-text` executable.

Polymux's [`internal/search`](../apps/server/internal/search/) is the product adapter: it indexes chats and attachments in the existing database, scopes SQL queries, and maps engine hits to messages, files, bots and conversations. The public engine does not import those models or require the Polymux database schema.

## Verification

```sh
scripts/test.sh packages -race
scripts/test.sh server -race
python3 -B scripts/packages_test.py
```

Package tests run with `GOWORK=off`. The consumer check creates a temporary, unrelated Go module and runs all four public examples without importing the server. Server tests cover the product adapters; native E2E flows exercise the server through the app.
