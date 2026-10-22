import Foundation
import Observation

@MainActor @Observable
public final class GlobalSearchStore {
    public private(set) var results: [SearchResult] = []
    public private(set) var isSearching = false
    public private(set) var isOffline = false
    public private(set) var error: String?
    @ObservationIgnored private let client: APIClient
    @ObservationIgnored private let cache: LocalCacheActor
    @ObservationIgnored private var generation = 0

    public init(client: APIClient, actor: LocalCacheActor) {
        self.client = client
        self.cache = actor
    }

    public func search(query: String, kind: String) async {
        generation += 1
        let current = generation
        let query = query.trimmingCharacters(in: .whitespacesAndNewlines)
        isSearching = true
        error = nil
        results = []
        defer { if generation == current { isSearching = false } }
        if let cached = try? await cache.call({ try $0.globalSearch(query: query, kind: kind) }) {
            guard generation == current, !Task.isCancelled else { return }
            results = cached
        }
        do {
            if !query.isEmpty { try await Task.sleep(for: .milliseconds(250)) }
            let found = try await client.search(query: query, kind: kind)
            guard generation == current, !Task.isCancelled else { return }
            results = found
            isOffline = false
            try? await cache.call { try $0.cacheSearch(found, query: query, kind: kind) }
        } catch {
            guard generation == current, !Task.isCancelled else { return }
            isOffline = true
            self.error = "Search is unavailable. Showing downloaded content."
        }
    }
}
