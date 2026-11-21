import SwiftUI
import PolymuxKit

/// One provider: its name and endpoint, the connections it is used through, and its models.
struct ProviderPage: View {
    let client: APIClient
    let providerID: String
    let onBack: () -> Void
    let connect: (ProviderOption, String?) -> Void
    let editConnection: (Connection?) -> Void
    let removed: (String) -> Void
    @Environment(\.panelClose) private var close
    @Environment(ProviderStore.self) private var store: ProviderStore?
    @State private var name = ""
    @State private var baseURL = ""
    @State private var query = ""
    @State private var modelID = ""
    @State private var levels: [String]?
    @State private var editing: String?
    @State private var busy = false
    @State private var refreshing = false
    @State private var issue: String?
    @State private var removal: Connection?
    @State private var loaded = false

    typealias Connection = ProviderConnection

    private var provider: Provider? { store?.providers?.first { $0.id == providerID } }
    private var options: [ProviderOption] { (store?.options ?? []).filter { $0.type == provider?.type } }
    /// A PiG provider keeps its own name, endpoint and model list.
    private var builtin: Bool { provider.map { ProviderPreset(rawValue: $0.type) == nil } ?? false }
    private var editable: Bool { !builtin }
    private var addsModels: Bool { editable && provider?.type != ProviderPreset.elevenlabs.rawValue }

    var body: some View {
        PanelView(provider?.name ?? "Provider", onClose: { close() }, onBack: { commitDetails(); onBack() }) {
            if let provider {
                Form {
                    details(provider)
                    connections(provider)
                    models(provider)
                    if let issue { Section { Text(issue).font(.footnote).foregroundStyle(.muted) }.panelRow() }
                    Section {
                        Button("Delete", role: .destructive) { delete() }
                            .buttonStyle(.borderless)
                            .frame(maxWidth: .infinity)
                    }
                    .panelRow()
                }
                .formStyle(.grouped)
                .disabled(busy)
            }
        }
        .panelDismiss(onClose: { commitDetails(); close() })
        .task { await store?.refreshOptions() }
        .onAppear {
            guard !loaded, let provider else { return }
            loaded = true
            name = provider.name
            baseURL = provider.baseURL
        }
        .alert("Remove connection?", isPresented: Binding(get: { removal != nil }, set: { if !$0 { removal = nil } }),
               presenting: removal) { connection in
            Button("Remove", role: .destructive) { remove(connection) }
            Button("Cancel", role: .cancel) {}
        } message: { connection in
            Text("This removes \(label(connection)) and its saved credentials from this server.")
        }
    }

    private func details(_ provider: Provider) -> some View {
        Section {
            if editable {
                SettingsField(title: "Name", text: $name, prompt: "Enter name").onSubmit(commitDetails)
                SettingsField(title: "Base URL", text: $baseURL, prompt: "https://api.example.com/v1").onSubmit(commitDetails)
            } else {
                LabeledContent("Name", value: provider.name)
                LabeledContent("Base URL") {
                    Text(provider.baseURL).lineLimit(1).truncationMode(.middle).foregroundStyle(.muted)
                }
            }
            Toggle("Enabled", isOn: Binding(get: { provider.enabled }, set: { value in commit { $0.enabled = value } }))
                .accessibilityLabel("Enabled")
        }
        .panelRow()
    }

    private func connections(_ provider: Provider) -> some View {
        Section {
            ForEach(provider.connections, id: \.id) { connection in row(connection, of: provider) }
        } header: {
            HStack {
                Text("Connections").foregroundStyle(.primary)
                Spacer()
                addMenu
            }
        }
        .panelRow()
    }

    @ViewBuilder private var addMenu: some View {
        if options.count > 1 {
            Menu {
                ForEach(options, id: \.self) { option in
                    Button(option.auth == ProviderOptionAuth.apiKey ? "API key" : "Sign in") { connect(option, nil) }
                }
            } label: {
                Image(systemName: "plus")
            }
            .menuStyle(.button).menuIndicator(.hidden).buttonStyle(.borderless)
            .accessibilityLabel("Add connection").help("Add connection")
        } else {
            Button("Add connection", systemImage: "plus") {
                if let option = options.first { connect(option, nil) } else { editConnection(nil) }
            }
            .labelStyle(.iconOnly).buttonStyle(.borderless)
            .accessibilityLabel("Add connection").help("Add connection")
        }
    }

