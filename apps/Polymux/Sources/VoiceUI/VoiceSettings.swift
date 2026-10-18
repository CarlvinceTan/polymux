import SwiftUI
import PolymuxKit

struct VoiceSettings: View {
    let store: SettingsStore

    var body: some View {
        if let settings = store.value {
            Section("Voice") {
                Picker("Mode", selection: binding(\.voiceMode) { SettingsPatch(voiceMode: $0) }) {
                    Text("Cascaded").tag("pipeline")
                    Text("Full Duplex").tag("live")
                }
                if settings.voiceMode == "live" {
                    Picker("Provider", selection: binding(\.liveProvider) { SettingsPatch(liveProvider: $0) }) {
                        Text("Gemini Live").tag("gemini")
                    }
                    ModelSetting(store: store, live: true).id("live")
                    Picker("Voice", selection: binding(\.liveVoice) { SettingsPatch(liveVoice: $0) }) {
                        ForEach(["Aoede", "Kore", "Puck", "Charon", "Fenrir", "Leda", "Orus", "Zephyr"], id: \.self) {
                            Text($0).tag($0)
                        }
                    }
                } else {
                    Picker("Provider", selection: binding(\.textProvider) { SettingsPatch(textProvider: $0) }) {
                        Text("Gemini").tag("gemini")
                    }
                    ModelSetting(store: store, live: false).id("pipeline")
                }
                VoiceKeyView(store: store, provider: settings.voiceMode == "live" ? settings.liveProvider : settings.textProvider)
            }
            .disabled(!store.isConnected)
            .panelRow()
        }
    }

    private func binding(_ path: KeyPath<PolymuxKit.Settings, String>, patch: @escaping (String) -> SettingsPatch) -> Binding<String> {
        Binding(get: { store.value?[keyPath: path] ?? "" }, set: { store.edit(patch($0)) })
    }
}

private struct ModelSetting: View {
    let store: SettingsStore
    let live: Bool
    @State private var draft = ""
    @FocusState private var focused: Bool

    private var model: String { (live ? store.value?.liveModel : store.value?.textModel) ?? "" }

    var body: some View {
        TextField("Model", text: $draft)
            .autocorrectionDisabled()
            .focused($focused)
            .onSubmit { save() }
            .onAppear { draft = model }
            .onChange(of: model) { _, value in if !focused { draft = value } }
            .onChange(of: focused) { _, value in if !value { save() } }
            .onDisappear { save() }
            #if os(iOS)
            .textInputAutocapitalization(.never)
            #endif
    }

    private func save() {
        let value = draft.trimmingCharacters(in: .whitespacesAndNewlines)
        guard value != model else { return }
        store.edit(live ? SettingsPatch(liveModel: value) : SettingsPatch(textModel: value))
    }
}

private struct VoiceKeyView: View {
    let store: SettingsStore
    let provider: String
    @State private var key = ""
    @State private var saving = false
    @State private var issue: String?

    private var saved: Bool { store.value?.speechKeys.contains(provider) == true }

    var body: some View {
        HStack(spacing: 12) {
            Text("Set API Key").fixedSize()
            SecureField("", text: $key)
                .labelsHidden()
                .accessibilityLabel("API key")
                .textFieldStyle(.plain)
                .lineLimit(1)
                .frame(minWidth: 0, maxWidth: .infinity)
                .clipped()
                .autocorrectionDisabled()
                .onSubmit { save() }
                #if os(iOS)
                .textInputAutocapitalization(.never)
                #endif
            Button("Save", action: save).disabled(key.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                .fixedSize()
            if saved { Button("Remove", role: .destructive) { update(nil) }.fixedSize() }
        }
        .buttonStyle(.bordered)
        .disabled(saving)
        if let issue { Text(issue).font(.footnote).foregroundStyle(.red) }
    }

    private func save() {
        let value = key.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !value.isEmpty else { return }
        update(value)
    }

    private func update(_ value: String?) {
        guard !saving else { return }
        saving = true
        Task {
            defer { saving = false }
            do { try await store.setKey(value, provider: provider); key = ""; issue = nil }
            catch { issue = error.localizedDescription }
        }
    }
}
