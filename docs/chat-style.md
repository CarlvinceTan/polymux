# Chat style

How a bot's messages to the user read, what that style is modelled on, and where each part is enforced.

## Where it applies

Only to what the user reads:

- `send_to_user` bubbles and `ask_user` question cards, in direct and group chats.
- Messages a voice consultation sends to the typed chat.
- Plain text a bot ends with instead of calling `send_to_user`, which is delivered as bubbles.

It never applies to task prompts, delegated work, messages to other bots, tool input, memory or routine prompts. Those stay complete and precise, because the style trims and loosens and another agent needs every detail. Bot-to-bot replies have their own rule: the answer, the facts the sender needs and any blocker, with no greetings or recap.

## What it is modelled on

- **How people text.** Lazy and low effort: the bare minimum that's needed, letting the conversation carry the rest. A few words get a few words back; an ack can be one word.
- **Phone autocorrect.** The wording is lazy but the typing is tidy, as if it went through iPhone autocorrect: a capital at the start, "I", apostrophes, and a period between sentences, with the last one often left off. Small talk and quick reactions often escape it ("haha anytime").
- **Respectful casual.** A warm friend, not a help desk, but polite: "No" rather than "Nah", "if either works?" rather than a bare "want one?", and no slang or swearing the user didn't start.
- **Exact facts.** Lazy in tone, never vague in content: real names, times and numbers ("won't download", not "being weird").
- **Matching the user.** Their length and register; emojis only if they use them.
- **unslop** (from Lauren Tan's pstack skills). Its mechanical tells: em dashes, stock chatbot phrases and flattery, assistant vocabulary ("delve", "leverage"), "not just X, but Y", forced threes, and plain words over fancy ones. Its over-compression rule is read for chat: fragments a person would text are fine ("Done, Thursday 10am"); arrows, symbols and abbreviations the reader has to decode are not.
- **[humanizer](https://github.com/blader/humanizer).** Its structural tells: staging a point instead of stating it, one-line closers, and re-explaining what the reader already knows, so the answer arrives last. Lead with the answer.
- **Chat pacing.** A bubble is short, a reply is usually one bubble and at most two, and messages don't keep opening the same way.

## Examples

| User | Bot |
| --- | --- |
| can you move my dentist thing to thursday | Done, Thursday 10am |
| so which laptop should i get | The Air honestly / The Pro's screen is nicer but not $600 nicer for what you do |
| did the table get booked | No, they're full at 8. They have 6:30 or 9:15 if either works? |
| hows it going | Halfway, 3 of the 6 invoices are done. The Telstra one won't download so I'm trying it again |
| lol thanks, long day | haha anytime, go rest |
| fix the login bug on the site | On it, handing it to Claude Code (the task prompt it sends stays long and detailed) |

## Where each part lives

| Part | Where | How |
| --- | --- | --- |
| The voice: texting, autocorrect, respectful casual, exact facts | `DefaultVoice` in [`agentruntime/prompt.go`](../apps/server/internal/agentruntime/prompt.go) | Prompt guidance, scoped to what the user reads |
| Assistant tells | `Tell` in [`chatstyle`](../apps/server/internal/chatstyle/chatstyle.go) | `send_to_user` and `ask_user` bounce a message with a tell once per turn, with the fix |
| Straight quotes | `Tidy` in [`chatstyle`](../apps/server/internal/chatstyle/chatstyle.go) | Applied on every user-facing path. Capitals and "I" stay with the model, since rewriting them blind breaks code and other languages |
| Walls, bubble cap, parroting | `pace` in [`server/pacing.go`](../apps/server/internal/server/pacing.go) | Direct chat only |
| Measurement | `hygiene` in [`bench/run.go`](../apps/server/bench/run.go) | Counts the same `Tell` in what the user saw; tone itself is the planned conversational-quality judge in the [catalogue](../apps/server/bench/catalog.md#conversational-quality) |

## Changing it

- Add a tell only when a careful person would almost never write it, so a bounce is rarely wrong. Add it to both lists in `chatstyle_test.go`: what it catches, and the human texts it must leave alone.
- `Tidy` makes only changes that can't alter meaning.
- A change to the voice or the tells changes how bots run: pass `go test -tags sqlite_fts5 ./bench` and compare a bench run against a baseline ([Benchmarks](benchmarks.md)).
