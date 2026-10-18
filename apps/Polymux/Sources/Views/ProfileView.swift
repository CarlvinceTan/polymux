import SwiftUI
import PolymuxKit

struct ProfileView: View {
    let agent: Agent
    let session: AppSession
    var threadID: String? = nil
    var attachments: [Attachment] = []
    var jump: (String) -> Void = { _ in }
    @State private var tab = ProfileTab.instructions
    @State private var query = ""
    @Environment(\.panelClose) private var dismiss
    @State private var name: String
    @State private var role: String
    @State private var prompt = ""
    @State private var loaded = false
    @State private var loading = true
    @State private var saving = false
    @State private var issue: String?
    @State private var showAvatar = false
    @State private var avatarHovered = false
    @State private var promptHeight: CGFloat = 72
    @State private var reviews: Proactivity?
    @State private var routines: [Routine] = []
    @State private var scheduleBusy = false

    init(agent: Agent, session: AppSession, threadID: String? = nil,
         attachments: [Attachment] = [], jump: @escaping (String) -> Void = { _ in }) {
        self.agent = agent
        self.session = session
        self.threadID = threadID
        self.attachments = attachments
        self.jump = jump
        _name = State(initialValue: agent.name)
        _role = State(initialValue: agent.role)
    }

    private var cleanName: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }

    private var showsEdit: Bool {
        #if os(macOS)
        avatarHovered
        #else
        true
        #endif
    }

    var body: some View {
        PanelView("Profile", enabled: !saving && !showAvatar, onClose: { dismiss() }) {
            Form {
                Section {
                    VStack(spacing: 8) {
                        Button { showAvatar = true } label: {
                            AvatarView(options: session.avatars.options(for: agent.id))
                                .frame(width: 100, height: 100)
                                .overlay {
                                    Circle().fill(.black.opacity(0.45))
                                        .overlay { Image(systemName: "pencil").font(.title2.weight(.semibold)).foregroundStyle(.white) }
                                        .opacity(showsEdit ? 1 : 0)
                                        .allowsHitTesting(false)
                                }
                                .clipShape(Circle())
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel("Edit avatar")
                        .onHover { avatarHovered = $0 }
                        InlineText(text: $name, prompt: "Name", font: .title2.weight(.semibold))
                            .autocorrectionDisabled()
                        InlineText(text: $role, prompt: "Role", font: .body, tint: .secondary)
                        if threadID != nil {
                            Picker("Section", selection: $tab) {
                                ForEach(ProfileTab.allCases) { Text($0.rawValue).tag($0) }
                            }
                            .pickerStyle(.segmented).labelsHidden()
                            .padding(.top, 8)
                        }
                    }
                    .frame(maxWidth: .infinity)
                    .padding(.vertical, 8)
                    .disabled(saving)
                }
                .panelRow()
                switch tab {
                case .instructions: instructions
                case .media: gallery(media)
                case .files: gallery(files)
                case .search: search
                }
            }
            .formStyle(.grouped)
        } actions: {
            Button { Task { await save() } } label: {
                if saving { ProgressView().controlSize(.small) }
                else { Text("Save") }
            }
            .disabled(!loaded || loading || saving || cleanName.isEmpty)
        }
        .panel(isPresented: $showAvatar) {
            AvatarEditor(agentID: agent.id, name: cleanName.isEmpty ? agent.name : cleanName,
                         client: session.client, store: session.avatars)
        }
        .interactiveDismissDisabled(saving)
        .panelDismiss(enabled: !saving && !showAvatar, onClose: { dismiss() })
        .task { await load() }
    }

    @ViewBuilder private var instructions: some View {
        Section {
            roleEditor
        } header: {
            Text("Role description")
        } footer: {
            Text("Describe what this agent should do and how it should respond.")
                .font(.footnote).foregroundStyle(.secondary)
        }
        .disabled(!loaded || saving)
        .panelRow()
        if loading {
            Section { ProgressView("Loading profile…") }
                .panelRow()
        }
        if let issue {
            Section {
                Text(issue).foregroundStyle(.secondary).textSelection(.enabled)
                if !loaded {
                    Button("Try again") { Task { await load() } }
                        .disabled(loading)
                }
            }
            .panelRow()
        }
        if loaded { scheduleSection }
    }

    private var media: [Attachment] { attachments.filter { ["image", "video", "audio"].contains($0.kind) } }
    private var files: [Attachment] { attachments.filter { !["image", "video", "audio"].contains($0.kind) } }

    @ViewBuilder private func gallery(_ items: [Attachment]) -> some View {
        Section {
            if items.isEmpty {
                Text("Nothing shared yet.").foregroundStyle(.secondary).frame(maxWidth: .infinity)
            } else {
                ForEach(items, id: \.id) { item in
                    MediaView(attachment: item, client: session.client)
                        .frame(maxWidth: .infinity, alignment: .leading)
                }
            }
        }
        .frame(minHeight: 360, alignment: items.isEmpty ? .center : .top)
        .panelRow()
    }

    @ViewBuilder private var search: some View {
        if let threadID {
            Section {
                HStack(spacing: 6) {
                    Image(systemName: "magnifyingglass").foregroundStyle(.secondary)
                    TextField("Search this conversation", text: $query).textFieldStyle(.plain)
                }
                SearchView(session: session, threadID: threadID, agentName: agent.name, query: $query,
                           close: {}) { id in
                    dismiss()
                    jump(id)
                }
                .frame(height: 360)
            }
            .panelRow()
        }
    }

    // What this agent does on its own: its review cadence and the routines it scheduled.
    @ViewBuilder private var scheduleSection: some View {
        Section {
            Toggle("Background reviews", isOn: Binding(
                get: { reviews?.enabled ?? false },
                set: { enabled in Task { await setReviews(enabled) } }))
                .disabled(reviews == nil || scheduleBusy)
            ForEach(routines, id: \.id) { routine in
                HStack(alignment: .firstTextBaseline) {
                    VStack(alignment: .leading, spacing: 2) {
                        Text(routine.prompt).lineLimit(2)
                        Text(routine.status == "pending" ? "Waiting for your approval" : nextFire(routine))
                            .font(.caption).foregroundStyle(.secondary)
                    }
                    Spacer(minLength: 8)
                    Button("Cancel", role: .destructive) { Task { await cancel(routine) } }
                        .buttonStyle(.plain)
                        .font(.caption)
                        .foregroundStyle(.secondary)
                        .disabled(scheduleBusy)
                        .accessibilityLabel("Cancel \(routine.prompt)")
                }
            }
            if routines.isEmpty && reviews == nil {
                Text("Nothing scheduled yet.").foregroundStyle(.secondary)
            }
        } header: {
            Text("Schedule")
        }
        .panelRow()
    }

    private func nextFire(_ routine: Routine) -> String {
        guard let at = routine.nextAt, let date = WireDate.parse(at) else { return "Scheduled" }
        return date.formatted(date: .abbreviated, time: .shortened)
    }

    private func setReviews(_ enabled: Bool) async {
        guard !scheduleBusy else { return }
        scheduleBusy = true
        defer { scheduleBusy = false }
        let current = reviews
        reviews = try? await session.client.setProactivity(
            agentID: agent.id,
            settings: Proactivity(enabled: enabled, intervalSeconds: current?.intervalSeconds ?? 14400))
    }

    private func cancel(_ routine: Routine) async {
        guard !scheduleBusy else { return }
        scheduleBusy = true
        defer { scheduleBusy = false }
        _ = try? await session.client.deleteRoutine(agentID: agent.id, routineID: routine.id)
        routines = (try? await session.client.routines(agentID: agent.id)) ?? routines
    }

    private var roleEditor: some View {
        TextEditor(text: $prompt)
            .font(.body)
            .scrollContentBackground(.hidden)
            .scrollIndicators(.hidden)
            .background(Color.clear)
            .frame(height: promptHeight)
            .background(alignment: .topLeading) {
                if prompt.isEmpty {
                    TextEditor(text: .constant("Add a role description…"))
                        .font(.body).foregroundStyle(.tertiary)
                        .scrollContentBackground(.hidden)
                        .scrollIndicators(.hidden)
                        .scrollDisabled(true)
                        .disabled(true)
                        .allowsHitTesting(false)
                        .accessibilityHidden(true)
                }
            }
            .background(alignment: .topLeading) {
                Text(prompt.isEmpty ? " " : prompt + "\n")
                    .font(.body)
                    .padding(.horizontal, 5).padding(.vertical, 8)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .fixedSize(horizontal: false, vertical: true)
                    .onGeometryChange(for: CGFloat.self, of: { $0.size.height }) { height in
                        promptHeight = min(220, max(72, height))
                    }
                    .hidden()
                    .allowsHitTesting(false)
                    .accessibilityHidden(true)
            }
            .accessibilityLabel("Role description")
    }

    private func load() async {
        guard !loaded else { return }
        loading = true
        issue = nil
        defer { loading = false }
        do {
            let profile = try await session.client.agentProfile(id: agent.id)
            try Task.checkCancellation()
            if name == agent.name { name = profile.agent.name }
            if role == agent.role { role = profile.agent.role }
            prompt = profile.prompt
            loaded = true
            async let schedule = session.client.proactivity(agentID: agent.id)
            async let scheduled = session.client.routines(agentID: agent.id)
            reviews = try? await schedule
            routines = (try? await scheduled) ?? []
        } catch is CancellationError {
        } catch {
            issue = "Couldn’t load this profile. \(error.localizedDescription)"
        }
    }

    private func save() async {
        guard loaded, !saving, !cleanName.isEmpty else { return }
        saving = true
        issue = nil
        defer { saving = false }
        do {
            let update = UpdateAgentRequest(name: cleanName,
                                            role: role.trimmingCharacters(in: .whitespacesAndNewlines),
                                            prompt: prompt.trimmingCharacters(in: .whitespacesAndNewlines))
            let profile = try await session.client.updateAgent(id: agent.id, request: update)
            await session.sidebar.accept(profile.agent)
            await session.sidebar.refresh()
            dismiss()
        } catch {
            issue = "Couldn’t save this profile. \(error.localizedDescription)"
        }
    }
}

