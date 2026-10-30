import SwiftUI
import PolymuxKit

struct VoiceSettings: View {
    let store: SettingsStore
    let providers: [Provider]

    var body: some View {
        if let settings = store.value {
            Section("Voice") {
                SettingsPicker("Mode", selection: binding(\.voiceMode) { SettingsPatch(voiceMode: $0) },
                               options: [("Take turns", "pipeline"), ("Live conversation", "live")])
                .accessibilityLabel("Voice mode")
                Text(settings.voiceMode == "live" ? "Talk naturally and interrupt replies." : "Speak, then wait for a reply.")
                    .font(.footnote).foregroundStyle(.muted)
                if settings.voiceMode == "live" {
                    // Each provider starts from its own model and voice.
                    SettingsPicker("Provider", selection: binding(\.liveProvider) { provider in
                        SettingsPatch(liveProvider: provider, liveModel: provider == "openai" ? "gpt-realtime" : "gemini-3.8-live",
                                      liveVoice: Self.voices[provider]?.first)
                    }, options: [("Gemini Live", "gemini"), ("OpenAI Realtime", "openai")])
                    .accessibilityLabel("Voice provider")
                    SettingsPicker("Model", selection: binding(\.liveModel) { SettingsPatch(liveModel: $0) },
                                   options: models(settings).map { ($0, $0) })
                    .accessibilityLabel("Voice model")
                    SettingsPicker("Voice", selection: binding(\.liveVoice) { SettingsPatch(liveVoice: $0) },
                                   options: (Self.voices[settings.liveProvider] ?? []).map { ($0.capitalized, $0) })
                }
                SettingsPicker("Language", selection: binding(\.voiceLanguage) { SettingsPatch(voiceLanguage: $0) },
                               options: [("System", "")] + Self.languages.map { (VoiceConfig.language($0), $0) })
                if settings.voiceMode == "live", !settings.speechKeys.contains(settings.liveProvider) {
                    Text("No \(settings.liveProvider == "openai" ? "OpenAI" : "Gemini") provider yet.")
                        .font(.footnote).foregroundStyle(.muted)
                }
            }
            .panelRow()
        }
    }

    /// Matches the voices the server accepts for each live provider.
    private static let voices = ["gemini": ["Aoede", "Kore", "Puck", "Charon", "Fenrir", "Leda", "Orus", "Zephyr"],
                                 "openai": ["marin", "cedar", "alloy", "ash", "ballad", "coral", "echo", "sage", "shimmer", "verse"]]

    private static let languages = ["en", "es", "fr", "de", "it", "pt", "nl", "pl", "ru", "tr", "ar", "hi", "id", "ja", "ko", "th", "vi", "zh"]

    private func models(_ settings: PolymuxKit.Settings) -> [String] {
        let defaults = settings.liveProvider == "openai" ? ["gpt-realtime", "gpt-realtime-mini"] : ["gemini-3.8-live"]
        let available = providers.filter { $0.enabled && $0.type == settings.liveProvider }
            .flatMap(\.models).filter { $0.enabled && $0.tags.contains("live") }.map(\.id)
        return Array(Set(defaults + available + [settings.liveModel])).filter { !$0.isEmpty }.sorted()
    }

    private func binding(_ path: KeyPath<PolymuxKit.Settings, String>, patch: @escaping (String) -> SettingsPatch) -> Binding<String> {
        Binding(get: { store.value?[keyPath: path] ?? "" }, set: { store.edit(patch($0)) })
    }
}
