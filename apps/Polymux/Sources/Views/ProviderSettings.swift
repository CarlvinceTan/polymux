import SwiftUI
import PolymuxKit
import UniformTypeIdentifiers

private struct ProviderDrag: Codable, Transferable {
    let id: String

    static var transferRepresentation: some TransferRepresentation {
        CodableRepresentation(contentType: UTType(exportedAs: "com.flarehq.polymux.provider-order", conformingTo: .data))
    }
}

enum ProviderPreset: String, CaseIterable, Identifiable {
    case ollama, lmstudio, elevenlabs, custom
    var id: String { rawValue }

    var label: String {
        switch self {
        case .ollama: "Ollama"
        case .lmstudio: "LM Studio"
        case .elevenlabs: "ElevenLabs"
        case .custom: "Custom"
        }
    }

    var baseURL: String {
        switch self {
        case .ollama: "http://127.0.0.1:11434/v1"
        case .lmstudio: "http://127.0.0.1:1234/v1"
        case .elevenlabs: "https://api.elevenlabs.io/v1"
        case .custom: ""
        }
    }
}

struct ProviderSettings: View {
    let client: APIClient?
    let settings: SettingsStore?
    @Environment(ProviderStore.self) private var store: ProviderStore?
    private var providers: [Provider] { store?.providers ?? [] }
    let edit: (Provider?) -> Void
    @Environment(\.colorScheme) private var scheme
    @State private var choosing = false
    @State private var issue: String?
    private var loaded: Bool { store?.providers != nil }
    @State private var moving = false
    @State private var reordering = false
    /// The provider order being edited in reorder mode; nil when not reordering.
    @State private var draft: [Provider]?
    @State private var refreshing = false
    private var labels: [String: String] { store?.names ?? [:] }

    private var ordered: [Provider] { draft ?? providers }
    private var groups: [ProviderGroup] { ProviderGroup.collect(ordered) }

