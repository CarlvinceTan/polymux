import SwiftUI
import PolymuxKit
import PolymuxVoice

struct VoiceSettings: View {
    let store: SettingsStore
    let providers: [Provider]
    let editFallbacks: () -> Void

    var body: some View {
        if let settings = store.value {
            Section("Voice") {
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
                Button(action: editFallbacks) {
                    HStack {
                        Text("Fallbacks").lineLimit(1).layoutPriority(1)
                        Spacer()
                        Text(Self.modes.first { $0.1 == settings.liveFallback.mode }?.0 ?? "").lineLimit(1)
                        Image(systemName: "chevron.right")
                    }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Voice fallbacks")
                SettingsPicker("Language", selection: binding(\.voiceLanguage) { SettingsPatch(voiceLanguage: $0) },
                               options: [("System", "")] + Self.languages.map { (VoiceConfig.language($0), $0) })
                if !settings.speechKeys.contains(settings.liveProvider) {
                    Text("No \(settings.liveProvider == "openai" ? "OpenAI" : "Gemini") provider yet.")
                        .font(.footnote).foregroundStyle(.muted)
                }
            }
            .panelRow()
        }
    }

    /// Matches the voices the server accepts for each live provider.
    static let modes = [("Off", LiveFallbackMode.off), ("Custom", LiveFallbackMode.custom), ("Auto", LiveFallbackMode.auto)]

    static let names = ["gemini": "Gemini Live", "openai": "OpenAI Realtime"]

    static let voices = ["gemini": ["Aoede", "Kore", "Puck", "Charon", "Fenrir", "Leda", "Orus", "Zephyr"],
                                 "openai": ["marin", "cedar", "alloy", "ash", "ballad", "coral", "echo", "sage", "shimmer", "verse"]]

    private static let languages = ["en", "es", "fr", "de", "it", "pt", "nl", "pl", "ru", "tr", "ar", "hi", "id", "ja", "ko", "th", "vi", "zh"]

    private func models(_ settings: PolymuxKit.Settings) -> [String] {
        Self.models(settings.liveProvider, providers: providers, including: settings.liveModel)
    }

    static func models(_ provider: String, providers: [Provider], including current: String = "") -> [String] {
        let defaults = provider == "openai" ? ["gpt-realtime", "gpt-realtime-mini"] : ["gemini-3.8-live"]
        let available = providers.filter { $0.enabled && $0.type == provider }
            .flatMap(\.models).filter { $0.enabled && $0.tags.contains("live") }.map(\.id)
        return Array(Set(defaults + available + [current])).filter { !$0.isEmpty }.sorted()
    }

    private func binding(_ path: KeyPath<PolymuxKit.Settings, String>, patch: @escaping (String) -> SettingsPatch) -> Binding<String> {
        Binding(get: { store.value?[keyPath: path] ?? "" }, set: { store.edit(patch($0)) })
    }
}
