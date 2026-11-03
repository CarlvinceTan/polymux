import SwiftUI
import PolymuxKit

/// Every mailbox's signatures, edited together and saved at once.
struct HubSignatures: View {
    @Bindable var store: HubStore
    let onClose: () -> Void
    let onBack: () -> Void
    /// The mailbox on show, or every mailbox.
    @State var scope: String?
    @State private var drafts: [String: HubMailSignatures] = [:]
    @State private var dirty: Set<String> = []
    @State private var selected: Key?
    @State private var saving = false

    private struct Key: Hashable {
        let account: String
        let signature: String
    }

    private var accounts: [HubMailAccount] { store.mail.accounts }

    private var rows: [(key: Key, email: String, signature: HubMailSignature)] {
        accounts.filter { scope == nil || $0.id == scope }.flatMap { account in
            (drafts[account.id]?.signatures ?? []).map { (Key(account: account.id, signature: $0.id), account.email, $0) }
        }
    }

    private var saveable: Bool {
        !dirty.isEmpty && !saving && !drafts.values.contains { $0.signatures.contains { $0.name.trimmingCharacters(in: .whitespaces).isEmpty } }
    }

    var body: some View {
        PanelView("Signatures", onClose: onClose, onBack: onBack, width: PanelSize.width, height: PanelSize.maximumHeight) {
            Form {
                Section {
                    Picker("Mailbox", selection: Binding(get: { scope }, set: choose)) {
                        Text("All signatures").tag(String?.none)
                        ForEach(accounts, id: \.id) { Text($0.email).tag(String?.some($0.id)) }
                    }
                    if rows.isEmpty {
                        Text("No signatures yet.").foregroundStyle(.muted).frame(maxWidth: .infinity)
                    }
                    ForEach(rows, id: \.key) { row in
                        Button { selected = row.key } label: {
                            HStack(spacing: PanelStyle.rowInset) {
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(row.signature.name.isEmpty ? "Untitled" : row.signature.name).lineLimit(1)
                                    Text(caption(row)).font(.caption).foregroundStyle(.muted).lineLimit(1)
                                }
                                Spacer(minLength: PanelStyle.rowInset)
                                if selected == row.key { Image(systemName: "checkmark").foregroundStyle(.muted) }
                            }
                            .contentShape(.rect)
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel(row.signature.name)
                        .accessibilityAddTraits(selected == row.key ? .isSelected : [])
                    }
                } header: {
                    HStack {
                        Text("Signatures").foregroundStyle(.primary)
                        Spacer()
                        Button("Add signature", systemImage: "plus", action: add)
                            .help("Add signature").labelStyle(.iconOnly).buttonStyle(.borderless).disabled(accounts.isEmpty)
                    }
                }
                .panelRow()
                if let selected, let row = rows.first(where: { $0.key == selected }) { editor(row.key, email: row.email, signature: row.signature).id(selected) }
                if let issue = store.issue {
                    Section { Text(issue).font(.footnote).foregroundStyle(.muted) }.panelRow()
                }
            }
            .formStyle(.grouped)
        } actions: {
            Button("Save") {
                saving = true
                Task {
                    defer { saving = false }
                    if await store.saveSignatures(drafts.filter { dirty.contains($0.key) }) { onBack() }
                }
            }
            .disabled(!saveable)
            .keyboardShortcut(.defaultAction)
        }
        .onAppear(perform: begin)
    }

    private func caption(_ row: (key: Key, email: String, signature: HubMailSignature)) -> String {
        if scope == nil { return row.email }
        return drafts[row.key.account]?.defaultSignature == row.signature.id ? "Default" : row.email
    }

    private func begin() {
        drafts = Dictionary(uniqueKeysWithValues: accounts.map { ($0.id, HubMailSignatures(signatures: $0.signatures ?? [], defaultSignature: $0.defaultSignature)) })
        selected = rows.first?.key
    }

    private func choose(_ value: String?) {
        scope = value
        selected = rows.first?.key
    }

    private func add() {
        guard let id = scope ?? selected?.account ?? accounts.first?.id else { return }
        let signature = HubMailSignature(id: UUID().uuidString, name: "New signature", text: "", html: "")
        var draft = drafts[id] ?? HubMailSignatures(signatures: [])
        draft.signatures.append(signature)
        if draft.defaultSignature == nil { draft.defaultSignature = signature.id }
        drafts[id] = draft
        dirty.insert(id)
        scope = id
        selected = Key(account: id, signature: signature.id)
    }

    private func update(_ key: Key, _ change: (inout HubMailSignature) -> Void) {
        guard let index = drafts[key.account]?.signatures.firstIndex(where: { $0.id == key.signature }) else { return }
        change(&drafts[key.account]!.signatures[index])
        dirty.insert(key.account)
    }

    private func remove(_ key: Key) {
        guard var draft = drafts[key.account] else { return }
        draft.signatures.removeAll { $0.id == key.signature }
        if draft.defaultSignature == key.signature { draft.defaultSignature = draft.signatures.first?.id }
        drafts[key.account] = draft
        dirty.insert(key.account)
        selected = rows.first?.key
    }

    private func editor(_ key: Key, email: String, signature: HubMailSignature) -> some View {
        Section {
            SettingsField(title: "Name", text: Binding(get: { signature.name }, set: { value in update(key) { $0.name = value } }), prompt: "Signature name")
            SignatureEditor(html: signature.html, text: signature.text) { html, text in
                update(key) { item in
                    item.text = text
                    item.html = text.isEmpty ? "" : html
                }
            }
            Toggle("Use by default for \(email)", isOn: Binding(get: { drafts[key.account]?.defaultSignature == key.signature }, set: { on in
                drafts[key.account]?.defaultSignature = on ? key.signature : nil
                dirty.insert(key.account)
            }))
            Button("Remove signature", role: .destructive) { remove(key) }
        } header: {
            Text("Edit").foregroundStyle(.primary)
        }
        .panelRow()
    }
}
