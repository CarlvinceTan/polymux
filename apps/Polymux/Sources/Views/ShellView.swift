import SwiftUI
import PolymuxVoice
import PolymuxKit
import AvatarKit
import CoreTransferable
#if os(macOS)
import AppKit
#endif

struct ShellView: View {
    let session: AppSession
    let pool: SessionPool
    @Binding var showSettings: Bool
    @State private var selection: String?
    @AppStorage private var lastChat: String?
    /// The open New Bot or New Group Chat form, so a relaunch continues where it left off.
    @AppStorage private var lastCreation: String?
    @AppStorage private var botSection: String?
    /// Whether the chat's side panel was open, so a relaunch reopens it.
    @AppStorage private var lastPanel: Bool
    @Environment(\.scenePhase) private var scenePhase
    @State private var windowID = UUID()
    @State private var visibility = NavigationSplitViewVisibility.all
    @State private var connectionDismissed = false
    @State private var serverUpdating = false
    @State private var searching = false
    @State private var searchTarget: SearchDestination?
    @State private var hover = RowHover()
    @State private var order = ChatOrder()
    @State private var geometry = SidebarGeometry()
    @State private var creation: ChatCreation?
    @State private var editing: AvatarContact?
    @State private var profileOpen = false
    @State private var profileID = UUID()
    @State private var openChat = OpenChat()
    @State private var deleteTarget: AvatarContact?
    @State private var groupDelete: PolymuxKit.Thread?
    @State private var deleting: String?
    @State private var deleteError: String?
    @State private var sectionIssue: String?
    @State private var sectionDelete: PolymuxKit.Section?
    @State private var removed: Set<String> = []
    @State private var sectionID: String?
    @State private var sectionDraft: PolymuxKit.Section?
    @State private var sectionThread: PolymuxKit.Thread?
    @State private var drops: [String: [DroppedFile]] = [:]
    @State private var files: [String: [DraftFile]] = [:]
    @State private var dragging: SidebarItem?
    @State private var dropTarget: SidebarDestination?
    /// Filled only while a drag needs them; a scroll must not update the whole window.
    @State private var dragFrames: [SidebarArea: CGRect] = [:]
    @State private var pinFrames: [SidebarArea: CGRect] = [:]
    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    #if os(iOS)
    @Environment(\.horizontalSizeClass) private var sizeClass
    #endif
    #if os(macOS)
    @State private var sidebar = SidebarState()
    @State private var sidebarOpening = false
    @State private var dropFolder: URL?
    @State private var dropWindow: NSWindow?
    @State private var dropDwell = DropDwell()
    #endif

    @AppStorage("sidebarWidth") private var sidebarWidth = SidebarWidth.initial
    @AppStorage("profileWidth") private var profileWidth = 360.0

    init(session: AppSession, pool: SessionPool, showSettings: Binding<Bool>) {
        self.session = session
        self.pool = pool
        _showSettings = showSettings
        let saved = AppStorage<String?>("lastChat.\(session.address)")
        _lastChat = saved
        _selection = State(initialValue: saved.wrappedValue)
        #if DEBUG
        if CallFixture.enabled { _selection = State(initialValue: "t-chief") }
        #endif
        let creation = AppStorage<String?>("lastCreation.\(session.address)")
        _lastCreation = creation
        _botSection = AppStorage<String?>("botSection.\(session.address)")
        _creation = State(initialValue: creation.wrappedValue.flatMap(ChatCreation.init(rawValue:)))
        let panel = AppStorage(wrappedValue: false, "lastPanel.\(session.address)")
        _lastPanel = panel
        _profileOpen = State(initialValue: panel.wrappedValue)
        #if DEBUG
        // Screenshots of a chat's side panel need it open without a click: --verify-panel <chat id>.
        let arguments = CommandLine.arguments
        #if os(macOS)
        if arguments.contains("--verify-sidebar-collapsed") {
            _visibility = State(initialValue: .detailOnly)
            _sidebar = State(initialValue: SidebarState(stage: .collapsed))
        }
        #endif
        if let flag = arguments.firstIndex(of: "--verify-panel"), flag + 1 < arguments.count {
            _selection = State(initialValue: arguments[flag + 1])
            _profileOpen = State(initialValue: true)
        }
        #endif
    }

    private var chatSelection: Binding<String?> {
        Binding(get: {
            #if os(macOS)
            selectedChat?.id
            #else
            selection
            #endif
        }, set: { value in
            #if os(macOS)
            guard value != nil || conversations.isEmpty else { return }
            #endif
            selection = value
        })
    }

    private var selectedChat: PolymuxKit.Thread? {
        if let task = session.sidebar.displayed(selection), task.isTask { return task }
        let available = conversations
        if let id = session.sidebar.displayed(selection)?.id,
           let thread = available.first(where: { $0.id == id }) { return thread }
        #if os(macOS)
        return available.first
        #else
        return nil
        #endif
    }

    private var widthBinding: Binding<Double> {
        Binding(get: { sidebarWidth }, set: { value in
            let width = min(Double(SidebarWidth.maximum), max(Double(SidebarWidth.minimum), value))
            guard abs(width - sidebarWidth) > 0.5 else { return }
            sidebarWidth = width
        })
    }

    /// The chat's panel: an agent's profile, or a group chat's editor.
    private var profileShown: Bool { profileOpen && selectedChat != nil }

    private var profileBinding: Binding<Bool> {
        Binding(get: { profileOpen }, set: { setPanel(open: $0) })
    }

    private func setPanel(open: Bool) {
        guard open != profileOpen else { return }
        if open { profileID = UUID() }
        withAnimation(Motion.move.unless(reduceMotion)) { profileOpen = open }
    }

    private var tabs: [ServerTab] { pool.tabs }

    /// A tab on another server moves the app there.
    private var tabSelection: Binding<String> {
        Binding(get: { session.address }, set: { id in
            if id != session.address { pool.select(id) }
        })
    }

    /// The iPhone's tab bar replaces the sidebar's switcher.
    private var usesTabBar: Bool {
        #if os(iOS)
        sizeClass == .compact && !tabs.isEmpty
        #else
        false
        #endif
    }

    #if os(macOS)
    private var panelWidth: CGFloat {
        min(PaneWidth.profile, max(PaneWidth.profileMin, CGFloat(profileWidth)))
    }

    private var minimumWidth: CGFloat {
        let left = sidebar.stage == .collapsed || sidebar.stage == .collapsing ? 0 : CGFloat(sidebarWidth)
        let right = profileShown ? panelWidth : 0
        return max(PaneWidth.window, PaneWidth.chat + left + right)
    }
    #endif

    var body: some View {
        root
        #if os(iOS)
        .onChange(of: place) { Keyboard.close() }
        #endif
    }

    /// The chat and side panel on show.
    private var place: [String?] { [selection, profileOpen ? "profile" : nil] }

