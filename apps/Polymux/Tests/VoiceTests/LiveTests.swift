import Foundation
import Testing
import PolymuxKit
@testable import PolymuxVoice

@MainActor
private final class ProviderProbe: LiveProvider {
    let inputRate = 16000
    var events: AsyncThrowingStream<LiveEvent, Error>.Continuation?
    var options: LiveOptions?
    var audio: [LivePCM] = []
    var results: [LiveResult] = []
    var endings = 0
    var stopped = 0
    var failStart = false

    func start(options: LiveOptions, key: String) async throws -> AsyncThrowingStream<LiveEvent, Error> {
        self.options = options
        if failStart { throw LiveFailure.rejected }
        let stream = AsyncThrowingStream<LiveEvent, Error>.makeStream()
        events = stream.continuation
        return stream.stream
    }
    func sendAudio(_ chunk: LivePCM) async throws { audio.append(chunk) }
    func endAudio() async throws { endings += 1 }
    func respond(_ result: LiveResult) async throws { results.append(result) }
    func stop() async { stopped += 1; events?.finish(); events = nil }
}

@MainActor
private final class SoundProbe: LiveSound {
    var playing = false
    var drained: (() -> Void)?
    var events: AsyncThrowingStream<MicEvent, Error>.Continuation?
    var chunks: [LivePCM] = []
    var rate = 0
    var stopped = 0
    var muted = false
    var interruptions = 0
    var trackEvents = false
    let processed = AsyncStream<MicEvent>.makeStream()
    private var iterator: AsyncThrowingStream<MicEvent, Error>.Iterator?
    private var previous: MicEvent?

    func start(rate: Int) async throws -> AsyncThrowingStream<MicEvent, Error> {
        self.rate = rate
        let stream = AsyncThrowingStream<MicEvent, Error>.makeStream()
        events = stream.continuation
        if trackEvents {
            iterator = stream.stream.makeAsyncIterator()
            return AsyncThrowingStream(unfolding: { try await self.nextEvent() })
        }
        return stream.stream
    }
    private func nextEvent() async throws -> MicEvent? {
        // Requesting the next event acknowledges that the send loop handled the previous one.
        if let previous { processed.continuation.yield(previous) }
        guard var iterator else { return nil }
        let event = try await iterator.next(isolation: MainActor.shared)
        self.iterator = iterator
        previous = event
        return event
    }
    func setMuted(_ muted: Bool) { self.muted = muted; if muted { events?.yield(.end) } }
    func play(_ chunk: LivePCM) throws { chunks.append(chunk); playing = true }
    func interrupt() { playing = false; interruptions += 1 }
    func stop() { stopped += 1; interrupt(); events?.finish(); events = nil }
    func drain() { playing = false; drained?() }
}

@MainActor
private final class ReplyProbe: VoiceReply {
    var texts: [String] = []
    var pending: AsyncThrowingStream<VoiceFragment, Error>.Continuation?
    var stopped = 0
    func preview(_ text: String) {}
    func stream(_ text: String) -> AsyncThrowingStream<VoiceFragment, Error> {
        texts.append(text)
        let stream = AsyncThrowingStream<VoiceFragment, Error>.makeStream()
        pending = stream.continuation
        return stream.stream
    }
    func stop() async { stopped += 1; pending?.finish(throwing: CancellationError()); pending = nil }
}

@MainActor
private struct LiveRig {
    let provider = ProviderProbe()
    let sound = SoundProbe()
    let reply = ReplyProbe()
    let store: LiveStore

    init() {
        let provider = provider
        store = LiveStore(options: .init(model: "fake-live", voice: "fake-voice", instructions: "Test"),
                          reply: reply, sound: sound, makeProvider: { provider }, key: { "placeholder" })
    }
}

@MainActor
private func settle(_ condition: () -> Bool) async {
    // Fixed yields are not a wall-clock wait: under build load the store's main-actor tasks
    // can miss the window, so poll on a deadline instead.
    let deadline = ContinuousClock.now.advanced(by: .seconds(2))
    while !condition() {
        guard !Task.isCancelled, ContinuousClock.now < deadline else { break }
        try? await Task.sleep(for: .milliseconds(2))
    }
    #expect(condition())
}

