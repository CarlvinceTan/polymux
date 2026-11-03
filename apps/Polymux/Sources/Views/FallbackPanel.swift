import SwiftUI
import PolymuxKit

/// Which models a bot moves to when its own can't answer.
struct FallbackPanel: View {
    let fallback: AgentFallback
    /// What a newly added model starts on.
    let reasoning: String
    let providers: [Provider]
    let saving: Bool
    let issue: String?
    let save: (AgentFallback) -> Void
    @Environment(\.panelClose) private var dismiss
    @State private var adding = false
    private let limit = 8
    private let modes = [("Off", AgentFallbackMode.off), ("Custom", AgentFallbackMode.custom), ("Auto", AgentFallbackMode.auto)]

    private var mode: Binding<String> {
        Binding(get: { fallback.mode }, set: { mode in
            var next = fallback
            next.mode = mode
            save(next)
        })
    }

    private var unavailable: Bool {
        !providers.isEmpty && fallback.models.contains { step in
            !providers.contains { provider in
                provider.id == step.provider && provider.enabled && provider.models.contains { $0.id == step.model && $0.enabled }
            }
        }
    }

    var body: some View {
        PanelView("Fallbacks", onClose: { dismiss() }, width: 460) {
            ScrollView {
                VStack(alignment: .leading, spacing: PanelStyle.sectionGap) {
                    PanelSection {
                        SettingsPicker("Mode", selection: mode, options: modes).sectionRow()
                    }
                    if fallback.mode == AgentFallbackMode.custom { order }
                    if let line = issue ?? (fallback.mode == AgentFallbackMode.custom && unavailable ? "Some models are unavailable." : nil) {
                        Text(line).font(.footnote).foregroundStyle(.muted)
                            .padding(.horizontal, PanelStyle.headingInset)
                    }
                }
                .disabled(saving)
                .padding(.horizontal, PanelStyle.contentInset)
            }
        }
        .panelDismiss(onClose: { dismiss() })
    }

    private var order: some View {
        PanelSection("Order") {
            if fallback.models.isEmpty {
                Text("No fallbacks.").foregroundStyle(.muted).frame(maxWidth: .infinity).sectionRow()
            }
            ForEach(Array(fallback.models.enumerated()), id: \.element.id) { index, step in
                FallbackRow(step: step, title: title(step), index: index, count: fallback.models.count, providers: providers) { next in
                    if next.id == step.id || !listed(next) { edit { $0[index] = next } }
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
                    .accessibilityLabel("Add fallback")
                    .dropdown(isPresented: $adding, alignment: .leading) {
                        ModelPicker(providers: providers, provider: "", model: "", clearable: false) { provider, model in
                            adding = false
                            let step = FallbackModel(provider: provider, model: model, reasoning: reasoning)
                            if !listed(step) { edit { $0.append(step) } }
                        }
                    }
            }
        }
    }

    private func title(_ step: FallbackModel) -> ModelName {
        ModelName(model: step.model, provider: providers.first { $0.id == step.provider }?.name ?? "Unavailable")
    }

    private func listed(_ step: FallbackModel) -> Bool { fallback.models.contains { $0.id == step.id } }

    private func edit(_ change: (inout [FallbackModel]) -> Void) {
        var next = fallback
        change(&next.models)
        save(next)
    }
}

private struct FallbackRow: View {
    let step: FallbackModel
    let title: ModelName
    let index: Int
    let count: Int
    let providers: [Provider]
    let change: (FallbackModel) -> Void
    let move: (Int) -> Void
    let remove: () -> Void
    @State private var choosing = false

    private var level: Binding<String> {
        Binding(get: { step.reasoning }, set: { reasoning in
            var next = step
            next.reasoning = reasoning
            change(next)
        })
    }

    var body: some View {
        HStack(spacing: PanelStyle.rowInset) {
            Text(index + 1, format: .number).monospacedDigit().foregroundStyle(.muted).accessibilityHidden(true)
            Button { choosing = true } label: {
                ModelLabel(name: title).frame(maxWidth: .infinity, alignment: .leading).contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Fallback \(index + 1)")
            .accessibilityValue(title.title)
            .dropdown(isPresented: $choosing, alignment: .leading) {
                ModelPicker(providers: providers, provider: step.provider, model: step.model, clearable: false) { provider, model in
                    choosing = false
                    var next = step
                    next.provider = provider
                    next.model = model
                    change(next)
                }
            }
            ReasoningPicker(selection: level, compact: true, levels: providers.levels(provider: step.provider, model: step.model))
                .accessibilityLabel("Reasoning for \(title.title)")
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
            .accessibilityLabel("More actions for \(title.title)")
        }
        .sectionRow()
    }
}
