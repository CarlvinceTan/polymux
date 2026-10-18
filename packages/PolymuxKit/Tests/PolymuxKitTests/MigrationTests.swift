import CSQLite
import Foundation
import Testing
@testable import PolymuxKit

@Suite
struct MigrationTests {
    private func location() -> URL {
        FileManager.default.temporaryDirectory.appendingPathComponent("polymux-migration-\(UUID())/cache.sqlite")
    }

    private func seed(_ cache: LocalCache) throws -> Message {
        try cache.save(agents: [], threads: [Thread(id: "one", agentID: "a")])
        var message = Message(id: "reply", threadID: "one", role: "assistant", parts: [Part(kind: "text", text: "First")], createdAt: "2026-10-09T10:00:00Z", status: "streaming", seq: 1)
        try cache.apply(Event(seq: 1, conversationID: "one", type: "message.start", messageID: message.id, message: message, createdAt: message.createdAt))
        message.parts[0].text = "First and second"
        message.seq = 2
        try cache.apply(Event(seq: 2, conversationID: "one", type: "message.delta", messageID: message.id, text: " and second", message: message, createdAt: message.createdAt))
        message.status = "done"
        message.seq = 3
        try cache.apply(Event(seq: 3, conversationID: "one", type: "message.done", messageID: message.id, message: message, createdAt: message.createdAt))
        _ = try cache.enqueue(threadID: "one", request: SendRequest(id: "offline-send", text: "Keep this queued"), attachments: [])
        try cache.markRead(threadID: "one", seq: 3)
        return message
    }

    private func markLegacy(_ url: URL, removeStarts: Bool) throws {
        var pointer: OpaquePointer?
        guard sqlite3_open(url.path, &pointer) == SQLITE_OK, let pointer else { throw ClientError.cache("Test database did not open.") }
        defer { sqlite3_close(pointer) }
        #expect(sqlite3_exec(pointer, "DELETE FROM metadata WHERE key IN ('cacheVersion','seqRepair')", nil, nil, nil) == SQLITE_OK)
        if removeStarts {
            #expect(sqlite3_exec(pointer, "DELETE FROM events WHERE json_extract(json,'$.type')='message.start'", nil, nil, nil) == SQLITE_OK)
        }
    }

    @Test func cachedStartRepairsSequenceAndKeepsCurrentState() throws {
        let url = location()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let original = try LocalCache(url: url)
        let old = try seed(original)
        #expect(try original.messages(threadID: "one").first?.seq == 3)
        try markLegacy(url, removeStarts: false)
        let repaired = try LocalCache(url: url)
        let message = try #require(repaired.messages(threadID: "one").first)
        #expect(message.seq == 1)
        #expect(message.text == old.text)
        #expect(message.status == "done")
        #expect(try repaired.cursor(threadID: "one") == 3)
        #expect(try repaired.pending(threadID: "one").map(\.id) == ["offline-send"])
        #expect(try repaired.threads().first?.lastReadSeq == 3)
        #expect(try repaired.threads().first?.lastSeq == 3)
        #expect(try repaired.threads().first?.lastMessage?.seq == 1)
        #expect(try repaired.search(threadID: "one", query: "second").map(\.id) == ["reply"])
        #expect(try repaired.repairIDs(threadID: "one").isEmpty)
        let secondLaunch = try LocalCache(url: url)
        #expect(try secondLaunch.messages(threadID: "one").first?.seq == 1)
        #expect(try secondLaunch.pending(threadID: "one").map(\.id) == ["offline-send"])
    }

    @Test func prunedStartUsesOneServerSnapshotRepair() throws {
        let url = location()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let original = try LocalCache(url: url)
        var snapshot = try seed(original)
        try markLegacy(url, removeStarts: true)
        let repaired = try LocalCache(url: url)
        #expect(try repaired.repairIDs(threadID: "one") == ["reply"])
        snapshot.seq = 1
        snapshot.parts[0].text = "An older HTTP snapshot must not overwrite the current text"
        try repaired.repairSequences([snapshot])
        #expect(try repaired.messages(threadID: "one").first?.seq == 1)
        #expect(try repaired.messages(threadID: "one").first?.text == "First and second")
        #expect(try repaired.cursor(threadID: "one") == 3)
        #expect(try repaired.pending(threadID: "one").map(\.id) == ["offline-send"])
        #expect(try repaired.repairIDs(threadID: "one").isEmpty)
        let secondLaunch = try LocalCache(url: url)
        #expect(try secondLaunch.repairIDs(threadID: "one").isEmpty)
        #expect(try secondLaunch.messages(threadID: "one").first?.seq == 1)
    }
}
