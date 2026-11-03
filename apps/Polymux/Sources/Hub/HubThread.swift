import PhotosUI
import SwiftUI
import PolymuxKit
import UniformTypeIdentifiers

/// Hub's content pane: the open chat, broadcast or mail.
struct HubDetail: View {
    @Bindable var store: HubStore
    let client: APIClient

    var body: some View {
        Group {
            if store.source == .mail {
                if let draft = store.mail.compose { HubCompose(store: store, draft: draft).id(draft.localID) }
                else if let open = store.mail.open { HubLetter(store: store, envelope: open) }
                else { Text("No message selected.").foregroundStyle(.muted) }
            } else if store.source == .broadcasts {
                if let list = store.broadcasts.first(where: { $0.id == store.broadcast }) { HubBroadcastThread(store: store, list: list) }
                else { Text("No list selected.").foregroundStyle(.muted) }
            } else if let chat = store.chats.first(where: { $0.id == store.chat }) {
                HubThread(store: store, chat: chat, client: client).id(chat.id)
            } else if store.status?.state == HubStatusState.starting {
                Text("Hub is starting.").foregroundStyle(.muted)
            } else {
                Text("No chat selected.").foregroundStyle(.muted)
            }
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}

extension HubMessage {
    private static let stickerSize = 160.0

    /// A Hub message in the app's own shape, so it draws with the same bubble as a bot's.
    var bubble: Message {
        var parts: [Part] = []
        if let attachment {
            let sticker = kind == HubMessageKind.sticker
            let images = kind == HubMessageKind.image || sticker
            // A sticker is carried as an image, but shown at a sticker's size.
            let fit = sticker ? min(1, HubMessage.stickerSize / Double(max(attachment.width, attachment.height, 1))) : 1
            let size = sticker && attachment.width == 0 ? HubMessage.stickerSize : nil
            let file = Attachment(id: attachment.uri, kind: images ? AttachmentKind.image : kind == HubMessageKind.video ? AttachmentKind.video
                                    : kind == HubMessageKind.audio ? AttachmentKind.audio : AttachmentKind.file,
                                  name: attachment.name, mimeType: attachment.mime, size: attachment.size,
                                  url: HubStore.mediaPath(attachment.uri),
                                  thumbnailURL: images ? HubStore.mediaPath(attachment.uri, thumbnail: true) : nil,
                                  width: size ?? (attachment.width > 0 ? Double(attachment.width) * fit : nil),
                                  height: size ?? (attachment.height > 0 ? Double(attachment.height) * fit : nil))
            parts.append(Part(kind: PartKind.attachment, attachment: file))
        }
        if !text.isEmpty { parts.append(Part(kind: PartKind.text, text: text)) }
        let date = Date(timeIntervalSince1970: Double(at) / 1000)
        var value = Message(id: id, threadID: chat, role: mine ? MessageRole.user : MessageRole.assistant, parts: parts,
                            createdAt: date.ISO8601Format(), status: id.hasPrefix("sending-") ? MessageStatus.pending : MessageStatus.done, seq: at)
        if let replyTo {
            value.replyToID = replyTo
            value.reply = ReplyContext(messageID: replyTo, role: replySender == nil || replySender == "You" ? MessageRole.user : MessageRole.assistant, text: replyText ?? "")
        }
        return value
    }

    var date: Date { Date(timeIntervalSince1970: Double(at) / 1000) }

    var bubbleReactions: [Reaction] {
        reactions.flatMap { reaction in
            (0..<Int(reaction.count)).map { index in
                Reaction(emoji: reaction.key, agentID: reaction.mine && index == 0 ? nil : "other-\(index)")
            }
        }
    }
}

struct HubThread: View {
    @Bindable var store: HubStore
    let chat: HubChat
    let client: APIClient
    @Namespace private var scrollSpace
    @State private var reply: ReplyContext?
    @State private var searching = false
    @State private var renaming = false
    @State private var glow: ReplyTarget?
    @State private var headerHeight: CGFloat = 96
    @State private var composerHeight: CGFloat = 70
    @State private var awayFromEnd = false
    @State private var edges = ScrollEdges()
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var toolbarInset = ToolbarInset.last
    #if os(iOS)
    @Environment(\.self) private var environment
    #endif

    private var messages: [HubMessage] { store.messages[chat.id] ?? [] }
    private var mentionable: [HubMember] { HubMentionText.merge(store.members[chat.id] ?? [], messages) }
    private var renamable: Bool { chat.group && (chat.platform != "wechat" || store.status?.wechatRename == true) }
    private let fadeHeight: CGFloat = 12

    /// How far the avatar rises into the navigation bar.
    private var headerInset: CGFloat {
        #if os(iOS)
        if #available(iOS 27.1, *), environment.toolbarVerticalEdge != nil { return 0 }
        #endif
        return toolbarInset
    }

    private var topInset: CGFloat { max(0, headerHeight - headerInset) + 8 }

    #if os(macOS)
    // Messages fade away under the header, as in a bot's chat.
    private var listTop: CGFloat { topInset }
    private var frameTop: CGFloat { 0 }
    private var fadeDepth: CGFloat { max(headerHeight + 24, 72) }
    #else
    private var listTop: CGFloat { fadeHeight }
    private var frameTop: CGFloat { topInset }
    private var fadeDepth: CGFloat { fadeHeight }
    #endif

    var body: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(spacing: 12) {
                    Color.clear.frame(height: 1).onAppear { Task { await store.loadOlder(chat.id) } }
                    ForEach(Array(messages.enumerated()), id: \.element.id) { index, message in
                        let previous = index > 0 ? messages[index - 1] : nil
                        if ChatStatus.starts(message.date, after: previous?.date) { ChatStatus(date: message.date) }
                        row(message, previous: previous, proxy)
                    }
                }
                .padding(.horizontal, 20).padding(.top, listTop).padding(.bottom, 12)
            }
            .defaultScrollAnchor(.bottom)
            .coordinateSpace(name: scrollSpace)
            .onScrollGeometryChange(for: Bool.self) { geometry in
                geometry.contentSize.height - geometry.visibleRect.maxY > 90
            } action: { _, away in
                awayFromEnd = away
            }
            .onScrollGeometryChange(for: ScrollEdges.self) { ScrollEdges($0) } action: { _, value in edges = value }
            .overlay(alignment: .top) { topFade }
            .overlay(alignment: .bottom) { bottomFade }
            // On iPhone the list itself stops below the header, so a full chat can't scroll behind it.
            .padding(.top, frameTop)
            .safeAreaInset(edge: .bottom, spacing: 0) {
                composer
                    .padding(.horizontal, ComposerMetrics.edge).padding(.bottom, ComposerMetrics.edge).padding(.top, 10)
                    .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { composerHeight = $0 }
                    .overlay(alignment: .top) {
                        if awayFromEnd, let last = messages.last {
                            LatestButton {
                                withAnimation(Motion.move.unless(reduceMotion)) { proxy.scrollTo(last.id, anchor: .bottom) }
                            }
                            .alignmentGuide(.top) { $0[.bottom] + 8 }
                        }
                    }
                    .motion(value: awayFromEnd)
            }
            #if os(macOS)
            .background {
                ToolbarAnchor { inset in
                    ToolbarInset.last = inset
                    toolbarInset = inset
                }
            }
            #endif
            .overlay(alignment: .top) {
                header.padding(.top, 8)
                    .offset(y: -headerInset)
                    .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { headerHeight = $0 }
                    #if os(iOS)
                    .background {
                        ToolbarAnchor { top, safeTop in
                            let inset = max(0, top + 8 - safeTop)
                            ToolbarInset.last = inset
                            toolbarInset = inset
                        }
                    }
                    #endif
            }
            #if os(macOS)
            .panelBar(value: renamable) {
                HStack(spacing: 0) {
                    Spacer(minLength: 0)
                    actions
                }
            }
            #endif
            .opacity(searching ? 0 : 1)
            .allowsHitTesting(!searching)
            .accessibilityHidden(searching)
            .overlay {
                if searching {
                    HubSearchPane(store: store, chat: chat, client: client, close: { searching = false }) { message in
                        searching = false
                        jump(to: message.id, proxy)
                    }
                }
            }
        }
        .task(id: chat.id) {
            async let members: Void = store.loadMembers(chat.id)
            async let stickers: Void = store.loadStickers()
            _ = await (members, stickers)
        }
        .panel(isPresented: $renaming) { HubRenameGroup(store: store, chat: chat) }
        #if os(iOS)
        .navigationTitle("")
        .navigationBarTitleDisplayMode(.inline)
        .toolbar(.hidden, for: .tabBar)
        .toolbar {
            if !searching {
                ToolbarItemGroup(placement: .primaryAction) {
                    if renamable { Button("Rename group", systemImage: "pencil") { renaming = true } }
                    Button("Search chat", systemImage: "magnifyingglass") { searching = true }
                }
            }
        }
        #else
        .navigationTitle(chat.name)
        #endif
    }

    /// Goes to a message, paging back if it's older than what's loaded, and glows it as a bot's chat does.
    private func jump(to id: String, _ proxy: ScrollViewProxy) {
        Task {
            while !messages.contains(where: { $0.id == id }), await store.loadOlder(chat.id) {}
            guard messages.contains(where: { $0.id == id }) else { return }
            await Task.yield()
            let target = ReplyTarget(id: id, part: nil)
            withAnimation(Motion.move.unless(reduceMotion)) {
                glow = target
                proxy.scrollTo(id, anchor: .center)
            }
            try? await Task.sleep(for: .seconds(1.2))
            guard glow == target else { return }
            withAnimation(Motion.fade.unless(reduceMotion)) { glow = nil }
        }
    }

    private func setReply(_ value: ReplyContext?) {
        withAnimation(Motion.quick.unless(reduceMotion)) { reply = value }
    }

    /// Who the reply on its way is to, for the preview in the field.
    private var replyName: String {
        messages.first { $0.id == reply?.messageID }?.senderName ?? ""
    }

    private var composer: some View {
        // Only what can really reach the chat is offered. WeChat takes no voice notes from here.
        let textOnly = store.textOnly(chat.platform)
        return HubComposer(reply: reply, replyName: replyName, clearReply: { setReply(nil) }, members: mentionable, group: chat.group, client: client,
                           stickers: store.stickers, sendSticker: textOnly ? nil : { await store.sendSticker($0, chat: chat.id) },
                           sendVoice: textOnly || chat.platform == "wechat" ? nil : { file, duration in
                               await store.send("", chat: chat.id, file: file, mime: "audio/mp4", duration: duration)
                           }, attachments: !textOnly, picturesOnly: chat.platform == "wechat") { text, file, mime in
            let target = reply?.messageID
            setReply(nil)
            let mentions = HubMentionText.mentions(in: text, members: mentionable, group: chat.group)
            return await store.send(text, chat: chat.id, replyTo: target, file: file, mime: mime, mentions: mentions)
        }
    }

    #if os(macOS)
    private var actions: some View {
        HStack(spacing: PanelStyle.rowInset) {
            if renamable {
                PanelNavButton(symbol: "pencil", label: "Rename group") { renaming = true }
            }
            PanelNavButton(symbol: "magnifyingglass", label: "Search chat") { searching = true }
        }
    }
    #endif

    private var topFade: some View {
        Rectangle().fill(.background).frame(height: fadeDepth)
            .mask {
                LinearGradient(stops: [.init(color: .black, location: 0), .init(color: .black.opacity(0.9), location: 0.35),
                                       .init(color: .clear, location: 1)], startPoint: .top, endPoint: .bottom)
            }
            .opacity(edges.above ? 1 : 0)
            .ignoresSafeArea(.container, edges: .top)
            .allowsHitTesting(false)
            .accessibilityHidden(true)
    }

    private var bottomFade: some View {
        Rectangle().fill(.background).frame(height: composerHeight + 30)
            .mask { LinearGradient(colors: [.clear, .black.opacity(0.9), .black], startPoint: .top, endPoint: .bottom) }
            .opacity(edges.below ? 1 : 0)
            .ignoresSafeArea(.container, edges: .bottom)
            .allowsHitTesting(false)
            .accessibilityHidden(true)
    }

    private var platformName: String {
        store.status?.platforms.first { $0.id == chat.platform }?.name ?? chat.platform.capitalized
    }

    private var header: some View {
        VStack(spacing: -4) {
            HubAvatar(name: chat.name, uri: chat.avatar, client: client, size: 46).zIndex(1)
            VStack(spacing: 2) {
                Text(chat.name).font(.headline).lineLimit(1)
                Text(platformName).font(.caption).foregroundStyle(.muted).lineLimit(1)
            }
            .padding(.horizontal, 16).padding(.vertical, 6)
            .glassEffect(.regular, in: Capsule())
        }
        .frame(maxWidth: 260)
        .accessibilityElement(children: .combine)
    }

    /// A quote shows the picture it answers when that message is loaded, as a bot's does.
    private func bubble(_ message: HubMessage) -> Message {
        var value = message.bubble
        if let original = messages.first(where: { $0.id == message.replyTo }) {
            value.reply?.attachment = original.bubble.attachments.first
        }
        return value
    }

    private func row(_ message: HubMessage, previous: HubMessage?, _ proxy: ScrollViewProxy) -> some View {
        let react = { (key: String) in Task { await store.react(key, message: message.id, chat: chat.id) } }
        return VStack(alignment: message.mine ? .trailing : .leading, spacing: 4) {
            MessageBubble(message: bubble(message), client: client, agentName: message.senderName,
                          showAuthor: chat.group && !message.mine && previous?.sender != message.sender,
                          replyName: message.replySender ?? "", sending: message.id.hasPrefix("sending-"), discard: {},
                          respond: setReply, jump: { jump(to: $0.messageID, proxy) }, ask: { _, _ in }, highlight: glow,
                          reactions: message.bubbleReactions, reacting: false, reactionError: nil, agents: [],
                          react: { key in _ = react(key) }, scrollSpace: scrollSpace, showsReactions: false)
                .equatable()
            if !message.reactions.isEmpty {
                HubReactionBar(reactions: message.reactions, outgoing: message.mine, client: client) { key in _ = react(key) }
            }
            if !message.readers.isEmpty {
                HubReaders(people: message.readers, outgoing: message.mine, client: client)
            }
        }
        .frame(maxWidth: .infinity, alignment: message.mine ? .trailing : .leading)
        .id(message.id)
    }
}

