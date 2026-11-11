import SwiftUI
import PolymuxKit

private enum PluginPage: String, CaseIterable {
    case installed, marketplace
    var title: String { rawValue.capitalized }
}

struct PluginSettings: View {
    let open: (String) -> Void
    let arrange: () -> Void
    @Environment(PluginStore.self) private var store: PluginStore?

    var body: some View {
        if let store { PluginBrowser(store: store, open: open, arrange: arrange) }
        else {
            Text("Plugins unavailable").foregroundStyle(.muted)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }
}

private struct PluginBrowser: View {
    let store: PluginStore
    let open: (String) -> Void
    let arrange: () -> Void
    @State private var page = PluginPage.installed
    @State private var query = ""
    @State private var confirming: MarketPlugin?

    var body: some View {
        Form {
            Section {
                PanelSearch("Search plugins", text: $query)
                switch page {
                case .installed: installedRows
                case .marketplace: marketRows
                }
                if let issue = store.issue { Text(issue).font(.footnote).foregroundStyle(.muted) }
            } header: {
                header
            }
            #if os(iOS)
            // The same tinted card as the grouped form on Mac.
            .listRowBackground(Color(uiColor: .quaternarySystemFill))
            #else
            .panelRow()
            #endif
        }
        .settingsForm()
        .task(id: page) {
            while !Task.isCancelled {
                if page == .marketplace { await store.loadMarket() }
                await store.refresh()
                try? await Task.sleep(for: .seconds(store.installed.contains { $0.state == PluginState.installing } ? 1 : 20))
            }
        }
        .alert("Install \(confirming?.title ?? "plugin")?", isPresented: Binding(
            get: { confirming != nil }, set: { if !$0 { confirming = nil } }
        ), presenting: confirming) { item in
            Button("Install") { store.install(item) }
            Button("Cancel", role: .cancel) {}
        } message: { item in
            Text("Version \(item.version) from \(item.source). It runs on your server.")
        }
    }

    private var header: some View {
        HStack {
            PillPicker("Plugin page", selection: $page,
                       options: PluginPage.allCases.map { ($0.title, $0) }, compact: true)
            Spacer()
            if page == .installed {
                Button("Arrange", systemImage: "arrow.up.arrow.down", action: arrange)
                    .accessibilityLabel("Arrange plugins")
                    .help("Arrange plugins")
                    .disabled(!arrangeable)
            }
        }
        .labelStyle(.iconOnly)
        .buttonStyle(.borderless)
    }

    private var arrangeable: Bool { store.installed.contains { !$0.surface.isEmpty } }

    private func matches(_ text: String...) -> Bool {
        let query = query.trimmingCharacters(in: .whitespacesAndNewlines)
        return query.isEmpty || text.contains { $0.localizedStandardContains(query) }
    }

    @ViewBuilder private var installedRows: some View {
        let rows = store.installed.filter { matches($0.title, $0.description) }
        if rows.isEmpty {
            placeholder(query.isEmpty ? "No plugins yet." : "No results found.")
        }
        ForEach(rows, id: \.name) { plugin in
            Button { open(plugin.name) } label: {
                HStack {
                    Label(plugin.title, systemImage: plugin.icon())
                        .labelStyle(PluginLabel()).lineLimit(1).layoutPriority(1)
                    Spacer(minLength: PanelStyle.rowInset)
                    Text(note(plugin)).foregroundStyle(.muted).lineLimit(1)
                    Image(systemName: "chevron.right").foregroundStyle(.muted)
                }
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel(plugin.title)
            .accessibilityValue(note(plugin))
        }
    }

    private func note(_ plugin: Plugin) -> String {
        if plugin.state == PluginState.installing { return "Installing" }
        if plugin.state == PluginState.failed { return plugin.issue ?? "Not working" }
        if plugin.kind == PluginKind.global { return "Global" }
        return plugin.chats.count == 1 ? "1 chat" : "\(plugin.chats.count) chats"
    }

    @ViewBuilder private var marketRows: some View {
        let rows = (store.market ?? []).filter { matches($0.title, $0.description, $0.author) }
        if let issue = store.marketIssue, rows.isEmpty {
            placeholder(issue)
        } else if store.market == nil {
            ProgressView().controlSize(.small).frame(maxWidth: .infinity)
        } else if rows.isEmpty {
            placeholder(query.isEmpty ? "No plugins listed." : "No results found.")
        }
        ForEach(rows, id: \.name) { item in
            HStack {
                Label {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(item.title).lineLimit(1)
                        Text(item.description).font(.caption).foregroundStyle(.muted).lineLimit(2)
                    }
                } icon: {
                    Image(systemName: item.symbol.isEmpty ? "puzzlepiece.extension" : item.symbol)
                }
                .labelStyle(PluginLabel())
                Spacer(minLength: PanelStyle.rowInset)
                action(item)
            }
        }
    }

