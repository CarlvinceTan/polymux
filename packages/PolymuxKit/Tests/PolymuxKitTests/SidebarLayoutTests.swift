import Foundation
import Testing
@testable import PolymuxKit

@Suite(.serialized) @MainActor
struct SidebarLayoutTests {
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
        #expect(restored.sections.map(\.name) == ["Career", "Projects", "General"])
        #expect(restored.sections.first?.collapsed == true)
        #expect(restored.applying(to: rig.thread).sectionID == id)
        #expect(restored.issue == nil)
    }

    @Test func failedMoveRollsBackAndCanRetry() async throws {
        let rig = try await LayoutRig()
        defer { rig.clean() }
        await rig.layout.refresh()
        await LayoutProtocol.server.pauseMutation()
        let task = Task { await rig.layout.move(rig.thread, to: "general") }
        while await !LayoutProtocol.server.waiting { await Task.yield() }
        #expect(rig.layout.applying(to: rig.thread).sectionID == "general")
        #expect(rig.layout.isSaving)
        await LayoutProtocol.server.release(fail: true)
        await task.value
        #expect(rig.layout.applying(to: rig.thread).sectionID == "projects")
        #expect(rig.layout.issue?.contains("Test refusal") == true)
        await rig.layout.retry()
        #expect(rig.layout.applying(to: rig.thread).sectionID == "general")
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

    @Test func createdSectionKeepsItsServerIDWhenRefreshFails() async throws {
        let rig = try await LayoutRig()
        defer { rig.clean() }
        await rig.layout.refresh()
        await LayoutProtocol.server.failRefreshAfterMutation()
        let id = try #require(await rig.layout.create("Career"))
        #expect(id == "career")
        #expect(rig.layout.sections.contains { $0.id == id })
        #expect(rig.layout.issue?.contains("change was saved") == true)
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

    @Test func failedRenameRollsBackAndRetryPersists() async throws {
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
        await rig.layout.retry()
        let restored = SidebarLayout(client: rig.client, cache: rig.cache)
        await restored.load()
        #expect(restored.sections.first?.name == "Career")
        #expect(rig.layout.issue == nil)
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
        Task {
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

    func pauseMutation() { pauseWrite = true }
    func pauseRefresh() { pauseGet = true }
    func failRefreshAfterMutation() { failNextGet = true }
    func release(fail: Bool = false) {
        failWrite = fail
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
            return (409, Data("{\"code\":\"test\",\"message\":\"Test refusal\"}".utf8))
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
        var placement = state.placements[0]
        placement.pinned = fields["pinned"] as? Bool ?? false
        placement.pinOrder = placement.pinned ? 0 : nil
        if path.hasSuffix("/placement") {
            receivedNull = fields["sectionID"] is NSNull
            placement.sectionID = fields["sectionID"] as? String
            placement.sectionOrder = 1
        } else {
            placement.sectionID = nil
            placement.sectionOrder = nil
        }
        state.placements[0] = placement
        return (200, try encoder.encode(Thread(id: "chat", pinned: placement.pinned, pinOrder: placement.pinOrder,
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
