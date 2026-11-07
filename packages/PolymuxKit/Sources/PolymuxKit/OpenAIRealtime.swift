import Foundation

/// OpenAI Realtime speech through the server's speech socket, which adds the key and model.
@MainActor
public final class OpenAIRealtime: LiveProvider {
    public let inputRate = 24_000
    private let makeSocket: () async throws -> any LiveSocket
    private var socket: (any LiveSocket)?
    private var reader: Task<Void, Never>?
    private var events: AsyncThrowingStream<LiveEvent, Error>.Continuation?
    private var token = UUID()

    public convenience init(client: APIClient) {
        self.init { LiveWebSocket(url: try await client.speechSocket(), token: await client.token) }
    }

    init(makeSocket: @escaping () async throws -> any LiveSocket) {
        self.makeSocket = makeSocket
    }

    public func start(options: LiveOptions, key: String) async throws -> AsyncThrowingStream<LiveEvent, Error> {
        await stop()
        let setup = try Self.setup(options)
        let current = UUID()
        token = current
        let socket = try await makeSocket()
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
                // The session is ready once the server confirms the configuration.
                while true {
                    let message = try Self.object(await socket.receive())
                    if message["type"] as? String == "error" { throw LiveFailure.rejected }
                    if message["type"] as? String == "session.updated" { break }
                }
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
                    stream.continuation.finish(throwing: Self.failure(error, socket: socket))
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
        try await send(["type": "input_audio_buffer.append", "audio": chunk.data.base64EncodedString()])
    }

    public func endAudio() async throws {
        try await send(["type": "input_audio_buffer.commit"])
    }

    public func respond(_ result: LiveResult) async throws {
        let output = result.failed ? "Error: " + result.text : result.text
        try await send(["type": "conversation.item.create",
                        "item": ["type": "function_call_output", "call_id": result.call.id, "output": output]])
        try await send(["type": "response.create"])
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

    /// The opening session.update; the server sets the chosen voice and model.
    static func setup(_ options: LiveOptions) throws -> Data {
        let tools: [[String: Any]] = try options.tools.map { tool in
            ["type": "function", "name": tool.name, "description": tool.description, "parameters": try object(tool.parameters)]
        }
        return try JSONSerialization.data(withJSONObject: ["type": "session.update", "session": [
            "type": "realtime",
            "instructions": options.instructions,
            "output_modalities": ["audio"],
            "audio": [
                "input": ["format": ["type": "audio/pcm", "rate": 24_000],
                          "transcription": ["model": "gpt-4o-mini-transcribe"],
                          "turn_detection": ["type": "server_vad", "silence_duration_ms": 500, "prefix_padding_ms": 300]],
                "output": ["format": ["type": "audio/pcm", "rate": 24_000]]
            ],
            "tools": tools,
            "tool_choice": "auto"
        ]])
    }

    static func decode(_ data: Data) throws -> [LiveEvent] {
        let message = try object(data)
        switch message["type"] as? String ?? "" {
        case "error":
            throw LiveFailure.rejected
        case "input_audio_buffer.speech_started":
            return [.interrupted]
        case "conversation.item.input_audio_transcription.delta":
            return (message["delta"] as? String).map { [.transcript($0)] } ?? []
        case "response.output_audio_transcript.delta", "response.audio_transcript.delta":
            return (message["delta"] as? String).map { [.response($0)] } ?? []
        case "response.output_audio.delta", "response.audio.delta":
            guard let base64 = message["delta"] as? String, let pcm = Data(base64Encoded: base64),
                  !pcm.isEmpty, pcm.count.isMultiple(of: 2) else { throw LiveFailure.invalidMessage }
            return [.audio(.init(data: pcm, rate: 24_000))]
        case "response.done":
            let response = message["response"] as? [String: Any] ?? [:]
            var events: [LiveEvent] = []
            let calls = try (response["output"] as? [[String: Any]] ?? []).filter { $0["type"] as? String == "function_call" }.map { item in
                guard let id = item["call_id"] as? String, !id.isEmpty, let name = item["name"] as? String else {
                    throw LiveFailure.invalidMessage
                }
                return LiveCall(id: id, name: name, arguments: Data((item["arguments"] as? String ?? "{}").utf8))
            }
            if !calls.isEmpty { events.append(.calls(calls)) }
            if let usage = response["usage"] as? [String: Any] {
                events.append(.usage(.init(input: usage["input_tokens"] as? Int ?? 0, output: usage["output_tokens"] as? Int ?? 0,
                                           total: usage["total_tokens"] as? Int ?? 0)))
            }
            // A response that only called tools isn't the end of the turn: their results start another.
            events.append(.turnComplete(idle: calls.isEmpty))
            return events
        default:
            return []
        }
    }

    private static func object(_ data: Data) throws -> [String: Any] {
        guard let value = try? JSONSerialization.jsonObject(with: data) as? [String: Any] else {
            throw LiveFailure.invalidMessage
        }
        return value
    }
}
