import Foundation
import Testing
@testable import PolymuxKit

@Suite(.serialized) @MainActor
struct SidebarOffMainTests {
    @Test func incomingEventPatchesTheRowInsideTheStateBudget() async throws {
        let rig = try await Rig()
        let beforeReads = rig.cache.threadReads
        let event = rig.event(seq: 2, threadID: "arlo", text: "Just in")
        rig.chat.onUpdate = { event, tap in
            rig.sidebar.note(event, at: tap)
            #expect(rig.cache.threadReads == beforeReads)
        }
        try await rig.chat.receive(event)
        let row = try #require(rig.sidebar.threads.first { $0.id == "arlo" })
        #expect(row.lastMessage?.text == "Just in")
        #expect(row.unreadCount == 1)
        #expect(rig.sidebar.threads.first?.id == "arlo")
        let span = try #require(rig.sidebar.trace.spans.first { $0.name == "sidebarRow" })
        #expect(span.budget == InstantUI.stateBudget)
        #expect(span.duration < InstantUI.stateBudget)
        #expect(span.withinBudget)
    }

    @Test func stalledRowReportsAMiss() async throws {
        let rig = try await Rig()
        rig.sidebar.rowStall = .milliseconds(70)
        let event = rig.event(seq: 2, threadID: "arlo", text: "Slow")
        rig.sidebar.note(event, at: ContinuousClock().now)
        let span = try #require(rig.sidebar.trace.spans.first { $0.name == "sidebarRow" })
        #expect(span.withinBudget == false)
        #expect(span.duration > InstantUI.stateBudget)
        #expect(rig.sidebar.trace.meetsBudgets == false)
        #expect(rig.sidebar.threads.first { $0.id == "arlo" }?.lastMessage?.text == "Slow")
    }

    @Test func liveSidebarWorkStaysOffTheMainQueue() async throws {
        let rig = try await Rig()
        let beforeReads = rig.cache.threadReads
        rig.chat.onUpdate = { event, tap in rig.sidebar.note(event, at: tap) }
        for index in 1...50 {
            let event = rig.event(seq: Int64(index + 1), threadID: "arlo", text: "Update \(index)")
            try await rig.chat.receive(event)
        }
        rig.sidebar.rename("arlo", to: "Renamed")
        #expect(rig.sidebar.threads.first { $0.id == "arlo" }?.title == "Renamed")
        await rig.chat.markRead()
        await rig.sidebar.settled()
        #expect(rig.sidebar.threads.first { $0.id == "arlo" }?.unreadCount == 0)
        #expect(await rig.sidebar.cacheActor.markCount == 1)
        #expect(await rig.sidebar.cacheActor.saveCount <= 5)
        #expect(await rig.sidebar.cacheActor.saveCount >= 1)
        #expect(rig.cache.threadReads - beforeReads < 50)
        #expect(try await rig.cache.offMain { try $0.threads() }.first { $0.id == "arlo" }?.title == "Renamed")
    }

    @Test func burstOfEventsSavesAtMostFiveTimes() async throws {
        let rig = try await Rig()
        try await noteBurst(rig)
        #expect(rig.sidebar.threads.first { $0.id == "arlo" }?.unreadCount == 100)
        await rig.sidebar.settled()
        let saves = await rig.sidebar.cacheActor.saveCount
        #expect(saves <= 5)
        #expect(saves >= 1)
        #expect(try await rig.cache.offMain { try $0.threads() }.first { $0.id == "arlo" }?.unreadCount == 100)
    }

    @Test func burstWithoutAGapSavesMoreThanFiveTimes() async throws {
        let rig = try await Rig()
        rig.sidebar.saveGap = .zero
        await rig.sidebar.cacheActor.setInstant(true)
        try await noteBurst(rig)
        await rig.sidebar.settled()
        #expect(await rig.sidebar.cacheActor.saveCount > 5)
    }

    @Test func renameShowsTheNewTitleOnTheSameTurn() async throws {
        let rig = try await Rig()
        rig.sidebar.rename("arlo", to: "Arlo West")
        #expect(rig.sidebar.threads.first { $0.id == "arlo" }?.title == "Arlo West")
        await CacheWork.untilSaved(rig.sidebar)
        #expect(rig.sidebar.threads.first { $0.id == "arlo" }?.title == "Arlo West")
        #expect(rig.sidebar.editIssues["arlo"] == nil)
        #expect(try await rig.cache.offMain { try $0.threads() }.first { $0.id == "arlo" }?.title == "Arlo West")
    }

