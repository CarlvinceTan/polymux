import Foundation
import Testing
@testable import PolymuxKit

@Suite
struct OrderTests {
    @Test func agentsFollowTheirLatestMessage() async throws {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent("polymux-order-\(UUID())")
        defer { try? FileManager.default.removeItem(at: folder) }
        let cache = try LocalCache(url: folder.appendingPathComponent("cache.sqlite"))
        let arlo = Agent(id: "arlo", name: "Arlo")
        let milo = Agent(id: "milo", name: "Milo")
        let cleo = Agent(id: "cleo", name: "Cleo")
        let older = message(id: "older", thread: "arlo", at: 1_700_000_000)
        let newer = message(id: "newer", thread: "milo", at: 1_700_000_600)
        try cache.save(agents: [arlo, milo, cleo], threads: [
            Thread(id: "arlo", agentID: "arlo", lastSeq: 1, lastMessage: older),
            Thread(id: "milo", agentID: "milo", lastSeq: 1, lastMessage: newer),
            Thread(id: "cleo", agentID: "cleo"),
        ])
        let sidebar = await SidebarStore(client: APIClient(baseURL: URL(string: "http://127.0.0.1:1")!), cache: cache)
        await sidebar.load()
        let forward = await sidebar.recentFirst([arlo, milo, cleo]).map(\.name)
        let reversed = await sidebar.recentFirst([cleo, milo, arlo]).map(\.name)
        #expect(forward == ["Milo", "Arlo", "Cleo"])
        #expect(reversed == ["Milo", "Arlo", "Cleo"])
    }

    @Test func threadsWithoutMessagesFallBackToName() async throws {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent("polymux-order-\(UUID())")
        defer { try? FileManager.default.removeItem(at: folder) }
        let cache = try LocalCache(url: folder.appendingPathComponent("cache.sqlite"))
        let wren = Agent(id: "wren", name: "Wren")
        let arlo = Agent(id: "arlo", name: "Arlo")
        try cache.save(agents: [wren, arlo], threads: [
            Thread(id: "wren", agentID: "wren"),
            Thread(id: "arlo", agentID: "arlo"),
        ])
        let sidebar = await SidebarStore(client: APIClient(baseURL: URL(string: "http://127.0.0.1:1")!), cache: cache)
        await sidebar.load()
        let names = await sidebar.recentFirst([wren, arlo]).map(\.name)
        #expect(names == ["Arlo", "Wren"])
    }

    private func message(id: String, thread: String, at seconds: TimeInterval) -> Message {
        Message(id: id, threadID: thread, role: "assistant", parts: [Part(kind: "text", text: id)],
                createdAt: WireDate.string(Date(timeIntervalSince1970: seconds)), status: "done", seq: 1)
    }
}
