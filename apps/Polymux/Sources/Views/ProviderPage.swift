import SwiftUI
import PolymuxKit

struct ProviderPage: View {
    let client: APIClient
    let type: String
    let connections: [Provider]
    let onBack: () -> Void
    let edit: (Provider) -> Void
    let connect: (ProviderOption) -> Void
    let removed: (String) -> Void
    let changed: ([Provider]) -> Void
    @Environment(\.panelClose) private var close
    @Environment(ProviderStore.self) private var store: ProviderStore?
    private var options: [ProviderOption] { (store?.options ?? []).filter { $0.type == type } }
    @State private var removing: Set<String> = []
    @State private var issues: [String: String] = [:]
    @State private var changing = false
    @State private var refreshing = false
    @State private var issue: String?
    @State private var query = ""
    @State private var removal: Provider?

    private var keys: [Provider] { connections.filter { $0.status == ProviderOptionAuth.apiKey && !removing.contains($0.id) } }
    private var accounts: [Provider] { connections.filter { $0.status != ProviderOptionAuth.apiKey && !removing.contains($0.id) } }
    private var busy: Bool { changing }
    private var models: [ProviderModel] {
        let values = connections.map { provider in
            var value = provider
            value.enabled = true
            return value
        }
        return ProviderGroup(id: type, name: type, connections: values).models
    }

    var body: some View {
        PanelView(store?.names[type] ?? connections.first?.name ?? type, onClose: { close() }, onBack: onBack) {
            Form {
                if !connections.isEmpty {
                    Section {
                        Toggle("Enabled", isOn: Binding(get: { connections.contains(where: \.enabled) }, set: { update(enabled: $0) }))
                            .accessibilityLabel("Enabled")
                    }
                    .disabled(busy)
                    .panelRow()
                }
                Section {
                    if models.isEmpty {
                        Text("No models yet.").frame(maxWidth: .infinity)
                    } else {
                        PanelSearch("Search models", text: $query, autofocus: true)
                        Toggle("All models", isOn: Binding(get: { models.allSatisfy(\.enabled) }, set: { enabled in
                            update(flags: Dictionary(uniqueKeysWithValues: models.map { ($0.id, enabled) }))
                        }))
                        .accessibilityLabel("All models")
                        ForEach(models, id: \.id) { model in
                            if query.isEmpty || model.id.localizedStandardContains(query) {
                                Toggle(model.id, isOn: Binding(get: { model.enabled }, set: { update(flags: [model.id: $0]) }))
                                    .accessibilityLabel(model.id)
                            }
                        }
                        if !query.isEmpty && !models.contains(where: { $0.id.localizedStandardContains(query) }) {
                            Text("No models found.").frame(maxWidth: .infinity)
                        }
                    }
                    if let issue { Text(issue).font(.footnote).foregroundStyle(.muted) }
                } header: {
                    HStack {
                        Text("Models").foregroundStyle(.primary)
                        Spacer()
                        Button("Refresh", systemImage: "arrow.clockwise", action: refresh)
                            .buttonStyle(.borderless).labelStyle(.iconOnly)
                            .accessibilityLabel("Refresh models").help("Refresh models")
                            .disabled(busy || refreshing || connections.isEmpty)
                    }
                }
                .disabled(busy)
                .panelRow()
                if !keys.isEmpty || options.contains(where: { $0.auth == ProviderOptionAuth.apiKey }) {
                    Section {
                        if keys.isEmpty { Text("No API keys yet.").frame(maxWidth: .infinity) }
                        ForEach(keys) { connection in row(connection) }
                    } header: {
                        heading("API keys", auth: ProviderOptionAuth.apiKey)
                    }
                    .panelRow()
                }
                if !accounts.isEmpty || options.contains(where: { $0.auth == ProviderOptionAuth.oauth }) {
                    Section {
                        if accounts.isEmpty { Text("No accounts yet.").frame(maxWidth: .infinity) }
                        ForEach(accounts) { connection in row(connection) }
                    } header: {
                        heading("Accounts", auth: ProviderOptionAuth.oauth)
                    }
                    .panelRow()
                }
            }
            .formStyle(.grouped)
        }
        .panelDismiss(onClose: { close() })
        .task { await store?.refreshOptions() }
        .alert("Remove connection?", isPresented: Binding(get: { removal != nil }, set: { if !$0 { removal = nil } }),
               presenting: removal) { connection in
            Button("Remove", role: .destructive) { remove(connection) }
            Button("Cancel", role: .cancel) {}
        } message: { connection in
            Text("This removes \(connection.name) and its saved credentials from this server.")
        }
    }