private struct HubRenameGroup: View {
    @Bindable var store: HubStore
    let chat: HubChat
    @Environment(\.panelClose) private var close
    @State private var name = ""

    private var trimmed: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }

    var body: some View {
        PanelView("Rename group", onClose: { close() }, width: PanelSize.width) {
            VStack(spacing: PanelStyle.contentInset) {
                Form { SettingsField(title: "Name", text: $name, prompt: chat.name) }.formStyle(.grouped).frame(height: 76)
                HStack {
                    Spacer()
                    Button("Save") {
                        let value = trimmed
                        close()
                        Task { await store.rename(chat.id, to: value) }
                    }
                    .buttonStyle(.borderedProminent)
                    .disabled(trimmed.isEmpty || trimmed == chat.name)
                }
            }
            .padding(PanelStyle.contentInset)
        }
        .onAppear { name = chat.name }
    }
}

/// The message box, shaped like the bot chat's.
struct HubComposer: View {
    /// What the + button has open: one dropdown that changes in place, so it stays above the button.
    private enum Adding {
        case menu, emoji, stickers
    }

    var reply: ReplyContext?
    var replyName = ""
    var clearReply: () -> Void = {}
    var members: [HubMember] = []
    var group = false
    var client: APIClient?
    var stickers: [HubAttachment] = []
    var sendSticker: ((HubAttachment) async -> Void)?
    var sendVoice: ((URL, Int64) async -> Bool)?
    var attachments = true
    var picturesOnly = false
    let send: (String, URL?, String) async -> Bool
    @State private var text = ""
    @State private var selection: TextSelection?
    @State private var adding: Adding?
    @State private var picking = false
    @State private var pickingPhotos = false
    @State private var photos: [PhotosPickerItem] = []
    @State private var camera = false
    @State private var sending = false
    @State private var highlighted = 0
    @State private var height: CGFloat = 0
    @State private var recorder = HubRecorder()
    @FocusState private var focused: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var size: CGFloat { ComposerMetrics.control }

