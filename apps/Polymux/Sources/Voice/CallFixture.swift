#if DEBUG
import Foundation
import Observation

/// A connected call with no provider, for checking the call screen and Live Activity.
@MainActor
@Observable
final class CallFixture: VoiceSession {
    static var enabled: Bool { CommandLine.arguments.contains("--verify-call") }
    private(set) var phase = VoicePhase.speaking
    let transcript = "Can you check my calendar for tomorrow"
    let response = ""
    let level: Float = 0
    let issue: String? = nil
    private(set) var muted = false
    private(set) var silenced = false
    private(set) var connected: Date? = Date().addingTimeInterval(-83)
    var working: Int { CommandLine.arguments.contains("--verify-call-working") ? 1 : 0 }

    func start() async {}
    func stop() async {}
    func retry() async {}
    func toggleMute() { muted.toggle() }
    func toggleSilence() { silenced.toggle() }
}
#endif
