import Foundation
import Observation

/// Polymux's own activity and what a plugin's service adds to it, saved for offline viewing.
@MainActor @Observable
public final class WorkStore {
    public private(set) var activity: WorkActivity?
    public private(set) var reported: WorkGraph?
    /// Both sources have answered or failed, so what's on show is as complete as it'll get.
    public private(set) var settled = false
    @ObservationIgnored private var askedActivity = false
    @ObservationIgnored private var askedReports = false
    @ObservationIgnored private let client: APIClient
    @ObservationIgnored private let cache: LocalCacheActor

    public init(client: APIClient, cache: LocalCacheActor) {
        self.client = client
        self.cache = cache
    }

    public func restore() async {
        if activity == nil, let saved: WorkActivity = await cache.snapshot("work-activity") { activity = saved }
        if reported == nil, let saved: WorkGraph = await cache.snapshot("work-reported") { reported = saved }
    }

    /// Only reads, so looking at the graph can't mark anything read or wake a bot.
    public func refreshActivity() async {
        if let next = try? await client.work(), next != activity {
            activity = next
            await cache.saveSnapshot("work-activity", next)
        }
        askedActivity = true
        settled = askedActivity && askedReports
    }

    /// Runs apart from Polymux's own activity, so a stuck plugin can't hold it up.
    public func refreshReported(plugin: String) async {
        if let next = try? await client.workGraph(plugin: plugin), next != reported {
            reported = next
            await cache.saveSnapshot("work-reported", next)
        }
        askedReports = true
        settled = askedActivity && askedReports
    }
}