    private var cursor: Int {
        if case let .selection(range) = selection?.indices, range.isEmpty, range.lowerBound <= text.endIndex {
            return text.distance(from: text.startIndex, to: range.lowerBound)
        }
        return text.count
    }

    private var token: HubToken? { focused ? HubMentionText.token(in: text, cursor: cursor) : nil }
    private var suggestions: [HubSuggestion] { HubMentionText.suggestions(token, members: members, group: group) }
    private var empty: Bool { text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty }

    var body: some View {
        Group {
            if recorder.phase == .idle { field } else { recording }
        }
        .onGeometryChange(for: CGFloat.self) { $0.size.height } action: { height = $0 }
        .overlay(alignment: .bottom) {
            if !suggestions.isEmpty {
                mentionList.fixedSize(horizontal: false, vertical: true).padding(.bottom, height + 8)
            }
        }
        .fileImporter(isPresented: $picking, allowedContentTypes: picturesOnly ? [.image] : [.item]) { result in
            guard case let .success(url) = result else { return }
            Task { await upload(url) }
        }
        .photosPicker(isPresented: $pickingPhotos, selection: $photos, maxSelectionCount: 8, matching: picturesOnly ? .images : .any(of: [.images, .videos]))
        .onChange(of: photos) { _, items in
            guard !items.isEmpty else { return }
            photos = []
            Task { for item in items { await upload(item) } }
        }
        #if os(macOS)
        .panel(isPresented: $camera) {
            CameraView { url in
                camera = false
                Task { await upload(url) }
            }
        }
        #else
        .sheet(isPresented: $camera) {
            CameraView { url in
                camera = false
                Task { await upload(url) }
            }
            .ignoresSafeArea()
        }
        #endif
    }

