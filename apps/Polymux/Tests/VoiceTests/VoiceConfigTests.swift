import Testing
import PolymuxKit
@testable import PolymuxVoice

@Test @MainActor func cascadedConfigDoesNotDependOnLiveCredentialsOrModels() {
    let first = VoiceConfig(settings: Settings(voiceMode: "pipeline"))
    let second = VoiceConfig(settings: Settings(voiceMode: "pipeline", liveProvider: "gemini",
                                               liveModel: "another-model", liveVoice: "Aoede",
                                               voicePrompt: "Live instructions", voiceLanguage: "es",
                                               speechKeys: ["gemini"], defaultProvider: "another-provider",
                                               defaultModel: "agent-model"))
    #expect(first == second)
    #expect(!second.hasKey)
}

@Test @MainActor func liveConfigKeepsItsDedicatedSettingsAndLanguage() {
    let config = VoiceConfig(settings: Settings(voiceMode: "live", liveProvider: "gemini",
                                               liveModel: "live-model", liveVoice: "Aoede",
                                               voicePrompt: "Speak for {target}.", voiceLanguage: "es",
                                               speechKeys: ["gemini"], defaultProvider: "unrelated",
                                               defaultModel: "agent-model"))
    #expect(config.mode == .live)
    #expect(config.provider == "gemini")
    #expect(config.model == "live-model")
    #expect(config.voice == "Aoede")
    #expect(config.hasKey)
    #expect(config.prompt.contains("Speak for {target}."))
    #expect(config.prompt.contains("Spanish"))
}
