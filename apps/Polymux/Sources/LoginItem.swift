#if os(macOS)
import ServiceManagement
import Observation

/// Whether the app opens when this Mac's user logs in.
@MainActor @Observable
final class LoginItem {
    static let shared = LoginItem()

    private(set) var enabled = SMAppService.mainApp.status == .enabled
    private(set) var issue: String?

    /// Catches a change made in System Settings.
    func refresh() {
        enabled = SMAppService.mainApp.status == .enabled
    }

    func set(_ on: Bool) {
        do {
            if on { try SMAppService.mainApp.register() } else { try SMAppService.mainApp.unregister() }
            issue = SMAppService.mainApp.status == .requiresApproval ? "Approve Polymux in System Settings › Login Items." : nil
        } catch {
            issue = "Couldn’t change launch at login."
        }
        refresh()
    }
}
#endif
