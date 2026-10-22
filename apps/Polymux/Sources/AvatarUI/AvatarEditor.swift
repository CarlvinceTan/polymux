import SwiftUI
import PolymuxKit

struct AvatarEditor: View {
    let agentID: String
    let name: String
    let client: APIClient?
    @Bindable var store: AvatarStore
    @Environment(\.panelClose) private var dismiss
    @State private var saving = false
    @State private var issue: String?
    @State private var editID = UUID()

    private var options: Binding<AvatarOptions> {
        Binding(get: { store.options(for: agentID) }, set: { store.set($0, for: agentID) })
    }

    var body: some View {
        PanelView("\(name)’s avatar", enabled: !saving, onClose: { dismiss() }, width: 460) {
            ScrollView {
                VStack(spacing: 20) {
                    AvatarView(options: options.wrappedValue).frame(width: 80, height: 80)
                    ShapeGrid(options: options, labels: false, columns: 5)
                    ColorGrid(options: options, columns: 6)
                }
                .padding(.horizontal, 24)
                .padding(.bottom, 24)
            }
            .scrollDisabled(true)
            .disabled(saving)
        } actions: {
            if let issue {
                Text(issue)
                    .font(.caption).foregroundStyle(.secondary).lineLimit(2)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            Button { Task { await finish() } } label: {
                if saving { ProgressView().controlSize(.small) }
                else { Text("Done") }
            }
            .disabled(saving)
        }
        .interactiveDismissDisabled(saving)
        .panelDismiss(enabled: !saving, onClose: { dismiss() })
        .onAppear { store.beginEditing(agentID, token: editID) }
        .onDisappear {
            store.endEditing(agentID, token: editID)
            if let client { Task { await store.flush(client: client) } }
        }
    }

    private func finish() async {
        guard !saving else { return }
        guard let client else { dismiss(); return }
        saving = true
        issue = nil
        defer { saving = false }
        do {
            try await store.commit(for: agentID, client: client)
            dismiss()
        } catch {
            issue = "Couldn’t save your avatar to the server. Your changes are still open here. \(error.localizedDescription)"
        }
    }
}