    private var field: some View {
        HStack(alignment: .bottom, spacing: 12) {
            add
            VStack(alignment: .leading, spacing: 10) {
                if let reply, let client {
                    HStack(alignment: .top, spacing: 8) {
                        ReplyPreview(reply: reply, agentName: replyName, client: client)
                            .frame(maxWidth: .infinity, alignment: .leading)
                        Button("Cancel reply", systemImage: "xmark", action: clearReply)
                            .labelStyle(.iconOnly).buttonStyle(.plain)
                            .foregroundStyle(.muted)
                            .tapArea(14)
                    }
                    .transition(.opacity)
                }
                TextField("Message", text: $text, selection: $selection, axis: .vertical)
                    .textFieldStyle(.plain).lineLimit(1...8).focused($focused)
                    .onSubmit {
                        if suggestions.isEmpty { submit() } else { choose() }
                    }
                    .onKeyPress(.downArrow) { move(1) }
                    .onKeyPress(.upArrow) { move(-1) }
                    .onKeyPress(.tab) {
                        guard !suggestions.isEmpty else { return .ignored }
                        choose()
                        return .handled
                    }
                    .onChange(of: token?.query) { highlighted = 0 }
                    .onChange(of: reply?.messageID) { _, value in if value != nil { focused = true } }
                    .accessibilityLabel("Message")
            }
            .padding(.horizontal, 15).padding(.vertical, reply == nil ? 7 : 12)
            .frame(minHeight: size)
            .glassEffect(.regular, in: RoundedRectangle(cornerRadius: reply == nil ? size / 2 : 24))
            // One button on the right, mirroring + on the left, as bots have: send once there's text, otherwise record.
            if empty, sendVoice != nil {
                Button { Task { await recorder.start() } } label: {
                    Image(systemName: "mic").font(.system(size: 15))
                        .frame(width: size, height: size).contentShape(Circle())
                }
                .buttonStyle(.plain)
                .glassEffect(.regular.interactive(), in: Circle())
                .accessibilityLabel("Record voice message")
                .transition(.scale(scale: 0.8).combined(with: .opacity))
            } else {
                Button(action: submit) {
                    Image(systemName: "arrow.up").font(.system(size: 15, weight: .semibold))
                        .frame(width: size, height: size).contentShape(Circle())
                }
                .buttonStyle(.plain)
                .glassEffect(.regular.interactive(), in: Circle())
                .disabled(empty || sending)
                .accessibilityLabel("Send")
                .transition(.scale(scale: 0.8).combined(with: .opacity))
            }
        }
        .animation(Motion.quick.unless(reduceMotion), value: empty)
    }

