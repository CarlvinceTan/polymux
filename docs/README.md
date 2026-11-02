# Developer guide

Start here for a quick map of Polymux and its documentation. These docs explain how the codebase fits together; follow the links when you need more detail.

## The big picture

Polymux is a native desktop client for AI agents running on your own Go server. The server runs agents and owns the data; the Tauri app connects to it through a shared API. The website is a separate project, not the app client.

Read [Architecture](architecture.md) first for the boundaries every change must respect.

## Repository map

Paths below are relative to the repository root.

| Directory | What lives here |
| --- | --- |
| [`apps/tauri/`](../apps/tauri/) | Tauri client, Svelte UI, native integrations, and app-specific tests. Start with `src/lib/shell/` for screens. |
| [`apps/server/`](../apps/server/) | Go server and CLI. `cmd/polymux/` is the entry point; `internal/` contains the implementation; `evals/` checks agent behaviour. |
| [`apps/site/`](../apps/site/) | Public website and user-facing documentation. |
| [`packages/`](../packages/) | Reusable Go backend modules (`runtime`, `memory`, `vault`, `search`) and the generated protocol. See [Backend packages](packages.md) for usage and boundaries. |
| [`contracts/`](../contracts/) | Shared API schema and code generation for the server and clients. |
| [`scripts/`](../scripts/) | Run, check, test, install, deploy, and release commands. |
| [`e2e/`](../e2e/) | Tauri UI flows and fixtures. |
| [`.agents/skills/`](../.agents/skills/) | Task-specific guidance and helper tools for coding agents. See [Project skills](skills.md). |
| [`docs/`](./) | Developer overviews and focused references, indexed below. |
| [`.github/workflows/`](../.github/workflows/) | CI checks and release workflows. |

Generated output is separate from source: `.build/` holds caches and local build staging, root `dist/` holds app/server distribution artifacts, and `apps/site/dist/` holds the built website.

## Documentation index

| Document | Read it for |
| --- | --- |
| [Architecture](architecture.md) | Server/client responsibilities, ownership, privacy, and dependency rules. |
| [Backend packages](packages.md) | Reusable Go backend modules, import examples and product adapters. |
| [Project skills](skills.md) | Available coding-agent skills, what they do, and when to use them. |
| [Development and delivery](development.md) | Local checks, isolated runs, CI, and release delivery. |
| [Vault](vault.md) | Credential handling, private cards, secret references, and security boundaries. |
| [References](references.md) | Other products that inform Polymux's behaviour. |

## Working on a change

- Read [`AGENTS.md`](../AGENTS.md) for repository conventions, test commands, and safe isolated runs.
- For UI work, read [`DESIGN.md`](../DESIGN.md) for Polymux policies and [cdesign](../.agents/skills/cdesign/SKILL.md) for general design rules.
- For the product introduction and requirements, see the [root README](../README.md).
- User-facing documentation lives in [`apps/site/src/content/docs/`](../apps/site/src/content/docs/); this folder is for understanding and developing the repository.
