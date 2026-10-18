import Foundation
import Testing
import PolymuxKit
@testable import PolymuxVoice

@MainActor
private final class Input: SpeechInput {
    let events = AsyncThrowingStream<VoiceEvent, Error>.makeStream()
    func start() async throws -> AsyncThrowingStream<VoiceEvent, Error> { events.stream }
    func stop() async { events.continuation.finish() }
    func setMuted(_ muted: Bool) {}
}

@MainActor
private final class Output: SpeechOutput {
    var firstAudio: (() -> Void)?
    var spoken: [String] = []
    private var completion: CheckedContinuation<Void, Error>?

    func speak(_ text: String) async throws {
        spoken.append(text)
        firstAudio?()
        try await withCheckedThrowingContinuation { completion = $0 }
    }

    func finish() { completion?.resume(); completion = nil }
    func stop() { completion?.resume(throwing: CancellationError()); completion = nil }
}

@MainActor
private final class Reply: VoiceReply {
    let response = AsyncThrowingStream<VoiceFragment, Error>.makeStream()
    func preview(_ text: String) {}
    func stream(_ text: String) -> AsyncThrowingStream<VoiceFragment, Error> { response.stream }
    func stop() async { response.continuation.finish(throwing: CancellationError()) }
}

@Suite @MainActor
struct VoiceTests {
    private func wait(_ condition: () -> Bool) async throws {
        for _ in 0..<200 {
            if condition() { return }
            try await Task.sleep(for: .milliseconds(5))
        }
        Issue.record("Voice state did not settle")
        throw CancellationError()
    }

    private func begin() async throws -> (VoiceStore, Output, Reply) {
        let input = Input(), output = Output(), reply = Reply()
        let store = VoiceStore(input: input, output: output, reply: reply)
        await store.start()
        input.events.continuation.yield(.transcript(VoiceTranscript(text: "Task", start: 0, end: 1, final: true)))
        try await wait { store.transcript == "Task" }
        store.submit()
        return (store, output, reply)
    }

    @Test func stopClearsSpeakingAuthor() async throws {
        let (store, output, reply) = try await begin()
        reply.response.continuation.yield(VoiceFragment(text: "Hello.", agentID: "lead"))
        try await wait { store.phase == .speaking && output.spoken == ["Hello."] }
        #expect(store.agentID == "lead")
        await store.stop()
        #expect(store.phase == .interrupted)
        #expect(store.agentID == nil)
        await Task.yield()
        #expect(store.phase != .speaking)
    }

    @Test func authorFollowsPlaybackAndSilentGaps() async throws {
        let (store, output, reply) = try await begin()
        reply.response.continuation.yield(VoiceFragment(text: "First.", agentID: "lead"))
        try await wait { output.spoken == ["First."] }
        #expect(store.agentID == "lead")
        output.finish()
        try await wait { store.phase == .thinking && store.agentID == nil }
        reply.response.continuation.yield(VoiceFragment(text: "Second.", agentID: "peer"))
        try await wait { output.spoken == ["First.", "Second."] }
        #expect(store.agentID == "peer")
        await store.stop()
    }

    @Test func interruptionClearsSpeakingAuthor() async throws {
        let (store, output, reply) = try await begin()
        reply.response.continuation.yield(VoiceFragment(text: "Hello.", agentID: "lead"))
        try await wait { output.spoken == ["Hello."] }
        store.interrupt()
        #expect(store.agentID == nil)
        #expect(store.phase != .speaking)
        await store.stop()
    }
}
