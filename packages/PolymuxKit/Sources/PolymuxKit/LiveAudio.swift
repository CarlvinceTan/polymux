import Foundation

/// Mono, signed 16-bit little-endian PCM without a container header.
public struct LivePCM: Sendable, Equatable {
    public let data: Data
    public let rate: Int

    public init(data: Data, rate: Int) {
        self.data = data
        self.rate = rate
    }
}

public struct LiveTool: Sendable {
    public let name: String
    public let description: String
    /// Standard JSON Schema, independent of a provider's wire format.
    public let parameters: Data

    public init(name: String, description: String, parameters: Data) {
        self.name = name
        self.description = description
        self.parameters = parameters
    }
}

public struct LiveOptions: Sendable {
    public var model: String
    public var voice: String
    public var instructions: String
    public var tools: [LiveTool]

    public init(model: String, voice: String, instructions: String, tools: [LiveTool] = []) {
        self.model = model
        self.voice = voice
        self.instructions = instructions
        self.tools = tools
    }
}

public struct LiveCall: Sendable, Equatable {
    public let id: String
    public let name: String
    public let arguments: Data

    public init(id: String, name: String, arguments: Data) {
        self.id = id
        self.name = name
        self.arguments = arguments
    }
}

public struct LiveResult: Sendable {
    public let call: LiveCall
    public let text: String
    public let failed: Bool

    public init(call: LiveCall, text: String, failed: Bool = false) {
        self.call = call
        self.text = text
        self.failed = failed
    }
}

public struct LiveUsage: Sendable, Equatable {
    public let input: Int
    public let output: Int
    public let total: Int

    public init(input: Int, output: Int, total: Int) {
        self.input = input
        self.output = output
        self.total = total
    }
}

public enum LiveEvent: Sendable, Equatable {
    case transcript(String)
    case response(String)
    case audio(LivePCM)
    case interrupted
    case turnComplete(idle: Bool)
    case calls([LiveCall])
    case cancel([String])
    case usage(LiveUsage)
}

/// Providers own authentication and wire formats, not microphone, playback, or agent work.
/// Return credential-safe errors and preserve call IDs for cancellation and result delivery.
@MainActor
public protocol LiveProvider: AnyObject, Sendable {
    var inputRate: Int { get }
    func start(options: LiveOptions, key: String) async throws -> AsyncThrowingStream<LiveEvent, Error>
    func sendAudio(_ chunk: LivePCM) async throws
    func endAudio() async throws
    func respond(_ result: LiveResult) async throws
    func stop() async
}

public enum LiveFailure: LocalizedError {
    case missingKey
    case configuration
    case connection
    case rejected
    case invalidMessage
    case ended
    case backlog
    case credits

    public var errorDescription: String? {
        switch self {
        case .missingKey: "Add an API key in Settings → Voice."
        case .configuration: "Check the Live provider, model, and voice in Settings."
        case .connection: "Could not connect to the Live API. Check your connection and API key, then retry."
        case .rejected: "The Live API rejected this session. Check the API key, model access, and quota."
        case .invalidMessage: "The Live API returned an unsupported message."
        case .ended: "The Live session ended. Start a new conversation to continue."
        case .backlog: "The Live connection cannot keep up with audio. Check your connection and retry."
        case .credits: "The Live API’s prepaid credits are depleted. Add credits to the provider project, then retry."
        }
    }
}

@MainActor
protocol LiveSocket: AnyObject, Sendable {
    var closeReason: String? { get }
    func send(_ data: Data) async throws
    func receive() async throws -> Data
    func close()
}

extension LiveSocket {
    var closeReason: String? { nil }
}

@MainActor
final class LiveWebSocket: LiveSocket {
    private let session: URLSession
    private let task: URLSessionWebSocketTask
    var closeReason: String? { task.closeReason.flatMap { String(data: $0, encoding: .utf8) } }

    init(url: URL, token: String? = nil) {
        session = URLSession(configuration: .ephemeral)
        var request = URLRequest(url: url)
        request.timeoutInterval = 20
        if let token {
            request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        }
        task = session.webSocketTask(with: request)
        task.resume()
    }

    func send(_ data: Data) async throws {
        guard let text = String(data: data, encoding: .utf8) else { throw LiveFailure.invalidMessage }
        try await task.send(.string(text))
    }

    func receive() async throws -> Data {
        switch try await task.receive() {
        case .data(let data): data
        case .string(let text): Data(text.utf8)
        @unknown default: throw LiveFailure.invalidMessage
        }
    }

    func close() {
        task.cancel(with: .normalClosure, reason: nil)
        session.invalidateAndCancel()
    }
}
