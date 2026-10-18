import SwiftUI
import PolymuxKit
#if os(macOS)
import AppKit
#else
import UIKit
#endif

struct ChatPane: View {
    let agent: Agent
    let thread: PolymuxKit.Thread
    let session: AppSession
    let drops: Binding<[URL]>
    @State private var store: ChatStore
    @Environment(\.scenePhase) private var scenePhase
    @State private var showSearch = false
    @State private var searchQuery = ""
    @State private var paneWidth: CGFloat = 720
    @State private var headerHeight: CGFloat = 88
    @State private var toolbarInset: CGFloat = 44
    @FocusState private var searchFocused: Bool
    @State private var showVoice = false
    @State private var voiceStore: (any VoiceSession)?
    @State private var openingVoice = false
    @State private var voiceIssue: String?
    @State private var showProfile = false
    @State private var target: ReplyTarget?
    @State private var highlight: ReplyTarget?
    @State private var jumpID = UUID()
    @State private var atBottom = true
    @State private var composerHeight: CGFloat = 70
    @State private var reply: ReplyContext?
    @Namespace private var scrollSpace
    @State private var dateFrames: [String: ScrollDateFrame] = [:]
    @State private var scrollHeight: CGFloat = 0
    @State private var scrollDate: Date?
    @State private var dateVisible = false
    @State private var dateScrolling = false
    @State private var dateTask: Task<Void, Never>?
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    private let mobileEdge: CGFloat = 24

    init(agent: Agent, thread: PolymuxKit.Thread, session: AppSession, drops: Binding<[URL]>) {
        self.agent = agent
        self.thread = thread
        self.session = session
        self.drops = drops
        _store = State(initialValue: ChatStore(client: session.client, actor: session.sidebar.cacheActor, threadID: thread.id, sidebar: session.sidebar))
    }

    var body: some View {
        VStack(spacing: 0) {
            if showSearch {
                SearchView(session: session, threadID: thread.id, agentName: chatTitle,
                           query: $searchQuery, topInset: topInset, bottomInset: 0,
                           close: closeSearch) { messageID in
                    jump(to: messageID)
                    closeSearch()
                }
                .overlay { edgeFades }
            } else {
                transcript
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
        .alert("Couldn’t open voice", isPresented: Binding(get: { voiceIssue != nil }, set: { if !$0 { voiceIssue = nil } })) {
            Button("OK") { voiceIssue = nil }
        } message: { Text(voiceIssue ?? "") }
        #if os(iOS)
        .simultaneousGesture(TapGesture().onEnded { searchFocused = false }, isEnabled: showSearch)
        #endif
        .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { paneWidth = $0 }
        #if os(macOS)
        .onGeometryChange(for: CGFloat.self) { $0.safeAreaInsets.top } action: { toolbarInset = $0 }
        #endif
        .overlay(alignment: .bottom) {
            composer
                .opacity(showSearch || showVoice ? 0 : 1)
                .allowsHitTesting(!showSearch && !showVoice)
                .accessibilityHidden(showSearch || showVoice)
        }
        .overlay {
            if showVoice, let voiceStore {
                VoiceOverlay(store: voiceStore, dismiss: { showVoice = false })
                    .padding(.top, max(0, headerHeight - toolbarInset) + 8)
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
                               conversationID: thread.id, runs: store.runs, activity: store.activity,
                                connected: store.isConnected, speakingID: showVoice && voiceStore?.phase == .speaking ? voiceStore?.agentID : nil,
                               edit: { showProfile = true })
                        .padding(.top, 8)
                        .offset(y: -toolbarInset)
                        .accessibilityLabel(thread.isGroup ? "Edit chat" : "Edit \(agent.name)’s profile")
                        .help(thread.isGroup ? "Edit chat" : "Edit agent profile")
                        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { headerHeight = $0 }
                }
                #if os(iOS)
                .background {
                    ToolbarAnchor { top, safeTop in alignHeader(top: top, safeTop: safeTop) }
                }
                #endif
            }
        }
        .toolbar {
            ToolbarItem(placement: .principal) {
                if showSearch {
                    searchField
                        .onGeometryChange(for: CGFloat.self, of: { $0.size.height }) { headerHeight = $0 }
                } else {
                    // The overlapping avatar occupies the native toolbar's hit region.
                    Button { showProfile = true } label: {
                        Color.clear.frame(width: 80, height: 44).contentShape(Rectangle())
                    }
                    .buttonStyle(.plain).accessibilityHidden(true)
                }
            }
            .sharedBackgroundVisibility(.hidden)
            ToolbarItem(placement: .primaryAction) {
                if showSearch {
                    Button("Close search", systemImage: "xmark", action: closeSearch)
                        .keyboardShortcut(.escape, modifiers: [])
                } else {
                    Button("Speech conversation", systemImage: "phone.fill", action: openVoice)
                        .disabled(openingVoice || showVoice)
                }
            }
        }
        .panel(isPresented: $showProfile) {
            if thread.isGroup { GroupEditor(thread: thread, session: session) }
            else {
                ProfileView(agent: agent, session: session, threadID: thread.id,
                            attachments: store.messages.flatMap { $0.parts.compactMap(\.attachment) },
                            jump: { jump(to: $0) })
            }
        }
        .task {
            store.isVisible = scenePhase == .active
            store.onUpdate = { event, tap in session.sidebar.note(event, at: tap) }
            await store.start()
            await store.markRead()
        }
        .task(id: agent.id) { await offerFocus() }
        .onDisappear { store.stop(); clearDate(); highlight = nil; target = nil }
        .onChange(of: scenePhase) { _, phase in
            store.isVisible = phase == .active
            if store.isVisible { Task { await store.markRead() } }
        }
    }

