import Foundation

@MainActor
public final class GeminiLive: LiveProvider {
    public let inputRate = 16_000
    private let makeSocket: (String) async throws -> any LiveSocket
    private let requiresKey: Bool
    private var socket: (any LiveSocket)?
    private var reader: Task<Void, Never>?
    private var events: AsyncThrowingStream<LiveEvent, Error>.Continuation?
    private var token = UUID()

    public convenience init() {
        self.init { key in
            var url = URLComponents()
            url.scheme = "wss"
            url.host = "generativelanguage.googleapis.com"
            url.path = "/ws/google.ai.generativelanguage.v1beta.GenerativeService.BidiGenerateContent"
            url.queryItems = [URLQueryItem(name: "key", value: key)]
            guard let endpoint = url.url else { throw LiveFailure.configuration }
            return LiveWebSocket(url: endpoint)
        }
    }

    public convenience init(client: APIClient) {
        self.init(requiresKey: false) { _ in LiveWebSocket(url: try client.speechSocket(), token: await client.token) }
    }

    init(requiresKey: Bool = true, makeSocket: @escaping (String) async throws -> any LiveSocket) {
        self.requiresKey = requiresKey
        self.makeSocket = makeSocket
    }

    public func start(options: LiveOptions, key: String) async throws -> AsyncThrowingStream<LiveEvent, Error> {
        await stop()
        guard !requiresKey || !key.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { throw LiveFailure.missingKey }
        let setup = try Self.setup(options)
        let current = UUID()
        token = current
        let socket = try await makeSocket(key)
        guard token == current, !Task.isCancelled else { socket.close(); throw CancellationError() }
        self.socket = socket
        let timeout = Task {
            try? await Task.sleep(for: .seconds(20))
            guard !Task.isCancelled, self.token == current else { return }
            socket.close()
        }
        defer { timeout.cancel() }
        do {
            try await withTaskCancellationHandler {
                try await socket.send(setup)
                let message = try Self.object(await socket.receive())
                guard message["setupComplete"] != nil else { throw LiveFailure.rejected }
            } onCancel: {
                Task { @MainActor in socket.close() }
            }
            try Task.checkCancellation()
            guard token == current else { throw CancellationError() }
            let stream = AsyncThrowingStream<LiveEvent, Error>.makeStream(bufferingPolicy: .bufferingOldest(512))
            events = stream.continuation
            stream.continuation.onTermination = { [weak self] _ in
                Task { @MainActor in
                    guard self?.token == current else { return }
                    await self?.stop()
                }
            }
            reader = Task { [weak self] in
                do {
                    while !Task.isCancelled {
                        let data = try await socket.receive()
                        guard let self, self.token == current else { return }
                        for event in try Self.decode(data) {
                            if case .dropped = stream.continuation.yield(event) { throw LiveFailure.backlog }
                        }
                    }
                } catch {
                    guard !Task.isCancelled, self?.token == current else { return }
                    // Transport errors can contain the authenticated URL.
                    let failure = Self.failure(error, socket: socket)
                    stream.continuation.finish(throwing: failure)
                    socket.close()
                }
            }
            return stream.stream
        } catch {
            let failure = Self.failure(error, socket: socket)
            socket.close()
            if token == current { self.socket = nil }
            if error is CancellationError || Task.isCancelled { throw CancellationError() }
            throw failure
        }
    }

    public func sendAudio(_ chunk: LivePCM) async throws {
        guard chunk.rate == inputRate, !chunk.data.isEmpty, chunk.data.count.isMultiple(of: 2) else {
            throw LiveFailure.configuration
        }
        try await send(["realtimeInput": ["audio": [
            "mimeType": "audio/pcm;rate=\(inputRate)", "data": chunk.data.base64EncodedString()
        ]]])
    }

    public func endAudio() async throws {
        try await send(["realtimeInput": ["audioStreamEnd": true]])
    }

    public func respond(_ result: LiveResult) async throws {
        try await send(["toolResponse": ["functionResponses": [[
            "id": result.call.id,
            "name": result.call.name,
            "response": [result.failed ? "error" : "result": result.text, "scheduling": "WHEN_IDLE"]
        ]]]])
    }

    public func stop() async {
        token = UUID()
        reader?.cancel()
        reader = nil
        socket?.close()
        socket = nil
        events?.finish()
        events = nil
    }

    private func send(_ message: [String: Any]) async throws {
        guard let socket else { throw LiveFailure.ended }
        let data = try JSONSerialization.data(withJSONObject: message)
        do { try await socket.send(data) }
        catch {
            if Task.isCancelled { throw CancellationError() }
            throw Self.failure(error, socket: socket)
        }
    }

