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

The model is never bundled. [`ModelStore`](../packages/SpeechKit/Sources/SpeechKit/ModelStore.swift) downloads the archive described in [`DecisionModel.swift`](../packages/SpeechKit/Sources/SpeechKit/DecisionModel.swift) from the `models` GitHub release, checks its SHA-256, compiles it and keeps it in the client data folder under `models/`. The Review model row in Voice settings controls it. Scoring runs in the app on both the Mac and iPhone.

GLiDE is under the Fastino Community License. The licence files ship in the package's [`Resources/`](../packages/SpeechKit/Sources/SpeechKit/Resources/) and inside the archive, and the settings row shows "Built with Fastino GLiDE".

## Conversion and the oracle

Scoring happens in the app; only preparing the model is offline. Fastino publishes GLiDE as PyTorch weights, and [`scripts/glide.py`](../scripts/glide.py) derives what the app uses from them:

| Step | Produces |
| --- | --- |
| `oracle <checkout>` | Rescores the oracle with the PyTorch model. Needed only when the model or prompt changes. |
| `fit` | The gate constants for [`DecisionModel.swift`](../packages/SpeechKit/Sources/SpeechKit/DecisionModel.swift), with the threshold that misses fewest wrong transcripts (best F2). |
| `convert <checkout> <out>` | The Core ML package: 16-bit floats, a fixed 256-token input, one probability out. |
| `parity <package>` | Fails if the Core ML probabilities drift more than 0.01 from the oracle. |
| `pack <package>` | `dist/models/glide-0.4b.tar` with the licence files, and the size and SHA-256 to put in `DecisionModel.swift`. |

`tokenizer.json` is the model's own; [`BPETokenizer`](../packages/SpeechKit/Sources/SpeechKit/BPETokenizer.swift) is a Swift port of its encoder. Upload a new archive under a new file name on the `models` release, since installed apps verify the old one by its checksum.

The oracle, [`packages/SpeechKit/Bench/oracle.json`](../packages/SpeechKit/Bench/oracle.json), is the benchmark all of this is checked against: 1,200 dictation transcripts (600 for fitting, 600 held out), each with its word confidences, whether it was wrong, and the token ids and probability the PyTorch model gave it. The transcripts came from spoken sentences run through the app's own dictation path.

[`OracleBench`](../packages/SpeechKit/Bench/OracleBench.swift) runs with the Swift tests. It checks that the Swift tokenizer reproduces every row's ids and that the shipped constants miss 17 of the 241 wrong held-out transcripts while flagging 320 of 600. To also score an installed model against the oracle, name the client folder that holds it:

```sh
POLYMUX_CLIENT_DIR=~/.polymux swift test --filter SpeechKitBench
```
