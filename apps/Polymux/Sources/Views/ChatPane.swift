import SwiftUI
import PolymuxKit
import PolymuxVoice
#if os(macOS)
import AppKit
#else
import UIKit
#endif

struct ChatPane: View {
    let agent: Agent
    let thread: PolymuxKit.Thread
    let session: AppSession
    let drops: Binding<[DroppedFile]>
    let files: Binding<[DraftFile]>
    let destination: SearchDestination?
    @State private var store: ChatStore
    @Environment(\.scenePhase) private var scenePhase
    @State private var showSearch = false
    @State private var searchQuery = ""
    @State private var paneWidth: CGFloat = 720
    @State private var headerHeight: CGFloat = 88
    @State private var toolbarInset = ToolbarInset.last
    @FocusState private var searchFocused: Bool
    @State private var showVoice = false
    @State private var voiceStore: (any VoiceSession)?
    @State private var openingVoice = false
    @State private var voiceIssue: String?
    @State private var dismissedIssue: String?
    /// Whether the profile panel (or a group's editor) is open; it stays open across chats.
    @Binding var showProfile: Bool
    @Binding var pluginPanel: String?
    /// What the profile panel, which the window owns on macOS, reads from this chat.
    let openChat: OpenChat
    @State private var target: ReplyTarget?
    @State private var highlight: ReplyTarget?
    @State private var jumpID = UUID()
    @State private var revealed: UUID?
    @State private var atBottom = true
    @State private var showLatest = false
    @State private var scrollDown = 0
    @State private var atTop = false
    @State private var composerHeight: CGFloat = 70
    @State private var reply: ReplyContext?
    @State private var dropping = false
    @State private var dropFolder: URL?
    @Namespace private var scrollSpace
    /// Change every frame of a scroll, so reading them must not update the chat.
    @State private var frames = TranscriptFrames()
    @State private var latestVisible = true
    @State private var scrollPosition = ScrollPosition()
    /// The message to reopen on, until the chat has scrolled there.
    @State private var place: String?
    @State private var scrollHeight: CGFloat = 0
    @State private var scrollDate: Date?
    @State private var dateVisible = false
    @State private var dateScrolling = false
    @State private var dateTask: Task<Void, Never>?
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.connectionDismissed) private var connectionDismissed
    #if os(iOS)
    @Environment(\.self) private var environment
    @Environment(\.horizontalSizeClass) private var sizeClass
    #endif

    private var verticalToolbar: Bool {
        #if os(iOS)
        if #available(iOS 27.1, *) { return environment.toolbarVerticalEdge != nil }
        #endif
        return false
    }

    private var headerInset: CGFloat { verticalToolbar ? 0 : toolbarInset }

    init(agent: Agent, thread: PolymuxKit.Thread, session: AppSession,
         drops: Binding<[DroppedFile]>, files: Binding<[DraftFile]>,
         showProfile: Binding<Bool>, pluginPanel: Binding<String?>, openChat: OpenChat,
         destination: SearchDestination? = nil, store: ChatStore) {
        self.agent = agent
        _showProfile = showProfile
        _pluginPanel = pluginPanel
        self.openChat = openChat
        self.thread = thread
        self.session = session
        self.drops = drops
        self.files = files
        self.destination = destination
        _store = State(initialValue: store)
        // Opens where it was left, before the first frame, unless a search result is taking it elsewhere.
        let place = destination?.threadID == thread.id ? nil : session.places[thread.id]
            .flatMap { id in store.messages.contains { $0.id == id } ? id : nil }
        _place = State(initialValue: place)
        _atBottom = State(initialValue: place == nil)
    }

    var body: some View {
        #if DEBUG
        let _ = RenderCount.hit("pane")
        #endif
        Group {
            #if os(iOS)
            NavigationStack {
                conversation
                    .navigationDestination(isPresented: profilePresented) { profile }
                    .navigationDestination(item: $pluginPanel) { name in
                        if let plugin = pluginGroups.joined().first(where: { $0.name == name }) {
                            PluginPanel(plugin: plugin, thread: thread, session: session) { pluginPanel = nil }
                        }
                    }
                    .toolbar(.hidden, for: .tabBar)
            }
            #else
            conversation
            #endif
        }
        .task {
            openChat.open(store) { id, part in jump(to: id, part: part) }
            store.isVisible = scenePhase == .active
            store.onUpdate = { event, tap in session.sidebar.note(event, at: tap) }
            await store.start()
            reveal()
            #if DEBUG
            if CallFixture.enabled { openVoice() }
            #endif
            await store.markRead()
        }
        .task(id: scenePhase == .background) {
            let profiles = thread.participantIDs.filter { !$0.hasPrefix("local-") }.map { session.profile($0) }
            while !Task.isCancelled {
                await withTaskGroup(of: Void.self) { group in
                    for profile in profiles { group.addTask { await profile.refresh() } }
                }
                do { try await Task.sleep(for: .seconds(scenePhase == .background ? 30 : 5)) }
                catch { break }
            }
        }
        .onChange(of: destination?.id) { _, _ in reveal() }
        .onChange(of: store.isConnected) { _, _ in reveal() }
        .onChange(of: store.error) { _, _ in reveal() }
        .onDisappear { keepPlace(); store.stop(); clearDate(); highlight = nil; target = nil }
        #if os(macOS)
        .focusedSceneValue(\.findInChat) { showSearch = true }
        #endif
        .onChange(of: scenePhase) { _, phase in
            store.isVisible = phase == .active
            if store.isVisible { Task { await store.markRead() } }
        }
    }

    private var pluginGroups: [[Plugin]] {
        session.plugins.buttons(chat: thread.isTask ? thread.agentID : thread.id)
    }

    private func togglePlugin(_ plugin: Plugin) {
        pluginPanel = pluginPanel == plugin.name ? nil : plugin.name
    }

    #if os(iOS)
    private var profilePresented: Binding<Bool> {
        Binding(get: { showProfile }, set: { showProfile = $0 })
    }

    /// On iPhone the profile is its own page in the chat's navigation.
    private var profile: some View {
        ProfilePanel(agent: agent, thread: thread, session: session, openChat: openChat,
                     close: { showProfile = false }) { id in
            showProfile = false
            jump(to: id)
        }
    }
    #endif

    private var conversation: some View {
        VStack(spacing: 0) {
            if showSearch {
                if verticalToolbar { searchField.padding(.vertical, 8) }
                SearchView(session: session, threadID: thread.id, agentName: chatTitle,
                           query: $searchQuery, topInset: topInset, bottomInset: 0,
                           close: closeSearch) { messageID in
                    jump(to: messageID)
                    closeSearch()
                }
                .overlay { edgeFades }
            } else {
                transcript
                    .overlay {
                        if store.messages.isEmpty && store.isConnected {
                            Text("No messages yet.").foregroundStyle(.muted)
                                .padding(.top, topInset)
                        }
                    }
                    .overlay { edgeFades }
                    .overlay(alignment: .top) {
                        if let scrollDate, !store.messages.isEmpty {
                            ScrollDate(date: scrollDate)
                                .padding(.top, topInset + 6)
                                .opacity(dateVisible ? 1 : 0)
                                .allowsHitTesting(false)
                        }
                    }
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        #if os(iOS)
        .simultaneousGesture(TapGesture().onEnded { searchFocused = false }, isEnabled: showSearch)
        #endif
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { paneWidth = $0 }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            composer
                .frame(height: showSearch || showVoice ? 0 : nil)
                .opacity(showSearch || showVoice ? 0 : 1)
                .allowsHitTesting(!showSearch && !showVoice)
                .accessibilityHidden(showSearch || showVoice)
            .motion(value: showLatest)
            .motion(value: latestVisible)
            .task(id: atBottom) {
                if atBottom {
                    showLatest = false
                    return
                }
                try? await Task.sleep(for: .milliseconds(150))
                if !Task.isCancelled { showLatest = true }
            }
        }
        .overlay {
            if showVoice, let voiceStore {
                VoiceOverlay(store: voiceStore, name: agent.name, dismiss: { showVoice = false })
            }
        }
        #if os(iOS)
        .ignoresSafeArea(.container, edges: showSearch ? [] : .bottom)
        #endif
        .onChange(of: showSearch) { _, searching in
            if searching {
                showVoice = false
                clearDate()
                highlight = nil
                target = nil
            }
        }
        .onChange(of: drops.wrappedValue) { _, files in
            if !files.isEmpty {
                closeSearch()
                showVoice = false
            }
        }
        .navigationTitle("")
        .overlay(alignment: .top) {
            if !showSearch {
                ZStack(alignment: .top) {
                    ChatHeader(title: chatTitle, subtitle: headerRole, members: members,
                               isGroup: thread.isGroup, conversationID: thread.id, runs: store.runs, activity: store.activity,
                               connected: session.sidebar.isConnected,
                               edit: { showProfile.toggle() })
                        .padding(.top, 8)
                        .offset(y: -headerInset)
                        .accessibilityLabel(thread.isGroup ? "Edit chat" : "Edit \(agent.name)’s profile")
                        .help(thread.isGroup ? "Edit chat" : "Edit agent profile")
                        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { headerHeight = $0 }
                }
                #if os(macOS)
                .background {
                    ToolbarAnchor { inset in
                        ToolbarInset.last = inset
                        toolbarInset = inset
                    }
                }
                #else
                .background {
                    ToolbarAnchor { top, safeTop in alignHeader(top: top, safeTop: safeTop) }
                }
                #endif
            }
        }
        #if os(macOS)
        .overlay {
            Rectangle()
                .fill(.primary.opacity(PanelStyle.selectedTint))
                .ignoresSafeArea(.container, edges: [.top, .bottom])
                .opacity(dropping ? 1 : 0)
                .motion(value: dropping)
                .allowsHitTesting(false)
                .accessibilityHidden(true)
        }
        .onDrop(of: DroppedFile.types, isTargeted: Binding(get: { dropping }, set: { inside in
            let types = NSPasteboard(name: .drag).types ?? []
            dropping = inside && !types.contains(NSPasteboard.PasteboardType(SidebarItem.contentType.identifier))
        })) { _ in
            dropping = false
            guard let folder = dropFolder else { drops.wrappedValue.append(.failed); return false }
            let accepted = DroppedFile.receive(from: NSPasteboard(name: .drag), into: folder) { drops.wrappedValue.append($0) }
            if accepted {
                dropFolder = nil
                Task { dropFolder = try? await DroppedFile.prepare() }
            }
            return accepted
        }
        .task { dropFolder = try? await DroppedFile.prepare() }
        #endif
        #if os(macOS)
        .panelBar(value: ChatBarState(search: showSearch, opening: openingVoice, voice: showVoice,
                                      plugins: pluginGroups, panel: pluginPanel)) {
            if showSearch {
                HStack(spacing: 8) {
                    searchField
                    PanelNavButton(symbol: "xmark", label: "Close search", action: closeSearch)
                        .keyboardShortcut(.escape, modifiers: [])
                }
            } else {
                ZStack {
                    Button { showProfile.toggle() } label: {
                        Color.clear.frame(width: 80, height: 36).contentShape(Rectangle())
                    }
                    .buttonStyle(.plain).accessibilityHidden(true)
                    HStack {
                        if thread.isTask {
                            PanelNavButton(symbol: "chevron.left", label: "Back") { openChat.show(agent.id) }
                        }
                        Spacer(minLength: 0)
                        PluginButtons(groups: pluginGroups, selected: pluginPanel, open: togglePlugin)
                        if !thread.isGroup {
                            PanelNavButton(symbol: "phone", label: "Speech conversation",
                                           enabled: !openingVoice && !showVoice, action: openVoice)
                        }
                    }
                }
            }
        }
        #else
        .toolbar {
            ToolbarItem(placement: .principal) {
                if showSearch && !verticalToolbar {
                    searchField
                        .onGeometryChange(for: CGFloat.self, of: { $0.size.height }) { headerHeight = $0 }
                } else if !showSearch && !verticalToolbar {
                    // The overlapping avatar occupies the native toolbar's hit region.
                    Button { showProfile.toggle() } label: {
                        Color.clear.frame(width: 80, height: 44).contentShape(Rectangle())
                    }
                    .buttonStyle(.plain).accessibilityHidden(true)
                }
            }
            .sharedBackgroundVisibility(.hidden)
            if !showSearch && !pluginGroups.isEmpty {
                ToolbarItem(placement: .primaryAction) {
                    if sizeClass == .compact {
                        PluginMenu(groups: pluginGroups, open: togglePlugin)
                    } else {
                        PluginButtons(groups: pluginGroups, selected: pluginPanel, open: togglePlugin)
                    }
                }
            }
            if showSearch || !thread.isGroup {
                ToolbarItem(placement: .primaryAction) {
                    if showSearch {
                        Button("Close search", systemImage: "xmark", action: closeSearch)
                            .keyboardShortcut(.escape, modifiers: [])
                    } else {
                        voiceButton
                    }
                }
            }
        }
        #endif
    }

    private var voiceButton: some View {
        Button("Speech conversation", systemImage: "phone", action: openVoice)
            .disabled(openingVoice || showVoice)
    }

    private var members: [Agent] {
        thread.participantIDs.compactMap { id in session.sidebar.agents.first { $0.id == id } }
    }

    #if os(iOS)
    private func alignHeader(top: CGFloat, safeTop: CGFloat) {
        let inset = max(0, top + 8 - safeTop)
        guard abs(toolbarInset - inset) > 0.5 else { return }
        ToolbarInset.last = inset
        toolbarInset = inset
    }
    #endif

    private var chatTitle: String { thread.id == agent.id ? agent.name : thread.title }

    private var headerRole: String {
        if thread.isTask { return agent.name }
        return thread.isGroup ? "" : agent.role.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// The bot that created this one, for the caption at the start of its chat.
    private var creator: String? {
        guard thread.id == agent.id, let id = agent.createdBy, !id.isEmpty else { return nil }
        return session.sidebar.agents.first { $0.id == id }?.name
    }

    private func author(_ message: Message) -> String {
        session.sidebar.agents.first { $0.id == message.agentID }?.name ?? agent.name
    }

    private var searchField: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass").foregroundStyle(.muted)
            TextField("Search this conversation", text: $searchQuery)
                .textFieldStyle(.plain).focused($searchFocused)
                .focusEffectDisabled()
                .submitLabel(.search)
                .accessibilityLabel("Search this conversation")
                .onKeyPress(.escape) { closeSearch(); return .handled }
                #if os(macOS)
                .background {
                    SearchFocus(prompt: "Search this conversation", blurred: { searchFocused = false })
                        .frame(width: 0, height: 0)
                }
                #endif
        }
        .padding(.horizontal, 12).padding(.vertical, 8)
        .frame(width: searchWidth)
        .glassEffect(.regular, in: Capsule())
        .onAppear { searchFocused = true }
    }

    private var searchWidth: CGFloat {
        #if os(macOS)
        min(560, max(120, paneWidth - 180))
        #else
        max(120, paneWidth - 130)
        #endif
    }

    private func closeSearch() {
        searchFocused = false
        showSearch = false
        searchQuery = ""
    }

    private var edgeFades: some View {
        VStack(spacing: 0) {
            Rectangle().fill(.background)
                .frame(height: max(headerHeight + 24, 72))
                .mask {
                    LinearGradient(stops: [.init(color: .black, location: 0),
                                           .init(color: .black.opacity(0.9), location: 0.35),
                                           .init(color: .clear, location: 1)],
                                   startPoint: .top, endPoint: .bottom)
                }
            Spacer(minLength: 0)
            if !showSearch {
                Rectangle().fill(.background)
                    .frame(height: composerHeight + 30)
                    .mask {
                        LinearGradient(colors: [.clear, .black.opacity(0.9), .black],
                                       startPoint: .top, endPoint: .bottom)
                    }
            }
        }
        .ignoresSafeArea(.container, edges: [.top, .bottom])
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }

    private var topInset: CGFloat { showSearch ? 8 : max(0, headerHeight - headerInset) + 8 }

    private var composer: some View {
        VStack(alignment: .leading, spacing: 6) {
            // A voice start that failed needs no action, so its note fades on its own.
            if let voiceIssue {
                Text(voiceIssue).font(.caption2).foregroundStyle(.muted)
                    .task(id: voiceIssue) {
                        try? await Task.sleep(for: .seconds(6))
                        self.voiceIssue = nil
                    }
            }
            composerField
                .overlay(alignment: .top) {
                    // Status and the latest button float above the input without changing the chat inset.
                    VStack(spacing: 8) {
                        ZStack(alignment: .bottom) {
                            ConnectionStatus(connected: connected, dismissed: connectionDismissed)
                            WarningBubble(text: serverIssue?.message ?? "", visible: issueVisible) {
                                dismissedIssue = serverIssue?.id
                            }
                        }
                        .frame(height: statusVisible ? nil : 0)
                        if showLatest && !latestVisible && !showSearch && !showVoice {
                            latestButton
                        }
                    }
                    .alignmentGuide(.top) { $0[.bottom] + 8 }
                }
        }
        .padding(.horizontal, ComposerMetrics.edge).padding(.bottom, ComposerMetrics.edge).padding(.top, 10)
        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { composerHeight = $0 }
    }

    private var connected: Bool { store.isConnected && session.sidebar.isConnected }

    private var serverIssue: (id: String, message: String)? {
        if let message = store.messages.last, let notice = store.activity.notice(for: message) {
            return (message.id, notice)
        }
        guard let message = store.messages.last(where: { $0.role == MessageRole.assistant && $0.status == MessageStatus.error }),
              let error = store.activity.issue(for: message),
              !error.message.isEmpty else { return nil }
        return (message.id, error.message)
    }

    private var issueVisible: Bool {
        serverIssue != nil && dismissedIssue != serverIssue?.id
            && connected && store.messages.last?.id == serverIssue?.id
    }

    private var statusVisible: Bool {
        (!connected && !connectionDismissed.wrappedValue) || issueVisible
    }

    private var latestButton: some View {
        LatestButton { scrollDown += 1 }
    }

    private var visibleMessages: [Message] {
        store.messages.filter { message in
            message.role != MessageRole.assistant || message.parts.contains {
                $0.attachment != nil || [PartKind.choice, PartKind.credential, PartKind.vault].contains($0.kind)
                    || (message.status != MessageStatus.streaming && !($0.text ?? "").isEmpty)
            }
        }
    }

    private var composerField: some View {
        ComposerView(client: session.client, draft: session.draft(thread.id),
                     isVoiceActive: showVoice || showSearch || openingVoice,
                      send: { text, files, quote, mentions in
                          session.avatars.poke(agent.id, .nod)
                          if reply == quote { setReply(nil) }
                          if files.isEmpty {
                              await store.send(text: text, reply: quote, mentions: mentions)
                          } else {
                              await store.send(text: text, files: files, reply: quote, mentions: mentions)
                          }
                     },
                     reply: reply, agentName: reply.flatMap { quote in session.sidebar.agents.first { $0.id == quote.agentID }?.name } ?? agent.name,
                      clearReply: { setReply(nil) }, restoreReply: setReply, drops: drops, members: thread.isGroup ? members : [],
                     drafting: { active in
                         for member in members {
                             store.activity.setListening(active, conversationID: thread.id, agentID: member.id)
                         }
                     },
                     files: files)
            .frame(height: showVoice ? 0 : nil)
            .opacity(showVoice ? 0 : 1)
            .allowsHitTesting(!showVoice)
            .accessibilityHidden(showVoice)
    }

    private func setReply(_ value: ReplyContext?) {
        withAnimation(Motion.quick.unless(reduceMotion)) { reply = value }
    }

    /// Opens at once from the settings already loaded; only a first launch waits for them.
    private func openVoice() {
        // Speech conversations are one to one.
        guard !thread.isGroup, !openingVoice, !showVoice else { return }
        if let settings = session.settings.value {
            startVoice(settings)
            return
        }
        openingVoice = true
        Task {
            defer { openingVoice = false }
            guard await session.settings.synchronize(), let settings = session.settings.value else {
                voiceIssue = session.settings.error ?? "Connect to the agent server and wait for settings to save."
                return
            }
            startVoice(settings)
        }
    }

    private func startVoice(_ settings: PolymuxKit.Settings) {
        let config = VoiceConfig(settings: settings)
        voiceStore = config.session(client: session.client,
            reply: VoiceBridge(client: session.client, threadID: thread.id),
            agent: "\(agent.name) (\(agent.id))"
        )
        showVoice = true
    }

    private var transcript: some View {
        ScrollViewReader { proxy in
            ScrollView {
                let visible = visibleMessages
                LazyVStack(spacing: 12) {
                    ForEach(Array(visible.enumerated()), id: \.element.id) { index, message in
                        if startsGroup(index, in: visible) {
                            ChatStatus(date: message.date)
                        }
                        if index == 0, !store.hasEarlier, let creator {
                            Text("Created by \(creator)")
                                .font(.caption2.weight(.medium)).foregroundStyle(.muted)
                                .frame(maxWidth: .infinity).padding(.bottom, 14)
                        }
                        MessageBubble(message: message, client: session.client, agentName: author(message),
                                      showAuthor: thread.isGroup && message.role == MessageRole.assistant
                                          || message.role == MessageRole.user && message.agentID != nil,
                                      replyName: message.reply.flatMap { quote in session.sidebar.agents.first { $0.id == quote.agentID }?.name } ?? agent.name,
                                      sending: store.isSending, discard: { await store.discard(messageID: message.id) },
                                      respond: setReply, jump: { jump(to: $0.messageID, part: $0.partIndex) },
                                      ask: { text, card in Task { await store.send(text: text, reply: card) } },
                                      highlight: highlight, reactions: store.reactions(for: message),
                                      reacting: store.pendingReactions[message.id] != nil,
                                       reactionError: store.reactionErrors[message.id], agents: session.sidebar.agents,
                                       react: { emoji in Task { await store.react(messageID: message.id, emoji: emoji) } },
                                      scrollSpace: scrollSpace)
                        .equatable()
                        .id(message.id)
                        .background {
                            GeometryReader { geometry in
                                Color.clear.preference(key: ScrollDateFrames.self,
                                                       value: [message.id: ScrollDateFrame(
                                                        date: message.date,
                                                        frame: geometry.frame(in: .named(scrollSpace)))])
                            }
                            .allowsHitTesting(false)
                        }
                    }
                    TypingRow(members: members.compactMap { member in
                        let view = store.activity.view(conversationID: thread.id, agentID: member.id, connected: session.sidebar.isConnected)
                        return (view.typing || view.working) && session.sidebar.isConnected ? (member, view) : nil
                    }, session: session, conversationID: thread.id, group: thread.isGroup)
                    // Closes the content: scrolling here rests at the exact end, where the bottom anchor holds.
                    Color.clear.frame(height: 15).id("bottom")
                }
                .padding(.horizontal, 20).padding(.top, 14)
            }
            .coordinateSpace(name: scrollSpace)
            // Hidden until it's in place, so reopening never flashes the end of the chat.
            .opacity(place == nil ? 1 : 0)
            .task {
                guard place != nil else { return }
                frames.moving = true
                // Gives up rather than leave the chat hidden if the message never lays out.
                try? await Task.sleep(for: .milliseconds(400))
                place = nil
                frames.moving = false
            }
            .scrollPosition($scrollPosition)
            .scrollIndicators(.hidden)
            .scrollEdgeEffectHidden()
            .contentMargins(.top, topInset, for: .scrollContent)
            .defaultScrollAnchor(.bottom, for: .initialOffset)
            // Not for size changes: anchored through a resize, lazy rows stay on screen but stop
            // reporting frames and accessibility. `ChatViewport` holds the end of the chat instead.
            .defaultScrollAnchor(.top, for: .alignment)
            .onChange(of: scrollDown) { _, _ in
                withAnimation(Motion.move.unless(reduceMotion)) { proxy.scrollTo("bottom", anchor: .bottom) }
            }
            .onPreferenceChange(ScrollDateFrames.self) { dates in
                frames.dates = dates
                updateDate()
                settlePlace(proxy)
            }
            .onPreferenceChange(PartFrames.self) { frames.parts = $0 }
            .onScrollGeometryChange(for: CGFloat.self) { $0.contentOffset.y } action: { _, offset in
                frames.offset = offset
            }
            .onScrollGeometryChange(for: CGFloat.self) { $0.containerSize.height } action: { _, height in
                scrollHeight = height
                updateDate()
            }
            .onScrollPhaseChange { _, phase in
                frames.moving = phase.isScrolling || frames.jump != nil
                datePhase(phase)
                // A chat can open at the top (first unread), so scrolling there also pages back.
                if phase.isScrolling && atTop { Task { await store.loadEarlier() } }
            }
            .onScrollGeometryChange(for: ChatViewport.self) { geometry in
                ChatViewport(size: geometry.containerSize, inset: geometry.contentInsets.bottom,
                             gap: geometry.contentSize.height - geometry.visibleRect.maxY)
            } action: { old, next in
                // Opening away from the end: nothing here may pull it back there.
                guard place == nil else { return }
                if old.size != next.size || old.inset != next.inset {
                    if atBottom { scrollPosition.scrollTo(edge: .bottom) }
                } else {
                    // Separate enter/leave thresholds keep lazy layout estimates from toggling the button.
                    let near = next.gap <= (atBottom ? 90 : 45)
                    // Only the user leaves the end of a chat; content arriving late keeps it there.
                    if near || frames.moving { atBottom = near }
                    // Content arriving at the end keeps it there, in one move.
                    // Not `scrollPosition`: it keeps the edge and would pull a later jump back.
                    if atBottom, !frames.moving, next.gap > old.gap, next.gap > 1 - next.inset {
                        withTransaction(Transaction(animation: nil)) { proxy.scrollTo("bottom", anchor: .bottom) }
                    }
                }
            }
            .onScrollGeometryChange(for: Bool.self) { geometry in
                geometry.contentOffset.y < 20 && geometry.contentSize.height > geometry.containerSize.height
            } action: { old, top in
                atTop = top
                // Initial layout can reach the top before the bottom anchor settles.
                if top && !old && dateScrolling { Task { await store.loadEarlier() } }
            }
            .onChange(of: composerHeight) { _, _ in updateDate() }
            .task(id: jumpID) {
                guard let destination = target else { return }
                let id = destination.id
                let current = jumpID
                // A newer jump cancels this one and owns the flag from then on.
                defer {
                    if frames.jump == current {
                        frames.jump = nil
                        frames.moving = false
                    }
                }
                while !store.messages.contains(where: { $0.id == id }) && store.hasEarlier {
                    guard !Task.isCancelled else { return }
                    let count = store.messages.count
                    await store.loadEarlier()
                    if count == store.messages.count { break }
                }
                await Task.yield()
                guard !Task.isCancelled, store.messages.contains(where: { $0.id == id }) else { return }
                if destination.part != nil {
                    proxy.scrollTo(id, anchor: .center)
                    try? await Task.sleep(for: .milliseconds(60))
                    guard !Task.isCancelled else { return }
                }
                guard destination.part == nil || frames.parts[destination] != nil else { return }
                target = nil
                withAnimation(Motion.move.unless(reduceMotion)) {
                    highlight = destination
                    if let frame = frames.parts[destination] {
                        let center = (topInset + scrollHeight - composerHeight) / 2
                        scrollPosition.scrollTo(y: frames.offset + frame.midY - center)
                    } else {
                        proxy.scrollTo(id, anchor: .center)
                    }
                }
                try? await Task.sleep(for: .seconds(1.2))
                guard !Task.isCancelled else { return }
                withAnimation(Motion.fade.unless(reduceMotion)) { highlight = nil }
            }
        }
    }

    private func updateDate() {
        let top = max(0, headerHeight - headerInset)
        let height = max(0, scrollHeight - composerHeight)
        let first = frames.dates.values
            .filter { $0.frame.maxY > top && $0.frame.minY < height }
            .min { $0.frame.minY < $1.frame.minY }
        let date = first.map { Calendar.current.startOfDay(for: $0.date) }
        if scrollDate != date { scrollDate = date }
        var shown = atBottom
        if let id = store.messages.last?.id, let frame = frames.dates[id]?.frame {
            shown = frame.maxY >= topInset && frame.maxY <= scrollHeight
        }
        if latestVisible != shown { latestVisible = shown }
        if dateScrolling, scrollDate != nil {
            withAnimation(Motion.quick.unless(reduceMotion)) { dateVisible = true }
        }
    }

    private func datePhase(_ phase: ScrollPhase) {
        if phase.isScrolling {
            dateTask?.cancel()
            dateTask = nil
            dateScrolling = true
            updateDate()
        } else if phase == .idle {
            dateScrolling = false
            guard dateVisible else { return }
            dateTask?.cancel()
            dateTask = Task { @MainActor in
                try? await Task.sleep(for: .seconds(1.2))
                guard !Task.isCancelled else { return }
                withAnimation(Motion.fade.unless(reduceMotion)) { dateVisible = false }
                dateTask = nil
            }
        } else if phase == .tracking {
            dateTask?.cancel()
            dateTask = nil
        }
    }

    private func clearDate() {
        dateTask?.cancel()
        dateTask = nil
        dateScrolling = false
        dateVisible = false
    }

    private func startsGroup(_ index: Int, in messages: [Message]) -> Bool {
        ChatStatus.starts(messages[index].date, after: index == 0 ? nil : messages[index - 1].date)
    }

    /// Remembers the top message of a chat left away from its end.
    private func keepPlace() {
        let top = frames.dates.filter { $0.value.frame.maxY > topInset }.min { $0.value.frame.minY < $1.value.frame.minY }
        session.places[thread.id] = atBottom ? nil : top?.key
    }

    /// Runs on layout, because a lazy transcript can't scroll to a message before it has laid out.
    private func settlePlace(_ proxy: ScrollViewProxy) {
        guard let place, !frames.dates.isEmpty else { return }
        if let frame = frames.dates[place]?.frame, frame.minY >= 0, frame.minY < scrollHeight / 2 {
            self.place = nil
            frames.moving = false
        } else {
            proxy.scrollTo(place, anchor: .top)
        }
    }

    private func reveal() {
        guard let destination, destination.id != revealed, destination.threadID == thread.id,
              store.isConnected || store.error != nil, let messageID = destination.messageID else { return }
        revealed = destination.id
        closeSearch()
        jump(to: messageID)
    }

    private func jump(to id: String, part: Int64? = nil) {
        atBottom = false
        let next = UUID()
        frames.jump = next
        frames.moving = true
        highlight = nil
        target = ReplyTarget(id: id, part: part)
        jumpID = next
    }
}

