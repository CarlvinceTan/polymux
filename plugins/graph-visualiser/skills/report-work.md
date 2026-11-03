---
name: report-work
description: Keep the Graph Visualiser true when you start outside workers, control apps, or work in a repository.
---

# Reporting work

Polymux already shows bots, handoffs, delegated tasks and tool use. Report only what it can't see, with `graph_visualiser__report_work`.

- Report a Claude Code, Codex or similar worker when you start it, with `parent` set to `me`, and a wire from `me` labelled with the task. Set its `state` to `done` or `failed` when it ends.
- Report an app you control, such as Safari, as a node under `me`, and its tabs or documents as nodes under that app.
- Link to a repository you work in: find its ID with `graph_visualiser__read_work`, then add a wire from `me` to it.
- Use stable IDs, so a later report updates the same node. Remove what no longer exists.
- Keep titles short, and never put secrets in a report.
