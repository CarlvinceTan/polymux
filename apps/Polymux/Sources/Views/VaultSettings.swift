import SwiftUI
import PolymuxKit

struct VaultSettings: View {
    @Environment(VaultLibrary.self) private var store: VaultLibrary?

    var body: some View {
        if let store { VaultBrowser(store: store) }
        else {
            Text("Vault unavailable").foregroundStyle(.muted)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
    }
}

private struct VaultBrowser: View {
    let store: VaultLibrary
    @State private var category = VaultCategory.all
    @State private var query = ""
    @State private var importing = false

    var body: some View {
        VStack(spacing: PanelStyle.contentInset) {
            if importing {
                VaultImportView(store: store, onClose: { importing = false; store.close() })
            } else if let draft = store.draft { editor(draft) }
            else {
                toolbar
                entries
                    // The count floats at the pane's foot, clear of the scrolling rows.
                    .overlay(alignment: .bottomTrailing) {
                        if !results.isEmpty {
                            Text(summary).font(.subheadline).foregroundStyle(.muted)
                                .padding(.horizontal, PanelStyle.headingInset)
                                .padding(.bottom, PanelStyle.rowInset)
                                .accessibilityLabel("Vault item count").accessibilityValue(summary)
                        }
                    }
            }
            if let imported = store.imported {
                Text("\(imported.added) imported · \(imported.skipped) already present")
                    .font(.footnote).foregroundStyle(.muted)
            }
            if !importing, let issue = store.issue ?? store.loadIssue {
                Text(issue).font(.footnote).foregroundStyle(.muted)
                    .frame(maxWidth: .infinity)
            }
        }
        // The search bar tops the pane's first row, like the settings search beside it.
        .padding(.top, 4)
        .padding(.horizontal, PanelStyle.contentInset)
        .padding(.bottom, PanelStyle.contentInset)
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .privacySensitive()
        .task {
            while !Task.isCancelled {
                await store.load()
                try? await Task.sleep(for: .seconds(5))
            }
        }
        .onChange(of: store.imported) { _, result in if result != nil { importing = false } }
        .onDisappear { store.close() }
    }

    private var toolbar: some View {
        VStack(spacing: PanelStyle.rowInset) {
            HStack(spacing: PanelStyle.rowInset) {
                HStack(spacing: PanelStyle.rowInset) {
                    PanelSearch("Search vault", text: $query)
                    if !query.isEmpty {
                        Button { query = "" } label: { Image(systemName: "xmark.circle.fill") }
                            .buttonStyle(.plain).foregroundStyle(.muted)
                            .accessibilityLabel("Clear vault search").help("Clear search")
                    }
                }
                .padding(.horizontal, PanelStyle.headingInset)
                .frame(height: 36)
                .glassEffect(.regular, in: Capsule())
                PanelMenu("Add vault item", symbol: "plus") {
                    ForEach(VaultCategory.allCases.filter { $0 != .all }) { kind in
                        Button(kind.entry, systemImage: kind.symbol) {
                            query = ""; category = .all
                            store.add(kind.rawValue)
                        }
                    }
                    Divider()
                    Button("Import", systemImage: "square.and.arrow.down") {
                        store.close(); importing = true
                    }
                }
                .frame(width: 36, height: 36)
                .glassEffect(.regular.interactive(!store.busy), in: Circle())
                .disabled(store.busy || store.status == nil)
            }
            ScrollView(.horizontal) {
                PillPicker("Vault category", selection: $category,
                           options: VaultCategory.allCases.map { ($0.title, $0) }, compact: true)
                    .padding(PanelStyle.cardInset)
            }
            .scrollIndicators(.hidden)
            .edgeFade()
        }
    }

    @ViewBuilder private var entries: some View {
        if results.isEmpty {
            Text(query.isEmpty ? "No items yet." : "No results found.")
                .foregroundStyle(.muted)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            ScrollView {
                PanelSection {
                    LazyVStack(spacing: 0) {
                        ForEach(results, id: \.id) { item in
                            Button { store.open(item.id) } label: {
                                HStack(spacing: PanelStyle.contentInset) {
                                    Image(systemName: VaultCategory(rawValue: item.kind)?.symbol ?? "lock")
                                        .font(.title3).foregroundStyle(.muted)
                                        .frame(width: PanelStyle.actionHeight, height: PanelStyle.actionHeight)
                                    VStack(alignment: .leading, spacing: PanelStyle.cardInset) {
                                        Text(item.title).font(.body).lineLimit(1)
                                        if !item.origin.isEmpty {
                                            Text(item.origin).font(.subheadline).foregroundStyle(.muted).lineLimit(1)
                                        }
                                    }
                                    Spacer(minLength: PanelStyle.rowInset)
                                    Image(systemName: "chevron.right").foregroundStyle(.muted)
                                }
                                .padding(PanelStyle.headingInset)
                                .contentShape(Rectangle())
                            }
                            .buttonStyle(.plain).accessibilityLabel(item.title).disabled(store.busy)
                        }
                    }
                }
            }
        }
    }

    private func editor(_ item: VaultItem) -> some View {
        let binding = Binding(get: { store.draft ?? VaultItem() }, set: {
            if store.draft?.id == item.id { store.draft = $0 }
        })
        return VStack(spacing: PanelStyle.contentInset) {
            HStack {
                Button("Back", systemImage: "chevron.left") { store.close() }
                Spacer()
                if item.revision > 0 {
                    Button("Delete", role: .destructive) { store.delete() }
                        .accessibilityLabel("Delete vault item")
                }
                Button("Save") { store.save() }
                    .buttonStyle(.borderedProminent)
                    .disabled(item.title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                    .accessibilityLabel("Save vault item")
            }
            .buttonStyle(.bordered)
            ScrollView { VaultFields(item: binding).id(item.id) }
        }
        .disabled(store.busy)
        .autocorrectionDisabled()
        #if os(iOS)
        .textInputAutocapitalization(.never)
        #endif
    }

    private var results: [VaultSummary] {
        let query = query.trimmingCharacters(in: .whitespacesAndNewlines)
        return store.items.filter {
            (category == .all || $0.kind == category.rawValue) &&
            (query.isEmpty || $0.title.localizedStandardContains(query) || $0.origin.localizedStandardContains(query))
        }
    }

    private var summary: String {
        let count = results.count
        let searching = !query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
        let noun = searching ? (count == 1 ? "result" : "results") : (count == 1 ? "item" : "items")
        return "\(count) \(noun)"
    }
}

enum VaultCategory: String, CaseIterable, Identifiable {
    case all, login, card, api, totp, note, recovery, wifi, passkey
    var id: String { rawValue }
    var title: String {
        switch self {
        case .all: "All"
        case .login: "Logins"
        case .card: "Cards"
        case .api: "Secrets"
        case .totp: "TOTP"
        case .note: "Notes"
        case .recovery: "Recovery codes"
        case .wifi: "Wi-Fi"
        case .passkey: "Passkeys"
        }
    }
    var entry: String {
        switch self {
        case .all: "All"
        case .login: "Login"
        case .card: "Card"
        case .api: "Secret"
        case .totp: "TOTP"
        case .note: "Note"
        case .recovery: "Recovery codes"
        case .wifi: "Wi-Fi"
        case .passkey: "Passkey"
        }
    }
    var symbol: String {
        switch self {
        case .all: "lock"
        case .login: "key"
        case .card: "creditcard"
        case .api: "curlybraces"
        case .totp: "timer"
        case .note: "note.text"
        case .recovery: "list.bullet.rectangle"
        case .wifi: "wifi"
        case .passkey: "person.badge.key"
        }
    }
}