    /// What + offers.
    private var createActions: [CreateAction] {
        [botAction(), CreateAction(title: "New Group Chat", symbol: "person.2", icon: .group) { creation = .group }]
    }

    private func botAction(in section: String? = nil) -> CreateAction {
        CreateAction(title: "New Bot", symbol: "face.smiling", icon: .bot) { newBot(in: section) }
    }

    @ViewBuilder private func createRow(_ item: CreateAction) -> some View {
        #if os(iOS)
        if let icon = item.icon {
            Button(action: item.run) {
                Label { Text(item.title) } icon: { icon.image(colorScheme) }
            }
        } else {
            Button(item.title, systemImage: item.symbol, action: item.run)
        }
        #else
        Button(item.title, systemImage: item.symbol, action: item.run)
        #endif
    }

    private func openSearch() {
        searching = true
    }

    @ViewBuilder private var root: some View {
        #if os(iOS)
        if usesTabBar {
            TabView(selection: tabSelection) {
                ForEach(tabs) { tab in
                    Tab(value: tab.id) {
                        // A tab's content doesn't reach the app's host.
                        if tab.id != session.address {
                            Color.clear
                        } else {
                            shell.dropdownHost()
                        }
                    } label: {
                        Label(tab.name, systemImage: ServerSwitcher.symbol(tab, selected: tab.id == tabSelection.wrappedValue))
                            .environment(\.symbolVariants, .none)
                            .accessibilityLabel(ServerSwitcher.title(tab))
                    }
                    .badge(pool.unread(tab))
                }
            }
        } else {
            shell
        }
        #else
        shell
        #endif
    }

