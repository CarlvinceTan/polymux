import Foundation
import Testing
@testable import PolymuxKit

@MainActor
private final class SocketProbe: LiveSocket {
    var sent: [Data] = []
    var incoming: [Data] = []
    var waiting: CheckedContinuation<Data, Error>?
    var closed = false
    var failSend = false
    var closeReason: String?

    func send(_ data: Data) async throws {
        if failSend { throw URLError(.badServerResponse, userInfo: [NSURLErrorFailingURLErrorKey: URL(string: "wss://example.test?key=secret-placeholder")!]) }
        sent.append(data)
    }

    func receive() async throws -> Data {
        if !incoming.isEmpty { return incoming.removeFirst() }
        if closed { throw URLError(.cancelled) }
        return try await withCheckedThrowingContinuation { waiting = $0 }
    }

    func push(_ json: String) {
        let data = Data(json.utf8)
        if let waiting { self.waiting = nil; waiting.resume(returning: data) }
        else { incoming.append(data) }
    }

    func close() {
        closed = true
        waiting?.resume(throwing: URLError(.cancelled))
        waiting = nil
    }
}

@MainActor
private func options() -> LiveOptions {
    LiveOptions(model: "gemini-3.8-live", voice: "Aoede", instructions: "Speak briefly.", tools: [
        LiveTool(name: "ask_agent", description: "Delegate a task", parameters: Data(#"{"type":"object"}"#.utf8))
    ])
}

@Test @MainActor func geminiSetupUsesNativeAudioAndBoundedHistory() throws {
    let data = try GeminiLive.setup(options())
    let root = try #require(JSONSerialization.jsonObject(with: data) as? [String: Any])
    let setup = try #require(root["setup"] as? [String: Any])
    #expect(setup["model"] as? String == "models/gemini-3.8-live")
    let generation = try #require(setup["generationConfig"] as? [String: Any])
    #expect(generation["responseModalities"] as? [String] == ["AUDIO"])
    #expect(generation["thinkingConfig"] == nil)
    #expect(setup["inputAudioTranscription"] != nil)
    #expect(setup["outputAudioTranscription"] != nil)
    let compression = try #require(setup["contextWindowCompression"] as? [String: Any])
    #expect(compression["triggerTokens"] as? Int == 16000)
    let tools = try #require(setup["tools"] as? [[String: Any]])
    let declarations = try #require(tools.first?["functionDeclarations"] as? [[String: Any]])
    #expect(declarations.first?["behavior"] as? String == "NON_BLOCKING")
    #expect((declarations.first?["parametersJsonSchema"] as? [String: String])?["type"] == "object")
}

@Test @MainActor func geminiDecodesAudioAndTranscriptionsTogether() throws {
    let events = try GeminiLive.decode(Data(#"{"serverContent":{"inputTranscription":{"text":"Hello"},"outputTranscription":{"text":"Hi"},"modelTurn":{"parts":[{"inlineData":{"mimeType":"audio/pcm;rate=24000","data":"AQACAA=="}}]},"turnComplete":true,"interactionStatus":"IN_PROGRESS"},"usageMetadata":{"promptTokenCount":10,"responseTokenCount":4,"totalTokenCount":14}}"#.utf8))
    #expect(events == [.transcript("Hello"), .response("Hi"), .audio(.init(data: Data([1, 0, 2, 0]), rate: 24000)),
                       .turnComplete(idle: false), .usage(.init(input: 10, output: 4, total: 14))])
}

@Test @MainActor func geminiInterruptionDiscardsStaleAudio() throws {
    let events = try GeminiLive.decode(Data(#"{"serverContent":{"interrupted":true,"modelTurn":{"parts":[{"inlineData":{"mimeType":"audio/pcm;rate=24000","data":"AQACAA=="}}]},"turnComplete":true,"interactionStatus":"IDLE"}}"#.utf8))
    #expect(events == [.interrupted, .turnComplete(idle: true)])
}

@Test @MainActor func geminiDecodesCallsAndTargetedCancellation() throws {
    let events = try GeminiLive.decode(Data(#"{"toolCall":{"functionCalls":[{"id":"job-1","name":"ask_agent","args":{"text":"Find my notes"}}]},"toolCallCancellation":{"ids":["job-0"]}}"#.utf8))
    guard case .calls(let calls) = events.first else { Issue.record("Missing call"); return }
    #expect(calls.first?.id == "job-1")
    #expect(calls.first?.name == "ask_agent")
    let arguments = try #require(calls.first?.arguments)
    let args = try #require(JSONSerialization.jsonObject(with: arguments) as? [String: String])
    #expect(args["text"] == "Find my notes")
    #expect(events.last == .cancel(["job-0"]))
}

@Test @MainActor func geminiRejectsMalformedAudio() throws {
    #expect(throws: LiveFailure.self) {
        try GeminiLive.decode(Data(#"{"serverContent":{"modelTurn":{"parts":[{"inlineData":{"mimeType":"audio/pcm;rate=24000","data":"AQ=="}}]}}}"#.utf8))
    }
    #expect(throws: LiveFailure.self) { try GeminiLive.decode(Data("not-json".utf8)) }
    #expect(throws: LiveFailure.self) { try GeminiLive.decode(Data(#"{"error":{"message":"private-provider-details"}}"#.utf8)) }
    #expect(throws: LiveFailure.self) { try GeminiLive.decode(Data(#"{"goAway":{"timeLeft":"10s"}}"#.utf8)) }
}

@Test @MainActor func geminiHandshakeAudioAndToolResult() async throws {
    let socket = SocketProbe()
    socket.push(#"{"setupComplete":{}}"#)
    let provider = GeminiLive(makeSocket: { _ in socket })
    let stream = try await provider.start(options: options(), key: "placeholder")
    try await provider.sendAudio(.init(data: Data([1, 0]), rate: 16000))
    try await provider.endAudio()
    let call = LiveCall(id: "job-1", name: "ask_agent", arguments: Data())
    try await provider.respond(.init(call: call, text: "Done"))
    let audio = try #require(JSONSerialization.jsonObject(with: socket.sent[1]) as? [String: Any])
    let input = try #require(audio["realtimeInput"] as? [String: Any])
    #expect((input["audio"] as? [String: String])?["mimeType"] == "audio/pcm;rate=16000")
    let result = try #require(JSONSerialization.jsonObject(with: socket.sent[3]) as? [String: Any])
    let responses = (result["toolResponse"] as? [String: Any])?["functionResponses"] as? [[String: Any]]
    #expect(responses?.first?["id"] as? String == "job-1")
    #expect((responses?.first?["response"] as? [String: String])?["scheduling"] == "WHEN_IDLE")
    socket.push(#"{"serverContent":{"outputTranscription":{"text":"Done"}}}"#)
    var iterator = stream.makeAsyncIterator()
    #expect(try await iterator.next() == .response("Done"))
    await provider.stop()
    #expect(socket.closed)
    #expect(try await iterator.next() == nil)
}

@Test @MainActor func geminiMissingKeyDoesNotOpenSocket() async {
    var opened = false
    let provider = GeminiLive(makeSocket: { _ in opened = true; return SocketProbe() })
    await #expect(throws: LiveFailure.self) { try await provider.start(options: options(), key: " ") }
    #expect(!opened)
}

@Test @MainActor func geminiErrorsNeverExposeAuthenticatedURL() async {
    let socket = SocketProbe()
    socket.failSend = true
    let provider = GeminiLive(makeSocket: { _ in socket })
    do {
        _ = try await provider.start(options: options(), key: "placeholder")
        Issue.record("Expected failure")
    } catch {
        #expect(!error.localizedDescription.contains("secret-placeholder"))
        #expect(!error.localizedDescription.contains("wss://"))
        #expect(socket.closed)
    }
}

@Test @MainActor func geminiStopCancelsPendingHandshake() async {
    let socket = SocketProbe()
    let provider = GeminiLive(makeSocket: { _ in socket })
    let opening = Task { try await provider.start(options: options(), key: "placeholder") }
    for _ in 0..<100 where socket.sent.isEmpty { await Task.yield() }
    #expect(!socket.sent.isEmpty)
    await provider.stop()
    _ = await opening.result
    #expect(socket.closed)
}

@Test @MainActor func geminiStopCancelsPendingCredential() async throws {
    let socket = SocketProbe()
    let waiting = AsyncStream<Void>.makeStream()
    var resume: CheckedContinuation<any LiveSocket, Never>?
    let provider = GeminiLive(makeSocket: { _ in
        await withCheckedContinuation {
            resume = $0
            waiting.continuation.yield(())
        }
    })
    let opening = Task { try await provider.start(options: options(), key: "placeholder") }
    var iterator = waiting.stream.makeAsyncIterator()
    await iterator.next()
    await provider.stop()
    try #require(resume).resume(returning: socket)
    await #expect(throws: CancellationError.self) { try await opening.value }
    #expect(socket.closed)
    #expect(socket.sent.isEmpty)
}

@Test @MainActor func geminiReportsDepletedCreditsWithoutRawProviderDetails() async {
    let socket = SocketProbe()
    socket.failSend = true
    socket.closeReason = "Your prepayment credits are depleted. private-provider-details"
    let provider = GeminiLive(makeSocket: { _ in socket })
    do {
        _ = try await provider.start(options: options(), key: "placeholder")
        Issue.record("Expected depleted credits")
    } catch {
        #expect(error.localizedDescription.contains("credits are depleted"))
        #expect(!error.localizedDescription.contains("private-provider-details"))
    }
}
