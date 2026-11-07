import Foundation
import Testing
@testable import PolymuxKit

@Suite(.serialized) @MainActor
struct SidebarLayoutTests {
    @Test func unassignedExcludesPinsAndAssignedChats() async throws {
        let rig = try await LayoutRig()
        defer { rig.clean() }
        await LayoutProtocol.server.addUnassigned()
        await rig.layout.refresh()
        let id = SidebarLayout.unassignedID
        let assigned = Thread(id: "assigned", sectionID: "projects")
        let pinned = Thread(id: "pinned", pinned: true)
        let free = Thread(id: "free")
        let orphan = Thread(id: "orphan", sectionID: "deleted")
        #expect(rig.layout.chats(in: id, from: [assigned, pinned, free, orphan]).map(\.id) == ["free", "orphan"])
        #expect(!rig.layout.visibleSections(from: [assigned, pinned]).contains { $0.id == id })
        #expect(!rig.layout.visibleSections(from: []).contains { $0.id == id })
        #expect(rig.layout.visibleSections(from: [free]).contains { $0.id == id })
        #expect(!rig.layout.customSections.contains { $0.id == id })
    }

    @Test func movingChatsKeepsTheConversationOrder() async throws {
        let rig = try await LayoutRig()
        defer { rig.clean() }
        await rig.layout.refresh()
        let recent = Thread(id: "recent", sectionID: "projects", sectionOrder: 10)
        let older = Thread(id: "older", sectionID: "projects", sectionOrder: 0)
        #expect(rig.layout.chats(in: "projects", from: [recent, older]).map(\.id) == ["recent", "older"])
    }

    @Test func unassignedPersistsButCannotBeRenamedOrDeleted() async throws {
        let rig = try await LayoutRig()
        defer { rig.clean() }
        await LayoutProtocol.server.addUnassigned()
        await rig.layout.refresh()
        let id = SidebarLayout.unassignedID
        await rig.layout.shift(id, by: -1)
        await rig.layout.collapse(id, collapsed: true)
        await rig.layout.rename(id, to: "Changed")
        #expect(await !rig.layout.delete(id, chats: true))
        let restored = SidebarLayout(client: rig.client, cache: rig.cache)
        await restored.load()
        #expect(restored.sections.map(\.id) == ["projects", id, "general"])
        #expect(restored.sections[1].name == "Unassigned")
        #expect(restored.sections[1].collapsed)
        #expect(restored.visibleSections(from: [Thread(id: "free")]).contains { $0.id == id })
        await rig.layout.shift("general", by: -1, among: rig.layout.visibleSections(from: [rig.thread]))
        #expect(rig.layout.customSections.map(\.id) == ["general", "projects"])
    }

    @Test func sectionsAndPlacementSurviveRelaunch() async throws {
        let rig = try await LayoutRig()
        defer { rig.clean() }
        await rig.layout.refresh()
        let id = try #require(await rig.layout.create("Career"))
        await rig.layout.reorder(id, before: "projects")
        await rig.layout.collapse(id, collapsed: true)
        await rig.layout.move(rig.thread, to: id)
        let restored = SidebarLayout(client: rig.client, cache: rig.cache)
        await restored.load()
        #expect(restored.customSections.map(\.name) == ["Career", "Projects", "General"])
        #expect(restored.visibleSections(from: [Thread(id: "free")]).last?.id == SidebarLayout.unassignedID)
        #expect(restored.sections.first?.collapsed == true)
        #expect(restored.applying(to: rig.thread).sectionID == id)
        #expect(restored.issue == nil)
    }