    private var shell: some View {
        #if DEBUG
        let _ = RenderCount.hit("shell")
        #endif
        return NavigationSplitView(columnVisibility: $visibility) {
            List(selection: chatSelection) {
                sidebarContent
            }
            .coordinateSpace(name: "chatList")
            .onPreferenceChange(SidebarFrames.self) { frames in
                geometry.areas = frames
                if dragging != nil { dragFrames = frames }
            }
            .overlay {
                GeometryReader { geometry in
                    let origin = geometry.frame(in: .named("chatList")).origin
                    let frames = dropFrames(origin: origin, width: geometry.size.width)
                    Color.clear.contentShape(Rectangle())
                        .onDrop(of: [SidebarItem.contentType.identifier],
                                delegate: SidebarDrop(item: dragging, origin: origin, frames: frames,
                                                      sections: dropSections.map(\.id), pins: pinnedChats.map(\.id),
                                                      target: $dropTarget, receive: dropSidebar))
                        .allowsHitTesting(dragging != nil)
                    SidebarFeedback(frames: frames, target: dropTarget)
                        .offset(x: -origin.x, y: -origin.y)
                    if pinZoneShown {
                        Text("Pinned").foregroundStyle(.secondary)
                            .frame(maxWidth: .infinity).frame(height: pinZone)
                            .background(sidebarSurface)
                            .allowsHitTesting(false)
                    }
                }
            }
            .onChange(of: dragging) { _, active in
                if active == nil { dropTarget = nil; dragFrames = [:]; pinFrames = [:] }
                else {
                    dragFrames = geometry.areas
                    pinFrames = geometry.areas.filter { if case .pin = $0.key { true } else { false } }
                }
            }
            .overlayPreferenceValue(AvatarAnchors.self) { anchors in
                HandoffOverlay(handoffs: session.sidebar.activity.handoffs, anchors: anchors)
            }
            .appMenu {
                Button("Create section", systemImage: "folder.badge.plus") { createSection() }
                    .disabled(session.layout.isSaving)
            }
            #if os(macOS)
            .listStyle(.sidebar)
            .overlay { ChatDropZone(drop: chatDrop) }
            .background { DropOrigin { dropWindow = $0 }.frame(width: 0, height: 0) }
            .onPreferenceChange(ChatFrames.self) { geometry.chats = $0 }
            .tint(Color(white: colorScheme == .dark ? 0.24 : 0.87))
            .scrollContentBackground(.hidden)
            .scrollEdgeEffectHidden()
            .background {
                sidebarSurface
                    .ignoresSafeArea(.container, edges: [.top, .bottom])
            }
            #else
            .listStyle(.plain)
            #endif
            .scrollIndicators(.hidden)
            #if os(iOS)
            .overlay {
                if session.sidebar.isConnected && session.sidebar.chats.isEmpty && dropSections.isEmpty {
                    Text("No chats yet.").foregroundStyle(.muted)
                }
            }
            #endif
            #if os(macOS)
            .sidebarFade(bottom: tabs.isEmpty ? SidebarFade.plain : SidebarFade.switcher)
            #endif
            // Floats over the list, so a note arriving never pushes the chats down.
            .overlay(alignment: .bottom) {
                VStack(spacing: 8) {
                    if !usesTabBar { sidebarNote }
                    if selectedChat == nil {
                        ConnectionStatus(connected: session.sidebar.isConnected, dismissed: $connectionDismissed)
                    }
                }
                .padding(.horizontal, 12).padding(.bottom, 8)
                .motion(Motion.fade, value: sidebarIssue)
            }
            #if os(iOS)
            // The tab bar owns the bottom edge, so the note floats under the title bar instead.
            .overlay(alignment: .top) {
                VStack {
                    if usesTabBar { sidebarNote }
                }
                .padding(.horizontal, 12).padding(.top, 8)
                .motion(Motion.fade, value: sidebarIssue)
                .allowsHitTesting(false)
            }
            #endif
            .safeAreaInset(edge: .bottom, spacing: 0) {
                if !tabs.isEmpty && !usesTabBar {
                    ServerSwitcher(tabs: tabs, selection: tabSelection,
                                   unread: Dictionary(uniqueKeysWithValues: tabs.map { ($0.id, pool.unread($0)) }))
                }
            }
            #if os(macOS)
            .navigationTitle("")
            #else
            .navigationTitle("Chats")
            #endif
            #if os(iOS)
            .navigationBarTitleDisplayMode(.inline)
            #endif
            .navigationSplitViewColumnWidth(min: SidebarWidth.minimum, ideal: max(SidebarWidth.minimum, CGFloat(sidebarWidth)), max: SidebarWidth.maximum)
            .background {
                #if os(macOS)
                SidebarLimits(width: widthBinding, state: sidebar, minimum: minimumWidth)
                    .frame(width: 0, height: 0)
                #endif
            }
            .toolbar {
                #if os(macOS)
                ToolbarItem(placement: .navigation) {
                    // Keep the titlebar tall enough before a chat supplies toolbar items.
                    Color.clear.frame(width: 1, height: 36)
                        .allowsHitTesting(false).accessibilityHidden(true)
                }
                .sharedBackgroundVisibility(.hidden)
                #else
                ToolbarItem(placement: .topBarLeading) {
                    toolbarButton("Settings", symbol: "gearshape") { showSettings = true }
                }
                .sharedBackgroundVisibility(.hidden)
                ToolbarItem(placement: .topBarTrailing) {
                    toolbarButton("Search", symbol: "magnifyingglass") { openSearch() }
                }
                .sharedBackgroundVisibility(.hidden)
                ToolbarItem(placement: .topBarTrailing) {
                    Menu("New chat", systemImage: "plus") {
                        ForEach(createActions, id: \.title) { createRow($0) }
                    }
                    .labelStyle(.iconOnly)
                    .font(.system(size: 18, weight: .regular))
                    .frame(width: 44, height: 44)
                    .contentShape(Circle())
                    .buttonStyle(.plain)
                    .glassEffect(.regular.interactive(), in: Circle())
                    .accessibilityLabel("New chat")
                }
                #endif
            }
            .toolbar(removing: .sidebarToggle)
            .toolbar(removing: .title)
        } detail: {
            if let thread = selectedChat,
               let agent = participants(thread).first {
                chatPane(thread, agent: agent)
            } else if let notice = session.sidebar.notice {
                Text(notice).foregroundStyle(.muted).multilineTextAlignment(.center)
                    .padding(.horizontal, 24)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                VStack(spacing: 16) {
                    // Waits for the server, so a first launch never claims there are no chats.
                    if session.sidebar.isConnected && session.sidebar.chats.isEmpty {
                        Text("No chats yet.").foregroundStyle(.muted)
                        Button("New Bot", systemImage: "plus") { newBot() }
                            .buttonStyle(.glass)
                    }
                }
                .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
        }
        .navigationSplitViewStyle(.balanced)
        .environment(\.connectionDismissed, $connectionDismissed)
        .environment(\.serverUpdating, serverUpdating)
        .task {
            // Quiet check; the notice also covers the restart that follows, when the server can't answer.
            var last = Date.distantPast
            while !Task.isCancelled {
                if await session.client.serverUpdating() { last = .now }
                serverUpdating = Date.now.timeIntervalSince(last) < 60 && (Date.now.timeIntervalSince(last) < 12 || !session.sidebar.isConnected)
                try? await Task.sleep(for: .seconds(5))
            }
        }
        #if os(macOS)
        .frame(minWidth: 0, maxWidth: .infinity, maxHeight: .infinity)
        .panelBarHost()
        #endif
        .navigationTitle("")
        #if DEBUG
        .background { RenderProbe() }
        #endif
        .background {
            Text("Application status")
                .font(.system(size: 1)).opacity(0.01)
                .accessibilityIdentifier("app.status")
                .accessibilityValue("connected=\(session.sidebar.isConnected ? 1 : 0), cache=\(session.sidebar.error == nil && !session.sidebar.chats.isEmpty ? "warm" : "cold")")
        }
        .onChange(of: selection, initial: true) { _, value in
            if let thread = session.sidebar.displayed(value) {
                lastChat = thread.id
            }
            Notifications.shared.visible(scenePhase == .active ? selectedChat?.id : nil, window: windowID)
            warmNeighbours()
        }
        .task {
            openChat.show = { id in
                #if os(iOS)
                profileBinding.wrappedValue = false
                #endif
                selection = id
            }
            PointerIntent.start()
            hover.settled = { [session] id in
                if id != session.sidebar.displayed(selection)?.id { session.warm(id) }
            }
            // Rows slide to their new place instead of jumping when a section or chat changes.
            let rows: (() -> Void) -> Void = { [reduceMotion] change in
                withAnimation(Motion.move.unless(reduceMotion), change)
            }
            session.layout.animate = rows
            session.sidebar.animate = rows
        }
        .onChange(of: creation) { _, value in lastCreation = value?.rawValue }
        .onChange(of: profileOpen) { _, open in lastPanel = open }
        .onChange(of: conversations.map(\.id), initial: true) { _, _ in
            if let thread = selectedChat { selection = thread.id }
        }
        .onChange(of: scenePhase) { _, phase in
            Notifications.shared.visible(phase == .active ? selectedChat?.id : nil, window: windowID)
        }
        .onDisappear { Notifications.shared.visible(nil, window: windowID) }
        .onChange(of: Notifications.shared.routeID, initial: true) { _, _ in
            if let thread = Notifications.shared.take(session.address) { selection = thread }
        }
        .toolbar(removing: .sidebarToggle)
        #if os(macOS)
        .task { dropFolder = try? await DroppedFile.prepare() }
        .toolbarBackgroundVisibility(.hidden, for: .windowToolbar)
        .background {
            SidebarControl(state: sidebar, reveal: showSidebar, search: openSearch, menu: createActions,
                           settings: { showSettings = true }).frame(width: 0, height: 0)
        }
        .onChange(of: sidebar.stage, initial: true) { _, stage in
            var transaction = Transaction(animation: nil)
            transaction.disablesAnimations = true
            withTransaction(transaction) {
                if stage == .expanded { visibility = .all }
                else if stage == .collapsed { visibility = .detailOnly }
                if !stage.moving { sidebarOpening = false }
            }
        }
        #endif
        #if os(macOS)
        .focusedSceneValue(\.searchPanel, $searching)
        .focusedSceneValue(\.toggleSidebar, toggleSidebar)
        #endif
        .panel(isPresented: $searching) {
            SearchPanel(session: session, close: { searching = false }) { result in
                searchTarget = SearchDestination(threadID: result.threadID, messageID: result.messageID)
                selection = result.threadID
                searching = false
            }
        }
        .panel(item: $creation) { kind in
            switch kind {
            case .bot:
                NewBotView(session: session, sectionID: botSection, close: { creation = nil }) { thread in
                    selection = thread.id
                    creation = nil
                }
            case .group:
                NewGroupView(session: session, close: { creation = nil }) { thread in
                    profileOpen = false
                    selection = thread.id
                    creation = nil
                }
            }
        }
        .panel(item: $editing) { contact in
            AvatarEditor(agentID: contact.agent.id, name: contact.agent.name, client: session.client, store: session.avatars)
        }
        .alert("Delete agent?", isPresented: Binding(
            get: { deleteTarget != nil }, set: { if !$0 { deleteTarget = nil } }
        ), presenting: deleteTarget) { contact in
            Button("Delete", role: .destructive) { Task { await delete(contact.agent) } }
            Button("Cancel", role: .cancel) {}
        } message: { contact in
            Text("This will permanently remove \(contact.agent.name) and their conversation.")
        }
        .alert("Delete chat?", isPresented: Binding(get: { groupDelete != nil }, set: { if !$0 { groupDelete = nil } }),
               presenting: groupDelete) { thread in
            Button("Delete", role: .destructive) { Task { await delete(thread) } }
            Button("Cancel", role: .cancel) {}
        } message: { thread in
            Text("This permanently removes \(thread.title) and its messages. Its bots stay available.")
        }
        .alert("Delete “\(sectionDelete?.name ?? "")”?", isPresented: Binding(
            get: { sectionDelete != nil }, set: { if !$0 { sectionDelete = nil } }
        ), presenting: sectionDelete) { section in
            Button("Delete section") { Task { await deleteSection(section, chats: false) } }
            if !chats(in: section.id).isEmpty {
                Button("Delete section and chats", role: .destructive) { Task { await deleteSection(section, chats: true) } }
            }
            Button("Cancel", role: .cancel) {}
        } message: { section in
            let count = chats(in: section.id).count
            if count == 0 {
                Text("This section has no chats.")
            } else {
                Text("Deleting just the section moves its \(count == 1 ? "chat" : "\(count) chats") to the main list. Deleting its chats also permanently removes them, and each bot whose own chat is in it.")
            }
        }
        // A hidden window refreshes rarely, and at once when it shows again.
        .task(id: scenePhase == .background) {
            while !Task.isCancelled {
                await session.sidebar.refresh()
                await session.layout.refresh()
                if session.sidebar.isConnected {
                    session.avatars.sync(agents: session.sidebar.agents)
                    await session.avatars.flush(client: session.client)
                }
                try? await Task.sleep(for: .seconds(scenePhase == .background ? 30 : 3))
            }
        }
    }

    private func chatPane(_ thread: PolymuxKit.Thread, agent: Agent) -> some View {
        let pane = ChatPane(agent: agent, thread: thread, session: session,
                            drops: chatDrops(thread.id), files: chatFiles(thread.id),
                            showProfile: profileBinding, openChat: openChat,
                            destination: searchTarget, store: session.chat(thread.id))
            .id(thread.id)
        #if os(macOS)
        return HStack(spacing: 0) {
            pane.frame(minWidth: PaneWidth.chat, maxWidth: .infinity, maxHeight: .infinity)
            if profileShown {
                ProfilePanel(agent: agent, thread: thread, session: session, openChat: openChat,
                             close: { profileBinding.wrappedValue = false }, jump: openChat.jump)
                    .id(profileID)
                    .frame(width: panelWidth)
                    .background(SideSurface().ignoresSafeArea())
                    .overlay(alignment: .leading) {
                        // The panel's surface runs behind the toolbar, so the divider does too.
                        SideDivider(width: $profileWidth) { profileBinding.wrappedValue = false }
                            .ignoresSafeArea(.container, edges: .top)
                    }
                    .transition(EdgeSlide(width: panelWidth).combined(with: .opacity))
            }
        }
        .frame(minWidth: 0, maxWidth: .infinity, maxHeight: .infinity)
        .navigationSplitViewColumnWidth(min: PaneWidth.chat + (profileShown ? panelWidth : 0),
                                        ideal: 720 + (profileShown ? panelWidth : 0))
        #else
        return pane
        #endif
    }

    @ViewBuilder private var sidebarContent: some View {
        let all = conversations
        let edges = (first: all.first?.id, last: all.last?.id)
        let selected = selectedChat?.id
        let pins = displayedPins
        if !pins.isEmpty {
            PinGrid {
                ForEach(pins) { thread in pinnedLink(thread, selected: selected == thread.id) }
            }
            .padding(.vertical, 6)
            .padding(.bottom, 12)
            .sidebarArea(.pins)
            .listRowInsets(sectionInsets)
            .listRowBackground(sidebarSurface)
            .listRowSeparator(.hidden)
        }
        let sections = dropSections
        // Unassigned only needs a header beside other sections.
        let plain = sections.allSatisfy { $0.id == SidebarLayout.unassignedID }
        ForEach(sections, id: \.id) { section in
            let rows = chats(in: section.id, from: all)
            Section {
                if !plain { sectionHeader(section, count: rows.count, selected: selected == section.id) }
                if plain || !section.collapsed {
                    if rows.isEmpty {
                        Text("No chats yet.")
                            .foregroundStyle(Color.primary.opacity(0.7))
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.vertical, 10)
                            .contentShape(Rectangle())
                            .sidebarArea(section.id == SidebarLayout.unassignedID ? .unassigned : .section(section.id))
                            .listRowBackground(sidebarSurface)
                            .listRowInsets(sectionInsets)
                            .listRowSeparator(.hidden)
                    } else {
                        ForEach(rows) { thread in chatLink(thread, edges: edges, selected: selected == thread.id) }
                    }
                }
            }
            .opacity(dragging == .section(section.id) ? 0.4 : 1)
        }
    }

