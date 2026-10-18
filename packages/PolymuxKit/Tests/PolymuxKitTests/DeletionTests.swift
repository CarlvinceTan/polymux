import CSQLite
import Foundation
import Testing
@testable import PolymuxKit

@Suite
struct DeletionTests {
    private func location() -> URL {
        FileManager.default.temporaryDirectory.appendingPathComponent("polymux-deletion-\(UUID())/cache.sqlite")
    }

    private func seed(_ cache: LocalCache, url: URL) throws {
        try cache.save(agents: [Agent(id: "deleted-agent"), Agent(id: "kept-agent")], threads: [
            Thread(id: "deleted", agentID: "deleted-agent"), Thread(id: "kept", agentID: "kept-agent")
        ])
        for thread in ["deleted", "kept", "unlisted"] {
            let message = Message(id: "\(thread)-reply", threadID: thread, role: "assistant",
                                  parts: [Part(kind: "text", text: "A notebook in \(thread)")],
                                  createdAt: "2026-10-09T10:00:00Z", status: "streaming", seq: 1)
            try cache.apply(Event(seq: 1, conversationID: thread, type: "message.start", message: message,
                                  createdAt: message.createdAt))
            try cache.apply(Event(seq: 2, conversationID: thread, type: "message.done", messageID: message.id,
                                  createdAt: message.createdAt))
            _ = try cache.enqueue(threadID: thread, request: SendRequest(id: "\(thread)-queued", text: "A queued notebook"), attachments: [])
        }
        _ = try cache.enqueue(threadID: "deleted", request: SendRequest(id: "deleted-failed", text: "Failed notebook"), attachments: [])
        try cache.setStatus(id: "deleted-failed", status: "error")
        try run(url, sql: "UPDATE metadata SET json='[\"deleted-reply\",\"kept-reply\",\"unlisted-reply\"]' WHERE key='seqRepair'")
    }

    private func open(_ url: URL) throws -> OpaquePointer {
        var pointer: OpaquePointer?
        guard sqlite3_open(url.path, &pointer) == SQLITE_OK, let pointer else {
            throw ClientError.cache("Test database did not open.")
        }
        return pointer
    }

    private func run(_ url: URL, sql: String) throws {
        let db = try open(url)
        defer { sqlite3_close(db) }
        guard sqlite3_exec(db, sql, nil, nil, nil) == SQLITE_OK else {
            throw ClientError.cache(String(cString: sqlite3_errmsg(db)))
        }
    }

    private func scalar(_ url: URL, sql: String) throws -> String {
        let db = try open(url)
        defer { sqlite3_close(db) }
        var pointer: OpaquePointer?
        guard sqlite3_prepare_v2(db, sql, -1, &pointer, nil) == SQLITE_OK, let pointer else {
            throw ClientError.cache(String(cString: sqlite3_errmsg(db)))
        }
        defer { sqlite3_finalize(pointer) }
        guard sqlite3_step(pointer) == SQLITE_ROW, let value = sqlite3_column_text(pointer, 0) else {
            throw ClientError.cache("Test query did not return a value.")
        }
        return String(cString: value)
    }

    private func repairs(_ url: URL) throws -> Set<String> {
        let json = try scalar(url, sql: "SELECT json FROM metadata WHERE key='seqRepair'")
        return Set(try JSONDecoder().decode([String].self, from: Data(json.utf8)))
    }

    @Test func refreshedRegistryPrunesOnlyDeletedChats() throws {
        let url = location()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try LocalCache(url: url)
        try seed(cache, url: url)
        let kept = try #require(cache.threads().first { $0.id == "kept" })
        let keptMessages = try cache.messages(threadID: "kept")
        let unlisted = try cache.messages(threadID: "unlisted")

        try cache.save(agents: [Agent(id: "kept-agent")], threads: [kept])
        let relaunched = try LocalCache(url: url)
        #expect(try relaunched.agents().map(\.id) == ["kept-agent"])
        #expect(try relaunched.threads() == [kept])
        #expect(try relaunched.messages(threadID: "deleted").isEmpty)
        #expect(try relaunched.search(threadID: "deleted", query: "notebook").isEmpty)
        #expect(try relaunched.pending(threadID: "deleted").isEmpty)
        #expect(try relaunched.cursor(threadID: "deleted") == 0)
        #expect(try relaunched.queued().allSatisfy { $0.threadID != "deleted" })
        for table in ["messages", "messages_fts", "outbox", "events", "cursors"] {
            #expect(try scalar(url, sql: "SELECT COUNT(*) FROM \(table) WHERE thread_id='deleted'") == "0")
        }
        #expect(try repairs(url) == ["kept-reply", "unlisted-reply"])
        #expect(try relaunched.messages(threadID: "kept") == keptMessages)
        #expect(try relaunched.search(threadID: "kept", query: "notebook").count == 2)
        #expect(try relaunched.pending(threadID: "kept").map(\.id) == ["kept-queued"])
        #expect(try relaunched.cursor(threadID: "kept") == 2)
        #expect(try relaunched.messages(threadID: "unlisted") == unlisted)
        #expect(try relaunched.pending(threadID: "unlisted").map(\.id) == ["unlisted-queued"])

        try relaunched.save(agents: [], threads: [])
        #expect(try relaunched.agents().isEmpty)
        #expect(try relaunched.threads().isEmpty)
        #expect(try relaunched.messages(threadID: "kept").isEmpty)
        #expect(try relaunched.messages(threadID: "unlisted") == unlisted)
        #expect(try repairs(url) == ["unlisted-reply"])
    }

    @Test func cleanupFailureRollsBackRegistryAndChatData() throws {
        let url = location()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try LocalCache(url: url)
        try seed(cache, url: url)
        let agents = try cache.agents()
        let threads = try cache.threads()
        let messages = try cache.messages(threadID: "deleted")
        let kept = try #require(threads.first { $0.id == "kept" })
        try run(url, sql: "CREATE TRIGGER fail_cleanup BEFORE DELETE ON messages WHEN OLD.thread_id='deleted' BEGIN SELECT RAISE(ABORT,'forced cleanup failure'); END")

        #expect(throws: ClientError.self) { try cache.save(agents: [Agent(id: "kept-agent")], threads: [kept]) }
        let relaunched = try LocalCache(url: url)
        #expect(try relaunched.agents() == agents)
        #expect(try relaunched.threads() == threads)
        #expect(try relaunched.messages(threadID: "deleted") == messages)
        #expect(try relaunched.search(threadID: "deleted", query: "notebook").count == 3)
        #expect(try relaunched.pending(threadID: "deleted").map(\.id) == ["deleted-queued"])
        #expect(try relaunched.cursor(threadID: "deleted") == 2)
        #expect(try scalar(url, sql: "SELECT COUNT(*) FROM events WHERE thread_id='deleted'") == "2")
        #expect(try scalar(url, sql: "SELECT COUNT(*) FROM outbox WHERE thread_id='deleted'") == "2")
        #expect(try repairs(url) == ["deleted-reply", "kept-reply", "unlisted-reply"])
        #expect(try relaunched.pending(threadID: "kept").map(\.id) == ["kept-queued"])
    }
}