    @Test func failedMoveRollsBackAndResendsOnReconnect() async throws {
        let rig = try await LayoutRig()
        defer { rig.clean() }
        await rig.layout.refresh()
        await LayoutProtocol.server.pauseMutation()
        let task = Task { await rig.layout.move(rig.thread, to: "general") }
        while await !LayoutProtocol.server.waiting { await Task.yield() }
        #expect(rig.layout.applying(to: rig.thread).sectionID == "general")
        #expect(rig.layout.isSaving)
        await LayoutProtocol.server.release(fail: true, status: 503)
        await task.value
        #expect(rig.layout.applying(to: rig.thread).sectionID == "projects")
        #expect(rig.layout.issue == nil)
        // The next successful refresh sends the change again without being asked.
        await rig.layout.refresh()
        #expect(rig.layout.applying(to: rig.thread).sectionID == "general")
        #expect(rig.layout.issue == nil)
    }

    @Test func retriedChangeShowsALineOnlyOnceRefused() async throws {
        let rig = try await LayoutRig()
        defer { rig.clean() }
        await rig.layout.refresh()
        await LayoutProtocol.server.pauseMutation()
        let move = Task { await rig.layout.move(rig.thread, to: "general") }
        while await !LayoutProtocol.server.waiting { await Task.yield() }
        await LayoutProtocol.server.release(fail: true, status: 503)
        await move.value
        #expect(rig.layout.issue == nil)
        await LayoutProtocol.server.pauseMutation()
        let retry = Task { await rig.layout.refresh() }
        while await !LayoutProtocol.server.waiting { await Task.yield() }
        await LayoutProtocol.server.release(fail: true)
        await retry.value
        #expect(rig.layout.applying(to: rig.thread).sectionID == "projects")
        #expect(rig.layout.issue?.contains("Test refusal") == true)
        // Refused for good: the line clears and the change isn't sent again.
        await rig.layout.refresh()
        #expect(rig.layout.applying(to: rig.thread).sectionID == "projects")
        #expect(rig.layout.issue == nil)
    }

    @Test func pinsAndMovesStayExclusive() async throws {
        let rig = try await LayoutRig()
        defer { rig.clean() }
        await rig.layout.refresh()
        await rig.layout.pin(rig.thread, pinned: true)
        let pinned = rig.layout.applying(to: rig.thread)
        #expect(pinned.pinned == true)
        #expect(pinned.sectionID == nil)
        await rig.layout.move(pinned, to: "general")
        let moved = rig.layout.applying(to: rig.thread)
        #expect(moved.pinned == false)
        #expect(moved.pinOrder == nil)
        #expect(moved.sectionID == "general")
        await rig.layout.move(moved, to: nil)
        #expect(await LayoutProtocol.server.receivedNull)
        #expect(rig.layout.applying(to: rig.thread).sectionID == nil)
    }

    @Test func pinReorderingPersistsAndFailedDropsRollBack() async throws {
        let rig = try await LayoutRig()
        defer { rig.clean() }
        await LayoutProtocol.server.addPins()
        await rig.layout.refresh()
        let moving = Thread(id: "third", pinned: true, pinOrder: 2)
        await LayoutProtocol.server.pauseMutation()
        let task = Task { await rig.layout.pin(moving, pinned: true, before: "second") }
        while await !LayoutProtocol.server.waiting { await Task.yield() }
        #expect(rig.layout.applying(to: moving).pinOrder == 1)
        await LayoutProtocol.server.release(fail: true, status: 503)
        await task.value
        #expect(rig.layout.applying(to: moving).pinOrder == 2)
        await rig.layout.refresh()
        #expect(rig.layout.applying(to: moving).pinOrder == 1)
        await rig.layout.pin(moving, pinned: true, before: "first")
        #expect(rig.layout.applying(to: moving).pinOrder == 0)
        await rig.layout.pin(moving, pinned: true)
        #expect(rig.layout.applying(to: moving).pinOrder == 2)
        await rig.layout.pin(rig.thread, pinned: true, before: "second")
        #expect(rig.layout.applying(to: rig.thread).pinOrder == 1)
        #expect(rig.layout.applying(to: rig.thread).sectionID == nil)
        let restored = SidebarLayout(client: rig.client, cache: rig.cache)
        await restored.load()
        #expect(restored.applying(to: rig.thread).pinOrder == 1)
        #expect(restored.applying(to: moving).pinOrder == 3)
    }

