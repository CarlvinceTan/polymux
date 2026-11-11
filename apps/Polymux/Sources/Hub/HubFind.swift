import SwiftUI
import PolymuxKit

extension HubStore {
    /// What Hub's + offers, starting with what fits the list on show.
    var actions: [PluginAction] {
        var out = [PluginAction(title: "New chat", symbol: "plus.message") { self.panel = .chat },
                   PluginAction(title: "New list", symbol: "megaphone") { self.panel = .list },
                   PluginAction(title: "Link chats", symbol: "link") { self.panel = .link },
                   PluginAction(title: "Add account", symbol: "person.crop.circle.badge.plus") { self.panel = .accounts }]
        if source == .mail {
            out.insert(PluginAction(title: "New mail", symbol: "square.and.pencil") { Task { await self.mail.start() } }, at: 0)
        }
        return out
    }
}

/// Hub's own settings, on its page under Plugins.
struct HubPluginSettings: View {
    let open: () -> Void
    @Environment(HubStore.self) private var store: HubStore?

    var body: some View {
        if let store {
            Section {
                Button(action: open) {
                    HStack {
                        Text("Accounts").lineLimit(1).layoutPriority(1)
                        Spacer()
                        Text(count(store), format: .number).monospacedDigit().foregroundStyle(.muted)
                        Image(systemName: "chevron.right").foregroundStyle(.muted)
                    }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Accounts")
                .accessibilityValue(String(count(store)))
                SettingsPicker("Platform selector",
                               selection: Binding(get: { store.selector }, set: { value in Task { await store.setSelector(value) } }),
                               options: [("Text", HubSettingsSelector.text), ("Text and logo", HubSettingsSelector.both),
                                         ("Logo", HubSettingsSelector.logo)])
                Toggle("Incognito", isOn: Binding(get: { store.incognito }, set: { on in Task { await store.setIncognito(on) } }))
                    .accessibilityLabel("Incognito")
            } header: {
                Text("Settings").foregroundStyle(.primary)
            }
            .panelRow()
        }
    }

    private func count(_ store: HubStore) -> Int {
        store.connected.reduce(store.status?.mail.count ?? 0) { $0 + $1.accounts.count }
    }
}

/// Hub's search: chats, contacts, lists and the mail on show, in one place.
struct HubFind: View {
    @Bindable var store: HubStore
    let client: APIClient
    @Environment(\.panelClose) private var close
    @State private var query = ""

    private enum Result: Identifiable {
        case chat(HubChat), contact(HubLink), list(HubBroadcast), mail(HubMailEnvelope)

        var id: String {
            switch self {
            case let .chat(value): "chat-" + value.id
            case let .contact(value): "contact-" + value.id
            case let .list(value): "list-" + value.id
            case let .mail(value): "mail-\(value.account)-\(value.id)"
            }
        }
    }

    private func matches(_ text: String...) -> Bool {
        text.contains { $0.localizedStandardContains(query.trimmingCharacters(in: .whitespacesAndNewlines)) }
    }

    private var results: [Result] {
        guard !query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty else { return [] }
        return store.chats.filter { !$0.space && matches($0.name, $0.preview) }.map(Result.chat)
            + store.links.filter { matches($0.name) }.map(Result.contact)
            + store.broadcasts.filter { matches($0.name) }.map(Result.list)
            + store.mail.recent.filter { matches($0.from, $0.subject, $0.preview) }.map(Result.mail)
    }

    var body: some View {
        PanelView("Search", onClose: { close() }, width: PanelSize.width, height: PanelSize.maximumHeight) {
            VStack(spacing: PanelStyle.rowInset) {
                HStack { PanelSearch("Search Hub", text: $query, autofocus: true) }
                    .padding(.horizontal, PanelStyle.headingInset)
                    .frame(height: 36)
                    .glassEffect(.regular, in: Capsule())
                    .padding(.horizontal, PanelStyle.contentInset)
                if results.isEmpty {
                    Text(query.isEmpty ? " " : "No results found.").foregroundStyle(.muted)
                        .frame(maxWidth: .infinity, maxHeight: .infinity)
                } else {
                    List(results) { result in
                        Button { open(result) } label: { row(result) }
                            .buttonStyle(.plain)
                            .listRowSeparator(.hidden)
                    }
                    .listStyle(.plain).scrollContentBackground(.hidden)
                }
            }
        }
        .panelDismiss(onClose: { close() })
    }

    @ViewBuilder private func row(_ result: Result) -> some View {
        switch result {
        case let .chat(chat):
            HubRow(title: chat.name, preview: chat.preview, at: chat.at, unread: chat.unread,
                   leading: AnyView(HubAvatar(name: chat.name, uri: chat.avatar, client: client)
                    .overlay(alignment: .bottomTrailing) { PlatformMark(platform: chat.platform, size: 15) }))
                .accessibilityLabel(chat.name)
        case let .contact(link):
            HubRow(title: link.name, preview: link.members.map(\.platform).joined(separator: ", "),
                   leading: AnyView(HubAvatar(name: link.name, uri: "", client: client)))
                .accessibilityLabel(link.name)
        case let .list(list):
            HubRow(title: list.name, preview: "\(list.recipients.count) people",
                   leading: AnyView(Image(systemName: "megaphone").foregroundStyle(.muted).frame(width: 44, height: 44)))
                .accessibilityLabel(list.name)
        case let .mail(envelope):
            HubRow(title: envelope.from, preview: envelope.subject, at: envelope.at,
                   leading: AnyView(HubAvatar(name: envelope.from, uri: "", client: client)))
                .accessibilityLabel(envelope.subject)
        }
    }

    private func open(_ result: Result) {
        switch result {
        case let .chat(chat):
            store.source = .all
            store.open(chat.id)
        case let .contact(link):
            store.source = .all
            if let first = link.members.first { store.open(first.chat) }
        case let .list(list):
            store.source = .broadcasts
            store.broadcast = list.id
        case let .mail(envelope):
            store.source = .mail
            Task { await store.mail.openRecent(envelope) }
        }
        close()
    }
}
