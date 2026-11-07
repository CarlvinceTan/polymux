import Foundation
import AVFoundation
import Speech

@MainActor @Observable
public final class DictationInput {
    public private(set) var active = false
    public private(set) var stopping = false
    public private(set) var ready = false
    public private(set) var level: Float = 0
    public private(set) var denied = false
    private let permission: @Sendable () -> Bool

    public nonisolated static func isDenied() -> Bool {
        AVAudioApplication.shared.recordPermission == .denied
            || [.denied, .restricted].contains(SFSpeechRecognizer.authorizationStatus())
    }
    private let makeInput: () -> (any SpeechInput, () -> Void)
    private var pause: (() -> Void)?
    private var input: (any SpeechInput)?
    private var task: Task<Void, Never>?
    private var final = false
    private var session = UUID()
    private var stopTask: Task<Void, Never>?
    private var terms = Vocabulary.terms(Vocabulary.base)

    public init() {
        #if DEBUG
        if CommandLine.arguments.contains("--verify-fixture"), CommandLine.arguments.contains("--verify-dictation") {
            permission = { false }
            makeInput = { (DictationFixture(), {}) }
            return
        }
        #endif
        permission = Self.isDenied
        makeInput = {
            let audio = VoiceAudio(duplex: false)
            return (AppleInput(audio: audio), { audio.stop() })
        }
    }

    public init(makeInput: @escaping () -> any SpeechInput, pause: @escaping () -> Void = {},
         denied: @escaping @Sendable () -> Bool = DictationInput.isDenied) {
        self.makeInput = { (makeInput(), pause) }
        permission = denied
    }

    /// Names the recogniser should hear correctly while dictating: agent and chat names.
    public func setVocabulary(_ names: [String]) {
        terms = Vocabulary.terms(Vocabulary.base + names)
        let current = terms
        if let input { Task { await input.setVocabulary(current) } }
    }

    public func refresh() async {
        let check = permission
        denied = await Task.detached { check() }.value
    }

    public func start(transcript: @escaping (VoiceTranscript) -> Void, failure: @escaping (Error) -> Void) {
        guard !active, !stopping else { return }
        active = true
        final = false
        ready = false
        session = UUID()
        let current = session
        let (input, pause) = makeInput()
        self.pause = pause
        self.input = input
        let startTerms = terms
        task = Task {
            do {
                await input.setVocabulary(startTerms)
                let stream = try await input.start()
                try Task.checkCancellation()
                guard session == current else { return }
                ready = true
                denied = false
                for try await event in stream {
                    guard !Task.isCancelled, session == current else { break }
                    switch event {
                    case .transcript(let value):
                        final = value.final
                        transcript(value)
                    case .level(let value): level = value
                    case .activity: break
                    }
                }
            } catch {
                if !Task.isCancelled, session == current {
                    await refresh()
                    if !Task.isCancelled, session == current { failure(error) }
                }
            }
            await input.stop()
            guard session == current else { return }
            active = false
            ready = false
            level = 0
        }
    }

    public func stop() async {
        if let stopTask { await stopTask.value; return }
        guard active else { return }
        let pending = Task { await finish() }
        stopTask = pending
        await pending.value
        stopTask = nil
        stopping = false
    }

    private func finish() async {
        stopping = true
        if let input {
            pause?()
            if ready {
                // Bound finalization so Send also works when recognition stalls.
                final = false
                let finalization = Task { try? await input.finalize() }
                let clock = ContinuousClock()
                let deadline = clock.now.advanced(by: .seconds(1))
                while !final && active && clock.now < deadline {
                    let next = min(deadline, clock.now.advanced(by: .milliseconds(50)))
                    try? await clock.sleep(until: next)
                }
                finalization.cancel()
            }
            session = UUID()
            task?.cancel()
            await input.stop()
            // A system permission prompt cannot be cancelled or awaited here.
        }
        input = nil
        pause = nil
        task = nil
        active = false
        ready = false
        level = 0
    }
}

#if DEBUG
@MainActor
private final class DictationFixture: SpeechInput {
    private var events: AsyncThrowingStream<VoiceEvent, Error>.Continuation?
    private var task: Task<Void, Never>?

    func start() async throws -> AsyncThrowingStream<VoiceEvent, Error> {
        try await Task.sleep(for: .seconds(3))
        let stream = AsyncThrowingStream<VoiceEvent, Error>.makeStream()
        events = stream.continuation
        task = Task {
            let levels: [Float] = [0, 0, 0.03, 0.08, 0.2, 0.5, 0.3, 0.1, 0.04, 0, 0.05, 0.15, 0.4, 0.8, 0.2, 0]
            // One segment, revised then closed, as the recogniser reports a sentence with a hesitation.
            let heard: [Int: (text: String, end: Double, final: Bool)] = [
                2: ("Let's, um,", 1.2, false),
                6: ("Let's, um, ship it", 2.4, true)
            ]
            var index = 0
            while !Task.isCancelled {
                stream.continuation.yield(.level(levels[index % levels.count]))
                if let sentence = heard[index] {
                    stream.continuation.yield(.transcript(.init(text: sentence.text, start: 1, end: sentence.end,
                                                                final: sentence.final)))
                }
                index += 1
                do { try await Task.sleep(for: .milliseconds(120)) }
                catch { break }
            }
        }
        return stream.stream
    }

    func finalize() async throws {
        events?.yield(.transcript(.init(text: "", start: 0, end: 0, final: true)))
    }

    public func stop() async {
        task?.cancel()
        task = nil
        events?.finish()
        events = nil
    }

    func setMuted(_ muted: Bool) {}
}
#endif
