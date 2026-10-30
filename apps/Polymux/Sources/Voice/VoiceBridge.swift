import Foundation
import PolymuxKit

@MainActor
final class VoiceBridge: VoiceReply {
    private let client: APIClient
    private let threadID: String
    /// The user hears the agents' replies directly, streamed; otherwise a live spokesperson consults them.
    private let spoken: Bool
    private var pending: Task<Void, Never>?

    init(client: APIClient, threadID: String, spoken: Bool = false) {
        self.client = client
        self.threadID = threadID
        self.spoken = spoken
    }

    func preview(_ text: String) {}

    func stream(_ text: String) -> AsyncThrowingStream<VoiceFragment, Error> {
        AsyncThrowingStream { continuation in
            let task = Task { [client, threadID, spoken] in
                do {
                    if spoken {
                        for try await fragment in client.voiceStream(threadID: threadID, request: VoiceRequest(id: UUID().uuidString, text: text)) {
                            continuation.yield(fragment)
                        }
                        continuation.finish()
                        return
                    }
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