    @Test func failedRenameRestoresThePreviousTitle() async throws {
        let rig = try await Rig()
        await rig.sidebar.cacheActor.setFailing(true)
        rig.sidebar.rename("arlo", to: "Arlo West")
        #expect(rig.sidebar.threads.first { $0.id == "arlo" }?.title == "Arlo West")
        await rig.sidebar.settled()
        #expect(rig.sidebar.threads.first { $0.id == "arlo" }?.title == "Arlo")
        #expect(rig.sidebar.editIssues["arlo"] == "Couldn’t save the new name. Retry")
    }

    @Test func renameStaysVisibleWhileTheSaveIsStalled() async throws {
        let rig = try await Rig()
        await rig.sidebar.cacheActor.parkNext()
        await rig.sidebar.cacheActor.setFailing(true)
        rig.sidebar.rename("arlo", to: "Arlo West")
        #expect(rig.sidebar.threads.first { $0.id == "arlo" }?.title == "Arlo West")
        while await !rig.sidebar.cacheActor.isParked { await Task.yield() }
        #expect(rig.sidebar.threads.first { $0.id == "arlo" }?.title == "Arlo West")
        #expect(rig.sidebar.editIssues["arlo"] == nil)
        await rig.sidebar.cacheActor.release()
        await rig.sidebar.settled()
        #expect(rig.sidebar.threads.first { $0.id == "arlo" }?.title == "Arlo")
        #expect(rig.sidebar.editIssues["arlo"] == "Couldn’t save the new name. Retry")
    }

    @Test func failedEditKeepsTheRenameAndRetriesTheSave() async throws {
        let rig = try await Rig()
        await rig.sidebar.cacheActor.setFailing(true)
        var thread = try #require(rig.sidebar.threads.first { $0.id == "arlo" })
        thread.title = "Arlo West"
        await rig.sidebar.edit(thread, delay: .seconds(3600))
        #expect(rig.sidebar.threads.first { $0.id == "arlo" }?.title == "Arlo West")
        #expect(rig.sidebar.editIssues["arlo"] == "Changes couldn’t be saved on this device.")
        await rig.sidebar.cacheActor.setFailing(false)
        await rig.sidebar.edit(thread, delay: .seconds(3600))
        #expect(rig.sidebar.editIssues["arlo"] == nil)
        #expect(try await rig.cache.offMain { try $0.threads() }.first { $0.id == "arlo" }?.title == "Arlo West")
    }

    @Test func streamedSaveFailureKeepsTheAgentError() async throws {
        let rig = try await Rig()
        await rig.sidebar.cacheActor.parkNext()
        await rig.sidebar.cacheActor.setFailing(true)
        let agent = Agent(id: "nova", name: "Nova")
        async let accepting: Void = rig.sidebar.accept(agent)
        while await !rig.sidebar.cacheActor.isParked { await Task.yield() }
        rig.sidebar.note(rig.event(seq: 3, threadID: "arlo", text: "While saving"))
        await rig.sidebar.cacheActor.release()
        await accepting
        #expect(rig.sidebar.agents.contains { $0.id == "nova" })
        #expect(rig.sidebar.error == "Couldn’t save this agent. Retry")
        await rig.sidebar.reload()
        #expect(rig.sidebar.agents.contains { $0.id == "nova" })
        #expect(rig.sidebar.error == "Couldn’t save this agent. Retry")
    }

    @Test func actorStallDoesNotBlockTheMainHeartbeat() async throws {
        let rig = try await Rig()
        await rig.sidebar.cacheActor.setStall(.milliseconds(250))
        let clock = ContinuousClock()
        let gaps = GapLog()
        let started = clock.now
        var previous = started
        async let stalled: Void = rig.sidebar.cacheActor.touch()
        await Task.yield()
        gaps.add(previous.duration(to: clock.now))
        previous = clock.now
        while started.duration(to: clock.now) < .milliseconds(100) {
            await Task.yield()
            let now = clock.now
            gaps.add(previous.duration(to: now))
            previous = now
        }
        try await stalled
        #expect(started.duration(to: clock.now) >= .milliseconds(100))
        #expect(gaps.max() < InstantUI.feedbackBudget)
    }