private final class TranscriptFrames {
    var dates: [String: ScrollDateFrame] = [:]
    var parts: [ReplyTarget: CGRect] = [:]
    var moving = false
    var jump: UUID?
    /// Changes every scroll tick, so it stays out of `@State` to keep the pane from redrawing.
    var offset: CGFloat = 0
}

private struct ChatViewport: Equatable {
    let size: CGSize
    let inset: CGFloat
    let gap: CGFloat
}

private struct ChatBarState: Hashable {
    let search: Bool
    let opening: Bool
    let voice: Bool
    let plugins: [[Plugin]]
    let panel: String?
}

struct ReplyTarget: Hashable, Sendable {
    let id: String
    let part: Int64?
}

private struct PartFrames: PreferenceKey {
    static let defaultValue: [ReplyTarget: CGRect] = [:]

    static func reduce(value: inout [ReplyTarget: CGRect], nextValue: () -> [ReplyTarget: CGRect]) {
        value.merge(nextValue(), uniquingKeysWith: { _, next in next })
    }
}

private struct JumpGlow: ViewModifier {
    let active: Bool

    func body(content: Content) -> some View {
        content.overlay {
            RoundedRectangle(cornerRadius: BubbleStyle.radius)
                .fill(.tint.opacity(0.08))
                .strokeBorder(.primary.opacity(0.3), lineWidth: 2)
                .opacity(active ? 1 : 0)
                .allowsHitTesting(false)
                .accessibilityHidden(true)
        }
    }
}

