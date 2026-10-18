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

    @Test func peerRepliesStayHiddenAfterReload() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let cache = try LocalCache(url: directory.appendingPathComponent("cache.sqlite"))
        let request = Message(id: "request", threadID: "chief", role: "assistant", parts: [],
                              createdAt: WireDate.string(), status: "done", seq: 1)
        let reply = Message(id: "reply", threadID: "chief", role: "assistant", parts: [],
                            createdAt: WireDate.string(), status: "done", seq: 3)
        var sent = event(2, kind: "agent.messaged", agents: ["chief", "aria"])
        sent.payload?.messageIDs = [request.id, reply.id, "receipt"]
        var received = event(4, kind: "agent.messaged", seconds: 20, agents: ["chief", "aria"])
        received.payload?.verb = "Message from"
        received.payload?.messageIDs = [reply.id, "receipt"]
        for input in [Event(seq: 1, conversationID: "chief", type: "message.start", message: request), sent,
                      Event(seq: 3, conversationID: "chief", type: "message.done", message: reply), received] {
            _ = try cache.apply(input)
        }
        let client = APIClient(baseURL: URL(string: "http://localhost")!)
        let store = await ChatStore(client: client, actor: LocalCacheActor(cache), threadID: "chief")
        await store.ready()
        #expect(await store.messages.isEmpty)
    }

    @Test func restoresOfflineRowsPastEventRetention() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let url = directory.appendingPathComponent("cache.sqlite")
        let cache = try LocalCache(url: url)
        try cache.apply(event(1))
        for seq in 2...1003 {
            try cache.apply(Event(seq: Int64(seq), conversationID: "chief", type: "run.update"))
        }
        let reopened = try LocalCache(url: url)
        let events = try reopened.activityEvents(threadID: "chief")
        #expect(events.count == 1)
        #expect(events.first?.payload?.kind == "memory.saved")
    }

    @Test func countsUnreadActivitiesWithoutHiddenMessages() throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let cache = try LocalCache(url: directory.appendingPathComponent("cache.sqlite"))
        try cache.save(agents: [], threads: [Thread(id: "chief", agentID: "chief")])
        let reply = Message(id: "reply", threadID: "chief", role: "assistant", parts: [],
                            createdAt: WireDate.string(), status: "done", seq: 1)
        try cache.apply(Event(seq: 1, conversationID: "chief", type: "message.start", message: reply))
        var sent = event(2, kind: "agent.messaged")
        sent.payload?.messageIDs = [reply.id, "future"]
        try cache.apply(sent)
        #expect(try cache.threads().first?.unreadCount == 1)
        let future = Message(id: "future", threadID: "chief", role: "assistant", parts: [],
                             createdAt: WireDate.string(), status: "done", seq: 3)
        try cache.apply(Event(seq: 3, conversationID: "chief", type: "message.start", message: future))
        try cache.apply(event(4, kind: "memory.saved"))
        try cache.apply(event(5, kind: "subagent.started"))
        let finished = event(6, kind: "subagent.finished")
        try cache.apply(finished)
        try cache.apply(finished)
        #expect(try cache.threads().first?.unreadCount == 2)
        try cache.markRead(threadID: "chief", seq: 6)
        #expect(try cache.threads().first?.unreadCount == 0)
    }

    @Test func boundsRetentionWithoutRevivingPeerBubbles() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let cache = try LocalCache(url: directory.appendingPathComponent("cache.sqlite"))
        try cache.save(agents: [], threads: [Thread(id: "chief", agentID: "chief")])
        let peer = Message(id: "peer", threadID: "chief", role: "assistant", parts: [], createdAt: WireDate.string(), status: "done", seq: 1)
        try cache.apply(Event(seq: 1, conversationID: "chief", type: "message.start", message: peer))
        var sent = event(2, kind: "agent.messaged")
        sent.payload?.messageIDs = [peer.id, "future"]
        try cache.apply(sent)
        for seq in 3...1003 { try cache.apply(event(Int64(seq))) }
        #expect(try cache.activityEvents(threadID: "chief").count == 1000)
        #expect(try cache.messages(threadID: "chief").isEmpty)
        let future = Message(id: "future", threadID: "chief", role: "assistant", parts: [], createdAt: WireDate.string(), status: "done", seq: 1004)
        try cache.apply(Event(seq: 1004, conversationID: "chief", type: "message.start", message: future))
        #expect(try cache.threads().first?.unreadCount == 1)
        let client = APIClient(baseURL: URL(string: "http://localhost")!)
        let store = await ChatStore(client: client, actor: LocalCacheActor(cache), threadID: "chief")
        await store.ready()
        #expect(await store.messages.isEmpty)
    }

    @Test func liveActivitiesHideLateReplies() async throws {
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let cache = try LocalCache(url: directory.appendingPathComponent("cache.sqlite"))
        let client = APIClient(baseURL: URL(string: "http://localhost")!)
        let store = await ChatStore(client: client, actor: LocalCacheActor(cache), threadID: "chief")
        await store.ready()
        var sent = event(1, kind: "agent.messaged", agents: ["chief", "aria"])
        sent.payload?.messageIDs = ["late"]
        try await store.receive(sent)
        try await store.receive(sent)
        let reply = Message(id: "late", threadID: "chief", role: "assistant", parts: [],
                            createdAt: WireDate.string(), status: "done", seq: 2)
        try await store.receive(Event(seq: 2, conversationID: "chief", type: "message.done", message: reply))
        #expect(await store.messages.isEmpty)
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