    @Test func blockedMainThreadMissesTheHeartbeat() {
        let clock = ContinuousClock()
        let gaps = GapLog()
        let started = clock.now
        while started.duration(to: clock.now) < .milliseconds(70) {}
        gaps.add(started.duration(to: clock.now))
        #expect(gaps.max() > InstantUI.feedbackBudget)
    }

    @Test func stalledHeartbeatReportsAMiss() {
        let trace = SignpostTrace()
        InstantUI.interval("heartbeat", budget: InstantUI.feedbackBudget, trace: trace, from: ContinuousClock().now) {
            let clock = ContinuousClock()
            let start = clock.now
            while start.duration(to: clock.now) < .milliseconds(70) {}
        }
        let span = trace.spans.first
        #expect(span?.withinBudget == false)
        #expect((span?.duration ?? .zero) > InstantUI.feedbackBudget)
        #expect(trace.meetsBudgets == false)
    }
}

/// One hundred row updates spread across about one second.
@MainActor
private func noteBurst(_ rig: Rig) async throws {
    let clock = ContinuousClock()
    let start = clock.now
    for index in 0..<100 {
        let deadline = start + .milliseconds(10 * index)
        let remaining = clock.now.duration(to: deadline)
        if remaining > .zero { try await Task.sleep(for: remaining) }
        rig.sidebar.note(rig.event(seq: Int64(index + 2), threadID: "arlo", text: "Burst \(index)"))
    }
}

@MainActor
private struct Rig {
    let cache: LocalCache
    let sidebar: SidebarStore
    let chat: ChatStore

    init() async throws {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent("polymux-sidebar-\(UUID())")
        let url = folder.appendingPathComponent("cache.sqlite")
        let cache = try await CacheWork.open(url)
        let arlo = Agent(id: "arlo", name: "Arlo")
        let milo = Agent(id: "milo", name: "Milo")
        let older = Message(id: "old", threadID: "arlo", role: "assistant", parts: [Part(kind: "text", text: "Earlier")],
                            createdAt: WireDate.string(Date(timeIntervalSince1970: 1_700_000_000)), status: "done", seq: 1)
        let newer = Message(id: "new", threadID: "milo", role: "assistant", parts: [Part(kind: "text", text: "Later")],
                            createdAt: WireDate.string(Date(timeIntervalSince1970: 1_700_000_600)), status: "done", seq: 1)
        try await cache.offMain { cache in
            try cache.save(agents: [arlo, milo], threads: [
                Thread(id: "arlo", agentID: "arlo", lastReadSeq: 1, lastSeq: 1, lastMessage: older, title: "Arlo"),
                Thread(id: "milo", agentID: "milo", lastReadSeq: 1, lastSeq: 1, lastMessage: newer, title: "Milo"),
            ])
            try cache.apply(Event(seq: 1, conversationID: "arlo", type: "message.start", message: older, createdAt: older.createdAt))
            try cache.apply(Event(seq: 1, conversationID: "milo", type: "message.start", message: newer, createdAt: newer.createdAt))
        }
        let client = APIClient(baseURL: URL(string: "http://127.0.0.1:1")!)
        let sidebar = SidebarStore(client: client, cache: cache)
        await sidebar.load()
        self.cache = cache
        self.sidebar = sidebar
        self.chat = ChatStore(client: client, actor: sidebar.cacheActor, threadID: "arlo", sidebar: sidebar)
        await self.chat.ready()
    }

    func event(seq: Int64, threadID: String, text: String) -> Event {
        let message = Message(id: "m-\(seq)", threadID: threadID, role: "assistant", parts: [Part(kind: "text", text: text)],
                              createdAt: WireDate.string(Date(timeIntervalSince1970: 1_700_000_600 + TimeInterval(seq))),
                              status: "done", seq: seq)
        return Event(seq: seq, conversationID: threadID, type: "message.start", message: message, createdAt: message.createdAt)
    }
}

@MainActor
private final class GapLog {
    private var gaps: [Duration] = []
    func add(_ gap: Duration) { gaps.append(gap) }
    func max() -> Duration { gaps.max() ?? .zero }
}
