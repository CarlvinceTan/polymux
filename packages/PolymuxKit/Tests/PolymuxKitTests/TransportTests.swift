import CSQLite
import Foundation
import Testing
@testable import PolymuxKit
@testable import PolymuxAvatar

private final class ProbeRoutes: @unchecked Sendable {
    typealias Handler = @Sendable (URLRequest) async throws -> (Int, Data)
    private let lock = NSLock()
    private var handlers: [String: Handler] = [:]

    func add(_ handler: @escaping Handler) -> String {
        let host = "probe-\(UUID().uuidString.lowercased()).test"
        lock.withLock { handlers[host] = handler }
        return host
    }

    func handler(_ host: String) -> Handler? { lock.withLock { handlers[host] } }
    func remove(_ host: String) { _ = lock.withLock { handlers.removeValue(forKey: host) } }
}

private final class CredentialProbe: @unchecked Sendable {
    private let condition = NSCondition()
    private var started = false
    private var released = false
    private var mainThread = false
    private var count = 0

    var calls: Int { condition.withLock { count } }
    var offMain: Bool { condition.withLock { started && !mainThread } }

    func resolve(_ url: URL) -> String? {
        condition.lock()
        defer { condition.unlock() }
        count += 1
        started = true
        mainThread = Thread.isMainThread
        condition.broadcast()
        let deadline = Date().addingTimeInterval(3)
        while !released && condition.wait(until: deadline) {}
        return "discovered-token"
    }

    func wait() -> Bool {
        condition.lock()
        defer { condition.unlock() }
        let deadline = Date().addingTimeInterval(3)
        while !started && condition.wait(until: deadline) {}
        return started
    }

    func release() {
        condition.withLock { released = true; condition.broadcast() }
    }
}

private final class ProbeProtocol: URLProtocol, @unchecked Sendable {
    static let routes = ProbeRoutes()
    private let lock = NSLock()
    private var worker: Task<Void, Never>?

    override class func canInit(with request: URLRequest) -> Bool { request.url?.host?.hasPrefix("probe-") == true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let request = request
        let task = Task { @Sendable [self, request] in
            do {
                guard let url = request.url, let handler = Self.routes.handler(url.host ?? "") else { throw URLError(.badURL) }
                let (status, data) = try await handler(request)
                try Task.checkCancellation()
                let response = HTTPURLResponse(url: url, statusCode: status, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": "application/json"])!
                client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
                client?.urlProtocol(self, didLoad: data)
                client?.urlProtocolDidFinishLoading(self)
            } catch { client?.urlProtocol(self, didFailWithError: error) }
        }
        lock.withLock { self.worker = task }
    }

    override func stopLoading() { lock.withLock { worker?.cancel() } }
}

private func requestBody(_ request: URLRequest) -> Data {
    var body = request.httpBody ?? Data()
    if body.isEmpty, let stream = request.httpBodyStream {
        stream.open()
        defer { stream.close() }
        var buffer = [UInt8](repeating: 0, count: 4096)
        while stream.hasBytesAvailable {
            let count = stream.read(&buffer, maxLength: buffer.count)
            if count <= 0 { break }
            body.append(contentsOf: buffer.prefix(count))
        }
    }
    return body
}

private actor SendProbe {
    var requests: [SendRequest] = []
    func handle(_ request: URLRequest) async throws -> (Int, Data) {
        if request.httpMethod == "GET" { return (200, Data("[]".utf8)) }
        let body = requestBody(request)
        let send = try JSONDecoder().decode(SendRequest.self, from: body)
        requests.append(send)
        if requests.count == 1 { try await Task.sleep(for: .milliseconds(200)) }
        return (202, Data())
    }
}

private actor ActivityProbe {
    let kind: String
    private(set) var reads: [Int64] = []
    private var streamed = false

    init(kind: String) { self.kind = kind }

    func handle(_ request: URLRequest) async throws -> (Int, Data) {
        let path = request.url?.path ?? ""
        if path.hasSuffix("/events"), streamed {
            try await Task.sleep(for: .seconds(5))
        }
        if path.hasSuffix("/read") {
            reads.append(try JSONDecoder().decode(ReadRequest.self, from: requestBody(request)).seq)
            return (204, Data())
        }
        if path.hasSuffix("/stream") {
            streamed = true
            let event = Event(seq: 1, conversationID: "one", type: "activity",
                              payload: Activity(kind: kind, verb: "Finished", objectLabel: "Aria"))
            let wire = "data: " + String(decoding: try JSONEncoder().encode(event), as: UTF8.self) + "\n\n"
            return (200, Data(wire.utf8))
        }
        return (200, Data("[]".utf8))
    }
}

@Suite @MainActor
struct ActivityReadTests {
    @Test(arguments: ["agent.messaged", "subagent.finished"], [true, false])
    func readsVisibleRows(kind: String, visible: Bool) async throws {
        let probe = ActivityProbe(kind: kind)
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: directory) }
        let cache = try await CacheWork.open(directory.appendingPathComponent("cache.sqlite"))
        try await cache.offMain { try $0.save(agents: [], threads: [Thread(id: "one", agentID: "aria")]) }
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [ProbeProtocol.self]
        let client = APIClient(baseURL: URL(string: "http://\(host)")!, session: URLSession(configuration: config))
        let chat = ChatStore(client: client, actor: LocalCacheActor(cache), threadID: "one")
        chat.isVisible = visible
        defer { chat.stop() }
        await chat.start()
        for _ in 0..<100 {
            let read = await probe.reads.contains(1)
            let seen = try await cache.offMain { try $0.cursor(threadID: "one") } >= 1
            if seen, !visible || read { break }
            try await Task.sleep(for: .milliseconds(5))
        }
        #expect(try await cache.offMain { try $0.cursor(threadID: "one") } >= 1)
        #expect(try await cache.offMain { try $0.threads().first?.unreadCount } == (visible ? 0 : 1))
        #expect((await probe.reads).contains(1) == visible)
    }
}

private actor ReplyProbe {
    var requests: [SendRequest] = []
    private var online = false

    func reconnect() { online = true }

    func handle(_ request: URLRequest) throws -> (Int, Data) {
        requests.append(try JSONDecoder().decode(SendRequest.self, from: requestBody(request)))
        guard online else { throw URLError(.notConnectedToInternet) }
        return (202, Data())
    }
}

private actor DeliveryProbe {
    var requests: [SendRequest] = []
    let failure: Int
    private var online = false

    init(failure: Int) { self.failure = failure }

    func reconnect() { online = true }

    func handle(_ request: URLRequest) throws -> (Int, Data) {
        guard request.httpMethod == "POST", request.url?.path.hasSuffix("/messages") == true else {
            return (200, Data("[]".utf8))
        }
        requests.append(try JSONDecoder().decode(SendRequest.self, from: requestBody(request)))
        if online { return (202, Data()) }
        if failure == 0 { throw URLError(.notConnectedToInternet) }
        return (failure, Data())
    }
}

private actor InterruptedProbe {
    var requests: [SendRequest] = []

    func handle(_ request: URLRequest) async throws -> (Int, Data) {
        let path = request.url?.path ?? ""
        if path.hasSuffix("/messages"), request.httpMethod == "POST" {
            requests.append(try JSONDecoder().decode(SendRequest.self, from: requestBody(request)))
            return (202, Data())
        }
        guard path.hasSuffix("/stream") else { return (200, Data("[]".utf8)) }
        while requests.isEmpty { try await Task.sleep(for: .milliseconds(5)) }
        let send = requests[0]
        let date = WireDate.string()
        let user = Message(id: send.id, threadID: "one", role: "user", parts: [Part(kind: "text", text: send.text)],
                           createdAt: date, status: "done", seq: 1)
        let reply = Message(id: "interrupted", threadID: "one", role: "assistant", parts: [],
                            createdAt: date, status: "streaming", seq: 2, replyToID: send.id)
        let events = [Event(seq: 1, conversationID: "one", type: "message.start", message: user, createdAt: date),
                      Event(seq: 2, conversationID: "one", type: "message.start", messageID: reply.id, message: reply, createdAt: date),
                      Event(seq: 3, conversationID: "one", type: "message.delta", messageID: reply.id, text: "Partial reply", createdAt: date)]
        let wire = try events.map { "data: " + String(decoding: try JSONEncoder().encode($0), as: UTF8.self) + "\n\n" }.joined()
        return (200, Data(wire.utf8))
    }
}

