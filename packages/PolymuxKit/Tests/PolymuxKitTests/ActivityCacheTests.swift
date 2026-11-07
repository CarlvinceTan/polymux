import Foundation
import Testing
@testable import PolymuxKit

@Suite struct ActivityCacheTests {
    private func event(_ seq: Int64, kind: String = "memory.saved", seconds: Double = 0,
                       thread: String = "chief", agents: [String]? = nil) -> Event {
        let date = Date(timeIntervalSince1970: 1_760_000_000 + seconds)
        return Event(seq: seq, conversationID: thread, type: "activity", agentID: "chief",
                     createdAt: WireDate.string(date),
                     payload: Activity(kind: kind, verb: kind == "agent.messaged" ? "Messaged" : "Saved",
                                       objectID: String(seq), objectLabel: "Preference \(seq)",
                                       at: WireDate.string(date), agentIDs: agents))
    }

    @Test func countsUnreadRepliesAndActivities() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let cache = try LocalCache(url: directory.appendingPathComponent("cache.sqlite"))
        try cache.save(agents: [], threads: [Thread(id: "chief", agentID: "chief")])
        let reply = Message(id: "reply", threadID: "chief", role: "assistant", parts: [],
                            createdAt: WireDate.string(), status: "done", seq: 1)
        try cache.apply(Event(seq: 1, conversationID: "chief", type: "message.start", message: reply))
        try cache.apply(event(2, kind: "agent.messaged"))
        #expect(try cache.threads().first?.unreadCount == 2)
        try cache.apply(event(3, kind: "memory.saved"))
        try cache.apply(event(4, kind: "subagent.started"))
        let finished = event(5, kind: "subagent.finished")
        try cache.apply(finished)
        try cache.apply(finished)
        #expect(try cache.threads().first?.unreadCount == 3)
        try cache.markRead(threadID: "chief", seq: 5)
        #expect(try cache.threads().first?.unreadCount == 0)
    }

    @Test @MainActor func sidebarCountsPeerActivityOnce() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let cache = try await CacheWork.open(directory.appendingPathComponent("cache.sqlite"))
        let client = APIClient(baseURL: URL(string: "http://localhost")!)
        let sidebar = SidebarStore(client: client, cache: cache)
        try await sidebar.cacheActor.call { try $0.save(agents: [], threads: [Thread(id: "chief", agentID: "chief")]) }
        await sidebar.load()
        let sent = event(1, kind: "agent.messaged", agents: ["chief", "aria"])
        sidebar.note(sent)
        sidebar.note(sent)
        #expect(sidebar.threads.first?.unreadCount == 1)
        sidebar.note(event(2))
        #expect(sidebar.threads.first?.unreadCount == 1)
        await sidebar.settled()
        #expect(try await sidebar.cacheActor.call { try $0.threads().first?.unreadCount } == 1)
    }

}
