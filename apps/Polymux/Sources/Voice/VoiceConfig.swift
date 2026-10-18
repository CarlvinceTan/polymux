import Foundation
import PolymuxKit

enum VoiceMode: String, CaseIterable {
    case pipeline
    case live
}

@MainActor
struct VoiceConfig: Equatable {
    let mode: VoiceMode
    let provider: String
    let model: String
    let voice: String
    let prompt: String
    let hasKey: Bool

    init(settings: Settings) {
        mode = VoiceMode(rawValue: settings.voiceMode) ?? .pipeline
        provider = mode == .live ? settings.liveProvider : settings.textProvider
        model = mode == .live ? settings.liveModel : settings.textModel
        voice = settings.liveVoice
        prompt = settings.voicePrompt
        hasKey = settings.speechKeys.contains(provider)
    }

    func session(client: APIClient, reply: any VoiceReply, agent: String) -> any VoiceSession {
        let providerID = provider
        let hasKey = hasKey
        let key: () throws -> String = {
            guard hasKey else { throw LiveFailure.missingKey }
            return ""
        }
        let options = LiveOptions(model: model, voice: voice, instructions: prompt.replacingOccurrences(of: "{target}", with: agent))
        if mode == .pipeline {
            return VoiceStore(reply: Spokesperson(options: options, delegate: reply, makeProvider: {
                guard providerID == "gemini" else { throw LiveFailure.configuration }
                return GeminiText(client: client)
            }, key: key))
        }
        return LiveStore(options: options, reply: reply, makeProvider: {
            guard providerID == "gemini" else { throw LiveFailure.configuration }
            return GeminiLive(client: client)
        }, key: key)
    }
}
