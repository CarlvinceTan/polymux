# Benchmarks

Status: runs in [`apps/server/bench`](../apps/server/bench/) with two suites. Solo has 16 tier 1 scenarios and 24 tier 2 and 3 errands on a persona's inbox, calendar, bank and six simulated websites. Group has 6 scenarios for a team of specialist bots. Systems: Polymux, a Polymux team, opencode and `null`, which does nothing. The rest of this page is the target design; [the catalogue](../apps/server/bench/catalog.md) tracks what is planned.

The benchmark scores how well a harness handles real assistant work, so changes to Polymux, and other harnesses, can be compared on quality, speed and cost. It complements [`apps/server/evals/`](../apps/server/evals/), which checks the server's behaviour deterministically against a scripted model; the benchmark uses a real, pinned model. What it optimises for is [Bot quality](architecture.md#bot-quality).

## What it reports

- **Quality** (0–100): the weighted average of category scores.
- **Speed** (0–100): 0.5 × time to completion + 0.3 × responsiveness + 0.2 × throughput. Time to completion is per task, from request to result; responsiveness is per user message, the time to the first reply, even while the agent is busy; throughput comes from scenarios where requests keep stacking. Each is scored 100 × min(1, target ÷ actual).
- **Cost**: dollars and tokens per task, raw and scored against a target.

Each suite has its own index: **solo** judges one assistant, **group** judges a team of bots sharing a chat. Every summary also lists task statistics: tasks every system passes (saturated), none passes (unsolved), that separate systems, or that flip between repeats (noise). Scores are comparable only within one index version.

## Structure

An index version weights categories; a category holds scenarios; a scenario expands into seeded variants.

- **Category**: a capability. Each spans scenarios from at least three domains (travel, bookings, shopping, email and calendar, files and coding, smart home, finance, research), so no category tests one situation.
- **Scenario**: a user script, environment events, checks, time and cost targets, tags and a difficulty tier.
- **Variant**: names, values, wording and fixture contents drawn from a seed, plus difficulty settings: stacked tasks, horizon length, context length, tool failures, distractors and ambiguity.

[`apps/server/bench/catalog.md`](../apps/server/bench/catalog.md) lists the 19 quality categories, their weights and every planned scenario, set in the lives of recurring personas so the data and phrasing stay realistic.

Conversational quality, whether the agent talks like a thoughtful person, as long as the content needs and without filler, has no standard benchmark. It is scored by a rubric judge calibrated against hand-rated replies, on a different pinned model and controlled for length, backed by automatic checks for filler, Markdown and walls of text.

## Fixed conditions

Each index version freezes the model, the simulated world, the simulated user, seeds and timeouts, so a score changes only with the harness.

- Model: `deepseek-v4.1-flash` through OpenCode Go, reasoning `high`. OpenCode Go serves only this alias, not the dated 1017 snapshot, so each run records its date and first scores a fixed canary set; a shift there means the model changed.
- A metering proxy sits between every harness and the model. It enforces the model and reasoning level, overriding whatever the harness asks for, counts tokens and cost identically for every harness, and holds the API key.

## Running it

From `apps/server`, with `.env` holding `POLYMUX_MODEL_URL`, `POLYMUX_MODEL_KEY` and `POLYMUX_MODEL`:

```sh
go run -tags sqlite_fts5 ./bench/cmd/bench -systems polymux,polymux-team,opencode,null -reps 3
go test -tags sqlite_fts5 ./bench    # the validity gate, no model needed
```

`-suites solo,group`, `-tiers 1,2,3` and `-scenarios a,b` narrow a run; `polymux-team` only runs the group suite. `-model` pins another model from the same provider: harness differences show more on a weaker one.

Results land in `.build/bench/<time>/`: `summary.md` with the index and failed tasks, `rows.json`, and a folder per run whose `run.json` holds what the user sent and saw, every world tool call, every model call and its cost, and whose `request.json` is the first request the harness sent with tools, to compare prompts. A run whose system stays busy without doing anything writes a goroutine dump (`stall-*.txt`). Polymux runs in-process, so limits PiG keeps per process, such as its codemode workers, are scaled by `-parallel` to match one server per run.

## Running other harnesses