struct LatestButton: View {
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Image(systemName: "chevron.down").font(.caption).imageScale(.small)
                .padding(.horizontal, 8).padding(.vertical, 6)
                .contentShape(Capsule())
        }
        .accessibilityLabel("Scroll to latest")
        .foregroundStyle(.muted)
        .buttonStyle(.plain)
        .glassEffect(.regular.interactive(), in: Capsule())
        .tapArea(10)
        .transition(.opacity)
    }
}

struct ChatStatus: View {
    let date: Date

    /// Whether a message opens a new stretch of the chat, so it gets a time above it.
    static func starts(_ current: Date, after previous: Date?) -> Bool {
        guard let previous else { return true }
        return !Calendar.current.isDate(previous, inSameDayAs: current) || current.timeIntervalSince(previous) > 1800
    }

    var body: some View {
        Text(date, format: ProgressiveTime(separator: true))
            .font(.caption2.weight(.medium)).foregroundStyle(.muted)
            .frame(maxWidth: .infinity).padding(.vertical, 14)
    }
}

struct MessageBubble: View, @MainActor Equatable {
    @Environment(\.colorScheme) private var colorScheme
    @State private var contentWidth: CGFloat = 0
    @State private var waited = false
    let message: Message
    let client: APIClient
    let agentName: String
    var showAuthor = false
    var replyName = ""
    var sending = false
    let discard: () async -> Void
    let respond: (ReplyContext) -> Void
    let jump: (ReplyContext) -> Void
    /// Sends text a card produced through `polymuxSend` to the agent as a reply to that card.
    let ask: (String, ReplyContext?) -> Void
    var highlight: ReplyTarget?
    let reactions: [Reaction]
    let reacting: Bool
    let reactionError: String?
    let agents: [Agent]
    let react: (String) -> Void
    let scrollSpace: Namespace.ID
    /// Hub draws its own reaction bar beneath the bubble.
    var showsReactions = true

