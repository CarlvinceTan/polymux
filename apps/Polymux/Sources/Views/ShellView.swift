import SwiftUI
import PolymuxKit
import AvatarKit

struct ShellView: View {
    let session: AppSession
    @Binding var showSettings: Bool
    @State private var selection: String?
    @AppStorage private var lastChat: String?
    @Environment(\.scenePhase) private var scenePhase
    @State private var windowID = UUID()
    @State private var visibility = NavigationSplitViewVisibility.all
    @State private var searching = false
    @State private var searchTarget: SearchDestination?
    @State private var hovered: String?
    @State private var adding = false
    @State private var editing: AvatarContact?
    @State private var profiling: AvatarContact?
    @State private var grouping: PolymuxKit.Thread?
    @State private var deleteTarget: AvatarContact?
    @State private var deleting: String?
    @State private var deleteError: String?
    @State private var sectionID: String?
    @State private var sectionKey = UUID().uuidString
    @State private var sectionCreation: Task<String?, Never>?
    @State private var dropped: [URL] = []
    @Environment(\.colorScheme) private var colorScheme
    #if os(macOS)
    @State private var sidebar = SidebarState()
    @State private var sidebarOpening = false
    #endif

    @AppStorage("sidebarWidth") private var sidebarWidth = SidebarWidth.initial

