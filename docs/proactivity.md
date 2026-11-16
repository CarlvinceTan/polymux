# Proactive behaviour — design notes

North star: lift cognitive load off the user. Every message costs attention, so it has to save more than it spends.

Design direction, not current implementation: the messaging part is built (see [Architecture](architecture.md)); the proactivity architecture below is not.

Hierarchical, systematic architecture — the same move as hierarchical memory (MemGPT-style tiers). Each level has its own timescale and its own process.

## Levels

- **Role / purpose** (months, open-ended) — the bot's mandate, given or deduced from conversation. Latitude matches position: a junior gets a clear goal, someone senior gets an open-ended one.
- **Goals** (days–weeks) — two kinds, with subgoals under them:
  - *Conditional*: clear completion conditions; split into stages, achieve. Afterwards "what's next?" stays tied to user intent — don't go haywire.
  - *Non-conditional* (open-ended, e.g. "improve company performance"): no completion condition; needs creativity and initiative.
- **Stages** (hours–days) — rhythm: work blocks that advance the current stage. Keeps alignment with the goal; stops sidetracking and over-investing in one thing.
- **Intentions** (minutes–hours) — cue → action, verified source. Prospective memory: "when X happens, do Y", fires on the cue, closes when met.
- **Schedules** — for the model to remind itself of things. Each carries its purpose and an end point; pauses itself when the purpose is met, it keeps failing, or nobody engages. No stale schedules.
- **Passive awareness** (continuous) — cheap matching of events to intentions and goals → recall ("oh, I need to…"). An observer of the overall situation, distinct from active focus.
- **Active focus** — the live conversation: full attention, on-topic. When the user isn't discussing, the bot still proactively does things, like a person does.

## Dreaming, ingestion, association — wanted, system not yet figured out

- Creativity comes from ingesting information and being inspired: hypothesising something that may be ridiculous but is still worth trying.
- Ingestion has to be good and automatic: the bot's information streams (repo, issues, CI history, dependency release notes, related chats, feeds in its domain) digested passively into knowledge — not only what the bot chooses to save from chat.
- Association: new items linked to goals and existing knowledge; dreams sample across distant links.
- The system for this part is still open.

## Separate system: messaging behaviour

Lazy messaging, reactions as replies, pacing with message bubbles and parroting prevention are a messaging-behaviour system of their own, independent of the proactivity architecture. Implemented for direct chat: the voice and guides in `DefaultVoice`, and the runtime pacing in `apps/server/internal/server/pacing.go`; see the messaging doctrine in [Architecture](architecture.md).