    /// Bubbles redraw for what they show. Their actions never change for a given message.
    static func == (lhs: Self, rhs: Self) -> Bool {
        lhs.message == rhs.message && lhs.agentName == rhs.agentName && lhs.showAuthor == rhs.showAuthor
            && lhs.replyName == rhs.replyName && lhs.sending == rhs.sending && lhs.glow == rhs.glow
            && lhs.reactions == rhs.reactions && lhs.reacting == rhs.reacting
            && lhs.reactionError == rhs.reactionError && lhs.agents == rhs.agents
    }

    private var glow: ReplyTarget? { highlight?.id == message.id ? highlight : nil }

    /// The user's own messages; a task another bot gave this one arrives from that bot.
    private var outgoing: Bool { message.role == MessageRole.user && message.agentID == nil }

    var body: some View {
        #if DEBUG
        let _ = RenderCount.hit("bubble")
        #endif
        HStack(alignment: .bottom, spacing: 36) {
            if outgoing { Spacer(minLength: 0) }
            // The glow hugs the natural content, not the full-width row frame.
            column
                .modifier(JumpGlow(active: highlight == ReplyTarget(id: message.id, part: nil)))
                .frame(maxWidth: BubbleStyle.width, alignment: outgoing ? .trailing : .leading)
            if !outgoing { Spacer(minLength: 0) }
        }
        .reactionPicker(react: react)
        .task(id: message.status) {
            guard message.status == MessageStatus.pending else { return }
            try? await Task.sleep(for: .seconds(1))
            if !Task.isCancelled { waited = true }
        }
    }

