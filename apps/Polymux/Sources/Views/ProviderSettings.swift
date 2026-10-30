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
    @State private var refreshing = false
    private var labels: [String: String] { store?.names ?? [:] }

    private var groups: [ProviderGroup] { ProviderGroup.collect(providers, names: labels) }

    var body: some View {
        Section {
            Button { choosing = true } label: {
                HStack {
                    Text("Default model").lineLimit(1).layoutPriority(1)
                    Spacer()
                    if selection.model.isEmpty { Text("Server default").foregroundStyle(.muted) }
                    else { ModelLabel(name: selection) }
                    Image(systemName: "chevron.up.chevron.down")
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Default model")
            .accessibilityValue(selection.model.isEmpty ? "Server default" : selection.title)
            .dropdown(isPresented: $choosing, width: 280) {
                ModelPicker(providers: providers, provider: settings?.value?.defaultProvider ?? "",
                            model: settings?.value?.defaultModel ?? "") { provider, model in
                    settings?.edit(SettingsPatch(defaultProvider: provider, defaultModel: model))
                    choosing = false
                }
                    .preferredColorScheme(scheme)
            }
            ReasoningPicker(selection: Binding(get: { settings?.value?.defaultReasoning ?? "off" },
                                               set: { settings?.edit(SettingsPatch(defaultReasoning: $0)) }))
        } header: {
            Text("Models").foregroundStyle(.primary)
        }
        .disabled(settings?.value == nil)
        .panelRow()
        Section {
            ForEach(groups) { group in
                HStack {
                    Button { edit(group.connections[0]) } label: {
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
                Text("No providers yet.").foregroundStyle(.primary).frame(maxWidth: .infinity)
            }
            if let issue = issue ?? store?.issue { Text(issue).font(.footnote).foregroundStyle(.muted) }
        } header: {
            HStack {
                Text("Providers").foregroundStyle(.primary)
                Spacer()
                Button("Refresh", systemImage: "arrow.clockwise") { Task { await refresh() } }
                    .accessibilityLabel("Refresh providers")
                    .help("Refresh providers")
                    .symbolEffect(.rotate, isActive: refreshing)
                    .disabled(client == nil || moving || refreshing)
                Button("Reorder", systemImage: "arrow.up.arrow.down") { reordering.toggle() }
                    .accessibilityLabel("Reorder providers")
                    .accessibilityValue(reordering ? "On" : "Off")
                    .help("Reorder providers")
                    .disabled(groups.count < 2 || moving || refreshing)
                Button("Add provider", systemImage: "plus") { edit(nil) }
                    .disabled(client == nil)
            }
            .labelStyle(.iconOnly)
            .buttonStyle(.borderless)
        }
        .panelRow()
        .task(id: client?.id) { await store?.refreshProviders() }
        .task(id: client?.id) { await store?.refreshOptions() }
    }

    private var selection: ModelName {
        guard let value = settings?.value, !value.defaultModel.isEmpty, !value.defaultProvider.isEmpty else { return ModelName(model: "") }
        let name = groups.first { $0.connections.contains { $0.id == value.defaultProvider } }?.name ?? value.defaultProvider
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
        guard !moving, !refreshing, loaded, let client,
              let source = groups.firstIndex(where: { $0.id == id }),
              groups.indices.contains(target), source != target else { return false }
        let previous = providers
        var ordered = groups
        ordered.insert(ordered.remove(at: source), at: target)
        var list = ordered.flatMap(\.connections)
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
}

struct ModelPicker: View {
    let providers: [Provider]
    let provider: String
    let model: String
    let choose: (String, String) -> Void
    @State private var query = ""
    @State private var highlighted: String?
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
                        if query.isEmpty {
                            option("Not set", id: "", selected: model.isEmpty) { choose("", "") }
                        }
                        ForEach(available) { group in
                            if query.isEmpty || group.id != available.first?.id {
                                Divider().padding(.vertical, PanelStyle.cardInset)
                            }
                            heading(group.name)
                            ForEach(matches(group), id: \.id) { item in
                                option(ModelName.format(item.id), id: key(group, item.id),
                                       selected: group.connections.contains { $0.id == provider } && model == item.id,
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
                .onChange(of: highlighted) { _, id in
                    if let id { scroll.scrollTo(id) }
                }
                .overlay {
                    if available.isEmpty && (!query.isEmpty || model.isEmpty) {
                        Text("No models found.").foregroundStyle(.primary)
                            .frame(maxWidth: .infinity, maxHeight: .infinity)
                            .allowsHitTesting(false)
                    }
                }
            }
        }
        .frame(idealHeight: height, maxHeight: height)
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
            .background(.primary.opacity(highlighted == id ? 0.1 : 0), in: .rect(cornerRadius: PanelStyle.rowRadius))
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label ?? title)
        .accessibilityValue(selected ? "Selected" : "")
        .accessibilityAddTraits(selected ? .isSelected : [])
        .onHover { hovering in
            if hovering { highlighted = id } else if highlighted == id { highlighted = nil }
        }
        .id(id)
    }

    private func key(_ group: ProviderGroup, _ model: String) -> String { group.id + "/" + model }

    private var choices: [(id: String, provider: String, model: String)] {
        var result: [(id: String, provider: String, model: String)] = query.isEmpty ? [("", "", "")] : []
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
    }

    private func select() {
        if let choice = choices.first(where: { $0.id == highlighted }) ?? (query.isEmpty ? nil : choices.first) {
            choose(choice.provider, choice.model)
        }
    }

    private var available: [ProviderGroup] {
        let connected = providers.filter {
            $0.enabled && (ProviderPreset(rawValue: $0.type) != nil || !($0.status ?? "").isEmpty)
        }
        return ProviderGroup.collect(connected, names: store?.names ?? [:]).filter { !matches($0).isEmpty }
    }

    private var currentTitle: String {
        ProviderGroup.collect(providers, names: store?.names ?? [:])
            .first { $0.connections.contains { $0.id == provider } }?.name ?? (provider.isEmpty ? "Default" : provider)
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
        let candidates = group.connections.filter { $0.models.contains { $0.id == model && $0.enabled } }
        return candidates.first { $0.id == provider }?.id ?? candidates.first?.id ?? ""
    }
}

struct ReasoningPicker: View {
    @Binding var selection: String
    var inset = false
    @State private var choosing = false
    private let options = [("Off", SettingsDefaultReasoning.off), ("Minimal", SettingsDefaultReasoning.minimal),
                           ("Low", SettingsDefaultReasoning.low), ("Medium", SettingsDefaultReasoning.medium),
                           ("High", SettingsDefaultReasoning.high), ("Xhigh", SettingsDefaultReasoning.xhigh),
                           ("Max", SettingsDefaultReasoning.max)]

    private var title: String { options.first { $0.1 == selection }?.0 ?? "Off" }

    var body: some View {
        if inset {
            panelPicker
        } else {
            SettingsPicker("Reasoning", selection: $selection, options: options)
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
        .sectionRow()
        .accessibilityLabel("Reasoning")
        .accessibilityValue(title)
        .dropdown(isPresented: $choosing, width: 160) {
            DropdownOptions(selection: $selection, options: options)
        }
    }
}

struct ProviderForm: View {
    let client: APIClient
    let preset: ProviderPreset?
    let connections: [Provider]
    let onBack: () -> Void
    let connect: (ProviderOption, String?) -> Void
    let select: (Provider) -> Void
    let add: () -> Void
    let saved: (Provider) -> Void
    let deleted: (String) -> Void
    @Environment(\.panelClose) private var close
    @State private var current: Provider?
    @State private var name: String
    @State private var baseURL: String
    @State private var key = ""
    @State private var enabled: Bool
    @State private var models: [ProviderModel]
    @Environment(ProviderStore.self) private var store: ProviderStore?
    private var options: [ProviderOption] { store?.options ?? [] }
    @State private var query = ""
    @State private var modelID = ""
    @State private var saving = false
    @State private var issue: String?

    init(client: APIClient, original: Provider?, preset: ProviderPreset?, connections: [Provider], onBack: @escaping () -> Void,
         connect: @escaping (ProviderOption, String?) -> Void, select: @escaping (Provider) -> Void, add: @escaping () -> Void,
         saved: @escaping (Provider) -> Void, deleted: @escaping (String) -> Void) {
        self.client = client
        self.preset = preset
        self.connections = connections
        self.onBack = onBack
        self.connect = connect
        self.select = select
        self.add = add
        self.saved = saved
        self.deleted = deleted
        _current = State(initialValue: original)
        _name = State(initialValue: original?.name ?? preset?.label ?? "")
        _baseURL = State(initialValue: original?.baseURL ?? preset?.baseURL ?? "")
        _enabled = State(initialValue: original?.enabled ?? true)
        _models = State(initialValue: original?.models ?? [])
    }

    private var manual: Bool { preset != nil }

    var body: some View {
        let type = current?.type ?? preset?.rawValue ?? "custom"
        let title = store?.names[type] ?? preset?.label ?? name
        PanelView(manual ? (current == nil ? "Add connection" : title) : (current?.status == ProviderOptionAuth.apiKey ? "Edit key" : "Edit account"),
                  enabled: !saving, onClose: { close() }, onBack: onBack) {
            Form {
                Section {
                    SettingsField(title: "Name", text: $name, prompt: "Enter name")
                    if manual {
                        SettingsField(title: "Base URL", text: $baseURL, prompt: "https://api.example.com/v1")
                    }
                    if current?.status == ProviderOptionAuth.apiKey {
                        LabeledContent("API key") {
                            Text(current?.keyHint ?? "••••••••").monospaced().foregroundStyle(.muted)
                        }
                    } else if manual {
                        SettingsField(title: "API key", text: $key, prompt: "Enter API key", secure: true)
                    } else if let current {
                        ForEach(options.filter { $0.auth == ProviderOptionAuth.oauth }, id: \.auth) { option in
                            LabeledContent("Account") {
                                Button("Reconnect") { connect(option, current.id) }
                                    .buttonStyle(.bordered)
                                    .accessibilityLabel("Reconnect account")
                            }
                        }
                    }
                    if current != nil && manual { Toggle("Enabled", isOn: $enabled) }
                } header: {
                    HStack {
                        Text(manual ? "Connection" : "Details").foregroundStyle(.primary)
                        Spacer()
                        if current != nil && manual {
                            Button("Add", systemImage: "plus", action: add)
                                .accessibilityLabel("Add connection").help("Add connection")
                        }
                    }
                    .buttonStyle(.borderless)
                    .labelStyle(.iconOnly)
                }
                .panelRow()
                if manual && connections.count > 1 {
                    Section {
                        ForEach(connections) { connection in
                            Button { select(connection) } label: {
                                HStack {
                                    Text(connection.name).lineLimit(1)
                                    Spacer()
                                    if current?.id == connection.id { Image(systemName: "checkmark") }
                                }
                                .contentShape(Rectangle())
                            }
                            .buttonStyle(.plain)
                            .accessibilityLabel("Select \(connection.name)")
                            .accessibilityValue(current?.id == connection.id ? "Selected" : "")
                        }
                    } header: { Text("Connections").foregroundStyle(.primary) }
                    .panelRow()
                }
                if manual {
                    Section {
                        if !models.isEmpty {
                            PanelSearch("Search models", text: $query)
                            Toggle("All models", isOn: Binding(get: { models.allSatisfy(\.enabled) }, set: { enabled in
                                for index in models.indices { models[index].enabled = enabled }
                            }))
                            .accessibilityLabel("All models")
                            ForEach($models, id: \.id) { $model in
                                if query.isEmpty || model.id.localizedStandardContains(query) {
                                    Toggle(model.id, isOn: $model.enabled).accessibilityLabel(model.id)
                                }
                            }
                            if !query.isEmpty && !models.contains(where: { $0.id.localizedStandardContains(query) }) {
                                Text("No models found.").frame(maxWidth: .infinity)
                            }
                        }
                        if preset != .elevenlabs {
                            HStack {
                                SettingsField(title: "Model ID", text: $modelID, prompt: "Enter model ID")
                                Button("Add") { addModel() }.disabled(modelID.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                            }
                        }
                    } header: {
                        HStack {
                            Text("Models").foregroundStyle(.primary)
                            Spacer()
                            if current != nil && preset != .elevenlabs {
                                Button("Refresh", systemImage: "arrow.clockwise") { Task { await refresh() } }
                                    .accessibilityLabel("Refresh models")
                                    .help("Refresh models")
                                    .labelStyle(.iconOnly)
                                    .buttonStyle(.borderless)
                            }
                        }
                    }
                    .panelRow()
                }
                if let issue {
                    Section { Text(issue).font(.footnote).foregroundStyle(.muted) }.panelRow()
                }
                if current != nil && manual {
                    Section {
                        Button("Delete", role: .destructive) { Task { await delete() } }
                            .buttonStyle(.borderless)
                            .frame(maxWidth: .infinity)
                    }
                    .panelRow()
                }
            }
            .formStyle(.grouped)
            .disabled(saving)
        } actions: {
            Button { Task { await save() } } label: {
                if saving { ProgressView().controlSize(.small) } else { Text("Save") }
            }
            .disabled(!valid || saving)
            .keyboardShortcut(.defaultAction)
        }
        .interactiveDismissDisabled(saving)
        .panelDismiss(enabled: !saving, onClose: { close() })
        .task { await store?.refreshOptions() }
    }

    private var valid: Bool {
        !name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && (!manual || URL(string: baseURL)?.host() != nil)
    }

    private func addModel() {
        let id = modelID.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !id.isEmpty else { return }
        if !models.contains(where: { $0.id == id }) { models.append(ProviderModel(id: id, tags: ["text"], enabled: true)) }
        modelID = ""
    }

    private func save() async {
        guard valid else { return }
        saving = true
        defer { saving = false }
        issue = nil
        if !manual, let current {
            do {
                let provider = try await client.renameProvider(id: current.id, name: name.trimmingCharacters(in: .whitespacesAndNewlines))
                saved(provider)
                onBack()
            } catch { issue = error.isOffline ? nil : error.reason(or: "Couldn’t save this name.") }
            return
        }
        let id = current?.id ?? "\(preset?.rawValue ?? "custom")-\(UUID().uuidString.lowercased())"
        var provider = Provider(id: id, type: current?.type ?? preset?.rawValue ?? "custom", name: name.trimmingCharacters(in: .whitespacesAndNewlines),
                                baseURL: baseURL.trimmingCharacters(in: .whitespacesAndNewlines), headers: current?.headers,
                                sessionHeader: current?.sessionHeader, enabled: enabled, order: current?.order ?? 0, models: models)
        let discover = manual && models.isEmpty && preset != .elevenlabs
        do {
            provider = try await client.saveProvider(provider, isNew: current == nil)
            current = provider
            if !key.isEmpty {
                try await client.setProviderKey(id: id, key: key)
                provider.status = ProviderOptionAuth.apiKey
                key = ""
            }
            if discover {
                do {
                    provider = try await client.discoverModels(id: id)
                    models = provider.models
                } catch {
                    saved(provider)
                    issue = error.reason(or: "Saved, but couldn’t load its models.")
                    return
                }
            }
            saved(provider)
            onBack()
        } catch {
            issue = error.isOffline ? "Connection lost before setup finished." : error.reason(or: "Couldn’t finish saving this provider.")
        }
    }

    private func refresh() async {
        guard let current else { return }
        saving = true
        defer { saving = false }
        do {
            let provider = try await client.discoverModels(id: current.id)
            self.current = provider
            models = provider.models
            saved(provider)
            issue = nil
        } catch { issue = error.reason(or: "Couldn’t load models.") }
    }

    private func delete() async {
        guard let current else { return }
        saving = true
        defer { saving = false }
        do {
            try await client.deleteProvider(id: current.id)
            deleted(current.id)
            onBack()
        } catch { issue = error.reason(or: "Couldn’t delete this provider.") }
    }
}

struct PanelSearch: View {
    let title: String
    @Binding var text: String
    let autofocus: Bool
    @FocusState private var focused: Bool

    init(_ title: String, text: Binding<String>, autofocus: Bool = false) {
        self.title = title
        _text = text
        self.autofocus = autofocus
    }

    var body: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass").foregroundStyle(.muted)
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
