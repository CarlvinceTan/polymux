import Foundation
import Observation

@MainActor @Observable
public final class ProfileCommit {
    public private(set) var profiles: [String: AgentProfile] = [:]
    public private(set) var notice: String?
    public let trace = SignpostTrace()
    @ObservationIgnored private var revisions: [String: UUID] = [:]
    @ObservationIgnored private var retry: (() -> Void)?

    public init() {}

    public func report(_ message: String, retry: @escaping () -> Void) {
        notice = message
        self.retry = retry
    }

    public func tryAgain() {
        let action = retry
        notice = nil
        retry = nil
        action?()
    }

    @discardableResult
    public func saveAvatar(id: String, previous: AvatarDesign, request: AvatarDesign,
                           sidebar: SidebarStore, client: APIClient,
                           apply: @escaping (AvatarDesign) -> Void,
                           close: () -> Void) -> Task<Void, Never> {
        let key = "avatar:" + id
        let token = UUID()
        revisions[key] = token
        InstantUI.interval("AvatarSave", budget: InstantUI.stateBudget, trace: trace) {
            apply(request)
            close()
        }
        return Task {
            do {
                let result = try await client.updateAvatar(id: id, request: request)
                guard revisions[key] == token else { return }
                sidebar.publish(result)
                apply(result.avatar ?? request)
            } catch {
                guard revisions[key] == token else { return }
                apply(previous)
                report("Couldn’t save the avatar. Previous appearance restored.") { [self] in
                    saveAvatar(id: id, previous: previous, request: request,
                               sidebar: sidebar, client: client, apply: apply, close: {})
                }
            }
        }
    }

    @discardableResult
    public func save(_ previous: AgentProfile, request: UpdateAgentRequest,
                     sidebar: SidebarStore, client: APIClient,
                     from tap: ContinuousClock.Instant = ContinuousClock().now,
                     close: () -> Void) -> Task<Void, Never> {
        let id = previous.agent.id
        let token = UUID()
        let baseline = profiles[id] ?? previous
        var optimistic = baseline
        optimistic.agent.name = request.name
        optimistic.agent.role = request.role
        optimistic.prompt = request.prompt
        revisions[id] = token
        InstantUI.interval("ProfileSave", budget: InstantUI.stateBudget, trace: trace, from: tap) {
            profiles[id] = optimistic
            sidebar.publish(optimistic.agent)
            close()
        }
        return Task {
            do {
                let result = try await client.updateAgent(id: id, request: request)
                guard revisions[id] == token else { return }
                profiles[id] = result
                sidebar.publish(result.agent)
            } catch {
                guard revisions[id] == token else { return }
                profiles[id] = baseline
                sidebar.publish(baseline.agent)
                report("Couldn’t save the profile or role description. Previous values restored.") { [self] in
                    save(baseline, request: request, sidebar: sidebar, client: client, close: {})
                }
            }
        }
    }
}
