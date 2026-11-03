import Foundation
import Testing
@testable import PolymuxKit

@Suite
struct TaskThreadTests {
    private func task(_ id: String, status: String = TaskInfoStatus.open, pinnedAt: String? = nil, at: String) -> PolymuxKit.Thread {
        PolymuxKit.Thread(id: id, agentID: "arlo", title: id, agentIDs: ["arlo"],
               task: TaskInfo(description: id, status: status, pinnedAt: pinnedAt, updatedAt: at))
    }

    @Test func tasksStayOutOfTheChatList() async throws {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent("polymux-tasks-\(UUID())")
        defer { try? FileManager.default.removeItem(at: folder) }
        let cache = try LocalCache(url: folder.appendingPathComponent("cache.sqlite"))
        let arlo = Agent(id: "arlo", name: "Arlo")
        let report = task("task-report", at: "2026-10-23T12:00:00Z")
        try cache.save(agents: [arlo], threads: [report, PolymuxKit.Thread(id: "arlo", agentID: "arlo")])
        let sidebar = await SidebarStore(client: APIClient(baseURL: URL(string: "http://127.0.0.1:1")!), cache: cache)
        await sidebar.load()
        #expect(await sidebar.chats.map(\.id) == ["arlo"])
        #expect(await sidebar.thread(for: arlo)?.id == "arlo")
        #expect(await sidebar.groups.isEmpty)
        #expect(await sidebar.tasks(of: "arlo").map(\.id) == ["task-report"])
        await #expect(throws: (any Error).self) {
            try await sidebar.updateTask("task-report", status: TaskInfoStatus.done)
        }
        #expect(await sidebar.tasks(of: "arlo").first?.task?.status == TaskInfoStatus.open)
    }

    @Test func pinnedComeFirstThenUnfinishedThenFinished() {
        let ordered = SidebarStore.ordered([
            task("done", status: TaskInfoStatus.done, at: "2026-10-23T12:09:00Z"),
            task("older", at: "2026-10-23T12:01:00Z"),
            task("newer", at: "2026-10-23T12:02:00Z"),
            task("pin-b", pinnedAt: "2026-10-23T12:04:00Z", at: "2026-10-23T12:00:00Z"),
            task("pin-a", status: TaskInfoStatus.done, pinnedAt: "2026-10-23T12:03:00Z", at: "2026-10-23T12:00:00Z"),
        ])
        #expect(ordered.map(\.id) == ["pin-a", "pin-b", "newer", "older", "done"])
    }

    @Test func aFinishedTaskIsNotWorking() {
        var thread = task("report", at: "2026-10-23T12:00:00Z")
        thread.task?.workers = 1
        #expect(thread.isWorking)
        thread.task?.status = TaskInfoStatus.done
        #expect(!thread.isWorking)
    }
}
