import Foundation
import Testing
@testable import PolymuxKit

@Suite
struct CacheTests {
    private func location() -> URL {
        FileManager.default.temporaryDirectory.appendingPathComponent("polymux-tests-\(UUID())/cache.sqlite")
    }

    @Test func replaySurvivesRelaunch() throws {
        let url = location()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let agent = Agent(id: "agent-a", name: "Assistant", role: "Helper", model: "test", active: true)
        let cache = try LocalCache(url: url)
        try cache.save(agents: [agent], threads: [Thread(id: "thread-a", agentID: agent.id)])
        let message = Message(id: "reply", threadID: "thread-a", role: "assistant", parts: [Part(kind: "text", text: "")], createdAt: "2026-10-09T10:00:00Z", status: "streaming", seq: 1)
        let start = Event(seq: 1, conversationID: "thread-a", type: "message.start", messageID: message.id, message: message, createdAt: message.createdAt)
        try cache.apply(start)
        try cache.apply(Event(seq: 2, conversationID: "thread-a", type: "message.delta", messageID: message.id, text: "Hello ", createdAt: message.createdAt))
        try cache.apply(Event(seq: 3, conversationID: "thread-a", type: "message.delta", messageID: message.id, text: "world", createdAt: message.createdAt))
        try cache.apply(Event(seq: 4, conversationID: "thread-a", type: "message.done", messageID: message.id, createdAt: message.createdAt))
        try cache.apply(start)
        let relaunched = try LocalCache(url: url)
        #expect(try relaunched.cursor(threadID: "thread-a") == 4)
        #expect(try relaunched.messages(threadID: "thread-a").count == 1)
        #expect(try relaunched.messages(threadID: "thread-a").first?.text == "Hello world")
        #expect(try relaunched.messages(threadID: "thread-a").first?.status == "done")
        #expect(try relaunched.threads().first?.unreadCount == 1)
        try relaunched.markRead(threadID: "thread-a", seq: 4)
        #expect(try relaunched.threads().first?.unreadCount == 0)
        #expect(try relaunched.threads().first?.lastReadSeq == 4)
    }

    @Test func snapshotReplacesDeltaOnce() throws {
        let url = location()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try LocalCache(url: url)
        var message = Message(id: "reply", threadID: "one", role: "assistant", parts: [Part(kind: "text", text: "")], createdAt: WireDate.string(), status: "streaming", seq: 1)
        try cache.apply(Event(seq: 1, conversationID: "one", type: "message.start", message: message, createdAt: message.createdAt))
        message.parts[0].text = "Hello"
        let delta = Event(seq: 2, conversationID: "one", type: "message.delta", messageID: message.id, text: "Hello", message: message, createdAt: message.createdAt)
        try cache.apply(delta)
        try cache.apply(delta)
        #expect(try cache.messages(threadID: "one").first?.text == "Hello")
        #expect(try cache.cursor(threadID: "one") == 2)
        #expect(try cache.messages(threadID: "one").first?.seq == 1)
    }

    @Test func queueKeepsMessageIdentity() throws {
        let url = location()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try LocalCache(url: url)
        let request = SendRequest(id: "offline-send", text: "Find the blue notebook", attachmentIDs: [])
        _ = try cache.enqueue(threadID: "one", request: request, attachments: [])
        _ = try cache.enqueue(threadID: "one", request: request, attachments: [])
        let relaunched = try LocalCache(url: url)
        #expect(try relaunched.pending(threadID: "one") == [request])
        #expect(try relaunched.messages(threadID: "one").count == 1)
        #expect(try relaunched.messages(threadID: "one").first?.status == "pending")
        let accepted = Message(id: request.id, threadID: "one", role: "user", parts: [Part(kind: "text", text: request.text)], createdAt: WireDate.string(), status: "done", seq: 1)
        try relaunched.apply(Event(seq: 1, conversationID: "one", type: "message.start", message: accepted, createdAt: accepted.createdAt))
        #expect(try relaunched.pending(threadID: "one").isEmpty)
        #expect(try relaunched.messages(threadID: "one").count == 1)
        #expect(try relaunched.messages(threadID: "one").first?.status == "done")
    }

    @Test func lexicalSearchIsScopedAndEscaped() throws {
        let url = location()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try LocalCache(url: url)
        try cache.merge([
            Message(id: "a", threadID: "one", role: "user", parts: [Part(kind: "text", text: "The blue notebook is on the desk")], createdAt: WireDate.string(), status: "done", seq: 1),
            Message(id: "b", threadID: "two", role: "user", parts: [Part(kind: "text", text: "The blue notebook is elsewhere")], createdAt: WireDate.string(), status: "done", seq: 1)
        ])
        #expect(try cache.search(threadID: "one", query: "blue notebook").map(\.id) == ["a"])
        #expect(throws: Never.self) { _ = try cache.search(threadID: "one", query: "blue\" OR *") }
    }

