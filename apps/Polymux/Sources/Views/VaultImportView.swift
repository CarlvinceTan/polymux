import SwiftUI
import UniformTypeIdentifiers
import PolymuxKit

struct VaultImportView: View {
    let store: VaultLibrary
    let onClose: () -> Void
    @State private var password = ""
    @State private var picking = false
    @State private var key = false

    var body: some View {
        VStack(spacing: PanelStyle.contentInset) {
            HStack {
                Button("Back", systemImage: "chevron.left", action: onClose)
                    .accessibilityLabel("Close vault import")
                Spacer()
                Text("Import").font(.headline)
            }
            .buttonStyle(.bordered)
            VStack(spacing: PanelStyle.contentInset) {
                PanelSection {
                    LabeledContent("File") {
                        Text(store.importName ?? "CSV, JSON or KDBX").lineLimit(1)
                        Button("Choose") { choose(key: false) }
                            .accessibilityLabel("Choose vault import")
                    }
                    .sectionRow()
                    if store.kdbx {
                        Divider()
                        SecureField("KDBX password", text: $password)
                            .accessibilityLabel("KDBX password").sectionRow()
                        Divider()
                        LabeledContent("Key file") {
                            if let name = store.keyName { Text(name).lineLimit(1) }
                            Button(store.hasKey ? "Change" : "Choose") { choose(key: true) }
                                .accessibilityLabel("Choose KDBX key file")
                        }
                        .sectionRow()
                    }
                }
                if store.kdbx {
                    Button("Import") {
                        let value = password
                        password = ""
                        store.importKDBX(value)
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(password.isEmpty && !store.hasKey)
                    .accessibilityLabel("Import encrypted KDBX")
                }
                if let issue = store.issue {
                    Text(issue).font(.footnote).foregroundStyle(.muted)
                }
            }
            .buttonStyle(.bordered)
            .textFieldStyle(.roundedBorder)
            .padding(PanelStyle.contentInset)
            .disabled(store.busy)
        }
        .privacySensitive()
        .fileImporter(isPresented: $picking, allowedContentTypes: [.data]) { result in
            guard case .success(let url) = result else { return }
            if key { store.importKey(url) }
            else { store.importFile(url) }
        }
        .onDisappear {
            password = ""
            if !picking { store.close() }
        }
    }

    private func choose(key: Bool) { self.key = key; picking = true }
}
