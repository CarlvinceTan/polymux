import Foundation
import Testing
@testable import PolymuxKit

@MainActor struct ResponsiveStoreTests {
    @Test func providerSectionsShareWorkAndPublishIndependently() async throws {
        let rig = try await ResponsiveRig(holding: ["GET /providers"])
        defer { rig.close() }
        await rig.cache.saveSnapshot("providers", [Provider(id: "cached", name: "Cached")])
        let store = ProviderStore(client: rig.client, cache: rig.cache)
        await store.restore()
        let first = Task { await store.refresh() }
        let second = Task { await store.refreshProviders() }
        try await eventually { store.options?.first?.name == "Catalogue" }
        #expect(store.providers?.first?.name == "Cached")
        #expect(await rig.backend.count("GET /providers") == 1)
        await rig.backend.release()
        await first.value
        await second.value
        #expect(store.providers?.first?.name == "Server")
        try await eventually {
            let saved: [Provider]? = await rig.cache.snapshot("providers")
            return saved?.first?.name == "Server"
        }
        let saved: [Provider]? = await rig.cache.snapshot("providers")
        #expect(saved?.first?.headers == nil)
        let reopened = ProviderStore(client: rig.client, cache: rig.cache)
        await reopened.restore()
        #expect(reopened.providers == saved)
        #expect(reopened.options == store.options)
    }

    @Test func providerMutationRejectsStaleRefresh() async throws {
        let rig = try await ResponsiveRig(holding: ["GET /providers"])
        defer { rig.close() }
        let store = ProviderStore(client: rig.client, cache: rig.cache)
        let refresh = Task { await store.refreshProviders() }
        try await eventually { await rig.backend.count("GET /providers") == 1 }
        store.begin()
        store.accept([Provider(id: "provider", name: "Edited")])
        store.end()
        await rig.backend.release()
        await refresh.value
        #expect(store.providers?.first?.name == "Edited")
        await rig.backend.setOffline(true)
        await store.refreshProviders(force: true)
        #expect(store.providers?.first?.name == "Edited")
        #expect(store.issue == nil)
    }

