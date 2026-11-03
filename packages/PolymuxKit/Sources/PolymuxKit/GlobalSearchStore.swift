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
    /// The chat a profile's search is limited to; nil searches everything.
    @ObservationIgnored private let scope: String?

    public init(client: APIClient, actor: LocalCacheActor, scope: String? = nil) {
        self.client = client
        self.cache = actor
        self.scope = scope
    }

    public func search(query: String, kind: String) async {
        generation += 1
        let current = generation
        let query = query.trimmingCharacters(in: .whitespacesAndNewlines)
        isSearching = true
        error = nil
        defer { if generation == current { isSearching = false } }
        if let cached = try? await cache.call({ try $0.globalSearch(query: query, kind: kind, scope: scope) }) {
            guard generation == current, !Task.isCancelled else { return }
            results = cached
        }
        do {
            if !query.isEmpty { try await Task.sleep(for: .milliseconds(250)) }
            let found = try await client.search(query: query, kind: kind, scope: scope)
            guard generation == current, !Task.isCancelled else { return }
            results = found
            isOffline = false
            try? await cache.call { try $0.cacheSearch(found, query: query, kind: kind, scope: scope) }
        } catch {
            guard generation == current, !Task.isCancelled else { return }
            isOffline = error.isOffline
        }
    }
}
