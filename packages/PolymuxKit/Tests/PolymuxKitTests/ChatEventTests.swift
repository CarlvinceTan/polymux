import Foundation
import Testing
@testable import PolymuxKit

@Suite @MainActor struct ChatEventTests {
    @Test func cachedEventsStillPublishLiveReplies() async throws {
        let folder = FileManager.default.temporaryDirectory.appending(path: "polymux-events-\(UUID())")
        defer { try? FileManager.default.removeItem(at: folder) }
        let cache = LocalCacheActor(try await CacheWork.open(folder.appending(path: "cache.sqlite")))
        let chat = ChatStore(client: APIClient(baseURL: URL(string: "http://127.0.0.1:1")!), actor: cache, threadID: "chat")
        await chat.ready()
        let first = message("first", seq: 1)
        let second = message("second", seq: 2)
        let events = [Event(seq: 1, conversationID: "chat", type: EventType.messageStart, message: first),
                      Event(seq: 2, conversationID: "chat", type: EventType.messageStart, message: second),
                      Event(seq: 3, conversationID: "chat", type: EventType.messageDone, messageID: first.id),
                      Event(seq: 4, conversationID: "chat", type: EventType.messageDone, messageID: second.id)]
        for event in events { _ = try await cache.call { try $0.apply(event) } }
        // Another reader can fill the shared cache before the open chat receives its stream.
        for event in events.reversed() { try await chat.receive(event) }
        #expect(chat.messages.map(\.id) == ["first", "second"])
        #expect(chat.messages.allSatisfy { $0.status == MessageStatus.done })
    }

    @Test func liveInsertsKeepAcceptedAndPendingMessagesOrdered() async throws {
        let folder = FileManager.default.temporaryDirectory.appending(path: "polymux-order-\(UUID())")
        defer { try? FileManager.default.removeItem(at: folder) }
        let cache = LocalCacheActor(try await CacheWork.open(folder.appending(path: "cache.sqlite")))
        let chat = ChatStore(client: APIClient(baseURL: URL(string: "http://127.0.0.1:1")!), actor: cache, threadID: "chat")
        await chat.ready()
        for (seq, id) in [(Int64(1), "user"), (2, "assistant"), (3, "next-user")] {
            let value = message(id, seq: seq)
            try await chat.receive(Event(seq: seq, conversationID: "chat", type: EventType.messageStart, message: value))
        }
        try await chat.receive(Event(seq: 4, conversationID: "chat", type: EventType.messageDone, messageID: "assistant"))
        #expect(chat.messages.map(\.id) == ["user", "assistant", "next-user"])
        var finished = message("assistant", seq: 5)
        finished.status = MessageStatus.done
        try await chat.receive(Event(seq: 5, conversationID: "chat", type: EventType.messageDone, message: finished))
        #expect(chat.messages.map(\.id) == ["user", "next-user", "assistant"])
    }

    private func message(_ id: String, seq: Int64) -> Message {
        Message(id: id, threadID: "chat", role: MessageRole.assistant, parts: [Part(kind: PartKind.text, text: id)],
                createdAt: "2026-10-29T00:00:00Z", status: MessageStatus.streaming, seq: seq)
    }
}
