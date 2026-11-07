import Foundation
import Testing
@testable import PolymuxKit

@MainActor struct ProfileStoreTests {
    @Test func sectionsPublishIndependently() async throws {
        let fixture = try await Fixture(holding: ["a", "configuration"])
        defer { fixture.close() }
        let refresh = Task { await fixture.store.refresh() }
        try await eventually { fixture.store.routines != nil && fixture.store.connections != nil }
        #expect(fixture.store.routines?.first?.prompt == "Scheduled check")
        #expect(fixture.store.agent == nil)
        #expect(fixture.store.configuration == nil)
        let saved: ProfileSnapshot? = await fixture.cache.snapshot("profile/a")
        #expect(saved?.routines?.first?.id == "routine")
        await fixture.backend.release()
        await refresh.value
        #expect(fixture.store.prompt == "Server instructions")
        #expect(fixture.store.configuration?.skills == ["report"])
    }

    @Test func cachedContentPrecedesSlowRefresh() async throws {
        let fixture = try await Fixture(holding: ["a", "routines", "configuration", "mcp"])
        defer { fixture.close() }
        let routine = Routine(id: "cached", agentID: "a", prompt: "Cached schedule")
        await fixture.cache.saveSnapshot("profile/a", ProfileSnapshot(prompt: "Cached instructions", routines: [routine],
            configuration: AgentConfiguration(skills: ["cached"]), connections: [MCPConnection(name: "Cached connection")]))
        await fixture.store.restore()
        let refresh = Task { await fixture.store.refresh() }
        try await eventually { await fixture.backend.count("routines") == 1 }
        #expect(fixture.store.prompt == "Cached instructions")
        #expect(fixture.store.routines == [routine])
        #expect(fixture.store.configuration?.skills == ["cached"])
        #expect(fixture.store.connections?.first?.name == "Cached connection")
        await fixture.backend.release()
        await refresh.value
        #expect(fixture.store.routines?.first?.id == "routine")
    }

    @Test func refreshKeepsFailuresAndAcceptsEmpty() async throws {
        let fixture = try await Fixture()
        defer { fixture.close() }
        await fixture.store.refresh()
        await fixture.backend.setOffline(true)
        await fixture.store.refresh(maxAge: .zero)
        #expect(fixture.store.routines?.first?.id == "routine")
        #expect(fixture.store.prompt == "Server instructions")
        #expect(fixture.store.configuration?.skills == ["report"])
        #expect(fixture.store.connections?.first?.name == "Calendar")
        #expect(fixture.store.issue == nil)
        let reopened = ProfileStore(agentID: "a", client: fixture.client, cache: fixture.cache)
        await reopened.restore()
        #expect(reopened.routines == fixture.store.routines)
        #expect(reopened.prompt == fixture.store.prompt)
        await fixture.backend.setOffline(false)
        await fixture.backend.setRoutines([])
        await fixture.store.refresh(maxAge: .zero)
        #expect(fixture.store.routines == [])
        let next = ProfileStore(agentID: "a", client: fixture.client, cache: fixture.cache)
        await next.restore()
        #expect(next.routines == [])
        let other = ProfileStore(agentID: "b", client: fixture.client, cache: fixture.cache)
        await other.restore()
        #expect(other.routines == nil)
    }

    @Test func overlappingRefreshesShareRequests() async throws {
        let fixture = try await Fixture(holding: ["routines"])
        defer { fixture.close() }
        let first = Task { await fixture.store.refresh() }
        let second = Task { await fixture.store.refresh() }
        try await eventually { await fixture.backend.count("routines") == 1 }
        first.cancel()
        await fixture.backend.release()
        await first.value
        await second.value
        await fixture.store.refresh()
        #expect(await fixture.backend.count("routines") == 1)
        #expect(await fixture.backend.count("a") == 1)
        #expect(fixture.store.routines?.count == 1)
    }

    @Test func cancellationRejectsStaleRefresh() async throws {
        let fixture = try await Fixture()
        defer { fixture.close() }
        await fixture.store.refresh()
        let routine = try #require(fixture.store.routines?.first)
        await fixture.backend.hold("routines")
        let refresh = Task { await fixture.store.refresh(maxAge: .zero) }
        try await eventually { await fixture.backend.count("routines") == 2 }
        await fixture.backend.hold("DELETE")
        let cancel = Task { await fixture.store.cancel(routine) }
        try await eventually { fixture.store.routines == [] }
        #expect(fixture.store.cancelIssue == nil)
        await fixture.backend.release()
        await cancel.value
        await refresh.value
        #expect(fixture.store.routines == [])
        let reopened = ProfileStore(agentID: "a", client: fixture.client, cache: fixture.cache)
        await reopened.restore()
        #expect(reopened.routines == [])
    }

