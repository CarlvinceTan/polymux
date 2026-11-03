import SwiftUI
import PolymuxKit

enum MediaKind: String {
    case image, video

    var title: String { rawValue.capitalized }
    var tag: String { rawValue + "-gen" }

    /// The provider types the server can generate with natively.
    func supports(_ type: String) -> Bool { type == "gemini" || (type == "openai" && self == .image) }
}

/// The Models tab entries for choosing which models generate images and videos.
struct MediaSettings: View {
    let store: SettingsStore
    let open: (MediaKind) -> Void

    var body: some View {
        if let settings = store.value {
            Section("Generation") {
                ForEach([MediaKind.image, .video], id: \.self) { kind in
                    let order = kind == .image ? settings.imageOrder : settings.videoOrder
                    Button { open(kind) } label: {
                        HStack {
                            Text(kind.title).lineLimit(1).layoutPriority(1)
                            Spacer()
                            Text(order.mode == MediaOrderMode.custom ? "Custom" : "Auto").lineLimit(1)
                            Image(systemName: "chevron.right")
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("\(kind.title) models")
                    .accessibilityValue(order.mode == MediaOrderMode.custom ? "Custom" : "Auto")
                }
            }
            .panelRow()
        }
    }
}

/// Which models make images or videos, in the order the server tries them.
struct MediaOrderView: View {
    let kind: MediaKind
    let store: SettingsStore
    let providers: [Provider]
    let onBack: () -> Void
    @State private var adding = false
    private let limit = 8
    private let modes = [("Auto", MediaOrderMode.auto), ("Custom", MediaOrderMode.custom)]

    private var order: MediaOrder {
        let settings = store.value
        return (kind == .image ? settings?.imageOrder : settings?.videoOrder) ?? MediaOrder(mode: MediaOrderMode.auto)
    }

    private var mode: Binding<String> {
        Binding(get: { order.mode }, set: { mode in
            var next = order
            next.mode = mode
            save(next)
        })
    }

    /// Every enabled model that can generate this kind, keyed "provider\tmodel".
    private var choices: [(String, String)] {
        providers.filter { $0.enabled && kind.supports($0.type) }.flatMap { provider in
            provider.models.filter { $0.enabled && $0.tags.contains(kind.tag) }.map {
                (ModelName(model: $0.id, provider: provider.name).title, provider.id + "\t" + $0.id)
            }
        }
    }

    var body: some View {
        PanelView("\(kind.title) models", onClose: onBack, onBack: onBack, width: 460) {
            ScrollView {
                VStack(alignment: .leading, spacing: PanelStyle.sectionGap) {
                    PanelSection {
                        SettingsPicker("Mode", selection: mode, options: modes).sectionRow()
                    }
                    if order.mode == MediaOrderMode.custom { list }
                    if let issue = store.error {
                        Text(issue).font(.footnote).foregroundStyle(.muted).padding(.horizontal, PanelStyle.headingInset)
                    } else if choices.isEmpty {
                        Text("No \(kind.rawValue) models yet. Add a Gemini\(kind == .image ? " or OpenAI" : "") provider.")
                            .font(.footnote).foregroundStyle(.muted).padding(.horizontal, PanelStyle.headingInset)
                    }
                }
                .padding(.horizontal, PanelStyle.contentInset)
            }
        }
    }

    private var list: some View {
        PanelSection("Order") {
            if order.models.isEmpty {
                Text("No models.").foregroundStyle(.muted).frame(maxWidth: .infinity).sectionRow()
            }
            ForEach(Array(order.models.enumerated()), id: \.offset) { index, step in
                MediaStepRow(step: step, title: title(step), index: index, count: order.models.count, choices: choices) { next in
                    edit { $0[index] = next }
                } move: { offset in
                    edit { $0.swapAt(index, index + offset) }
                } remove: {
                    edit { $0.remove(at: index) }
                }
                if index < order.models.count - 1 || order.models.count < limit {
                    Divider().padding(.horizontal, PanelStyle.rowInset)
                }
            }
            if order.models.count < limit {
                Button { adding = true } label: { Label("Add", systemImage: "plus") }
                    .buttonStyle(SectionButton())
                    .accessibilityLabel("Add \(kind.rawValue) model")
                    .dropdown(isPresented: $adding, alignment: .leading) {
                        DropdownOptions(selection: Binding(get: { "" }, set: { choice in
                            adding = false
                            if let step = MediaStepRow.step(choice) { edit { $0.append(step) } }
                        }), options: choices)
                    }
            }
        }
    }

    private func title(_ step: MediaStep) -> String {
        let name = providers.first { $0.id == step.provider }?.name ?? "Unavailable"
        return ModelName(model: step.model, provider: name).title
    }

    private func edit(_ change: (inout [MediaStep]) -> Void) {
        var next = order
        change(&next.models)
        save(next)
    }

    private func save(_ next: MediaOrder) {
        store.edit(kind == .image ? SettingsPatch(imageOrder: next) : SettingsPatch(videoOrder: next))
    }
}

private struct MediaStepRow: View {
    let step: MediaStep
    let title: String
    let index: Int
    let count: Int
    let choices: [(String, String)]
    let change: (MediaStep) -> Void
    let move: (Int) -> Void
    let remove: () -> Void
    @State private var choosing = false

    static func step(_ choice: String) -> MediaStep? {
        let parts = choice.split(separator: "\t", maxSplits: 1).map(String.init)
        return parts.count == 2 ? MediaStep(provider: parts[0], model: parts[1]) : nil
    }

    private var model: Binding<String> {
        Binding(get: { step.provider + "\t" + step.model }, set: { choice in
            if let next = Self.step(choice) { change(next) }
        })
    }

    var body: some View {
        HStack(spacing: PanelStyle.rowInset) {
            Text(index + 1, format: .number).monospacedDigit().foregroundStyle(.muted).accessibilityHidden(true)
            Button { choosing = true } label: {
                Text(title).lineLimit(1).frame(maxWidth: .infinity, alignment: .leading).contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Model \(index + 1)")
            .accessibilityValue(title)
            .dropdown(isPresented: $choosing, alignment: .leading) {
                DropdownOptions(selection: model, options: choices)
            }
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
