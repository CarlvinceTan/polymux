import SwiftUI
import PolymuxKit
import AvatarKit

struct NewBotView: View {
    let session: AppSession
    var sectionID: String? = nil
    let close: () -> Void
    let select: (PolymuxKit.Thread) -> Void
    @State private var name = ""
    @State private var options = Self.randomOptions()
    @State private var creating = false
    @FocusState private var focused: Bool

    var body: some View {
        PanelView("New Bot", enabled: !creating, onClose: cancel, width: 460) {
            ScrollView {
                VStack(spacing: 12) {
                    VStack(spacing: 12) {
                        AvatarView(options: options, playing: creating, mood: creating ? .spawning : .idle)
                            .frame(width: 80, height: 80)
                        TextField("Name (optional)", text: $name, prompt: Text("Name (optional)").foregroundStyle(.muted))
                            .textFieldStyle(.roundedBorder).accessibilityLabel("Name")
                            .focused($focused).frame(maxWidth: 260)
                            #if os(macOS)
                            .background { SearchFocus(prompt: "Name (optional)", autofocus: true).frame(width: 0, height: 0) }
                            #endif
                    }
                    ShapeGrid(options: $options, labels: false, columns: 5, size: 40, colorSize: 28, rowSpacing: 4)
                    LabeledContent("Model") {
                        Text(session.settings.value?.defaultModel.isEmpty == false
                             ? ModelName(model: session.settings.value?.defaultModel ?? "").title : "Server default")
                            .foregroundStyle(.muted).lineLimit(1)
                    }
                    .font(.subheadline)
                }
                .padding(.horizontal, 24)
            }
            .disabled(creating)
        } actions: {
            if let message {
                Text(message)
                    .font(.caption).foregroundStyle(.muted).lineLimit(2)
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
        .panelDismiss(enabled: !creating, onClose: cancel)
        .task { focused = true }
        .keepDraft(Self.draftKey, value: Binding(get: { BotDraft(name: name, options: options) },
                                                 set: { name = $0.name; options = $0.options }),
                   cache: session.sidebar.cacheActor)
    }

    private static let draftKey = "draft/new-bot"

    private func cancel() {
        KeptDraft<BotDraft>.discard(Self.draftKey, cache: session.sidebar.cacheActor)
        close()
    }

    private var cleanName: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var validName: Bool { cleanName.utf8.count <= 128 }
    private var message: String? {
        if cleanName.utf8.count > 128 { return "Choose a shorter name." }
        return nil
    }

    private static func randomOptions() -> AvatarOptions {
        var options = AvatarOptions.default
        options.shape = ShapeCategory.shapes.shapes.randomElement() ?? .circle
        options.color = ColorID.avatarColors.randomElement() ?? .ink
        return options
    }

    private func create() async {
        guard !creating, validName else { return }
        creating = true
        let request = CreateAgentRequest(name: cleanName, role: "", prompt: "", avatar: options.design, sectionID: sectionID)
        let draft = BotDraft(name: name, options: options)
        let thread = session.sidebar.createAgent(request) {
            KeptDraft<BotDraft>.discard(Self.draftKey, matching: draft, cache: session.sidebar.cacheActor)
            Task { await session.layout.refresh() }
        }
        session.avatars.sync(agents: session.sidebar.agents)
        select(thread)
    }
}

private struct BotDraft: Codable, Equatable, Sendable {
    var name: String
    var options: AvatarOptions
}