    @Test func createdSectionKeepsItsServerIDWhenRefreshFails() async throws {
        let rig = try await LayoutRig()
        defer { rig.clean() }
        await rig.layout.refresh()
        await LayoutProtocol.server.failRefreshAfterMutation()
        let id = try #require(await rig.layout.create("Career"))
        #expect(id == "career")
        #expect(rig.layout.sections.contains { $0.id == id })
        // The list settles on the next refresh, so a saved change needs no note.
        #expect(rig.layout.issue == nil)
        await rig.layout.refresh()
        #expect(rig.layout.sections.contains { $0.id == id })
        #expect(rig.layout.issue == nil, "An unavailable server must not add a second connection error above cached chats")
        let restored = SidebarLayout(client: rig.client, cache: rig.cache)
        await restored.load()
        #expect(restored.sections.contains { $0.id == id })
    }

    @Test func creationExposesDraftBeforeAcceptingServerID() async throws {
        let rig = try await LayoutRig()
        defer { rig.clean() }
        await rig.layout.refresh()
        await LayoutProtocol.server.pauseMutation()
        var accepted: String?
        let task = Task {
            await rig.layout.create("Career", id: "draft", accepted: { accepted = $0 })
        }
        while await !LayoutProtocol.server.waiting { await Task.yield() }
        #expect(rig.layout.sections.last?.id == "draft")
        #expect(accepted == nil)
        await LayoutProtocol.server.release()
        #expect(await task.value == "career")
        #expect(accepted == "career")
        #expect(!rig.layout.sections.contains { $0.id == "draft" })
    }

    @Test func refusedRenameRollsBackWithoutResending() async throws {
        let rig = try await LayoutRig()
        defer { rig.clean() }
        await rig.layout.refresh()
        await LayoutProtocol.server.pauseMutation()
        let task = Task { await rig.layout.rename("projects", to: "Career") }
        while await !LayoutProtocol.server.waiting { await Task.yield() }
        #expect(rig.layout.sections.first?.name == "Career")
        await LayoutProtocol.server.release(fail: true)
        await task.value
        #expect(rig.layout.sections.first?.name == "Projects")
        #expect(rig.layout.issue?.contains("Test refusal") == true)
        // A refused change would fail again, so a refresh must not resend it.
        await rig.layout.refresh()
        #expect(rig.layout.sections.first?.name == "Projects")
    }

    @Test func staleRefreshCannotUndoANewerMutation() async throws {
        let rig = try await LayoutRig()
        defer { rig.clean() }
        await rig.layout.refresh()
        await LayoutProtocol.server.pauseRefresh()
        let refresh = Task { await rig.layout.refresh() }
        while await !LayoutProtocol.server.waiting { await Task.yield() }
        await rig.layout.pin(rig.thread, pinned: true)
        await LayoutProtocol.server.release()
        await refresh.value
        #expect(rig.layout.applying(to: rig.thread).pinned == true)
    }

    @Test func collapsingDoesNotLockTheSidebar() async throws {
        let rig = try await LayoutRig()
        defer { rig.clean() }
        await rig.layout.refresh()
        await LayoutProtocol.server.pauseMutation()
        let task = Task { await rig.layout.collapse("projects", collapsed: true) }
        while await !LayoutProtocol.server.waiting { await Task.yield() }
        #expect(rig.layout.sections.first?.collapsed == true)
        #expect(!rig.layout.isSaving)
        await LayoutProtocol.server.release()
        await task.value
        #expect(rig.layout.sections.first?.collapsed == true)
    }

    @Test func deletingASectionReturnsItsChatsAtOnce() async throws {
        let rig = try await LayoutRig()
        defer { rig.clean() }
        await rig.layout.refresh()
        await LayoutProtocol.server.pauseMutation()
        let task = Task { await rig.layout.delete("projects", chats: false) }
        while await !LayoutProtocol.server.waiting { await Task.yield() }
        #expect(rig.layout.sections.map(\.id) == ["general"])
        #expect(rig.layout.applying(to: rig.thread).sectionID == nil)
        await LayoutProtocol.server.release()
        #expect(await task.value)
        #expect(rig.layout.sections.map(\.id) == ["general"])
        #expect(rig.layout.applying(to: rig.thread).sectionID == nil)
    }

