import SwiftUI
import PolymuxKit

/// Starts a chat with one person, or a group with several on the same account.
struct HubNewChat: View {
    @Bindable var store: HubStore
    let client: APIClient
    @Environment(\.panelClose) private var close
    @State private var query = ""
    @State private var selected: [HubPerson] = []
    @State private var name = ""
    @State private var focused = false
    @State private var loading = true
    @State private var creating = false

    var body: some View {
        PanelView("New chat", enabled: !creating, onClose: { close() }, header: AnyView(header),
                  width: PanelSize.width, height: PanelSize.maximumHeight) {
            ScrollView {
                LazyVStack(spacing: 0) {
                    if rows.isEmpty { empty }
                    ForEach(rows, id: \.self) { person in
                        row(person)
                            .padding(.horizontal, PanelStyle.rowInset)
                        if person != rows.last { Divider().padding(.leading, 70) }
                    }
                }
                .padding(.horizontal, PanelStyle.rowInset)
                .frame(minHeight: 440, alignment: .top)
            }
            .disabled(creating)
        } actions: {
            if let message {
                Text(message)
                    .font(.caption).foregroundStyle(.muted).lineLimit(2)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            Button { Task { await create() } } label: {
                if creating { ProgressView().controlSize(.small) } else { Text("Create") }
            }
            .disabled(!valid || creating)
            .keyboardShortcut(.defaultAction)
        }
        .interactiveDismissDisabled(creating)
        .panelDismiss(enabled: !creating, onClose: { close() })
        .task {
            focused = true
            await store.loadPeople()
            loading = false
        }
    }

    private var header: some View {
        VStack(spacing: 12) {
            RecipientBox(items: selected, title: \.name, label: \.name, query: $query, focused: $focused,
                         placeholder: selected.isEmpty ? "Search contacts" : "", onSubmit: chooseFirst,
                         onDelete: { if let last = selected.last { remove(last) } }) { remove($0) }
            if selected.count > 1 {
                TextField("Group name", text: $name)
                    .accessibilityLabel("Group name")
                    .textFieldStyle(.plain)
                    .autocorrectionDisabled()
                    .padding(.horizontal, 14).padding(.vertical, 10)
                    .frame(minHeight: 44)
                    .glassEffect(.regular, in: Capsule())
            }
        }
        .disabled(creating)
    }

    @ViewBuilder private var empty: some View {
        Group {
            if loading && store.people.isEmpty { ProgressView().controlSize(.small) }
            else { Text(query.isEmpty ? "No contacts yet." : "No matching contacts.").foregroundStyle(.muted) }
        }
        .frame(maxWidth: .infinity, minHeight: 320)
    }

    private func row(_ person: HubPerson) -> some View {
        Button { choose(person) } label: {
            HStack(spacing: 16) {
                HubAvatar(name: person.name, uri: person.avatar, client: client, size: 44)
                    .overlay(alignment: .bottomTrailing) { PlatformMark(platform: person.platform, size: 15) }
                VStack(alignment: .leading, spacing: 3) {
                    Text(person.name).lineLimit(1)
                    Text(detail(person)).font(.callout).foregroundStyle(.muted).lineLimit(1)
                }
            }
            .padding(.vertical, 8)
            .frame(maxWidth: .infinity, alignment: .leading)
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(!joinable(person))
        .accessibilityLabel("Select \(person.name)")
        .accessibilityValue(detail(person))
    }

    /// The platform, then the account when there's more than one to tell apart.
    private func detail(_ person: HubPerson) -> String {
        let platform = store.status?.platforms.first { $0.id == person.platform }
        let name = platform?.name ?? person.platform.capitalized
        guard let accounts = platform?.accounts, accounts.count > 1,
              let account = accounts.first(where: { $0.id == person.account }) else { return name }
        return "\(name) · \(account.name)"
    }

    /// A group lives on one account, so the first choice settles which.
    private func joinable(_ person: HubPerson) -> Bool {
        guard let first = selected.first else { return true }
        return person.platform == first.platform && person.account == first.account
    }

    private var rows: [HubPerson] {
        var people = store.people.filter { !selected.contains($0) }
        if case let .platform(id) = store.source { people = people.filter { $0.platform == id } }
        let term = query.trimmingCharacters(in: .whitespacesAndNewlines)
        if !term.isEmpty { people = people.filter { $0.name.localizedStandardContains(term) } }
        return people.sorted { $0.name.localizedStandardCompare($1.name) == .orderedAscending }
    }

    private var cleanName: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var valid: Bool { selected.count == 1 || (selected.count > 1 && !cleanName.isEmpty) }

    private var message: String? {
        if let issue = store.issue { return issue }
        return selected.count > 1 && cleanName.isEmpty ? "Name this group." : nil
    }

    private func choose(_ person: HubPerson) {
        guard !creating, joinable(person) else { return }
        store.issue = nil
        selected.append(person)
        query = ""
        focused = true
    }

    private func chooseFirst() {
        if query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            if valid { Task { await create() } }
        } else if let first = rows.first(where: joinable) { choose(first) }
    }

    private func remove(_ person: HubPerson) {
        guard !creating else { return }
        selected.removeAll { $0 == person }
        focused = true
    }

    private func create() async {
        guard valid, !creating else { return }
        creating = true
        defer { creating = false }
        if await store.createChat(selected, name: cleanName) { close() }
    }
}