private func call(_ id: String = "one", name: String = "ask_agent") -> LiveCall {
    LiveCall(id: id, name: name, arguments: Data(#"{"text":"Find the notes"}"#.utf8))
}

@Test @MainActor func liveRunsOnClientWithProviderNeutralAudio() async {
    let rig = LiveRig()
    await rig.store.start()
    #expect(rig.store.phase == .listening)
    #expect(rig.sound.rate == rig.provider.inputRate)
    #expect(rig.provider.options?.model == "fake-live")
    #expect(rig.provider.options?.tools.first?.name == "ask_agent")
    let pcm = LivePCM(data: Data([1, 0]), rate: 16000)
    rig.sound.events?.yield(.frame(pcm, level: 0.4))
    await settle { rig.provider.audio.count == 1 }
    #expect(rig.store.level == 0.4)
    rig.provider.events?.yield(.audio(.init(data: Data([2, 0]), rate: 24000)))
    await settle { rig.sound.chunks.count == 1 }
    #expect(rig.store.phase == .speaking)
    #expect(rig.reply.texts.isEmpty)
    await rig.store.stop()
    #expect(rig.reply.stopped == 0)
}

@Test @MainActor func liveMuteStopsSendingAndEndsProviderAudio() async {
    let rig = LiveRig()
    rig.sound.trackEvents = true
    var processed = rig.sound.processed.stream.makeAsyncIterator()
    await rig.store.start()
    rig.store.toggleMute()
    rig.sound.events?.yield(.frame(.init(data: Data([1, 0]), rate: 16000), level: 1))
    while let event = await processed.next() {
        if case .frame = event { break }
    }
    #expect(rig.provider.endings == 1)
    #expect(rig.provider.audio.isEmpty)
    #expect(rig.store.muted)
    #expect(rig.store.level == 0)
    rig.store.toggleMute()
    rig.sound.events?.yield(.frame(.init(data: Data([1, 0]), rate: 16000), level: 0.5))
    while let event = await processed.next() {
        if case .frame = event { break }
    }
    #expect(rig.provider.audio.count == 1)
    await rig.store.stop()
}

@Test @MainActor func liveWaitsForPlaybackAndBackgroundReasoning() async {
    let rig = LiveRig()
    await rig.store.start()
    rig.provider.events?.yield(.response("Hello"))
    rig.provider.events?.yield(.audio(.init(data: Data([1, 0]), rate: 24000)))
    rig.provider.events?.yield(.turnComplete(idle: false))
    await settle { rig.sound.playing }
    rig.sound.drain()
    #expect(rig.store.phase == .thinking)
    rig.provider.events?.yield(.response(" again"))
    rig.provider.events?.yield(.turnComplete(idle: true))
    await settle { rig.store.phase == .listening }
    #expect(rig.store.response == "Hello again")
    await rig.store.stop()
}

@Test @MainActor func liveTranscriptionDoesNotResetInterleavedResponse() async {
    let rig = LiveRig()
    await rig.store.start()
    rig.provider.events?.yield(.response("Hello"))
    rig.provider.events?.yield(.transcript("Hi"))
    rig.provider.events?.yield(.response(" there"))
    await settle { rig.store.response == "Hello there" }
    #expect(rig.store.transcript == "Hi")
    await rig.store.stop()
}

@Test @MainActor func liveDelegatesOnceAndReturnsResult() async {
    let rig = LiveRig()
    await rig.store.start()
    rig.provider.events?.yield(.calls([call(), call()]))
    await settle { rig.reply.texts.count == 1 }
    rig.reply.pending?.yield(.init(text: "Found "))
    rig.reply.pending?.yield(.init(text: "the notes"))
    rig.reply.pending?.finish()
    await settle { rig.provider.results.count == 1 }
    #expect(rig.provider.results.first?.text == "Found the notes")
    #expect(rig.provider.results.first?.call.id == "one")
    #expect(rig.provider.results.first?.failed == false)
    await rig.store.stop()
}

@Test @MainActor func liveSpeechInterruptionDoesNotCancelAgent() async {
    let rig = LiveRig()
    await rig.store.start()
    rig.provider.events?.yield(.calls([call()]))
    await settle { rig.reply.texts.count == 1 }
    rig.provider.events?.yield(.audio(.init(data: Data([1, 0]), rate: 24000)))
    rig.provider.events?.yield(.interrupted)
    await settle { rig.sound.interruptions == 1 }
    #expect(rig.reply.stopped == 0)
    rig.reply.pending?.yield(.init(text: "Finished"))
    rig.reply.pending?.finish()
    await settle { rig.provider.results.count == 1 }
    await rig.store.stop()
}

@Test @MainActor func liveCancelsOnlyMatchingDelegation() async {
    let rig = LiveRig()
    await rig.store.start()
    rig.provider.events?.yield(.calls([call()]))
    await settle { rig.reply.texts.count == 1 }
    rig.provider.events?.yield(.cancel(["other"]))
    rig.provider.events?.yield(.usage(.init(input: 1, output: 1, total: 2)))
    await settle { rig.store.usage != nil }
    #expect(rig.reply.stopped == 0)
    rig.provider.events?.yield(.cancel(["one"]))
    await settle { rig.reply.stopped == 1 }
    #expect(rig.provider.results.isEmpty)
    await rig.store.stop()
}

@Test @MainActor func liveRejectsParallelAndUnknownTools() async {
    let rig = LiveRig()
    await rig.store.start()
    rig.provider.events?.yield(.calls([call(), call("two"), call("three", name: "shell")]))
    await settle { rig.provider.results.count == 2 && rig.reply.texts.count == 1 }
    #expect(rig.provider.results.allSatisfy { $0.failed })
    #expect(rig.reply.texts.count == 1)
    await rig.store.stop()
    #expect(rig.reply.stopped == 1)
}

@Test @MainActor func liveProviderFailureStopsAudioAndOwnedWork() async {
    let rig = LiveRig()
    await rig.store.start()
    rig.provider.events?.yield(.calls([call()]))
    await settle { rig.reply.texts.count == 1 }
    rig.provider.events?.finish(throwing: LiveFailure.connection)
    await settle { rig.store.phase == .failed && rig.reply.stopped == 1 }
    #expect(rig.store.issue != nil)
    #expect(rig.sound.stopped == 1)
    #expect(rig.provider.stopped == 1)
    await rig.store.stop()
}

@Test @MainActor func liveFailedSetupCanRetryWithoutFallback() async {
    let rig = LiveRig()
    rig.provider.failStart = true
    await rig.store.start()
    #expect(rig.store.phase == .failed)
    #expect(rig.sound.rate == 0)
    #expect(rig.reply.texts.isEmpty)
    rig.provider.failStart = false
    await rig.store.retry()
    #expect(rig.store.phase == .listening)
    #expect(rig.store.issue == nil)
    await rig.store.stop()
}

@Test @MainActor func voicePreferencesComeFromServer() throws {
    var settings = Settings(voiceMode: "pipeline", liveProvider: "gemini", liveModel: "server-live",
                            liveVoice: "Kore", textProvider: "gemini", textModel: "server-text",
                            voicePrompt: "Represent {target}.", speechKeys: ["gemini"])
    #expect(VoiceConfig(settings: settings).mode == .pipeline)
    #expect(VoiceConfig(settings: settings).model == "server-text")
    settings.voiceMode = "live"
    let config = VoiceConfig(settings: settings)
    #expect(config.mode == .live)
    #expect(config.model == "server-live")
    #expect(config.voice == "Kore")
    #expect(config.provider == "gemini")
    #expect(config.hasKey)
    #expect(config.prompt == "Represent {target}.")
}