    private var column: some View {
        VStack(alignment: outgoing ? .trailing : .leading, spacing: 6) {
            if showAuthor && (message.status != MessageStatus.streaming || !displayParts.isEmpty) {
                Text(agentName).font(.caption.weight(.medium)).foregroundStyle(.muted).padding(.horizontal, 12)
            }
            VStack(alignment: outgoing ? .trailing : .leading, spacing: 12) {
                ForEach(displayParts, id: \.offset) { index, part in
                    partView(part, index: index)
                        .background {
                            GeometryReader { geometry in
                                Color.clear.preference(key: PartFrames.self,
                                    value: [ReplyTarget(id: message.id, part: Int64(index)):
                                                geometry.frame(in: .named(scrollSpace))])
                            }
                            .allowsHitTesting(false)
                        }
                        .modifier(JumpGlow(active: highlight == ReplyTarget(id: message.id, part: Int64(index))))
                        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width in
                            if index == displayParts.last?.offset { contentWidth = width }
                        }
                }
            }
            if let reply = message.reply {
                ReplyBubble(reply: reply, author: reply.role == ReplyContextRole.user ? "You" : replyName,
                            client: client, jump: { jump(reply) },
                             width: replyWidth(reply))
            }
            if showsReactions, !reactions.isEmpty {
                ReactionsView(reactions: reactions, agents: agents, react: react)
                    .disabled(!canReact || reacting)
            }
            if let reactionError {
                Text(reactionError).font(.caption2).foregroundStyle(.muted)
            }
            if outgoing, unsentLine {
                if sending && message.status == MessageStatus.pending {
                    Text("Sending…").font(.caption2).foregroundStyle(.muted)
                } else {
                    // Unsent messages go out on their own once the chat reconnects; refused ones stay.
                    HStack(spacing: 6) {
                        Text(message.status == MessageStatus.error ? "Not sent" : "Not sent yet")
                        Text("·")
                        Button("Delete") { Task { await discard() } }
                            .buttonStyle(.borderless)
                            .foregroundStyle(.muted)
                            .accessibilityLabel("Delete unsent message")
                            .disabled(sending)
                    }
                    .font(.caption2).foregroundStyle(.muted)
                }
            }
        }
    }

    /// A send that lands right away shows no line, so the bubble keeps its height.
    private var unsentLine: Bool {
        message.status == MessageStatus.pending ? waited
            : [MessageStatus.failed, MessageStatus.error].contains(message.status)
    }

    private var bubble: Color {
        outgoing ? Color("UserBubble") : BubbleStyle.incoming(colorScheme)
    }

    private var canReact: Bool {
        message.seq > 0 && [MessageStatus.done, MessageStatus.error].contains(message.status)
    }

    @ViewBuilder private var reactionMenu: some View {
        if canReact {
            ReactionMenu(reactions: reactions, react: react).disabled(reacting)
        }
    }

    private var displayParts: [(offset: Int, element: Part)] {
        let parts = Array(message.parts.enumerated())
        let attachments = parts.filter { $0.element.attachment != nil }
        let choices = parts.filter { [PartKind.choice, PartKind.credential, PartKind.vault].contains($0.element.kind) }
        // A reply still being written is shown whole when it finishes, not as it arrives.
        // Its text is still recorded, so a stopped reply keeps what it managed to say.
        if message.status == MessageStatus.streaming { return attachments + choices }
        return attachments + choices +
            parts.filter { $0.element.attachment == nil && ![PartKind.choice, PartKind.credential, PartKind.vault].contains($0.element.kind) && !($0.element.text ?? "").isEmpty }
    }

    @ViewBuilder private func partView(_ part: Part, index: Int) -> some View {
        if let attachment = part.attachment {
            MediaView(attachment: attachment, client: client, bubbleColor: bubble,
                      alignment: outgoing ? .trailing : .leading,
                      respond: { respond(to: index) },
                      action: { ask($0, message.replyContext(part: index)) },
                      reactions: reactions, react: canReact && !reacting ? react : nil)
        } else if part.kind == PartKind.choice {
            ChoiceCard(part: part, client: client, message: message, bubble: bubble)
                .appMenu { reactionMenu }
        } else if let request = part.credential, part.kind == PartKind.credential {
            CredentialCard(request: request, client: client, message: message, bubble: bubble)
                .appMenu { reactionMenu }
        } else if let request = part.vault, part.kind == PartKind.vault {
            PrivateVaultCard(request: request, client: client, message: message, bubble: bubble)
        } else if let text = part.text {
            Text(text).fixedSize(horizontal: false, vertical: true)
                .padding(12)
                .background(bubble, in: RoundedRectangle(cornerRadius: BubbleStyle.radius))
                .shareMenu(item: .text(text)) { share in
                    reactionMenu
                    Button("Copy", systemImage: "doc.on.doc") { copyText(text) }
                    if let share { share }
                    Button("Reply", systemImage: "arrowshape.turn.up.left") { respond(to: index) }
                }
                .accessibilityElement(children: .ignore)
                .accessibilityLabel(text)
                .accessibilityAddTraits(.isButton)
                .accessibilityAction(named: "Copy") { copyText(text) }
                .accessibilityAction(named: "Reply") { respond(to: index) }
        }
    }

    private func respond(to index: Int) {
        if let quote = message.replyContext(part: index) { respond(quote) }
    }

    private func replyWidth(_ reply: ReplyContext) -> CGFloat {
        if reply.attachment == nil { return BubbleStyle.width }
        guard contentWidth > 0 else { return BubbleStyle.width }
        return max(0, contentWidth - 24)
    }

    private func copyText(_ text: String) {
        #if os(macOS)
        NSPasteboard.general.clearContents()
        NSPasteboard.general.setString(text, forType: .string)
        #else
        UIPasteboard.general.string = text
        #endif
    }
}