    private var members: [Agent] {
        thread.participantIDs.compactMap { id in session.sidebar.agents.first { $0.id == id } }
    }

    #if os(iOS)
    private func alignHeader(top: CGFloat, safeTop: CGFloat) {
        let inset = max(0, top + 8 - safeTop)
        guard abs(toolbarInset - inset) > 0.5 else { return }
        toolbarInset = inset
    }
    #endif

    private var chatTitle: String { thread.isGroup ? thread.title : agent.name }

    private var headerRole: String {
        thread.isGroup ? "" : agent.role.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    private func author(_ message: Message) -> String {
        session.sidebar.agents.first { $0.id == message.agentID }?.name ?? agent.name
    }

    private var searchField: some View {
        HStack(spacing: 8) {
            Image(systemName: "magnifyingglass").foregroundStyle(.secondary)
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

    private var topInset: CGFloat { showSearch ? 8 : max(0, headerHeight - toolbarInset) + 8 }

    private var composer: some View {
        VStack(spacing: 4) {
            ZStack(alignment: .bottom) {
                ComposerView(client: session.client,
                             isVoiceActive: showVoice || showSearch || openingVoice,
                             send: { text, attachments, quote, mentions in
                                 await store.send(text: text, attachments: attachments, reply: quote, mentions: mentions)
                                 if reply == quote { reply = nil }
                             },
                             reply: reply, agentName: reply.flatMap { quote in session.sidebar.agents.first { $0.id == quote.agentID }?.name } ?? agent.name,
                             clearReply: { reply = nil }, drops: drops, members: thread.isGroup ? members : [])
                    .frame(height: showVoice ? 0 : nil)
                    .opacity(showVoice ? 0 : 1)
                    .allowsHitTesting(!showVoice)
                    .accessibilityHidden(showVoice)
            }
        }
        .frame(maxWidth: 720)
        #if os(iOS)
        .padding(.horizontal, mobileEdge).padding(.bottom, mobileEdge).padding(.top, 10)
        #else
        .padding(.horizontal, 20).padding(.bottom, 14).padding(.top, 10)
        #endif
        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { composerHeight = $0 }
    }

    private func openVoice() {
        guard !openingVoice, !showVoice else { return }
        openingVoice = true
        Task {
            defer { openingVoice = false }
            guard await session.settings.synchronize(), let settings = session.settings.value else {
                voiceIssue = session.settings.error ?? "Connect to the agent server and wait for settings to save."
                return
            }
            voiceStore = VoiceConfig(settings: settings).session(client: session.client,
                reply: VoiceBridge(client: session.client, threadID: thread.id),
                agent: thread.isGroup ? members.map { "\($0.name) (\($0.id))" }.joined(separator: ", ") : "\(agent.name) (\(agent.id))"
            )
            showVoice = true
        }
    }

    private var transcript: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: 12) {
                    if !store.messages.isEmpty && store.hasEarlier {
                        Button("Earlier messages") { Task { await store.loadEarlier() } }
                            .font(.caption).foregroundStyle(.secondary)
                            .buttonStyle(.plain).padding(.top, 12)
                    }
                    ForEach(Array(store.messages.enumerated()), id: \.element.id) { index, message in
                        if startsGroup(index) {
                            ChatStatus(date: message.date)
                        }
                        MessageBubble(message: message, client: session.client, agentName: author(message),
                                      showAuthor: thread.isGroup && message.role == "assistant",
                                      replyName: message.reply.flatMap { quote in session.sidebar.agents.first { $0.id == quote.agentID }?.name } ?? agent.name,
                                      sending: store.isSending, retry: { await store.retry(messageID: message.id) },
                                      respond: { reply = $0 }, jump: { jump(to: $0.messageID, part: $0.partIndex) },
                                      ask: { text in Task { await store.send(text: text) } },
                                       highlight: highlight, activity: bubbleActivity(message))
                        .id(message.id)
                        .modifier(JumpGlow(active: highlight == ReplyTarget(id: message.id, part: nil)))
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
                    ForEach(members, id: \.id) { member in
                        let presentation = store.activity.view(conversationID: thread.id, agentID: member.id, connected: store.isConnected)
                        if presentation.label != "Ready" && store.isConnected && !store.messages.contains(where: { $0.agentID == member.id && $0.status == "streaming" && $0.text.isEmpty }) {
                            HStack(spacing: 8) {
                                if presentation.typing { ThinkingDots() }
                                Text("\(thread.isGroup ? member.name + ": " : "")\(presentation.label)")
                                    .font(.caption).foregroundStyle(.secondary)
                                Spacer()
                            }
                            .accessibilityElement(children: .combine)
                        }
                    }
                    Color.clear.frame(height: 1).id("bottom")
                }
                .frame(maxWidth: 720)
                .padding(.horizontal, 20).padding(.vertical, 14)
                .frame(maxWidth: .infinity)
            }
            .coordinateSpace(name: scrollSpace)
            .scrollIndicators(.hidden)
            .scrollEdgeEffectHidden()
            .contentMargins(.top, topInset, for: .scrollContent)
            .contentMargins(.bottom, composerHeight, for: .scrollContent)
            .defaultScrollAnchor(.bottom)
            .defaultScrollAnchor(.top, for: .alignment)
            .onPreferenceChange(ScrollDateFrames.self) { frames in
                dateFrames = frames
                updateDate()
            }
            .onScrollGeometryChange(for: CGFloat.self) { $0.containerSize.height } action: { _, height in
                scrollHeight = height
                updateDate()
            }
            .onScrollPhaseChange { _, phase in datePhase(phase) }
            .onScrollGeometryChange(for: Bool.self) { geometry in
                geometry.contentOffset.y + geometry.containerSize.height >= geometry.contentSize.height - 90
            } action: { _, bottom in atBottom = bottom }
            .onScrollGeometryChange(for: Bool.self) { geometry in
                geometry.contentOffset.y < 20 && geometry.contentSize.height > geometry.containerSize.height
            } action: { old, top in
                if top && !old { Task { await store.loadEarlier() } }
            }
            .onChange(of: store.messages.last?.text) { _, _ in
                if atBottom { proxy.scrollTo("bottom", anchor: .bottom) }
            }
            .onChange(of: store.messages.count) { _, _ in
                if atBottom { proxy.scrollTo("bottom", anchor: .bottom) }
            }
            .onChange(of: composerHeight) { _, _ in
                updateDate()
                if atBottom { proxy.scrollTo("bottom", anchor: .bottom) }
            }
            .task(id: jumpID) {
                guard let destination = target else { return }
                let id = destination.id
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
                target = nil
                withAnimation(reduceMotion ? nil : .easeOut(duration: 0.2)) {
                    highlight = destination
                    if destination.part == nil { proxy.scrollTo(id, anchor: .center) }
                    else { proxy.scrollTo(destination, anchor: .center) }
                }
                try? await Task.sleep(for: .seconds(1.2))
                guard !Task.isCancelled else { return }
                withAnimation(reduceMotion ? nil : .easeOut(duration: 0.25)) { highlight = nil }
            }
        }
    }

