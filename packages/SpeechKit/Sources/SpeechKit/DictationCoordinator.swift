import Foundation
import Speech

/// One finished transcription candidate from a language model.
struct DictationFinal: Equatable, Sendable {
    let text: String
    let confidence: Double
}

enum DictationChoice {
    /// The most confident transcript wins; a longer one breaks a tie.
    static func best(_ finals: [String: DictationFinal]) -> DictationFinal? {
        finals.values.max {
            $0.confidence == $1.confidence ? $0.text.count < $1.text.count : $0.confidence < $1.confidence
        }
    }
}

/// Merges the live results of several dictation models into one transcript.
/// The models hear the same audio in different languages; the one that heard
/// the speaker has the higher confidence when a segment finalizes.
actor DictationCoordinator {
    private let continuation: AsyncThrowingStream<VoiceEvent, Error>.Continuation
    private let locales: Set<String>
    private var failed: Set<String> = []
    private var current: Segment?
    private var nextID = 0
    private var finished = false
    private var lastText = ""

    private struct Segment {
        let id: Int
        let start: Double
        var end: Double
        var partials: [String: String] = [:]
        var finals: [String: DictationFinal] = [:]
        var timer: Task<Void, Never>?
    }

    init(continuation: AsyncThrowingStream<VoiceEvent, Error>.Continuation, locales: [String]) {
        self.continuation = continuation
        self.locales = Set(locales)
    }

    func accept(_ result: DictationTranscriber.Result, locale: String) {
        accept(text: String(result.text.characters), start: result.range.start.seconds,
               end: result.range.end.seconds, final: result.isFinal,
               confidence: averageConfidence(result.text), locale: locale)
    }

    func accept(text: String, start: Double, end: Double, final: Bool, confidence: Double, locale: String) {
        guard !finished, !failed.contains(locale) else { return }
        let text = text.trimmingCharacters(in: .whitespacesAndNewlines)
        if current == nil || abs(start - current!.start) > 0.6 {
            commit()
            nextID += 1
            current = Segment(id: nextID, start: start, end: end)
        }
        guard current != nil else { return }
        current!.end = max(current!.end, end)
        if final {
            current!.finals[locale] = DictationFinal(text: text, confidence: confidence)
            current!.partials[locale] = text
            if current!.finals.count >= activeLocales.count {
                commit()
            } else {
                scheduleCommit()
            }
        } else if !text.isEmpty {
            current!.partials[locale] = text
            emitPartial()
        }
    }

    func failed(_ locale: String) {
        guard !finished, locales.contains(locale) else { return }
        failed.insert(locale)
        if failed.count >= locales.count {
            finished = true
            continuation.finish(throwing: SpeechFailure.speech)
        } else if let current, current.finals.count >= activeLocales.count {
            commit()
        }
    }

    func finish() {
        guard !finished else { return }
        commit()
        finished = true
        continuation.finish()
    }

    private var activeLocales: Set<String> { locales.subtracting(failed) }

    private func emitPartial() {
        guard let segment = current else { return }
        let text = DictationChoice.best(segment.finals)?.text
            ?? segment.partials.values.max { $0.count < $1.count } ?? ""
        guard !text.isEmpty, text != lastText else { return }
        lastText = text
        continuation.yield(.transcript(VoiceTranscript(text: text, start: segment.start, end: segment.end, final: false)))
    }

    private func scheduleCommit() {
        guard let segment = current else { return }
        segment.timer?.cancel()
        let id = segment.id
        current!.timer = Task { [weak self] in
            try? await Task.sleep(for: .milliseconds(500))
            guard !Task.isCancelled else { return }
            await self?.commitIfCurrent(id)
        }
    }

    private func commitIfCurrent(_ id: Int) {
        guard current?.id == id else { return }
        commit()
    }

    private func commit() {
        guard let segment = current else { return }
        segment.timer?.cancel()
        current = nil
        lastText = ""
        let text = DictationChoice.best(segment.finals)?.text
            ?? segment.partials.values.max { $0.count < $1.count } ?? ""
        guard !text.isEmpty else { return }
        continuation.yield(.transcript(VoiceTranscript(text: text, start: segment.start, end: segment.end, final: true)))
    }

    private func averageConfidence(_ text: AttributedString) -> Double {
        let values = text.runs.compactMap { $0.transcriptionConfidence }
        guard !values.isEmpty else { return 0 }
        return values.reduce(0, +) / Double(values.count)
    }
}
