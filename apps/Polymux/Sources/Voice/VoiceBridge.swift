import Foundation
import PolymuxKit

@MainActor
final class VoiceBridge: VoiceReply {
    private let client: APIClient
    private let threadID: String
    private var pending: Task<Void, Never>?

    init(client: APIClient, threadID: String) {
        self.client = client
        self.threadID = threadID
    }

    func preview(_ text: String) {}

    func stream(_ text: String) -> AsyncThrowingStream<VoiceFragment, Error> {
        AsyncThrowingStream { continuation in
            let task = Task { [client, threadID] in
                do {
                    let response = try await client.voiceTask(threadID: threadID, request: VoiceRequest(id: UUID().uuidString, text: text))
                    try Task.checkCancellation()
                    continuation.yield(VoiceFragment(text: response.text))
                    continuation.finish()
                } catch { continuation.finish(throwing: error) }
            }
            pending = task
            continuation.onTermination = { _ in task.cancel() }
        }
    }

    func stop() async {
        let task = pending
        pending = nil
        task?.cancel()
        await task?.value
    }
}