    @Test func sectionCanMoveToTheEnd() async throws {
        let rig = try await LayoutRig()
        defer { rig.clean() }
        await rig.layout.refresh()
        await rig.layout.reorder("projects", before: nil)
        #expect(rig.layout.sections.map(\.id) == ["general", "projects"])
        await rig.layout.shift("projects", by: -1)
        #expect(rig.layout.sections.map(\.id) == ["projects", "general"])
    }
}

@MainActor private struct LayoutRig {
    let folder: URL
    let client: APIClient
    let cache: LocalCacheActor
    let layout: SidebarLayout
    let thread = Thread(id: "chat", sectionID: "projects", sectionOrder: 0)

    init() async throws {
        folder = FileManager.default.temporaryDirectory.appending(path: "sidebar-layout-\(UUID())")
        let file = folder.appending(path: "cache.sqlite")
        cache = try await Task.detached { LocalCacheActor(try LocalCache(url: file)) }.value
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [LayoutProtocol.self]
        client = APIClient(baseURL: URL(string: "http://sidebar-layout.test")!,
                           session: URLSession(configuration: configuration), token: "")
        layout = SidebarLayout(client: client, cache: cache)
        await LayoutProtocol.server.reset()
    }

    func clean() { try? FileManager.default.removeItem(at: folder) }
}

private final class LayoutProtocol: URLProtocol, @unchecked Sendable {
    static let server = LayoutServer()
    override class func canInit(with request: URLRequest) -> Bool { request.url?.host == "sidebar-layout.test" }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        Task { @Sendable [self] in
            do {
                let (status, data) = try await Self.server.respond(request)
                let response = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: nil,
                                               headerFields: ["Content-Type": "application/json"])!
                client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
                client?.urlProtocol(self, didLoad: data)
                client?.urlProtocolDidFinishLoading(self)
            } catch { client?.urlProtocol(self, didFailWithError: error) }
        }
    }
    override func stopLoading() {}
}