    var body: some View {
        Section {
            Button { choosing = true } label: {
                HStack {
                    Text("Model").lineLimit(1).layoutPriority(1)
                    Spacer()
                    if selection.model.isEmpty { Text("Server default").foregroundStyle(.muted) }
                    else { ModelLabel(name: selection) }
                    Image(systemName: "chevron.up.chevron.down")
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Model")
            .accessibilityValue(selection.model.isEmpty ? "Server default" : selection.title)
            .dropdown(isPresented: $choosing, width: .fixed(DropdownStyle.modelWidth)) {
                ModelPicker(providers: providers, provider: settings?.value?.defaultProvider ?? "",
                            model: settings?.value?.defaultModel ?? "") { provider, model in
                    settings?.edit(SettingsPatch(defaultProvider: provider, defaultModel: model))
                    choosing = false
                }
                    .preferredColorScheme(scheme)
            }
            ReasoningPicker(selection: Binding(get: { settings?.value?.defaultReasoning ?? "off" },
                                               set: { settings?.edit(SettingsPatch(defaultReasoning: $0)) }),
                            levels: providers.levels(provider: settings?.value?.defaultProvider ?? "",
                                                     model: settings?.value?.defaultModel ?? ""))
        } header: {
            SectionHeader("Default model", symbol: "cpu.fill", tint: .purple)
        }
        .disabled(settings?.value == nil)
        .panelRow()
        Section {
            ForEach(groups) { group in
                HStack {
                    Button { edit(group.provider) } label: {
                        HStack {
                            VStack(alignment: .leading, spacing: 2) {
                                Text(group.name)
                                Text(group.summary).font(.caption).foregroundStyle(.primary)
                            }
                            .lineLimit(1)
                            Spacer()
                            if !reordering {
                                Image(systemName: "chevron.right").foregroundStyle(.muted)
                            }
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .accessibilityLabel("Edit \(group.name)")
                    .accessibilityValue(group.summary)
                    .disabled(reordering)
                    if reordering {
                        Button("Move up", systemImage: "arrow.up") { move(group.id, by: -1) }
                            .accessibilityLabel("Move \(group.name) up")
                            .help("Move up")
                            .disabled(moving || refreshing || !loaded || groups.first?.id == group.id)
                        Button("Move down", systemImage: "arrow.down") { move(group.id, by: 1) }
                            .accessibilityLabel("Move \(group.name) down")
                            .help("Move down")
                            .disabled(moving || refreshing || !loaded || groups.last?.id == group.id)
                    }
                }
                .labelStyle(.iconOnly)
                .buttonStyle(.borderless)
                .draggable(ProviderDrag(id: group.id))
                .dropDestination(for: ProviderDrag.self) { items, _ in
                    guard let id = items.first?.id,
                          let target = groups.firstIndex(where: { $0.id == group.id }) else { return false }
                    return move(id, to: target)
                }
            }
            if providers.isEmpty && loaded {
                Text("No providers yet.").foregroundStyle(.muted).frame(maxWidth: .infinity)
            }
            if let issue = issue ?? store?.issue { Text(issue).font(.footnote).foregroundStyle(.muted) }
        } header: {
            SectionHeader("Providers", symbol: "cloud.fill", tint: .blue) {
                Group {
                    if reordering {
                        Button("Save order", systemImage: "checkmark") { saveOrder() }
                            .accessibilityLabel("Save provider order")
                            .help("Save order")
                            .disabled(moving || !loaded)
                        Button("Cancel", systemImage: "xmark") { cancelOrder() }
                            .accessibilityLabel("Cancel reordering")
                            .help("Cancel reordering")
                            .disabled(moving)
                    } else {
                        Button("Refresh", systemImage: "arrow.clockwise") { Task { await refresh() } }
                            .accessibilityLabel("Refresh providers")
                            .help("Refresh providers")
                            .symbolEffect(.rotate, isActive: refreshing)
                            .disabled(client == nil || moving || refreshing)
                        Button("Reorder", systemImage: "arrow.up.arrow.down") { startReordering() }
                            .accessibilityLabel("Reorder providers")
                            .help("Reorder providers")
                            .disabled(groups.count < 2 || moving || refreshing)
                        Button("Add provider", systemImage: "plus") { edit(nil) }
                            .disabled(client == nil)
                    }
                }
                .labelStyle(.iconOnly)
                .buttonStyle(.borderless)
            }
        }
        .panelRow()
        .task(id: client?.id) { await store?.refreshProviders() }
        .task(id: client?.id) { await store?.refreshOptions() }
    }

    private var selection: ModelName {
        guard let value = settings?.value, !value.defaultModel.isEmpty, !value.defaultProvider.isEmpty else { return ModelName(model: "") }
        let name = groups.first { $0.id == value.defaultProvider }?.name ?? value.defaultProvider
        return ModelName(model: value.defaultModel, provider: name)
    }

    private func refresh() async {
        guard !refreshing, !moving, let client else { return }
        refreshing = true
        defer { refreshing = false }
        await store?.refreshProviders(force: true)
        store?.begin()
        defer { store?.end() }
        var failed = false
        await withTaskGroup(of: Provider?.self) { group in
            for provider in providers where provider.type != ProviderPreset.elevenlabs.rawValue {
                group.addTask { try? await client.discoverModels(id: provider.id) }
            }
            for await updated in group {
                if let updated { store?.merge([updated]) } else { failed = true }
            }
        }
        issue = failed ? "Couldn’t refresh some models." : nil
    }

    private func move(_ id: String, by offset: Int) {
        guard let source = groups.firstIndex(where: { $0.id == id }) else { return }
        move(id, to: source + offset)
    }

    @discardableResult
    private func move(_ id: String, to target: Int) -> Bool {
        guard !moving, !refreshing, loaded, let source = groups.firstIndex(where: { $0.id == id }),
              groups.indices.contains(target), source != target else { return false }
        if draft != nil {
            var ordered = groups
            ordered.insert(ordered.remove(at: source), at: target)
            draft = ordered.map(\.provider)
            return true
        }
        guard let client else { return false }
        let previous = providers
        var ordered = groups
        ordered.insert(ordered.remove(at: source), at: target)
        var list = ordered.map(\.provider)
        for index in list.indices { list[index].order = Int64(index) }
        store?.begin()
        store?.accept(list)
        moving = true
        issue = nil
        let ids = providers.map(\.id)
        Task {
            defer { moving = false; store?.end() }
            do {
                store?.accept(try await client.orderProviders(ids))
            } catch {
                store?.accept(previous)
                issue = "Couldn’t save provider order."
            }
        }
        return true
    }

    private func startReordering() {
        guard groups.count > 1 else { return }
        draft = providers
        reordering = true
    }

    private func cancelOrder() {
        draft = nil
        reordering = false
    }

    private func saveOrder() {
        guard !moving else { return }
        guard let client, let draft else { cancelOrder(); return }
        let previous = providers
        var list = draft
        for index in list.indices { list[index].order = Int64(index) }
        store?.begin()
        store?.accept(list)
        moving = true
        issue = nil
        let ids = list.map(\.id)
        Task {
            defer { store?.end() }
            do {
                store?.accept(try await client.orderProviders(ids))
                moving = false
                self.draft = nil
                reordering = false
            } catch {
                store?.accept(previous)
                moving = false
                issue = "Couldn’t save provider order."
            }
        }
    }
}

struct ModelPicker: View {
    let providers: [Provider]
    let provider: String
    let model: String
    /// Offers Not set.
    var clearable = true
    let choose: (String, String) -> Void
    @State private var query = ""
    @State private var highlighted: String?
    @State private var keyed: String?
    @Environment(\.dropdownClose) private var dismiss
    @Environment(ProviderStore.self) private var store: ProviderStore?

    var body: some View {
        VStack(spacing: 0) {
            PanelSearch("Search models", text: $query, autofocus: true)
                .padding(PanelStyle.headingInset)
                .onSubmit { select() }
            Divider()
            ScrollViewReader { scroll in
                ScrollView {
                    LazyVStack(alignment: .leading, spacing: 0) {
                        if query.isEmpty && clearable {
                            option("Not set", id: "", selected: model.isEmpty) { choose("", "") }
                        }
                        ForEach(available) { group in
                            if (query.isEmpty && clearable) || group.id != available.first?.id {
                                Divider().padding(.vertical, PanelStyle.cardInset)
                            }
                            heading(group.name)
                            ForEach(matches(group), id: \.id) { item in
                                option(ModelName.format(item.id), id: key(group, item.id),
                                       selected: group.id == provider && model == item.id,
                                       label: ModelName(model: item.id, provider: group.name).title) {
                                    choose(source(group, model: item.id), item.id)
                                }
                            }
                        }
                        if available.isEmpty, query.isEmpty, !model.isEmpty {
                            Divider().padding(.vertical, PanelStyle.cardInset)
                            heading(currentTitle)
                            option(ModelName.format(model), id: "current", selected: true,
                                   label: ModelName(model: model, provider: currentTitle).title) {
                                choose(provider, model)
                            }
                        }
                    }
                    .padding(PanelStyle.cardInset)
                }
                .scrollBounceBehavior(.basedOnSize)
                // Only the keyboard scrolls: a row under the pointer is already in view.
                .onChange(of: keyed) { _, id in
                    if let id { scroll.scrollTo(id) }
                }
                .overlay {
                    if available.isEmpty && (!query.isEmpty || model.isEmpty) {
                        Text("No models found.").foregroundStyle(.muted)
                            .frame(maxWidth: .infinity, maxHeight: .infinity)
                            .allowsHitTesting(false)
                    }
                }
            }
        }
        .frame(idealWidth: DropdownStyle.listWidth, idealHeight: height, maxHeight: height)
        .onChange(of: query) { highlighted = nil }
        #if os(macOS)
        .onKeyPress(.downArrow) { move(1); return .handled }
        .onKeyPress(.upArrow) { move(-1); return .handled }
        .onExitCommand { dismiss() }
        #endif
    }

    private var height: CGFloat {
        let rows = providers.reduce(0) { $0 + $1.models.count }
        return min(DropdownStyle.maximumHeight, max(160, CGFloat(rows + 2) * DropdownStyle.rowHeight + 60))
    }

    private func heading(_ title: String) -> some View {
        Text(ModelName.format(title))
            .font(.caption.weight(.semibold)).foregroundStyle(.primary)
            .lineLimit(1)
            .padding(.leading, PanelStyle.rowInset + PanelStyle.contentInset + PanelStyle.rowInset)
            .padding(.vertical, PanelStyle.rowPadding)
            .accessibilityAddTraits(.isHeader)
    }

    private func option(_ title: String, id: String, selected: Bool, label: String? = nil,
                        action: @escaping () -> Void) -> some View {
        Button(action: action) {
            HStack(spacing: PanelStyle.rowInset) {
                Image(systemName: "checkmark").opacity(selected ? 1 : 0)
                    .frame(width: PanelStyle.contentInset).accessibilityHidden(true)
                Text(title).lineLimit(1).frame(maxWidth: .infinity, alignment: .leading)
            }
            .padding(.horizontal, PanelStyle.rowInset)
            .frame(minHeight: DropdownStyle.rowHeight)
            .contentShape(.rect(cornerRadius: PanelStyle.rowRadius))
            .background(.primary.opacity(highlighted == id ? PanelStyle.selectedTint : 0), in: .rect(cornerRadius: PanelStyle.rowRadius))
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label ?? title)
        .accessibilityValue(selected ? "Selected" : "")
        .accessibilityAddTraits(selected ? .isSelected : [])
        .pointerHover { hovering in
            if hovering { highlighted = id } else if highlighted == id { highlighted = nil }
        }
        .id(id)
    }

    private func key(_ group: ProviderGroup, _ model: String) -> String { group.id + "/" + model }

    private var choices: [(id: String, provider: String, model: String)] {
        var result: [(id: String, provider: String, model: String)] = query.isEmpty && clearable ? [("", "", "")] : []
        for group in available {
            result += matches(group).map { (key(group, $0.id), source(group, model: $0.id), $0.id) }
        }
        if available.isEmpty, query.isEmpty, !model.isEmpty { result.append(("current", provider, model)) }
        return result
    }

    private func move(_ offset: Int) {
        let choices = choices
        guard !choices.isEmpty else { return }
        let index = highlighted.flatMap { id in choices.firstIndex { $0.id == id } } ?? (offset > 0 ? -1 : choices.count)
        highlighted = choices[max(0, min(choices.count - 1, index + offset))].id
        keyed = highlighted
    }

    private func select() {
        if let choice = choices.first(where: { $0.id == highlighted }) ?? (query.isEmpty ? nil : choices.first) {
            choose(choice.provider, choice.model)
        }
    }

    private var available: [ProviderGroup] {
        let connected = providers.filter {
            $0.enabled && (ProviderPreset(rawValue: $0.type) != nil || $0.signedIn)
        }
        return ProviderGroup.collect(connected).filter { !matches($0).isEmpty }
    }

    private var currentTitle: String {
        ProviderGroup.collect(providers).first { $0.id == provider }?.name ?? (provider.isEmpty ? "Default" : provider)
    }

    private func matches(_ group: ProviderGroup) -> [ProviderModel] {
        let terms = query.split(whereSeparator: \.isWhitespace)
        return group.models.filter { model in
            let text = "\(group.name) \(model.id) \(ModelName(model: model.id, provider: group.name).title)"
            return model.enabled && (model.tags.isEmpty || model.tags.contains("text"))
                && terms.allSatisfy { text.localizedStandardContains(String($0)) }
        }
    }

    private func source(_ group: ProviderGroup, model: String) -> String {
        group.provider.models.contains { $0.id == model && $0.enabled } ? group.id : ""
    }
}

/// The reasoning levels a model takes, set where its provider is added.
struct ReasoningLevelsMenu: View {
    @Binding var levels: [String]?
    @State private var choosing = false
    private let all = [("Off", SettingsDefaultReasoning.off), ("Low", SettingsDefaultReasoning.low),
                       ("Medium", SettingsDefaultReasoning.medium), ("High", SettingsDefaultReasoning.high),
                       ("Xhigh", SettingsDefaultReasoning.xhigh), ("Max", SettingsDefaultReasoning.max)]

    /// The highest level the model takes, which bounds the levels below it.
    private var level: String {
        all.last { levels?.contains($0.1) ?? false }?.1 ?? SettingsDefaultReasoning.off
    }

    private var title: String { all.first { $0.1 == level }?.0 ?? "Off" }

    var body: some View {
        Button { choosing = true } label: {
            HStack(spacing: 4) {
                Image(systemName: "brain")
                Text(title).lineLimit(1)
            }
            .foregroundStyle(.muted)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Reasoning levels")
        .accessibilityValue(title)
        .dropdown(isPresented: $choosing) {
            DropdownOptions(selection: Binding(get: { level }, set: { reset(upTo: $0) }), options: all)
        }
    }

    private func reset(upTo level: String) {
        let index = all.firstIndex { $0.1 == level } ?? 0
        levels = Array(all[..<index].map(\.1)) + [level]
    }
}

struct ReasoningPicker: View {
    @Binding var selection: String
    var inset = false
    /// Just the value, for a row that already shows the model.
    var compact = false
    /// The model's levels; empty offers them all.
    var levels: [String] = []
    @State private var choosing = false
    private let all = [("Off", SettingsDefaultReasoning.off), ("Minimal", SettingsDefaultReasoning.minimal),
                           ("Low", SettingsDefaultReasoning.low), ("Medium", SettingsDefaultReasoning.medium),
                           ("High", SettingsDefaultReasoning.high), ("Xhigh", SettingsDefaultReasoning.xhigh),
                           ("Max", SettingsDefaultReasoning.max)]

    /// Off, then the levels the model declared.
    private var options: [(String, String)] {
        levels.isEmpty ? all : all.filter { $0.1 == SettingsDefaultReasoning.off || levels.contains($0.1) }
    }

    /// A saved level the model lacks shows as the one it runs at: the next up, else the next down.
    private var level: String {
        let offered = options.map(\.1)
        guard !offered.contains(selection) else { return selection }
        let order = all.map(\.1)
        let index = order.firstIndex(of: selection) ?? 0
        return order[index...].first(where: offered.contains) ?? order[..<index].last(where: offered.contains) ?? selection
    }

    private var chosen: Binding<String> {
        Binding(get: { level }, set: { selection = $0 })
    }

    private var title: String { all.first { $0.1 == level }?.0 ?? "Off" }

    var body: some View {
        if compact {
            valuePicker
        } else if inset {
            panelPicker
        } else {
            SettingsPicker("Reasoning", selection: chosen, options: options)
        }
    }

    private var valuePicker: some View {
        Button { choosing = true } label: {
            HStack(spacing: PanelStyle.cardInset) {
                Text(title).lineLimit(1)
                Image(systemName: "chevron.up.chevron.down")
            }
            .foregroundStyle(.muted)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Reasoning")
        .accessibilityValue(title)
        .dropdown(isPresented: $choosing) {
            DropdownOptions(selection: chosen, options: options)
        }
    }

    private var panelPicker: some View {
        Button { choosing = true } label: {
            HStack {
                Label(title, systemImage: "brain.head.profile")
                    .lineLimit(1).layoutPriority(1)
                Spacer()
                Image(systemName: "chevron.up.chevron.down")
            }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("Reasoning")
        .accessibilityValue(title)
        .dropdown(isPresented: $choosing, width: .trigger) {
            DropdownOptions(selection: chosen, options: options)
        }
        .sectionRow()
    }
}

struct PanelSearch: View {
    let title: String
    @Binding var text: String
    let autofocus: Bool
    /// Widens the glyph's slot, so it lines up with row icons below.
    let glyph: CGFloat?
    @FocusState private var focused: Bool

    init(_ title: String, text: Binding<String>, autofocus: Bool = false, glyph: CGFloat? = nil) {
        self.title = title
        _text = text
        self.autofocus = autofocus
        self.glyph = glyph
    }

    var body: some View {
        HStack(spacing: glyph == nil ? 8 : SettingsNav.iconGap) {
            Image(systemName: "magnifyingglass").foregroundStyle(.muted)
                .frame(width: glyph)
            TextField(title, text: $text, prompt: Text(title).foregroundStyle(.muted))
                .labelsHidden()
                .textFieldStyle(.plain).autocorrectionDisabled().accessibilityLabel(title)
                .focused($focused)
                .onAppear { if autofocus { focused = true } }
                #if os(macOS)
                .background { SearchFocus(prompt: title, autofocus: autofocus).frame(width: 0, height: 0) }
                #endif
        }
    }
}