    @Test func failedCancellationRollsBack() async throws {
        let fixture = try await Fixture()
        defer { fixture.close() }
        await fixture.store.refresh()
        let routine = try #require(fixture.store.routines?.first)
        await fixture.backend.setOffline(true)
        await fixture.store.cancel(routine)
        #expect(fixture.store.routines == [routine])
        #expect(fixture.store.cancelIssue == "Not cancelled yet.")
        let saved: ProfileSnapshot? = await fixture.cache.snapshot("profile/a")
        #expect(saved?.routines == [routine])
    }

    @Test func savedEditsRejectStaleRefresh() async throws {
        let fixture = try await Fixture(holding: ["a"])
        defer { fixture.close() }
        let refresh = Task { await fixture.store.refresh() }
        try await eventually { await fixture.backend.count("a") == 1 }
        await fixture.store.accept(AgentProfile(agent: Agent(id: "a", name: "Edited"), prompt: "Edited instructions"))
        await fixture.backend.release()
        await refresh.value
        #expect(fixture.store.agent?.name == "Edited")
        #expect(fixture.store.prompt == "Edited instructions")
    }

    private func eventually(_ condition: () async -> Bool) async throws {
        let deadline = ContinuousClock.now.advanced(by: .seconds(3))
        while !(await condition()), ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(5)) }
        #expect(await condition())
    }
}

@MainActor private struct Fixture {
    let folder: URL
    let backend: ProfileBackend
    let client: APIClient
    let cache: LocalCacheActor
    let store: ProfileStore
    let host: String

    init(holding: Set<String> = []) async throws {
        folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        cache = LocalCacheActor(try await CacheWork.open(folder.appendingPathComponent("cache.db")))
        backend = ProfileBackend(holding: holding)
        host = UUID().uuidString.lowercased() + ".profile.test"
        ProfileProtocol.lock.withLock { [host, backend] in ProfileProtocol.backends[host] = backend }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [ProfileProtocol.self]
        client = APIClient(baseURL: URL(string: "http://" + host)!, session: URLSession(configuration: configuration), token: "")
        store = ProfileStore(agentID: "a", client: client, cache: cache)
    }

    func close() {
        Task { await backend.release() }
        _ = ProfileProtocol.lock.withLock { ProfileProtocol.backends.removeValue(forKey: host) }
        try? FileManager.default.removeItem(at: folder)
    }
}

private actor ProfileBackend {
    private var holding: Set<String>
    private var waiters: [CheckedContinuation<Void, Never>] = []
    private var calls: [String: Int] = [:]
    private var offline = false
    private var routines = [Routine(id: "routine", agentID: "a", prompt: "Scheduled check")]

    init(holding: Set<String>) { self.holding = holding }
    func count(_ path: String) -> Int { calls[path, default: 0] }
    func hold(_ path: String) { holding.insert(path) }
    func setOffline(_ value: Bool) { offline = value }
    func setRoutines(_ value: [Routine]) { routines = value }
    func release() {
        holding.removeAll()
        let waiting = waiters
        waiters.removeAll()
        for waiter in waiting { waiter.resume() }
    }

    func response(_ request: URLRequest) async throws -> Data {
        let path = request.httpMethod == "DELETE" ? "DELETE" : request.url!.lastPathComponent
        calls[path, default: 0] += 1
        let data: Data
        switch path {
        case "routines": data = try JSONEncoder().encode(routines)
        case "configuration": data = try JSONEncoder().encode(AgentConfiguration(skills: ["report"]))
        case "mcp": data = try JSONEncoder().encode([MCPConnection(name: "Calendar")])
        case "DELETE":
            routines = []
            data = try JSONEncoder().encode(DeleteResponse(deleted: true))
        default: data = try JSONEncoder().encode(AgentProfile(agent: Agent(id: "a", name: "A"), prompt: "Server instructions"))
        }
        if holding.contains(path) { await withCheckedContinuation { waiters.append($0) } }
        if offline { throw URLError(.notConnectedToInternet) }
        return data
    }
}

private final class ProfileProtocol: URLProtocol, @unchecked Sendable {
    static let lock = NSLock()
    nonisolated(unsafe) static var backends: [String: ProfileBackend] = [:]
    private let taskLock = NSLock()
    private var worker: Task<Void, Never>?

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let task = Task { @Sendable [self] in
            do {
                let backend = Self.lock.withLock { Self.backends[request.url!.host!]! }
                let data = try await backend.response(request)
                try Task.checkCancellation()
                let response = HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: nil)!
                client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
                client?.urlProtocol(self, didLoad: data)
                client?.urlProtocolDidFinishLoading(self)
            } catch { client?.urlProtocol(self, didFailWithError: error) }
        }
        taskLock.withLock { self.worker = task }
    }
    override func stopLoading() { taskLock.withLock { worker?.cancel() } }
}