enum ProfileTab: String, CaseIterable, Identifiable {
    case instructions = "Instructions", media = "Media", files = "Files", search = "Search"
    var id: String { rawValue }
}

/// Centred text that highlights on hover and edits in place when clicked.
private struct InlineText: View {
    @Binding var text: String
    let prompt: String
    let font: Font
    var tint: HierarchicalShapeStyle = .primary
    @State private var editing = false
    @State private var hovered = false
    @FocusState private var focused: Bool

    var body: some View {
        Group {
            if editing {
                TextField(prompt, text: $text)
                    .textFieldStyle(.plain)
                    .focused($focused)
                    .onSubmit { editing = false }
                    .onChange(of: focused) { _, now in if !now { editing = false } }
                    .onAppear { focused = true }
            } else {
                Button { editing = true } label: {
                    Text(text.isEmpty ? prompt : text).foregroundStyle(text.isEmpty ? HierarchicalShapeStyle.tertiary : tint)
                }
                .buttonStyle(.plain)
            }
        }
        .font(font)
        .multilineTextAlignment(.center)
        .padding(.horizontal, 10).padding(.vertical, 4)
        .background(.quaternary.opacity(hovered && !editing ? 1 : 0), in: RoundedRectangle(cornerRadius: 8))
        .fixedSize(horizontal: editing ? false : true, vertical: true)
        .frame(maxWidth: 280)
        .onHover { hovered = $0 }
        .accessibilityLabel(prompt)
    }
}
