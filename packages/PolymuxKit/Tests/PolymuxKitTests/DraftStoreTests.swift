import Foundation
import Testing
@testable import PolymuxKit

@MainActor struct DraftStoreTests {
    @Test func typingDebouncesAndNeverWaitsForNetwork() async throws {
        let f = try await DraftFixture()
        defer { f.close() }
        await f.store.restore()
        await f.backend.hold()
        let start = ContinuousClock.now
        for i in 0..<200 { f.store.edit(ChatDraft(text: "Prompt \(i)")) }
        #expect(start.duration(to: .now) < .milliseconds(50))
        #expect(f.store.value.text == "Prompt 199")
        try await eventually { await f.backend.puts == 1 }
        f.store.edit(ChatDraft(text: "Typed while saving"), after: .zero)
        #expect(f.store.value.text == "Typed while saving")
        await f.backend.release()
        try await eventually { await f.backend.value.value.text == "Typed while saving" }
        #expect(await f.backend.puts == 2)
    }

    @Test func clientsSyncAndClearWithoutEchoing() async throws {
        let f = try await DraftFixture()
        defer { f.close() }
        await f.store.restore()
        f.store.edit(ChatDraft(text: "Half written 👋"), after: .zero)
        try await eventually { await f.backend.value.revision == 1 }
        let other = DraftStore(client: f.client, cache: f.otherCache, threadID: "chat")
        await other.restore()
        await other.refresh()
        #expect(other.value.text == "Half written 👋")
        other.edit(ChatDraft(), after: .zero)
        try await eventually { await f.backend.value.revision == 2 }
        // The active editor waits briefly before accepting remote text.
        try await Task.sleep(for: .milliseconds(2050))
        await f.store.refresh()
        #expect(f.store.value.text.isEmpty)
        #expect(await f.backend.puts == 2)
    }

    @Test func staleResponsesCannotReplaceNewTyping() async throws {
        let f = try await DraftFixture()
        defer { f.close() }
        await f.store.restore()
        await f.backend.set(DraftState(revision: 2, writeID: "remote", value: ChatDraft(text: "Old response")))
        await f.backend.hold()
        let refresh = Task { await f.store.refresh() }
        try await eventually { await f.backend.gets == 1 }
        f.store.edit(ChatDraft(text: "New local input"))
        await f.backend.release()
        await refresh.value
        #expect(f.store.value.text == "New local input")
        f.store.flush()
        try await eventually { f.store.issue != nil }
    }

    @Test func conflictsKeepBothDraftsAcrossRelaunch() async throws {
        let f = try await DraftFixture()
        defer { f.close() }
        await f.store.restore()
        await f.backend.set(DraftState(revision: 3, writeID: "other", value: ChatDraft(text: "Other device")))
        f.store.edit(ChatDraft(text: "Keep my text"), after: .zero)
        try await eventually { f.store.issue != nil }
        // Allow the async cache write following the published conflict to finish.
        try await Task.sleep(for: .milliseconds(50))
        let reopened = DraftStore(client: f.client, cache: f.cache, threadID: "chat")
        await reopened.restore()
        #expect(reopened.value.text == "Keep my text")
        #expect(reopened.issue != nil)
        reopened.flush()
        try await Task.sleep(for: .milliseconds(50))
        #expect(await f.backend.value.value.text == "Other device")
        #expect(await f.backend.puts == 1)
    }

    @Test func offlineDraftAndLostAcknowledgementRecover() async throws {
        let f = try await DraftFixture()
        defer { f.close() }
        await f.store.restore()
        await f.backend.setOffline(true)
        f.store.edit(ChatDraft(text: "Offline prompt"), after: .zero)
        try await eventually { await f.backend.puts == 1 }
        let reopened = DraftStore(client: f.client, cache: f.cache, threadID: "chat")
        await reopened.restore()
        #expect(reopened.value.text == "Offline prompt")
        await f.backend.setOffline(false)
        await f.backend.loseAcknowledgement()
        reopened.flush()
        try await eventually { await f.backend.value.revision == 1 }
        reopened.flush()
        try await eventually { await f.backend.puts == 3 }
        #expect(await f.backend.value.revision == 1)
        #expect(reopened.issue == nil)
    }

    @Test func newerTypingSurvivesLostAcknowledgementAndRelaunch() async throws {
        let f = try await DraftFixture()
        defer { f.close() }
        await f.store.restore()
        await f.backend.loseAcknowledgement()
        f.store.edit(ChatDraft(text: "Committed before disconnect"), after: .zero)
        try await eventually { await f.backend.value.revision == 1 }
        await f.backend.setOffline(true)
        f.store.edit(ChatDraft(text: "Newer offline input"), after: .zero)
        try await eventually { await f.backend.puts >= 2 }
        let reopened = DraftStore(client: f.client, cache: f.cache, threadID: "chat")
        await reopened.restore()
        #expect(reopened.value.text == "Newer offline input")
        await f.backend.setOffline(false)
        reopened.flush()
        try await eventually { await f.backend.value.value.text == "Newer offline input" }
        #expect(await f.backend.value.revision == 2)
        #expect(reopened.issue == nil)
    }

