import SwiftUI
import PolymuxKit
import AvatarKit

struct ProfileView: View {
    let agent: Agent
    let session: AppSession
    var threadID: String? = nil
    var attachments: [Attachment] = []
    /// The chat's runs, live, for the Work tab's tasks.
    var runs: [Run] = []
    var jump: (String) -> Void = { _ in }
    var locate: (Attachment) -> Void = { _ in }
    /// Embedded beside the chat or on its own navigation page, with avatar editing and search as modals.
    var inspector = false
    var onBack: (() -> Void)? = nil
    @State private var editingAvatar = false
    @State private var searching = false
    @State private var tab: ProfileTab
    @State private var page = ProfilePage.profile
    @Environment(\.panelClose) private var dismiss
    @State private var name: String
    @State private var role: String
    /// The agent's instructions, saved with the name and role.
    @State private var prompt = ""
    @State private var expanding = false
    /// The model this agent runs on, as the server reports it.
    @State private var model: String
    @State private var provider: String
    @State private var reasoning: String
    private var providers: [Provider] { session.providers.providers ?? [] }
    private var providersLoaded: Bool { session.providers.providers != nil }
    @State private var choosingModel = false
    @State private var fallback: AgentFallback
    @State private var editingFallbacks = false
    /// The external agent this bot runs on, if it is attached to one.
    @State private var acp: AgentACP?
    @State private var editingACP = false
    @State private var modelSaving = false
    @State private var modelIssue: String?
    @State private var saved: ProfileValues
    @State private var store: ProfileStore
    @Environment(\.scenePhase) private var scenePhase
    @State private var issue: String?
    @State private var avatarHovered = false
    private var routines: [Routine] { store.routines ?? [] }
    private var configuration: AgentConfiguration? { store.configuration }
    private var connections: [MCPConnection] { store.connections ?? [] }
    private var loaded: Bool { store.agent != nil }
    /// Whether each MCP connection answered the server's probe.
    @State private var statuses: [String: Bool] = [:]
    @State private var mcpTools: [String: [String]] = [:]
    @State private var notificationNote: Bool?
    @State private var retries = 0
    /// Whether an unsaved edit kept from last time has been put back.
    @State private var restored = false

    init(agent: Agent, session: AppSession, threadID: String? = nil, attachments: [Attachment] = [],
         runs: [Run] = [], inspector: Bool = false, onBack: (() -> Void)? = nil, locate: @escaping (Attachment) -> Void = { _ in },
         jump: @escaping (String) -> Void = { _ in }) {
        self.agent = agent
        self.session = session
        self.threadID = threadID
        self.attachments = attachments
        self.runs = runs
        self.inspector = inspector
        self.onBack = onBack
        self.locate = locate
        self.jump = jump
        _tab = State(initialValue: inspector ? .work : .capabilities)
        _name = State(initialValue: agent.name)
        _model = State(initialValue: agent.model)
        _provider = State(initialValue: agent.provider ?? "")
        _reasoning = State(initialValue: agent.reasoning ?? "off")
        _fallback = State(initialValue: agent.fallbacks)
        _acp = State(initialValue: agent.acp)
        _role = State(initialValue: agent.role)
        let store = session.profile(agent.id)
        _store = State(initialValue: store)
        _prompt = State(initialValue: store.prompt ?? "")
        _saved = State(initialValue: ProfileValues(name: agent.name, role: agent.role, prompt: store.prompt ?? ""))
    }

