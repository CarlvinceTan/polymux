---
title: Schedules and tasks
slug: schedules-and-tasks
description: Ask agents to schedule routines, control background reviews, and delegate bounded work.
section: Using Polymux
sectionOrder: 2
order: 5
published: true
---

# Schedules and tasks

Agents can schedule routines and delegate work on the Go server. The native app shows the conversation and profile controls rather than the older Tasks board and Subagents workspace tabs.

## Schedule a routine

Ask an agent in chat for a one-time or recurring request. Include the work, start time, timezone, and interval clearly. For example:

> Remind me in two hours to review the project brief. Confirm the time and timezone before saving it.

The current scheduler supports one-shot and interval routines, not a general cron-expression editor. Check the agent's confirmation and the routine's displayed next run rather than assuming it interpreted a calendar rule as intended.

Open the agent's **Edit profile → Schedule** section to review routines and cancel one. A pending routine can be waiting for your approval. The server must be running with the necessary model and tool connections when work is due. Missed recurring intervals advance to a future run rather than replaying every missed occurrence.

## Background reviews

The profile's **Background reviews** toggle controls the agent's periodic reviews. These become eligible after the first direct-chat user task; the normal cadence is four hours. You can also send the exact chat command `pause proactivity` or `resume proactivity`.

Notifications are optional and configured in the native Settings panel. Whether a notification arrives also depends on operating-system permission and the server's delivery setup.

## Subagents

An agent can use `spawn_task` to queue a bounded child task, optionally for another agent. The child receives the explicit task rather than the parent's whole conversation or private memory, and cannot recursively delegate more children.

Follow activity and results in the conversation. Starting a child task is not evidence that it has finished. There is no separate native Subagents tab or Tasks board at present.