    /// The drop zone for a first pin. It floats above the list, because inserting
    /// a row would push everything down under the pointer as the drag begins.
    private var pinZoneShown: Bool {
        if case .chat = dragging { pinnedChats.isEmpty } else { false }
    }

    private var pinZone: CGFloat { 32 }

    private func dropFrames(origin: CGPoint, width: CGFloat) -> [SidebarArea: CGRect] {
        var frames = dragFrames.merging(pinFrames) { _, fixed in fixed }
        if pinZoneShown { frames[.pins] = CGRect(x: origin.x, y: origin.y, width: width, height: pinZone) }
        return frames
    }

    private var sidebarIssue: String? {
        #if DEBUG
        // A real failed save needs a refusing server, and refresh clears it within seconds.
        if CommandLine.arguments.contains("--verify-sidebar-note") { return "Couldn’t save that change." }
        #endif
        return deleteError ?? sectionIssue ?? session.layout.issue ?? session.sidebar.commits.notice
    }

    @ViewBuilder private var sidebarNote: some View {
        if let sidebarIssue {
            QuietNote(text: sidebarIssue)
                .task(id: sidebarIssue) {
                    // A failed delete, avatar save or an over-long section name needs no action, so its note fades on its own.
                    guard deleteError != nil || sectionIssue != nil || session.sidebar.commits.notice != nil else { return }
                    try? await Task.sleep(for: .seconds(6))
                    deleteError = nil
                    sectionIssue = nil
                    session.sidebar.commits.clearNotice()
                }
        }
    }

    private var pinnedChats: [PolymuxKit.Thread] { conversations.filter { $0.pinned == true } }

    private var displayedPins: [PolymuxKit.Thread] {
        guard case let .chat(id) = dragging, case let .pin(before) = dropTarget else { return pinnedChats }
        let ids = DropTarget.pins(pinnedChats.map(\.id), moving: id, before: before)
        return ids.compactMap { id in conversations.first { $0.id == id } }
    }

    private var dropSections: [PolymuxKit.Section] {
        let sections: [PolymuxKit.Section]
        if case .chat = dragging { sections = session.layout.sections }
        else { sections = session.layout.visibleSections(from: conversations) }
        return sections + (sectionDraft.map { [$0] } ?? [])
    }

    private var sectionInsets: EdgeInsets {
        #if os(macOS)
        EdgeInsets(top: 0, leading: 4, bottom: 0, trailing: 4)
        #else
        EdgeInsets(top: 0, leading: 20, bottom: 0, trailing: 20)
        #endif
    }