    private var cleanName: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var cleanRole: String { role.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var cleanPrompt: String { prompt.trimmingCharacters(in: .whitespacesAndNewlines) }
    private var cleanValues: ProfileValues { ProfileValues(name: cleanName, role: cleanRole, prompt: cleanPrompt) }

    private var tabs: [ProfileTab] { threadID == nil ? [.work, .capabilities] : ProfileTab.allCases }

    private var compactTabs: Bool {
        #if os(macOS)
        inspector
        #else
        false
        #endif
    }

    var body: some View {
        Group {
            if inspector { sidePanel } else { modal }
        }
        .onChange(of: serverValues, initial: true) { _, _ in applyProfile() }
        .onChange(of: store.agent) { _, fresh in
            guard let fresh, !modelSaving else { return }
            model = fresh.model
            provider = fresh.provider ?? ""
            reasoning = fresh.reasoning ?? "off"
            fallback = fresh.fallbacks
        }
        .task(id: scenePhase == .background) {
            while !Task.isCancelled {
                await store.refresh()
                do { try await Task.sleep(for: .seconds(scenePhase == .background ? 30 : 5)) }
                catch { break }
            }
        }
        .task { await loadProviders() }
        .task { await loadStatuses() }
        .task {
            await store.restore()
            if let tools: [String: [String]] = await session.sidebar.cacheActor.snapshot("mcp-tools/" + agent.id) {
                mcpTools = tools
            }
            if let edit: ProfileValues = await session.sidebar.cacheActor.snapshot(editKey) {
                name = edit.name
                role = edit.role
                prompt = edit.prompt
            }
            restored = true
        }
        // Name, role and instructions save shortly after each edit, once the profile loads, and again after a failed save.
        .task(id: [cleanName, cleanRole, cleanPrompt, "\(retries)\(restored)\(loaded)"]) {
            guard restored else { return }
            // Kept on this device until saved, so a crash or quit doesn't lose it.
            let edit = cleanValues == saved ? nil : ProfileValues(name: name, role: role, prompt: prompt)
            await session.sidebar.cacheActor.saveSnapshot(editKey, edit)
            try? await Task.sleep(for: .milliseconds(retries == 0 ? 700 : 5000))
            guard !Task.isCancelled else { return }
            await save()
        }
        .onChange(of: agent) { _, agent in
            let update = ProfileValues(name: agent.name, role: agent.role, prompt: saved.prompt)
            let edit = ProfileValues(name: name, role: role, prompt: prompt).merging(update, saved: saved)
            name = edit.name
            role = edit.role
            prompt = edit.prompt
            saved = update
            if !modelSaving {
                model = agent.model
                provider = agent.provider ?? ""
                reasoning = agent.reasoning ?? "off"
                fallback = agent.fallbacks
            }
        }
        .task(id: choosingModel) {
            if choosingModel { await loadProviders() }
        }
        // Closing the panel or switching chats mustn't drop an edit still waiting to save.
        .onDisappear { flush() }
    }

    /// The right-hand panel on macOS, its own page on iPhone; search sits in its bar.
    private var sidePanel: some View {
        noted
            #if os(macOS)
            .panelBar(value: BarState(muted: Notifications.shared.isMuted(agent.id),
                                     notice: notificationNote, back: onBack != nil, search: threadID != nil)) {
                HStack(spacing: 8) {
                    if let onBack { PanelNavButton(symbol: "chevron.left", label: "Back", action: onBack) }
                    notificationButton
                    Spacer(minLength: 0)
                    if threadID != nil {
                        PanelNavButton(symbol: "magnifyingglass", label: "Search this chat") { searching = true }
                    }
                    if threadID != nil || onBack != nil {
                        PanelNavButton(symbol: "xmark", label: "Close side panel") { dismiss() }
                    }
                }
            }
            #else
            .navigationTitle("Profile")
            .navigationBarTitleDisplayMode(.inline)
            .toolbar {
                ToolbarItem(placement: .topBarLeading) {
                    Button(bellLabel, systemImage: bellSymbol, action: toggleBell)
                }
                ToolbarItem(placement: .primaryAction) {
                    if threadID != nil {
                        Button("Search this chat", systemImage: "magnifyingglass") { searching = true }
                    }
                }
            }
            #endif
            .panel(isPresented: $editingAvatar) {
                AvatarEditor(agentID: agent.id, name: cleanName.isEmpty ? agent.name : cleanName,
                             client: session.client, store: session.avatars)
            }
            .panel(isPresented: $searching) {
                if let threadID {
                    SearchPanel(session: session, scope: threadID, close: { searching = false }) { result in
                        searching = false
                        jump(result.messageID ?? result.threadID)
                    }
                }
            }
    }

    // One panel at a time: sub-pages replace the profile, Back returns, close leaves entirely.
    private var modal: some View {
        ZStack {
            switch page {
            case .avatar:
                AvatarEditor(agentID: agent.id, name: cleanName.isEmpty ? agent.name : cleanName,
                             client: session.client, store: session.avatars, onBack: { page = .profile })
            case .search:
                if let threadID {
                    SearchPanel(session: session, scope: threadID, onBack: { page = .profile },
                                close: { dismiss() }) { result in
                        dismiss()
                        jump(result.messageID ?? result.threadID)
                    }
                    .preference(key: PanelWidth.self, value: ProfileSize.width)
                    .preference(key: PanelHeight.self, value: ProfileSize.height)
                }
            case .profile:
                profile
            }
        }
    }

    private var profile: some View {
        PanelView("Profile", onClose: { dismiss() },
                  accessory: threadID == nil ? nil : PanelAccessory(symbol: "magnifyingglass", label: "Search this chat") { page = .search },
                  width: ProfileSize.width, height: ProfileSize.height) {
            noted
        }
        .panelDismiss(onClose: { dismiss() })
    }

    private var bellSymbol: String { Notifications.shared.isMuted(agent.id) ? "bell.slash" : "bell" }
    private var bellLabel: String { Notifications.shared.isMuted(agent.id) ? "Unmute notifications" : "Mute notifications" }

    private func toggleBell() {
        let muted = Notifications.shared.isMuted(agent.id)
        Notifications.shared.setMuted(!muted, agent: agent.id)
        notificationNote = !muted
    }

    private var notificationButton: some View {
        HStack(spacing: 8) {
            PanelNavButton(symbol: bellSymbol, label: bellLabel, action: toggleBell)
            bellNote
        }
    }

    private var bellText: String? {
        notificationNote.map { $0 ? "Notifications turned off" : "Notifications turned on" }
    }

    @ViewBuilder private var bellNote: some View {
        if let bellText {
            Text(bellText).font(.caption).foregroundStyle(.muted).lineLimit(1)
        }
    }

    /// The details, with any note floating over them so it never pushes the tabs down.
    private var noted: some View {
        #if os(macOS)
        let bell: String? = nil
        #else
        // The iPhone's toolbar has no room for the bell's note.
        let bell = inspector ? bellText : nil
        #endif
        let note = issue ?? store.cancelIssue ?? store.issue ?? bell
        return details
            .overlay(alignment: .bottom) {
                if let note { QuietNote(text: note).padding(.bottom, 12) }
            }
            .motion(Motion.fade, value: note)
            .task(id: notificationNote) {
                guard notificationNote != nil else { return }
                try? await Task.sleep(for: .seconds(1.5))
                if !Task.isCancelled { notificationNote = nil }
            }
    }

    private var details: some View {
        VStack(spacing: PanelStyle.sectionGap) {
            VStack(spacing: 8) {
                Button { if inspector { editingAvatar = true } else { page = .avatar } } label: {
                    AvatarView(options: session.avatars.options(for: agent.id))
                        .allowsHitTesting(false)
                        .frame(width: 100, height: 100)
                        .contentShape(Rectangle())
                }
                .buttonStyle(AvatarEditStyle(hovered: $avatarHovered))
                .accessibilityLabel("Edit avatar")
                .pointerHover { avatarHovered = $0 }
                InlineText(text: $name, prompt: "Name", font: .title2.weight(.semibold))
                    .autocorrectionDisabled()
                InlineText(text: $role, prompt: "Role", font: .body, tint: .secondary)
                PillPicker("Section", selection: $tab, options: tabs.map { ($0.rawValue, $0) }, compact: compactTabs)
                    .padding(.top, 8)
            }
            .frame(maxWidth: .infinity)
            .fixedSize(horizontal: false, vertical: true)
            .overlay(alignment: .topLeading) { if !inspector { notificationButton } }
            Group {
                switch tab {
                case .capabilities: capabilities
                case .work: work
                case .media: mediaGallery
                case .files: gallery(files, empty: "No files yet.")
                }
            }
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .padding(.horizontal, PanelStyle.contentInset)
        .padding(.bottom, PanelStyle.sectionGap)
    }

    private var work: some View { workSection }

    /// What shapes how the bot works: its model or external agent, instructions, skills and MCP connections.
    private var capabilities: some View {
        VStack(alignment: .leading, spacing: PanelStyle.sectionGap) {
            if acp == nil {
                PanelSection("Model", accessory: loaded ? fallbackAccessory : nil) {
                    Button { choosingModel = true } label: {
                        HStack {
                            Label {
                                ModelLabel(name: modelName)
                            } icon: {
                                Image(systemName: "cpu")
                            }
                            Spacer()
                            Image(systemName: "chevron.up.chevron.down")
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                    .sectionRow()
                    .accessibilityLabel("Model")
                    .accessibilityValue(modelName.title)
                    .disabled(!loaded || modelSaving)
                    .dropdown(isPresented: $choosingModel) {
                        ModelPicker(providers: providers, provider: provider, model: model, choose: remap)
                    }
                    Divider().padding(.horizontal, PanelStyle.rowInset)
                    ReasoningPicker(selection: Binding(get: { reasoning }, set: {
                        saveModel(UpdateAgentRequest(reasoning: $0))
                    }), inset: true, levels: providers.levels(provider: provider, model: model))
                    .disabled(!loaded || modelSaving)
                    if let modelIssue {
                        Text(modelIssue).font(.footnote).foregroundStyle(.muted)
                    } else if providersLoaded, !provider.isEmpty, !providers.contains(where: { $0.id == provider && $0.enabled }) {
                        Text("Provider unavailable. Choose another model.").font(.footnote).foregroundStyle(.muted)
                    }
                }
                .panel(isPresented: $editingFallbacks) {
                    FallbackPanel(fallback: fallback, reasoning: reasoning, providers: providers, saving: modelSaving, issue: modelIssue) {
                        saveModel(UpdateAgentRequest(fallback: $0))
                    }
                }
            }
            externalAgent
            VStack(alignment: .leading, spacing: PanelStyle.sectionGap) {
                instructions
                sectionCard("Skills", count: configuration == nil ? nil : skillNames.count, empty: skillNames.isEmpty) {
                    if configuration == nil { sectionLoad }
                    else if skillNames.isEmpty { Text("No skills.").foregroundStyle(.muted) }
                    ForEach(skillNames, id: \.self) { Label($0, systemImage: "sparkles").sectionRow() }
                }
                sectionCard("MCPs", count: store.connections == nil ? nil : activeConnections.count, empty: activeConnections.isEmpty) {
                    if store.connections == nil { sectionLoad }
                    else if activeConnections.isEmpty { Text("No MCPs.").foregroundStyle(.muted) }
                    ForEach(activeConnections, id: \.name) { connection in mcpRow(connection) }
                }
            }
            .frame(maxHeight: .infinity)
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// Edits in place; the heading's button opens the same text in a larger editor.
    private var instructions: some View {
        PanelSection("Instructions", fills: true, accessory: store.prompt == nil ? nil : expandAccessory) {
            PromptEditor(text: $prompt, hinted: store.prompt != nil)
                .disabled(store.prompt == nil)
                .overlay {
                    if store.prompt == nil, prompt.isEmpty { sectionLoad }
                }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .panel(isPresented: $expanding) { PromptPanel(text: $prompt) }
    }

    private var fallbackAccessory: PanelAccessory {
        PanelAccessory(symbol: "gearshape", label: "Fallback settings") { editingFallbacks = true }
    }

    /// Attaches the bot to an external Agent Client Protocol process, which then runs its turns.
    private var externalAgent: some View {
        PanelSection("External agent") {
            if let acp {
                HStack(spacing: PanelStyle.rowInset) {
                    Label {
                        Text(([acp.command] + (acp.args ?? [])).joined(separator: " ")).lineLimit(1)
                    } icon: {
                        Image(systemName: "bolt.horizontal")
                    }
                    Spacer(minLength: 8)
                    Button("Detach") { saveACP(nil) }
                        .buttonStyle(.plain)
                        .foregroundStyle(.muted)
                        .disabled(!loaded || modelSaving)
                        .accessibilityLabel("Detach external agent")
                }
                .sectionRow()
                .accessibilityElement(children: .contain)
                .accessibilityLabel("External agent")
                if let modelIssue {
                    Text(modelIssue).font(.footnote).foregroundStyle(.muted).sectionRow()
                }
            } else {
                Button { editingACP = true } label: {
                    Label("Attach", systemImage: "plus")
                }
                .buttonStyle(SectionButton())
                .disabled(!loaded || modelSaving)
                .accessibilityLabel("Attach external agent")
            }
        }
        .panel(isPresented: $editingACP) {
            ACPPanel(attached: acp, saving: modelSaving) { command, arguments in
                saveACP(AgentACP(command: command, args: arguments))
            }
        }
    }

    private var expandAccessory: PanelAccessory {
        PanelAccessory(symbol: "arrow.up.left.and.arrow.down.right", label: "Expand instructions") { expanding = true }
    }

    private var activeConnections: [MCPConnection] { connections.filter { $0.enabled && !$0.hidden } }

    private var sectionLoad: SectionLoad { SectionLoad(failed: store.settled, offline: !session.sidebar.isConnected) }

    private var skillNames: [String] {
        var seen: Set<String> = []
        let names = ((configuration?.bundledSkills ?? []) + (configuration?.skills ?? [])).map(Self.skillName)
        return names.filter { seen.insert($0).inserted }
    }

    private func mcpRow(_ connection: MCPConnection) -> some View {
        DisclosureGroup {
            if let tools = mcpTools[connection.name] {
                if tools.isEmpty { Text("No tools.").foregroundStyle(.muted).sectionRow() }
                ForEach(tools, id: \.self) { Label($0, systemImage: "wrench.and.screwdriver").sectionRow() }
            } else {
                Text(statuses[connection.name] == nil ? "Checking" : "Unavailable")
                    .foregroundStyle(.muted).sectionRow()
            }
        } label: {
            HStack {
                Label(connection.name, systemImage: "network").lineLimit(1)
                Spacer(minLength: 8)
                if let tools = mcpTools[connection.name] { Text(tools.count, format: .number).monospacedDigit() }
                StatusDot(state: statuses[connection.name].map { $0 ? .online : .offline } ?? .checking)
            }
        }
        .sectionRow()
    }

    /// A skill is configured by its path; its folder or file name reads as its name.
    private static func skillName(_ path: String) -> String {
        let url = URL(fileURLWithPath: path)
        let name = url.lastPathComponent == "SKILL.md"
            ? url.deletingLastPathComponent().lastPathComponent : url.deletingPathExtension().lastPathComponent
        return name.split(whereSeparator: { $0 == "-" || $0 == "_" || $0.isWhitespace })
            .map { word in word.lowercased() == "html" ? "HTML" : word.prefix(1).uppercased() + word.dropFirst() }
            .joined(separator: " ")
    }

    private var media: [Attachment] { attachments.filter { ["image", "video", "audio"].contains($0.kind) } }
    private var files: [Attachment] { attachments.filter { !["image", "video", "audio"].contains($0.kind) } }

    private var mediaGallery: some View {
        sectionCard("Media", count: media.count, label: "Media gallery", empty: media.isEmpty) {
            if media.isEmpty {
                Text("No media yet.").foregroundStyle(.muted)
            } else {
                MediaGrid(attachments: media.filter { $0.kind != AttachmentKind.audio }, client: session.client, choose: locate)
                ForEach(media.filter { $0.kind == AttachmentKind.audio }, id: \.id) {
                    MediaView(attachment: $0, client: session.client, compact: true).sectionRow()
                }
            }
        }
    }

    private func gallery(_ items: [Attachment], empty: String) -> some View {
        sectionCard("Files", count: items.count, label: "Files list", empty: items.isEmpty) {
            if items.isEmpty {
                Text(empty).foregroundStyle(.muted)
            } else {
                ForEach(items, id: \.id) { item in
                    MediaView(attachment: item, client: session.client, compact: true).sectionRow()
                }
            }
        }
    }

    private func sectionCard<Content: View>(_ title: String?, count: Int? = nil, label: String? = nil,
                                          empty: Bool = false, @ViewBuilder content: @escaping () -> Content) -> some View {
        PanelSection(title, count: count, fills: true) {
            GeometryReader { geometry in
                ScrollView {
                    LazyVStack(alignment: empty ? .center : .leading, spacing: 0, content: content)
                        .frame(maxWidth: .infinity, minHeight: geometry.size.height,
                               alignment: empty ? .center : .topLeading)
                        .multilineTextAlignment(empty ? .center : .leading)
                }
                .scrollIndicators(.hidden)
                .accessibilityLabel(label ?? title ?? "")
            }
            .clipShape(.rect(cornerRadius: PanelStyle.cardRadius))
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }

    /// What the bot does on its own (its schedule) and what it has been working on (its tasks).
    @ViewBuilder private var workSection: some View {
        VStack(alignment: .leading, spacing: PanelStyle.sectionGap) {
            sectionCard("Schedule", count: store.routines?.count, empty: routines.isEmpty) {
                if store.routines == nil { sectionLoad }
                else if routines.isEmpty { Text("No scheduled work.").foregroundStyle(.muted) }
                ForEach(routines, id: \.id) { routine in
                    HStack(alignment: .firstTextBaseline) {
                        VStack(alignment: .leading, spacing: 2) {
                            Text(routine.prompt).lineLimit(2)
                            Text(routine.status == RoutineStatus.pending ? "Waiting for your approval" : nextFire(routine))
                                .font(.caption).foregroundStyle(.muted)
                        }
                        Spacer(minLength: 8)
                        Button("Cancel", role: .destructive) { Task { await store.cancel(routine) } }
                            .buttonStyle(.plain)
                            .font(.caption)
                            .foregroundStyle(.muted)
                            .accessibilityLabel("Cancel \(routine.prompt)")
                    }
                    .sectionRow()
                }
            }
            let threads = session.sidebar.tasks(of: agent.id)
            sectionCard("Tasks", count: threads.count + tasks.count, empty: threads.isEmpty && tasks.isEmpty) {
                if threads.isEmpty && tasks.isEmpty { Text("No tasks yet.").foregroundStyle(.muted) }
                ForEach(threads) { thread in TaskRow(thread: thread).sectionRow() }
                ForEach(tasks, id: \.id) { run in TaskRow(run: run).sectionRow() }
            }
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }

    /// Delegated work only, active first and then newest.
    private var tasks: [Run] {
        runs.filter { $0.agentID == agent.id && $0.trigger == RunTrigger.child }.sorted { lhs, rhs in
            if TaskRow.active(lhs) != TaskRow.active(rhs) { return TaskRow.active(lhs) }
            return lhs.createdAt > rhs.createdAt
        }
    }

    private func nextFire(_ routine: Routine) -> String {
        guard let at = routine.nextAt, let date = WireDate.parse(at) else { return "Scheduled" }
        return date.formatted(date: .abbreviated, time: .shortened)
    }

    private var serverValues: ProfileValues? {
        guard let prompt = store.prompt else { return nil }
        let fresh = store.agent ?? agent
        return ProfileValues(name: fresh.name, role: fresh.role, prompt: prompt)
    }

    private func applyProfile() {
        guard let update = serverValues else { return }
        let edit = ProfileValues(name: name, role: role, prompt: prompt).merging(update, saved: saved)
        name = edit.name
        role = edit.role
        prompt = edit.prompt
        saved = update
    }

    /// Saves the name, role and instructions when they differ from what the server has.
    private func save() async {
        guard let update = pendingUpdate else { return }
        do {
            let profile = try await session.client.updateAgent(id: agent.id, request: update)
            saved = ProfileValues(name: profile.agent.name, role: profile.agent.role, prompt: profile.prompt)
            issue = nil
            retries = 0
            if cleanValues == saved { await session.sidebar.cacheActor.saveSnapshot(editKey, ProfileValues?.none) }
            await store.accept(profile)
            await session.sidebar.accept(profile.agent)
            await session.sidebar.refresh()
        } catch is CancellationError {
        } catch {
            issue = error.isOffline ? "Not saved yet." : error.reason(or: "Couldn’t save this change.")
            if error.isTransient { retries += 1 }
        }
    }

    private var pendingUpdate: UpdateAgentRequest? {
        guard loaded, !cleanName.isEmpty, cleanValues != saved else { return nil }
        return UpdateAgentRequest(name: cleanName, role: cleanRole, prompt: cleanPrompt)
    }

    /// Sends an unsaved edit on its own, since the view and its debounce are going away.
    private func flush() {
        guard let update = pendingUpdate else { return }
        let client = session.client, sidebar = session.sidebar, store = store, id = agent.id, key = editKey
        Task {
            guard let profile = try? await client.updateAgent(id: id, request: update) else { return }
            await sidebar.cacheActor.saveSnapshot(key, ProfileValues?.none)
            await store.accept(profile)
            await sidebar.accept(profile.agent)
        }
    }

    private var editKey: String { "profile-edit/" + agent.id }

    private var modelName: ModelName {
        let name = providers.first(where: { $0.id == provider })?.name ?? (providersLoaded && !provider.isEmpty ? "Unavailable" : "")
        return ModelName(model: model, provider: name)
    }

    private func loadProviders() async { await session.providers.refreshProviders() }

    /// Asks the server to probe each MCP connection; the dots fill in when it answers.
    private func loadStatuses() async {
        guard let list = try? await session.client.mcpStatus(agentID: agent.id), !Task.isCancelled else { return }
        statuses = Dictionary(uniqueKeysWithValues: list.map { ($0.name, $0.connected) })
        for status in list where status.connected { mcpTools[status.name] = status.tools }
        await session.sidebar.cacheActor.saveSnapshot("mcp-tools/" + agent.id, mcpTools)
    }

    private func remap(_ nextProvider: String, _ nextModel: String) {
        choosingModel = false
        saveModel(UpdateAgentRequest(provider: nextProvider, model: nextModel))
    }

    private func saveModel(_ patch: UpdateAgentRequest) {
        guard !modelSaving else { return }
        let previous = (provider, model, reasoning, fallback)
        if let value = patch.provider { provider = value }
        if let value = patch.model { model = value }
        if let value = patch.reasoning { reasoning = value }
        if let value = patch.fallback { fallback = value }
        modelSaving = true
        modelIssue = nil
        Task {
            defer { modelSaving = false }
            do {
                let profile = try await session.client.updateAgent(id: agent.id,
                    request: patch)
                provider = profile.agent.provider ?? ""
                model = profile.agent.model
                reasoning = profile.agent.reasoning ?? "off"
                fallback = profile.agent.fallbacks
                await store.accept(profile)
                await session.sidebar.accept(profile.agent)
            } catch {
                (provider, model, reasoning, fallback) = previous
                modelIssue = error.isOffline ? "Not saved. Connect before changing model settings." : error.reason(or: "Couldn’t save model settings.")
            }
        }
    }

    private func saveACP(_ next: AgentACP?) {
        guard !modelSaving else { return }
        let previous = acp
        acp = next
        modelSaving = true
        modelIssue = nil
        Task {
            defer { modelSaving = false }
            do {
                let profile = try await session.client.updateAgent(id: agent.id,
                    request: UpdateAgentRequest(acp: next ?? AgentACP()))
                acp = profile.agent.acp
                provider = profile.agent.provider ?? ""
                model = profile.agent.model
                reasoning = profile.agent.reasoning ?? "off"
                fallback = profile.agent.fallbacks
                editingACP = false
                await store.accept(profile)
                await session.sidebar.accept(profile.agent)
            } catch {
                acp = previous
                modelIssue = error.isOffline ? "Not saved. Connect before changing the external agent." : error.reason(or: "Couldn’t save the external agent.")
            }
        }
    }
}

private struct BarState: Hashable {
    let muted: Bool
    let notice: Bool?
    let back: Bool
    let search: Bool
}

enum ProfileTab: String, CaseIterable, Identifiable {
    case work = "Work", media = "Media", files = "Files", capabilities = "Capabilities"
    var id: String { rawValue }
}

/// What the profile panel is showing; it never stacks a second panel.
private enum ProfilePage { case profile, avatar, search }

/// The profile shares the avatar editor's proportions, so moving between them doesn't jump.
enum ProfileSize {
    static let width: CGFloat = 460
    static let height: CGFloat = 560
}

/// A labelled block of a side panel: a heading over its rows.
struct SetupGroup<Content: View>: View {
    let title: String
    let content: Content

    init(_ title: String, @ViewBuilder content: () -> Content) {
        self.title = title
        self.content = content()
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 6) {
            Text(title).font(.subheadline.weight(.semibold)).foregroundStyle(.muted)
            content
        }
        .frame(maxWidth: .infinity, alignment: .leading)
    }
}

/// Centred text that highlights on hover and edits in place when clicked.
struct InlineText: View {
    @Binding var text: String
    let prompt: String
    let font: Font
    var tint: Color = .primary
    var placeholder: String?
    var autofocus = false
    @State private var hovered = false
    #if os(macOS)
    @State private var focused = false
    #else
    @State private var selection: TextSelection?
    @FocusState private var focused: Bool
    #endif

    var body: some View {
        Text(text.isEmpty ? placeholder ?? prompt : text)
            .lineLimit(1)
            .hidden()
            .padding(.horizontal, 10).padding(.vertical, 4)
            .overlay {
                Group {
                    #if os(macOS)
                    InlineInput(text: $text, focused: $focused, prompt: prompt, font: font, tint: tint, autofocus: autofocus)
                    #else
                    TextField("", text: $text, selection: $selection)
                        .labelsHidden()
                        .textFieldStyle(.plain)
                        .foregroundStyle(focused || text.isEmpty ? tint : .clear)
                        .focused($focused)
                        .onSubmit { focused = false }
                        .accessibilityLabel(prompt)
                        .multilineTextAlignment(.center)
                    #endif
                }
                .overlay {
                    if text.isEmpty && !focused {
                        Text(placeholder ?? prompt).foregroundStyle(.muted)
                            .allowsHitTesting(false)
                            .accessibilityHidden(true)
                    }
                }
                if !focused && !text.isEmpty {
                    Text(text).font(font).foregroundStyle(tint)
                        .lineLimit(1)
                        .allowsHitTesting(false)
                        .accessibilityHidden(true)
                }
                #if os(iOS)
                if !focused {
                    Color.clear.contentShape(Rectangle())
                        .onTapGesture {
                            selection = TextSelection(range: text.startIndex..<text.endIndex)
                            focused = true
                        }
                        .accessibilityHidden(true)
                }
                #endif
            }
            .font(font)
            .background(.quaternary.opacity(hovered && !focused ? 1 : 0), in: RoundedRectangle(cornerRadius: 8))
            .motion(value: hovered)
            .pointerHover { hovered = $0 }
    }
}

/// A bot's instructions, with the hint where the first line starts.
private struct PromptEditor: View {
    @Binding var text: String
    var hinted = true

    /// Where TextEditor's own text container starts the first line.
    #if os(macOS)
    private static let inset = EdgeInsets(top: 0, leading: 5, bottom: 0, trailing: 5)
    #else
    private static let inset = EdgeInsets(top: 8, leading: 5, bottom: 8, trailing: 5)
    #endif

    var body: some View {
        TextEditor(text: $text)
            .font(.body)
            .scrollContentBackground(.hidden)
            .scrollIndicators(.hidden)
            // AppKit draws scrolled text past the editor's bounds otherwise.
            .clipped()
            .overlay(alignment: .topLeading) {
                if text.isEmpty, hinted {
                    Text("Add instructions").foregroundStyle(.muted)
                        .padding(.leading, Self.inset.leading)
                        .padding(.top, Self.inset.top)
                        .allowsHitTesting(false)
                        .accessibilityHidden(true)
                }
            }
            .padding(.horizontal, PanelStyle.rowInset - Self.inset.leading)
            .padding(.vertical, max(0, PanelStyle.rowPadding - Self.inset.top))
            .accessibilityLabel("Instructions")
    }
}

/// The instructions in a larger editor over the window. It edits the same text as the field.
private struct PromptPanel: View {
    @Binding var text: String
    @Environment(\.panelClose) private var dismiss
    @FocusState private var focused: Bool

    var body: some View {
        PanelView("Instructions", onClose: { dismiss() }, scrollUnder: false, height: PanelSize.maximumHeight) {
            PanelSection(fills: true) {
                PromptEditor(text: $text).focused($focused)
            }
            .padding(.horizontal, PanelStyle.contentInset)
        }
        .panelDismiss(onClose: { dismiss() })
        .onAppear { focused = true }
    }
}

/// Attaches a bot to an external Agent Client Protocol process, or edits its launch command.
private struct ACPPanel: View {
    let attached: AgentACP?
    let saving: Bool
    let save: (String, [String]) -> Void
    @Environment(\.panelClose) private var dismiss
    @State private var command: String
    @State private var arguments: String

    init(attached: AgentACP?, saving: Bool, save: @escaping (String, [String]) -> Void) {
        self.attached = attached
        self.saving = saving
        self.save = save
        _command = State(initialValue: attached?.command ?? "")
        _arguments = State(initialValue: (attached?.args ?? []).joined(separator: " "))
    }

    private var clean: String { command.trimmingCharacters(in: .whitespacesAndNewlines) }

    var body: some View {
        PanelView("External agent", enabled: !saving, onClose: { dismiss() }) {
            Form {
                Section {
                    SettingsField(title: "Command", text: $command, prompt: "Agent executable")
                    SettingsField(title: "Arguments", text: $arguments, prompt: "Separated by spaces")
                }
                .panelRow()
            }
            .formStyle(.grouped)
            .disabled(saving)
        } actions: {
            Button { save(clean, arguments.split(whereSeparator: \.isWhitespace).map(String.init)) } label: {
                if saving { ProgressView().controlSize(.small) } else { Text(attached == nil ? "Attach" : "Save") }
            }
            .disabled(clean.isEmpty || saving)
            .keyboardShortcut(.defaultAction)
        }
        .interactiveDismissDisabled(saving)
        .panelDismiss(enabled: !saving, onClose: { dismiss() })
    }
}

/// Hovering with a pointer, or pressing on touch, darkens the face and centres a pencil on it.
private struct AvatarEditStyle: ButtonStyle {
    @Binding var hovered: Bool

    func makeBody(configuration: Configuration) -> some View {
        let showing = hovered || configuration.isPressed
        return configuration.label
            .colorMultiply(showing ? Color(white: 0.55) : .white)
            .overlay {
                Image(systemName: "pencil")
                    .font(.title2.weight(.semibold))
                    .foregroundStyle(.white)
                    .opacity(showing ? 1 : 0)
                    .allowsHitTesting(false)
            }
    }
}


/// A bot's task or delegated work, shown with its state only.
private struct TaskRow: View {
    enum Phase { case working, open, done, stopped }

    let title: String
    let phase: Phase

    init(thread: PolymuxKit.Thread) {
        title = thread.title
        phase = thread.isWorking ? .working : thread.task?.status == TaskInfoStatus.done ? .done : .open
    }

    init(run: Run) {
        let active = Self.active(run)
        title = active && !run.activity.isEmpty ? run.activity : run.summary
        switch run.status {
        case _ where active: phase = .working
        case RunStatus.completed: phase = .done
        case RunStatus.failed, RunStatus.cancelled, RunStatus.interrupted: phase = .stopped
        default: phase = .open
        }
    }

    static func active(_ run: Run) -> Bool {
        [RunStatus.queued, RunStatus.running, RunStatus.waitingPeer, RunStatus.resuming].contains(run.status)
    }

    private var status: String {
        switch phase {
        case .working: "In progress"
        case .open: "Open"
        case .done: "Done"
        case .stopped: "Stopped"
        }
    }

    var body: some View {
        HStack(spacing: 8) {
            Group {
                switch phase {
                case .working: WorkingDots()
                case .open: Image(systemName: "circle")
                case .done: Image(systemName: "checkmark")
                case .stopped: Image(systemName: "xmark")
                }
            }
            .foregroundStyle(.muted)
            .frame(width: 20)
            .accessibilityLabel(status)
            Text(title)
                .lineLimit(2)
                .foregroundStyle(phase == .working || phase == .open ? AnyShapeStyle(.primary) : AnyShapeStyle(.muted))
            Spacer(minLength: 0)
        }
    }
}

/// Three dots that rise in turn, with the avatar's thinking motion.
private struct WorkingDots: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    var body: some View {
        TimelineView(.animation(minimumInterval: 1.0 / 30, paused: reduceMotion)) { context in
            Canvas { canvas, size in
                let time = context.date.timeIntervalSinceReferenceDate
                // Points per avatar body radius: the three dots span 168 of its 114.27 units.
                let unit = size.width * 114.2705 / 168
                for index in 0..<3 {
                    let dot = thinkingDot(index, time: time, amount: 1, reducedMotion: reduceMotion)
                    let radius = dot.r * unit
                    let x = size.width / 2 + dot.x * unit, y = size.height / 2 + dot.y * unit
                    var layer = canvas
                    layer.opacity = dot.opacity
                    layer.fill(Path(ellipseIn: CGRect(x: x - radius, y: y - radius, width: 2 * radius, height: 2 * radius)),
                               with: .foreground)
                }
            }
        }
        .frame(width: 18, height: 8)
    }
}

/// Stands in for a section with no data: a spinner, then an icon if it couldn't load. The panel keeps retrying.
private struct SectionLoad: View {
    let failed: Bool
    let offline: Bool

    var body: some View {
        if failed {
            Image(systemName: offline ? "wifi.slash" : "exclamationmark.triangle")
                .foregroundStyle(.muted)
                .accessibilityLabel(offline ? "Offline" : "Couldn’t load")
        } else {
            ProgressView().controlSize(.small).accessibilityLabel("Loading")
        }
    }
}
