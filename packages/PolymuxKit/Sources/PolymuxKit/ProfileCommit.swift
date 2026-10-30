import Foundation
import Observation

/// Saves avatar designs optimistically; a failure restores the previous design and says so.
@MainActor @Observable
public final class ProfileCommit {
    public private(set) var notice: String?
    public let trace = SignpostTrace()
    @ObservationIgnored private var revisions: [String: UUID] = [:]

    public init() {}

    public func report(_ message: String) { notice = message }

    public func clearNotice() { notice = nil }

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
                notice = nil
            } catch {
                guard revisions[key] == token else { return }
                apply(previous)
                report("Couldn’t save the avatar. Previous appearance restored.")
            }
        }
    }
}