    private var add: some View {
        Button { adding = adding == nil ? .menu : nil } label: {
            Image(systemName: "plus").font(.system(size: 17, weight: .regular))
                .frame(width: size, height: size).contentShape(Circle())
        }
        .buttonStyle(.plain)
        .glassEffect(.regular.interactive(), in: Circle())
        .accessibilityLabel("Add attachment")
        .dropdown(isPresented: Binding(get: { adding != nil }, set: { if !$0 { adding = nil } }), alignment: .leading) {
            switch adding {
            case .emoji: EmojiPicker(action: "Insert", select: insert)
            case .stickers: stickerGrid
            default:
                AttachmentMenu {
                    if attachments {
                        addRow("Files", icon: "doc") { picking = true }
                        addRow("Photos", icon: "photo") { pickingPhotos = true }
                        addRow("Camera", icon: "camera") { camera = true }
                            #if os(iOS)
                            .disabled(!UIImagePickerController.isSourceTypeAvailable(.camera))
                            #endif
                    }
                    AttachmentRow(title: "Emoji", icon: "face.smiling") { adding = .emoji }
                    if sendSticker != nil {
                        AttachmentRow(title: "Stickers", icon: "photo.on.rectangle") { adding = .stickers }
                    }
                }
            }
        }
    }

    private func addRow(_ title: String, icon: String, action: @escaping () -> Void) -> some View {
        AttachmentRow(title: title, icon: icon) {
            adding = nil
            action()
        }
    }

