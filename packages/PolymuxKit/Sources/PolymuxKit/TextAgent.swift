import Foundation

public struct TextTurn: Sendable {
    public enum Role: Sendable { case user, assistant, tool }
    public let role: Role
    public let text: String
    public let state: Data?
    public let result: LiveResult?

    public init(role: Role, text: String = "", state: Data? = nil, result: LiveResult? = nil) {
        self.role = role
        self.text = text
        self.state = state
        self.result = result
    }
}

public struct TextAnswer: Sendable {
    public let text: String
    public let calls: [LiveCall]
    public let state: Data

    public init(text: String, calls: [LiveCall] = [], state: Data = Data()) {
        self.text = text
        self.calls = calls
        self.state = state
    }
}

@MainActor
public protocol TextProvider: AnyObject, Sendable {
    func generate(turns: [TextTurn], options: LiveOptions, key: String) async throws -> TextAnswer
}

@MainActor
public final class GeminiText: TextProvider {
    private let session: URLSession
    private let client: APIClient?

    public init(session: URLSession = URLSession(configuration: .ephemeral), client: APIClient? = nil) {
        self.session = session
        self.client = client
    }

    public func generate(turns: [TextTurn], options: LiveOptions, key: String) async throws -> TextAnswer {
        if let client {
            do { return try Self.answer(await client.speechText(Self.body(turns: turns, options: options))) }
            catch let error as ClientError {
                if case .response(409, _) = error { throw LiveFailure.missingKey }
                if case .response(502, "Provider credits depleted.") = error { throw LiveFailure.credits }
                throw error
            }
        }
        guard !key.isEmpty else { throw LiveFailure.missingKey }
        let model = options.model.replacingOccurrences(of: "models/", with: "")
        let allowed = CharacterSet.alphanumerics.union(CharacterSet(charactersIn: "-_."))
        guard !model.isEmpty, model.unicodeScalars.allSatisfy(allowed.contains) else { throw LiveFailure.configuration }
        let url = URL(string: "https://generativelanguage.googleapis.com/v1beta/models/\(model):generateContent")!
        var request = URLRequest(url: url)
        request.httpMethod = "POST"
        request.setValue(key, forHTTPHeaderField: "x-goog-api-key")
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.timeoutInterval = 45
        request.httpBody = try Self.body(turns: turns, options: options)
        let data: Data
        let response: URLResponse
        do { (data, response) = try await session.data(for: request) }
        catch {
            if Task.isCancelled { throw CancellationError() }
            throw LiveFailure.connection
        }
        guard let http = response as? HTTPURLResponse else { throw LiveFailure.connection }
        guard (200..<300).contains(http.statusCode) else {
            let message = String(data: data, encoding: .utf8)?.lowercased() ?? ""
            if message.contains("credits") && message.contains("depleted") { throw LiveFailure.credits }
            throw LiveFailure.rejected
        }
        return try Self.answer(data)
    }

    static func body(turns: [TextTurn], options: LiveOptions) throws -> Data {
        let contents: [[String: Any]] = try turns.map { turn in
            switch turn.role {
            case .user: return ["role": "user", "parts": [["text": turn.text]]]
            case .assistant:
                if let state = turn.state, !state.isEmpty,
                   let parts = try JSONSerialization.jsonObject(with: state) as? [[String: Any]] {
                    return ["role": "model", "parts": parts]
                }
                return ["role": "model", "parts": [["text": turn.text]]]
            case .tool:
                guard let result = turn.result else { throw LiveFailure.configuration }
                return ["role": "user", "parts": [["functionResponse": [
                    "name": result.call.name, "id": result.call.id,
                    "response": [result.failed ? "error" : "result": result.text]
                ]]]]
            }
        }
        let tools: [[String: Any]] = try options.tools.map {
            guard let schema = try JSONSerialization.jsonObject(with: $0.parameters) as? [String: Any] else {
                throw LiveFailure.configuration
            }
            return ["name": $0.name, "description": $0.description, "parametersJsonSchema": schema]
        }
        return try JSONSerialization.data(withJSONObject: [
            "systemInstruction": ["parts": [["text": options.instructions]]],
            "contents": contents, "generationConfig": ["maxOutputTokens": 4096],
            "tools": tools.isEmpty ? [] : [["functionDeclarations": tools]]
        ])
    }

    static func answer(_ data: Data) throws -> TextAnswer {
        guard let root = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let candidates = root["candidates"] as? [[String: Any]],
              let content = candidates.first?["content"] as? [String: Any],
              let parts = content["parts"] as? [[String: Any]] else { throw LiveFailure.invalidMessage }
        var text = ""
        var calls: [LiveCall] = []
        for part in parts {
            if part["thought"] as? Bool != true, let fragment = part["text"] as? String { text += fragment }
            if let function = part["functionCall"] as? [String: Any] {
                guard let name = function["name"] as? String else { throw LiveFailure.invalidMessage }
                calls.append(LiveCall(id: function["id"] as? String ?? UUID().uuidString, name: name,
                                      arguments: try JSONSerialization.data(withJSONObject: function["args"] ?? [:])))
            }
        }
        return TextAnswer(text: text, calls: calls, state: try JSONSerialization.data(withJSONObject: parts))
    }
}
