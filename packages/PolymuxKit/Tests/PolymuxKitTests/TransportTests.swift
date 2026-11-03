import CSQLite
import CryptoKit
import Foundation
import IrohLib
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

    override class func canInit(with request: URLRequest) -> Bool {
        request.url?.host?.hasPrefix("probe-") == true || request.url?.path.hasPrefix("/probe-") == true
    }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let request = request
        let task = Task { @Sendable [self, request] in
            do {
                // A probe host can also stand in for a Tailscale name, such as probe-….ts.net.
                let host = request.url?.host ?? ""
                guard let url = request.url, let handler = Self.routes.handler(host)
                        ?? Self.routes.handler(String(host.dropLast(".ts.net".count)))
                        ?? request.url?.path.split(separator: "/").first.flatMap({ Self.routes.handler(String($0)) }) else { throw URLError(.badURL) }
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

@Suite
struct RouteTests {
    /// A tunnel invite from another machine can carry this Mac's loopback address, which mustn't share its key.
    @Test func foreignLoopbackInviteGetsItsOwnAddress() async throws {
        func ticket() throws -> String {
            try EndpointTicket.fromAddr(addr: EndpointAddr(id: SecretKey.generate().public(), relayUrl: nil, addresses: [])).description
        }
        let first = try ticket(), second = try ticket()
        let identity = try #require(Tunnel.identity(first))
        #expect(identity.count == 16 && identity.allSatisfy(\.isHexDigit))
        #expect(Tunnel.identity(first) == identity && Tunnel.identity(second) != identity)
        // Nothing on this port accepts the credential, so the server is elsewhere.
        let loopback = URL(string: "http://127.0.0.1:9")!
        let kept = await Tunnel.address(for: loopback, ticket: first, token: "device-token")
        #expect(kept.absoluteString == "http://\(identity).tunnel.invalid")
        #expect(Tunnel.trusted(kept) == false)
        #expect(await Tunnel.address(for: loopback, ticket: first, token: "new-token", knownTicket: first) == loopback)
        #expect(await Tunnel.address(for: loopback, ticket: second, token: "new-token", knownTicket: first) != loopback)
        for (url, ticket) in [(loopback, nil), (URL(string: "http://100.70.1.2:7337")!, first), (loopback, "endpointabc")] as [(URL, String?)] {
            #expect(await Tunnel.address(for: url, ticket: ticket, token: "device-token") == url)
        }
    }

    @Test(arguments: [200, 401, 403])
    func loopbackPairingUsesTheCredentialProbe(status: Int) async throws {
        let ticket = try EndpointTicket.fromAddr(addr: EndpointAddr(id: SecretKey.generate().public(), relayUrl: nil, addresses: [])).description
        let host = ProbeProtocol.routes.add { request in
            #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer device-token")
            return (status, Data("{}".utf8))
        }
        defer { ProbeProtocol.routes.remove(host) }
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [ProbeProtocol.self]
        let loopback = URL(string: "http://localhost/\(host)")!
        let kept = await Tunnel.address(for: loopback, ticket: ticket, token: "device-token", session: URLSession(configuration: config))
        #expect((kept == loopback) == (status == 200))
    }

    @Test(arguments: zip([200, 401, 403], [true, false, false]))
    func directRouteNeedsAcceptedCredential(status: Int, direct: Bool) async {
        let host = ProbeProtocol.routes.add { request in
            #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer device-token")
            return (status, Data("{}".utf8))
        }
        defer { ProbeProtocol.routes.remove(host) }
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [ProbeProtocol.self]
        let url = URL(string: "https://\(host):7337")!
        #expect(await Tunnel.reachable(url, token: "device-token", session: URLSession(configuration: config)) == direct)
    }

    /// The credential never goes over plain HTTP on an ordinary network.
    @Test func lanAddressGetsNoCredential() async throws {
        let host = ProbeProtocol.routes.add { _ in
            Issue.record("A LAN address was sent the credential")
            return (200, Data("{}".utf8))
        }
        defer { ProbeProtocol.routes.remove(host) }
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [ProbeProtocol.self]
        #expect(await !Tunnel.reachable(URL(string: "http://\(host):7337")!, token: "device-token", session: URLSession(configuration: config)))
        for (address, trusted) in [("http://127.0.0.1:7337", true), ("http://[::1]:7337", true), ("https://server.example", true),
                                   ("http://100.101.1.1:7337", false), ("http://[fd7a:115c:a1e0::9]:7337", false), ("http://mac.tail1234.ts.net", false),
                                   ("http://192.168.1.20:7337", false), ("http://10.0.0.4:7337", false), ("http://100.128.0.1:7337", false)] {
            #expect(Tunnel.trusted(URL(string: address)!) == trusted, "\(address)")
        }
        await #expect(throws: ClientError.self) { _ = try await Tunnel.route(URL(string: "http://192.168.1.20:7337")!, ticket: nil, token: "device-token") }
    }
}

@Suite
struct DownloadTests {
    @Test(arguments: ["same", "host", "port", "scheme"])
    func attachmentCredential(origin: String) async throws {
        let handler: ProbeRoutes.Handler = { request in
            #expect(request.value(forHTTPHeaderField: "X-Polymux-Client") == "polymux-kit")
            #expect(origin == "same")
            #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer attachment-token")
            return (200, Data("<p>Visual card</p>".utf8))
        }
        let host = ProbeProtocol.routes.add(handler)
        let other = ProbeProtocol.routes.add(handler)
        defer { ProbeProtocol.routes.remove(host); ProbeProtocol.routes.remove(other) }
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [ProbeProtocol.self]
        let client = APIClient(baseURL: URL(string: "http://\(host):8000")!, session: URLSession(configuration: config), token: "attachment-token")
        let scheme = origin == "scheme" ? "https" : "http"
        let target = origin == "host" ? other : host
        let port = origin == "port" ? 8001 : 8000
        let url = URL(string: "\(scheme)://\(target):\(port)/attachments/card/content")!
        guard origin == "same" else {
            await #expect(throws: ClientError.self) { _ = try await client.download(url) }
            return
        }
        let (file, response) = try await client.download(url)
        defer { try? FileManager.default.removeItem(at: file) }
        #expect((response as? HTTPURLResponse)?.statusCode == 200)
        #expect(try String(contentsOf: file, encoding: .utf8) == "<p>Visual card</p>")
    }
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

private actor ReactionProbe {
    var requests: [ReactionRequest] = []
    let message: Message
    let fails: Bool
    private var pending: CheckedContinuation<Void, Never>?

    init(message: Message, fails: Bool) { self.message = message; self.fails = fails }

    func release() { pending?.resume(); pending = nil }

    func handle(_ request: URLRequest) async throws -> (Int, Data) {
        #expect(request.httpMethod == "PUT")
        #expect(request.url?.path == "/threads/one/messages/target/reaction")
        let change = try JSONDecoder().decode(ReactionRequest.self, from: requestBody(request))
        requests.append(change)
        await withCheckedContinuation { pending = $0 }
        if fails { throw URLError(.notConnectedToInternet) }
        var accepted = message
        accepted.reactions = (message.reactions ?? []).filter { $0.emoji != change.emoji || $0.agentID != nil }
        if !change.remove { accepted.reactions?.append(Reaction(emoji: change.emoji)) }
        return (200, try JSONEncoder().encode(accepted))
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

private actor HistoryProbe {
    let messages = (1...150).map {
        Message(id: "history-\($0)", threadID: "one", role: "assistant", status: "done", seq: Int64($0))
    }
    var parked = false
    private var released = false
    private var gate: CheckedContinuation<Void, Never>?

    func release() {
        released = true
        gate?.resume()
        gate = nil
    }

    func handle(_ request: URLRequest) async throws -> (Int, Data) {
        let path = request.url!.path
        let query = URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems ?? []
        if path.hasSuffix("/events") {
            let after = Int64(query.first { $0.name == "after" }?.value ?? "0") ?? 0
            if after == 100, !released {
                parked = true
                await withCheckedContinuation { gate = $0 }
            }
            let page = messages.filter { $0.seq > after }.prefix(100).map {
                Event(seq: $0.seq, conversationID: "one", type: "message.done", message: $0)
            }
            return (200, try JSONEncoder().encode(page))
        }
        if path.hasSuffix("/messages") {
            let before = Int64(query.first { $0.name == "before" }?.value ?? "") ?? .max
            return (200, try JSONEncoder().encode(Array(messages.filter { $0.seq < before }.suffix(100))))
        }
        if path.hasSuffix("/stream") { try await Task.sleep(for: .seconds(60)) }
        return (200, Data("[]".utf8))
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
    var value = Settings(revision: 1, appearance: "system",
                         liveProvider: "gemini", liveModel: "server-live", liveVoice: "Kore",
                         voicePrompt: "Represent {target}.", defaultProvider: "server-provider", defaultModel: "server-text")
    var patches: [SettingsPatch] = []
    var failWrites = false
    var failStatus = 400
    var holdRead = false
    private var held: CheckedContinuation<Void, Never>?
    private var started: CheckedContinuation<Void, Never>?

    func holdNext() { holdRead = true }
    func fail(status: Int = 400) { failWrites = true; failStatus = status }
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
        if failWrites { return (failStatus, try JSONEncoder().encode(WireError(code: "invalid_settings", message: "Invalid model."))) }
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
        second.edit(SettingsPatch(defaultModel: "updated-text"))
        for _ in 0..<300 where second.isSaving { try await Task.sleep(for: .milliseconds(5)) }
        await first.refresh()
        #expect(first.value?.appearance == "dark")
        #expect(first.value?.liveVoice == "Aoede")
        #expect(first.value?.defaultModel == "updated-text")
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
        store.edit(SettingsPatch(liveVoice: "Aoede"))
        for _ in 0..<300 where store.isSaving { try await Task.sleep(for: .milliseconds(5)) }
        #expect(!store.isSaving)
        await probe.resume()
        await read.value
        #expect(store.value?.liveVoice == "Aoede")
        #expect(store.value?.revision == 2)
    }

    @Test func failedSettingsWritesRollbackWithoutReplayOrCrossServerState() async throws {
        let probe = SettingsProbe()
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let store = SettingsStore(client: client(host: host))
        await store.refresh()
        await probe.fail()
        store.edit(SettingsPatch(defaultModel: "invalid"))
        #expect(store.value?.defaultModel == "invalid")
        for _ in 0..<300 where store.isSaving { try await Task.sleep(for: .milliseconds(5)) }
        #expect(!store.isSaving)
        #expect(store.value?.defaultModel == "server-text")
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
        await probe.fail(status: 503)
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
        store.edit(SettingsPatch(liveModel: "updated-live", liveVoice: "Aoede"))
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
        weak let released = store
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
                            token: nil, discover: { url, _ in discovery.resolve(url) })
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
                            token: "explicit-token", discover: { url, _ in discovery.resolve(url) })
        #expect(try await api.agents().isEmpty)
        #expect(discovery.calls == 0)
    }

    @Test func replacedLocalCredentialRecoversWithoutReopeningClient() async throws {
        actor Owner {
            var token = "first-owner-token"
            var discoveries = 0
            func discover() -> String { discoveries += 1; return token }
            func replace() { token = "replacement-owner-token" }
            func response(_ request: URLRequest) -> (Int, Data) {
                (request.value(forHTTPHeaderField: "Authorization") == "Bearer \(token)" ? 200 : 401, Data("[]".utf8))
            }
        }
        let owner = Owner()
        let host = ProbeProtocol.routes.add { await owner.response($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [ProbeProtocol.self]
        let api = APIClient(baseURL: URL(string: "http://\(host)")!, session: URLSession(configuration: configuration),
                            token: nil, discover: { _, _ in await owner.discover() })
        #expect(try await api.agents().isEmpty)
        await owner.replace()
        #expect(try await api.agents().isEmpty)
        #expect(await owner.discoveries == 2)
    }

    @Test func speechSocketUsesServerTransport() async throws {
        let socket = try await client(host: "speech.test").speechSocket()
        #expect(socket.scheme == "ws")
        #expect(socket.host == "speech.test")
        #expect(socket.path == "/speech/live")
        #expect(socket.query == nil)
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

    @Test func avatarCommitReportsAStalledClose() async throws {
        let host = ProbeProtocol.routes.add { _ in (500, Data()) }
        defer { ProbeProtocol.routes.remove(host) }
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let api = client(host: host)
        let sidebar = SidebarStore(client: api, cache: try await CacheWork.open(url))
        let task = sidebar.commits.saveAvatar(id: "slow", previous: AvatarDesign(), request: AvatarDesign(),
                                             sidebar: sidebar, client: api, apply: { _ in }) {
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
        #expect(sidebar.error == "Couldn’t save this bot on this device.")
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

    @Test func unsentMessageCanBeDeletedBeforeItGoesOut() async throws {
        let probe = GroupProbe()
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let api = client(host: host)
        let created = try await api.createThread(agentIDs: ["b", "a"], title: "Trip")
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        let chat = ChatStore(client: api, actor: LocalCacheActor(cache), threadID: created.id)
        await chat.send(text: "Never mind")
        let request = try #require((try await cache.offMain { try $0.pending(threadID: created.id) }).first)
        await chat.discard(messageID: request.id)
        #expect(chat.messages.isEmpty)
        #expect(try await cache.offMain { try $0.pending(threadID: created.id) }.isEmpty)
        // Reconnecting must not send what was deleted.
        await probe.reconnect()
        await chat.retry(messageID: request.id)
        #expect(await probe.sends == [request])
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

    @Test func renameKeepsRemotePins() async throws {
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
        try? await sidebar.syncEdit("a")
        await probe.remotePin()
        let reopened = try await CacheWork.open(url)
        let relaunched = SidebarStore(client: api, cache: reopened)
        await relaunched.load()
        await probe.reconnect()
        try await relaunched.syncEdit("a")
        let update = try #require(await probe.updates.last)
        #expect(update.title == "Local rename")
        #expect(update.pinned == nil && update.pinOrder == nil)
        #expect(relaunched.threads.first?.pinned == true)
        #expect(relaunched.threads.first?.pinOrder == 77)
        #expect(try await reopened.offMain { try $0.threadEdits() }.isEmpty)
    }

    @Test(arguments: [false, true], ["👍", "👩🏽‍💻"]) func reactionsAreOptimisticAndPreserveBotIdentity(fails: Bool, emoji: String) async throws {
        let original = Message(id: "target", threadID: "one", role: "user", status: "done", seq: 1,
                               reactions: [Reaction(emoji: emoji, agentID: "bot")])
        let probe = ReactionProbe(message: original, fails: fails)
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        try await cache.offMain { try $0.apply(Event(seq: 1, conversationID: "one", type: "message.start", message: original)) }
        let chat = ChatStore(client: client(host: host), actor: LocalCacheActor(cache), threadID: "one")
        await chat.ready()
        let work = Task { await chat.react(messageID: "target", emoji: emoji) }
        defer { work.cancel(); Task { await probe.release() } }
        for _ in 0..<100 {
            if await probe.requests.count == 1 { break }
            try await Task.sleep(for: .milliseconds(5))
        }
        #expect(chat.reactions(for: original).count == 2)
        #expect(chat.messages.first?.reactions == original.reactions)
        await chat.react(messageID: "target", emoji: emoji)
        #expect(await probe.requests == [ReactionRequest(emoji: emoji, remove: false)])
        await probe.release()
        await work.value
        #expect(chat.pendingReactions.isEmpty)
        #expect(chat.messages.first?.reactions?.count == (fails ? 1 : 2))
        #expect((chat.reactionErrors["target"] != nil) == fails)
        #expect(chat.messages.first?.reactions?.first == Reaction(emoji: emoji, agentID: "bot"))
        let reopened = try await CacheWork.open(url)
        #expect(try await reopened.offMain { try $0.messages(threadID: "one").first?.reactions?.count } == (fails ? 1 : 2))
    }

    @Test func reactionReplayWinsOverAnOlderHTTPResponse() async throws {
        let original = Message(id: "target", threadID: "one", role: "user", status: "done", seq: 1)
        let probe = ReactionProbe(message: original, fails: false)
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        try await cache.offMain { try $0.apply(Event(seq: 1, conversationID: "one", type: "message.start", message: original)) }
        let chat = ChatStore(client: client(host: host), actor: LocalCacheActor(cache), threadID: "one")
        await chat.ready()
        let work = Task { await chat.react(messageID: "target", emoji: "👍") }
        defer { work.cancel(); Task { await probe.release() } }
        for _ in 0..<100 {
            if await probe.requests.count == 1 { break }
            try await Task.sleep(for: .milliseconds(5))
        }
        var newer = original
        newer.reactions = [Reaction(emoji: "👍"), Reaction(emoji: "🎉", agentID: "bot")]
        try await chat.receive(Event(seq: 2, conversationID: "one", type: "message.reactions", messageID: original.id, message: newer))
        await probe.release()
        await work.value
        #expect(chat.messages.first == newer)
        let reopened = try await CacheWork.open(url)
        #expect(try await reopened.offMain { try $0.messages(threadID: "one") } == [newer])
    }

    @Test(arguments: [1, 100]) func cachedReplyTargetsLoadOffline(earlier: Int) async throws {
        let host = ProbeProtocol.routes.add { _ in throw URLError(.notConnectedToInternet) }
        defer { ProbeProtocol.routes.remove(host) }
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        let history = (1...(100 + earlier)).map { index in
            Message(id: "message-\(index)", threadID: "one", role: "assistant", status: "done", seq: Int64(index))
        }
        try await cache.offMain { try $0.merge(history) }
        let chat = ChatStore(client: client(host: host), actor: LocalCacheActor(cache), threadID: "one")
        await chat.ready()
        #expect(chat.messages.count == 100)
        #expect(!chat.messages.contains { $0.id == "message-1" })
        await chat.loadEarlier()
        #expect(chat.messages == history)
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

    @Test(arguments: [false, true]) func replaySnapshot(cached: Bool) async throws {
        let probe = HistoryProbe()
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        defer { ProbeProtocol.routes.remove(host) }
        let url = cacheURL()
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let cache = try await CacheWork.open(url)
        let history = await probe.messages
        let initial = cached ? Array(history.suffix(100)) : []
        try await cache.offMain { try $0.merge(initial) }
        let chat = ChatStore(client: client(host: host), actor: LocalCacheActor(cache), threadID: "one")
        defer { chat.stop(); Task { await probe.release() } }
        await chat.start()
        for _ in 0..<200 {
            if await probe.parked { break }
            try await Task.sleep(for: .milliseconds(10))
        }
        try #require(await probe.parked)
        #expect(chat.messages == initial, "Historical replay must not replace the visible transcript one event at a time")
        await probe.release()
        for _ in 0..<200 {
            if chat.isConnected { break }
            try await Task.sleep(for: .milliseconds(10))
        }
        try #require(chat.isConnected)
        #expect(chat.messages == Array(history.suffix(100)))
        #expect(chat.hasEarlier)
        #expect(try await cache.offMain { try $0.cursor(threadID: "one") } == 150)
        await chat.loadEarlier()
        #expect(chat.messages == history)
        #expect(!chat.hasEarlier)
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
            if try await cache.offMain({ try $0.cursor(threadID: "one") }) == 2100, await probe.cursors.contains(2100) { break }
            try await Task.sleep(for: .milliseconds(10))
        }
        #expect(try await cache.offMain { try $0.cursor(threadID: "one") } == 2100)
        let cursors = await probe.cursors
        #expect(cursors.starts(with: [0, 1000, 2000, 2100]))
    }
}

private actor ProviderProbe {
    var answered = false
    var cancellations = 0
    var refuse = false

    func rejectAnswer() { refuse = true }

    func handle(_ request: URLRequest) async throws -> (Int, Data) {
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer provider-token")
        let path = request.url!.path
        if request.httpMethod == "DELETE" { cancellations += 1; return (204, Data()) }
        if path.hasSuffix("/answer") {
            let body = try JSONSerialization.jsonObject(with: requestBody(request)) as! [String: Any]
            #expect(body["prompt"] as? Int == 1)
            #expect(body["value"] as? String == "key")
            if refuse { return (400, Data(#"{"code":"invalid_answer","message":"Enter a valid answer."}"#.utf8)) }
            answered = true
            return (204, Data())
        }
        if request.httpMethod == "POST" {
            let body = try JSONSerialization.jsonObject(with: requestBody(request)) as! [String: String]
            #expect(body["type"] == "openai")
            #expect(body["auth"] == "api_key")
            return (200, try JSONEncoder().encode(ProviderLogin(id: "login", revision: 1,
                prompt: LoginPrompt(id: 1, type: "secret", message: "Enter key"))))
        }
        #expect(request.timeoutInterval == 35)
        #expect(URLComponents(url: request.url!, resolvingAgainstBaseURL: false)?.queryItems?.first?.value == "1")
        while !answered { try await Task.sleep(for: .milliseconds(10)) }
        return (200, try JSONEncoder().encode(ProviderLogin(id: "login", revision: 2, done: true,
            provider: Provider(id: "connected", type: "openai", name: "OpenAI", enabled: true))))
    }
}

@Suite @MainActor struct ProviderTests {
    @Test func savesConnectionOrder() async throws {
        let host = ProbeProtocol.routes.add { request in
            #expect(request.url?.path == "/providers/order")
            #expect(request.httpMethod == "PUT")
            #expect(try JSONDecoder().decode([String].self, from: requestBody(request)) == ["second", "first"])
            return (200, try JSONEncoder().encode([Provider(id: "second", order: 0), Provider(id: "first", order: 1)]))
        }
        defer { ProbeProtocol.routes.remove(host) }
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [ProbeProtocol.self]
        let client = APIClient(baseURL: URL(string: "http://\(host)")!, session: URLSession(configuration: config), token: "provider-token")
        let providers = try await client.orderProviders(["second", "first"])
        #expect(providers.map(\.id) == ["second", "first"])
    }

    private func setup(_ probe: ProviderProbe) -> (ProviderSignIn, String) {
        let host = ProbeProtocol.routes.add { try await probe.handle($0) }
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [ProbeProtocol.self]
        let client = APIClient(baseURL: URL(string: "http://\(host)")!, session: URLSession(configuration: config), token: "provider-token")
        return (ProviderSignIn(client: client), host)
    }

    @Test func loginAnswersAndPublishesProvider() async throws {
        let probe = ProviderProbe()
        let (store, host) = setup(probe)
        defer { ProbeProtocol.routes.remove(host) }
        let worker = Task { await store.connect(option: ProviderOption(type: "openai", auth: "api_key")) }
        for _ in 0..<100 {
            if store.state?.prompt != nil { break }
            try await Task.sleep(for: .milliseconds(10))
        }
        #expect(store.state?.prompt?.type == "secret")
        await store.submit("key")
        await worker.value
        #expect(store.state?.done == true)
        #expect(store.state?.provider?.id == "connected")
        #expect(store.issue == nil)
        #expect(await probe.cancellations == 0)
    }

    @Test func cancelledLoginStopsServerSession() async throws {
        let probe = ProviderProbe()
        let (store, host) = setup(probe)
        defer { ProbeProtocol.routes.remove(host) }
        let worker = Task { await store.connect(option: ProviderOption(type: "openai", auth: "api_key")) }
        for _ in 0..<100 {
            if store.state != nil { break }
            try await Task.sleep(for: .milliseconds(10))
        }
        worker.cancel()
        await worker.value
        for _ in 0..<100 {
            if await probe.cancellations == 1 { break }
            try await Task.sleep(for: .milliseconds(10))
        }
        #expect(await probe.cancellations == 1)
    }

    @Test func refusedAnswerRestoresPrompt() async throws {
        let probe = ProviderProbe()
        await probe.rejectAnswer()
        let (store, host) = setup(probe)
        defer { ProbeProtocol.routes.remove(host) }
        let worker = Task { await store.connect(option: ProviderOption(type: "openai", auth: "api_key")) }
        for _ in 0..<100 {
            if store.state?.prompt != nil { break }
            try await Task.sleep(for: .milliseconds(10))
        }
        await store.submit("key")
        #expect(store.state?.prompt?.id == 1)
        #expect(store.issue == "Enter a valid answer.")
        worker.cancel()
        await worker.value
    }
}

@Test func legacyNoticePage() throws {
    let data = Data(#"{"cursor":7,"notices":[{"seq":7,"conversationID":"chat","messageID":"reply","title":"Polymux","createdAt":"2026-10-13T08:00:00Z"}]}"#.utf8)
    let page = try JSONDecoder().decode(NoticePage.self, from: data)
    #expect(page.cursor == 7)
    #expect(page.notices.first?.body == nil)
    #expect(page.notices.first?.outcome == nil)
}

@Suite
struct LocalProofTests {
    /// Answers the proof with the given secret, as a server holding it would.
    private func client(secret: String) -> (URL, URLSession, String) {
        let host = ProbeProtocol.routes.add { request in
            #expect(request.value(forHTTPHeaderField: "Authorization") == nil)
            let nonce = (try? JSONDecoder().decode([String: String].self, from: requestBody(request)))?["nonce"] ?? ""
            let proof = HMAC<SHA256>.authenticationCode(for: Data("polymux-local-proof:\(request.url?.port ?? 80):\(nonce)".utf8), using: SymmetricKey(data: Data(secret.utf8)))
                .map { String(format: "%02x", $0) }.joined()
            return (200, try! JSONEncoder().encode(["proof": proof]))
        }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [ProbeProtocol.self]
        return (URL(string: "http://\(host)")!, URLSession(configuration: configuration), host)
    }

    @Test func realServerProvesOwnerToken() async {
        let (url, session, host) = client(secret: "owner-token")
        defer { ProbeProtocol.routes.remove(host) }
        #expect(await APIClient.proves("owner-token", url: url, session: session))
    }

    @Test func impostorCannotProve() async {
        let (url, session, host) = client(secret: "guess")
        defer { ProbeProtocol.routes.remove(host) }
        #expect(!(await APIClient.proves("owner-token", url: url, session: session)))
    }
}