private actor GroupProbe {
    var sends: [SendRequest] = []
    var updates: [UpdateThreadRequest] = []
    var updateBodies: [Data] = []
    var deliveries: [[String]] = []
    var thread = Thread(id: "group", agentID: "a", title: "A, B", agentIDs: ["a", "b"])
    private let guardEdits: Bool
    private var online = false
    init(guardEdits: Bool = false) { self.guardEdits = guardEdits }
    func reconnect() { online = true }

    func handle(_ request: URLRequest) async throws -> (Int, Data) {
        if request.httpMethod == "GET" {
            if request.url?.path == "/threads" { return (200, try JSONEncoder().encode([thread])) }
            return (200, try JSONEncoder().encode([Agent(id: "a"), Agent(id: "b"), Agent(id: "c")]))
        }
        if request.url?.path == "/threads", request.httpMethod == "POST" {
            let create = try JSONDecoder().decode(CreateThreadRequest.self, from: requestBody(request))
            thread.agentIDs = create.agentIDs
            thread.agentID = create.agentIDs[0]
            thread.title = create.title ?? thread.title
            return (201, try JSONEncoder().encode(thread))
        }
        if request.httpMethod == "PATCH" {
            let body = requestBody(request)
            let update = try JSONDecoder().decode(UpdateThreadRequest.self, from: body)
            updates.append(update)
            updateBodies.append(body)
            if guardEdits {
                guard online else { throw URLError(.notConnectedToInternet) }
                try await Task.sleep(for: .milliseconds(80))
            }
            if let title = update.title { thread.title = title }
            if let pinned = update.pinned {
                thread.pinned = pinned
                if !pinned { thread.pinOrder = nil }
            }
            if let order = update.pinOrder { thread.pinOrder = order }
            if let members = update.agentIDs {
                thread.agentIDs = members
                thread.agentID = members[0]
            }
            return (200, try JSONEncoder().encode(thread))
        }
        sends.append(try JSONDecoder().decode(SendRequest.self, from: requestBody(request)))
        guard online else { throw URLError(.notConnectedToInternet) }
        deliveries.append(thread.participantIDs)
        return (202, Data())
    }
}

private actor RegistryProbe {
    let agents: [Agent]
    let initial: [PolymuxKit.Thread]
    var latest: [PolymuxKit.Thread]
    let holdFirst: Bool
    let failing: Bool
    private var reads = 0
    private var release: CheckedContinuation<Void, Never>?
    private var started: CheckedContinuation<Void, Never>?

    init(agents: [Agent], threads: [PolymuxKit.Thread], holdFirst: Bool = false, failing: Bool = false) {
        self.agents = agents
        initial = threads
        latest = threads
        self.holdFirst = holdFirst
        self.failing = failing
    }

    func waitStarted() async {
        guard reads == 0 else { return }
        await withCheckedContinuation { started = $0 }
    }

    func resume() { release?.resume(); release = nil }
    func update(_ threads: [PolymuxKit.Thread]) { latest = threads }

    func handle(_ request: URLRequest) async throws -> (Int, Data) {
        if failing { throw URLError(.notConnectedToInternet) }
        if request.url?.path == "/agents" { return (200, try JSONEncoder().encode(agents)) }
        reads += 1
        let snapshot = reads == 1 ? initial : latest
        if reads == 1, holdFirst {
            started?.resume()
            started = nil
            await withCheckedContinuation { release = $0 }
        }
        return (200, try JSONEncoder().encode(snapshot))
    }
}

private actor ReplayProbe {
    var cursors: [Int64] = []
    let events = (1...2100).map {
        Event(seq: Int64($0), conversationID: "one", type: "agent.presence", agentID: "agent-a", active: true, createdAt: "2026-10-09T10:00:00Z")
    }
    func handle(_ request: URLRequest) throws -> (Int, Data) {
        guard request.url?.path.hasSuffix("/events") == true else { return (200, Data()) }
        let parts = URLComponents(url: request.url!, resolvingAgainstBaseURL: false)
        let after = Int64(parts?.queryItems?.first { $0.name == "after" }?.value ?? "0") ?? 0
        cursors.append(after)
        return (200, try JSONEncoder().encode(Array(events.filter { $0.seq > after }.prefix(1000))))
    }
}

private actor VoiceProbe {
    var requests: [SendRequest] = []

    func handle(_ request: URLRequest) async throws -> (Int, Data) {
        let path = request.url?.path ?? ""
        if path.hasSuffix("/messages") {
            requests.append(try JSONDecoder().decode(SendRequest.self, from: requestBody(request)))
            return (202, Data())
        }
        if !path.hasSuffix("/stream") { return (200, Data("[]".utf8)) }
        while requests.count < 2 { try await Task.sleep(for: .milliseconds(5)) }
        let date = "2026-10-09T10:00:00Z"
        var events: [Event] = []
        for (offset, request) in requests.enumerated() {
            let message = Message(id: request.id, threadID: "one", role: "user", parts: [Part(kind: "text", text: request.text)], createdAt: date, status: "done", seq: Int64(offset + 1))
            events.append(Event(seq: Int64(events.count + 1), conversationID: "one", type: "message.start", message: message, createdAt: date))
        }
        for request in requests.reversed() {
            let id = "reply-" + request.id
            let author = request.text == "First voice" ? "b" : "c"
            var message = Message(id: id, threadID: "one", role: "assistant", parts: [], createdAt: date, status: "streaming", seq: Int64(events.count + 1), replyToID: request.id, agentID: author)
            events.append(Event(seq: Int64(events.count + 1), conversationID: "one", type: "message.start", messageID: id, message: message, createdAt: date))
            let text = "Response to " + request.text
            message.parts = [Part(kind: "text", text: text)]
            events.append(Event(seq: Int64(events.count + 1), conversationID: "one", type: "message.delta", messageID: id, text: text, message: message, createdAt: date))
            message.status = "done"
            events.append(Event(seq: Int64(events.count + 1), conversationID: "one", type: "message.done", messageID: id, message: message, createdAt: date))
        }
        let wire = try events.map { "data: " + String(decoding: try JSONEncoder().encode($0), as: UTF8.self) + "\n\n" }.joined()
        return (200, Data(wire.utf8))
    }
}

private actor SettingsProbe {
    var value = Settings(revision: 1, appearance: "system", sidebarWidth: 320, voiceMode: "pipeline",
                         liveProvider: "gemini", liveModel: "server-live", liveVoice: "Kore",
                         textProvider: "gemini", textModel: "server-text", voicePrompt: "Represent {target}.")
    var patches: [SettingsPatch] = []
    var failWrites = false
    var holdRead = false
    private var held: CheckedContinuation<Void, Never>?
    private var started: CheckedContinuation<Void, Never>?

    func holdNext() { holdRead = true }
    func fail() { failWrites = true }
    func recover() { failWrites = false }
    func waitHeld() async {
        if held != nil { return }
        await withCheckedContinuation { started = $0 }
    }
    func resume() { held?.resume(); held = nil }
    func handle(_ request: URLRequest) async throws -> (Int, Data) {
        if request.httpMethod == "GET" {
            let snapshot = value
            if holdRead {
                holdRead = false
                await withCheckedContinuation { held = $0; started?.resume(); started = nil }
            }
            return (200, try JSONEncoder().encode(snapshot))
        }
        let patch = try JSONDecoder().decode(SettingsPatch.self, from: requestBody(request))
        patches.append(patch)
        if failWrites { return (400, try JSONEncoder().encode(WireError(code: "invalid_settings", message: "Invalid model."))) }
        patch.apply(to: &value)
        value.revision += 1
        return (200, try JSONEncoder().encode(value))
    }
}

