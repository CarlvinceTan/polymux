import SwiftUI
import PolymuxKit

struct GroupEditor: View {
    let thread: PolymuxKit.Thread
    let session: AppSession
    @Environment(\.panelClose) private var dismiss
    @State private var name: String
    @State private var selected: [String]
    @State private var lead: String
    @State private var query = ""
    @State private var picking = false
    @State private var profiling: Agent?
    @State private var hovered: String?
    @State private var hoveredRemove: String?
    @FocusState private var nameFocused: Bool
    @FocusState private var searchFocused: Bool

    init(thread: PolymuxKit.Thread, session: AppSession) {
        self.thread = thread
        self.session = session
        _name = State(initialValue: thread.title)
        _selected = State(initialValue: thread.participantIDs)
        _lead = State(initialValue: thread.leadAgentID ?? thread.agentID)
    }

    var body: some View {
        PanelView(picking ? "Add agents" : "Edit chat", enabled: profiling == nil, onClose: { dismiss() },
                  onBack: picking ? { finishPicker() } : nil,
                  header: picking ? AnyView(pickerHeader) : nil) {
            if picking { picker }
            else { profile }
        } actions: {
            if picking {
                Button("Done", action: finishPicker)
            }
        }
        .panel(item: $profiling) { agent in
            ProfileView(agent: agent, session: session)
        }
        .panelDismiss(enabled: profiling == nil, onClose: { dismiss() })
        .onChange(of: name) { _, _ in save(delay: .milliseconds(550)) }
        .onChange(of: selected) { _, _ in
            if !selected.contains(lead) { lead = selected.first ?? "" }
            save(delay: .zero)
        }
        .onChange(of: lead) { _, _ in save(delay: .zero) }
    }

    private var profile: some View {
        Form {
            Section("Chat") {
                TextField("Name", text: $name)
                    .autocorrectionDisabled().focused($nameFocused)
                    .accessibilityLabel("Chat name")
                Picker("Default responder", selection: $lead) {
                    ForEach(selected, id: \.self) { id in Text(memberName(id)).tag(id) }
                }
            }
            .panelRow()
            Section {
                LazyVGrid(columns: [GridItem(.adaptive(minimum: 88, maximum: 120), spacing: 16)],
                          alignment: .center, spacing: 16) {
                    ForEach(selected, id: \.self) { id in
                        memberTile(id)
                    }
                    addTile
                }
                .padding(.vertical, 8)
            } header: {
                Text("Members")
            } footer: {
                if let hint { Text(hint).font(.footnote).foregroundStyle(.secondary) }
            }
            .panelRow()
            if let issue = session.sidebar.editIssues[thread.id] {
                Section {
                    Text(issue).foregroundStyle(.secondary).textSelection(.enabled)
                    Button("Try again") { session.sidebar.retryEdit(thread.id) }
                }
                .panelRow()
            }
        }
        .formStyle(.grouped)
    }

