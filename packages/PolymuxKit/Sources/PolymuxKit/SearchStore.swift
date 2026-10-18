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
        guard !query.isEmpty else { results = []; isSearching = false; return }
        isSearching = true
        defer { if current == generation { isSearching = false } }
        do {
            let results = try await client.search(threadID: threadID, query: query)
            guard current == generation else { return }
            self.results = results
            isOffline = false
            error = nil
        } catch {
            guard current == generation else { return }
            do {
                results = try await cacheActor.call { try $0.search(threadID: threadID, query: query) }
                isOffline = true
                self.error = nil
            } catch { self.error = error.localizedDescription }
        }
    }
}