    @Test func restoresOldDraftAndReply() async throws {
        let f = try await DraftFixture()
        defer { f.close() }
        struct Old: Codable, Sendable { var text: String; var mentions: [ChatDraft.Mention] }
        let mention = ChatDraft.Mention(id: "agent", text: "@Aria", location: 0, length: 5)
        let quote = ReplyContext(messageID: "quoted", role: "assistant", text: "Earlier reply")
        await f.cache.saveSnapshot("draft/chat", Old(text: "@Aria before update", mentions: [mention]))
        await f.cache.saveSnapshot("reply/chat", quote)
        async let first: Void = f.store.restore()
        async let second: Void = f.store.restore()
        _ = await (first, second)
        #expect(f.store.value.text == "@Aria before update")
        #expect(f.store.value.mentions == [mention])
        #expect(f.store.value.reply == quote)
        f.store.flush()
        try await eventually { await f.backend.value.value.text == "@Aria before update" }
    }

    private func eventually(_ condition: () async -> Bool) async throws {
        let deadline = ContinuousClock.now.advanced(by: .seconds(4))
        while !(await condition()), ContinuousClock.now < deadline { try await Task.sleep(for: .milliseconds(5)) }
        #expect(await condition())
    }
}

@MainActor private struct DraftFixture {
    let folder: URL
    let cache: LocalCacheActor
    let otherCache: LocalCacheActor
    let backend = DraftBackend()
    let client: APIClient
    let store: DraftStore
    let host = UUID().uuidString.lowercased() + ".draft.test"

    init() async throws {
        folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        cache = LocalCacheActor(try await CacheWork.open(folder.appendingPathComponent("a/cache.db")))
        otherCache = LocalCacheActor(try await CacheWork.open(folder.appendingPathComponent("b/cache.db")))
        DraftProtocol.lock.withLock { [host, backend] in DraftProtocol.backends[host] = backend }
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [DraftProtocol.self]
        client = APIClient(baseURL: URL(string: "http://" + host)!, session: URLSession(configuration: configuration), token: "")
        store = DraftStore(client: client, cache: cache, threadID: "chat")
    }

    func close() {
        Task { await backend.release() }
        _ = DraftProtocol.lock.withLock { DraftProtocol.backends.removeValue(forKey: host) }
        try? FileManager.default.removeItem(at: folder)
    }
}

private actor DraftBackend {
    var value = DraftState()
    var puts = 0
    var gets = 0
    private var held = false
    private var offline = false
    private var loseAck = false
    private var waiters: [CheckedContinuation<Void, Never>] = []
    func hold() { held = true }
    func set(_ value: DraftState) { self.value = value }
    func setOffline(_ value: Bool) { offline = value }
    func loseAcknowledgement() { loseAck = true }
    func release() {
        held = false
        let pending = waiters; waiters = []
        for waiter in pending { waiter.resume() }
    }
    func response(_ request: URLRequest, body: Data) async throws -> (Int, Data) {
        let writing = request.httpMethod == "PUT"
        if writing { puts += 1 } else { gets += 1 }
        var code = 200
        let data: Data
        if writing {
            if offline { throw URLError(.notConnectedToInternet) }
            var next = try JSONDecoder().decode(DraftState.self, from: body)
            if next.writeID == value.writeID { }
            else if next.revision != value.revision { code = 409 }
            else { next.revision += 1; value = next }
            data = try JSONEncoder().encode(value)
        } else { data = try JSONEncoder().encode(value) }
        if held { await withCheckedContinuation { waiters.append($0) } }
        if offline || (writing && loseAck) {
            loseAck = false
            throw URLError(.networkConnectionLost)
        }
        return (code, data)
    }
}

private final class DraftProtocol: URLProtocol, @unchecked Sendable {
    static let lock = NSLock()
    nonisolated(unsafe) static var backends: [String: DraftBackend] = [:]
    private let taskLock = NSLock()
    private var worker: Task<Void, Never>?
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        var body = request.httpBody ?? Data()
        if let stream = request.httpBodyStream {
            stream.open(); defer { stream.close() }
            var bytes = [UInt8](repeating: 0, count: 4096)
            while stream.hasBytesAvailable {
                let n = stream.read(&bytes, maxLength: bytes.count)
                if n <= 0 { break }; body.append(contentsOf: bytes.prefix(n))
            }
        }
        let payload = body
        let task = Task { @Sendable [self] in
            do {
                guard let backend = Self.lock.withLock({ Self.backends[request.url!.host!] }) else { return }
                let (code, data) = try await backend.response(request, body: payload)
                try Task.checkCancellation()
                let response = HTTPURLResponse(url: request.url!, statusCode: code, httpVersion: nil, headerFields: nil)!
                client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
                client?.urlProtocol(self, didLoad: data)
                client?.urlProtocolDidFinishLoading(self)
            } catch { client?.urlProtocol(self, didFailWithError: error) }
        }
        taskLock.withLock { worker = task }
    }
    override func stopLoading() { taskLock.withLock { worker?.cancel() } }
}