- The simulated world is an MCP server, so any harness with MCP gets the same tools and state. Checks read the world's state, not the harness's account of it.
- An adapter per harness starts it with the proxy and world, sends user messages, delivers environment events, stops it and collects what the user saw, with timestamps.
- Adapters declare capabilities: messages during a run, background work, scheduled reminders, outside events, parallel tasks. A scenario that needs a missing capability fails, with the reason shown.
- Planned adapters: Polymux (an isolated server), opencode, Hermes Agent, Codex CLI, Claude Code (through the proxy's API translation) and Rakazo.
- External suites, such as Terminal-Bench 2, BrowseComp-Plus, τ²-bench, GAIA2 and MacAgentBench, are mapped to categories in [the catalogue](../apps/server/bench/catalog.md#external-suites). A suite joins an index only once it runs offline inside the sandbox.
- App-control scenarios drive a Mac through device tools, so they run only in a macOS virtual machine sandbox, never on a real desktop. Browser scenarios drive Chromium against simulated websites the world serves locally.

## Safety

- Every effectful tool, such as bookings, email, purchases and files, is simulated, so nothing real can be reached, including from prompt-injection scenarios. Polymux runs with no host tools, only the world's, and the coding scenarios' tests run in their own sandbox.
- Each run is sandboxed. On macOS that is a Seatbelt profile (`sandbox-exec`), as Codex CLI and Claude Code use, with a throwaway home and workspace, network limited to the proxy and the world on localhost, and CPU, memory and time limits. No run sees real configuration, Keychain or app data.
- The proxy enforces spend caps per run and per suite.
- Sandbox providers are pluggable. macOS Seatbelt comes first because Polymux runs on macOS; a macOS virtual machine provider can add full isolation later, and Linux providers (bubblewrap, Apple's `container`, which runs Linux rather than macOS) arrive with Linux support.

## Keeping it honest

- **Validity gate**: before a scenario counts, a scripted perfect agent must pass every task and a do-nothing agent none (`valid_test.go`). A check that only guards against harm, such as "booked nothing extra", scores only when the job itself was done. The `null` system repeats the do-nothing check live.
- **Checker agreement**: state checks are preferred over text matching. A text check tries a pattern first; when the pattern misses, a judge on its own pinned model (`-judge`, `judge.go`) rules on whether the replies answer the check's question. It can rescue a correct answer worded unexpectedly but never fails one, and each ruling is kept in `run.json`. A hand-labelled sample of transcripts must agree with every checker at least 95% of the time.
- **Task statistics** in every summary: difficulty (pass rate), discrimination (whether a task separates systems) and noise (flips between repeats). Tasks everyone passes, nobody passes or that flip at random are fixed or dropped. Tier 1 is mostly saturated, which is why tiers 2 and 3 exist.
- **Representativeness**: a coverage table maps scenarios to real workflows, and weights are reviewed against it.
- **Generalisation**: seeded variants (a gap between public and variant scores means overfitting), a private split kept away from agents that edit prompts, held-out domains and runs on at least two models.
- **Growth**: when a category's best score passes 90%, the next difficulty tier joins in a new index version.

## Layout

| In `apps/server/bench/` | Holds |
| --- | --- |
| `proxy.go` | The metering proxy |
| `world.go`, `fixtures.go` | The simulated world and its MCP server, with tool sets per bot role |
| `persona.go`, `office.go`, `web.go` | Ana's life: inbox, calendar and bank tools, and the simulated websites |
| `scenario.go`, `scenarios.go`, `solo.go`, `group.go` | The scenario framework, the simulated user, tier 1, and the solo and group suites |
| `valid_test.go` | The validity gate |
| `judge.go` | The judge for replies a pattern misses |
| `system.go`, `opencode.go` | The system interface and its adapters, including the Polymux team and `null` |
| `run.go`, `score.go` | The runner and the index |
| `cmd/bench/` | The command |
| `catalog.md` | Every planned scenario and external suite |

As scenarios grow they move into folders per category, and world data into packs per domain.

## Prior art

- [Artificial Analysis](https://artificialanalysis.ai): weighted categories, versioned indices, private tasks, speed and price reported separately.
- [Inspect](https://inspect.aisi.org.uk/tasks.html): a task is a dataset, a solver and a scorer.
- [Harbor](https://docs.litellm.ai/docs/projects/Harbor.md) and [Terminal-Bench](https://huggingface.co/datasets/harborframework/terminal-bench-2.0): one folder per task, custom agent adapters.
- [τ²-bench](https://arxiv.org/pdf/2506.07982): simulated users, domains and reliability over repeats.
- [GAIA2](https://huggingface.co/blog/gaia2): environment events, timing, noise and ambiguity.
- [π-Bench](https://www.alphaxiv.org/abs/2605.14678) and [ProAgentBench](https://arxiv.org/pdf/2602.04482): proactivity and long-horizon assistance.
- [Agentic Benchmark Checklist](https://arxiv.org/html/2507.02825v5) and [BetterBench](https://arxiv.org/html/2411.12990v1): task and outcome validity, reporting.
- [tinyBenchmarks](https://arxiv.org/html/2402.14992): item response theory for task statistics.
- [GSM-Symbolic](https://arxiv.org/html/2410.05229v1): templated variants to expose overfitting.