    @Test func deviceEditsPersistAndRejectOldRefresh() async throws {
        let rig = try await ResponsiveRig(holding: ["GET /network"])
        defer { rig.close() }
        await rig.cache.saveSnapshot("network", ResponsiveBackend.network)
        let store = NetworkStore(client: rig.client, cache: rig.cache)
        await store.restore()
        let device = try #require(store.value?.devices.first)
        let refresh = Task { await store.refresh() }
        try await eventually { await rig.backend.count("GET /network") == 1 }
        await store.rename(device, label: "Edited Mac")
        #expect(store.value?.devices.first?.title == "Edited Mac")
        await rig.backend.release()
        await refresh.value
        #expect(store.value?.devices.first?.title == "Edited Mac")
        let reopened = NetworkStore(client: rig.client, cache: rig.cache)
        await reopened.restore()
        #expect(reopened.value?.devices.first?.title == "Edited Mac")
        await rig.backend.setOffline(true)
        await store.remove(try #require(store.value?.devices.first))
        #expect(store.value?.devices.first?.title == "Edited Mac")
        #expect(store.actionIssue == "Couldn’t remove Edited Mac.")
        await store.refresh()
        #expect(store.value?.devices.first?.title == "Edited Mac")
        #expect(store.issue == nil)
    }

    @Test func chatSearchShowsDownloadedAndSemanticResultsBeforeNetwork() async throws {
        let rig = try await ResponsiveRig(holding: ["GET /threads/chat/search"])
        defer { rig.close() }
        let local = ResponsiveBackend.message("local", text: "calendar locally downloaded")
        let semantic = ResponsiveBackend.message("semantic", text: "Planning the week")
        try await rig.cache.call { try $0.merge([local]) }
        await rig.cache.saveSnapshot("chat-search/chat/calendar", [SearchHit(message: semantic)])
        let store = SearchStore(client: rig.client, actor: rig.cache, threadID: "chat")
        store.query = "calendar"
        let search = Task { await store.search() }
        try await eventually { store.results.count == 2 }
        #expect(Set(store.results.map(\.message.id)) == ["local", "semantic"])
        #expect(store.isSearching)
        try await eventually { await rig.backend.count("GET /threads/chat/search") == 1 }
        await rig.backend.release()
        await search.value
        #expect(store.results.first?.message.id == "remote-calendar")
        await rig.backend.setOffline(true)
        let reopened = SearchStore(client: rig.client, actor: rig.cache, threadID: "chat")
        reopened.query = "calendar"
        let offline = Task { await reopened.search() }
        try await eventually { reopened.results.contains { $0.message.id == "remote-calendar" } }
        await offline.value
        #expect(reopened.isOffline)
        #expect(reopened.error == nil)
    }

    @Test func editedQueryRejectsLateResults() async throws {
        let rig = try await ResponsiveRig(holding: ["GET /threads/chat/search"])
        defer { rig.close() }
        let store = SearchStore(client: rig.client, actor: rig.cache, threadID: "chat")
        store.query = "old"
        let old = Task { await store.search() }
        try await eventually { await rig.backend.count("GET /threads/chat/search") == 1 }
        store.query = "new"
        let new = Task { await store.search() }
        try await eventually { await rig.backend.count("GET /threads/chat/search") == 2 }
        await rig.backend.release()
        await old.value
        await new.value
        #expect(store.results.map(\.message.id) == ["remote-new"])
    }

    @Test func onlySafeReadsRetryAndResolveTheNewRoute() async throws {
        let rig = try await ResponsiveRig()
        defer { rig.close() }
        let route = TestRoute(url: rig.url)
        let client = APIClient(baseURL: rig.url, session: rig.session, token: "", endpoint: { await route.url },
                               recover: { await route.recover() })
        await rig.backend.fail("GET /providers", times: 1)
        #expect(try await client.providers().count == 1)
        #expect(await rig.backend.count("GET /providers") == 2)
        #expect(await rig.backend.count("GET /recovered/providers") == 1)
        #expect(await route.recoveries == 1)
        await rig.backend.fail("POST /agents", times: 1)
        await #expect(throws: URLError.self) { try await client.createAgent(CreateAgentRequest(name: "Bot")) }
        #expect(await rig.backend.count("POST /agents") == 1)
        await rig.backend.fail("GET /network/invite", times: 1)
        await #expect(throws: URLError.self) { try await client.invite() }
        #expect(await rig.backend.count("GET /network/invite") == 1)
    }

    @Test func liveMessagesDoNotWaitForHistoryOrReadReceipts() async throws {
        let rig = try await ResponsiveRig(holding: ["GET /threads/chat/tool-runs", "POST /threads/chat/read"])
        defer { rig.close() }
        let chat = ChatStore(client: rig.client, actor: rig.cache, threadID: "chat")
        defer { chat.stop() }
        await chat.start()
        try await eventually { chat.messages.contains { $0.id == "live" } }
        #expect(await rig.backend.count("GET /threads/chat/tool-runs") == 1)
        #expect(await rig.backend.count("GET /threads/chat/stream") >= 1)
        #expect(chat.runs.isEmpty)
        await rig.backend.release()
        try await eventually { !chat.runs.isEmpty }
    }

    @Test func creationStagesImmediatelyAndQueuedSendsUseTheReconciledChat() async throws {
        let rig = try await ResponsiveRig(holding: ["POST /agents"])
        defer { rig.close() }
        let sidebar = SidebarStore(client: rig.client, cache: rig.rawCache)
        await sidebar.load()
        let local = sidebar.createAgent(CreateAgentRequest(name: "Pending Bot"))
        #expect(local.id.hasPrefix("local-"))
        #expect(sidebar.threads.contains { $0.id == local.id })
        let chat = ChatStore(client: rig.client, actor: rig.cache, threadID: local.id, sidebar: sidebar)
        await chat.ready()
        let send = Task { await chat.send(text: "Keep this queued") }
        try await eventually { chat.messages.first?.text == "Keep this queued" }
        #expect(await rig.backend.count("POST /threads/created/messages") == 0)
        await rig.backend.release()
        await send.value
        #expect(sidebar.displayed(local.id)?.id == "created")
        #expect(sidebar.threads.contains { $0.id == "created" })
        #expect(!sidebar.threads.contains { $0.id == local.id })
        #expect(await rig.backend.count("POST /threads/created/messages") == 1)
        #expect(chat.messages.first?.threadID == "created")
    }

    @Test func attachmentsWaitForCreationAndUseTheResolvedThread() async throws {
        let rig = try await ResponsiveRig(holding: ["POST /threads"])
        defer { rig.close() }
        let sidebar = SidebarStore(client: rig.client, cache: rig.rawCache)
        await sidebar.load()
        let local = sidebar.createThread(agentIDs: ["a", "b"], title: "Pending group")
        let uploader = ResponsiveUpload()
        let chat = ChatStore(client: rig.client, actor: sidebar.cacheActor, threadID: local.id, sidebar: sidebar, uploadClient: uploader)
        await chat.ready()
        let url = rig.folder.appending(path: "note.txt")
        try Data("A queued attachment".utf8).write(to: url)
        let file = LocalFile(id: "file", url: url, name: "note.txt", kind: "file", mimeType: "text/plain", size: 19)
        let send = Task { await chat.send(text: "With a file", files: [file]) }
        try await eventually { chat.messages.first?.attachments.count == 1 }
        #expect(await uploader.calls == 0)
        await rig.backend.release()
        await send.value
        try await eventually { await rig.backend.count("POST /threads/created/messages") == 1 }
        #expect(chat.messages.first?.threadID == "created")
        #expect(await uploader.calls == 1)
        #expect(await rig.backend.count("POST /threads/\(local.id)/messages") == 0)
    }

    @Test func failedCreationRollsBackWithoutRetrying() async throws {
        let rig = try await ResponsiveRig()
        defer { rig.close() }
        let sidebar = SidebarStore(client: rig.client, cache: rig.rawCache)
        await sidebar.load()
        await rig.backend.fail("POST /threads", times: 1)
        let local = sidebar.createThread(agentIDs: ["a", "b"], title: "Pending group")
        #expect(sidebar.threads.contains { $0.id == local.id })
        try await eventually { sidebar.notice != nil }
        #expect(!sidebar.threads.contains { $0.id == local.id })
        #expect(await sidebar.resolved(local.id) == nil)
        #expect(await rig.backend.count("POST /threads") == 1)
    }

    private func eventually(_ condition: () async throws -> Bool) async throws {
        let deadline = ContinuousClock.now.advanced(by: .seconds(3))
        while try await !condition(), ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(5)) }
        try #require(try await condition())
    }
}

