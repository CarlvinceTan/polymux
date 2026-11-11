import SwiftUI
import PolymuxKit
import SpeechKit

/// The Voice tab: live voice plus the dictation settings kept on this device.
struct VoicePane: View {
    let store: SettingsStore
    let providers: [Provider]
    let editFallbacks: () -> Void
    @State private var dictation = DictationSettings.shared

    var body: some View {
        VoiceSettings(store: store, providers: providers, editFallbacks: editFallbacks)
        dictationSection
        dictionarySection
        recognitionSection
    }

    private var dictationSection: some View {
        Section {
            SettingsPicker("Language", selection: Binding(get: { dictation.language.rawValue },
                                                          set: { dictation.language = DictationLanguage(rawValue: $0) ?? .multilingual }),
                           options: DictationLanguage.allCases.map { ($0 == .english ? "English only" : $0.title, $0.rawValue) })
                .accessibilityLabel("Dictation language")
            Toggle("Filler words", isOn: $dictation.fillerWords)
                .accessibilityLabel("Filler words")
            Toggle("Smart spacing", isOn: $dictation.smartSpacing)
                .accessibilityLabel("Smart spacing")
            Toggle("Spoken symbols", isOn: $dictation.spokenSymbols)
                .accessibilityLabel("Spoken symbols")
            Toggle("Spoken formatting", isOn: $dictation.spokenFormatting)
                .accessibilityLabel("Spoken formatting")
            Toggle("Output fixes", isOn: $dictation.outputFixes)
                .accessibilityLabel("Output fixes")
            Toggle("On-device polish", isOn: $dictation.polish)
                .accessibilityLabel("On-device polish")
                .disabled(!DictationPolish.isAvailable)
            if !DictationPolish.isAvailable {
                Text("Polish isn’t available on this device.")
                    .font(.footnote).foregroundStyle(.muted)
            }
        } header: {
            SectionHeader("Dictation", symbol: "mic.fill")
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
        }
        .panelRow()
    }

    private var recognitionSection: some View {
        Section {
            LabeledContent("Engine", value: "Apple Speech")
            LabeledContent("Listening for", value: DictationDevice.languageName(for: dictation.deviceLanguage))
            if dictation.fallsBack {
                Text("Multilingual falls back to English on this device.")
                    .font(.footnote).foregroundStyle(.muted)
            } else if dictation.language == .multilingual {
                Text("Multilingual follows this device’s language.")
                    .font(.footnote).foregroundStyle(.muted)
            }
        } header: {
            SectionHeader("Recognition", symbol: "cpu.fill")
        }
        .panelRow()
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
}
