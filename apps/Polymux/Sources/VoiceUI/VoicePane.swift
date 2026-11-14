import SwiftUI
import PolymuxKit
import SpeechKit

/// The Voice tab: live voice plus the dictation settings kept on this device.
struct VoicePane: View {
    let store: SettingsStore
    let providers: [Provider]
    let editFallbacks: () -> Void
    @State private var dictation = DictationSettings.shared
    @State private var models = ModelStore.shared

    var body: some View {
        VoiceSettings(store: store, providers: providers, editFallbacks: editFallbacks)
        dictationSection
        if !dictation.suggestions.isEmpty { suggestionSection }
        dictionarySection
        recognitionSection
    }

    private var dictationSection: some View {
        Section {
            SettingsPicker("Language", selection: Binding(get: { dictation.language.rawValue },
                                                          set: { dictation.language = DictationLanguage(rawValue: $0) ?? .multilingual }),
                           options: DictationLanguage.allCases.map { ($0 == .english ? "English only" : $0.title, $0.rawValue) })
                .accessibilityLabel("Dictation language")
            settingToggle("Filler words", "Removes “um”, “uh” and similar fillers.",
                          isOn: $dictation.fillerWords)
            settingToggle("Smart spacing", "Adds missing spaces around dictation.",
                          isOn: $dictation.smartSpacing)
            settingToggle("Spoken symbols", "Turns “at sign” and “hashtag” into symbols.",
                          isOn: $dictation.spokenSymbols)
            settingToggle("Spoken formatting", "Adds punctuation and line breaks by voice.",
                          isOn: $dictation.spokenFormatting)
            settingToggle("Output fixes", "Fixes capitals, spacing and punctuation.",
                          isOn: $dictation.outputFixes)
            settingToggle("On-device polish", "Fixes text with Apple Intelligence.",
                          isOn: $dictation.polish, enabled: DictationPolish.isAvailable)
            if !DictationPolish.isAvailable {
                Text("Polish isn’t available on this device.")
                    .font(.footnote).foregroundStyle(.muted)
            }
        } header: {
            SectionHeader("Dictation", symbol: "mic.fill", tint: .pink)
        }
        .panelRow()
    }

