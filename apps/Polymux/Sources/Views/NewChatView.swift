import SwiftUI
import PolymuxKit
import AvatarKit

struct NewChatView: View {
    let session: AppSession
    let close: () -> Void
    let select: (PolymuxKit.Thread) -> Void
    @State private var query = ""
    @State private var recipients: [Agent] = []
    @State private var title = ""
    @State private var creating = false
    @State private var starting = false
    @State private var error: String?
    @State private var focused = false
    @State private var recipientHeight: CGFloat = 28

    var body: some View {
        PanelView("New Chat", enabled: !working, onClose: close, header: AnyView(recipientBar)) {
            ScrollView {
                LazyVStack(spacing: 0) {
                    if let error {
                        Text(error).font(.caption).foregroundStyle(.secondary)
                            .padding(.vertical, 8)
                    }
                    if contacts.isEmpty {
                        Text(query.isEmpty ? "No available agents." : "No matching agents.")
                            .foregroundStyle(.secondary)
                            .multilineTextAlignment(.center)
                            .frame(maxWidth: .infinity, minHeight: 320, alignment: .center)
                    }
                    ForEach(contacts, id: \.id) { agent in
                        Button { choose(agent) } label: {
                            AgentRow(agent: agent, thread: session.sidebar.thread(for: agent),
                                     duplicate: session.sidebar.agents.filter { $0.name == agent.name }.count > 1,
                                      gap: 16, activity: session.sidebar.activity, connected: session.sidebar.isConnected)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .padding(rowInset)
                                .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        if agent.id != contacts.last?.id { Divider().padding(.leading, 70) }
                    }
                }
                .padding(.horizontal, 8)
                .frame(minHeight: 440, alignment: .top)
            }
            .scrollIndicators(.hidden)
            .disabled(working)
        } actions: {
            Button { Task { await start() } } label: {
                if starting { ProgressView().controlSize(.small) }
                else { Text("Create") }
            }
            .disabled(recipients.isEmpty || working || !validTitle)
            .keyboardShortcut(.defaultAction)
        }
        .interactiveDismissDisabled(working)
        .panelDismiss(enabled: !working, onClose: close)
        .task { focused = true }
    }

    private var recipientBar: some View {
        VStack(spacing: 12) {
            GlassEffectContainer(spacing: 0) {
                HStack(alignment: .top, spacing: 12) {
                    HStack(alignment: .top, spacing: 8) {
                        Text("To:").foregroundStyle(.secondary).frame(height: 28)
                        ScrollViewReader { proxy in
                            ScrollView(.vertical) {
                                RecipientFlow {
                                    ForEach(recipients, id: \.id) { agent in
                                        Button { remove(agent.id) } label: {
                                            HStack(spacing: 5) {
                                                Text(agent.name).lineLimit(1)
                                                Image(systemName: "xmark").font(.caption2.weight(.semibold))
                                            }
                                            .padding(.horizontal, 10).padding(.vertical, 5)
                                            .background(.quaternary, in: Capsule())
                                        }
                                        .buttonStyle(.plain)
                                        .frame(maxWidth: 170)
                                        .accessibilityLabel("Remove \(agent.name)")
                                        .disabled(working)
                                    }
                                    RecipientField(text: $query, focused: $focused,
                                                   placeholder: recipients.isEmpty ? "Name" : "",
                                                   onSubmit: { chooseFirst() }, onDelete: { removeLast() })
                                        .frame(minWidth: 80, minHeight: 28)
                                        .accessibilityLabel("To")
                                        .disabled(working)
                                        .id("recipientInput")
                                }
                                .onGeometryChange(for: CGFloat.self, of: { $0.size.height }) { recipientHeight = $0 }
                            }
                            .scrollIndicators(.hidden)
                            .frame(height: min(136, max(28, recipientHeight)))
                            .onChange(of: recipientHeight) { _, _ in
                                if focused { proxy.scrollTo("recipientInput", anchor: .bottom) }
                            }
                            .onChange(of: focused) { _, value in
                                if value { proxy.scrollTo("recipientInput", anchor: .bottom) }
                            }
                        }
                    }
                    .padding(.horizontal, 14).padding(.vertical, 8)
                    .frame(maxWidth: .infinity, minHeight: 44)
                    .glassEffect(.regular, in: .rect(cornerRadius: 22))
                    Button { Task { await create() } } label: {
                        if creating { ProgressView().controlSize(.small) }
                        else { Image(systemName: "plus").font(.system(size: 17)) }
                    }
                    .buttonStyle(.plain)
                    .foregroundStyle(.primary)
                    .frame(width: 44, height: 44)
                    .contentShape(Circle())
                    .glassEffect(.regular.interactive(!working), in: Circle())
                    .accessibilityLabel("Create new agent")
                    .disabled(working || recipients.count >= 16)
                }
            }
            if recipients.count > 1 {
                VStack(alignment: .leading, spacing: 6) {
                    TextField("Chat name (optional)", text: $title)
                        .textFieldStyle(.plain)
                        .autocorrectionDisabled()
                        .padding(.horizontal, 14).padding(.vertical, 10)
                        .frame(minHeight: 44)
                        .glassEffect(.regular, in: Capsule())
                        .disabled(working)
                    if !validTitle {
                        Text("Choose a shorter chat name.").font(.caption).foregroundStyle(.secondary)
                    }
                }
            }
        }
    }

    private var working: Bool { creating || starting }
    private var cleanTitle: String { title.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var validTitle: Bool { recipients.count < 2 || cleanTitle.utf8.count <= 128 }

    private var rowInset: EdgeInsets {
        #if os(macOS)
        EdgeInsets(top: 0, leading: 8, bottom: 0, trailing: 8)
        #else
        EdgeInsets(top: 4, leading: 16, bottom: 4, trailing: 16)
        #endif
    }

    private var contacts: [Agent] {
        let term = query.trimmingCharacters(in: .whitespacesAndNewlines)
        return session.sidebar.recentFirst(session.sidebar.agents.filter { agent in
            !recipients.contains(where: { $0.id == agent.id }) && (term.isEmpty || agent.name.localizedStandardContains(term) || agent.role.localizedStandardContains(term))
        })
    }

    private func chooseFirst() {
        guard !working else { return }
        if query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            if !recipients.isEmpty { Task { await start() } }
            return
        }
        guard let first = contacts.first else { return }
        choose(first)
    }

    private func choose(_ agent: Agent) {
        guard !working, !recipients.contains(where: { $0.id == agent.id }) else { return }
        guard recipients.count < 16 else {
            error = "A chat can have up to 16 agents."
            return
        }
        recipients.append(agent)
        query = ""
        focused = true
        error = nil
    }

    private func remove(_ id: String) {
        guard !working else { return }
        recipients.removeAll { $0.id == id }
        focused = true
    }

    private func removeLast() {
        guard !working, !recipients.isEmpty else { return }
        recipients.removeLast()
        focused = true
    }

    private func start() async {
        guard !working, !recipients.isEmpty, validTitle else { return }
        starting = true
        error = nil
        focused = false
        defer { starting = false }
        do {
            let thread: PolymuxKit.Thread
            if recipients.count == 1 {
                let agent = recipients[0]
                if session.sidebar.thread(for: agent) == nil { await session.sidebar.refresh() }
                guard let existing = session.sidebar.thread(for: agent) else {
                    error = "This agent’s chat isn’t available yet. Try again."
                    return
                }
                thread = existing
            } else {
                thread = try await session.client.createThread(agentIDs: recipients.map(\.id),
                                                                title: cleanTitle.isEmpty ? nil : cleanTitle)
                await session.sidebar.accept(thread)
                await session.sidebar.refresh()
            }
            select(thread)
        } catch {
            self.error = "Couldn’t create this chat. \(error.localizedDescription)"
        }
    }

    private func create() async {
        guard !working, recipients.count < 16 else { return }
        creating = true
        defer { creating = false }
        error = nil
        do {
            let names = ["Arlo", "Aster", "Cleo", "Echo", "Ember", "Fern", "Milo", "Nova", "Olive", "Pip", "Sage", "Wren"]
            var options = AvatarOptions.default
            options.shape = ShapeID.allCases.randomElement() ?? .circle
            options.color = ColorID.allCases.randomElement() ?? .ink
            let agent = try await session.client.createAgent(
                CreateAgentRequest(name: names.randomElement() ?? "Nova", role: "", prompt: "", avatar: options.design)
            )
            await session.sidebar.accept(agent)
            await session.sidebar.refresh()
            session.avatars.sync(agents: session.sidebar.agents)
            recipients.append(agent)
            query = ""
            focused = true
        } catch { self.error = error.localizedDescription }
    }
}

private struct RecipientFlow: Layout {
    private struct Placement {
        let point: CGPoint
        let size: CGSize
    }

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = max(80, proposal.width ?? 300)
        let rows = positions(subviews, width: width)
        let height = rows.map { $0.point.y + $0.size.height }.max() ?? 28
        return CGSize(width: width, height: height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let rows = positions(subviews, width: bounds.width)
        for (index, row) in rows.enumerated() {
            subviews[index].place(at: CGPoint(x: bounds.minX + row.point.x, y: bounds.minY + row.point.y),
                                 proposal: ProposedViewSize(row.size))
        }
    }

    private func positions(_ subviews: Subviews, width: CGFloat) -> [Placement] {
        var rows: [Placement] = []
        var x: CGFloat = 0, y: CGFloat = 0, height: CGFloat = 0
        for index in subviews.indices {
            let field = index == subviews.count - 1
            var size = subviews[index].sizeThatFits(field ? ProposedViewSize(width: max(80, width - x), height: nil) : .unspecified)
            if x > 0 && x + size.width > width {
                x = 0
                y += height + 8
                height = 0
                size = subviews[index].sizeThatFits(ProposedViewSize(width: width, height: nil))
            }
            rows.append(Placement(point: CGPoint(x: x, y: y), size: size))
            x += size.width + 8
            height = max(height, size.height)
        }
        return rows
    }
}