private actor PinProbe {
    var thread = PolymuxKit.Thread(id: "a", agentID: "a", lastReadSeq: 2, lastSeq: 5,
                                  unreadCount: 3, title: "Current name", agentIDs: ["a"])
    var updates: [UpdateThreadRequest] = []
    var applied = 0
    private var online = false

    func reconnect() { online = true }
    func remotePin() { thread.pinned = true; thread.pinOrder = 77 }

    func handle(_ request: URLRequest) throws -> (Int, Data) {
        if request.httpMethod == "GET" {
            if request.url?.path == "/threads" { return (200, try JSONEncoder().encode([thread])) }
            return (200, try JSONEncoder().encode([Agent(id: "a", name: "Current name")]))
        }
        let update = try JSONDecoder().decode(UpdateThreadRequest.self, from: requestBody(request))
        updates.append(update)
        guard online else { throw URLError(.notConnectedToInternet) }
        if let title = update.title { thread.title = title }
        if let pinned = update.pinned {
            thread.pinned = pinned
            if !pinned { thread.pinOrder = nil }
        }
        if let order = update.pinOrder { thread.pinOrder = order }
        applied += 1
        return (200, try JSONEncoder().encode(thread))
    }
}

@Suite @MainActor
struct TransportTests {
    @Test func avatarsAreConnectionScopedAndNeverBackfillServerDefaults() async throws {
        let host = ProbeProtocol.routes.add { _ in (200, Data("[]".utf8)) }
        defer { ProbeProtocol.routes.remove(host) }
        let firstURL = cacheURL()
        let secondURL = cacheURL()
        defer {
            try? FileManager.default.removeItem(at: firstURL.deletingLastPathComponent())
            try? FileManager.default.removeItem(at: secondURL.deletingLastPathComponent())
        }
        let first = AvatarStore(sidebar: SidebarStore(client: client(host: host), cache: try await CacheWork.open(firstURL)))
        let second = AvatarStore(sidebar: SidebarStore(client: client(host: host), cache: try await CacheWork.open(secondURL)))
        var options = AvatarOptions()
        options.animates = false
        first.sync(agents: [Agent(id: "assistant", avatar: options.design)])
        #expect(first.options(for: "assistant").animates == false)
        #expect(second.options(for: "assistant").animates == true)
        #expect(first.dirtyIDs.isEmpty)
        first.sync(agents: [Agent(id: "assistant")])
        #expect(first.options(for: "assistant").animates == true)
        #expect(first.dirtyIDs.isEmpty)
        first.set(options, for: "assistant")
        #expect(first.dirtyIDs == ["assistant"])
        first.sync(agents: [])
        #expect(first.saved.isEmpty)
        #expect(first.dirtyIDs.isEmpty)
        second.sync(agents: [Agent(id: "assistant", avatar: options.design)])
        #expect(second.options(for: "assistant").animates == false)
    }

    @Test func acceptedAvatarWriteInvalidatesStaleSidebarRefresh() async throws {
        let original = Agent(id: "assistant", name: "Assistant", avatar: AvatarOptions.default.design)
        var options = AvatarOptions.default
        options.followCursor = false
        let accepted = Agent(id: original.id, name: original.name, avatar: options.design)
        let probe = RegistryProbe(agents: [original], threads: [], holdFirst: true)
        let host = ProbeProtocol.routes.add { request in
            if request.httpMethod == "PATCH" {
                #expect(request.url?.path == "/agents/assistant/avatar")
                #expect(try JSONDecoder().decode(AvatarDesign.self, from: requestBody(request)) == accepted.avatar)
                return (200, try JSONEncoder().encode(accepted))
            }
            return try await probe.handle(request)
        }
        defer { ProbeProtocol.routes.remove(host) }
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        try await cache.offMain { try $0.save(agents: [original], threads: []) }
        let api = client(host: host)
        let sidebar = SidebarStore(client: api, cache: cache)
        let avatars = AvatarStore(sidebar: sidebar)
        let read = Task { await sidebar.refresh() }
        await probe.waitStarted()
        avatars.set(options, for: original.id)
        try await avatars.commit(for: original.id, client: api)
        await probe.resume()
        await read.value
        avatars.sync(agents: sidebar.agents)
        #expect(sidebar.agents == [accepted])
        #expect(avatars.options(for: original.id).followCursor == false)
        #expect(avatars.dirtyIDs.isEmpty)
        #expect(try await cache.offMain { try $0.agents() } == [accepted])
    }

    @Test func settingsPatchesCoalesceWithoutOverwritingOtherFields() async throws {
        let probe = SettingsProbe()
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let first = SettingsStore(client: client(host: host))
        let second = SettingsStore(client: client(host: host))
        await first.refresh()
        await second.refresh()
        first.edit(SettingsPatch(appearance: "light"))
        first.edit(SettingsPatch(appearance: "dark", liveVoice: "Aoede"))
        for _ in 0..<300 where first.isSaving { try await Task.sleep(for: .milliseconds(5)) }
        #expect(!first.isSaving)
        #expect(await probe.patches == [SettingsPatch(appearance: "dark", liveVoice: "Aoede")])
        second.edit(SettingsPatch(textModel: "updated-text"))
        for _ in 0..<300 where second.isSaving { try await Task.sleep(for: .milliseconds(5)) }
        await first.refresh()
        #expect(first.value?.appearance == "dark")
        #expect(first.value?.liveVoice == "Aoede")
        #expect(first.value?.textModel == "updated-text")
        #expect(first.value == second.value)
    }

    @Test func staleSettingsReadCannotUndoAcceptedWrite() async throws {
        let probe = SettingsProbe()
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let store = SettingsStore(client: client(host: host))
        await store.refresh()
        await probe.holdNext()
        let read = Task { await store.refresh() }
        await probe.waitHeld()
        store.edit(SettingsPatch(voiceMode: "live"))
        for _ in 0..<300 where store.isSaving { try await Task.sleep(for: .milliseconds(5)) }
        #expect(!store.isSaving)
        await probe.resume()
        await read.value
        #expect(store.value?.voiceMode == "live")
        #expect(store.value?.revision == 2)
    }

    @Test func failedSettingsWritesRollbackWithoutReplayOrCrossServerState() async throws {
        let probe = SettingsProbe()
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let store = SettingsStore(client: client(host: host))
        await store.refresh()
        await probe.fail()
        store.edit(SettingsPatch(textModel: "invalid"))
        #expect(store.value?.textModel == "invalid")
        for _ in 0..<300 where store.isSaving { try await Task.sleep(for: .milliseconds(5)) }
        #expect(!store.isSaving)
        #expect(store.value?.textModel == "server-text")
        #expect(store.error == "Invalid model.")
        await store.refresh()
        #expect(await probe.patches.count == 1)
        let other = SettingsStore(client: client(host: host))
        #expect(other.value == nil)
    }