    private func insert(_ value: String) {
        let at = text.index(text.startIndex, offsetBy: min(cursor, text.count))
        text.insert(contentsOf: value, at: at)
        selection = TextSelection(insertionPoint: text.index(at, offsetBy: value.count))
    }

    private var recording: some View {
        HStack(alignment: .bottom, spacing: 8) {
            Button { recorder.discard() } label: {
                Image(systemName: "trash").frame(width: size, height: size).contentShape(Circle())
            }
            .buttonStyle(.plain)
            .glassEffect(.regular.interactive(), in: Circle())
            .accessibilityLabel("Discard voice message")
            HStack(spacing: 8) {
                Text(Duration.seconds(recorder.seconds).formatted(.time(pattern: .minuteSecond)))
                    .font(.subheadline).monospacedDigit().foregroundStyle(.muted)
                    .accessibilityLabel("Recording")
                if recorder.phase == .recording { Waveform(level: recorder.level).frame(height: 18) } else { Spacer(minLength: 0) }
                Button { recorder.toggle() } label: {
                    Image(systemName: recorder.phase == .recording ? "pause" : "mic").frame(width: size, height: size).contentShape(Circle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel(recorder.phase == .recording ? "Pause recording" : "Resume recording")
                Button(action: finishVoice) {
                    Image(systemName: "arrow.up").frame(width: size, height: size).contentShape(Circle())
                }
                .buttonStyle(.plain)
                .disabled(sending)
                .accessibilityLabel("Send voice message")
            }
            .padding(.leading, 14)
            .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 20))
        }
    }