    private var suggestionSection: some View {
        Section {
            ForEach(dictation.suggestions) { item in
                HStack(spacing: PanelStyle.rowInset) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(item.term)
                        Text(item.heard).font(.caption).foregroundStyle(.muted)
                    }
                    Spacer(minLength: 0)
                    Button("Dismiss") { dictation.dismiss(item) }
                        .buttonStyle(.plain)
                        .foregroundStyle(.muted)
                    Button("Add") { dictation.accept(item) }
                        .buttonStyle(.plain)
                        .foregroundStyle(.tint)
                }
                .accessibilityElement(children: .contain)
            }
        } header: {
            SectionHeader("Suggested", symbol: "sparkles", tint: .orange)
        }
        .panelRow()
    }

    private var dictionarySection: some View {
        Section {
            ForEach(dictation.vocabulary) { entry in
                row(entry)
            }
            if dictation.canAddEntry {
                Button { dictation.addEntry() } label: {
                    Label("Add", systemImage: "plus")
                }
                .buttonStyle(SectionButton())
                .accessibilityLabel("Add dictionary word")
            }
        } header: {
            SectionHeader("Dictionary", symbol: "character.book.closed.fill")
        } footer: {
            Text("Words the recogniser should hear correctly, with the phrases it often mishears.")
                .font(.footnote).foregroundStyle(.muted)
        }
        .panelRow()
    }

    private var recognitionSection: some View {
        Section {
            LabeledContent("Engine", value: "Apple Speech")
            LabeledContent("Listening for", value: DictationDevice.languageNames(for: dictation.deviceLanguage))
            // The review model only feeds on-device polish, and is untested on iPhone.
            #if os(macOS)
            if DictationPolish.isAvailable, DecisionModels.default.downloadURL != nil { modelRow }
            #endif
        } header: {
            SectionHeader("Recognition", symbol: "cpu.fill", tint: .purple)
        } footer: {
            if dictation.fallsBack {
                Text("Multilingual falls back to English on this device.")
                    .font(.footnote).foregroundStyle(.muted)
            } else if dictation.language == .multilingual {
                Text("Multilingual listens for English and this device’s languages.")
                    .font(.footnote).foregroundStyle(.muted)
            }
        }
        .panelRow()
    }

    private var modelRow: some View {
        let model = DecisionModels.default
        let state = models.state(model.id)
        return HStack(spacing: PanelStyle.rowInset) {
            VStack(alignment: .leading, spacing: 2) {
                Text("Review model")
                Group {
                    if case .failed(let reason) = state { Text(reason) } else { Text(model.licenseNotice) }
                }
                .font(.footnote).foregroundStyle(.muted)
            }
            Spacer(minLength: 0)
            switch state {
            case .downloading(let fraction):
                ProgressView(value: fraction).frame(width: 80)
                    .accessibilityLabel("Review model download")
                Button("Cancel") { models.remove(model.id) }
                    .buttonStyle(.plain).foregroundStyle(.muted)
            case .ready:
                Text(model.displayName).foregroundStyle(.muted)
                Button("Remove") { models.remove(model.id) }
                    .buttonStyle(.plain).foregroundStyle(.muted)
            case .notDownloaded, .failed:
                Text(ByteCountFormatter.string(fromByteCount: model.byteSize, countStyle: .file))
                    .foregroundStyle(.muted)
                Button("Download") { models.download(model) }
                    .buttonStyle(.plain).foregroundStyle(.tint)
            }
        }
        .accessibilityElement(children: .contain)
    }

    private func row(_ entry: VocabularyEntry) -> some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: PanelStyle.rowInset) {
                TextField("Word", text: term(entry))
                    .textFieldStyle(.plain)
                    .accessibilityLabel("Dictionary word")
                    .accessibilityValue(entry.term)
                Button("Remove", systemImage: "minus.circle", role: .destructive) {
                    dictation.removeEntry(entry.id)
                }
                .labelStyle(.iconOnly)
                .buttonStyle(.plain)
                .foregroundStyle(.muted)
                .accessibilityLabel(entry.term.isEmpty ? "Remove word" : "Remove \(entry.term)")
            }
            TextField("Sounds like", text: soundsLike(entry))
                .textFieldStyle(.plain)
                .font(.caption)
                .foregroundStyle(.muted)
                .accessibilityLabel(entry.term.isEmpty ? "Sounds like" : "Sounds like for \(entry.term)")
        }
    }

    private func term(_ entry: VocabularyEntry) -> Binding<String> {
        Binding(get: {
            dictation.vocabulary.first { $0.id == entry.id }?.term ?? ""
        }, set: { value in
            guard let index = dictation.vocabulary.firstIndex(where: { $0.id == entry.id }) else { return }
            dictation.vocabulary[index].term = value
        })
    }

    private func soundsLike(_ entry: VocabularyEntry) -> Binding<String> {
        Binding(get: {
            dictation.vocabulary.first { $0.id == entry.id }?.soundsLike.joined(separator: ", ") ?? ""
        }, set: { value in
            guard let index = dictation.vocabulary.firstIndex(where: { $0.id == entry.id }) else { return }
            dictation.vocabulary[index].soundsLike = value.split(separator: ",").map {
                $0.trimmingCharacters(in: .whitespaces)
            }.filter { !$0.isEmpty }
        })
    }

    private func settingToggle(_ title: String, _ detail: String, isOn: Binding<Bool>,
                               enabled: Bool = true) -> some View {
        Grid(alignment: .leading, horizontalSpacing: 12) {
            GridRow(alignment: .center) {
                VStack(alignment: .leading, spacing: 2) {
                    Text(title)
                    Text(detail)
                        .font(.footnote)
                        .foregroundStyle(.muted)
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .accessibilityHidden(true)
                Toggle("", isOn: isOn)
                    .labelsHidden()
                    .disabled(!enabled)
                    .accessibilityLabel(title)
                    .accessibilityHint(detail)
            }
        }
    }
}