private actor LayoutServer {
    private var state = SidebarSnapshot()
    private var gate: CheckedContinuation<Void, Never>?
    private var pauseGet = false
    private var pauseWrite = false
    private var failWrite = false
    private var failStatus = 409
    private var failGet = false
    private var failNextGet = false
    private(set) var receivedNull = false
    var waiting: Bool { gate != nil }

    func reset() {
        state = SidebarSnapshot(cursor: 1,
            sections: [Section(id: "projects", name: "Projects", order: 0), Section(id: "general", name: "General", order: 1)],
            placements: [SidebarPlacement(threadID: "chat", sectionID: "projects", sectionOrder: 0)])
        pauseGet = false
        pauseWrite = false
        failWrite = false
        failGet = false
        failNextGet = false
        receivedNull = false
    }

    func addUnassigned() {
        state.sections.append(Section(id: SidebarLayout.unassignedID, name: "Unassigned", order: 2))
    }

    func addPins() {
        state.placements += ["first", "second", "third"].enumerated().map {
            SidebarPlacement(threadID: $0.element, pinned: true, pinOrder: Double($0.offset))
        }
    }

    func pauseMutation() { pauseWrite = true }
    func pauseRefresh() { pauseGet = true }
    func failRefreshAfterMutation() { failNextGet = true }
    func release(fail: Bool = false, status: Int = 409) {
        failWrite = fail
        failStatus = status
        gate?.resume()
        gate = nil
    }

    func respond(_ request: URLRequest) async throws -> (Int, Data) {
        let encoder = JSONEncoder()
        if request.httpMethod == "GET" {
            let captured = state
            if pauseGet {
                pauseGet = false
                await withCheckedContinuation { gate = $0 }
            }
            if failGet { return (503, Data("{\"code\":\"test\",\"message\":\"Test offline\"}".utf8)) }
            return (200, try encoder.encode(captured))
        }
        if pauseWrite {
            pauseWrite = false
            await withCheckedContinuation { gate = $0 }
        }
        if failWrite {
            failWrite = false
            return (failStatus, Data("{\"code\":\"test\",\"message\":\"Test refusal\"}".utf8))
        }
        if request.httpMethod == "DELETE" {
            let id = request.url!.lastPathComponent
            state.cursor += 1
            state.sections.removeAll { $0.id == id }
            for index in state.placements.indices where state.placements[index].sectionID == id {
                state.placements[index].sectionID = nil
                state.placements[index].sectionOrder = nil
            }
            return (200, Data("{}".utf8))
        }
        let fields = try JSONSerialization.jsonObject(with: body(request)) as! [String: Any]
        let path = request.url!.path
        state.cursor += 1
        if failNextGet { failGet = true; failNextGet = false }
        if path == "/sections" {
            let name = fields["name"] as! String
            let section = Section(id: name.lowercased(), name: name, order: Double(state.sections.count))
            state.sections.append(section)
            return (201, try encoder.encode(section))
        }
        if path.hasPrefix("/sections/") {
            let index = state.sections.firstIndex { $0.id == request.url!.lastPathComponent }!
            if let name = fields["name"] as? String { state.sections[index].name = name }
            if let collapsed = fields["collapsed"] as? Bool { state.sections[index].collapsed = collapsed }
            return (200, try encoder.encode(state.sections[index]))
        }
        if path == "/sidebar/reorder" {
            let index = state.sections.firstIndex { $0.id == fields["id"] as? String }!
            let section = state.sections.remove(at: index)
            let target = state.sections.firstIndex { $0.id == fields["beforeID"] as? String } ?? state.sections.count
            state.sections.insert(section, at: target)
            for index in state.sections.indices { state.sections[index].order = Double(index) }
            return (200, Data("{}".utf8))
        }
        let id = request.url!.pathComponents[2]
        let index = state.placements.firstIndex { $0.threadID == id }!
        var placement = state.placements[index]
        placement.pinned = fields["pinned"] as? Bool ?? false
        placement.pinOrder = nil
        if placement.pinned {
            guard fields["sectionID"] == nil else { return (422, Data()) }
            var pins = state.placements.filter { $0.pinned && $0.threadID != id }
                .sorted { ($0.pinOrder ?? 0) < ($1.pinOrder ?? 0) }
            let target: Int
            if let before = fields["beforeID"] as? String {
                target = pins.firstIndex { $0.threadID == before }!
            } else if let after = fields["afterID"] as? String {
                target = pins.firstIndex { $0.threadID == after }! + 1
            } else { target = pins.count }
            placement.sectionID = nil
            placement.sectionOrder = nil
            pins.insert(placement, at: target)
            for (order, pin) in pins.enumerated() {
                let position = state.placements.firstIndex { $0.threadID == pin.threadID }!
                state.placements[position] = pin
                state.placements[position].pinOrder = Double(order)
            }
            placement = state.placements[index]
        } else {
            receivedNull = fields["sectionID"] is NSNull
            placement.sectionID = fields["sectionID"] as? String
            placement.sectionOrder = 1
            state.placements[index] = placement
        }
        return (200, try encoder.encode(Thread(id: id, pinned: placement.pinned, pinOrder: placement.pinOrder,
                                               sectionID: placement.sectionID, sectionOrder: placement.sectionOrder)))
    }

    private func body(_ request: URLRequest) -> Data {
        if let body = request.httpBody { return body }
        guard let stream = request.httpBodyStream else { return Data() }
        stream.open()
        defer { stream.close() }
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 2048)
        while stream.hasBytesAvailable {
            let count = stream.read(&buffer, maxLength: buffer.count)
            if count <= 0 { break }
            data.append(contentsOf: buffer.prefix(count))
        }
        return data
    }
}
