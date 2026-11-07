import Foundation
import PolymuxKit
import SpeechKit

public enum VoicePhase: String {
    case preparing = "Preparing voice"
    case listening = "Listening"
    case thinking = "Thinking"
    case speaking = "Speaking"
    case interrupted = "Interrupted"
    case failed = "Voice unavailable"
}

@MainActor
public protocol VoiceSession: AnyObject {
    var phase: VoicePhase { get }
    var transcript: String { get }
    var response: String { get }
    var level: Float { get }
    var issue: String? { get }
    var muted: Bool { get }
    /// When the call connected; nil while dialing.
    var connected: Date? { get }
    /// Tasks the agent is still running for this call.
    var working: Int { get }
    func start() async
    func stop() async
    func retry() async
    var silenced: Bool { get }
    func toggleMute()
    func toggleSilence()
}

@MainActor
public protocol VoiceReply: AnyObject {
    func stream(_ text: String) -> AsyncThrowingStream<VoiceFragment, Error>
    func stop() async
}

extension Error {
    /// Words for a failed voice session: speech failures explain themselves, anything else never shows system text.
    public var voiceIssue: String {
        if self is SpeechFailure || self is LiveFailure { return localizedDescription }
        if isOffline { return "Voice stopped because Polymux is offline." }
        return reason(or: "Voice stopped unexpectedly.")
    }
}
