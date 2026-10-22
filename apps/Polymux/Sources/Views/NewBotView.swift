import SwiftUI
import PolymuxKit
import AvatarKit

struct NewBotView: View {
    let session: AppSession
    let close: () -> Void
    let select: (PolymuxKit.Thread) -> Void
    @State private var name = ""
    @State private var options = Self.randomOptions()
    @State private var creating = false
    @State private var error: String?

    var body: some View {
        PanelView("New Bot", enabled: !creating, onClose: close, width: 460) {
            ScrollView {
                VStack(spacing: 20) {
                    VStack(spacing: 12) {
                        AvatarView(options: options, mood: creating ? .spawning : .idle)
                            .frame(width: 80, height: 80)
                        InlineText(text: $name, prompt: "Name", font: .title2.weight(.semibold))
                    }
                    ShapeGrid(options: $options, labels: false, columns: 5)
                    ColorGrid(options: $options, columns: 6)
                }
                .padding(.horizontal, 24)
                // Clears the fade above the actions.
                .padding(.bottom, 24)
            }
            .scrollDisabled(true)
            .disabled(creating)
        } actions: {
            if let message {
                Text(message)
                    .font(.caption).foregroundStyle(.secondary).lineLimit(2)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            Button { Task { await create() } } label: {
                if creating { ProgressView().controlSize(.small) }
                else { Text("Create") }
            }
            .disabled(!validName || creating)
            .keyboardShortcut(.defaultAction)
        }
        .interactiveDismissDisabled(creating)
        .panelDismiss(enabled: !creating, onClose: close)
    }

    private var cleanName: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var validName: Bool { cleanName.utf8.count <= 128 }
    private var message: String? { validName ? error : "Choose a shorter name." }

    private static func randomOptions() -> AvatarOptions {
        var options = AvatarOptions.default
        options.shape = ShapeID.creationShapes.randomElement() ?? .circle
        options.color = ColorID.avatarColors.randomElement() ?? .ink
        return options
    }

    private func create() async {
        guard !creating, validName else { return }
        creating = true
        defer { creating = false }
        error = nil
        do {
            let agent = try await session.client.createAgent(
                CreateAgentRequest(name: cleanName, role: "", prompt: "", avatar: options.design)
            )
            await session.sidebar.accept(agent)
            await session.sidebar.refresh()
            session.avatars.sync(agents: session.sidebar.agents)
            guard let thread = session.sidebar.thread(for: agent) else {
                error = "\(agent.name) was created, but their chat isn’t available yet."
                return
            }
            select(thread)
        } catch {
            self.error = "Couldn’t create this bot. \(error.localizedDescription)"
        }
    }
}
