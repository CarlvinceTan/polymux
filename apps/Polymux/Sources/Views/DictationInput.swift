import SwiftUI
import AVFoundation
import Speech

@MainActor @Observable
final class DictationInput {
    private(set) var active = false
    private(set) var stopping = false
    private(set) var level: Float = 0
    private let permission: @MainActor () -> Bool
    var denied: Bool { permission() }
    var microphoneDenied: Bool { AVAudioApplication.shared.recordPermission == .denied }

    private static func isDenied() -> Bool {
        AVAudioApplication.shared.recordPermission == .denied
            || [.denied, .restricted].contains(SFSpeechRecognizer.authorizationStatus())
    }
    private let makeInput: () -> (any SpeechInput, () -> Void)
    private var pause: (() -> Void)?
    private var input: (any SpeechInput)?
    private var task: Task<Void, Never>?
    private var final = false
    private var ready = false
    private var session = UUID()
    private var stopTask: Task<Void, Never>?

    init() {
        permission = Self.isDenied
        makeInput = {
            let audio = VoiceAudio()
            return (AppleInput(audio: audio), { audio.stop() })
        }
    }

    init(makeInput: @escaping () -> any SpeechInput, pause: @escaping () -> Void = {},
         denied: @escaping @MainActor () -> Bool = DictationInput.isDenied) {
        self.makeInput = { (makeInput(), pause) }
        permission = denied
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
                if !Task.isCancelled, session == current { failure(error) }
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
