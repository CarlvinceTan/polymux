import Foundation

/// One recognition result on the dictated timeline, in seconds.
public struct VoiceTranscript: Sendable {
    public let text: String
    public let start: Double
    public let end: Double
    public let final: Bool
    public init(text: String, start: Double, end: Double, final: Bool) {
        self.text = text
        self.start = start
        self.end = end
        self.final = final
    }
}

public enum VoiceEvent: Sendable {
    case transcript(VoiceTranscript)
    case level(Float)
    case activity(Bool)
}

@MainActor
public protocol SpeechInput: AnyObject {
    func start() async throws -> AsyncThrowingStream<VoiceEvent, Error>
    func stop() async
    func setMuted(_ muted: Bool)
    func finalize() async throws
    /// Recognition terms (names, product words) the engine should hear correctly.
    func setVocabulary(_ terms: [String]) async
}

extension SpeechInput {
    public func finalize() async throws {}
    public func setVocabulary(_ terms: [String]) async {}
}

/// Why speech capture or recognition stopped, phrased for the user.
public enum SpeechFailure: LocalizedError {
    case microphone
    case recognition
    case unavailable
    case locale
    case audio
    case speech
    case echo

    public var errorDescription: String? {
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
