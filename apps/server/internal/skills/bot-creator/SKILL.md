---
name: bot-creator
description: Design, create or improve a Polymux bot. Use when the user asks to make or hire a bot, wants a bot to take on a different job, or asks why a bot keeps behaving badly. Covers one job, anti-jobs, voice, wake mode, instructions and verification.
---

# Bot creator

A bot is one job, its anti-jobs, a voice, and when it wakes. Write those four before calling `create_agent`. If the job needs "and" to describe it, it is two bots or a group.

## Intake

Ask only what changes the design, and skip what is already answered:

- the one job, with two or three real requests the user would send it
- what it must never do, especially the adjacent verb: a scout does not post, a drafter does not send
- when it wakes: on demand, a routine, a trigger, or a mix
- who it talks to: this user, a group, another bot, an outside channel
- name and voice only if they care

Do not ask about the model, tools or skills when the default or a working sibling will do. Pick reversible details yourself and say what you picked. Once the job is clear, create it; do not ask "should I?".

## Choose the shape

A bot is for a persistent role with its own chat and memory. Use a task for bounded work, a routine for recurring work, a group for several specialists, and a plugin for a UI or tool. Create a bot only when the job is real and steady.

## Write the instructions

- `role` is a short label of the one job, not a paragraph. It is what the user sees.
- `prompt` is the job, the anti-jobs, the concrete how (which inbox, API, folder or channel), and when to stay quiet.
- Do not restate Polymux's default behaviour or voice. `create_agent` appends them. If the user wants a distinct voice, create the bot first, then set its full instructions with `update_agent`; that replaces the defaults, so carry over the job, anti-jobs and the parts that still apply.
- No "I can also help with..." or capability lists. No instructions for work the bot will not do. Keep it short enough to read in one screen.
- Every bot already has Polymux's tools. Do not attach an integration, skill or plugin the job will not use.

## Create

1. Find `create_agent` with `tool_search`. Pass `role` and `prompt`. Set `name` only when the user asked for one. Leave the model unset unless they named one; if they did, set both provider and model. Use `initial_task` only for a real first piece of work. If a chat's rules block it, create without it and message the new bot instead.
2. The new bot's first turn is an introduction, and it can only ask a question; its initial task starts after that.
3. A new bot starts in its own direct chat. Add it to a group only for team work.

## Wake and quiet

- On demand: nothing else to set up.
- Routine: you cannot create one for another bot. After creating it, message it with the exact prompt, schedule and timezone, and tell it to stay quiet when there is nothing to report. Ask it to confirm the routine; do not say it is set up until it does.
- Trigger: connect an event to work that is already defined; do not use one for an undefined job.
- A routine wakes the bot and spends tokens every time. Prefer the least frequent schedule that does the job, and pause or cancel one that never reports.

## Verify

Send the new bot one real request from the intake and read its reply, not just the create result. If it misses the job, fix its instructions with `update_agent` and test again. Tell the user its name and one-job line, and that they can delete it from the sidebar if it is wrong.

## Improving a bot

Subtract before adding. Check its recent chat for corrections and instructions it ignored; fix the prompt before adding a skill, integration or routine. A new bot is for a steady specialist job, not for one that keeps changing. `update_agent` replaces the whole prompt, so carry over what should stay.

## Keeping the set healthy

A bot can only read conversations it is in and its own routines. When the user has several bots, offer a review one bot at a time: look for repeated corrections, rules the bot ignores, and routines that fail or never report. Propose at most three changes with the exact edit, and change nothing until the user picks.

---

Adapted for Polymux from Lauren Tan's design-a-grok-bot (dr eggbot): one job, anti-jobs, voice and wake, rewritten for Polymux's prompt layers and tools.