private actor ResponsiveUpload: UploadClient {
    private(set) var calls = 0
    func upload(file: LocalFile, progress: @escaping @Sendable (Double) -> Void) async throws -> PolymuxKit.Attachment {
        calls += 1
        return Attachment(id: file.id, kind: file.kind, name: file.name, mimeType: file.mimeType, size: file.size, url: "/attachments/" + file.id)
    }
}

private actor TestRoute {
    private(set) var url: URL
    private(set) var recoveries = 0
    init(url: URL) { self.url = url }
    func recover() {
        recoveries += 1
        url = url.appending(path: "recovered")
    }
}

@MainActor private struct ResponsiveRig {
    let folder: URL
    let backend: ResponsiveBackend
    let url: URL
    let session: URLSession
    let client: APIClient
    let rawCache: LocalCache
    let cache: LocalCacheActor

    init(holding: Set<String> = []) async throws {
        folder = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString)
        rawCache = try await CacheWork.open(folder.appending(path: "cache.db"))
        cache = LocalCacheActor(rawCache)
        backend = ResponsiveBackend(holding: holding)
        let host = UUID().uuidString.lowercased() + ".responsive.test"
        ResponsiveProtocol.lock.withLock { [backend] in ResponsiveProtocol.backends[host] = backend }
        url = URL(string: "http://" + host)!
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [ResponsiveProtocol.self]
        session = URLSession(configuration: configuration)
        client = APIClient(baseURL: url, session: session, token: "")
    }

    func close() {
        Task { await backend.release() }
        session.invalidateAndCancel()
        _ = ResponsiveProtocol.lock.withLock { ResponsiveProtocol.backends.removeValue(forKey: url.host!) }
        try? FileManager.default.removeItem(at: folder)
    }
}

