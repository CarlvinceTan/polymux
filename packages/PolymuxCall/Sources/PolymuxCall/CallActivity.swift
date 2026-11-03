#if os(iOS)
import ActivityKit
import AppIntents
import Foundation

/// A voice call as the lock screen and Dynamic Island show it.
public struct CallAttributes: ActivityAttributes {
    public struct ContentState: Codable, Hashable, Sendable {
        public var status: String
        public var muted: Bool
        /// When the call connected; nil while it is still dialing.
        public var started: Date?

        public init(status: String, muted: Bool, started: Date?) {
            self.status = status
            self.muted = muted
            self.started = started
        }
    }

    public var name: String

    public init(name: String) { self.name = name }
}

public extension Notification.Name {
    static let callEnd = Notification.Name("com.flarehq.polymux.call.end")
}

/// Runs in the app process, so the call hangs up without opening the app.
public struct EndCall: LiveActivityIntent {
    public static let title: LocalizedStringResource = "End call"

    public init() {}

    public func perform() async throws -> some IntentResult {
        await MainActor.run { NotificationCenter.default.post(name: .callEnd, object: nil) }
        return .result()
    }
}
#endif