    private func memberTile(_ id: String) -> some View {
        let agent = session.sidebar.agents.first { $0.id == id }
        #if os(macOS)
        let showsRemove = hovered == id || hoveredRemove == id
        #else
        let showsRemove = true
        #endif
        return VStack(spacing: 4) {
            Button { profiling = agent } label: {
                AvatarView(options: session.avatars.options(for: id))
                    .frame(width: 72, height: 72)
                    .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .disabled(agent == nil)
            .accessibilityLabel("Edit \(memberName(id))’s profile")
            .accessibilityActions {
                if selected.count > 1 {
                    Button("Remove \(memberName(id)) from chat") { remove(id) }
                }
            }
            .overlay(alignment: .topTrailing) {
                Button { remove(id) } label: {
                    Image(systemName: "xmark")
                        .font(.system(size: 8, weight: .semibold))
                        .frame(width: 16, height: 16)
                        .glassEffect(.regular.interactive(selected.count > 1), in: Circle())
                        .frame(width: 44, height: 44)
                        .contentShape(Circle())
                }
                .buttonStyle(.plain)
                .offset(x: 22, y: -12)
                .disabled(selected.count == 1 || !showsRemove)
                .accessibilityLabel("Remove \(memberName(id)) from chat")
                .accessibilityHint(selected.count == 1 ? "A chat needs at least one agent" : "")
                .opacity(showsRemove ? 1 : 0)
                .allowsHitTesting(showsRemove)
                .accessibilityHidden(!showsRemove)
                .onHover { inside in
                    if inside { hoveredRemove = id }
                    else if hoveredRemove == id { hoveredRemove = nil }
                }
            }
            .onHover { inside in
                if inside { hovered = id }
                else if hovered == id { hovered = nil }
            }
            Text(memberName(id)).font(.caption).multilineTextAlignment(.center).lineLimit(2)
                .frame(maxWidth: .infinity, minHeight: 32, alignment: .top)
        }
        .frame(maxWidth: .infinity, alignment: .top)
    }

    private var addTile: some View {
        VStack(spacing: 4) {
            Button(action: openPicker) {
                Image(systemName: "plus").font(.system(size: 22, weight: .regular))
                    .frame(width: 72 * 200 / 316, height: 72 * 200 / 316)
                    .contentShape(Circle())
            }
            .buttonStyle(.plain)
            .glassEffect(.regular.interactive(selected.count < 16), in: Circle())
            .frame(width: 72, height: 72)
            .disabled(selected.count >= 16)
            .accessibilityLabel("Add agents to chat")
            Text("Add").font(.caption)
                .frame(maxWidth: .infinity, minHeight: 32, alignment: .top)
        }
        .frame(maxWidth: .infinity, alignment: .top)
    }

    private var pickerHeader: some View {
        HStack(spacing: 8) {
            HStack(spacing: 8) {
                Image(systemName: "magnifyingglass").foregroundStyle(.secondary)
                TextField("Search agents", text: $query)
                    .textFieldStyle(.plain).autocorrectionDisabled()
                    .focused($searchFocused).onSubmit(chooseFirst)
                    .accessibilityLabel("Search agents to add")
            }
            .padding(.horizontal, 14).padding(.vertical, 10)
            .frame(minHeight: 44)
            .glassEffect(.regular, in: Capsule())
        }
        .onAppear { searchFocused = true }
    }

    private var picker: some View {
        List {
            if selected.count == 16 {
                Text("A chat can have up to 16 agents.")
                    .font(.caption).foregroundStyle(.secondary).listRowSeparator(.hidden)
                    .panelRow()
            }
            ForEach(contacts) { agent in
                Button { choose(agent) } label: {
                    AgentRow(agent: agent, thread: session.sidebar.thread(for: agent),
                             duplicate: session.sidebar.agents.filter { $0.name == agent.name }.count > 1,
                              gap: 16, activity: session.sidebar.activity, connected: session.sidebar.isConnected)
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain).disabled(selected.count >= 16)
                .listRowInsets(rowInset)
                .listRowSeparator(agent.id == contacts.first?.id ? .hidden : .automatic, edges: .top)
                .listRowSeparator(agent.id == contacts.last?.id ? .hidden : .automatic, edges: .bottom)
                .alignmentGuide(.listRowSeparatorTrailing) { $0[.trailing] }
                .accessibilityLabel("Add \(memberName(agent.id)) to chat")
                .panelRow()
            }
            if contacts.isEmpty {
                Text(query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty
                     ? "All available agents are already in this chat." : "No matching agents.")
                    .foregroundStyle(.secondary).listRowSeparator(.hidden)
                    .panelRow()
            }
        }
        .listStyle(.plain).contentMargins(.horizontal, 0, for: .scrollContent)
    }

    private var rowInset: EdgeInsets {
        #if os(macOS)
        EdgeInsets(top: 0, leading: 8, bottom: 0, trailing: 8)
        #else
        EdgeInsets(top: 4, leading: 16, bottom: 4, trailing: 16)
        #endif
    }

    private var cleanName: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var validName: Bool { !cleanName.isEmpty && cleanName.utf8.count <= 128 }
    private var validMembers: Bool {
        (1...16).contains(selected.count) && Set(selected).count == selected.count
            && selected.allSatisfy { id in session.sidebar.agents.contains { $0.id == id } }
    }

    private var hint: String? {
        if cleanName.isEmpty { return "Enter a chat name." }
        if cleanName.utf8.count > 128 { return "Choose a shorter chat name." }
        if !validMembers { return "Remove unavailable agents before saving." }
        if selected.count == 16 { return "Remove an agent to add another." }
        return nil
    }

    private var contacts: [Agent] {
        let term = query.trimmingCharacters(in: .whitespacesAndNewlines)
        return session.sidebar.recentFirst(session.sidebar.agents.filter { agent in
            !selected.contains(agent.id)
                && (term.isEmpty || agent.name.localizedStandardContains(term) || agent.role.localizedStandardContains(term))
        })
    }

    private func memberName(_ id: String) -> String {
        guard let agent = session.sidebar.agents.first(where: { $0.id == id }) else {
            return "Unavailable agent · \(id.prefix(6))"
        }
        return session.sidebar.agents.filter { $0.name == agent.name }.count > 1
            ? "\(agent.name) · \(id.prefix(6))" : agent.name
    }

    private func openPicker() {
        guard selected.count < 16 else { return }
        nameFocused = false
        query = ""
        picking = true
    }

    private func finishPicker() {
        searchFocused = false
        query = ""
        picking = false
    }

    private func chooseFirst() {
        guard !query.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              let first = contacts.first else { return }
        choose(first)
    }

    private func choose(_ agent: Agent) {
        guard selected.count < 16, !selected.contains(agent.id) else { return }
        selected.append(agent.id)
        query = ""
        searchFocused = true
    }

    private func remove(_ id: String) {
        guard selected.count > 1 else { return }
        if hovered == id { hovered = nil }
        if hoveredRemove == id { hoveredRemove = nil }
        selected.removeAll { $0 == id }
    }

    private func save(delay: Duration) {
        guard validMembers else { return }
        var updated = session.sidebar.threads.first { $0.id == thread.id } ?? thread
        let title = validName ? cleanName : updated.title
        guard title != updated.title || selected != updated.participantIDs || lead != updated.leadAgentID else { return }
        updated.title = title
        updated.agentIDs = selected
        updated.leadAgentID = lead
        Task { await session.sidebar.edit(updated, delay: delay) }
    }
}