private struct TypingRow: View {
    let members: [(Agent, ActivityView)]
    let session: AppSession
    let conversationID: String
    let group: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var displayed: String?
    @State private var changedAt = Date.distantPast
    /// The row's space stays once used, so the row leaving never moves the chat.
    @State private var reserved = false

    private var markSize: CGFloat { group ? 22 : 28 }
    private var text: String { ActivityPhrase.line(members.map { ($0.0.name, $0.1) }) }
    private var line: String { displayed ?? text }

    /// Several active bots take turns as the mark, every two seconds, as Grok Bot does.
    private func mark(at date: Date) -> Agent? {
        guard members.count > 1, !reduceMotion else { return members.first?.0 }
        return members[Int(date.timeIntervalSinceReferenceDate / 2) % members.count].0
    }

    var body: some View {
        Group {
            if !members.isEmpty || reserved {
                ZStack {
                    if !members.isEmpty {
                        HStack(spacing: group ? 8 : 14) {
                            TimelineView(.periodic(from: .now, by: 2)) { context in
                                let mark = mark(at: context.date)
                                ZStack {
                                    if let mark {
                                        AgentAvatar(agentID: mark.id, conversationID: conversationID,
                                                    activity: session.sidebar.activity, connected: true)
                                            .frame(width: markSize * 316 / 200, height: markSize * 316 / 200)
                                            .frame(width: markSize, height: markSize)
                                            .id(mark.id)
                                            .transition(.opacity)
                                    }
                                }
                                .motion(Motion.fade, value: mark?.id)
                            }
                            .frame(width: markSize, height: markSize)
                            ActivityLabel(text: line)
                                .id(line)
                                .lineLimit(1)
                            Spacer(minLength: 0)
                        }
                        .transition(.opacity.combined(with: .scale(scale: 0.92, anchor: .leading)))
                        .accessibilityElement(children: .ignore)
                        .accessibilityLabel(line)
                    }
                }
                .frame(height: 32)
                .onAppear { reserved = true }
            }
        }
        .motion(value: members.map { $0.0.id })
        .task(id: text) {
            guard displayed != text else { return }
            let delay = max(0, 0.8 - Date().timeIntervalSince(changedAt))
            if delay > 0 { try? await Task.sleep(for: .seconds(delay)) }
            guard !Task.isCancelled else { return }
            displayed = text
            changedAt = .now
        }
    }
}