    @ViewBuilder private func action(_ item: MarketPlugin) -> some View {
        let plugin = store.plugin(named: item.name)
        if plugin?.state == PluginState.installing {
            ProgressView().controlSize(.small).accessibilityLabel("Installing \(item.title)")
        } else if !item.surface.isEmpty && !store.surfaces.contains(item.surface) {
            Text("Needs update").foregroundStyle(.muted)
        } else if !item.compatible {
            Text("Unavailable").foregroundStyle(.muted)
        } else if item.installed == item.version {
            Text("Installed").foregroundStyle(.muted)
        } else {
            Button(item.installed == nil ? "Install" : "Update") { confirming = item }
                .buttonStyle(.bordered)
                .accessibilityLabel("\(item.installed == nil ? "Install" : "Update") \(item.title)")
        }
    }

    private func placeholder(_ text: String) -> some View {
        Text(text).foregroundStyle(.muted).multilineTextAlignment(.center).frame(maxWidth: .infinity)
    }
}

/// Lines titles up whatever the width of each plugin's icon.
struct PluginLabel: LabelStyle {
    func makeBody(configuration: Configuration) -> some View {
        HStack(spacing: PanelStyle.rowInset) {
            configuration.icon.frame(width: 20)
            configuration.title
        }
    }
}

/// A plugin's page in Settings, opened from its row.
struct PluginDetail: View {
    let name: String
    let onBack: () -> Void
    @Environment(PluginStore.self) private var store: PluginStore?
    @Environment(SidebarStore.self) private var sidebar: SidebarStore?
    @Environment(\.panelClose) private var close
    @State private var removing = false
    @State private var subpage = false

    var body: some View {
        if let store, let plugin = store.plugin(named: name) {
            if subpage {
                PluginSurface.subpage(plugin) { subpage = false }
            } else {
                page(plugin, store: store)
            }
        }
    }

    private func page(_ plugin: Plugin, store: PluginStore) -> some View {
        PanelView(plugin.title, onClose: { close() }, onBack: onBack, closeLabel: "Close Settings") {
            Form {
                Section {
                    if !plugin.description.isEmpty { Text(plugin.description).foregroundStyle(.muted) }
                    LabeledContent("Type", value: plugin.kind == PluginKind.both ? "Global and Scoped" : plugin.kind.capitalized)
                    LabeledContent("Version", value: plugin.version)
                    LabeledContent("Source") { Text(plugin.source).lineLimit(1).truncationMode(.middle) }
                    if let issue = plugin.issue { LabeledContent("Status", value: issue) }
                } header: {
                    Text("About").foregroundStyle(.primary)
                }
                .panelRow()
                PluginOptions(plugin: plugin, store: store)
                PluginSurface.settings(plugin) { subpage = true }
                if plugin.kind != PluginKind.global { chats(plugin, store: store) }
                Section {
                    Button("Uninstall", role: .destructive) { removing = true }
                        .buttonStyle(.borderless)
                        .frame(maxWidth: .infinity)
                        .accessibilityLabel("Uninstall \(plugin.title)")
                }
                .panelRow()
            }
            .formStyle(.grouped)
            .task(id: plugin.name) {
                await store.loadSettings(plugin.name)
                while !Task.isCancelled {
                    await store.loadTransports()
                    try? await Task.sleep(for: .seconds(5))
                }
            }
        }
        .panelDismiss(onClose: { close() })
        .alert("Uninstall \(plugin.title)?", isPresented: $removing) {
            Button("Uninstall", role: .destructive) {
                onBack()
                store.remove(plugin)
            }
            Button("Cancel", role: .cancel) {}
        } message: {
            Text("This removes \(plugin.title) and everything saved in it.")
        }
    }

