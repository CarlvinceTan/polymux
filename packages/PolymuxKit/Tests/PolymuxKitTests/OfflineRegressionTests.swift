import Foundation
import PolymuxKit
import Testing

@Suite(.serialized) @MainActor
struct OfflineRegressionTests {
    @Test func offlinePaginationShowsCachedHistory() async throws {
        let directory = FileManager.default.temporaryDirectory.appending(path: "polymux-offline-\(UUID())")
        defer { try? FileManager.default.removeItem(at: directory) }
        let cache = try await Task.detached {
            let cache = try LocalCache(url: directory.appending(path: "cache.sqlite"))
            let messages = (1...150).map { n in
                Message(id: "history-\(n)", threadID: "cached", role: "user", parts: [Part(kind: "text", text: "Cached history \(n)")], createdAt: "2026-10-23T00:00:00Z", status: "done", seq: Int64(n))
            }
            try cache.merge(messages)
            return cache
        }.value
        let client = APIClient(baseURL: URL(string: "http://127.0.0.1:1")!, token: "")
        let store = ChatStore(client: client, actor: LocalCacheActor(cache), threadID: "cached")
        await store.ready()
        #expect(store.messages.count == 100)
        await store.loadEarlier()
        #expect(store.messages.count == 150, "Cached older messages must remain accessible without the server")
    }

    @Test func scopedOfflineSearchLimitsWithinScope() async throws {
        let directory = FileManager.default.temporaryDirectory.appending(path: "polymux-offline-\(UUID())")
        defer { try? FileManager.default.removeItem(at: directory) }
        let counts = try await Task.detached {
            let cache = try LocalCache(url: directory.appending(path: "cache.sqlite"))
            try cache.save(agents: [Agent(id: "target", name: "Target"), Agent(id: "other", name: "Other")],
                           threads: [PolymuxKit.Thread(id: "target", agentID: "target"), PolymuxKit.Thread(id: "other", agentID: "other")])
            var messages = (1...60).map { n in
                Message(id: "strong-\(n)", threadID: "other", role: "user", parts: [Part(kind: "text", text: String(repeating: "needle ", count: 20))], createdAt: "2026-10-23T00:00:00Z", status: "done", seq: Int64(n))
            }
            messages.append(Message(id: "target-hit", threadID: "target", role: "user", parts: [Part(kind: "text", text: "needle " + String(repeating: "filler ", count: 200))], createdAt: "2026-10-23T00:00:00Z", status: "done", seq: 1))
            try cache.merge(messages)
            return (try cache.search(threadID: "target", query: "needle").count,
                    try cache.globalSearch(query: "needle", kind: "messages", scope: "target").count)
        }.value
        #expect(counts.0 == 1)
        #expect(counts.1 == 1, "Chat-scoped offline search must not lose matches behind other chats' 50-result limit")
    }

    @Test func offlineSearchFindsNewQueries() async throws {
        let directory = FileManager.default.temporaryDirectory.appending(path: "polymux-offline-\(UUID())")
        defer { try? FileManager.default.removeItem(at: directory) }
        let cache = try await Task.detached {
            let cache = try LocalCache(url: directory.appending(path: "cache.sqlite"))
            try cache.save(agents: [Agent(id: "a", name: "Lyla"), Agent(id: "b", name: "Bolt")],
                           threads: [PolymuxKit.Thread(id: "a", agentID: "a"), PolymuxKit.Thread(id: "b", agentID: "b")])
            let file = Attachment(id: "file", name: "itinerary.pdf", mimeType: "application/pdf",
                                  extractedText: "Vaccination requirements")
            try cache.merge([
                Message(id: "one", threadID: "a", parts: [Part(kind: "text", text: "Passport renewal")], seq: 1),
                Message(id: "two", threadID: "b", parts: [Part(kind: "text", text: "Passport checklist"),
                                                        Part(kind: "attachment", attachment: file)], seq: 1)
            ])
            return cache
        }.value
        let client = APIClient(baseURL: URL(string: "http://127.0.0.1:1")!, token: "")
        let actor = LocalCacheActor(cache)
        let global = GlobalSearchStore(client: client, actor: actor)
        for (query, kind, ids) in [("pass", "messages", ["message/one", "message/two"]),
                                   ("vacc", "files", ["attachment/file"]),
                                   ("missing", "all", [])] {
            await global.search(query: query, kind: kind)
            #expect(Set(global.results.map(\.id)) == Set(ids))
            #expect(global.isOffline)
            #expect(!global.isSearching)
        }
        let chat = SearchStore(client: client, actor: actor, threadID: "a")
        chat.query = "pass"
        await chat.search()
        #expect(chat.results.map(\.message.id) == ["one"])
        #expect(chat.isOffline)
        #expect(chat.error == nil)
    }

    @Test func successfulUnrelatedSettingPreservesRetry() async throws {
        SettingsMock.state.reset()
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [SettingsMock.self]
        let client = APIClient(baseURL: URL(string: "http://settings.invalid")!, session: URLSession(configuration: configuration), token: "")
        let store = SettingsStore(client: client)
        await store.refresh()
        store.edit(SettingsPatch(appearance: "dark"))
        _ = await store.synchronize()
        store.edit(SettingsPatch(defaultReasoning: "high"))
        _ = await store.synchronize()
        await store.retry()
        #expect(SettingsMock.state.snapshot().appearance == "dark", "The unrelated successful write must not erase an earlier promised retry")
    }
}

final class SettingsMock: URLProtocol, @unchecked Sendable {
    final class State: @unchecked Sendable {
        let lock = NSLock()
        var settings = Settings(revision: 1, appearance: "light")
        var patches = 0
        func reset() { lock.withLock { settings = Settings(revision: 1, appearance: "light"); patches = 0 } }
        func snapshot() -> Settings { lock.withLock { settings } }
        func update(_ patch: SettingsPatch) throws -> Settings {
            try lock.withLock {
                patches += 1
                if patches == 1 { throw URLError(.notConnectedToInternet) }
                if let value = patch.appearance { settings.appearance = value }
                if let value = patch.defaultReasoning { settings.defaultReasoning = value }
                settings.revision += 1
                return settings
            }
        }
    }
    static let state = State()
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        do {
            let settings: Settings
            if request.httpMethod == "PATCH" {
                let data: Data
                if let body = request.httpBody { data = body }
                else if let stream = request.httpBodyStream {
                    stream.open(); defer { stream.close() }
                    var body = Data(); var buffer = [UInt8](repeating: 0, count: 4096)
                    while stream.hasBytesAvailable { let n = stream.read(&buffer, maxLength: buffer.count); if n <= 0 { break }; body.append(contentsOf: buffer.prefix(n)) }
                    data = body
                } else { data = Data("{}".utf8) }
                settings = try Self.state.update(JSONDecoder().decode(SettingsPatch.self, from: data))
            } else { settings = Self.state.snapshot() }
            let response = HTTPURLResponse(url: request.url!, statusCode: 200, httpVersion: nil, headerFields: ["Content-Type": "application/json"])!
            client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: try JSONEncoder().encode(settings))
            client?.urlProtocolDidFinishLoading(self)
        } catch { client?.urlProtocol(self, didFailWithError: error) }
    }
    override func stopLoading() {}
}