    private func heading(_ title: String, auth: String) -> some View {
        HStack {
            Text(title).foregroundStyle(.primary)
            Spacer()
            if let option = options.first(where: { $0.auth == auth }) {
                Button(auth == ProviderOptionAuth.apiKey ? "Add" : "Sign in", systemImage: "plus") { connect(option) }
                    .buttonStyle(.borderless)
                    .labelStyle(.iconOnly)
                    .accessibilityLabel(auth == ProviderOptionAuth.apiKey ? "Add API key" : "Add account")
                    .help(auth == ProviderOptionAuth.apiKey ? "Add API key" : "Add account")
                    .disabled(busy)
            }
        }
    }

    private func row(_ connection: Provider) -> some View {
        VStack(alignment: .leading, spacing: PanelStyle.rowPadding) {
            ViewThatFits(in: .horizontal) {
                HStack {
                    identity(connection)
                    Spacer(minLength: PanelStyle.rowInset)
                    controls(connection)
                }
                VStack(alignment: .leading, spacing: PanelStyle.rowPadding) {
                    identity(connection)
                    controls(connection)
                }
            }
            if let issue = issues[connection.id] {
                Text(issue).font(.footnote).foregroundStyle(.muted)
            }
        }
    }

    private func identity(_ connection: Provider) -> some View {
        VStack(alignment: .leading, spacing: PanelStyle.cardInset) {
            Text(connection.name).lineLimit(1)
            if connection.status == ProviderOptionAuth.apiKey {
                Text(connection.keyHint ?? "••••••••").font(.caption.monospaced()).foregroundStyle(.muted).lineLimit(1)
            }
        }
    }

    private func controls(_ connection: Provider) -> some View {
        HStack {
            Button("Edit") { edit(connection) }.accessibilityLabel("Edit \(connection.name)")
            Button("Remove", role: .destructive) { removal = connection }.accessibilityLabel("Remove \(connection.name)")
        }
        .buttonStyle(.bordered)
        .disabled(busy)
    }

    private func remove(_ connection: Provider) {
        guard removing.insert(connection.id).inserted else { return }
        issues[connection.id] = nil
        store?.begin()
        store?.remove(connection.id)
        Task {
            defer { store?.end() }
            do {
                try await client.deleteProvider(id: connection.id)
                removed(connection.id)
                removing.remove(connection.id)
            } catch {
                store?.merge([connection])
                removing.remove(connection.id)
                issues[connection.id] = error.reason(or: "Couldn’t remove this connection.")
            }
        }
    }

    private func update(enabled: Bool? = nil, flags: [String: Bool]? = nil) {
        guard !busy else { return }
        let previous = connections
        let values = connections.map { provider in
            var value = provider
            if let enabled { value.enabled = enabled }
            for index in value.models.indices {
                if let flag = flags?[value.models[index].id] { value.models[index].enabled = flag }
            }
            return value
        }
        changing = true
        issue = nil
        store?.begin()
        changed(values)
        Task {
            defer { changing = false; store?.end() }
            do {
                changed(try await client.updateProvider(type: type, enabled: enabled, models: flags))
            } catch {
                changed(previous)
                issue = error.reason(or: "Couldn’t save provider settings.")
            }
        }
    }

    private func refresh() {
        guard !busy, !refreshing else { return }
        refreshing = true
        issue = nil
        let previous = connections
        store?.begin()
        Task {
            defer { refreshing = false; store?.end() }
            await withTaskGroup(of: (Provider, Provider?).self) { group in
                for connection in previous {
                    group.addTask { (connection, try? await client.discoverModels(id: connection.id)) }
                }
                for await (previous, updated) in group {
                    if let updated, store?.providers?.first(where: { $0.id == previous.id }) == previous {
                        changed([updated])
                    }
                }
            }
        }
    }
}
