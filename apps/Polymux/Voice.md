# Voice conversations

Voice is a private conversation that never enters typed chat or search.

- Cascaded: Apple recognition → the chat's agent → Apple synthesis. The transcript goes
  straight to `POST /threads/{id}/voice/stream`; the agent replies in spoken style and its
  text streams as server-sent events, so speech starts at the first sentence. Any provider works.
- Live (full duplex): a native-audio model (Gemini Live or OpenAI Realtime) is a spokesperson
  that consults the chat's agents through `ask_agent` → `VoiceBridge` → `POST /threads/{id}/voice`.
- Voice turns run in isolated, non-persisted sessions with the agent's configured model, identity,
  tools, MCP, workspace, coordination limits and tool hooks. Their durable transcript is deleted
  when the turn ends. Agents use `send_to_user` or `show_html` to publish into typed chat on purpose.
- Closing or cancelling a voice turn never stops the typed thread. Voice turns are not queued
  offline or replayed automatically; a failed session retries itself a few times.

## Adding a live provider

Implement `LiveProvider` in PolymuxKit (setup, wire encoding, normalized `LiveEvent`s) and teach
`/speech/live` to dial it (`dialSpeech`, `configureSpeech`) and validate its voices in settings.
`LiveStore` owns audio, playback interruption and asynchronous agent consultation. Use mono
PCM16 little-endian bytes with explicit sample rates, keep tool-call IDs, and distinguish turn
completion from an idle session. Never expose credentials in errors.

Voice mode, live provider, model, voice, language and instructions are server settings. Keys come
from the connected provider of the same type in Settings → Providers; the server adds them
upstream, so the app never holds them.
`/speech/live` rejects browser-origin requests and relies on the same network boundary as the
rest of Polymux: loopback, Tailscale, or the encrypted tunnel. Live playback interruption is separate from cancelling delegated work.

Run `swift test`, the macOS/iOS builds, and `go test -race -tags sqlite_fts5 ./...`
in `apps/server`. The privacy and voice stream tests guard this separation.
Shared UI rules and the macOS input-focus check are documented in `DESIGN.md`
at the repository root.

## Diagnostic access

Instruction-only: rebuilt diagnostic executables may require fresh macOS Keychain
approval, even at the same path. Unattended helpers should request non-interactive
access (`kSecUseAuthenticationUIFail`) and fail with a safe access error, rather than
wait on a hidden approval dialog. Bound the process as well as network timeouts;
never change Keychain permissions or export the key to make a check work.

Observed 2026-10-09: the trusted helper verified Gemini Live audio, but rebuilt
tool-check helpers stalled at Keychain access before sending requests. Those
processes were stopped. This guidance is not a runtime guard; real-provider tool
round trips remain unverified until approved credential access is available.