    private func row(_ connection: Connection, of provider: Provider) -> some View {
        HStack {
            Button { editConnection(connection) } label: {
                HStack {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(label(connection)).lineLimit(1)
                        if let hint = connection.keyHint, !hint.isEmpty {
                            Text(hint).font(.caption.monospaced()).foregroundStyle(.muted).lineLimit(1)
                        } else if connection.status == ProviderOptionAuth.oauth {
                            Text("Signed in").font(.caption).foregroundStyle(.muted)
                        } else if connection.status?.isEmpty != false {
                            Text("No key").font(.caption).foregroundStyle(.muted)
                        }
                    }
                    Spacer(minLength: PanelStyle.rowInset)
                    if !connection.enabled { Text("Off").foregroundStyle(.muted) }
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel("Edit \(label(connection))")
            .accessibilityValue(connection.keyHint ?? "")
            Menu {
                Button("Edit", systemImage: "pencil") { editConnection(connection) }
                Button(connection.enabled ? "Turn off" : "Turn on", systemImage: connection.enabled ? "pause" : "play") {
                    toggle(connection)
                }
                Button("Remove", systemImage: "minus.circle", role: .destructive) { removal = connection }
                    .disabled(provider.connections.count < 2)
            } label: {
                Image(systemName: "ellipsis").foregroundStyle(.muted).frame(width: 20, height: 20).contentShape(Rectangle())
            }
            .menuStyle(.button).menuIndicator(.hidden).buttonStyle(.plain).fixedSize()
            .accessibilityLabel("More actions for \(label(connection))")
        }
    }

    private func label(_ connection: Connection) -> String {
        if !connection.name.isEmpty { return connection.name }
        return connection.status == ProviderOptionAuth.oauth ? "Account" : "API key"
    }

    private func models(_ provider: Provider) -> some View {
        let models = provider.models.filter { query.isEmpty || $0.id.localizedStandardContains(query) }
        return Section {
            if provider.models.isEmpty {
                Text("No models yet.").frame(maxWidth: .infinity)
            } else {
                PanelSearch("Search models", text: $query)
                Toggle("All models", isOn: Binding(get: { provider.models.allSatisfy(\.enabled) }, set: { value in
                    commit { for index in $0.models.indices { $0.models[index].enabled = value } }
                }))
                .accessibilityLabel("All models")
                ForEach(models, id: \.id) { model in modelRow(model) }
                if models.isEmpty { Text("No models found.").frame(maxWidth: .infinity) }
            }
            if addsModels {
                HStack {
                    SettingsField(title: "Model ID", text: $modelID, prompt: "Enter model ID")
                    ReasoningLevelsMenu(levels: $levels)
                    Button(editing == nil ? "Add" : "Save", action: saveModel)
                        .disabled(modelID.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                }
            }
        } header: {
            HStack {
                Text("Models").foregroundStyle(.primary)
                Spacer()
                if provider.type != ProviderPreset.elevenlabs.rawValue {
                    Button("Refresh", systemImage: "arrow.clockwise", action: refresh)
                        .buttonStyle(.borderless).labelStyle(.iconOnly)
                        .accessibilityLabel("Refresh models").help("Refresh models")
                        .disabled(refreshing)
                }
            }
        }
        .panelRow()
    }

    private func modelRow(_ model: ProviderModel) -> some View {
        HStack {
            Toggle(isOn: Binding(get: { model.enabled }, set: { value in
                commit { if let index = $0.models.firstIndex(where: { $0.id == model.id }) { $0.models[index].enabled = value } }
            })) { Text(model.id).lineLimit(1) }
                .accessibilityLabel(model.id)
            if addsModels {
                Button("Edit", systemImage: "pencil") {
                    editing = model.id
                    modelID = model.id
                    levels = model.reasoning
                }
                .labelStyle(.iconOnly).buttonStyle(.borderless).foregroundStyle(.muted)
                .accessibilityLabel("Edit \(model.id)")
                Button("Remove", systemImage: "minus.circle") {
                    commit { $0.models.removeAll { $0.id == model.id } }
                    if editing == model.id { editing = nil; modelID = ""; levels = nil }
                }
                .labelStyle(.iconOnly).buttonStyle(.borderless).foregroundStyle(.muted)
                .accessibilityLabel("Remove \(model.id)")
            }
        }
    }

    private func saveModel() {
        let id = modelID.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !id.isEmpty else { return }
        let previous = editing
        commit { provider in
            if let previous, let index = provider.models.firstIndex(where: { $0.id == previous }) {
                if !provider.models.contains(where: { $0.id == id && $0.id != previous }) { provider.models[index].id = id }
                provider.models[index].reasoning = levels
            } else if !provider.models.contains(where: { $0.id == id }) {
                provider.models.append(ProviderModel(id: id, tags: ["text"], enabled: true, reasoning: levels))
            }
        }
        editing = nil
        modelID = ""
        levels = nil
    }

    /// Saves the name and endpoint once they've been typed.
    private func commitDetails() {
        guard editable, let provider else { return }
        let nextName = name.trimmingCharacters(in: .whitespacesAndNewlines)
        let nextURL = baseURL.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !nextName.isEmpty, URL(string: nextURL)?.host() != nil,
              nextName != provider.name || nextURL != provider.baseURL else { return }
        commit { $0.name = nextName; $0.baseURL = nextURL }
    }

    /// Applies a change at once and saves it, putting the old provider back if the server refuses.
    private func commit(_ change: (inout Provider) -> Void) {
        guard let previous = provider else { return }
        var next = previous
        change(&next)
        issue = nil
        store?.begin()
        store?.merge([next])
        Task {
            defer { store?.end() }
            do {
                store?.merge([try await client.saveProvider(next, isNew: false)])
            } catch {
                store?.merge([previous])
                issue = error.reason(or: "Couldn’t save provider settings.")
            }
        }
    }

    private func toggle(_ connection: Connection) {
        act { try await client.enableConnection(provider: providerID, connection: connection.id, enabled: !connection.enabled) }
    }

    private func remove(_ connection: Connection) {
        act { try await client.removeConnection(provider: providerID, connection: connection.id) }
    }

    private func act(_ work: @escaping () async throws -> Provider) {
        busy = true
        issue = nil
        store?.begin()
        Task {
            defer { busy = false; store?.end() }
            do { store?.merge([try await work()]) }
            catch { issue = error.reason(or: "Couldn’t change this connection.") }
        }
    }

    private func refresh() {
        guard !refreshing, let provider else { return }
        refreshing = true
        issue = nil
        store?.begin()
        Task {
            defer { refreshing = false; store?.end() }
            do { store?.merge([try await client.discoverModels(id: provider.id, connection: provider.connections.first { $0.enabled }?.id)]) }
            catch { issue = error.reason(or: "Couldn’t load models.") }
        }
    }

    private func delete() {
        busy = true
        Task {
            defer { busy = false }
            do {
                try await client.deleteProvider(id: providerID)
                removed(providerID)
            } catch { issue = error.reason(or: "Couldn’t delete this provider.") }
        }
    }
}

/// Names a connection and sets its key; a signed-in account is reconnected instead.
struct ConnectionForm: View {
    let client: APIClient
    let provider: Provider
    let connection: ProviderConnection?
    let onBack: () -> Void
    let reconnect: (ProviderOption) -> Void
    let saved: (Provider) -> Void
    @Environment(\.panelClose) private var close
    @Environment(ProviderStore.self) private var store: ProviderStore?
    @State private var name: String
    @State private var key = ""
    @State private var saving = false
    @State private var issue: String?

    init(client: APIClient, provider: Provider, connection: ProviderConnection?, onBack: @escaping () -> Void,
         reconnect: @escaping (ProviderOption) -> Void, saved: @escaping (Provider) -> Void) {
        self.client = client
        self.provider = provider
        self.connection = connection
        self.onBack = onBack
        self.reconnect = reconnect
        self.saved = saved
        _name = State(initialValue: connection?.name ?? "")
    }

    private var account: Bool { connection?.status == ProviderOptionAuth.oauth }
    private var valid: Bool { account || connection != nil || !key.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    var body: some View {
        PanelView(connection == nil ? "Add connection" : "Edit connection", enabled: !saving, onClose: { close() }, onBack: onBack) {
            Form {
                Section {
                    SettingsField(title: "Name", text: $name, prompt: connection == nil ? "API key" : "Enter name")
                    if account {
                        LabeledContent("Account") {
                            Button("Reconnect") {
                                if let option = store?.options?.first(where: { $0.type == provider.type && $0.auth == ProviderOptionAuth.oauth }) {
                                    reconnect(option)
                                }
                            }
                            .buttonStyle(.bordered)
                            .accessibilityLabel("Reconnect account")
                        }
                    } else {
                        SettingsField(title: "API key", text: $key, prompt: connection == nil ? "Enter API key" : "Enter a new key to replace it", secure: true)
                        if let hint = connection?.keyHint, !hint.isEmpty {
                            LabeledContent("Current key") { Text(hint).monospaced().foregroundStyle(.muted) }
                        }
                    }
                }
                .panelRow()
                if let issue { Section { Text(issue).font(.footnote).foregroundStyle(.muted) }.panelRow() }
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
        .panelDismiss(enabled: !saving, onClose: { close() })
    }

    private func save() async {
        saving = true
        defer { saving = false }
        issue = nil
        let label = name.trimmingCharacters(in: .whitespacesAndNewlines)
        let secret = key.trimmingCharacters(in: .whitespacesAndNewlines)
        do {
            var result = provider
            if let connection {
                if label != connection.name {
                    result = try await client.renameConnection(provider: provider.id, connection: connection.id, name: label)
                }
                if !secret.isEmpty {
                    result = try await client.setConnectionKey(provider: provider.id, connection: connection.id, key: secret)
                }
            } else {
                result = try await client.addConnection(provider: provider.id, key: secret, name: label.isEmpty ? nil : label)
            }
            saved(result)
            onBack()
        } catch { issue = error.isOffline ? nil : error.reason(or: "Couldn’t save this connection.") }
    }
}

/// Sets up a provider that isn't a PiG one: its name and endpoint, optionally its first key, and its models.
struct ProviderCreate: View {
    let client: APIClient
    let preset: ProviderPreset
    let onBack: () -> Void
    let created: (Provider) -> Void
    @Environment(\.panelClose) private var close
    @State private var name: String
    @State private var baseURL: String
    @State private var key = ""
    @State private var models: [ProviderModel] = []
    @State private var modelID = ""
    @State private var levels: [String]?
    @State private var saving = false
    @State private var issue: String?

    init(client: APIClient, preset: ProviderPreset, onBack: @escaping () -> Void, created: @escaping (Provider) -> Void) {
        self.client = client
        self.preset = preset
        self.onBack = onBack
        self.created = created
        _name = State(initialValue: preset.label)
        _baseURL = State(initialValue: preset.baseURL)
    }

    private var valid: Bool {
        !name.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty && URL(string: baseURL.trimmingCharacters(in: .whitespacesAndNewlines))?.host() != nil
    }

    var body: some View {
        PanelView("Add provider", enabled: !saving, onClose: { close() }, onBack: onBack) {
            Form {
                Section {
                    SettingsField(title: "Name", text: $name, prompt: "Enter name")
                    SettingsField(title: "Base URL", text: $baseURL, prompt: "https://api.example.com/v1")
                    SettingsField(title: "API key", text: $key, prompt: "Optional", secure: true)
                }
                .panelRow()
                if preset != .elevenlabs {
                    Section {
                        ForEach(models, id: \.id) { model in
                            HStack {
                                Text(model.id).lineLimit(1)
                                Spacer()
                                Button("Remove", systemImage: "minus.circle") { models.removeAll { $0.id == model.id } }
                                    .labelStyle(.iconOnly).buttonStyle(.borderless).foregroundStyle(.muted)
                                    .accessibilityLabel("Remove \(model.id)")
                            }
                        }
                        HStack {
                            SettingsField(title: "Model ID", text: $modelID, prompt: "Enter model ID")
                            ReasoningLevelsMenu(levels: $levels)
                            Button("Add", action: addModel)
                                .disabled(modelID.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                        }
                    } header: {
                        Text("Models").foregroundStyle(.primary)
                    } footer: {
                        if models.isEmpty { Text("Leave empty to load them from the provider.").font(.footnote).foregroundStyle(.muted) }
                    }
                    .panelRow()
                }
                if let issue { Section { Text(issue).font(.footnote).foregroundStyle(.muted) }.panelRow() }
            }
            .formStyle(.grouped)
            .disabled(saving)
        } actions: {
            Button { Task { await create() } } label: {
                if saving { ProgressView().controlSize(.small) } else { Text("Save") }
            }
            .disabled(!valid || saving)
            .keyboardShortcut(.defaultAction)
        }
        .panelDismiss(enabled: !saving, onClose: { close() })
    }

    private func addModel() {
        let id = modelID.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !id.isEmpty else { return }
        if !models.contains(where: { $0.id == id }) { models.append(ProviderModel(id: id, tags: ["text"], enabled: true, reasoning: levels)) }
        modelID = ""
        levels = nil
    }

    private func create() async {
        saving = true
        defer { saving = false }
        issue = nil
        let id = "\(preset.rawValue)-\(UUID().uuidString.lowercased())"
        let secret = key.trimmingCharacters(in: .whitespacesAndNewlines)
        do {
            var provider = try await client.saveProvider(
                Provider(id: id, type: preset.rawValue, name: name.trimmingCharacters(in: .whitespacesAndNewlines),
                         baseURL: baseURL.trimmingCharacters(in: .whitespacesAndNewlines), enabled: true, order: 0, models: models),
                isNew: true)
            if !secret.isEmpty { provider = try await client.setConnectionKey(provider: id, connection: id, key: secret) }
            if preset != .elevenlabs, models.isEmpty {
                do { provider = try await client.discoverModels(id: id) }
                catch {
                    created(provider)
                    issue = error.reason(or: "Saved, but couldn’t load its models.")
                    return
                }
            }
            created(provider)
        } catch {
            issue = error.isOffline ? "Connection lost before setup finished." : error.reason(or: "Couldn’t finish saving this provider.")
        }
    }
}