private struct ActivityLabel: View {
    let text: String
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var startedAt = Date()
    @State private var visible = false

    var body: some View {
        Text(text)
            .foregroundStyle(.muted)
            .overlay {
                if !reduceMotion {
                    Text(text).foregroundStyle(.primary)
                        .mask {
                            TimelineView(.animation(minimumInterval: 2.2 / 60, paused: !visible)) { context in
                                GeometryReader { geometry in
                                    let elapsed = max(0, context.date.timeIntervalSince(startedAt))
                                    let phase = floor(elapsed.truncatingRemainder(dividingBy: 2.2) / 2.2 * 60) / 60
                                    LinearGradient(stops: [.init(color: .clear, location: 0.25),
                                                           .init(color: .white, location: 0.6),
                                                           .init(color: .clear, location: 0.75)],
                                                   startPoint: .leading, endPoint: .trailing)
                                        .frame(width: geometry.size.width * 2)
                                        .offset(x: geometry.size.width * (phase * 4 - 2))
                                }
                            }
                        }
                }
            }
            .font(.system(size: 14))
            .onAppear { startedAt = .now; visible = true }
            .onDisappear { visible = false }
    }
}

/// The last measured toolbar height, saved so a new chat draws its header in place from its first frame.
@MainActor enum ToolbarInset {
    private static let key = "toolbarInset"
    #if os(macOS)
    private static let initial: CGFloat = 52
    #else
    private static let initial: CGFloat = 44
    #endif

