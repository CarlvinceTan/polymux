import SwiftUI
import AVFoundation
import Speech

@MainActor @Observable
final class DictationInput {
    private(set) var active = false
    private(set) var stopping = false
    private(set) var ready = false
    private(set) var level: Float = 0
    private(set) var denied = false
    private let permission: @Sendable () -> Bool

    private nonisolated static func isDenied() -> Bool {
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

    init() {
        #if DEBUG
        if VerifyFixture.enabled, CommandLine.arguments.contains("--verify-dictation") {
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

    init(makeInput: @escaping () -> any SpeechInput, pause: @escaping () -> Void = {},
         denied: @escaping @Sendable () -> Bool = DictationInput.isDenied) {
        self.makeInput = { (makeInput(), pause) }
        permission = denied
    }

    func refresh() async {
        let check = permission
        denied = await Task.detached { check() }.value
    }

    func start(transcript: @escaping (VoiceTranscript) -> Void, failure: @escaping (Error) -> Void) {
        guard !active, !stopping else { return }
        active = true
        final = false
        ready = false
        session = UUID()
        let current = session
        let (input, pause) = makeInput()
        self.pause = pause
        self.input = input
        task = Task {
            do {
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

    func stop() async {
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
            var index = 0
            while !Task.isCancelled {
                stream.continuation.yield(.level(levels[index % levels.count]))
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

    func stop() async {
        task?.cancel()
        task = nil
        events?.finish()
        events = nil
    }

    func setMuted(_ muted: Bool) {}
}
#endif
