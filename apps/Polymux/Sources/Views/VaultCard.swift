import SwiftUI
import UniformTypeIdentifiers
import PolymuxKit

struct PrivateVaultCard: View {
    let request: VaultCard
    let bubble: Color
    @State private var store: VaultStore
    @State private var opened = false
    @State private var password = ""
    @State private var picking = false
    @State private var key = false
    @Environment(\.scenePhase) private var phase

    init(request: VaultCard, client: APIClient, message: Message, bubble: Color) {
        self.request = request; self.bubble = bubble
        _store = State(initialValue: VaultStore(client: client.vaultClient(), card: request, messageID: message.id))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: PanelStyle.contentInset) {
            Label(request.title, systemImage: "lock").font(.headline)
            if store.done || request.status == VaultCardStatus.done {
                Label("Completed", systemImage: "checkmark").font(.caption)
                if let result = store.imported {
                    Text("\(result.added) imported · \(result.skipped) already present").font(.caption)
                }
            } else if ![VaultCardAction.edit, VaultCardAction.import].contains(request.action) {
                Text("This card is unavailable.").font(.caption).foregroundStyle(.muted)
            } else if !opened {
                Button("Open") { opened = true; store.load() }
                    .accessibilityLabel("Open private vault card")
            } else {
                if let id = store.draft?.id {
                    let binding = Binding(get: { store.draft ?? VaultItem() }, set: {
                        if store.draft?.id == id { store.draft = $0 }
                    })
                    VaultFields(item: binding).id(id)
                } else if request.action == VaultCardAction.import, store.status != nil {
                    if store.kdbx {
                        if let name = store.importName { Text(name).font(.caption) }
                        SecureField("KDBX password", text: $password).accessibilityLabel("KDBX password")
                        HStack {
                            Button("Choose") { choose(key: true) }
                                .accessibilityLabel("Choose KDBX key file")
                            if let name = store.keyName { Text(name).font(.caption) }
                            Button("Import") {
                                let value = password; password = ""
                                store.importKDBX(password: value)
                            }
                            .disabled(password.isEmpty && !store.hasKey)
                            .accessibilityLabel("Import encrypted KDBX")
                        }
                    }
                    Button("Choose") { choose(key: false) }.accessibilityLabel("Choose private vault import")
                }
                HStack {
                    Button("Close", action: close).accessibilityLabel("Close private vault card")
                    Spacer()
                    if let draft = store.draft {
                        Button("Save") { store.save() }
                            .buttonStyle(.borderedProminent)
                            .disabled(store.busy || draft.title.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty)
                            .accessibilityLabel("Save private vault entry")
                    }
                }
            }
            if let issue = store.issue { Text(issue).font(.caption).foregroundStyle(.muted) }
        }
        .textFieldStyle(.roundedBorder)
        .buttonStyle(.bordered)
        .autocorrectionDisabled()
        #if os(iOS)
        .textInputAutocapitalization(.never)
        #endif
        .padding(PanelStyle.contentInset)
        .frame(maxWidth: BubbleStyle.width, alignment: .leading)
        .background(bubble, in: RoundedRectangle(cornerRadius: BubbleStyle.radius))
        .privacySensitive()
        .fileImporter(isPresented: $picking, allowedContentTypes: [.data]) { result in
            guard case .success(let url) = result else { return }
            if key { store.importKeyFile(url) }
            else { store.importFile(url) }
        }
        .onChange(of: request.status) { _, status in if status == VaultCardStatus.done { close() } }
        .onChange(of: phase) { _, phase in if phase != .active && !picking { close() } }
        .onDisappear { close() }
    }

    private func choose(key: Bool) { self.key = key; picking = true }
    private func close() { password = ""; opened = false; picking = false; store.close() }
}

struct VaultFields: View {
    @Binding var item: VaultItem
    @State private var saved: Set<String>
    @State private var codes: Set<String>

    init(item: Binding<VaultItem>) {
        _item = item
        _saved = State(initialValue: Set(item.wrappedValue.fields.keys))
        _codes = State(initialValue: Set(item.wrappedValue.codes.map(\.id)))
    }

