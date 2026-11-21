import Testing
@testable import SpeechKit

@MainActor
struct DictationInputTests {
    @Test func stopDeliversEveryFinal() async throws {
        let speech = FinishingSpeech()
        let input = DictationInput(makeInput: { _ in speech }, denied: { false })
        var text: [String] = []
        input.start { text.append($0.text) } failure: { Issue.record($0) }
        try await waitReady(input)
        await input.stop()
        #expect(text == ["First sentence.", "Last words."])
        #expect(!input.active)
    }

    @Test func concurrentStopsRecoverFromStalledSpeech() async throws {
        let speech = FinishingSpeech(stalls: true)
        let input = DictationInput(makeInput: { _ in speech }, denied: { false })
        var text: [String] = []
        input.start { text.append($0.text) } failure: { Issue.record($0) }
        try await waitReady(input)
        let started = ContinuousClock.now
        async let first: Void = input.stop()
        async let second: Void = input.stop()
        _ = await (first, second)
        #expect(started.duration(to: .now) < .seconds(2))
        #expect(text == ["First sentence."])
        #expect(speech.stops == 1)
        #expect(!input.active && !input.stopping)
    }

    private func waitReady(_ input: DictationInput) async throws {
        for _ in 0..<100 where !input.ready {
            try await Task.sleep(for: .milliseconds(10))
        }
        try #require(input.ready)
    }
}

@MainActor
private final class FinishingSpeech: SpeechInput {
    private let events = AsyncThrowingStream<VoiceEvent, Error>.makeStream()
    private let stalls: Bool
    private(set) var stops = 0

    init(stalls: Bool = false) { self.stalls = stalls }

    func start() async throws -> AsyncThrowingStream<VoiceEvent, Error> { events.stream }
    func stop() async {
        stops += 1
        events.continuation.finish()
    }
    func setMuted(_ muted: Bool) {}

    func finalize() async throws {
        events.continuation.yield(.transcript(.init(text: "First sentence.", start: 0, end: 1, final: true)))
        if stalls { return }
        try await Task.sleep(for: .milliseconds(150))
        events.continuation.yield(.transcript(.init(text: "Last words.", start: 1, end: 2, final: true)))
        events.continuation.finish()
    }
}