    static var last: CGFloat = {
        let saved = UserDefaults.standard.double(forKey: key)
        return saved > 0 ? saved : initial
    }() {
        didSet { UserDefaults.standard.set(Double(last), forKey: key) }
    }
}

#if os(macOS)
struct ToolbarAnchor: NSViewRepresentable {
    let changed: (CGFloat) -> Void

    func makeNSView(context: Context) -> ToolbarView { ToolbarView() }

    func updateNSView(_ view: ToolbarView, context: Context) {
        view.changed = changed
        view.measure()
    }
}

final class ToolbarView: NSView {
    var changed: (CGFloat) -> Void = { _ in }
    private var pending = false
    private var lastInset: CGFloat?

    override func layout() { super.layout(); measure() }
    override func viewDidMoveToWindow() { super.viewDidMoveToWindow(); measure() }
    override func viewDidMoveToSuperview() { super.viewDidMoveToSuperview(); measure() }
    override func hitTest(_ point: NSPoint) -> NSView? { nil }

    func measure() {
        guard !pending else { return }
        pending = true
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            pending = false
            guard let window else { return }
            // Inspector resizing can clear SwiftUI's safe-area inset without moving the chat.
            let inset = max(0, window.frame.height - convert(bounds, to: nil).maxY)
            guard inset.isFinite, lastInset.map({ abs($0 - inset) > 0.5 }) ?? true else { return }
            lastInset = inset
            changed(inset)
        }
    }
}
#else
struct ToolbarAnchor: UIViewRepresentable {
    let changed: (CGFloat, CGFloat) -> Void

    func makeUIView(context: Context) -> ToolbarView {
        let view = ToolbarView()
        view.isUserInteractionEnabled = false
        view.isAccessibilityElement = false
        return view
    }

    func updateUIView(_ view: ToolbarView, context: Context) {
        view.changed = changed
        view.measure()
    }
}

final class ToolbarView: UIView {
    var changed: (CGFloat, CGFloat) -> Void = { _, _ in }
    private var pending = false
    private var lastTop: CGFloat?
    private var lastSafeTop: CGFloat?

    override func layoutSubviews() { super.layoutSubviews(); measure() }
    override func didMoveToWindow() { super.didMoveToWindow(); measure() }
    override func didMoveToSuperview() { super.didMoveToSuperview(); measure() }
    override func safeAreaInsetsDidChange() { super.safeAreaInsetsDidChange(); measure() }

    func measure() {
        guard !pending else { return }
        pending = true
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            pending = false
            guard let window else { return }
            let top = convert(bounds, to: window).minY
            let safeTop = window.safeAreaInsets.top
            guard top.isFinite, safeTop.isFinite,
                  (lastTop.map { abs($0 - top) > 0.5 } ?? true) ||
                  (lastSafeTop.map { abs($0 - safeTop) > 0.5 } ?? true) else { return }
            lastTop = top
            lastSafeTop = safeTop
            changed(top, safeTop)
        }
    }
}
#endif
