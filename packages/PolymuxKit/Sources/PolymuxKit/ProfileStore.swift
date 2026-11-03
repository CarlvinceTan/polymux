import Foundation
import Observation

@MainActor @Observable
public final class ProfileStore {
    public private(set) var agent: Agent?
    public private(set) var prompt: String?
    public private(set) var routines: [Routine]?
    public private(set) var configuration: AgentConfiguration?
    public private(set) var connections: [MCPConnection]?
    public private(set) var issue: String?
    public private(set) var cancelIssue: String?
    /// Whether a refresh has finished, so a section still without data failed to load.
    public private(set) var settled = false

    @ObservationIgnored private let agentID: String
    @ObservationIgnored private let client: APIClient
    @ObservationIgnored private let cache: LocalCacheActor
    @ObservationIgnored private var restored = false
    @ObservationIgnored private var restoring: Task<Void, Never>?
    @ObservationIgnored private var refreshing: Task<Void, Never>?
    @ObservationIgnored private var checkedAt: ContinuousClock.Instant?
    @ObservationIgnored private var profileRevision = 0
    @ObservationIgnored private var routineRevision = 0
    @ObservationIgnored private var cancelling: Set<String> = []

    public init(agentID: String, client: APIClient, cache: LocalCacheActor) {
        self.agentID = agentID
        self.client = client
        self.cache = cache
    }

    public func restore() async {
        if let restoring { await restoring.value; return }
        guard !restored else { return }
        let task = Task {
            if let saved: ProfileSnapshot = await cache.snapshot(key) {
                prompt = saved.prompt
                routines = saved.routines
                configuration = saved.configuration
                connections = saved.connections
            }
            restored = true
        }
        restoring = task
        await task.value
        restoring = nil
    }

    /// Each section publishes independently; leaving the panel doesn't discard an in-flight refresh.
    public func refresh(maxAge: Duration = .seconds(2)) async {
        await restore()
        if let refreshing { await refreshing.value; return }
        if let checkedAt, checkedAt.duration(to: .now) < maxAge { return }
        let task = Task {
            async let profile: () = loadProfile()
            async let schedule: () = loadRoutines()
            async let setup: () = loadConfiguration()
            async let mcp: () = loadConnections()
            _ = await (profile, schedule, setup, mcp)
            checkedAt = .now
            settled = true
        }
        refreshing = task
        await task.value
        refreshing = nil
    }

    public func accept(_ profile: AgentProfile) async {
        profileRevision += 1
        agent = profile.agent
        prompt = profile.prompt
        issue = nil
        await persist()
    }

    public func cancel(_ routine: Routine) async {
        guard cancelling.insert(routine.id).inserted else { return }
        let index = routines?.firstIndex(where: { $0.id == routine.id }) ?? 0
        routineRevision += 1
        routines?.removeAll { $0.id == routine.id }
        cancelIssue = nil
        await persist()
        do {
            _ = try await client.deleteRoutine(agentID: agentID, routineID: routine.id)
        } catch {
            if routines?.contains(where: { $0.id == routine.id }) == false {
                let insertion = min(index, routines?.count ?? 0)
                routines?.insert(routine, at: insertion)
            }
            cancelIssue = error.isOffline ? "Not cancelled yet." : error.reason(or: "Couldn’t cancel that routine.")
        }
        cancelling.remove(routine.id)
        routineRevision += 1
        await persist()
    }

    private var key: String { "profile/" + agentID }

    private func loadProfile() async {
        let revision = profileRevision
        do {
            let profile = try await client.agentProfile(id: agentID)
            guard revision == profileRevision else { return }
            issue = nil
            guard agent != profile.agent || prompt != profile.prompt else { return }
            agent = profile.agent
            prompt = profile.prompt
            await persist()
        } catch {
            issue = error.isOffline ? nil : "Couldn’t load this profile."
        }
    }

    private func loadRoutines() async {
        let revision = routineRevision
        guard let fresh = try? await client.routines(agentID: agentID), revision == routineRevision else { return }
        let visible = fresh.filter { !cancelling.contains($0.id) }
        guard routines != visible else { return }
        routines = visible
        await persist()
    }

    private func loadConfiguration() async {
        guard let fresh = try? await client.configuration(agentID: agentID), configuration != fresh else { return }
        configuration = fresh
        await persist()
    }

    private func loadConnections() async {
        guard let fresh = try? await client.connections(agentID: agentID), connections != fresh else { return }
        connections = fresh
        await persist()
    }

    private func persist() async {
        await cache.saveSnapshot(key, ProfileSnapshot(prompt: prompt, routines: routines,
                                                     configuration: configuration, connections: connections))
    }
}

struct ProfileSnapshot: Codable, Sendable {
    var prompt: String?
    var routines: [Routine]?
    var configuration: AgentConfiguration?
    var connections: [MCPConnection]?
}
