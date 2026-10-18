import Dispatch
import Foundation

/// Background owner of one `LocalCache` connection.
/// Work entered here is never on the main queue.
public actor LocalCacheActor {
    private let cache: LocalCache
    private var saves = 0
    private var marks = 0
    private var stall: Duration = .zero
    private var failing = false
    private var instant = false
    private var parkNextEntry = false
    private var parked: CheckedContinuation<Void, Never>?
    private var deliveries: [String: Task<Void, any Error>] = [:]

    public init(_ cache: LocalCache) {
        self.cache = cache
    }

    public var saveCount: Int { saves }
    public var markCount: Int { marks }

    public func setStall(_ duration: Duration) { stall = duration }
    public func setFailing(_ enabled: Bool) { failing = enabled }

    /// Counts the save and returns before SQLite, so a coalesce test is not limited by disk.
    public func setInstant(_ enabled: Bool) { instant = enabled }

    /// Holds the next cache entry until `release()`. Tests overlap a save with a stream or reload.
    public func parkNext() { parkNextEntry = true }
    public var isParked: Bool { parked != nil }
    public func release() {
        parkNextEntry = false
        parked?.resume()
        parked = nil
    }

    /// Holds the actor without touching SQLite. Used to prove the main actor keeps running.
    public func touch() async throws {
        try await enter()
    }

    public func save(agents: [Agent], threads: [Thread], edits: [Thread]? = nil, pinEdits: [String]? = nil, dirtyPins: [String]? = nil) async throws {
        try await enter()
        saves += 1
        if failing { throw ClientError.cache("forced") }
        if instant { return }
        if let edits { try cache.saveEdits(edits, pins: pinEdits, dirtyPins: dirtyPins) }
        try cache.save(agents: agents, threads: threads)
    }

    public func markRead(threadID: String, seq: Int64) async throws {
        try await enter()
        marks += 1
        try cache.markRead(threadID: threadID, seq: seq)
    }

    public func call<T: Sendable>(_ body: @Sendable (LocalCache) throws -> T) async throws -> T {
        try await enter()
        return try body(cache)
    }

    func deliver(client: APIClient, threadID: String, request: SendRequest) async throws {
        try await enter()
        if let delivery = deliveries[request.id] {
            try await delivery.value
            return
        }
        // Both stores can read the outbox before either finishes delivery.
        guard try cache.pending(threadID: threadID).contains(where: { $0.id == request.id }) else { return }
        let delivery = Task {
            try await client.send(threadID: threadID, request: request)
            try await self.call { try $0.acknowledge(id: request.id) }
        }
        deliveries[request.id] = delivery
        defer { deliveries[request.id] = nil }
        try await delivery.value
    }

    private func block(_ duration: Duration) {
        let clock = ContinuousClock()
        let start = clock.now
        while start.duration(to: clock.now) < duration {}
    }

    private func enter() async throws {
        #if DEBUG
        // Debug builds trap if a cache call is still on the main queue.
        dispatchPrecondition(condition: .notOnQueue(.main))
        #endif
        if parkNextEntry {
            parkNextEntry = false
            await withCheckedContinuation { parked = $0 }
        }
        // A sleep would yield, so a stall on the main thread would not miss the heartbeat.
        if stall > .zero { block(stall) }
    }
}
