import Foundation
#if os(iOS)
import ActivityKit
import PolymuxCall
#endif

/// The call's Live Activity on the lock screen and Dynamic Island; a no-op on macOS.
@MainActor
final class CallActivity {
    #if os(iOS)
    private var id: String?
    private var opening = false
    private var last: CallAttributes.ContentState?
    #endif

    func update(name: String, status: String, muted: Bool, started: Date?) {
        #if os(iOS)
        let state = CallAttributes.ContentState(status: status, muted: muted, started: started)
        guard state != last else { return }
        last = state
        if let id {
            Task { await Self.push(id, state) }
            return
        }
        guard !opening, ActivityAuthorizationInfo().areActivitiesEnabled else { return }
        opening = true
        Task {
            let opened = await Self.open(name, state)
            opening = false
            // The call may have ended while the activity was starting.
            if last == nil { await Self.clear() } else { id = opened }
        }
        #endif
    }

    func end() {
        #if os(iOS)
        id = nil
        last = nil
        Task { await Self.clear() }
        #endif
    }

    #if os(iOS)
    private nonisolated static func push(_ id: String, _ state: CallAttributes.ContentState) async {
        for activity in Activity<CallAttributes>.activities where activity.id == id {
            await activity.update(ActivityContent(state: state, staleDate: nil))
        }
    }

    /// Starts the activity, dropping one a killed run left behind.
    private nonisolated static func open(_ name: String, _ state: CallAttributes.ContentState) async -> String? {
        await clear()
        return try? Activity.request(attributes: CallAttributes(name: name), content: ActivityContent(state: state, staleDate: nil)).id
    }

    private nonisolated static func clear() async {
        for activity in Activity<CallAttributes>.activities { await activity.end(nil, dismissalPolicy: .immediate) }
    }
    #endif
}