private actor ResponsiveBackend {
    private var holding: Set<String>
    private var waiters: [CheckedContinuation<Void, Never>] = []
    private var calls: [String: Int] = [:]
    private var failures: [String: Int] = [:]
    private var offline = false

    static let network = NetworkInfo(server: .init(name: "Server", build: "", platform: "macOS", role: "server"),
                                    devices: [.init(id: "mac", name: "Mac")], current: "mac", host: nil, online: [])
    static func message(_ id: String, text: String) -> Message {
        Message(id: id, threadID: "chat", role: "assistant", parts: [Part(kind: "text", text: text)],
                createdAt: "2026-10-29T00:00:00Z", status: "done", seq: 1)
    }

    init(holding: Set<String>) { self.holding = holding }
    func count(_ key: String) -> Int { calls[key, default: 0] }
    func fail(_ key: String, times: Int) { failures[key] = times }
    func setOffline(_ value: Bool) { offline = value }
    func release() {
        holding.removeAll()
        let waiting = waiters
        waiters.removeAll()
        for waiter in waiting { waiter.resume() }
    }

    func response(_ request: URLRequest) async throws -> Data {
        let path = request.url!.path.replacingOccurrences(of: "/recovered", with: "")
        let key = request.httpMethod! + " " + path
        calls[key, default: 0] += 1
        if path != request.url!.path { calls[request.httpMethod! + " " + request.url!.path, default: 0] += 1 }
        if offline { throw URLError(.notConnectedToInternet) }
        if failures[key, default: 0] > 0 {
            failures[key, default: 0] -= 1
            throw URLError(.networkConnectionLost)
        }
        let data: Data
        switch key {
        case "GET /providers":
            data = try JSONEncoder().encode([Provider(id: "provider", name: "Server", headers: ["Authorization": "secret"])])
        case "GET /providers/options": data = try JSONEncoder().encode([ProviderOption(type: "provider", name: "Catalogue")])
        case "GET /network": data = try JSONEncoder().encode(Self.network)
        case "GET /threads/chat/search":
            let query = URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == "q" }?.value ?? ""
            data = try JSONEncoder().encode([SearchHit(message: Self.message("remote-" + query, text: "Semantic result"))])
        case "GET /threads/chat/stream":
            let event = Event(seq: 1, conversationID: "chat", type: "message.start", message: Self.message("live", text: "Live reply"))
            data = Data("data: \(String(decoding: try JSONEncoder().encode(event), as: UTF8.self))\n\n".utf8)
        case "GET /threads/chat/tool-runs": data = try JSONEncoder().encode([Run(id: "history", conversationID: "chat", seq: 1, status: "done")])
        case "POST /agents": data = try JSONEncoder().encode(Agent(id: "created", name: "Pending Bot"))
        case "POST /threads": data = try JSONEncoder().encode(Thread(id: "created", agentID: "a", title: "Created group", agentIDs: ["a", "b"]))
        case "GET /agents", "GET /threads", "GET /threads/chat/events": data = Data("[]".utf8)
        default: data = Data("{}".utf8)
        }
        if holding.contains(key) { await withCheckedContinuation { waiters.append($0) } }
        return data
    }
}

private final class ResponsiveProtocol: URLProtocol, @unchecked Sendable {
    static let lock = NSLock()
    nonisolated(unsafe) static var backends: [String: ResponsiveBackend] = [:]
    private let taskLock = NSLock()
    private var worker: Task<Void, Never>?

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let task = Task { @Sendable [self] in
            do {
                guard let backend = Self.lock.withLock({ Self.backends[request.url!.host!] }) else { throw URLError(.cancelled) }
                let data = try await backend.response(request)
                try Task.checkCancellation()
                let response = HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: nil)!
                client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
                client?.urlProtocol(self, didLoad: data)
                client?.urlProtocolDidFinishLoading(self)
            } catch { client?.urlProtocol(self, didFailWithError: error) }
        }
        taskLock.withLock { worker = task }
    }
    override func stopLoading() { taskLock.withLock { worker?.cancel() } }
}