    private static func failure(_ error: Error, socket: any LiveSocket) -> LiveFailure {
        let reason = socket.closeReason?.lowercased() ?? ""
        if reason.contains("credits") && reason.contains("depleted") { return .credits }
        return error as? LiveFailure ?? .connection
    }

    static func setup(_ options: LiveOptions) throws -> Data {
        let model = options.model.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !model.isEmpty, !options.voice.isEmpty else { throw LiveFailure.configuration }
        let declarations: [[String: Any]] = try options.tools.map { tool in
            ["name": tool.name, "description": tool.description, "parametersJsonSchema": try object(tool.parameters),
             "behavior": "NON_BLOCKING"]
        }
        return try JSONSerialization.data(withJSONObject: ["setup": [
            "model": model.hasPrefix("models/") ? model : "models/\(model)",
            "generationConfig": [
                "responseModalities": ["AUDIO"],
                "speechConfig": ["voiceConfig": ["prebuiltVoiceConfig": ["voiceName": options.voice]]]
            ],
            "systemInstruction": ["parts": [["text": options.instructions]]],
            "inputAudioTranscription": [:],
            "outputAudioTranscription": [:],
            "realtimeInputConfig": [
                "automaticActivityDetection": ["silenceDurationMs": 500, "prefixPaddingMs": 100],
                "activityHandling": "START_OF_ACTIVITY_INTERRUPTS"
            ],
            "contextWindowCompression": ["triggerTokens": 16000, "slidingWindow": ["targetTokens": 8000]],
            "tools": declarations.isEmpty ? [] : [["functionDeclarations": declarations]]
        ]])
    }

    static func decode(_ data: Data) throws -> [LiveEvent] {
        let message = try object(data)
        if message["error"] != nil { throw LiveFailure.rejected }
        if message["goAway"] != nil { throw LiveFailure.ended }
        var events: [LiveEvent] = []
        if let content = message["serverContent"] as? [String: Any] {
            let interrupted = content["interrupted"] as? Bool == true
            if interrupted { events.append(.interrupted) }
            if let text = (content["inputTranscription"] as? [String: Any])?["text"] as? String {
                events.append(.transcript(text))
            }
            if !interrupted {
                if let text = (content["outputTranscription"] as? [String: Any])?["text"] as? String {
                    events.append(.response(text))
                }
                let parts = (content["modelTurn"] as? [String: Any])?["parts"] as? [[String: Any]] ?? []
                for part in parts {
                    guard let inline = part["inlineData"] as? [String: Any] else { continue }
                    guard let mime = inline["mimeType"] as? String, mime.hasPrefix("audio/pcm"),
                          let base64 = inline["data"] as? String, let pcm = Data(base64Encoded: base64),
                          !pcm.isEmpty, pcm.count.isMultiple(of: 2) else { throw LiveFailure.invalidMessage }
                    let rate = mime.split(separator: ";").compactMap { field -> Int? in
                        let pair = field.split(separator: "=", maxSplits: 1)
                        return pair.count == 2 && pair[0].trimmingCharacters(in: .whitespaces) == "rate" ? Int(pair[1]) : nil
                    }.first ?? 24000
                    guard rate > 0, rate <= 192000 else { throw LiveFailure.invalidMessage }
                    events.append(.audio(.init(data: pcm, rate: rate)))
                }
            }
            if content["turnComplete"] as? Bool == true {
                events.append(.turnComplete(idle: content["interactionStatus"] as? String != "IN_PROGRESS"))
            }
        }
        if let calls = (message["toolCall"] as? [String: Any])?["functionCalls"] as? [[String: Any]] {
            events.append(.calls(try calls.map { call in
                guard let id = call["id"] as? String, !id.isEmpty, let name = call["name"] as? String else {
                    throw LiveFailure.invalidMessage
                }
                return LiveCall(id: id, name: name, arguments: try JSONSerialization.data(withJSONObject: call["args"] ?? [:]))
            }))
        }
        if let ids = (message["toolCallCancellation"] as? [String: Any])?["ids"] as? [String] {
            events.append(.cancel(ids))
        }
        if let usage = message["usageMetadata"] as? [String: Any] {
            events.append(.usage(.init(input: usage["promptTokenCount"] as? Int ?? 0,
                                      output: usage["responseTokenCount"] as? Int ?? 0,
                                      total: usage["totalTokenCount"] as? Int ?? 0)))
        }
        return events
    }

    private static func object(_ data: Data) throws -> [String: Any] {
        guard let value = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw LiveFailure.invalidMessage
        }
        return value
    }
}