    init(session: AppSession, showSettings: Binding<Bool>) {
        self.session = session
        _showSettings = showSettings
        let saved = AppStorage<String?>("lastChat.\(session.address)")
        _lastChat = saved
        _selection = State(initialValue: saved.wrappedValue)
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

    var body: some View {
        NavigationSplitView(columnVisibility: $visibility) {
            List(selection: chatSelection) {
                sidebarContent
            }
            .appMenu {
                Button("Create section", systemImage: "folder.badge.plus") { createSection() }
                    .disabled(session.layout.isSaving)
            }
            #if os(macOS)
            .listStyle(.sidebar)
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
                SidebarLimits(width: widthBinding, state: sidebar).frame(width: 0, height: 0)
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
                    HStack(spacing: 10) {
                        toolbarButton("Search", symbol: "magnifyingglass") { searching = true }
                        toolbarButton("New chat", symbol: "plus") { adding = true }
                    }
                }
                .sharedBackgroundVisibility(.hidden)
                #endif
            }
            .toolbar(removing: .sidebarToggle)
            .toolbar(removing: .title)
        } detail: {
            if let thread = selectedChat,
               let agent = participants(thread).first {
                ChatPane(agent: agent, thread: thread, session: session, drops: $dropped, destination: searchTarget)
                    .id(thread.id)
            } else if let notice = session.sidebar.notice {
                ContentUnavailableView(notice, systemImage: "exclamationmark.bubble")
            } else {
                Color.clear
            }
        }
        .navigationSplitViewStyle(.balanced)
        .navigationTitle("")
        .background {
            Text("Application status")
                .font(.system(size: 1)).opacity(0.01)
                .accessibilityIdentifier("app.status")
                .accessibilityValue("connected=\(session.sidebar.isConnected ? 1 : 0), cache=\(session.sidebar.error == nil && !session.sidebar.threads.isEmpty ? "warm" : "cold")")
        }
        .onChange(of: selection, initial: true) { _, value in
            if let thread = session.sidebar.displayed(value) { lastChat = thread.id }
            Notifications.shared.visible(scenePhase == .active ? selectedChat?.id : nil, window: windowID)
        }
        .onChange(of: conversations.map(\.id), initial: true) { _, _ in
            if let thread = selectedChat { selection = thread.id }
        }
        .onChange(of: scenePhase) { _, phase in
            Notifications.shared.visible(phase == .active ? selectedChat?.id : nil, window: windowID)
        }
        .onDisappear { Notifications.shared.visible(nil, window: windowID) }
        .onChange(of: Notifications.shared.routeID, initial: true) { _, _ in
            if let thread = Notifications.shared.destination { selection = thread }
        }
        #if os(macOS)
        .dropDestination(for: URL.self) { urls, _ in
            let files = urls.filter(\.isFileURL)
            guard !files.isEmpty, let selection = selectedChat?.id,
                  session.sidebar.threads.contains(where: { $0.id == selection }) else { return false }
            dropped.append(contentsOf: files)
            return true
        }
        #endif
        .toolbar(removing: .sidebarToggle)
        #if os(macOS)
        .toolbarBackgroundVisibility(.hidden, for: .windowToolbar)
        .background {
            SidebarControl(state: sidebar, edge: sidebar.edge, expanded: sidebar.stage == .expanded,
                           reveal: showSidebar, search: { searching = true }, add: { adding = true },
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
        .panel(isPresented: $searching) {
            SearchPanel(session: session, close: { searching = false }) { result in
                searchTarget = SearchDestination(threadID: result.threadID, messageID: result.messageID)
                selection = result.threadID
                searching = false
            }
        }
        .panel(isPresented: $adding) {
            NewBotView(session: session, close: { adding = false }) { thread in
                selection = thread.id
                adding = false
            }
        }
        .panel(item: $editing) { contact in
            AvatarEditor(agentID: contact.agent.id, name: contact.agent.name, client: session.client, store: session.avatars)
        }
        .panel(item: $grouping) { thread in
            GroupEditor(thread: thread, session: session)
        }
        .panel(item: $profiling) { contact in
            ProfileView(agent: contact.agent, session: session)
        }
        .alert("Delete agent?", isPresented: Binding(
            get: { deleteTarget != nil }, set: { if !$0 { deleteTarget = nil } }
        ), presenting: deleteTarget) { contact in
            Button("Delete", role: .destructive) { Task { await delete(contact.agent) } }
            Button("Cancel", role: .cancel) {}
        } message: { contact in
            Text("This will permanently remove \(contact.agent.name) and their conversation.")
        }
        .alert("Couldn’t delete agent", isPresented: Binding(
            get: { deleteError != nil }, set: { if !$0 { deleteError = nil } }
        )) {
            Button("OK") { deleteError = nil }
        } message: { Text(deleteError ?? "") }
        .task {
            while !Task.isCancelled {
                await session.sidebar.refresh()
                await session.layout.refresh()
                if session.sidebar.isConnected {
                    session.avatars.sync(agents: session.sidebar.agents)
                    await session.avatars.flush(client: session.client)
                }
                try? await Task.sleep(for: .seconds(3))
            }
        }
    }

    @ViewBuilder private var sidebarContent: some View {
        if let issue = session.layout.issue {
            VStack(alignment: .leading, spacing: 8) {
                Text(issue).font(.footnote).foregroundStyle(.secondary)
                Button("Retry") { Task { await session.layout.retry() } }
                    .disabled(session.layout.isSaving)
            }
            .listRowBackground(sidebarSurface)
        }
        if !pins.isEmpty {
            PinGrid {
                ForEach(pins) { thread in pinnedLink(thread) }
            }
            .padding(.vertical, 6)
            .padding(.bottom, 12)
            .listRowInsets(sectionInsets)
            .listRowBackground(sidebarSurface)
            .listRowSeparator(.hidden)
        }
        ForEach(session.layout.sections.map {
            (id: $0.id == sectionID ? sectionKey : $0.id, section: $0)
        }, id: \.id) { row in
            Section {
                sectionHeader(row.section)
                if !row.section.collapsed {
                    if chats(in: row.section.id).isEmpty {
                        Text("Drag Bots here")
                            .foregroundStyle(Color.primary.opacity(0.7))
                            .frame(maxWidth: .infinity, alignment: .leading)
                            .padding(.vertical, 10)
                            .contentShape(Rectangle())
                            .dropDestination(for: String.self) { items, _ in dropChat(items, to: row.section.id) }
                            .listRowBackground(sidebarSurface)
                            .listRowInsets(sectionInsets)
                            .listRowSeparator(.hidden)
                    } else {
                        ForEach(chats(in: row.section.id)) { thread in chatLink(thread) }
                    }
                }
            }
        }
        if !chats(in: nil).isEmpty {
            Section {
                if !session.layout.sections.isEmpty {
                    Text("Unassigned")
                        .font(.body)
                        .foregroundStyle(Color.primary.opacity(0.7))
                        .frame(maxWidth: .infinity, alignment: .leading)
                        .padding(.vertical, 6)
                        .contentShape(Rectangle())
                        .dropDestination(for: String.self) { items, _ in dropChat(items, to: nil) }
                        .listRowInsets(sectionInsets)
                        .listRowBackground(sidebarSurface)
                        .listRowSeparator(.hidden)
                        .accessibilityAddTraits(.isHeader)
                }
                ForEach(chats(in: nil)) { thread in chatLink(thread) }
            }
        }
    }

    private var pins: [PolymuxKit.Thread] { conversations.filter { $0.pinned == true } }

    private var sectionInsets: EdgeInsets {
        #if os(macOS)
        EdgeInsets(top: 0, leading: 4, bottom: 0, trailing: 4)
        #else
        EdgeInsets(top: 0, leading: 20, bottom: 0, trailing: 20)
        #endif
    }

    private func chats(in sectionID: String?) -> [PolymuxKit.Thread] {
        conversations.filter { thread in
            thread.pinned != true && (thread.sectionID == sectionID ||
                (sectionID == nil && !session.layout.sections.contains(where: { $0.id == thread.sectionID })))
        }.sorted { lhs, rhs in
            let left = lhs.sectionOrder ?? .greatestFiniteMagnitude
            let right = rhs.sectionOrder ?? .greatestFiniteMagnitude
            if left != right { return left < right }
            let leftDate = lhs.lastMessage?.date ?? .distantPast
            let rightDate = rhs.lastMessage?.date ?? .distantPast
            return leftDate == rightDate ? lhs.id < rhs.id : leftDate > rightDate
        }
    }

    @ViewBuilder private func sectionHeader(_ section: PolymuxKit.Section) -> some View {
        if sectionID == section.id {
            HStack {
                SectionEditor(name: section.name, save: finishSection, cancel: { sectionID = nil })
                Spacer()
                if section.collapsed { Text(chats(in: section.id).count, format: .number) }
                else { Image(systemName: "chevron.down") }
            }
            .font(.body)
            .fontWeight(.regular)
            .foregroundStyle(Color.primary.opacity(0.7))
            .padding(.vertical, 6)
            .textCase(nil)
            .listRowInsets(sectionInsets)
            .listRowBackground(rowBackground(section.id))
            .listRowSeparator(.hidden)
        } else {
            Button {
                Task { await session.layout.collapse(section.id, collapsed: !section.collapsed) }
            } label: {
                HStack {
                    Text(section.name)
                    Spacer()
                    if section.collapsed { Text(chats(in: section.id).count, format: .number) }
                    else { Image(systemName: "chevron.down") }
                }
                .font(.body)
                .fontWeight(.regular)
                .foregroundStyle(Color.primary.opacity(0.7))
                .padding(.vertical, 6)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .textCase(nil)
            .listRowInsets(sectionInsets)
            .listRowBackground(rowBackground(section.id))
            .listRowSeparator(.hidden)
            .accessibilityAddTraits(.isHeader)
            .disabled(session.layout.isSaving)
            .onHover { inside in
                if inside { hovered = section.id }
                else if hovered == section.id { hovered = nil }
            }
            .accessibilityLabel("\(section.collapsed ? "Expand" : "Collapse") \(section.name)")
            .draggable("polymux-section:\(section.id)")
            .dropDestination(for: String.self) { items, _ in
                if dropChat(items, to: section.id) { return true }
                guard !session.layout.isSaving, let value = items.first,
                      value.hasPrefix("polymux-section:") else { return false }
                let id = String(value.dropFirst("polymux-section:".count))
                let ordered = session.layout.sections.map(\.id)
                guard let source = ordered.firstIndex(of: id), let target = ordered.firstIndex(of: section.id),
                      source != target else { return false }
                let before = source < target ? ordered.dropFirst(target + 1).first : section.id
                Task { await session.layout.reorder(id, before: before) }
                return true
            }
            .appMenu {
                Button("Rename", systemImage: "pencil") {
                    sectionCreation = nil
                    sectionKey = UUID().uuidString
                    sectionID = section.id
                }
                .disabled(session.layout.isSaving)
                Divider()
                Button("Move up", systemImage: "arrow.up") { Task { await session.layout.shift(section.id, by: -1) } }
                    .disabled(session.layout.isSaving || session.layout.sections.first?.id == section.id)
                Button("Move down", systemImage: "arrow.down") { Task { await session.layout.shift(section.id, by: 1) } }
                    .disabled(session.layout.isSaving || session.layout.sections.last?.id == section.id)
                Divider()
                Button("Create section", systemImage: "folder.badge.plus") { createSection() }
            }
        }
    }

    private func pinnedLink(_ thread: PolymuxKit.Thread) -> some View {
        chatControl(thread) {
            VStack(spacing: 6) {
                AvatarStack(members: participants(thread), size: 76, conversationID: thread.id,
                            runs: thread.runs, activity: session.sidebar.activity, connected: session.sidebar.isConnected)
                Text(title(thread)).font(.callout).foregroundStyle(Color.primary).lineLimit(1)
            }
            .padding(.horizontal, 8)
            .padding(.top, 6)
            .padding(.bottom, 4)
            .frame(minWidth: 0, maxWidth: .infinity)
            .background(rowFill(thread.id), in: RoundedRectangle(cornerRadius: 10))
            .contentShape(RoundedRectangle(cornerRadius: 10))
        }
        .buttonStyle(.plain)
        .accessibilityLabel("\(title(thread)), pinned chat")
        .draggable("polymux-chat:\(thread.id)")
        #if os(macOS)
        .overlay { rowMenu(thread).accessibilityHidden(true) }
        #else
        .appMenu { chatMenu(thread) }
        #endif
        .onHover { hovered = $0 ? thread.id : nil }
    }

    private func createSection(for thread: PolymuxKit.Thread? = nil) {
        guard !session.layout.isSaving else { return }
        let localID = UUID().uuidString
        sectionKey = localID
        sectionID = localID
        let names = Set(session.layout.sections.map { $0.name.lowercased() })
        var name = "New section"
        var suffix = 2
        while names.contains(name.lowercased()) {
            name = "New section \(suffix)"
            suffix += 1
        }
        let initial = name
        sectionCreation = Task {
            guard let id = await session.layout.create(initial, id: localID, accepted: { id in
                if sectionID == localID { sectionID = id }
            }) else {
                if sectionID == localID { sectionID = nil }
                return nil
            }
            if let thread { await session.layout.move(thread, to: id) }
            return id
        }
    }

    private func finishSection(_ name: String) {
        guard let id = sectionID else { return }
        let creation = sectionCreation
        sectionID = nil
        sectionCreation = nil
        Task {
            let savedID: String
            if let creation {
                guard let created = await creation.value else { return }
                savedID = created
            } else { savedID = id }
            await session.layout.rename(savedID, to: name)
        }
    }

    private func move(_ thread: PolymuxKit.Thread, to sectionID: String?) {
        Task { await session.layout.move(thread, to: sectionID) }
    }

    private func dropChat(_ items: [String], to sectionID: String?) -> Bool {
        guard !session.layout.isSaving, let value = items.first, value.hasPrefix("polymux-chat:"),
              let thread = conversations.first(where: { $0.id == String(value.dropFirst("polymux-chat:".count)) }),
              thread.pinned == true || thread.sectionID != sectionID else { return false }
        move(thread, to: sectionID)
        return true
    }

    #if os(macOS)
    private func rowMenu(_ thread: PolymuxKit.Thread) -> some View {
        let agent = participants(thread).first
        return RowMenu(profile: { edit(thread) }, pinned: thread.pinned == true,
                       pin: { togglePin(thread) },
                       avatar: thread.isGroup ? nil : { if let agent { editing = AvatarContact(agent: agent) } },
                       delete: thread.isGroup ? nil : { if let agent { requestDelete(agent) } },
                       title: thread.isGroup ? "Edit chat" : "Edit profile",
                       sections: session.layout.sections, sectionID: thread.sectionID,
                       move: { move(thread, to: $0) }, create: { createSection(for: thread) })
            .disabled(session.layout.isSaving)
    }
    #endif

    @ViewBuilder private func chatMenu(_ thread: PolymuxKit.Thread) -> some View {
        Button(thread.pinned == true ? "Unpin" : "Pin", systemImage: thread.pinned == true ? "pin.slash" : "pin") {
            togglePin(thread)
        }
        Menu("Move to", systemImage: "folder") {
            ForEach(session.layout.sections, id: \.id) { section in
                Button { move(thread, to: section.id) } label: {
                    Label(section.name, systemImage: thread.sectionID == section.id ? "checkmark" : "folder")
                }
            }
            Button("Unassigned") { move(thread, to: nil) }
            Divider()
            Button("Create section", systemImage: "folder.badge.plus") { createSection(for: thread) }
        }
        .disabled(session.layout.isSaving)
        if thread.isGroup {
            Button("Edit chat", systemImage: "pencil") { grouping = thread }
        } else if let agent = participants(thread).first {
            Button("Edit profile", systemImage: "pencil") { profiling = AvatarContact(agent: agent) }
            Button("Customise avatar", systemImage: "face.smiling") { editing = AvatarContact(agent: agent) }
            Divider()
            Button("Delete", systemImage: "trash", role: .destructive) { requestDelete(agent) }
        }
    }

    private func participants(_ thread: PolymuxKit.Thread) -> [Agent] {
        thread.participantIDs.compactMap { id in session.sidebar.agents.first { $0.id == id } }
    }

    private func title(_ thread: PolymuxKit.Thread) -> String {
        thread.isGroup ? thread.title : participants(thread).first?.name ?? "Chat"
    }

    #if os(macOS)
    private func showSidebar() {
        guard sidebar.stage == .collapsed, !sidebarOpening else { return }
        sidebarOpening = true
        if !sidebar.reveal() {
            sidebar.stage = .expanding
            visibility = .all
        }
    }
    #endif

    /// The sidebar's own surface. Row fills composite over it so the system selection fill
    /// cannot show through, and it must match the list background or the rows band visibly.
    private var sidebarSurface: Color {
        #if os(macOS)
        Color(white: colorScheme == .dark ? 38.0 / 255 : 242.0 / 255)
        #else
        AppBackground.color(colorScheme)
        #endif
    }

    /// Selection reads as a slightly stronger version of hover, not the system's
    /// unfocused selection fill, which is far brighter than the row it marks.
    private func rowFill(_ id: String) -> Color {
        if selectedChat?.id == id { return Color.primary.opacity(0.11) }
        if hovered == id { return Color.primary.opacity(0.06) }
        return .clear
    }

    private func rowBackground(_ id: String) -> some View {
        ZStack {
            sidebarSurface
            RoundedRectangle(cornerRadius: 10)
                .fill(rowFill(id))
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

    private var conversations: [PolymuxKit.Thread] {
        session.sidebar.threads.map { session.layout.applying(to: $0) }.filter { !participants($0).isEmpty }.sorted { lhs, rhs in
            switch (lhs.pinRank, rhs.pinRank) {
            case let (left?, right?) where left != right: return left < right
            case (.some, .none): return true
            case (.none, .some): return false
            default: break
            }
            let left = lhs.lastMessage?.date ?? .distantPast
            let right = rhs.lastMessage?.date ?? .distantPast
            if left == right { return lhs.id < rhs.id }
            return left > right
        }
    }

    private func chatLink(_ thread: PolymuxKit.Thread) -> some View {
        chatControl(thread) {
            if thread.isGroup {
                GroupRow(thread: thread, members: participants(thread), activity: session.sidebar.activity, connected: session.sidebar.isConnected)
            } else if let agent = participants(thread).first {
                AgentRow(agent: agent, thread: thread,
                         duplicate: session.sidebar.agents.filter { $0.name == agent.name }.count > 1,
                         activity: session.sidebar.activity, connected: session.sidebar.isConnected)
            }
        }
        .navigationLinkIndicatorVisibility(.hidden)
        .listRowBackground(rowBackground(thread.id))
        #if os(macOS)
        // The native sidebar adds 6pt between these insets and the highlight.
        .listRowInsets(EdgeInsets(top: 0, leading: RowDetails.gap - 6, bottom: 0, trailing: RowDetails.gap - 6))
        .overlay {
            rowMenu(thread).accessibilityHidden(true)
        }
        .accessibilityActions {
            Button(thread.pinned == true ? "Unpin" : "Pin") { togglePin(thread) }
            ForEach(session.layout.sections, id: \.id) { section in
                Button("Move to \(section.name)") { move(thread, to: section.id) }
            }
            Button("Move to Unassigned") { move(thread, to: nil) }
            Button("Create section") { createSection(for: thread) }
            Button(thread.isGroup ? "Edit chat" : "Edit profile") { edit(thread) }
            if !thread.isGroup, let agent = participants(thread).first {
                Button("Customise avatar") { editing = AvatarContact(agent: agent) }
                Button("Delete") { requestDelete(agent) }
            }
        }
        #else
        .listRowInsets(EdgeInsets(top: 4, leading: 16, bottom: 4, trailing: 16))
        .listRowSeparator(thread.id == conversations.first?.id ? .hidden : .automatic, edges: .top)
        .listRowSeparator(thread.id == conversations.last?.id ? .hidden : .automatic, edges: .bottom)
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
        .draggable("polymux-chat:\(thread.id)")
        .dropDestination(for: String.self) { items, _ in dropChat(items, to: thread.sectionID) }
        .onHover { inside in
            if inside { hovered = thread.id }
            else if hovered == thread.id { hovered = nil }
        }
    }

    @ViewBuilder private func chatControl<Label: View>(_ thread: PolymuxKit.Thread,
                                                      @ViewBuilder label: () -> Label) -> some View {
        #if os(macOS)
        Button { selection = thread.id } label: {
            label().frame(maxWidth: .infinity).contentShape(Rectangle())
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

    private func edit(_ thread: PolymuxKit.Thread) {
        if thread.isGroup { grouping = thread }
        else if let agent = participants(thread).first { profiling = AvatarContact(agent: agent) }
    }

    private func requestDelete(_ agent: Agent) {
        deleteTarget = AvatarContact(agent: agent)
    }

    private func delete(_ agent: Agent) async {
        deleting = agent.id
        defer { deleting = nil }
        do {
            try await session.client.deleteAgent(id: agent.id)
            session.avatars.remove(id: agent.id)
            await session.sidebar.refresh()
            if !session.sidebar.threads.contains(where: { $0.id == selection }) { selection = conversations.first?.id }
        } catch { deleteError = error.localizedDescription }
    }
}

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

struct AgentRow: View {
    let agent: Agent
    let thread: PolymuxKit.Thread?
    let duplicate: Bool
    var gap: CGFloat? = nil
    var activity: ActivityStore?
    var connected = true

    var body: some View {
        HStack(spacing: gap ?? RowDetails.gap) {
            AgentAvatar(agentID: agent.id, conversationID: thread?.id ?? agent.id,
                        runs: thread?.runs ?? [], activity: activity, connected: connected,
                        unread: (thread?.unreadCount ?? 0) > 0)
                .frame(width: avatarSize, height: avatarSize)
                .modifier(OnlineDot(shown: agent.active, size: avatarSize))
                #if os(macOS)
                .frame(width: 34)
                #endif
                .accessibilityHidden(true)
            RowDetails(name: agent.name, preview: preview, thread: thread)
        }
        .padding(.vertical, 4)
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(agent.name), \(agent.role), \(preview)")
        .accessibilityValue([agent.active ? "Active" : nil, unread ? "Unread" : nil, presentation.label]
            .compactMap { $0 }.joined(separator: ", "))
    }

    private var unread: Bool { (thread?.unreadCount ?? 0) > 0 }

    private var avatarSize: CGFloat {
        #if os(iOS)
        60
        #else
        52
        #endif
    }

    private var presentation: ActivityView {
        ActivityStore.present(runs: thread?.runs ?? [], conversationID: thread?.id ?? agent.id,
                              agentID: agent.id, connected: connected)
    }

    private var preview: String {
        guard let message = thread?.lastMessage else { return duplicate ? agent.role : "" }
        let text: String
        if let question = message.parts.first(where: { $0.kind == "choice" })?.question, !question.isEmpty {
            text = question
        } else if message.status == "streaming" {
            // The reply is shown whole when it finishes; name its activity until then.
            text = presentation.working ? presentation.label : ""
        } else if !message.text.isEmpty {
            text = message.text.replacingOccurrences(of: "\n", with: " ")
        } else if let name = message.parts.compactMap(\.attachment).first?.name {
            text = name
        } else {
            text = ""
        }
        return duplicate ? "\(agent.role) · \(agent.id.prefix(6)) · \(text)" : text
    }
}

struct GroupRow: View {
    let thread: PolymuxKit.Thread
    let members: [Agent]
    var activity: ActivityStore?
    var connected = true

    var body: some View {
        HStack(spacing: RowDetails.gap) {
            #if os(macOS)
            AvatarStack(members: members, conversationID: thread.id, runs: thread.runs, activity: activity, connected: connected).frame(width: 34)
            #else
            AvatarStack(members: members, size: 60, conversationID: thread.id, runs: thread.runs, activity: activity, connected: connected)
            #endif
            RowDetails(name: thread.title, preview: preview, thread: thread)
        }
        .padding(.vertical, 4)
        .accessibilityElement(children: .combine)
        .accessibilityLabel("\(thread.title), \(members.map(\.name).joined(separator: ", ")), \(preview)")
        .accessibilityValue(thread.unreadCount > 0 ? "Unread" : "")
    }

    private var preview: String {
        guard let message = thread.lastMessage else { return members.map(\.name).joined(separator: ", ") }
        return message.text.isEmpty ? message.attachments.first?.name ?? "Attachment" : message.text.replacingOccurrences(of: "\n", with: " ")
    }
}

private struct RowDetails: View {
    let name: String
    let preview: String
    let thread: PolymuxKit.Thread?

    static var gap: CGFloat {
        #if os(macOS)
        8
        #else
        10
        #endif
    }

    private var foreground: Color {
        #if os(macOS)
        Color(nsColor: .labelColor)
        #else
        .primary
        #endif
    }

    private var secondary: Color {
        #if os(macOS)
        Color(nsColor: .secondaryLabelColor)
        #else
        .secondary
        #endif
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 5) {
            HStack(spacing: 6) {
                Text(name)
                    #if os(iOS)
                    .font(.headline)
                    #else
                    .font(.body.weight(.medium))
                    #endif
                    .foregroundStyle(foreground).lineLimit(1)
                Spacer(minLength: 4)
                if thread?.pinned == true {
                    Image(systemName: "pin.fill")
                        .font(.caption2).foregroundStyle(secondary)
                        .accessibilityLabel("Pinned")
                }
                if let date = thread?.lastMessage?.date {
                    Text(date, format: ProgressiveTime())
                        .font(.caption2).foregroundStyle(secondary).lineLimit(1).fixedSize()
                }
            }
            if !preview.isEmpty || (thread?.unreadCount ?? 0) > 0 {
                HStack(spacing: 6) {
                    Text(preview)
                        #if os(iOS)
                        .font(.subheadline)
                        #else
                        .font(.caption)
                        #endif
                        .lineLimit(1).foregroundStyle(secondary)
                        .frame(maxWidth: .infinity, alignment: .leading)
                    if (thread?.unreadCount ?? 0) > 0 {
                        Circle().fill(.blue).frame(width: 8, height: 8).accessibilityHidden(true)
                    }
                }
            }
        }
    }
}

/// Marks an online agent at the top right of its avatar, with a gap cut into the avatar
/// like overlapping avatars have.
private struct OnlineDot: ViewModifier {
    let shown: Bool
    let size: CGFloat
    private static let green = Color(red: 49 / 255, green: 162 / 255, blue: 76 / 255)

    func body(content: Content) -> some View {
        if shown {
            // The drawn body is 200/316 of the avatar frame; the dot sits on it at 45°.
            let body = size * 200 / 316
            let dot = body * 0.22
            let gap = body * 2 / 46
            let offset = CGSize(width: body / 2 * 0.7071, height: -body / 2 * 0.7071)
            content
                .mask {
                    Hole(center: CGPoint(x: size / 2 + offset.width, y: size / 2 + offset.height), radius: dot / 2 + gap)
                        .fill(style: FillStyle(eoFill: true))
                }
                .overlay {
                    Circle().fill(Self.green).frame(width: dot, height: dot).offset(offset)
                }
        } else {
            content
        }
    }
}

/// The avatar frame with a round hole punched out where the dot sits.
private struct Hole: Shape {
    let center: CGPoint
    let radius: CGFloat

    func path(in rect: CGRect) -> Path {
        var path = Path(rect.insetBy(dx: -rect.width, dy: -rect.height))
        path.addEllipse(in: CGRect(x: center.x - radius, y: center.y - radius, width: radius * 2, height: radius * 2))
        return path
    }
}

struct SearchDestination: Equatable {
    let id = UUID()
    let threadID: String
    let messageID: String?
}

private struct AvatarContact: Identifiable {
    let agent: Agent
    var id: String { agent.id }
}