    @Test func gapsDoNotAdvanceCursor() throws {
        let url = location()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try LocalCache(url: url)
        let event = Event(seq: 2, conversationID: "one", type: "agent.presence", agentID: "agent-a", active: false, createdAt: WireDate.string())
        #expect(throws: ClientError.self) { try cache.apply(event) }
        #expect(try cache.cursor(threadID: "one") == 0)
    }

    @Test func failedSendsWaitForExplicitRetry() throws {
        let url = location()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try LocalCache(url: url)
        let request = SendRequest(id: "bad-send", text: "Hello", attachmentIDs: [])
        _ = try cache.enqueue(threadID: "one", request: request, attachments: [])
        try cache.setStatus(id: request.id, status: "error")
        #expect(try cache.pending(threadID: "one").isEmpty)
        try cache.setStatus(id: request.id, status: "pending")
        #expect(try cache.pending(threadID: "one") == [request])
    }

    @Test func cachedHistoryPagesWithoutDuplicates() throws {
        let url = location()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try LocalCache(url: url)
        let history = (1...150).map { index in
            Message(id: "m-\(index)", threadID: "one", role: "user", parts: [Part(kind: "text", text: "Message \(index)")], createdAt: WireDate.string(Date(timeIntervalSince1970: TimeInterval(index))), status: "done", seq: Int64(index))
        }
        try cache.merge(history)
        let latest = try cache.messages(threadID: "one", limit: 100)
        #expect(latest.count == 100)
        #expect(latest.first?.seq == 51)
        let earlier = try cache.messages(threadID: "one", before: 51, limit: 100)
        #expect(earlier.count == 50)
        #expect(Set((earlier + latest).map(\.id)).count == 150)
    }

    @Test func lateHistorySnapshotCannotEraseStreamingText() throws {
        let url = location()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try LocalCache(url: url)
        var old = Message(id: "reply", threadID: "one", role: "assistant", parts: [Part(kind: "text", text: "First")], createdAt: WireDate.string(), status: "streaming", seq: 1)
        try cache.apply(Event(seq: 1, conversationID: "one", type: "message.start", message: old, createdAt: old.createdAt))
        var current = old
        current.parts[0].text = "First and second"
        current.seq = 1
        try cache.apply(Event(seq: 2, conversationID: "one", type: "message.delta", text: " and second", message: current, createdAt: old.createdAt))
        old.status = "done"
        try cache.merge([old])
        #expect(try cache.messages(threadID: "one").first?.text == "First and second")
        #expect(try cache.search(threadID: "one", query: "second").count == 1)
        #expect(try cache.cursor(threadID: "one") == 2)
    }

    @Test func lateHttpAcknowledgementKeepsServerSnapshot() throws {
        let url = location()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try LocalCache(url: url)
        let request = SendRequest(id: "send", text: "Hello")
        _ = try cache.enqueue(threadID: "one", request: request, attachments: [])
        let accepted = Message(id: request.id, threadID: "one", role: "user", parts: [Part(kind: "text", text: "Hello")], createdAt: "2026-10-09T10:00:00Z", status: "done", seq: 1)
        try cache.apply(Event(seq: 1, conversationID: "one", type: "message.start", message: accepted, createdAt: accepted.createdAt))
        try cache.acknowledge(id: request.id)
        #expect(try cache.messages(threadID: "one") == [accepted])
        #expect(try cache.pending(threadID: "one").isEmpty)
    }

    @Test func registrySnapshotDoesNotDoubleCountReplayedUnread() throws {
        let url = location()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try LocalCache(url: url)
        try cache.save(agents: [], threads: [Thread(id: "one", agentID: "a", lastReadSeq: 0, lastSeq: 1, unreadCount: 1)])
        let message = Message(id: "reply", threadID: "one", role: "assistant", parts: [], createdAt: WireDate.string(), status: "done", seq: 1)
        try cache.apply(Event(seq: 1, conversationID: "one", type: "message.start", message: message, createdAt: message.createdAt))
        #expect(try cache.threads().first?.unreadCount == 1)
    }

    @Test func datesAcceptServerPrecision() throws {
        #expect(WireDate.parse("2026-10-09T10:00:00Z") != nil)
        #expect(WireDate.parse("2026-10-09T10:00:00.123456789Z") != nil)
        #expect(WireDate.parse(WireDate.string()) != nil)
    }

    @Test func wireDateParseIsStableUnderRepeatedUse() {
        let stamp = "2026-10-09T10:00:00.123Z"
        let expected = WireDate.parse(stamp)
        for _ in 0..<500 {
            #expect(WireDate.parse(stamp) == expected)
        }
    }
}
