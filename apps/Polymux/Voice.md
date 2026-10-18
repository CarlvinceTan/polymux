# Voice conversations

Voice is a private conversation with a spokesperson agent representing the selected
agent or group. Typed messaging remains a separate, direct conversation.

- STT/TTS: Apple recognition → text spokesperson LLM → Apple synthesis.
- Live: a native-audio LLM is the spokesperson, with no extra frontend LLM.
- Both spokespersons consult workers through `ask_agent` and `VoiceBridge`.
- `POST /threads/{id}/voice` executes workers in isolated, non-persisted sessions.
  It preserves their configured model, identity, builtin tools, MCP, workspace,
  coordination limits, and tool hooks. No private text enters chat or search.
- Workers explicitly use `send_to_user` or `show_html` to publish useful material
  in typed chat. Ordinary worker replies return only to the spokesperson.
- Closing or cancelling a private request never calls the typed thread's stop
  endpoint. Private tasks are not queued offline or automatically replayed.

## Adding a provider

Implement `LiveProvider` in PolymuxKit and add its server relay and settings validation.
The provider handles setup, wire encoding, and normalized events; the server authenticates upstream.
`LiveStore` owns audio, playback interruption, and asynchronous worker consultation.
Use mono PCM16 little-endian bytes with explicit sample rates, retain tool-call IDs,
and distinguish turn completion from an idle session. Never expose credentials in errors.

For STT/TTS, implement `TextProvider` and add its server relay. `Spokesperson`
owns private conversation history and the worker tool loop. Preserve provider-owned
assistant state, including function signatures, in `TextAnswer.state`.

Model/provider preferences, voice instructions, theme, and saved sidebar width belong
to the server. Clients poll `/settings` and PATCH only changed fields. Speech API keys
are write-only through `/settings/keys/{provider}` and stay in the server's owner-only
`settings.json`; GET returns only configured provider names. `/speech/text` and
`/speech/live` authenticate and relay Gemini traffic without returning the key.
These endpoints rely on the same private-tailnet boundary as the rest of Polymux and
reject browser-origin requests. Do not expose the server publicly.
Old device preferences are not uploaded and existing device keys are not imported.
Set the key once through Settings after updating. Settings apply when reopening
voice. Live playback interruption is separate from delegated-task cancellation;
STT/TTS currently cancels the current private turn on barge-in.

Run `swift test`, the macOS/iOS builds, and `go test -race -tags sqlite_fts5 ./...`
in `apps/server`. The privacy and spokesperson tests guard this separation.
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