    /// A section's chats, keeping the conversation list's newest-first order.
    private func chats(in sectionID: String?, from all: [PolymuxKit.Thread]? = nil) -> [PolymuxKit.Thread] {
        session.layout.chats(in: sectionID, from: all ?? conversations)
    }

    @ViewBuilder private func sectionHeader(_ section: PolymuxKit.Section, count: Int, selected: Bool) -> some View {
        let unassigned = section.id == SidebarLayout.unassignedID
        let area: SidebarArea = unassigned ? .unassigned : .section(section.id)
        let visible = session.layout.visibleSections(from: conversations)
        if sectionID == section.id {
            HStack {
                SectionEditor(name: section.name, save: finishSection, cancel: cancelSection,
                              saveOnBlur: sectionDraft == nil)
                Spacer()
                if section.collapsed { Text(count, format: .number) }
                else { Image(systemName: "chevron.down") }
            }
            .font(.body)
            .fontWeight(.regular)
            .foregroundStyle(Color.primary.opacity(0.7))
            .padding(.vertical, 6)
            .textCase(nil)
            .listRowInsets(sectionInsets)
            .listRowBackground(rowBackground(section.id, selected: selected))
            .listRowSeparator(.hidden)
            .sidebarArea(.section(section.id))
        } else {
            Button {
                Task { await session.layout.collapse(section.id, collapsed: !section.collapsed) }
            } label: {
                HStack {
                    Text(section.name)
                    Spacer()
                    if section.collapsed { Text(count, format: .number) }
                    else { Image(systemName: "chevron.down") }
                }
                .font(.body)
                .fontWeight(.regular)
                .foregroundStyle(Color.primary.opacity(0.7))
                .padding(.vertical, 6)
                .contentShape(Rectangle())
            }
            .buttonStyle(ToggleRow())
            .textCase(nil)
            .listRowInsets(sectionInsets)
            .listRowBackground(rowBackground(section.id, selected: selected))
            .listRowSeparator(.hidden)
            .sidebarArea(area)
            .accessibilityAddTraits(.isHeader)
            .pointerHover { hover.update(section.id, inside: $0) }
            .accessibilityLabel("\(section.collapsed ? "Expand" : "Collapse") \(section.name)")
            .sidebarDrag(.section(section.id), active: $dragging) {
                Text(section.name)
                    .font(.body).lineLimit(1)
                    .padding(.horizontal, 10).padding(.vertical, 6)
                    .frame(maxWidth: 220)
                    .background(sidebarSurface, in: RoundedRectangle(cornerRadius: 8))
                    .overlay { RoundedRectangle(cornerRadius: 8).stroke(Color.primary.opacity(0.12), lineWidth: 0.5) }
            }
            .appMenu(destructive: ["Delete section"]) {
                createRow(botAction(in: section.id))
                Divider()
                if !unassigned {
                    Button("Rename", systemImage: "pencil") {
                        cancelSection()
                        sectionID = section.id
                    }
                    .disabled(session.layout.isSaving)
                    Divider()
                }
                Button("Move up", systemImage: "arrow.up") {
                    Task { await session.layout.shift(section.id, by: -1, among: visible) }
                }
                .disabled(session.layout.isSaving || visible.first?.id == section.id)
                Button("Move down", systemImage: "arrow.down") {
                    Task { await session.layout.shift(section.id, by: 1, among: visible) }
                }
                .disabled(session.layout.isSaving || visible.last?.id == section.id)
                Divider()
                Button("Create section", systemImage: "folder.badge.plus") { createSection() }
                if !unassigned {
                    Divider()
                    Button("Delete section", systemImage: "trash", role: .destructive) { sectionDelete = section }
                        .disabled(session.layout.isSaving)
                }
            }
        }
    }

    private func pinnedLink(_ thread: PolymuxKit.Thread, selected: Bool) -> some View {
        pinControl(thread) {
            VStack(spacing: 6) {
                AvatarStack(members: participants(thread), isGroup: thread.isGroup, size: 76, conversationID: thread.id,
                            runs: thread.runs, activity: session.sidebar.activity, connected: session.sidebar.isConnected,
                            unread: thread.unreadCount > 0)
                Text(title(thread)).font(.callout).foregroundStyle(Color.primary).lineLimit(1)
            }
            .padding(.horizontal, 8)
            .padding(.top, 6)
            .padding(.bottom, 4)
            .frame(minWidth: 0, maxWidth: .infinity)
            .background { RowFill(id: thread.id, selected: selected, hover: hover) }
            .contentShape(RoundedRectangle(cornerRadius: 10))
        }
        .buttonStyle(.plain)
        .navigationLinkIndicatorVisibility(.hidden)
        .accessibilityLabel("\(title(thread)), pinned chat")
        .accessibilityValue(session.sidebar.activity.marker(conversationID: thread.id, agentIDs: participants(thread).map(\.id),
                                                           unread: thread.unreadCount > 0)?.label ?? "")
        .sidebarDrag(.chat(thread.id), active: $dragging) { dragPreview(thread) }
        .sidebarArea(.pin(thread.id))
        .opacity(dragging == .chat(thread.id) ? 0 : 1)
        #if os(macOS)
        .overlay { rowMenu(thread).accessibilityHidden(true) }
        #endif
        .pointerHover { hover.update(thread.id, inside: $0) }
    }

    private func newBot(in section: String? = nil) {
        botSection = section == SidebarLayout.unassignedID ? nil : section
        creation = .bot
    }

    private func createSection(for thread: PolymuxKit.Thread? = nil) {
        guard !session.layout.isSaving else { return }
        let localID = UUID().uuidString
        let names = Set(session.layout.sections.map { $0.name.lowercased() })
        var name = "New section"
        var suffix = 2
        while names.contains(name.lowercased()) {
            name = "New section \(suffix)"
            suffix += 1
        }
        sectionDraft = PolymuxKit.Section(id: localID, name: name)
        sectionThread = thread
        sectionID = localID
    }

    private func finishSection(_ name: String) {
        guard let id = sectionID else { return }
        let draft = sectionDraft
        let thread = sectionThread
        cancelSection()
        Task {
            if let draft {
                guard let created = await session.layout.create(name, id: draft.id) else { return }
                if let thread { await session.layout.move(thread, to: created) }
            } else { await session.layout.rename(id, to: name) }
        }
    }

    private func cancelSection() {
        sectionID = nil
        sectionDraft = nil
        sectionThread = nil
    }

    private func move(_ thread: PolymuxKit.Thread, to sectionID: String?) {
        Task { await session.layout.move(thread, to: sectionID) }
    }

    private func chatDrops(_ id: String) -> Binding<[DroppedFile]> {
        Binding(get: { drops[id] ?? [] }, set: { drops[id] = $0.isEmpty ? nil : $0 })
    }

