import SwiftUI
import PolymuxKit
import AvatarKit

struct AvatarEditor: View {
    let agentID: String
    let name: String
    let client: APIClient?
    @Bindable var store: AvatarStore
    /// Set when the editor opens from another panel; it then returns there instead of closing.
    var onBack: (() -> Void)? = nil
    @Environment(\.panelClose) private var dismiss
    @State private var issue: String?
    @State private var editID = UUID()

    private var options: Binding<AvatarOptions> {
        Binding(get: { store.options(for: agentID) }, set: { store.set($0, for: agentID) })
    }

    var body: some View {
        // Opened from the profile, it keeps the profile's size so moving between them doesn't jump.
        PanelView("\(name)’s avatar", onClose: { dismiss() }, onBack: onBack, width: ProfileSize.width,
                  height: onBack == nil ? nil : ProfileSize.height) {
            ScrollView {
                VStack(spacing: 20) {
                    AvatarView(options: options.wrappedValue, playing: true, interactive: true, personality: agentID)
                        .frame(width: 120, height: 120)
                    ShapeGrid(options: options, labels: false, columns: 5, paletteInset: PanelStyle.headingInset)
                    if let issue {
                        Text(issue).font(.footnote).foregroundStyle(.muted).multilineTextAlignment(.center)
                    }
                }
                .padding(.horizontal, 24)
            }
        }
        .panelDismiss(onClose: { dismiss() })
        // Each change saves shortly after it is made.
        .task(id: options.wrappedValue) {
            try? await Task.sleep(for: .milliseconds(600))
            guard !Task.isCancelled else { return }
            await save()
        }
        .onAppear { store.beginEditing(agentID, token: editID) }
        .onDisappear {
            store.endEditing(agentID, token: editID)
            if let client { Task { await store.flush(client: client) } }
        }
    }

    private func save() async {
        guard let client else { return }
        do {
            try await store.commit(for: agentID, client: client)
            issue = nil
        } catch is CancellationError {
        } catch {
            issue = error.isOffline ? "Not saved yet." : "Couldn’t save the avatar."
        }
    }
}
