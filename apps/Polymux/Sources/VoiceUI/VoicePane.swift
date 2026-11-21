import SwiftUI
import PolymuxKit
import SpeechKit

/// The Voice tab: live voice plus the dictation settings kept on this device.
struct VoicePane: View {
    let store: SettingsStore
    let providers: [Provider]
    @State private var dictation = DictationSettings.shared
    @State private var models = ModelStore.shared

    var body: some View {
        VoiceSettings(store: store, providers: providers)
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
                    Label("Add word", systemImage: "plus")
                }
                .buttonStyle(SectionButton())
                .accessibilityLabel("Add dictionary word")
            }
        } header: {
            SectionHeader("Dictionary", symbol: "character.book.closed.fill")
        } footer: {
            Text("Add a word to spell correctly, then the phrases it’s often heard as, separated by commas.")
                .font(.footnote).foregroundStyle(.muted)
        }
        .panelRow()
    }

    private var recognitionSection: some View {
        Section {
            LabeledContent("Engine", value: "Apple Speech")
            LabeledContent("Listening for", value: DictationDevice.languageNames(for: dictation.deviceLanguage))
            settingToggle("Review dictation", "Adds misheard words to your dictionary automatically.",
                          isOn: $dictation.autoReview, enabled: DictationPolish.isAvailable)
            // The review model sharpens which dictation gets reviewed.
            if dictation.autoReview, DictationPolish.isAvailable, DecisionModels.default.downloadURL != nil { modelRow }
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
                    .buttonStyle(.bordered).buttonBorderShape(.roundedRectangle).controlSize(.small)
            case .ready:
                Text(model.displayName).foregroundStyle(.muted)
                Button("Remove") { models.remove(model.id) }
                    .buttonStyle(.bordered).buttonBorderShape(.roundedRectangle).controlSize(.small)
            case .notDownloaded, .failed:
                Text(ByteCountFormatter.string(fromByteCount: model.byteSize, countStyle: .file))
                    .foregroundStyle(.muted)
                Button("Download") { models.download(model) }
                    .buttonStyle(.bordered).buttonBorderShape(.roundedRectangle).controlSize(.small)
            }
        }
        .accessibilityElement(children: .contain)
    }

    private func row(_ entry: VocabularyEntry) -> some View {
        DictionaryRow(entry: entry, dictation: dictation)
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

/// One dictionary entry: the word, and what the recogniser tends to hear instead.
private struct DictionaryRow: View {
    let entry: VocabularyEntry
    let dictation: DictationSettings
    // Kept apart from the entry so a comma typed mid-phrase isn't stripped.
    @State private var heard = ""

    var body: some View {
        HStack(spacing: 8) {
            field("Dictionary word", prompt: "Word (e.g. cappuccino)", text: Binding(
                get: { dictation.vocabulary.first { $0.id == entry.id }?.term ?? "" },
                set: { value in
                    guard let index = dictation.vocabulary.firstIndex(where: { $0.id == entry.id }) else { return }
                    dictation.vocabulary[index].term = value
                }))
            field("Sounds like", prompt: "Sounds like (e.g. capuchin, cap a chino)", text: $heard)
            Button("Remove", systemImage: "xmark", role: .destructive) { dictation.removeEntry(entry.id) }
                .labelStyle(.iconOnly)
                .buttonStyle(.plain)
                .foregroundStyle(.muted)
                .accessibilityLabel(entry.term.isEmpty ? "Remove word" : "Remove \(entry.term)")
        }
        .onAppear { heard = entry.soundsLike.joined(separator: ", ") }
        .onChange(of: heard) {
            guard let index = dictation.vocabulary.firstIndex(where: { $0.id == entry.id }) else { return }
            dictation.vocabulary[index].soundsLike = heard.split(separator: ",").map {
                $0.trimmingCharacters(in: .whitespaces)
            }.filter { !$0.isEmpty }
        }
    }

    private func field(_ label: String, prompt: String, text: Binding<String>) -> some View {
        TextField(label, text: text, prompt: Text(prompt).foregroundStyle(.muted))
            .textFieldStyle(.plain)
            .autocorrectionDisabled()
            .padding(.horizontal, 10).padding(.vertical, 7)
            .background(.quaternary.opacity(0.5), in: .rect(cornerRadius: 8))
            .overlay { RoundedRectangle(cornerRadius: 8).strokeBorder(.quaternary) }
            .accessibilityLabel(label)
            .accessibilityHint(prompt)
    }
}
