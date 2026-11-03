import Foundation
@testable import PolymuxKit

/// Awaitable cache access for tests. Production code has no blocking hop.
enum CacheWork {
    static func open(_ url: URL) async throws -> LocalCache {
        try await Task.detached { try LocalCache(url: url) }.value
    }

    static func untilSaved(_ sidebar: SidebarStore) async {
        await sidebar.settled()
    }
}

extension LocalCache {
    @discardableResult
    func offMain<T: Sendable>(_ body: @escaping @Sendable (LocalCache) throws -> T) async throws -> T {
        let cache = self
        return try await Task.detached { try body(cache) }.value
    }
}