    var body: some View {
        VStack(alignment: .leading, spacing: PanelStyle.contentInset) {
            PanelSection("Entry") {
                Grid(alignment: .leading, horizontalSpacing: PanelStyle.contentInset, verticalSpacing: 0) {
                    row("Title") { TextField("Title", text: $item.title).accessibilityLabel("Vault title") }
                    Divider()
                    row("Website") { TextField("Website", text: $item.url).accessibilityLabel("Vault website") }
                }
                .padding(.horizontal, PanelStyle.rowInset)
            }
            PanelSection("Details") {
                Grid(alignment: .leading, horizontalSpacing: PanelStyle.contentInset, verticalSpacing: 0) {
                    ForEach(fields, id: \.key) { field in
                        row(field.title) {
                            HStack(spacing: PanelStyle.rowInset) {
                                let text = Binding(get: { item.fields[field.key] ?? "" }, set: {
                                    item.fields[field.key] = $0
                                    if !$0.isEmpty { item.removed.removeAll { $0 == field.key } }
                                })
                                let hint = saved.contains(field.key) && !item.removed.contains(field.key) ? "Saved" : "Not set"
                                if field.key == "privateKey" {
                                    // A PEM key spans lines, which a secure field can't take.
                                    TextEditor(text: text)
                                        .font(.caption.monospaced())
                                        .scrollContentBackground(.hidden)
                                        .frame(minHeight: 72, maxHeight: 120)
                                        .padding(6)
                                        .background(.quaternary, in: RoundedRectangle(cornerRadius: 6))
                                        .overlay(alignment: .topLeading) {
                                            if text.wrappedValue.isEmpty {
                                                Text(hint).font(.caption.monospaced()).foregroundStyle(.muted)
                                                    .padding(.horizontal, 11).padding(.vertical, 6).allowsHitTesting(false)
                                            }
                                        }
                                        .accessibilityLabel(field.title)
                                } else {
                                    SecureField("", text: text, prompt: Text(hint).foregroundStyle(.muted))
                                        .accessibilityLabel(field.title)
                                }
                                if saved.contains(field.key), !item.removed.contains(field.key) {
                                    Button("Clear", systemImage: "minus.circle") {
                                        item.fields[field.key] = ""
                                        item.removed.append(field.key)
                                    }
                                    .labelStyle(.iconOnly).buttonStyle(.borderless)
                                    .accessibilityLabel("Clear \(field.title)").help("Clear \(field.title)")
                                }
                            }
                        }
                        if field.key != fields.last?.key { Divider() }
                    }
                }
                .padding(.horizontal, PanelStyle.rowInset)
            }
            if !item.codes.isEmpty || ["login", "recovery"].contains(item.kind) {
                PanelSection("Recovery codes", count: item.codes.isEmpty ? nil : item.codes.count) {
                    ForEach(item.codes.map(\.id), id: \.self) { id in
                        let code = Binding(get: { item.codes.first { $0.id == id } ?? VaultCode(id: id) }, set: { value in
                            if let index = item.codes.firstIndex(where: { $0.id == id }) { item.codes[index] = value }
                        })
                        HStack(spacing: PanelStyle.rowInset) {
                            SecureField(codes.contains(id) ? "Saved" : "Recovery code", text: code.value)
                                .accessibilityLabel("Recovery code")
                            Toggle("Used", isOn: code.used).accessibilityLabel("Recovery code used").fixedSize()
                            Button("Remove", systemImage: "minus.circle") { item.codes.removeAll { $0.id == id } }
                                .labelStyle(.iconOnly).buttonStyle(.borderless).accessibilityLabel("Remove recovery code")
                        }
                        .sectionRow()
                        Divider()
                    }
                    Button("Add code", systemImage: "plus") { item.codes.append(VaultCode(id: UUID().uuidString)) }
                        .buttonStyle(SectionButton()).accessibilityLabel("Add recovery code")
                }
            }
        }
        .textFieldStyle(.plain)
    }

    private func row<Content: View>(_ title: String, @ViewBuilder content: () -> Content) -> some View {
        GridRow(alignment: .firstTextBaseline) {
            Text(title).foregroundStyle(.muted).padding(.vertical, PanelStyle.rowPadding)
            content().frame(maxWidth: .infinity, alignment: .leading).padding(.vertical, PanelStyle.rowPadding)
        }
    }

    struct Field {
        let key: String
        let title: String
    }
    private var fields: [Field] { Self.fields(item.kind) }

    static func fields(_ kind: String) -> [Field] {
        let user = Field(key: "username", title: "Username")
        let password = Field(key: "password", title: "Password")
        let totp = Field(key: "totp", title: "TOTP seed or URI")
        let notes = Field(key: "notes", title: "Notes")
        switch kind {
        case "login": return [user, password, totp, notes]
        case "api": return [Field(key: "token", title: "Secret"), notes]
        case "card": return [Field(key: "holder", title: "Cardholder"), Field(key: "number", title: "Card number"),
                             Field(key: "expiry", title: "Expiry"), Field(key: "cvc", title: "Security code"), Field(key: "pin", title: "PIN"), notes]
        case "totp": return [user, totp, notes]
        case "note": return [notes]
        case "recovery": return [user, notes]
        case "wifi": return [Field(key: "ssid", title: "Network"), Field(key: "security", title: "Security"), user, password, notes]
        case "passkey": return [user, Field(key: "rpID", title: "Relying party"), Field(key: "credentialID", title: "Credential ID"),
                                Field(key: "userHandle", title: "User handle"), Field(key: "privateKey", title: "Private key"), notes]
        default: return []
        }
    }
}
