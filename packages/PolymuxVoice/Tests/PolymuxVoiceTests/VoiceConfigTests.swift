import Testing
import PolymuxKit
@testable import PolymuxVoice

@Test @MainActor func liveConfigKeepsItsDedicatedSettingsAndLanguage() {
    let config = VoiceConfig(settings: Settings(liveProvider: "gemini",
                                               liveModel: "live-model", liveVoice: "Aoede",
                                               voicePrompt: "Speak for {target}.", voiceLanguage: "es",
                                               speechKeys: ["gemini"], defaultProvider: "unrelated",
                                               defaultModel: "agent-model"))
    #expect(config.provider == "gemini")
    #expect(config.model == "live-model")
    #expect(config.voice == "Aoede")
    #expect(config.hasKey)
    #expect(config.prompt.contains("Speak for {target}."))
    #expect(config.prompt.contains("Spanish"))
}
