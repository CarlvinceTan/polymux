import Foundation
import PolymuxKit

enum VoicePhase: String {
    case preparing = "Preparing voice"
    case listening = "Listening"
    case thinking = "Thinking"
    case speaking = "Speaking"
    case interrupted = "Interrupted"
    case failed = "Voice unavailable"
}

struct VoiceTranscript: Sendable {
    let text: String
    let start: Double
    let end: Double
    let final: Bool
}

enum VoiceEvent: Sendable {
    case transcript(VoiceTranscript)
    case level(Float)
    case activity(Bool)
}

@MainActor
protocol VoiceSession: AnyObject {
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
protocol SpeechInput: AnyObject {
    func start() async throws -> AsyncThrowingStream<VoiceEvent, Error>
    func stop() async
    func setMuted(_ muted: Bool)
    func finalize() async throws
}

extension SpeechInput {
    func finalize() async throws {}
}

@MainActor
protocol VoiceReply: AnyObject {
    func stream(_ text: String) -> AsyncThrowingStream<VoiceFragment, Error>
    func stop() async
}

enum VoiceFailure: LocalizedError {
    case microphone
    case recognition
    case unavailable
    case locale
    case audio
    case speech
    case echo

    var errorDescription: String? {
        switch self {
        case .microphone:
            "Allow Polymux to use the microphone in \(Self.settings)."
        case .recognition:
            "Allow Polymux to use speech recognition in \(Self.settings)."
        case .unavailable:
            "Speech recognition isn’t available on this device right now."
        case .locale:
            "Speech recognition does not support the current language."
        case .audio:
            "No working microphone or speaker was found. Check the selected audio devices."
        case .speech:
            "Speech recognition stopped. Try again."
        case .echo:
            "This audio route cannot cancel echo. Change the microphone or speaker."
        }
    }

    /// Where each platform grants permissions.
    private static var settings: String {
        #if os(macOS)
        "System Settings"
        #else
        "Settings"
        #endif
    }
}

extension Error {
    /// Words for a failed voice session: voice failures explain themselves, anything else never shows system text.
    var voiceIssue: String {
        if self is VoiceFailure || self is LiveFailure { return localizedDescription }
        if isOffline { return "Voice stopped because Polymux is offline." }
        return reason(or: "Voice stopped unexpectedly.")
    }
}
