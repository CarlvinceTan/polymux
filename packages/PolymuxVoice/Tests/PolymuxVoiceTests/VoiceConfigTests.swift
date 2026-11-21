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

@Test @MainActor func liveCallNeedsConfiguredPrimary() {
    var settings = Settings()
    #expect(!VoiceConfig(settings: settings).canStart)
    settings.liveProvider = "gemini"
    settings.liveModel = "gemini-3.8-live"
    settings.liveVoice = "Aoede"
    settings.speechKeys = ["openai"]
    #expect(!VoiceConfig(settings: settings).canStart)
    settings.speechKeys = ["gemini"]
    #expect(VoiceConfig(settings: settings).canStart)
    settings.liveModel = " "
    #expect(!VoiceConfig(settings: settings).canStart)
    settings.liveModel = "custom-live"
    #expect(VoiceConfig(settings: settings).canStart)
    settings.liveVoice = ""
    #expect(!VoiceConfig(settings: settings).canStart)
    settings.liveVoice = "Aoede"
    settings.liveProvider = "unknown"
    settings.speechKeys = ["unknown"]
    #expect(!VoiceConfig(settings: settings).canStart)
}