    private func chatFiles(_ id: String) -> Binding<[DraftFile]> {
        Binding(get: { files[id] ?? [] }, set: { files[id] = $0.isEmpty ? nil : $0 })
    }

    #if os(macOS)
    private var chatDrop: ChatDrop {
        ChatDrop(geometry: geometry, window: dropWindow, dwell: dropDwell, hover: hover,
                 open: { if selection != $0 { selection = $0 } }, attach: attach)
    }

    private func attach(_ id: String) -> Bool {
        guard conversations.contains(where: { $0.id == id }) else { return false }
        selection = id
        guard let folder = dropFolder else { drops[id, default: []].append(.failed); return false }
        let accepted = DroppedFile.receive(from: NSPasteboard(name: .drag), into: folder) {
            drops[id, default: []].append($0)
        }
        if accepted {
            dropFolder = nil
            Task { dropFolder = try? await DroppedFile.prepare() }
        }
        return accepted
    }
    #endif

    private func dropSidebar(_ item: SidebarItem, to destination: SidebarDestination) -> Bool {
        guard !session.layout.isSaving else { return false }
        switch (item, destination) {
        case let (.section(id), .section(target, after)):
            let ordered = dropSections.map(\.id)
            guard id != target, let index = ordered.firstIndex(of: target) else { return false }
            let before = after ? ordered.dropFirst(index + 1).first : target
            Task { await session.layout.reorder(id, before: before) }
        case let (.chat(id), .pin(before)):
            guard let thread = conversations.first(where: { $0.id == id }), id != before else { return false }
            Task { await session.layout.pin(thread, pinned: true, before: before) }
        case let (.chat(id), .chats(area)):
            guard let thread = conversations.first(where: { $0.id == id }) else { return false }
            let sectionID: String?
            switch area {
            case let .section(id): sectionID = id
            case .unassigned: sectionID = nil
            default: return false
            }
            guard thread.pinned == true || thread.sectionID != sectionID else { return false }
            move(thread, to: sectionID)
        default: return false
        }
        return true
    }

    #if os(macOS)
    private func rowMenu(_ thread: PolymuxKit.Thread) -> some View {
        let agent = participants(thread).first
        return RowMenu(profile: { edit(thread) }, pinned: thread.pinned == true,
                       pin: { togglePin(thread) },
                       avatar: thread.isGroup ? nil : { if let agent { editing = AvatarContact(agent: agent) } },
                       delete: { if thread.isGroup { groupDelete = thread } else if let agent { requestDelete(agent) } },
                       title: thread.isGroup ? "Edit chat" : "Edit profile",
                       sections: session.layout.customSections, sectionID: thread.sectionID,
                       move: { move(thread, to: $0) }, create: { createSection(for: thread) })
            .disabled(session.layout.isSaving)
    }
    #endif

    @ViewBuilder private func chatMenu(_ thread: PolymuxKit.Thread) -> some View {
        Button(thread.pinned == true ? "Unpin" : "Pin", systemImage: thread.pinned == true ? "pin.slash" : "pin") {
            togglePin(thread)
        }
        Menu("Move to", systemImage: "folder") {
            ForEach(session.layout.customSections, id: \.id) { section in
                Button { move(thread, to: section.id) } label: {
                    Label(section.name, systemImage: thread.sectionID == section.id ? "checkmark" : "folder")
                }
            }
            if !session.layout.customSections.isEmpty {
                Button("Unassigned") { move(thread, to: nil) }
                Divider()
            }
            Button("Create section", systemImage: "folder.badge.plus") { createSection(for: thread) }
        }
        .disabled(session.layout.isSaving)
        if thread.isGroup {
            Button("Edit chat", systemImage: "pencil") { openProfile(thread) }
            Button("Delete", systemImage: "trash", role: .destructive) { groupDelete = thread }
        } else if let agent = participants(thread).first {
            Button("Edit profile", systemImage: "pencil") { openProfile(thread) }
            Button("Customise avatar", systemImage: "face.smiling") { editing = AvatarContact(agent: agent) }
            Divider()
            Button("Delete", systemImage: "trash", role: .destructive) { requestDelete(agent) }
        }
    }

    private func participants(_ thread: PolymuxKit.Thread) -> [Agent] {
        _ = conversations
        return order.members(thread)
    }

    private func title(_ thread: PolymuxKit.Thread) -> String {
        thread.isGroup ? thread.title : participants(thread).first?.name ?? "Chat"
    }

    #if os(macOS)
    private func toggleSidebar() {
        if sidebar.stage == .expanded { sidebar.collapse() } else { showSidebar() }
    }

    private func showSidebar() {
        guard sidebar.stage == .collapsed, !sidebarOpening else { return }
        sidebarOpening = true
        guard let token = sidebar.beginReveal() else {
            sidebar.stage = .expanding
            visibility = .all
            return
        }
        withAnimation(Animation.easeInOut(duration: Motion.columnTime).unless(reduceMotion)) {
            visibility = .all
        }
        // The binding finishes before the column's slide; settle the stage once it ends.
        let delay = reduceMotion ? 0.05 : Motion.columnTime
        DispatchQueue.main.asyncAfter(deadline: .now() + delay) { sidebar.endReveal(token) }
    }
    #endif

    /// The sidebar's own surface. Row fills composite over it so the system selection fill
    /// cannot show through, and it must match the list background or the rows band visibly.
    private var sidebarSurface: Color {
        #if os(macOS)
        SidebarFade.surface(colorScheme)
        #else
        AppBackground.color(colorScheme)
        #endif
    }

    private func dragPreview(_ thread: PolymuxKit.Thread) -> some View {
        VStack(spacing: 6) {
            AvatarStack(members: participants(thread), isGroup: thread.isGroup, size: 60, conversationID: thread.id,
                        runs: thread.runs, activity: session.sidebar.activity, connected: session.sidebar.isConnected,
                        unread: thread.unreadCount > 0)
            Text(title(thread)).font(.callout).lineLimit(1)
        }
        .padding(8)
        .frame(width: 80)
        .background(sidebarSurface, in: RoundedRectangle(cornerRadius: 10))
        .environment(\.rasterAvatars, true)
        // iOS draws the preview outside this view's hierarchy, so it inherits no environment.
        .environment(session.avatars)
    }

    private func rowBackground(_ id: String, selected: Bool) -> some View {
        ZStack {
            sidebarSurface
            RowFill(id: id, selected: selected, hover: hover)
                .padding(.horizontal, 10).padding(.vertical, 2)
        }
    }

    private func toolbarButton(_ title: String, symbol: String, action: @escaping () -> Void) -> some View {
        #if os(macOS)
        let size: CGFloat = 36
        #else
        let size: CGFloat = 44
        #endif
        return Button(title, systemImage: symbol, action: action)
            .labelStyle(.iconOnly).buttonStyle(.plain)
            .frame(width: size, height: size)
            .contentShape(Circle())
            .glassEffect(.regular.interactive(), in: Circle())
    }

