import CSQLite
import Foundation
import Testing
@testable import PolymuxKit

@Suite
struct GroupTests {
    @Test func groupRegistryKeepsIndividualLookupAndOfflineMentions() async throws {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent("polymux-group-\(UUID())")
        defer { try? FileManager.default.removeItem(at: folder) }
        let url = folder.appendingPathComponent("cache.sqlite")
        let cache = try LocalCache(url: url)
        let a = Agent(id: "a", name: "Research Writer")
        let b = Agent(id: "b", name: "Éditeur")
        let group = Thread(id: "group", agentID: a.id, title: "Trip", agentIDs: [a.id, b.id])
        let individual = Thread(id: a.id, agentID: a.id, title: a.name, agentIDs: [a.id])
        try cache.save(agents: [a, b], threads: [group, individual])
        let sidebar = await SidebarStore(client: APIClient(baseURL: URL(string: "http://127.0.0.1:1")!), cache: cache)
        await sidebar.load()
        let direct = await sidebar.thread(for: a)
        let groups = await sidebar.groups
        let people = await sidebar.participants(in: group)
        #expect(direct == individual)
        #expect(groups == [group])
        #expect(people == [a, b])
        let request = SendRequest(id: "offline", text: "@Éditeur check this", mentions: [b.id])
        let pending = try cache.enqueue(threadID: group.id, request: request, attachments: [])
        #expect(pending.mentions == [b.id])
        let reply = Message(id: "reply", threadID: group.id, role: "assistant", parts: [Part(kind: "text", text: "The blue route")], createdAt: WireDate.string(), status: "done", seq: 1, agentID: b.id)
        try cache.apply(Event(seq: 1, conversationID: group.id, type: "message.start", message: reply, createdAt: reply.createdAt))
        let reopened = try LocalCache(url: url)
        #expect(try reopened.pending(threadID: group.id) == [request])
        #expect(try reopened.threads().first?.agentIDs == [a.id, b.id])
        #expect(try reopened.messages(threadID: group.id).first?.agentID == b.id)
        #expect(reply.replyContext.agentID == b.id)
        #expect(try reopened.search(threadID: group.id, query: "blue").map(\.id) == [reply.id])
        #expect(try reopened.messages(threadID: individual.id).isEmpty)
    }

    @Test func oldRegistryMigrationPreservesQueuedData() throws {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent("polymux-group-migrate-\(UUID())")
        defer { try? FileManager.default.removeItem(at: folder) }
        let url = folder.appendingPathComponent("cache.sqlite")
        let cache = try LocalCache(url: url)
        let agent = Agent(id: "a", name: "Writer")
        try cache.save(agents: [agent], threads: [Thread(id: "a", agentID: "a", lastReadSeq: 1, lastSeq: 1)])
        let request = SendRequest(id: "offline", text: "Keep queued work")
        _ = try cache.enqueue(threadID: "a", request: request, attachments: [])
        var db: OpaquePointer?
        #expect(sqlite3_open(url.path, &db) == SQLITE_OK)
        defer { sqlite3_close(db) }
        let sql = "DELETE FROM metadata WHERE key='groupVersion'; UPDATE metadata SET json=(SELECT json_group_array(json_remove(value,'$.title','$.agentIDs')) FROM json_each(metadata.json)) WHERE key='threads';"
        #expect(sqlite3_exec(db, sql, nil, nil, nil) == SQLITE_OK)
        let migrated = try LocalCache(url: url)
        #expect(try migrated.threads().first?.title == "Writer")
        #expect(try migrated.threads().first?.agentIDs == ["a"])
        #expect(try migrated.threads().first?.lastReadSeq == 1)
        #expect(try migrated.pending(threadID: "a") == [request])
        #expect(try migrated.messages(threadID: "a").first?.text == request.text)
    }
}
