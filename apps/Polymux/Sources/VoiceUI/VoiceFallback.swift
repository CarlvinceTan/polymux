import SwiftUI
import PolymuxKit

/// Which live providers voice moves to when its own can't connect.
struct VoiceFallback: View {
    let store: SettingsStore
    let providers: [Provider]
    let onBack: () -> Void
    @State private var adding = false
    private let limit = 8

    private var fallback: LiveFallback { store.value?.liveFallback ?? LiveFallback(mode: LiveFallbackMode.auto) }

    private var mode: Binding<String> {
        Binding(get: { fallback.mode }, set: { mode in
            var next = fallback
            next.mode = mode
            save(next)
        })
    }

    /// Every provider and model voice can run on, as "provider/model".
    private var choices: [(String, String)] {
        VoiceSettings.names.sorted { $0.key < $1.key }.flatMap { provider, name in
            VoiceSettings.models(provider, providers: providers).map { ("\(name) · \($0)", "\(provider)/\($0)") }
        }
    }

    var body: some View {
        PanelView("Voice fallbacks", onClose: onBack, onBack: onBack, width: 460) {
            ScrollView {
                VStack(alignment: .leading, spacing: PanelStyle.sectionGap) {
                    PanelSection {
                        SettingsPicker("Mode", selection: mode, options: VoiceSettings.modes).sectionRow()
                    }
                    if fallback.mode == LiveFallbackMode.custom { order }
                    if let issue = store.error {
                        Text(issue).font(.footnote).foregroundStyle(.muted).padding(.horizontal, PanelStyle.headingInset)
                    }
                }
                .padding(.horizontal, PanelStyle.contentInset)
            }
        }
    }

    private var order: some View {
        PanelSection("Order") {
            if fallback.models.isEmpty {
                Text("No fallbacks.").foregroundStyle(.muted).frame(maxWidth: .infinity).sectionRow()
            }
            ForEach(Array(fallback.models.enumerated()), id: \.offset) { index, step in
                VoiceStepRow(step: step, index: index, count: fallback.models.count, choices: choices) { next in
                    edit { $0[index] = next }
                } move: { offset in
                    edit { $0.swapAt(index, index + offset) }
                } remove: {
                    edit { $0.remove(at: index) }
                }
                if index < fallback.models.count - 1 || fallback.models.count < limit {
                    Divider().padding(.horizontal, PanelStyle.rowInset)
                }
            }
            if fallback.models.count < limit {
                Button { adding = true } label: { Label("Add", systemImage: "plus") }
                    .buttonStyle(SectionButton())
                    .accessibilityLabel("Add voice fallback")
                    .dropdown(isPresented: $adding, alignment: .leading) {
                        DropdownOptions(selection: Binding(get: { "" }, set: { choice in
                            adding = false
                            if let step = VoiceStepRow.step(choice, voice: nil) { edit { $0.append(step) } }
                        }), options: choices)
                    }
            }
        }
    }

    private func edit(_ change: (inout [LiveStep]) -> Void) {
        var next = fallback
        change(&next.models)
        save(next)
    }

    private func save(_ next: LiveFallback) { store.edit(SettingsPatch(liveFallback: next)) }
}

private struct VoiceStepRow: View {
    let step: LiveStep
    let index: Int
    let count: Int
    let choices: [(String, String)]
    let change: (LiveStep) -> Void
    let move: (Int) -> Void
    let remove: () -> Void
    @State private var choosing = false

    /// A new provider starts from its first voice; the same provider keeps its voice.
    static func step(_ choice: String, voice: LiveStep?) -> LiveStep? {
        let parts = choice.split(separator: "/", maxSplits: 1).map(String.init)
        guard parts.count == 2 else { return nil }
        let keep = voice.flatMap { $0.provider == parts[0] ? $0.voice : nil }
        guard let voice = keep ?? VoiceSettings.voices[parts[0]]?.first else { return nil }
        return LiveStep(provider: parts[0], model: parts[1], voice: voice)
    }

    private var model: Binding<String> {
        Binding(get: { "\(step.provider)/\(step.model)" }, set: { choice in
            if let next = Self.step(choice, voice: step) { change(next) }
        })
    }

    private var voice: Binding<String> {
        Binding(get: { step.voice }, set: { next in
            var step = step
            step.voice = next
            change(step)
        })
    }

    private var title: String { "\(VoiceSettings.names[step.provider] ?? step.provider) · \(step.model)" }

    var body: some View {
        HStack(spacing: PanelStyle.rowInset) {
            Text(index + 1, format: .number).monospacedDigit().foregroundStyle(.muted).accessibilityHidden(true)
            Button { choosing = true } label: {
                Text(title).lineLimit(1).frame(maxWidth: .infinity, alignment: .leading).contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Voice fallback \(index + 1)")
            .accessibilityValue(title)
            .dropdown(isPresented: $choosing, alignment: .leading) {
                DropdownOptions(selection: model, options: choices)
            }
            VoicePicker(selection: voice, voices: VoiceSettings.voices[step.provider] ?? [])
                .accessibilityLabel("Voice for \(title)")
            Menu {
                Button("Move up", systemImage: "arrow.up") { move(-1) }.disabled(index == 0)
                Button("Move down", systemImage: "arrow.down") { move(1) }.disabled(index == count - 1)
                Button("Remove", systemImage: "minus.circle", role: .destructive, action: remove)
            } label: {
                Image(systemName: "ellipsis")
                    .foregroundStyle(.muted)
                    #if os(iOS)
                    .frame(width: 44, height: 44)
                    #else
                    .frame(width: 20, height: 20)
                    #endif
                    .contentShape(Rectangle())
            }
            .menuStyle(.button)
            .menuIndicator(.hidden)
            .buttonStyle(.plain)
            .fixedSize()
            .accessibilityLabel("More actions for \(title)")
        }
        .sectionRow()
    }
}

private struct VoicePicker: View {
    @Binding var selection: String
    let voices: [String]
    @State private var choosing = false

    var body: some View {
        Button { choosing = true } label: {
            HStack(spacing: PanelStyle.cardInset) {
                Text(selection.capitalized).lineLimit(1)
                Image(systemName: "chevron.up.chevron.down")
            }
            .foregroundStyle(.muted)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityValue(selection.capitalized)
        .dropdown(isPresented: $choosing) {
            DropdownOptions(selection: $selection, options: voices.map { ($0.capitalized, $0) })
        }
    }
}