    private func chats(_ plugin: Plugin, store: PluginStore) -> some View {
        Section {
            let threads = sidebar?.chats ?? []
            if threads.isEmpty {
                Text("No chats yet.").foregroundStyle(.muted).frame(maxWidth: .infinity)
            }
            ForEach(threads) { thread in
                Toggle(isOn: Binding(get: { store.plugin(named: plugin.name)?.chats.contains(thread.id) ?? false },
                                     set: { store.setActive($0, plugin: plugin, chat: thread.id) })) {
                    Text(title(thread)).lineLimit(1)
                }
            }
        } header: {
            HStack {
                Text("Chats").foregroundStyle(.primary)
                Spacer()
                Text(plugin.chats.count, format: .number).monospacedDigit().foregroundStyle(.muted)
            }
        }
        .panelRow()
    }

    private func title(_ thread: PolymuxKit.Thread) -> String {
        thread.isGroup ? thread.title : sidebar?.agents.first { $0.id == thread.agentID }?.name ?? "Chat"
    }
}

/// A plugin's declared settings, and where it publishes this server when it is a transport.
private struct PluginOptions: View {
    let plugin: Plugin
    let store: PluginStore

    private var options: [PluginSetting] { store.settings[plugin.name] ?? [] }
    private var report: PluginTransport? { store.transports.first { $0.plugin == plugin.name } }

    var body: some View {
        if let report {
            Section {
                LabeledContent("Status", value: report.state == "ready" ? "Ready" : report.message ?? "Starting")
                if let address = report.api {
                    LabeledContent("Address") { Text(address).lineLimit(1).truncationMode(.middle) }
                }
            } header: {
                Text("Connection").foregroundStyle(.primary)
            }
            .panelRow()
        }
        if !options.isEmpty {
            Section {
                ForEach(options) { option in row(option) }
            } header: {
                Text("Settings").foregroundStyle(.primary)
            }
            .panelRow()
        }
    }

    @ViewBuilder private func row(_ option: PluginSetting) -> some View {
        if option.type == "bool" {
            Toggle(option.title, isOn: Binding(
                get: { option.value == .bool(true) },
                set: { store.set(.bool($0), key: option.key, plugin: plugin.name) }))
        } else {
            PluginTextOption(title: option.title, saved: option.value.flatMap(text) ?? "") {
                store.set(.text($0), key: option.key, plugin: plugin.name)
            }
        }
    }

    private func text(_ value: PluginSettingValue) -> String? {
        if case .text(let text) = value { return text }
        return nil
    }
}

/// Edits locally and saves when the field is submitted or loses focus.
private struct PluginTextOption: View {
    let title: String
    let saved: String
    let save: (String) -> Void
    @State private var draft = ""
    @FocusState private var focused: Bool

    var body: some View {
        SettingsField(title: title, text: $draft, prompt: title)
            .focused($focused)
            .onSubmit { save(draft) }
            .onChange(of: focused) { if !focused { save(draft) } }
            .onChange(of: saved, initial: true) { if !focused { draft = saved } }
    }
}
