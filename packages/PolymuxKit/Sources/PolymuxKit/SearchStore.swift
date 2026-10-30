import Foundation
import Observation

@MainActor @Observable
public final class SearchStore {
    public var query = ""
    public private(set) var results: [SearchHit] = []
    public private(set) var isOffline = false
    public private(set) var isSearching = false
    public private(set) var error: String?
    @ObservationIgnored private let client: APIClient
    @ObservationIgnored private let cacheActor: LocalCacheActor
    @ObservationIgnored private let threadID: String
    @ObservationIgnored private var generation = 0

    public init(client: APIClient, actor: LocalCacheActor, threadID: String) {
        self.client = client
        self.cacheActor = actor
        self.threadID = threadID
    }

    public func search() async {
        generation += 1
        let current = generation
        let query = query.trimmingCharacters(in: .whitespacesAndNewlines)
        error = nil
        guard !query.isEmpty else { results = []; isSearching = false; isOffline = false; return }
        isSearching = true
        defer { if current == generation { isSearching = false } }
        let key = "chat-search/" + threadID + "/" + query
        let threadID = threadID
        let saved: [SearchHit] = await cacheActor.snapshot(key) ?? []
        let local = (try? await cacheActor.call { try $0.search(threadID: threadID, query: query) }) ?? []
        guard current == generation, !Task.isCancelled else { return }
        let ids = Set(saved.map(\.message.id))
        results = saved + local.filter { !ids.contains($0.message.id) }
        do {
            try await Task.sleep(for: .milliseconds(250))
            let results = try await client.search(threadID: threadID, query: query)
            guard current == generation, !Task.isCancelled else { return }
            self.results = results
            isOffline = false
            try? await cacheActor.call { try $0.merge(results.map(\.message)) }
            await cacheActor.saveSnapshot(key, results)
        } catch {
            guard current == generation, !Task.isCancelled else { return }
            isOffline = error.isOffline
        }
    }
}