    /// Mirrors Rakazo's focus prompt: offer the deterministic card once the chat settles,
    /// and only while the user has not spoken. The server decides whether it still applies.
    private func offerFocus() async {
        guard !thread.isGroup, store.messages.isEmpty else { return }
        do { try await Task.sleep(for: .seconds(10)) } catch { return }
        guard !Task.isCancelled, store.messages.isEmpty, !store.isSending else { return }
        _ = try? await session.client.requestFocus(agentID: agent.id)
    }

    private func bubbleActivity(_ message: Message) -> ActivityView? {
        guard let runID = message.runID, let run = store.runs.first(where: { $0.id == runID }) else { return nil }
        return ActivityStore.present(runs: [run], conversationID: thread.id, agentID: run.agentID, connected: store.isConnected)
    }

    private func updateDate() {
        let top = max(0, headerHeight - toolbarInset)
        let height = max(0, scrollHeight - composerHeight)
        let first = dateFrames.values
            .filter { $0.frame.maxY > top && $0.frame.minY < height }
            .min { $0.frame.minY < $1.frame.minY }
        scrollDate = first.map { Calendar.current.startOfDay(for: $0.date) }
        if dateScrolling, scrollDate != nil {
            withAnimation(reduceMotion ? nil : .easeOut(duration: 0.18)) { dateVisible = true }
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
                withAnimation(reduceMotion ? nil : .easeOut(duration: 0.25)) { dateVisible = false }
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

    private func startsGroup(_ index: Int) -> Bool {
        if index == 0 { return true }
        let previous = store.messages[index - 1].date
        let current = store.messages[index].date
        return !Calendar.current.isDate(previous, inSameDayAs: current) || current.timeIntervalSince(previous) > 1800
    }

    private func jump(to id: String, part: Int64? = nil) {
        highlight = nil
        target = ReplyTarget(id: id, part: part)
        jumpID = UUID()
    }
}

struct ReplyTarget: Hashable {
    let id: String
    let part: Int64?
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

struct ChatStatus: View {
    let date: Date

    var body: some View {
        Text(date, format: ProgressiveTime(separator: true))
            .font(.caption2.weight(.medium)).foregroundStyle(.secondary)
            .frame(maxWidth: .infinity).padding(.vertical, 14)
    }
}

struct MessageBubble: View {
    @Environment(\.colorScheme) private var colorScheme
    @State private var contentWidth: CGFloat = 0
    let message: Message
    let client: APIClient
    let agentName: String
    var showAuthor = false
    var replyName = ""
    var sending = false
    let retry: () async -> Void
    let respond: (ReplyContext) -> Void
    let jump: (ReplyContext) -> Void
    /// Sends text a card produced through `polymuxSend` to the agent as the user's next message.
    let ask: (String) -> Void
    var highlight: ReplyTarget?
    var activity: ActivityView?

    var body: some View {
        HStack(alignment: .bottom, spacing: 36) {
            if message.role == "user" { Spacer(minLength: 0) }
            VStack(alignment: message.role == "user" ? .trailing : .leading, spacing: 6) {
                if showAuthor {
                    Text(agentName).font(.caption.weight(.medium)).foregroundStyle(.secondary).padding(.horizontal, 12)
                }
                VStack(alignment: message.role == "user" ? .trailing : .leading, spacing: 12) {
                    ForEach(displayParts, id: \.offset) { index, part in
                        partView(part, index: index)
                            .id(ReplyTarget(id: message.id, part: Int64(index)))
                            .modifier(JumpGlow(active: highlight == ReplyTarget(id: message.id, part: Int64(index))))
                            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width in
                                if index == displayParts.last?.offset { contentWidth = width }
                            }
                    }
                    if displayParts.isEmpty {
                        VStack(alignment: .leading, spacing: 10) {
                            if message.status == "streaming", activity?.typing == true {
                                ThinkingDots()
                            } else if message.status == "streaming" {
                                Text(activity?.label ?? "Receiving reply").foregroundStyle(.secondary)
                            } else {
                                Text(message.status == "error" ? "Reply failed" : "Reply stopped")
                                    .foregroundStyle(.secondary)
                            }
                        }
                        .padding(12)
                        .background(bubble, in: RoundedRectangle(cornerRadius: BubbleStyle.radius))
                    }
                }
                if let reply = message.reply {
                    ReplyBubble(reply: reply, author: reply.role == "user" ? "You" : replyName,
                                client: client, jump: { jump(reply) },
                                width: replyWidth(reply))
                }
                if message.role == "user", ["pending", "queued", "sending", "failed", "error"].contains(message.status) {
                    if sending && ["pending", "queued", "sending"].contains(message.status) {
                        Text("Sending…").font(.caption2).foregroundStyle(.secondary)
                    } else {
                        Button("Unsent", systemImage: "arrow.clockwise") { Task { await retry() } }
                            .font(.caption).buttonStyle(.plain).foregroundStyle(.secondary)
                            .accessibilityLabel("Unsent. Resend message")
                            .disabled(sending)
                    }
                } else if message.status == "error" {
                    Text("Couldn’t complete this reply").font(.caption2).foregroundStyle(.secondary)
                }
            }
            .frame(maxWidth: BubbleStyle.width, alignment: message.role == "user" ? .trailing : .leading)
            if message.role != "user" { Spacer(minLength: 0) }
        }
    }

    private var bubble: Color {
        message.role == "user" ? Color("UserBubble") : BubbleStyle.incoming(colorScheme)
    }

    private var displayParts: [(offset: Int, element: Part)] {
        let parts = Array(message.parts.enumerated())
        let attachments = parts.filter { $0.element.attachment != nil }
        let choices = parts.filter { $0.element.kind == "choice" }
        // A reply still being written is shown whole when it finishes, not as it arrives.
        // Its text is still recorded, so a stopped reply keeps what it managed to say.
        if message.status == "streaming" { return attachments + choices }
        return attachments + choices +
            parts.filter { $0.element.attachment == nil && $0.element.kind != "choice" && !($0.element.text ?? "").isEmpty }
    }

    @ViewBuilder private func partView(_ part: Part, index: Int) -> some View {
        if let attachment = part.attachment {
            MediaView(attachment: attachment, client: client, bubbleColor: bubble,
                      respond: { respond(to: index) }, action: ask)
        } else if part.kind == "choice" {
            FocusCard(part: part, client: client, agentID: message.agentID ?? "", bubble: bubble)
        } else if let text = part.text {
            Text(text).fixedSize(horizontal: false, vertical: true)
                .padding(12)
                .background(bubble, in: RoundedRectangle(cornerRadius: BubbleStyle.radius))
                .shareMenu(item: .text(text)) { share in
                    Button("Copy", systemImage: "doc.on.doc") { copyText(text) }
                    if let share { share }
                    Button("Reply", systemImage: "arrowshape.turn.up.left") { respond(to: index) }
                }
        }
    }

    private func respond(to index: Int) {
        if let quote = message.replyContext(part: index) { respond(quote) }
    }

    private func replyWidth(_ reply: ReplyContext) -> CGFloat {
        guard contentWidth > 0 else { return BubbleStyle.width }
        let limit = max(0, contentWidth - 24)
        return reply.attachment == nil ? max(160, limit) : limit
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

private struct ThinkingDots: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.scenePhase) private var scene
    @State private var visible = false

    var body: some View {
        TimelineView(.animation(minimumInterval: 1.0 / 30, paused: reduceMotion || !visible || scene != .active)) { context in
            HStack(spacing: 5) {
                ForEach(0..<3) { index in
                    let phase = context.date.timeIntervalSinceReferenceDate * 4 - Double(index) * 0.7
                    let pulse = reduceMotion ? 0.5 : (sin(phase) + 1) / 2
                    Circle().fill(.secondary)
                        .frame(width: 7, height: 7)
                        .opacity(0.4 + pulse * 0.6)
                        .scaleEffect(0.85 + pulse * 0.15)
                }
            }
            .frame(width: 34, height: 20)
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Thinking")
        .onAppear { visible = true }
        .onDisappear { visible = false }
    }
}

#if os(iOS)
private struct ToolbarAnchor: UIViewRepresentable {
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

private final class ToolbarView: UIView {
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
