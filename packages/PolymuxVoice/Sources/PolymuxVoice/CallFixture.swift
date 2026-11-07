#if DEBUG
import Foundation
import Observation

/// A connected call with no provider, for checking the call screen and Live Activity.
@MainActor
@Observable
public final class CallFixture: VoiceSession {
    public static var enabled: Bool { CommandLine.arguments.contains("--verify-call") }
    public private(set) var phase = VoicePhase.speaking
    public let transcript = "Can you check my calendar for tomorrow"
    public let response = ""
    public let level: Float = 0
    public let issue: String? = nil
    public private(set) var muted = false
    public private(set) var silenced = false
    public private(set) var connected: Date? = Date().addingTimeInterval(-83)
    public var working: Int { CommandLine.arguments.contains("--verify-call-working") ? 1 : 0 }

    public func start() async {}
    public func stop() async {}
    public func retry() async {}
    public func toggleMute() { muted.toggle() }
    public func toggleSilence() { silenced.toggle() }
}
#endif
