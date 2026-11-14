# Speech

[`packages/SpeechKit/`](../packages/SpeechKit/) is the Swift package for speech-to-text: microphone capture, Apple recognition and dictation post-processing. It has no Polymux dependencies. Speech-to-speech calls are separate, in [`packages/PolymuxVoice/`](../packages/PolymuxVoice/).

## Dictation

A dictation passes through four stages. The composer drives them in [`ComposerView.swift`](../apps/Polymux/Sources/Views/ComposerView.swift); the settings are device-local, in [`DictationSettings.swift`](../apps/Polymux/Sources/VoiceUI/DictationSettings.swift).

| Stage | Code | What it does |
| --- | --- | --- |
| Recognise | `DictationInput`, `AppleInput`, `DictationCoordinator` | Captures audio, runs the recognisers and merges their results, keeping each word's confidence. |
| Clean | `DictationText`, `FillerWords`, `Vocabulary` | Deterministic fixes: fillers, spacing, spoken symbols and dictionary terms. Always runs. |
| Polish | `DictationPolish` | Optional rewrite by Apple's on-device model. |
| Review | `DictationGate`, `GlideModel`, `DictationReview` | Decides whether the transcript looks misheard and, if so, asks Apple's model for dictionary suggestions. |

Review never changes the dictated text. Its findings wait under Suggested in Voice settings until the user adds or dismisses them.

## Review gate

The gate keeps the review model from running on transcripts that are probably fine.

- **Confidence only.** Without a downloaded model, [`DictationGate`](../packages/SpeechKit/Sources/SpeechKit/Review.swift) flags a dictation when its mean word confidence is below 0.92.
- **With the model.** [`GlideModel`](../packages/SpeechKit/Sources/SpeechKit/GlideModel.swift) runs GLiDE 0.4B on a fixed prompt and reads one number, the probability that a word was misheard. [`GateConstants`](../packages/SpeechKit/Sources/SpeechKit/GateConstants.swift) combines it with confidence in a fitted logistic. Prompts over 256 tokens fall back to confidence only.

The model is never bundled. [`ModelStore`](../packages/SpeechKit/Sources/SpeechKit/ModelStore.swift) downloads the archive described in [`DecisionModel.swift`](../packages/SpeechKit/Sources/SpeechKit/DecisionModel.swift) from the `models` GitHub release, checks its SHA-256, compiles it and keeps it in the client data folder under `models/`. The Review model row in Voice settings controls it, on the Mac only.

GLiDE is under the Fastino Community License. The licence files ship in the package's [`Resources/`](../packages/SpeechKit/Sources/SpeechKit/Resources/) and inside the archive, and the settings row shows "Built with Fastino GLiDE".

## Conversion and the oracle

Fastino publishes GLiDE as PyTorch weights. Three things in the repository are derived from them offline, and must be redone together if the model, prompt or tokenizer changes:

- **The archive.** The encoder converted to a Core ML package (16-bit floats, fixed 256-token input) that returns the misheard probability directly, packed as a plain tar with the licence files.
- **The tokenizer.** `tokenizer.json` is the model's own; [`BPETokenizer`](../packages/SpeechKit/Sources/SpeechKit/BPETokenizer.swift) is a Swift port of its encoder.
- **The gate constants.** Fitted on 600 labelled transcripts, with the threshold chosen to miss few wrong transcripts (best F2).

The oracle is the reference these are checked against: 1,200 dictation transcripts (600 for fitting, 600 held out), each with its word confidences, whether it was wrong, and the token ids and probability the original PyTorch model gave it. The port passes when the Swift tokenizer produces the same ids, the Core ML probability stays within 0.01, and the flags agree. At conversion it matched 1,200 of 1,200 ids and 1,199 of 1,200 flags; on the held-out set the gate missed 17 of 241 wrong transcripts while flagging 53%.

[`TokenizerTests`](../packages/SpeechKit/Tests/SpeechKitTests/TokenizerTests.swift) and [`GateTests`](../packages/SpeechKit/Tests/SpeechKitTests/GateTests.swift) pin a sample of oracle rows. The conversion scripts and the full oracle are not in the repository.