    private var mentionList: some View {
        VStack(alignment: .leading, spacing: 2) {
            ForEach(Array(suggestions.enumerated()), id: \.element.id) { index, item in
                Button { choose(item) } label: {
                    HStack(spacing: 8) {
                        Text(item.label).lineLimit(1)
                        Spacer(minLength: 8)
                        Text(item.detail).font(.caption).foregroundStyle(.muted).lineLimit(1)
                    }
                    .padding(.horizontal, 12).padding(.vertical, 8)
                    .frame(minHeight: 38)
                    .background(index == highlighted ? Color.primary.opacity(PanelStyle.selectedTint) : .clear, in: .rect(cornerRadius: 10))
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Mention \(item.label)")
            }
        }
        .padding(4)
        .frame(maxWidth: 320)
        .glassEffect(.regular, in: .rect(cornerRadius: 16))
        .frame(maxWidth: .infinity, alignment: .leading)
        .padding(.leading, size + 8)
    }

    @ViewBuilder private var stickerGrid: some View {
        if stickers.isEmpty {
            Text("No stickers yet.").foregroundStyle(.muted).frame(width: 240, height: 120)
        } else if let client {
            ScrollView {
                LazyVGrid(columns: Array(repeating: GridItem(.fixed(72), spacing: 8), count: 4), spacing: 8) {
                    ForEach(stickers, id: \.uri) { sticker in
                        Button {
                            adding = nil
                            Task { await sendSticker?(sticker) }
                        } label: {
                            CachedImage(url: client.resolve(HubStore.mediaPath(sticker.uri, thumbnail: true)), name: sticker.name, client: client) { image in
                                image.resizable().scaledToFit()
                            } placeholder: {
                                RoundedRectangle(cornerRadius: 8).fill(.quaternary)
                            }
                            .frame(width: 72, height: 72)
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel("Send sticker \(sticker.name)")
                    }
                }
                .padding(12)
            }
            .frame(width: 348, height: 260)
        }
    }

    private func move(_ offset: Int) -> KeyPress.Result {
        guard !suggestions.isEmpty else { return .ignored }
        highlighted = max(0, min(suggestions.count - 1, highlighted + offset))
        return .handled
    }

    private func choose(_ item: HubSuggestion? = nil) {
        guard let token, let target = item ?? (suggestions.indices.contains(highlighted) ? suggestions[highlighted] : suggestions.first) else { return }
        let replaced = HubMentionText.replace(text, token, with: target.value)
        text = replaced.text
        selection = TextSelection(insertionPoint: text.index(text.startIndex, offsetBy: replaced.caret))
        focused = true
    }

    private func submit() {
        let value = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !value.isEmpty, !sending else { return }
        text = ""
        selection = nil
        Task {
            sending = true
            defer { sending = false }
            if await !send(value, nil, ""), text.isEmpty { text = value }
        }
    }

    private func finishVoice() {
        guard let take = recorder.finish(), let sendVoice else { return }
        Task {
            sending = true
            defer {
                sending = false
                try? FileManager.default.removeItem(at: take.file.deletingLastPathComponent())
            }
            _ = await sendVoice(take.file, take.duration)
        }
    }

    private func upload(_ item: PhotosPickerItem) async {
        let type = item.supportedContentTypes.first ?? .data
        guard let data = try? await item.loadTransferable(type: Data.self) else { return }
        let name = "Photo-\(UUID().uuidString.prefix(8)).\(type.preferredFilenameExtension ?? "bin")"
        let file = FileManager.default.temporaryDirectory.appending(path: name)
        let saved = await Task.detached(priority: .userInitiated) { (try? data.write(to: file, options: .atomic)) != nil }.value
        guard saved else { return }
        await upload(file)
        try? FileManager.default.removeItem(at: file)
    }

    private func upload(_ url: URL) async {
        let access = url.startAccessingSecurityScopedResource()
        defer { if access { url.stopAccessingSecurityScopedResource() } }
        let mime = UTType(filenameExtension: url.pathExtension)?.preferredMIMEType ?? "application/octet-stream"
        _ = await send("", url, mime)
    }
}

struct HubBroadcastThread: View {
    @Bindable var store: HubStore
    let list: HubBroadcast

    var body: some View {
        VStack(spacing: 0) {
            ScrollView {
                LazyVStack(alignment: .trailing, spacing: 12) {
                    ForEach(list.messages, id: \.id) { message in
                        VStack(alignment: .trailing, spacing: 4) {
                            Text(message.text).padding(.horizontal, 12).padding(.vertical, 8)
                                .background(Color("UserBubble"), in: .rect(cornerRadius: BubbleStyle.radius))
                            Text(delivery(message)).font(.caption2).foregroundStyle(.muted)
                        }
                    }
                }
                .padding(16)
                .frame(maxWidth: .infinity, alignment: .trailing)
            }
            .defaultScrollAnchor(.bottom)
            HubComposer(attachments: false) { text, _, _ in
                await store.sendBroadcast(text, id: list.id)
                return true
            }
            .padding(.horizontal, ComposerMetrics.edge).padding(.bottom, ComposerMetrics.edge)
        }
        .overlay {
            if list.messages.isEmpty { Text("No messages yet.").foregroundStyle(.muted) }
        }
    }

    private func delivery(_ message: HubBroadcastMessage) -> String {
        let delivered = message.deliveries.filter(\.ok).count
        return "Sent to \(delivered) of \(message.deliveries.count)"
    }
}

#if os(iOS)
/// Hub on iPhone: the list, with each chat as a page over it.
struct HubRoot: View {
    @Bindable var store: HubStore
    let client: APIClient
    let settings: () -> Void

    private var open: Binding<Bool> {
        Binding(get: { store.chat != nil || store.broadcast != nil || store.mail.open != nil || store.mail.compose != nil }, set: { shown in
            guard !shown else { return }
            store.chat = nil
            store.broadcast = nil
            store.mail.clearReader()
            Task { await store.mail.leave() }
        })
    }

    var body: some View {
        NavigationStack {
            HubSidebar(store: store, client: client)
                .navigationTitle("Hub")
                .navigationBarTitleDisplayMode(.inline)
                .navigationDestination(isPresented: open) { HubDetail(store: store, client: client) }
                .toolbar {
                    ToolbarItem(placement: .topBarLeading) { Button("Settings", systemImage: "gearshape", action: settings) }
                    ToolbarItem(placement: .topBarTrailing) {
                        Button("Search", systemImage: "magnifyingglass") { store.panel = .search }
                    }
                    ToolbarItem(placement: .topBarTrailing) {
                        Menu("New chat", systemImage: "plus") {
                            ForEach(store.actions, id: \.title) { item in
                                Button(item.title, systemImage: item.symbol, action: item.run)
                            }
                        }
                    }
                }
        }
        #if DEBUG
        .task { await HubMailFixture.show(store) }
        #endif
    }
}
#endif