    /// Chats with at least one known agent: pinned first, then newest message first.
    private var conversations: [PolymuxKit.Thread] {
        order.update(threads: session.sidebar.chats, agents: session.sidebar.agents,
                     layout: session.layout, removed: removed)
        return order.chats
    }

    /// The chats an arrow key reaches next, read ahead so stepping through them never waits.
    private func warmNeighbours() {
        let all = conversations
        guard let index = all.firstIndex(where: { $0.id == selectedChat?.id }) else { return }
        for neighbour in [index - 1, index + 1] where all.indices.contains(neighbour) {
            session.warm(all[neighbour].id)
        }
    }

    private func chatLink(_ thread: PolymuxKit.Thread, edges: (first: String?, last: String?), selected: Bool) -> some View {
        chatControl(thread) {
            if thread.isGroup {
                GroupRow(thread: thread, members: participants(thread), activity: session.sidebar.activity,
                         connected: session.sidebar.isConnected, finished: session.sidebar.lastFinished[thread.id])
            } else if let agent = participants(thread).first {
                AgentRow(agent: agent, thread: thread,
                         duplicate: order.repeated.contains(agent.name),
                         activity: session.sidebar.activity, connected: session.sidebar.isConnected,
                         finished: session.sidebar.lastFinished[thread.id])
            }
        }
        .navigationLinkIndicatorVisibility(.hidden)
        .listRowBackground(rowBackground(thread.id, selected: selected))
        .sidebarArea(thread.sectionID.flatMap { id in
            session.layout.sections.contains(where: { $0.id == id }) ? .section(id) : nil
        } ?? .unassigned)
        #if os(macOS)
        // The native sidebar adds 6pt between these insets and the highlight.
        .listRowInsets(EdgeInsets(top: 0, leading: RowDetails.gap - 6, bottom: 0, trailing: RowDetails.gap - 6))
        .overlay {
            rowMenu(thread).accessibilityHidden(true)
        }
        .accessibilityActions {
            Button(thread.pinned == true ? "Unpin" : "Pin") { togglePin(thread) }
            ForEach(session.layout.customSections, id: \.id) { section in
                Button("Move to \(section.name)") { move(thread, to: section.id) }
            }
            if !session.layout.customSections.isEmpty {
                Button("Move to Unassigned") { move(thread, to: nil) }
            }
            Button("Create section") { createSection(for: thread) }
            Button(thread.isGroup ? "Edit chat" : "Edit profile") { edit(thread) }
            if !thread.isGroup, let agent = participants(thread).first {
                Button("Customise avatar") { editing = AvatarContact(agent: agent) }
                Button("Delete") { requestDelete(agent) }
            }
        }
        #else
        .listRowInsets(EdgeInsets(top: 4, leading: 16, bottom: 4, trailing: 16))
        .listRowSeparator(thread.id == edges.first ? .hidden : .automatic, edges: .top)
        .listRowSeparator(thread.id == edges.last ? .hidden : .automatic, edges: .bottom)
        .appMenu {
            chatMenu(thread)
        }
        .swipeActions(edge: .trailing, allowsFullSwipe: false) {
            if !thread.isGroup, let agent = participants(thread).first {
                Button("Delete", systemImage: "trash", role: .destructive) { requestDelete(agent) }.tint(.gray)
            }
            Button("Edit", systemImage: "pencil") { edit(thread) }.tint(.gray.opacity(0.7))
        }
        #endif
        .disabled(deleting == thread.agentID)
        .sidebarDrag(.chat(thread.id), active: $dragging) { dragPreview(thread) }
        .opacity(dragging == .chat(thread.id) ? 0.4 : 1)
        .pointerHover { hover.update(thread.id, inside: $0) }
    }

    /// Pins share one list row, whose context menu and drag are always the first pin's; a Menu is each pin's own.
    @ViewBuilder private func pinControl<Label: View>(_ thread: PolymuxKit.Thread,
                                                     @ViewBuilder label: () -> Label) -> some View {
        #if os(macOS)
        chatControl(thread, label: label)
        #else
        Menu { chatMenu(thread) } label: { label() } primaryAction: { selection = thread.id }
        #endif
    }

    @ViewBuilder private func chatControl<Label: View>(_ thread: PolymuxKit.Thread,
                                                      @ViewBuilder label: () -> Label) -> some View {
        #if os(macOS)
        Button { selection = thread.id } label: {
            label().frame(maxWidth: .infinity).contentShape(Rectangle())
        }
            .background {
                GeometryReader { geometry in
                    Color.clear.preference(key: ChatFrames.self,
                                           value: [thread.id: geometry.frame(in: .named("chatList"))])
                }
            }
            .buttonStyle(.plain)
            .tag(thread.id)
            .accessibilityAddTraits(selectedChat?.id == thread.id ? .isSelected : [])
        #else
        NavigationLink(value: thread.id, label: label)
        #endif
    }

    private func togglePin(_ thread: PolymuxKit.Thread) {
        Task { await session.layout.pin(thread, pinned: thread.pinned != true) }
    }

    private func edit(_ thread: PolymuxKit.Thread) { openProfile(thread) }

    /// Opens the chat with its side panel: the agent's profile, or the group's editor.
    private func openProfile(_ thread: PolymuxKit.Thread) {
        selection = thread.id
        profileBinding.wrappedValue = true
    }

    private func requestDelete(_ agent: Agent) {
        deleteTarget = AvatarContact(agent: agent)
    }

    private func delete(_ thread: PolymuxKit.Thread) async {
        removed.insert(thread.id)
        do {
            _ = try await session.client.deleteThread(id: thread.id)
            await session.sidebar.refresh()
            if selection == thread.id {
                profileBinding.wrappedValue = false
                selection = conversations.first?.id
            }
        } catch {
            removed.remove(thread.id)
            deleteError = error.reason(or: "Couldn’t delete this chat.")
        }
    }

    private func deleteSection(_ section: PolymuxKit.Section, chats: Bool) async {
        let doomed = chats ? self.chats(in: section.id) : []
        // Hide the doomed chats now so they don't flash into the main list.
        removed.formUnion(doomed.map(\.id))
        defer { removed.subtract(doomed.map(\.id)) }
        guard await session.layout.delete(section.id, chats: chats), chats else { return }
        for thread in doomed where !thread.isGroup { session.avatars.remove(id: thread.agentID) }
        await session.sidebar.refresh()
        if !conversations.contains(where: { $0.id == selection }) { selection = conversations.first?.id }
    }

    private func delete(_ agent: Agent) async {
        deleting = agent.id
        defer { deleting = nil }
        // The chat leaves the list at once and comes back if the server refuses.
        let hidden = session.sidebar.thread(for: agent)?.id
        if let hidden { removed.insert(hidden) }
        do {
            try await session.client.deleteAgent(id: agent.id)
            session.avatars.remove(id: agent.id)
            await session.sidebar.refresh()
            if !session.sidebar.threads.contains(where: { $0.id == selection }) { selection = conversations.first?.id }
        } catch {
            if let hidden { removed.remove(hidden) }
            deleteError = error.isOffline ? "You’re offline, so \(agent.name) wasn’t deleted." : error.reason(or: "Couldn’t delete \(agent.name).")
        }
    }
}