    @Test func settingsRetryResendsOnlyTheFailedFields() async throws {
        let probe = SettingsProbe()
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let api = client(host: host)
        let store = SettingsStore(client: api)
        await store.refresh()
        await probe.fail()
        store.edit(SettingsPatch(liveVoice: "Aoede"))
        #expect(await store.synchronize() == false)
        #expect(store.value?.liveVoice == "Kore")
        await probe.recover()
        _ = try await api.updateSettings(SettingsPatch(appearance: "dark"))
        await store.retry()
        #expect(store.error == nil)
        #expect(store.value?.liveVoice == "Aoede")
        #expect(store.value?.appearance == "dark")
        #expect(await probe.patches == [SettingsPatch(liveVoice: "Aoede"), SettingsPatch(appearance: "dark"),
                                       SettingsPatch(liveVoice: "Aoede")])
    }

    @Test func speechPreparationWaitsForCommittedSettings() async throws {
        let probe = SettingsProbe()
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let store = SettingsStore(client: client(host: host))
        await store.refresh()
        store.edit(SettingsPatch(voiceMode: "live", liveModel: "updated-live", liveVoice: "Aoede"))
        #expect(await store.synchronize())
        #expect(!store.isSaving)
        #expect(store.value == (await probe.value))
        #expect(store.value?.liveModel == "updated-live")
        #expect(store.value?.liveVoice == "Aoede")
    }

    @Test func queuedSettingsSaveSurvivesItsWindowReleasingTheStore() async throws {
        let probe = SettingsProbe()
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        var store: SettingsStore? = SettingsStore(client: client(host: host))
        await store?.refresh()
        weak var released = store
        store?.edit(SettingsPatch(appearance: "dark"))
        store = nil
        for _ in 0..<300 {
            if released == nil { break }
            try await Task.sleep(for: .milliseconds(5))
        }
        #expect(await probe.value.appearance == "dark")
        #expect(await probe.patches == [SettingsPatch(appearance: "dark")])
        #expect(released == nil)
    }

    @Test func apiSendsCredential() async throws {
        let host = ProbeProtocol.routes.add { request in
            #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer test-device-token")
            return (200, Data("[]".utf8))
        }
        defer { ProbeProtocol.routes.remove(host) }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [ProbeProtocol.self]
        let api = APIClient(baseURL: URL(string: "http://\(host)")!,
                            session: URLSession(configuration: configuration), token: "test-device-token")
        #expect(try await api.agents().isEmpty)
    }

    @Test @MainActor func discoveryKeepsMainActorFree() async throws {
        let discovery = CredentialProbe()
        defer { discovery.release() }
        let host = ProbeProtocol.routes.add { request in
            #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer discovered-token")
            return (200, Data("[]".utf8))
        }
        defer { ProbeProtocol.routes.remove(host) }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [ProbeProtocol.self]
        let api = APIClient(baseURL: URL(string: "http://\(host)")!, session: URLSession(configuration: configuration),
                            token: nil, discover: discovery.resolve)
        #expect(await Task.detached { discovery.wait() }.value)
        #expect(discovery.offMain)
        discovery.release()
        async let first = api.agents()
        async let second = api.agents()
        #expect(try await first.isEmpty)
        #expect(try await second.isEmpty)
        #expect(discovery.calls == 1)
    }

    @Test func explicitCredentialSkipsDiscovery() async throws {
        let discovery = CredentialProbe()
        let host = ProbeProtocol.routes.add { request in
            #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer explicit-token")
            return (200, Data("[]".utf8))
        }
        defer { ProbeProtocol.routes.remove(host) }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [ProbeProtocol.self]
        let api = APIClient(baseURL: URL(string: "http://\(host)")!, session: URLSession(configuration: configuration),
                            token: "explicit-token", discover: discovery.resolve)
        #expect(try await api.agents().isEmpty)
        #expect(discovery.calls == 0)
    }

    @Test func speechUsesServerTransportWithoutClientCredentials() async throws {
        let host = ProbeProtocol.routes.add { request in
            #expect(request.url?.path == "/speech/text")
            #expect(request.httpMethod == "POST")
            #expect(request.url?.query == nil)
            #expect(request.value(forHTTPHeaderField: "x-goog-api-key") == nil)
            #expect(!String(decoding: requestBody(request), as: UTF8.self).contains("unused-client-key"))
            return (200, Data(#"{"candidates":[{"content":{"parts":[{"text":"Hello"}]}}]}"#.utf8))
        }
        defer { ProbeProtocol.routes.remove(host) }
        let api = client(host: host)
        let provider = GeminiText(client: api)
        let answer = try await provider.generate(turns: [TextTurn(role: .user, text: "Hi")],
                                                 options: LiveOptions(model: "server-text", voice: "Kore", instructions: "Speak briefly."),
                                                 key: "unused-client-key")
        #expect(answer.text == "Hello")
        let socket = try api.speechSocket()
        #expect(socket.scheme == "ws")
        #expect(socket.host == host)
        #expect(socket.path == "/speech/live")
        #expect(socket.query == nil)
    }

    @Test func speechKeysAreWriteOnlyAndRemovedOnServer() async throws {
        let host = ProbeProtocol.routes.add { request in
            #expect(request.url?.path == "/settings/keys/gemini")
            if request.httpMethod == "PUT" {
                #expect(try JSONDecoder().decode(SpeechKey.self, from: requestBody(request)).key == "test-only-secret")
            } else {
                #expect(request.httpMethod == "DELETE")
                #expect(requestBody(request).isEmpty)
            }
            return (200, try JSONEncoder().encode(Settings(revision: 2, speechKeys: request.httpMethod == "PUT" ? ["gemini"] : [])))
        }
        defer { ProbeProtocol.routes.remove(host) }
        let store = SettingsStore(client: client(host: host))
        try await store.setKey("test-only-secret", provider: "gemini")
        #expect(store.value?.speechKeys == ["gemini"])
        try await store.setKey(nil, provider: "gemini")
        #expect(store.value?.speechKeys.isEmpty == true)
    }

    @Test(arguments: ["Planning assistant", ""])
    func profileEdits(role: String) async throws {
        let agent = Agent(id: "research", name: "Research", role: "Research assistant")
        let profile = AgentProfile(agent: agent, prompt: "Keep this role description.\nPreserve its second line.")
        var renamed = agent
        renamed.name = "Rowan"
        renamed.role = role
        let updated = AgentProfile(agent: renamed, prompt: profile.prompt)
        let update = UpdateAgentRequest(name: renamed.name, role: role, prompt: profile.prompt)
        let host = ProbeProtocol.routes.add { request in
            #expect(request.url?.path == "/agents/research")
            switch request.httpMethod {
            case "GET": return (200, try JSONEncoder().encode(profile))
            case "PATCH":
                #expect(request.value(forHTTPHeaderField: "Content-Type") == "application/json")
                #expect(try JSONDecoder().decode(UpdateAgentRequest.self, from: requestBody(request)) == update)
                return (200, try JSONEncoder().encode(updated))
            default: return (405, Data())
            }
        }
        defer { ProbeProtocol.routes.remove(host) }
        let api = client(host: host)
        #expect(try await api.agentProfile(id: agent.id) == profile)
        let saved = try await api.updateAgent(id: agent.id, request: update)
        #expect(saved == updated)
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let sidebar = SidebarStore(client: api, cache: try await CacheWork.open(url))
        await sidebar.accept(saved.agent)
        #expect(sidebar.agents == [renamed])
        let opened = try await CacheWork.open(url)
        #expect(try await opened.offMain { try $0.agents() } == [renamed])
    }

    @Test func routineClientTargetsOwnerAndPreservesSchedule() async throws {
        let schedule = RoutineRequest(prompt: "Review my draft", at: "2026-10-10T09:00:00Z", intervalSeconds: 86400)
        let routine = Routine(at: schedule.at, nextAt: schedule.at, intervalSeconds: schedule.intervalSeconds,
                              status: "active", createdAt: "2026-10-09T09:00:00Z", id: "daily", agentID: "a",
                              trigger: "interval", prompt: schedule.prompt)
        let settings = Proactivity(enabled: false, intervalSeconds: 14400)
        let host = ProbeProtocol.routes.add { request in
            let path = request.url?.path ?? ""
            switch (request.httpMethod, path) {
            case ("GET", "/agents/a/routines"):
                return (200, try JSONEncoder().encode([routine]))
            case ("GET", "/agents/a/proactivity"):
                return (200, try JSONEncoder().encode(settings))
            case ("PATCH", "/agents/a/proactivity"):
                #expect(try JSONDecoder().decode(Proactivity.self, from: requestBody(request)) == settings)
                return (200, try JSONEncoder().encode(settings))
            default: return (404, Data())
            }
        }
        defer { ProbeProtocol.routes.remove(host) }
        let api = client(host: host)
        #expect(try await api.routines(agentID: "a") == [routine])
        #expect(try await api.proactivity(agentID: "a") == settings)
        #expect(try await api.setProactivity(agentID: "a", settings: settings) == settings)
    }

    private func cacheURL() -> URL {
        FileManager.default.temporaryDirectory.appendingPathComponent("polymux-transport-\(UUID())/cache.sqlite")
    }

    @Test func notificationCursorIsIndependentAndRegistrationIsWriteOnly() async throws {
        let device = PushDevice(id: "installation-fixture", token: String(repeating: "ab", count: 32), environment: "sandbox",
            server: "http://server.test:7337", enabled: true)
        let host = ProbeProtocol.routes.add { request in
            if request.url?.path == "/notifications/device" {
                #expect(request.httpMethod == "PUT")
                #expect(try JSONDecoder().decode(PushDevice.self, from: requestBody(request)) == device)
                return (200, try JSONEncoder().encode(PushStatus(registered: true, available: true)))
            }
            #expect(request.url?.path == "/notifications")
            let after = URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems?.first?.value
            if after == nil { return (200, try JSONEncoder().encode(NoticePage(cursor: 100, notices: []))) }
            #expect(after == "100")
            return (200, try JSONEncoder().encode(NoticePage(cursor: 101, notices: [Notice(seq: 101, conversationID: "chat",
                messageID: "result", title: "Agent", createdAt: "2026-10-10T12:00:00Z")])))
        }
        defer { ProbeProtocol.routes.remove(host) }
        let api = client(host: host)
        let baseline = try await api.notices()
        #expect(baseline.cursor == 100 && baseline.notices.isEmpty)
        let page = try await api.notices(after: baseline.cursor)
        #expect(page.notices.map(\.messageID) == ["result"])
        #expect(try await api.registerPush(device).available)
    }

    @Test func profileCommitRestoresFailureAndProtectsNewerPrompt() async throws {
        let host = ProbeProtocol.routes.add { _ in (500, Data()) }
        defer { ProbeProtocol.routes.remove(host) }
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let api = client(host: host)
        let sidebar = SidebarStore(client: api, cache: try await CacheWork.open(url))
        let previous = AgentProfile(agent: Agent(id: "profile", name: "Old"), prompt: "Old prompt")
        let update = UpdateAgentRequest(name: "New", role: "Role", prompt: "First prompt")
        var closed = false
        let first = sidebar.commits.save(previous, request: update, sidebar: sidebar, client: api) {
            closed = true
        }
        #expect(closed)
        #expect(sidebar.agents.first?.name == "New")
        #expect(sidebar.commits.profiles["profile"]?.prompt == "First prompt")
        await first.value
        #expect(sidebar.agents.first?.name == "Old")
        #expect(sidebar.commits.profiles["profile"] == previous)
        #expect(sidebar.commits.notice != nil)
        let older = sidebar.commits.save(previous, request: update, sidebar: sidebar, client: api, close: {})
        var newer = update
        newer.prompt = "Second prompt"
        let latest = sidebar.commits.save(previous, request: newer, sidebar: sidebar, client: api, close: {})
        await older.value
        #expect(sidebar.commits.profiles["profile"]?.prompt != "Old prompt")
        await latest.value
        // The newer failure restores the state that existed when it was submitted.
        #expect(sidebar.commits.profiles["profile"]?.prompt == "First prompt")
        await sidebar.settled()
    }

    @Test func avatarCommitRestoresFailureAfterClosing() async throws {
        let host = ProbeProtocol.routes.add { _ in (500, Data()) }
        defer { ProbeProtocol.routes.remove(host) }
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let api = client(host: host)
        let sidebar = SidebarStore(client: api, cache: try await CacheWork.open(url))
        let previous = AvatarDesign()
        var next = previous
        next.shape = "square"
        var visible = previous
        var closed = false
        let task = sidebar.commits.saveAvatar(id: "avatar", previous: previous, request: next,
                                             sidebar: sidebar, client: api,
                                             apply: { visible = $0 }, close: { closed = true })
        #expect(closed && visible == next)
        await task.value
        #expect(visible == previous)
        #expect(sidebar.commits.notice != nil)
    }

    @Test func profileCommitReportsAStalledClose() async throws {
        let host = ProbeProtocol.routes.add { _ in (500, Data()) }
        defer { ProbeProtocol.routes.remove(host) }
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let api = client(host: host)
        let sidebar = SidebarStore(client: api, cache: try await CacheWork.open(url))
        let task = sidebar.commits.save(AgentProfile(agent: Agent(id: "slow")),
                                       request: UpdateAgentRequest(name: "Slow"),
                                       sidebar: sidebar, client: api) {
            let clock = ContinuousClock()
            let start = clock.now
            while start.duration(to: clock.now) < .milliseconds(70) {}
        }
        #expect(sidebar.commits.trace.meetsBudgets == false)
        await task.value
        await sidebar.settled()
    }

    private func client(host: String) -> APIClient {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [ProbeProtocol.self]
        return APIClient(baseURL: URL(string: "http://\(host)")!, session: URLSession(configuration: config))
    }

    @Test(arguments: [0, 408, 429, 500, 503, 400])
    func unsentRetryKeepsIdentity(failure: Int) async throws {
        let probe = DeliveryProbe(failure: failure)
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        let chat = ChatStore(client: client(host: host), actor: LocalCacheActor(cache), threadID: "one")
        await chat.send(text: "Keep this unsent message")
        let unsent = try #require(chat.messages.first)
        #expect(unsent.status == (failure == 400 ? "error" : "failed"))
        #expect(try await cache.offMain { try $0.pending(threadID: "one") }.count == (failure == 400 ? 0 : 1))
        let reopened = try await CacheWork.open(url)
        let resumed = ChatStore(client: client(host: host), actor: LocalCacheActor(reopened), threadID: "one")
        await resumed.ready()
        #expect(resumed.messages.first?.status == unsent.status)
        await probe.reconnect()
        await resumed.retry(messageID: unsent.id)
        let requests = await probe.requests
        #expect(requests.count == 2)
        #expect(requests[0] == requests[1])
        #expect(resumed.messages.count == 1)
        #expect(resumed.messages.first?.status == "done")
        #expect(try await reopened.offMain { try $0.pending(threadID: "one") }.isEmpty)
    }

    @Test func unsentReconnectResendsAutomatically() async throws {
        let probe = DeliveryProbe(failure: 503)
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        let chat = ChatStore(client: client(host: host), actor: LocalCacheActor(cache), threadID: "one")
        await chat.send(text: "Retry after the server returns")
        let unsent = try #require(chat.messages.first)
        #expect(unsent.status == "failed")
        let resumedCache = try await CacheWork.open(url)
        let resumed = ChatStore(client: client(host: host), actor: LocalCacheActor(resumedCache), threadID: "one")
        defer { resumed.stop() }
        await probe.reconnect()
        await resumed.start()
        for _ in 0..<100 {
            if resumed.messages.first?.status == "done" { break }
            try await Task.sleep(for: .milliseconds(5))
        }
        let requests = await probe.requests
        #expect(requests.count == 2)
        #expect(requests[0] == requests[1])
        #expect(resumed.messages.first?.id == unsent.id)
        #expect(resumed.messages.first?.status == "done")
        #expect(try await cache.offMain { try $0.pending(threadID: "one") }.isEmpty)
    }

    @Test(arguments: [429, 503])
    func inactiveUnsentRetriesAfterRefresh(failure: Int) async throws {
        let probe = DeliveryProbe(failure: failure)
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        let request = SendRequest(id: "inactive", text: "Keep this in the outbox")
        _ = try await cache.offMain { try $0.enqueue(threadID: "one", request: request, attachments: []) }
        let sidebar = SidebarStore(client: client(host: host), cache: cache)
        await sidebar.refresh()
        #expect(try await cache.offMain { try $0.messages(threadID: "one") }.first?.status == "failed")
        #expect(try await cache.offMain { try $0.queued() }.map(\.request) == [request])
        await probe.reconnect()
        await sidebar.refresh()
        #expect(await probe.requests == [request, request])
        #expect(try await cache.offMain { try $0.messages(threadID: "one") }.first?.status == "done")
        #expect(try await cache.offMain { try $0.queued() }.isEmpty)
    }

    @Test func staleRefreshCannotEraseAcceptedChatOrOutbox() async throws {
        let agents = [Agent(id: "a", name: "A"), Agent(id: "b", name: "B")]
        let individual = Thread(id: "a", agentID: "a", title: "A", agentIDs: ["a"])
        let probe = RegistryProbe(agents: agents, threads: [individual], holdFirst: true)
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        try await cache.offMain { try $0.save(agents: agents, threads: [individual]) }
        let sidebar = SidebarStore(client: client(host: host), cache: cache)
        await sidebar.load()
        let refresh = Task { await sidebar.refresh() }
        await probe.waitStarted()
        let group = Thread(id: "group", agentID: "a", title: "Trip", agentIDs: ["a", "b"])
        await sidebar.accept(group)
        let queued = SendRequest(id: "offline", text: "Keep this group draft")
        _ = try await cache.offMain { try $0.enqueue(threadID: group.id, request: queued, attachments: []) }
        await probe.resume()
        await refresh.value
        #expect(sidebar.groups == [group])
        #expect(sidebar.error == nil)
        let reopened = try await CacheWork.open(url)
        #expect(try await reopened.offMain { try $0.threads() }.contains(group))
        #expect(try await reopened.offMain { try $0.pending(threadID: group.id) } == [queued])
        #expect(try await reopened.offMain { try $0.messages(threadID: group.id) }.first?.text == queued.text)
    }

    @Test func newestRefreshWinsWithoutLocalMutation() async throws {
        let agent = Agent(id: "a", name: "A")
        let initial = Thread(id: "a", agentID: "a", title: "A", agentIDs: ["a"])
        let group = Thread(id: "group", agentID: "a", title: "New chat", agentIDs: ["a", "b"])
        let probe = RegistryProbe(agents: [agent], threads: [initial], holdFirst: true)
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        let sidebar = SidebarStore(client: client(host: host), cache: cache)
        let older = Task { await sidebar.refresh() }
        await probe.waitStarted()
        await probe.update([initial, group])
        await sidebar.refresh()
        await probe.resume()
        await older.value
        #expect(sidebar.groups == [group])
        #expect(try await cache.offMain { try $0.threads() } == [initial, group])
    }

    @Test func acceptedRenameAndAgentSurviveRefreshFailure() async throws {
        let a = Agent(id: "a", name: "A")
        let b = Agent(id: "b", name: "B")
        let preview = Message(id: "reply", threadID: "group", role: "assistant", parts: [Part(kind: "text", text: "Keep the preview")], createdAt: WireDate.string(), status: "done", seq: 3)
        let group = Thread(id: "group", agentID: "a", lastReadSeq: 2, lastSeq: 4, unreadCount: 1, lastMessage: preview, title: "Old name", agentIDs: ["a", "b"])
        let direct = Thread(id: "a", agentID: "a", lastReadSeq: 7, lastSeq: 9, title: "A", agentIDs: ["a"])
        let probe = RegistryProbe(agents: [a, b], threads: [group, direct], failing: true)
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        try await cache.offMain { try $0.save(agents: [a, b], threads: [group, direct]) }
        try await cache.offMain { try $0.merge([preview]) }
        let sidebar = SidebarStore(client: client(host: host), cache: cache)
        await sidebar.load()
        let renamed = Thread(id: group.id, agentID: "a", lastReadSeq: 2, lastSeq: 4, title: "Trip", agentIDs: ["a", "b"])
        await sidebar.accept(renamed)
        let created = Agent(id: "c", name: "Coach")
        await sidebar.accept(created)
        await sidebar.accept(a)
        await sidebar.refresh()
        #expect(sidebar.groups.first?.title == "Trip")
        #expect(sidebar.groups.first?.lastMessage == preview)
        #expect(sidebar.groups.first?.unreadCount == 1)
        #expect(sidebar.thread(for: a)?.lastReadSeq == 7)
        #expect(sidebar.thread(for: created)?.participantIDs == [created.id])
        #expect(sidebar.agents.contains(created))
        #expect(sidebar.error != nil)
        let reopened = try await CacheWork.open(url)
        let relaunched = SidebarStore(client: client(host: host), cache: reopened)
        await relaunched.load()
        #expect(relaunched.groups.first?.title == "Trip")
        #expect(relaunched.agents.contains(created))
        #expect(relaunched.thread(for: created)?.id == created.id)
        #expect(try await reopened.offMain { try $0.messages(threadID: group.id) } == [preview])
    }

    @Test func acceptedResultStaysVisibleWhenCacheWriteFails() async throws {
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        var db: OpaquePointer?
        #expect(sqlite3_open(url.path, &db) == SQLITE_OK)
        defer { sqlite3_close(db) }
        #expect(sqlite3_exec(db, "CREATE TRIGGER fail_metadata BEFORE INSERT ON metadata BEGIN SELECT RAISE(FAIL,'forced cache failure'); END", nil, nil, nil) == SQLITE_OK)
        let sidebar = SidebarStore(client: APIClient(baseURL: URL(string: "http://127.0.0.1:1")!), cache: cache)
        let agent = Agent(id: "new", name: "New agent")
        await sidebar.accept(agent)
        #expect(sidebar.agents == [agent])
        #expect(sidebar.thread(for: agent)?.id == agent.id)
        #expect(sidebar.error == "Couldn’t save this agent. Retry")
        await sidebar.reload()
        #expect(sidebar.agents == [agent])
        #expect(sidebar.thread(for: agent)?.id == agent.id)
        #expect(sqlite3_exec(db, "DROP TRIGGER fail_metadata", nil, nil, nil) == SQLITE_OK)
        await sidebar.accept(agent)
        #expect(sidebar.error == nil)
        let reopened = try await CacheWork.open(url)
        #expect(try await reopened.offMain { try $0.agents() } == [agent])
        #expect(try await reopened.offMain { try $0.threads() }.first?.id == agent.id)
    }

    @Test func groupAPIPreservesMentionIdentityAcrossOfflineRetry() async throws {
        let probe = GroupProbe()
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let api = client(host: host)
        let created = try await api.createThread(agentIDs: ["b", "a"], title: "Trip")
        #expect(created.title == "Trip")
        #expect(created.agentIDs == ["b", "a"])
        let renamed = try await api.updateThread(id: created.id, title: "Planning")
        #expect(renamed.title == "Planning")
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        let chat = ChatStore(client: api, actor: LocalCacheActor(cache), threadID: created.id)
        await chat.send(text: "@A review this", mentions: ["a"])
        let request = try #require((try await cache.offMain { try $0.pending(threadID: created.id) }).first)
        #expect(request.mentions == ["a"])
        #expect(chat.messages.first?.mentions == ["a"])
        let reopened = try await CacheWork.open(url)
        #expect(try await reopened.offMain { try $0.pending(threadID: created.id) } == [request])
        await probe.reconnect()
        await chat.retry(messageID: request.id)
        #expect(await probe.sends == [request, request])
        var accepted = try #require(chat.messages.first)
        accepted.seq = 1
        accepted.status = "done"
        let stored = accepted
        try await reopened.offMain { try $0.apply(Event(seq: 1, conversationID: created.id, type: "message.start", message: stored, createdAt: stored.createdAt)) }
        #expect(try await reopened.offMain { try $0.messages(threadID: created.id) }.first?.mentions == ["a"])
        #expect(try await reopened.offMain { try $0.pending(threadID: created.id) }.isEmpty)
    }

    @Test func groupMembershipUpdateKeepsCachedHistoryAndPendingSend() async throws {
        let probe = GroupProbe()
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let api = client(host: host)
        let group = try await api.createThread(agentIDs: ["a", "b"], title: "Team")
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        let agents = [Agent(id: "a", name: "A"), Agent(id: "b", name: "B"), Agent(id: "c", name: "C")]
        try await cache.offMain { try $0.save(agents: agents, threads: agents.map { Thread(id: $0.id, agentID: $0.id, agentIDs: [$0.id]) } + [group]) }
        let reply = Message(id: "old-author", threadID: group.id, role: "assistant",
                            parts: [Part(kind: "text", text: "Keep this reply")],
                            createdAt: "2026-10-09T10:00:00Z", status: "done", seq: 1, agentID: "b")
        try await cache.offMain { try $0.apply(Event(seq: 1, conversationID: group.id, type: "message.done", message: reply, createdAt: reply.createdAt)) }
        let chat = ChatStore(client: api, actor: LocalCacheActor(cache), threadID: group.id)
        await chat.send(text: "@B queued before removal", mentions: ["b"])
        let queued = try #require((try await cache.offMain { try $0.pending(threadID: group.id) }).first)
        let sidebar = SidebarStore(client: api, cache: cache)
        await sidebar.load()
        let updated = try await api.updateThread(id: group.id, title: "Solo team", agentIDs: ["c"])
        await sidebar.accept(updated)
        #expect(updated.id == group.id)
        #expect(updated.isGroup)
        #expect(updated.participantIDs == ["c"])
        #expect(sidebar.groups.first?.participantIDs == ["c"])
        #expect(sidebar.agents == agents)
        let renamed = try await api.updateThread(id: group.id, title: "Renamed")
        await sidebar.accept(renamed)
        #expect(renamed.participantIDs == ["c"])
        #expect(await probe.updates.map(\.agentIDs) == [["c"], nil])
        let bodies = await probe.updateBodies
        let membershipBody = try #require(JSONSerialization.jsonObject(with: bodies[0]) as? [String: Any])
        let renameBody = try #require(JSONSerialization.jsonObject(with: bodies[1]) as? [String: Any])
        #expect(membershipBody["agentIDs"] as? [String] == ["c"])
        #expect(renameBody["agentIDs"] == nil)
        let reopened = try await CacheWork.open(url)
        #expect(try await reopened.offMain { try $0.threads() }.first { $0.id == group.id }?.participantIDs == ["c"])
        #expect(try await reopened.offMain { try $0.threads() }.first { $0.id == group.id }?.title == "Renamed")
        #expect(try await reopened.offMain { try $0.messages(threadID: group.id) }.first { $0.id == reply.id } == reply)
        #expect(try await reopened.offMain { try $0.pending(threadID: group.id) } == [queued])
        #expect(try await reopened.offMain { try $0.agents() } == agents)
    }

    @Test(arguments: [false, true]) func groupSendWaitsForPendingMembership(reconnect: Bool) async throws {
        let probe = GroupProbe(guardEdits: true)
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let api = client(host: host)
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        let initial = await probe.thread
        try await cache.offMain { try $0.save(agents: [Agent(id: "a"), Agent(id: "b"), Agent(id: "c")], threads: [initial]) }
        let sidebar = SidebarStore(client: api, cache: cache)
        await sidebar.load()
        let chat = ChatStore(client: api, actor: sidebar.cacheActor, threadID: initial.id, sidebar: sidebar)
        var updated = initial
        updated.agentIDs = ["a", "c"]
        await sidebar.edit(updated, delay: .seconds(10))
        #expect(sidebar.groups.first?.participantIDs == ["a", "c"])
        await chat.send(text: "Send to the current members")
        let unsent = try #require(chat.messages.first)
        #expect(unsent.status == "failed")
        #expect(await probe.sends.isEmpty)
        #expect(try await cache.offMain { try $0.pending(threadID: initial.id) }.first?.id == unsent.id)
        #expect(try await cache.offMain { try $0.threadEdits() } == [updated])
        await probe.reconnect()
        if reconnect { await sidebar.refresh() }
        else { await chat.retry(messageID: unsent.id) }
        #expect(await probe.sends.map(\.id) == [unsent.id])
        #expect(await probe.deliveries == [["a", "c"]])
        #expect(try await cache.offMain { try $0.pending(threadID: initial.id) }.isEmpty)
        #expect(try await cache.offMain { try $0.threadEdits() }.isEmpty)
    }

    @Test func pinSurvivesOfflineRelaunchWithoutRenamingTheChat() async throws {
        let probe = PinProbe()
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let api = client(host: host)
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        let initial = await probe.thread
        try await cache.offMain { try $0.save(agents: [Agent(id: "a", name: "Current name")], threads: [initial]) }
        let sidebar = SidebarStore(client: api, cache: cache)
        await sidebar.load()
        await sidebar.setPin("a", pinned: true)
        try? await sidebar.syncEdit("a")
        #expect(sidebar.threads.first?.pinned == true)
        #expect(sidebar.pinIssue != nil)
        #expect(try await cache.offMain { try $0.pinEdits() } == ["a"])

        let reopened = try await CacheWork.open(url)
        let relaunched = SidebarStore(client: api, cache: reopened)
        await relaunched.load()
        #expect(relaunched.threads.first?.pinned == true)
        #expect(relaunched.threads.first?.unreadCount == 3)
        await probe.reconnect()
        await relaunched.refresh()
        try await relaunched.syncEdit("a")
        #expect(await probe.applied == 1)
        #expect(await probe.updates.allSatisfy { $0.title == nil && $0.agentIDs == nil && $0.leadAgentID == nil })
        #expect(relaunched.threads.first?.title == initial.title)
        #expect(relaunched.threads.first?.lastReadSeq == 2)
        #expect(relaunched.threads.first?.lastSeq == 5)
        #expect(relaunched.pinIssue == nil)
        #expect(try await reopened.offMain { try $0.threadEdits() }.isEmpty)
        #expect(try await reopened.offMain { try $0.pinEdits() }.isEmpty)

        await relaunched.setPin("a", pinned: false)
        try await relaunched.syncEdit("a")
        #expect(await probe.applied == 2)
        #expect(relaunched.threads.first?.pinned == false)
        #expect(relaunched.threads.first?.pinOrder == nil)
    }

    @Test(arguments: [false, true]) func renameKeepsRemotePinsAndPersistsExplicitPinChanges(combined: Bool) async throws {
        let probe = PinProbe()
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let api = client(host: host)
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        let initial = await probe.thread
        try await cache.offMain { try $0.save(agents: [Agent(id: "a")], threads: [initial]) }
        let sidebar = SidebarStore(client: api, cache: cache)
        await sidebar.load()
        var renamed = initial
        renamed.title = "Local rename"
        await sidebar.edit(renamed, delay: .seconds(10))
        if combined { await sidebar.setPin("a", pinned: true) }
        try? await sidebar.syncEdit("a")
        if combined {
            #expect(sidebar.pinIssue != nil)
            #expect(try await cache.offMain { try $0.dirtyPins() } == ["a"])
        } else { await probe.remotePin() }
        let reopened = try await CacheWork.open(url)
        let relaunched = SidebarStore(client: api, cache: reopened)
        await relaunched.load()
        await probe.reconnect()
        try await relaunched.syncEdit("a")
        let update = try #require(await probe.updates.last)
        #expect(update.title == "Local rename")
        #expect(update.pinned == (combined ? true : nil))
        #expect(update.pinOrder == (combined ? 0 : nil))
        #expect(relaunched.threads.first?.pinned == true)
        #expect(relaunched.threads.first?.pinOrder == (combined ? 0 : 77))
        #expect(relaunched.pinIssue == nil)
        #expect(try await reopened.offMain { try $0.dirtyPins() }.isEmpty)
    }

    @Test func pinLimitKeepsTheNextChatUnchanged() async throws {
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        let threads = (0..<13).map { PolymuxKit.Thread(id: "chat-\($0)", agentID: "chat-\($0)", pinned: $0 < 12) }
        try await cache.offMain { try $0.save(agents: [], threads: threads) }
        let sidebar = SidebarStore(client: client(host: "offline.test"), cache: cache)
        await sidebar.load()
        await sidebar.setPin("chat-12", pinned: true)
        #expect(sidebar.threads.filter { $0.pinned == true }.count == 12)
        #expect(sidebar.threads.first { $0.id == "chat-12" }?.pinned == false)
        #expect(sidebar.editIssues["chat-12"] == "You can pin up to 12 chats.")
        #expect(try await cache.offMain { try $0.threadEdits() }.isEmpty)
    }

    @Test func offlineReplyKeepsQuoteAndIdentityThroughRetryAndReplay() async throws {
        let probe = ReplyProbe()
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        let attachment = Attachment(id: "quoted-image", kind: "image", name: "Notebook.png",
                                    mimeType: "image/png", size: 123, url: "/attachments/quoted-image", indexingState: "ready")
        let target = Message(id: "quoted-message", threadID: "one", role: "assistant",
                             parts: [Part(kind: "text", text: "Which notebook?"), Part(kind: "attachment", attachment: attachment)],
                             createdAt: "2026-10-09T10:00:00Z", status: "done", seq: 1)
        try await cache.offMain { try $0.apply(Event(seq: 1, conversationID: "one", type: "message.start", message: target,
                              createdAt: target.createdAt)) }
        let chat = ChatStore(client: client(host: host), actor: LocalCacheActor(cache), threadID: "one")
        await chat.send(text: "The blue one", reply: target.replyContext)
        let optimistic = try #require(chat.messages.first { $0.role == "user" })
        #expect(optimistic.status == "failed")
        #expect(optimistic.reply == target.replyContext)
        #expect(optimistic.replyToID == nil)
        let request = try #require((try await cache.offMain { try $0.pending(threadID: "one") }).first)
        #expect(request.replyToID == target.id)
        #expect(request.id == optimistic.id)
        #expect(await probe.requests == [request])

        let relaunched = try await CacheWork.open(url)
        let resumed = ChatStore(client: client(host: host), actor: LocalCacheActor(relaunched), threadID: "one")
        await resumed.ready()
        #expect(resumed.messages.first { $0.id == optimistic.id }?.reply?.attachment == attachment)
        #expect(try await relaunched.offMain { try $0.pending(threadID: "one") } == [request])
        await probe.reconnect()
        await resumed.retry(messageID: optimistic.id)
        #expect(await probe.requests == [request, request])
        #expect(try await relaunched.offMain { try $0.pending(threadID: "one") }.isEmpty)
        #expect(resumed.messages.first { $0.id == optimistic.id }?.reply == target.replyContext)

        let canonical = ReplyContext(messageID: target.id, role: target.role, text: "Server quote snapshot", attachment: attachment)
        let accepted = Message(id: request.id, threadID: "one", role: "user", parts: [Part(kind: "text", text: request.text)],
                               createdAt: "2026-10-09T10:01:00Z", status: "done", seq: 2, reply: canonical)
        let event = Event(seq: 2, conversationID: "one", type: "message.start", message: accepted, createdAt: accepted.createdAt)
        try await relaunched.offMain { try $0.apply(event) }
        try await relaunched.offMain { try $0.apply(event) }
        try await relaunched.offMain { try $0.acknowledge(id: accepted.id) }
        let replayCache = try await CacheWork.open(url)
        let replayed = ChatStore(client: client(host: host), actor: LocalCacheActor(replayCache), threadID: "one")
        await replayed.ready()
        #expect(replayed.messages.count == 2)
        #expect(replayed.messages.first { $0.id == accepted.id } == accepted)
        #expect(try await relaunched.offMain { try $0.cursor(threadID: "one") } == 2)
        #expect(try await relaunched.offMain { try $0.pending(threadID: "one") }.isEmpty)
    }

    @Test func offlinePartReplyKeepsSelectorThroughRetryAndReconciliation() async throws {
        let probe = ReplyProbe()
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        let first = Attachment(id: "first", kind: "image", name: "Same.png")
        let second = Attachment(id: "second", kind: "image", name: "Same.png")
        let source = Message(id: "original", threadID: "chat", role: "assistant",
                             parts: [Part(kind: "attachment", attachment: first), Part(kind: "text", text: "Caption"),
                                     Part(kind: "attachment", attachment: second)],
                             createdAt: "2026-10-09T10:00:00Z", status: "done", seq: 1, agentID: "research")
        try await cache.offMain { try $0.apply(Event(seq: 1, conversationID: "chat", type: "message.done", message: source, createdAt: source.createdAt)) }
        let quote = try #require(source.replyContext(part: 2))
        let chat = ChatStore(client: client(host: host), actor: LocalCacheActor(cache), threadID: "chat")
        await chat.send(text: "This image", attachments: [first, second], reply: quote)
        let optimistic = try #require(chat.messages.first { $0.role == "user" })
        let request = try #require((try await cache.offMain { try $0.pending(threadID: "chat") }).first)
        #expect(request.replyPart == 2)
        #expect(request.attachmentIDs == [first.id, second.id])
        #expect(optimistic.parts.count == 3)
        #expect(optimistic.reply == quote)
        let reopened = try await CacheWork.open(url)
        #expect(try await reopened.offMain { try $0.pending(threadID: "chat") } == [request])
        let resumed = ChatStore(client: client(host: host), actor: LocalCacheActor(reopened), threadID: "chat")
        await probe.reconnect()
        await resumed.retry(messageID: optimistic.id)
        #expect(await probe.requests == [request, request])
        var accepted = optimistic
        accepted.seq = 2
        accepted.status = "done"
        let event = Event(seq: 2, conversationID: "chat", type: "message.start", message: accepted, createdAt: accepted.createdAt)
        try await reopened.offMain { try $0.apply(event) }
        try await reopened.offMain { try $0.apply(event) }
        let final = try await CacheWork.open(url)
        #expect(try await final.offMain { try $0.messages(threadID: "chat") }.count == 2)
        #expect(try await final.offMain { try $0.messages(threadID: "chat") }.first { $0.id == accepted.id }?.reply == quote)
        #expect(try await final.offMain { try $0.pending(threadID: "chat") }.isEmpty)
    }

    @Test func sendDuringPendingAcknowledgementIsFlushed() async throws {
        let probe = SendProbe()
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        let chat = ChatStore(client: client(host: host), actor: LocalCacheActor(cache), threadID: "one")
        let first = Task { await chat.send(text: "First send") }
        for _ in 0..<100 {
            if await probe.requests.count == 1 { break }
            try await Task.sleep(for: .milliseconds(5))
        }
        await chat.send(text: "Second send while first POST is pending")
        await first.value
        let requests = await probe.requests
        #expect(requests.map(\.text) == ["First send", "Second send while first POST is pending"])
        #expect(Set(requests.map(\.id)).count == 2)
        #expect(try await cache.offMain { try $0.pending(threadID: "one") }.isEmpty)
        #expect(chat.messages.allSatisfy { $0.status == "done" })
    }

    @Test func reconnectFlushesAnInactiveConversation() async throws {
        let probe = SendProbe()
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        let request = SendRequest(id: "inactive-send", text: "Queued before selecting another agent")
        _ = try await cache.offMain { try $0.enqueue(threadID: "inactive", request: request, attachments: []) }
        let sidebar = SidebarStore(client: client(host: host), cache: cache)
        await sidebar.refresh()
        #expect(sidebar.isConnected)
        #expect(await probe.requests == [request])
        #expect(try await cache.offMain { try $0.queued() }.isEmpty)
        #expect(try await cache.offMain { try $0.messages(threadID: "inactive") }.first?.status == "done")
    }

    @Test func catchupFetchesEveryPageBeforeLiveDelivery() async throws {
        let probe = ReplayProbe()
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        let chat = ChatStore(client: client(host: host), actor: LocalCacheActor(cache), threadID: "one")
        defer { chat.stop() }
        await chat.start()
        for _ in 0..<200 {
            if try await cache.offMain { try $0.cursor(threadID: "one") } == 2100, await probe.cursors.contains(2100) { break }
            try await Task.sleep(for: .milliseconds(10))
        }
        #expect(try await cache.offMain { try $0.cursor(threadID: "one") } == 2100)
        let cursors = await probe.cursors
        #expect(cursors.starts(with: [0, 1000, 2000, 2100]))
    }
}

@Test func legacyNoticePage() throws {
    let data = Data(#"{"cursor":7,"notices":[{"seq":7,"conversationID":"chat","messageID":"reply","title":"Polymux","createdAt":"2026-10-13T08:00:00Z"}]}"#.utf8)
    let page = try JSONDecoder().decode(NoticePage.self, from: data)
    #expect(page.cursor == 7)
    #expect(page.notices.first?.body == nil)
    #expect(page.notices.first?.outcome == nil)
}
