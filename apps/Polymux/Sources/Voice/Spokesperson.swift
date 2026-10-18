import Foundation
import PolymuxKit

@MainActor
final class Spokesperson: VoiceReply {
    private let makeProvider: () throws -> any TextProvider
    private let options: LiveOptions
    private let delegate: any VoiceReply
    private let key: () throws -> String
    private var history: [TextTurn] = []
    private var task: Task<Void, Never>?
    private var working = false
    private var token = UUID()

    init(options: LiveOptions, delegate: any VoiceReply, makeProvider: @escaping () throws -> any TextProvider,
         key: @escaping () throws -> String) {
        self.makeProvider = makeProvider
        var config = options
        config.tools = [LiveStore.agentTool]
        self.options = config
        self.delegate = delegate
        self.key = key
    }

    func preview(_ text: String) {}

    func stream(_ text: String) -> AsyncThrowingStream<VoiceFragment, Error> {
        let current = UUID()
        token = current
        return AsyncThrowingStream { continuation in
            let task = Task { [weak self] in
                guard let self else { continuation.finish(); return }
                let previous = self.history
                self.history.append(TextTurn(role: .user, text: text))
                do {
                    guard !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty, text.count <= 16384 else {
                        throw LiveFailure.configuration
                    }
                    let provider = try self.makeProvider()
                    let key = try self.key()
                    var results: [String: LiveResult] = [:]
                    for _ in 0..<8 {
                        try Task.checkCancellation()
                        let answer = try await provider.generate(turns: self.history, options: self.options, key: key)
                        try Task.checkCancellation()
                        guard self.token == current else { throw CancellationError() }
                        self.history.append(TextTurn(role: .assistant, text: answer.text, state: answer.state))
                        if !answer.text.isEmpty { continuation.yield(VoiceFragment(text: answer.text)) }
                        if answer.calls.isEmpty {
                            self.trimHistory()
                            continuation.finish()
                            return
                        }
                        for call in answer.calls {
                            let result: LiveResult
                            if let previous = results[call.id] { result = previous }
                            else { result = try await self.consult(call); results[call.id] = result }
                            try Task.checkCancellation()
                            self.history.append(TextTurn(role: .tool, result: result))
                        }
                    }
                    throw LiveFailure.backlog
                } catch {
                    if self.token == current { self.history = previous }
                    continuation.finish(throwing: error)
                }
            }
            self.task = task
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    func stop() async {
        let pending = task
        task = nil
        pending?.cancel()
        if working { await delegate.stop() }
        await pending?.value
        token = UUID()
    }

    private func consult(_ call: LiveCall) async throws -> LiveResult {
        let args = try? JSONDecoder().decode(AgentArgs.self, from: call.arguments)
        let text = args?.text.trimmingCharacters(in: .whitespacesAndNewlines) ?? ""
        guard call.name == "ask_agent", !text.isEmpty, text.count <= 16384 else {
            return LiveResult(call: call, text: "Unsupported tool or invalid request.", failed: true)
        }
        working = true
        defer { working = false }
        var result = ""
        do {
            for try await fragment in delegate.stream(text) {
                try Task.checkCancellation()
                result += fragment.text.prefix(max(0, 160000 - result.count))
            }
            try Task.checkCancellation()
            return LiveResult(call: call, text: result)
        } catch {
            if Task.isCancelled { throw CancellationError() }
            return LiveResult(call: call, text: "The consultation failed and its outcome is unconfirmed. Actions may already have occurred. Do not retry automatically; ask the user before retrying.", failed: true)
        }
    }

    private func trimHistory() {
        // Never cut through a function-call exchange.
        while history.count > 32 || history.reduce(0, { $0 + $1.text.utf8.count + ($1.state?.count ?? 0) + ($1.result?.text.utf8.count ?? 0) }) > 128000 {
            guard let start = history.indices.dropFirst().first(where: { history[$0].role == .user }) else { return }
            history.removeFirst(start)
        }
    }

    private struct AgentArgs: Decodable { let text: String }
}