#if os(macOS)
/// Finds the chat under a file drag and waits for it to settle before opening it.
@MainActor
private struct ChatDrop {
    let geometry: SidebarGeometry
    let window: NSWindow?
    let dwell: DropDwell
    let hover: RowHover
    let open: (String) -> Void
    let attach: (String) -> Bool
    private static var row: String?

    func update() -> Bool {
        let id = DropTarget.chat(at: NSEvent.mouseLocation, frames: geometry.chats, window: window?.frame ?? .zero)
        if let row = Self.row, row != id { hover.update(row, inside: false) }
        if let id { hover.update(id, inside: true) }
        Self.row = id
        dwell.hold(id, at: NSEvent.mouseLocation, open: open)
        return id != nil
    }

    func exit() {
        if let row = Self.row { hover.update(row, inside: false) }
        Self.row = nil
        dwell.hold(nil, at: .zero, open: open)
    }

    func perform() -> Bool {
        let id = Self.row
        exit()
        guard let id else { return false }
        open(id)
        return attach(id)
    }
}

/// Takes file drags for the whole list. An AppKit view, because the native list never offers them to SwiftUI drop handlers.
private struct ChatDropZone: NSViewRepresentable {
    let drop: ChatDrop

    func makeNSView(context: Context) -> ChatDropView { ChatDropView() }
    func updateNSView(_ view: ChatDropView, context: Context) { view.drop = drop }
}

private final class ChatDropView: NSView {
    var drop: ChatDrop?

    override init(frame: NSRect) {
        super.init(frame: frame)
        registerForDraggedTypes(DroppedFile.types.map { NSPasteboard.PasteboardType($0) })
    }

    required init?(coder: NSCoder) { nil }

    // Clicks and scrolling pass through; AppKit routes drags by registered types instead.
    override func hitTest(_ point: NSPoint) -> NSView? { nil }

    private func accepts(_ info: NSDraggingInfo) -> Bool {
        !(info.draggingPasteboard.types ?? []).contains(NSPasteboard.PasteboardType(SidebarItem.contentType.identifier))
    }

    override func draggingEntered(_ info: NSDraggingInfo) -> NSDragOperation { update(info) }
    override func draggingUpdated(_ info: NSDraggingInfo) -> NSDragOperation { update(info) }
    override func draggingExited(_ info: NSDraggingInfo?) { drop?.exit() }
    override func performDragOperation(_ info: NSDraggingInfo) -> Bool { accepts(info) && drop?.perform() == true }

    private func update(_ info: NSDraggingInfo) -> NSDragOperation {
        accepts(info) && drop?.update() == true ? .copy : []
    }
}

private struct ChatFrames: PreferenceKey {
    static let defaultValue: [String: CGRect] = [:]

    static func reduce(value: inout [String: CGRect], nextValue: () -> [String: CGRect]) {
        value.merge(nextValue(), uniquingKeysWith: { _, latest in latest })
    }
}

private struct DropOrigin: NSViewRepresentable {
    let changed: (NSWindow?) -> Void

    func makeNSView(context: Context) -> DropView { DropView() }

    func updateNSView(_ view: DropView, context: Context) {
        view.changed = changed
        view.measure()
    }
}

private final class DropView: NSView {
    var changed: (NSWindow?) -> Void = { _ in }
    private var pending = false
    private weak var lastWindow: NSWindow?

    override func layout() { super.layout(); measure() }
    override func viewDidMoveToWindow() { super.viewDidMoveToWindow(); measure() }
    override func hitTest(_ point: NSPoint) -> NSView? { nil }

    func measure() {
        guard !pending else { return }
        pending = true
        DispatchQueue.main.async { [weak self] in
            guard let self else { return }
            pending = false
            guard lastWindow !== window else { return }
            lastWindow = window
            changed(window)
        }
    }
}
#endif

private struct PinGrid: Layout {
    private let gap: CGFloat = 4
    private let rowGap: CGFloat = 12

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        guard !subviews.isEmpty else { return .zero }
        let width = proposal.width.flatMap { $0.isFinite ? $0 : nil } ?? CGFloat(subviews.count) * 99 - gap
        let grid = sizing(width: width, subviews: subviews)
        let rows = (subviews.count + grid.columns - 1) / grid.columns
        return CGSize(width: width, height: CGFloat(rows) * (grid.tile.height + rowGap) - rowGap)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        guard !subviews.isEmpty else { return }
        let grid = sizing(width: bounds.width, subviews: subviews)
        let width = CGFloat(grid.columns) * (grid.tile.width + gap) - gap
        let origin = bounds.minX + (bounds.width - width) / 2
        for (index, view) in subviews.enumerated() {
            let point = CGPoint(x: origin + CGFloat(index % grid.columns) * (grid.tile.width + gap),
                                y: bounds.minY + CGFloat(index / grid.columns) * (grid.tile.height + rowGap))
            view.place(at: point, anchor: .topLeading, proposal: ProposedViewSize(grid.tile))
        }
    }

    private func sizing(width: CGFloat, subviews: Subviews) -> (columns: Int, tile: CGSize) {
        let columns = min(subviews.count, max(1, Int((width + gap) / (80 + gap))))
        let tileWidth = min(95, max(0, (width - CGFloat(columns - 1) * gap) / CGFloat(columns)))
        let proposal = ProposedViewSize(width: tileWidth, height: nil)
        let height = subviews.map { $0.sizeThatFits(proposal).height }.max() ?? 0
        return (columns, CGSize(width: tileWidth, height: height))
    }
}

/// Selection reads as a slightly stronger version of hover, not the system's unfocused selection
/// fill, which is far brighter than the row it marks. Reads the hover here so only this row redraws.
private struct RowFill: View {
    let id: String
    let selected: Bool
    let hover: RowHover

    var body: some View {
        let hovered = hover.id == id
        RoundedRectangle(cornerRadius: 10)
            .fill(Color.primary.opacity(selected ? PanelStyle.selectedTint : hovered ? PanelStyle.hoverTint : 0))
            // Answers the pointer at once and lets go softly.
            .motion(hovered ? Motion.instant : Motion.quick, value: hovered)
    }
}

/// A plain button without the pressed dimming, so a toggled row keeps its look under the pointer.
private struct ToggleRow: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View { configuration.label }
}

struct SearchDestination: Equatable {
    let id = UUID()
    let threadID: String
    let messageID: String?
}

private enum ChatCreation: String, Identifiable {
    case bot, group
    var id: Self { self }
}

private struct AvatarContact: Identifiable {
    let agent: Agent
    var id: String { agent.id }
}
