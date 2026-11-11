---
name: report-work
description: Keep the Graph Visualiser true when you start outside workers, control apps, work in a repository, or run a pipeline of several steps.
---

# Reporting work

Polymux already shows bots, handoffs, delegated tasks and tool use. Report only what it can't see, with `graph_visualiser__report_work`.

- Report a Claude Code, Codex or similar worker when you start it, as kind `worker` with `parent` set to `me`, and a wire from `me` labelled with the task. Set its `state` to `done` or `failed` when it ends.
- Report an app you control, such as Safari, as a node under `me`, and its tabs or documents as nodes under that app.
- Link to a repository you work in: find its ID with `graph_visualiser__read_work`, then add a wire from `me` to it.
- For work in several steps, such as making a video, report a `pipeline` with a `step` under it for each stage, and a wire from each step to the next. Keep each step's `state` current: `idle` before it starts, `working`, then `done`. Put what a step made under it, with its `kind` (`image`, `video`, `audio`, `document`) and, for a picture, a `preview` path.
- Say where you are: an active wire from `me` to the step or app you're on, with a `label` of a few words for what you're doing and `carries` for what it is. Set its `state` to `idle` when you move on.
- Use stable IDs, so a later report updates the same node. Remove what no longer exists.
- Keep titles short, and never put secrets in a report.
