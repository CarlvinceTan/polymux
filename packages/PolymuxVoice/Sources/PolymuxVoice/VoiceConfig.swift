import Foundation
import PolymuxKit

@MainActor
public struct VoiceConfig: Equatable {
    let provider: String
    let model: String
    let voice: String
    let prompt: String
    let hasKey: Bool
    /// The primary, then the fallbacks that have a key, in the order tried.
    let steps: [LiveStep]

    public init(settings: Settings) {
        provider = settings.liveProvider
        model = settings.liveModel
        voice = settings.liveVoice
        prompt = settings.voicePrompt + "\nAlways speak \(Self.language(settings.voiceLanguage))."
        hasKey = settings.speechKeys.contains(provider)
        let primary = LiveStep(provider: provider, model: model, voice: voice)
        let rest: [LiveStep] = switch settings.liveFallback.mode {
        case LiveFallbackMode.custom: settings.liveFallback.models
        case LiveFallbackMode.auto: Self.defaults.filter { $0.provider != settings.liveProvider }
        default: []
        }
        steps = [primary] + rest.filter { $0 != primary && settings.speechKeys.contains($0.provider) }
    }

    /// Each live provider's starting model and voice, as the server defines them.
    public static let defaults = [LiveStep(provider: "gemini", model: "gemini-3.8-live", voice: "Aoede"),
                           LiveStep(provider: "openai", model: "gpt-realtime", voice: "marin")]

    // An empty code follows the system language.
    public static func language(_ code: String) -> String {
        let code = code.isEmpty ? (Locale.current.language.languageCode?.identifier ?? "en") : code
        return Locale(identifier: "en").localizedString(forIdentifier: code) ?? "English"
    }

    public func session(client: APIClient, reply: any VoiceReply, agent: String) -> any VoiceSession {
        #if DEBUG
        if CallFixture.enabled { return CallFixture() }
        #endif
        let instructions = prompt.replacingOccurrences(of: "{target}", with: agent)
        let attempts = steps.enumerated().map { index, step in
            // Only the primary can lack a key; the rest were filtered.
            let hasKey = index > 0 || hasKey
            return LiveAttempt(options: LiveOptions(model: step.model, voice: step.voice, instructions: instructions), makeProvider: {
                switch step.provider {
                case "gemini": return GeminiLive(client: client)
                case "openai": return OpenAIRealtime(client: client)
                default: throw LiveFailure.configuration
                }
            }, key: {
                guard hasKey else { throw LiveFailure.missingKey }
                return ""
            })
        }
        return LiveStore(attempts: attempts, reply: reply)
    }
}
