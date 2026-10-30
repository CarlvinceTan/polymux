import SwiftUI
import PolymuxKit

struct NewGroupView: View {
    let session: AppSession
    let close: () -> Void
    let select: (PolymuxKit.Thread) -> Void
    @State private var name = ""
    @State private var query = ""
    @State private var selected: [String] = []
    @State private var creating = false
    @State private var adding = false
    @State private var focused = false
    @State private var recipientHeight: CGFloat = 28

    var body: some View {
        Group {
            if adding {
                NewBotView(session: session, close: { adding = false }) { _ in adding = false }
            } else { picker }
        }
        .onChange(of: session.sidebar.agents.map(\.id)) { _, ids in
            selected.removeAll { !ids.contains($0) }
        }
        .keepDraft(Self.draftKey, value: Binding(get: { GroupDraft(name: name, selected: selected) },
                                                 set: { name = $0.name; selected = $0.selected }),
                   cache: session.sidebar.cacheActor)
    }

    private var picker: some View {
        PanelView("New Group Chat", enabled: !creating, onClose: cancel,
                  header: AnyView(header)) {
            ScrollView {
                LazyVStack(spacing: 0) {
                    if contacts.isEmpty {
                        Text(emptyMessage)
                            .foregroundStyle(.muted)
                            .multilineTextAlignment(.center)
                            .frame(maxWidth: .infinity, minHeight: 320)
                    }
                    ForEach(contacts) { agent in
                        member(agent)
                            .padding(rowInset)
                        if agent.id != contacts.last?.id { Divider().padding(.leading, 70) }
                    }
                }
                .padding(.horizontal, 8)
                .frame(minHeight: 440, alignment: .top)
            }
            .disabled(creating)
        } actions: {
            if session.sidebar.agents.count < 2 {
                Button("New Bot", systemImage: "plus") { adding = true }
                    .disabled(creating)
            }
            if let message {
                Text(message)
                    .font(.caption).foregroundStyle(.muted).lineLimit(2)
                    .frame(maxWidth: .infinity, alignment: .leading)
            }
            Button { Task { await create() } } label: {
                if creating { ProgressView().controlSize(.small) }
                else { Text("Create") }
            }
            .disabled(!validMembers || !validName || creating)
            .keyboardShortcut(.defaultAction)
            .help("Choose between two and sixteen bots")
        }
        .interactiveDismissDisabled(creating)
        .panelDismiss(enabled: !creating, onClose: cancel)
        .task { focused = true }
    }

    private static let draftKey = "draft/new-group"

    private func cancel() {
        KeptDraft<GroupDraft>.discard(Self.draftKey, cache: session.sidebar.cacheActor)
        close()
    }

    private var header: some View {
        VStack(spacing: 12) {
            HStack(alignment: .top, spacing: 8) {
                Text("To:").foregroundStyle(.muted).frame(height: 28)
                ScrollViewReader { proxy in
                    ScrollView(.vertical) {
                        RecipientFlow {
                            ForEach(recipients) { agent in
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
                                .accessibilityLabel("Remove \(memberName(agent))")
                            }
                            RecipientField(text: $query, focused: $focused,
                                           placeholder: selected.isEmpty ? "Add bot" : "",
                                           onSubmit: chooseFirst, onDelete: removeLast)
                                .frame(minWidth: 80, minHeight: 28)
                                .accessibilityLabel("To")
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
            if selected.count > 1 {
                TextField("Chat name (optional)", text: $name)
                    .accessibilityLabel("Chat name (optional)")
                    .textFieldStyle(.plain)
                    .autocorrectionDisabled()
                    .padding(.horizontal, 14).padding(.vertical, 10)
                    .frame(minHeight: 44)
                    .glassEffect(.regular, in: Capsule())
            }
        }
        .disabled(creating)
    }

    private func member(_ agent: Agent) -> some View {
        Button { choose(agent) } label: {
            HStack(spacing: 16) {
                AgentAvatar(agentID: agent.id, showsMarker: false)
                    .frame(width: 52, height: 52)
                    .accessibilityHidden(true)
                VStack(alignment: .leading, spacing: 3) {
                    Text(memberName(agent)).lineLimit(1)
                    if !agent.role.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
                        Text(agent.role).font(.callout).foregroundStyle(.muted).lineLimit(2)
                    }
                }
            }
                .padding(.vertical, 4)
                .frame(maxWidth: .infinity, alignment: .leading)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .disabled(selected.count >= 16)
        .accessibilityLabel("Select \(memberName(agent))")
    }

    private var cleanName: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var validName: Bool { cleanName.utf8.count <= 128 }
    private var validMembers: Bool {
        (2...16).contains(selected.count)
            && selected.allSatisfy { id in session.sidebar.agents.contains { $0.id == id } }
    }
    private var message: String? {
        if !validName { return "Choose a shorter group name." }
        if selected.count < 2 { return "Choose at least two bots." }
        return nil
    }

    private var contacts: [Agent] {
        let term = query.trimmingCharacters(in: .whitespacesAndNewlines)
        return session.sidebar.recentFirst(session.sidebar.agents.filter { agent in
            !selected.contains(agent.id)
                && (term.isEmpty || agent.name.localizedStandardContains(term) || agent.role.localizedStandardContains(term))
        })
    }

    private var recipients: [Agent] {
        selected.compactMap { id in session.sidebar.agents.first { $0.id == id } }
    }

    private var emptyMessage: String {
        if !query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty { return "No matching bots." }
        return session.sidebar.agents.isEmpty ? "No bots yet." : "No available bots."
    }

    private var rowInset: EdgeInsets {
        #if os(macOS)
        EdgeInsets(top: 0, leading: 8, bottom: 0, trailing: 8)
        #else
        EdgeInsets(top: 4, leading: 16, bottom: 4, trailing: 16)
        #endif
    }

    private func choose(_ agent: Agent) {
        guard !creating, selected.count < 16, !selected.contains(agent.id) else { return }
        selected.append(agent.id)
        query = ""
        focused = true
    }

    private func chooseFirst() {
        guard !creating else { return }
        if query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty {
            if validMembers && validName { Task { await create() } }
        } else if let first = contacts.first { choose(first) }
    }

    private func remove(_ id: String) {
        guard !creating else { return }
        selected.removeAll { $0 == id }
        focused = true
    }

    private func removeLast() {
        if let last = selected.last { remove(last) }
    }

    private func memberName(_ agent: Agent) -> String {
        session.sidebar.agents.filter { $0.name == agent.name }.count > 1
            ? "\(agent.name) · \(agent.id.prefix(6))" : agent.name
    }

    private func create() async {
        guard !creating, validMembers, validName else { return }
        creating = true
        let draft = GroupDraft(name: name, selected: selected)
        let thread = session.sidebar.createThread(agentIDs: selected, title: cleanName.isEmpty ? nil : cleanName) {
            KeptDraft<GroupDraft>.discard(Self.draftKey, matching: draft, cache: session.sidebar.cacheActor)
        }
        select(thread)
    }
}

private struct GroupDraft: Codable, Equatable, Sendable {
    var name: String
    var selected: [String]
}
