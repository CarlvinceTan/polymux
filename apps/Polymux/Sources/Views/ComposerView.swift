import SwiftUI
import PhotosUI
import UniformTypeIdentifiers
import PolymuxKit

nonisolated struct DraftFile: Identifiable, Sendable {
    let id = UUID()
    let url: URL
    let kind: UTType

    /// The file as the chat's send queue takes it.
    var local: LocalFile {
        let size = (try? url.resourceValues(forKeys: [.fileSizeKey]).fileSize).map(Int64.init) ?? 0
        let category = kind.conforms(to: .image) ? AttachmentKind.image : kind.conforms(to: .movie) ? AttachmentKind.video
            : kind.conforms(to: .audio) ? AttachmentKind.audio : AttachmentKind.file
        return LocalFile(id: id.uuidString, url: url, name: url.lastPathComponent, kind: category,
                         mimeType: kind.preferredMIMEType ?? "application/octet-stream", size: size)
    }
}

/// The composer row's sizes, which the sidebar's switcher lines up with.
enum ComposerMetrics {
    static var control: CGFloat {
        #if os(macOS)
        32
        #else
        44
        #endif
    }

    static var edge: CGFloat {
        #if os(macOS)
        14
        #else
        24
        #endif
    }
}

struct ComposerView: View {
    /// What the + button has open: one dropdown that changes in place, so it stays above the button.
    private enum Adding {
        case menu, emoji
    }

    let client: APIClient
    let draft: DraftStore
    let isVoiceActive: Bool
    let send: (String, [LocalFile], ReplyContext?, [String]) async -> Void
    var reply: ReplyContext? = nil
    var agentName = ""
    var clearReply: () -> Void = {}
    var restoreReply: (ReplyContext?) -> Void = { _ in }
    var drops: Binding<[DroppedFile]> = .constant([])
    var members: [Agent] = []
    var drafting: (Bool) -> Void = { _ in }
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    @State private var mentionIndex = 0
    private var motion: Animation? { Motion.quick.unless(reduceMotion) }
    @State private var dictation = DictationInput()
    @State private var dictationDraft = DictationDraft()
    @State private var ignoresDictation = false
    @State private var dictated: Range<Int>?
    @State private var polishing = false
    @State private var mentions: [MentionToken] = []
    @State private var text = ""
    @State private var selection: TextSelection?
    @State private var insertion = NSRange(location: 0, length: 0)
    @State private var caret: Int?
    /// Where the caret was when + took focus, since the field forgets it on macOS.
    @State private var held: TextSelection?
    @Binding var files: [DraftFile]
    @State private var photos: [PhotosPickerItem] = []
    @State private var adding: Adding?
    @State private var importing = false
    @State private var pickingPhotos = false
    @State private var camera = false
    @State private var imports = 0
    private var loadingPhotos: Bool { imports > 0 }
    @State private var error: String?
    @State private var draftTask: Task<Void, Never>?
    @FocusState private var focused: Bool

    private var currentDraft: ChatDraft {
        ChatDraft(text: text, mentions: mentions.map {
            .init(id: $0.id, text: $0.text, location: $0.range.location, length: $0.range.length)
        }, reply: reply)
    }

    private func applyDraft() {
        let saved = draft.value
        if text != saved.text { text = saved.text }
        mentions = saved.mentions.compactMap {
            guard $0.location >= 0, $0.length >= 0, $0.location <= saved.text.utf16.count,
                  $0.length <= saved.text.utf16.count - $0.location else { return nil }
            let range = NSRange(location: $0.location, length: $0.length)
            guard Range(range, in: saved.text) != nil else { return nil }
            return MentionToken(id: $0.id, text: $0.text, range: range)
        }
        if reply != saved.reply { restoreReply(saved.reply) }
    }

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            controls
            if let error = error ?? draft.issue {
                Text(error).font(.caption2).foregroundStyle(.muted).padding(.leading, controlSize + 12)
            }
        }
        .animation(motion, value: recording)
        .animation(motion, value: error)
        .task { await dictation.refresh() }
        #if os(macOS)
        // A phone keeps the keyboard down until asked: raising it hides half of the chat just opened.
        .task {
            await Task.yield()
            if !isVoiceActive { focused = true }
        }
        #endif
        // A draft read ahead of opening shows with the chat, not a moment after it.
        .onAppear { applyDraft() }
        .task(id: scenePhase == .background) {
            await draft.restore()
            applyDraft()
            if scenePhase != .background { await draft.watch() }
        }
        .onChange(of: draft.value) { _, _ in applyDraft() }
        .onChange(of: currentDraft) { _, value in
            draft.edit(value, after: value.text.isEmpty ? .zero : .milliseconds(400))
        }
        .onChange(of: isVoiceActive) { _, active in
            focused = !active
            if active {
                adding = nil
                Task { await stopDictation() }
            }
        }
        .onDisappear {
            draftTask?.cancel()
            draft.flush()
            drafting(false)
            Task { await stopDictation() }
        }
        .onChange(of: text) { _, value in
            // The field ignores a caret set along with new text, so it follows once the text has landed.
            if let caret {
                self.caret = nil
                selection = TextSelection(insertionPoint: String.Index(utf16Offset: min(caret, value.utf16.count), in: value))
                held = selection
            }
            draftTask?.cancel()
            drafting(!value.isEmpty && focused)
            draftTask = Task {
                do { try await Task.sleep(for: .seconds(3)) } catch { return }
                drafting(recording)
            }
        }
        .onChange(of: focused) { _, value in
            if !value, !recording { drafting(false) }
        }
        .onChange(of: scenePhase) { _, phase in
            if phase == .background {
                draft.flush()
                Task { await stopDictation() }
            }
            if phase == .active { Task { await dictation.refresh() } }
        }
        .onChange(of: dictation.active) { _, active in
            if !active {
                dictationDraft.stop()
                if !isVoiceActive { focused = true }
            }
        }
        .onChange(of: recording) { _, active in
            drafting(active)
            if !active, !isVoiceActive {
                focused = true
                Task { @MainActor in
                    await Task.yield()
                    guard !recording, let range = Range(insertion, in: text) else { return }
                    selection = TextSelection(range: range)
                }
            }
        }
        .onChange(of: reply?.messageID) { _, value in
            if value != nil { focused = true }
        }
        #if os(macOS)
        .onChange(of: adding) { old, new in
            guard old == .emoji, new == nil, let held else { return }
            Task { @MainActor in
                // After the dropdown has handed focus back, which would otherwise take it from the field.
                await Task.yield()
                focused = true
                await Task.yield()
                selection = held
            }
        }
        #endif
        .onChange(of: drops.wrappedValue, initial: true) { _, items in
            guard !items.isEmpty else { return }
            drops.wrappedValue = []
            error = nil
            for item in items {
                switch item {
                case let .file(url):
                    let kind = UTType(filenameExtension: url.pathExtension) ?? .data
                    withAnimation(motion) { files.append(DraftFile(url: url, kind: kind)) }
                case .failed:
                    error = "Couldn’t attach that file."
                }
            }
            focused = true
        }
        .fileImporter(isPresented: $importing, allowedContentTypes: [.item], allowsMultipleSelection: true) { result in
            imports += 1
            Task {
                defer { imports -= 1 }
                do {
                    for url in try result.get() { try await addFile(url) }
                } catch { self.error = "Couldn’t attach that file." }
            }
        }
        .photosPicker(isPresented: $pickingPhotos, selection: $photos, maxSelectionCount: 8, matching: .any(of: [.images, .videos]))
        .onChange(of: photos) { _, items in
            guard !items.isEmpty else { return }
            imports += 1
            Task {
                do {
                    for item in items {
                        let type = item.supportedContentTypes.first ?? .data
                        if let data = try await item.loadTransferable(type: Data.self) {
                            let file = try await Task.detached(priority: .userInitiated) {
                                let file = FileManager.default.temporaryDirectory
                                    .appending(path: "Photo-\(UUID().uuidString.prefix(8)).\(type.preferredFilenameExtension ?? "bin")")
                                try data.write(to: file, options: .atomic)
                                return file
                            }.value
                            withAnimation(motion) { files.append(DraftFile(url: file, kind: type)) }
                        }
                    }
                    photos = []
                } catch { self.error = "Couldn’t attach that photo." }
                imports -= 1
            }
        }
        #if os(macOS)
        .panel(isPresented: $camera) {
            CameraView { url in
                withAnimation(motion) { files.append(DraftFile(url: url, kind: .jpeg)) }
                camera = false
            }
        }
        #else
        .sheet(isPresented: $camera) {
            CameraView { url in
                withAnimation(motion) { files.append(DraftFile(url: url, kind: .jpeg)) }
                camera = false
            }
            .ignoresSafeArea()
        }
        #endif
    }

    private var controls: some View {
        HStack(alignment: .bottom, spacing: 12) {
            if !recording {
                Button {
                    #if os(macOS)
                    // Regaining focus selects the whole draft, so the next key would replace it.
                    held = focused ? selection : nil
                    focused = false
                    #endif
                    adding = adding == nil ? .menu : nil
                } label: {
                    Image(systemName: "plus").font(.system(size: 17, weight: .regular))
                        .frame(width: controlSize, height: controlSize)
                        .contentShape(Circle())
                }
                .buttonStyle(.plain).frame(width: controlSize, height: controlSize)
                .glassEffect(.regular.interactive(), in: Circle())
                .accessibilityLabel("Add attachment")
                .dropdown(isPresented: Binding(get: { adding != nil }, set: { if !$0 { adding = nil } }), alignment: .leading) {
                    if adding == .emoji {
                        EmojiPicker(action: "Insert", select: insert)
                    } else {
                        AttachmentMenu {
                            attachmentButton("Files", icon: "doc") { importing = true }
                            attachmentButton("Photos", icon: "photo") { pickingPhotos = true }
                            attachmentButton("Camera", icon: "camera") { camera = true }
                                #if os(iOS)
                                .disabled(!UIImagePickerController.isSourceTypeAvailable(.camera))
                                #endif
                            AttachmentRow(title: "Emoji", icon: "face.smiling") { adding = .emoji }
                        }
                    }
                }
            }
            VStack(alignment: .leading, spacing: 10) {
                if recording {
                    Color.clear.frame(height: 18).accessibilityHidden(true)
                } else {
                    if let reply {
                        HStack(alignment: .top, spacing: 8) {
                            ReplyPreview(reply: reply, agentName: agentName, client: client)
                                .frame(maxWidth: .infinity, alignment: .leading)
                            Button("Cancel reply", systemImage: "xmark") { clearReply() }
                                .labelStyle(.iconOnly).buttonStyle(.plain)
                                .foregroundStyle(.muted)
                                .tapArea(14)
                        }
                        .transition(.opacity)
                    }
                    if !files.isEmpty {
                        ScrollView(.horizontal) {
                            HStack(alignment: .center, spacing: 10) {
                                ForEach(files) { file in
                                    DraftPreview(file: file) {
                                        withAnimation(motion) { files.removeAll { $0.id == file.id } }
                                    }
                                }
                            }
                        }
                        .scrollIndicators(.hidden)
                        .transition(.opacity)
                    }
                    TextField("Message", text: draftText, selection: $selection, axis: .vertical)
                        .lineLimit(1...7).textFieldStyle(.plain)
                        .scrollIndicators(.hidden).focused($focused)
                        .opacity(polishing ? 0.5 : 1)
                        .onSubmit {
                            if let mentionTarget { mention(mentionTarget) }
                            else { submit() }
                        }
                        .onKeyPress(.downArrow) { moveMention(1) }
                        .onKeyPress(.upArrow) { moveMention(-1) }
                        .onChange(of: mentionQuery?.term) { mentionIndex = 0 }
                        .onKeyPress(keys: [.return], phases: .down) { key in
                            guard key.modifiers.contains(.shift) else { return .ignored }
                            let range: Range<String.Index>
                            if case let .selection(bounds) = selection?.indices { range = bounds }
                            else { range = text.endIndex..<text.endIndex }
                            var updated = text
                            updated.replaceSubrange(range, with: "\n")
                            let offset = text.distance(from: text.startIndex, to: range.lowerBound) + 1
                            dictationDraft.edit(from: text, to: updated)
                            editText(updated, range: NSRange(range, in: text))
                            selection = TextSelection(insertionPoint: text.index(text.startIndex, offsetBy: offset))
                            return .handled
                        }
                        .accessibilityLabel("Message")
                        .anchorPreference(key: MentionAnchor.self, value: .bounds) { $0 }
                        .transaction { $0.animation = nil }
                }
            }
            .padding(.horizontal, 15).padding(.vertical, isExpanded ? 12 : 7)
            .frame(minHeight: controlSize)
            .glassEffect(.regular, in: RoundedRectangle(cornerRadius: isExpanded ? 24 : controlSize / 2))
            .overlay {
                if recording {
                    Waveform(level: dictation.level).padding(.horizontal, 15)
                }
            }
            .overlayPreferenceValue(MentionAnchor.self) { anchor in
                GeometryReader { geometry in
                    if let anchor, !mentionChoices.isEmpty {
                        let field = geometry[anchor]
                        mentionList(width: min(320, field.width))
                            .offset(x: field.minX, y: field.minY - mentionHeight - 8)
                    }
                }
            }
            .zIndex(1)
            if loadingPhotos {
                ProgressView().controlSize(.small).frame(width: controlSize, height: controlSize)
            } else {
                // One button on the right, mirroring + on the left: send once there's something to send, otherwise dictate.
                if hasContent && !recording {
                    Button { submit() } label: {
                        Image(systemName: "arrow.up").font(.system(size: 15, weight: .semibold))
                            .frame(width: controlSize, height: controlSize)
                            .contentShape(Circle())
                    }
                    .buttonStyle(.plain)
                    .glassEffect(.regular.interactive(), in: Circle())
                    .accessibilityLabel("Send message")
                    .transition(.scale(scale: 0.8).combined(with: .opacity))
                } else {
                    Button { toggleDictation() } label: {
                        Image(systemName: recording ? "pause.fill" : dictation.active ? "xmark" : dictation.denied ? "mic.slash.fill" : "mic.fill")
                            .font(.system(size: 15))
                            .frame(width: controlSize, height: controlSize)
                            .contentShape(Circle())
                    }
                    .buttonStyle(.plain)
                    .glassEffect(.regular.interactive(), in: Circle())
                    .inputAccessory()
                    .accessibilityLabel(recording ? "Pause dictation" : dictation.active ? "Cancel dictation" : "Start dictation")
                    .accessibilityValue(dictation.denied ? "Permission denied" : recording ? "Listening" : dictation.active ? "Preparing" : "")
                    .disabled(dictation.stopping || isVoiceActive)
                    .transition(.scale(scale: 0.8).combined(with: .opacity))
                }
            }
        }
        .animation(motion, value: hasContent)
    }

    private var mentionQuery: (start: String.Index, term: String)? {
        guard members.count > 1, let start = text.lastIndex(of: "@") else { return nil }
        if start > text.startIndex {
            let previous = text[text.index(before: start)]
            guard previous.isWhitespace || "([{".contains(previous) else { return nil }
        }
        let offset = start.utf16Offset(in: text)
        if mentions.contains(where: { token in
            guard token.range.location == offset, let bounds = Range(token.range, in: text) else { return false }
            return text[bounds.upperBound...].first?.isWhitespace == true
        }) { return nil }
        let term = String(text[text.index(after: start)...])
        guard !term.contains("\n"), term.count <= 40 else { return nil }
        return (start, term)
    }

    private var mentionChoices: [Agent] {
        guard focused, let query = mentionQuery else { return [] }
        let everyone = Agent(id: "everyone", name: "everyone", role: "All members")
        return ([everyone] + members).enumerated().compactMap { index, agent -> (agent: Agent, rank: Int, index: Int)? in
            guard let rank = MentionRank.value(agent.name, term: query.term) else { return nil }
            return (agent, rank, index)
        }.sorted {
            $0.rank == $1.rank ? $0.index < $1.index : $0.rank < $1.rank
        }.map(\.agent)
    }

    private var mentionHeight: CGFloat { min(160, CGFloat(mentionChoices.count) * 40 + 6) }

    private var mentionTarget: Agent? {
        mentionChoices.indices.contains(mentionIndex) ? mentionChoices[mentionIndex] : mentionChoices.first
    }

    private func moveMention(_ offset: Int) -> KeyPress.Result {
        guard !mentionChoices.isEmpty else { return .ignored }
        mentionIndex = max(0, min(mentionChoices.count - 1, mentionIndex + offset))
        return .handled
    }

    private func mentionList(width: CGFloat) -> some View {
        MentionSize(width: width, height: mentionHeight) {
            VStack(alignment: .leading, spacing: 2) {
                ForEach(mentionChoices) { member in
                    mentionLabel(member, fill: false)
                }
            }
            .padding(4).hidden().accessibilityHidden(true).allowsHitTesting(false)
            ScrollView {
                VStack(alignment: .leading, spacing: 2) {
                    ForEach(mentionChoices) { member in
                        Button { mention(member) } label: {
                            mentionLabel(member, fill: true)
                                .pointerHover { inside in
                                    if inside, let index = mentionChoices.firstIndex(of: member) { mentionIndex = index }
                                }
                                .background(member.id == mentionTarget?.id ? Color.primary.opacity(PanelStyle.selectedTint) : .clear,
                                            in: .rect(cornerRadius: 10))
                                .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel(members.filter({ $0.name == member.name }).count > 1
                                                ? "Mention \(member.name), \(member.role), \(member.id.prefix(6))"
                                                : "Mention \(member.name)")
                        .accessibilityValue(member.id == mentionTarget?.id ? "Top match" : "")
                        .accessibilityHint(member.id == mentionTarget?.id ? "Press Return to select" : "")
                    }
                }
                .padding(4)
            }
            .scrollIndicators(.hidden)
        }
        .glassEffect(.regular, in: .rect(cornerRadius: 16))
        .inputAccessory()
    }

    private func mentionLabel(_ member: Agent, fill: Bool) -> some View {
        let first = member.id == mentionTarget?.id
        return HStack(spacing: 0) {
            Text("@\(member.name)").lineLimit(1).layoutPriority(1)
            if member.id == "everyone" || first {
                if fill { Spacer(minLength: 8) }
                else { Color.clear.frame(width: 8) }
                HStack(spacing: 8) {
                    if member.id == "everyone" {
                        Text("All members").font(.caption).foregroundStyle(.muted)
                            .lineLimit(1).layoutPriority(-1)
                    }
                    if first {
                        Image(systemName: "return").font(.caption).foregroundStyle(.muted)
                            .fixedSize().accessibilityHidden(true)
                    }
                }
            } else if fill { Spacer(minLength: 0) }
        }
        .padding(.horizontal, 12).padding(.vertical, 8)
        .frame(minHeight: 38)
    }

    private func mention(_ agent: Agent) {
        guard let query = mentionQuery else { return }
        let replaced = NSRange(query.start..., in: text)
        mentions = MentionToken.removing(mentions, range: replaced)
        let token = "@\(agent.name)"
        var updated = text
        updated.replaceSubrange(query.start..., with: token + " ")
        dictationDraft.edit(from: text, to: updated)
        editText(updated, range: replaced)
        if agent.id != "everyone" {
            mentions.append(MentionToken(id: agent.id, text: token,
                                         range: NSRange(location: replaced.location, length: token.utf16.count)))
        }
        selection = TextSelection(insertionPoint: text.endIndex)
        focused = true
    }

    private var draftText: Binding<String> {
        Binding(get: { text }, set: {
            dictationDraft.edit(from: text, to: $0)
            editText($0)
        })
    }

    private func editText(_ updated: String, range: NSRange? = nil) {
        let selected: NSRange?
        if let range { selected = range }
        // The field's caret belongs to its own text, which `text` can't index once it's been set from elsewhere.
        else if case let .selection(bounds) = selection?.indices,
                bounds.lowerBound >= updated.startIndex, bounds.upperBound <= updated.endIndex {
            selected = NSRange(bounds, in: updated)
        } else { selected = nil }
        mentions = MentionToken.updated(mentions, from: text, to: updated, selection: selected)
        text = updated
    }

    private func attachmentButton(_ title: String, icon: String, action: @escaping () -> Void) -> some View {
        AttachmentRow(title: title, icon: icon) {
            adding = nil
            action()
        }
    }

    private func insert(_ value: String) {
        #if os(macOS)
        let place = held
        #else
        let place = selection
        #endif
        var at = text.endIndex
        if case let .selection(range) = place?.indices, range.isEmpty, range.lowerBound <= text.endIndex {
            at = range.lowerBound
        }
        var updated = text
        updated.insert(contentsOf: value, at: at)
        caret = at.utf16Offset(in: text) + value.utf16.count
        dictationDraft.edit(from: text, to: updated)
        editText(updated, range: NSRange(at..<at, in: text))
    }

    private var hasContent: Bool { !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !files.isEmpty }
    private var recording: Bool { dictation.ready && !dictation.stopping }
    private var isExpanded: Bool { !recording && (!files.isEmpty || reply != nil) }

    private var controlSize: CGFloat { ComposerMetrics.control }

    private func addFile(_ url: URL) async throws {
        let file = try await Task.detached(priority: .userInitiated) {
            let scoped = url.startAccessingSecurityScopedResource()
            defer { if scoped { url.stopAccessingSecurityScopedResource() } }
            let source = (url as NSURL).filePathURL ?? url
            guard try source.resourceValues(forKeys: [.isDirectoryKey]).isDirectory != true else {
                throw CocoaError(.fileReadUnsupportedScheme)
            }
            let folder = FileManager.default.temporaryDirectory.appending(path: UUID().uuidString, directoryHint: .isDirectory)
            try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
            let copy = folder.appending(path: source.lastPathComponent)
            try FileManager.default.copyItem(at: source, to: copy)
            return DraftFile(url: copy, kind: UTType(filenameExtension: source.pathExtension) ?? .data)
        }.value
        withAnimation(motion) { files.append(file) }
    }

    private func toggleDictation() {
        if dictation.active {
            Task { await stopDictation(polish: true) }
            return
        }
        error = nil
        if case let .selection(bounds) = selection?.indices,
           bounds.lowerBound >= text.startIndex, bounds.upperBound <= text.endIndex {
            insertion = NSRange(bounds, in: text)
        } else { insertion = NSRange(location: text.utf16.count, length: 0) }
        focused = false
        adding = nil
        ignoresDictation = false
        dictationDraft.stop()
        dictated = nil
        dictation.start { value in
            guard !ignoresDictation else { return }
            if let result = dictationDraft.update(value.text, start: value.start, final: value.final,
                                                  text: text, selection: insertion) {
                editText(result.text, range: result.range)
                insertion = NSRange(location: result.cursor, length: 0)
                dictated = (dictated?.lowerBound ?? result.range.location)..<result.cursor
                let cursor = String.Index(utf16Offset: result.cursor, in: text)
                selection = TextSelection(insertionPoint: cursor)
            }
        } failure: { _ in
            error = dictation.denied ? "Allow microphone and speech recognition in Settings." : "Dictation isn’t available right now."
        }
    }

    private func stopDictation(polish: Bool = false) async {
        await dictation.stop()
        dictationDraft.stop()
        guard polish, let span = dictated else { return }
        dictated = nil
        let before = text
        let utf16 = Array(before.utf16)
        guard span.upperBound <= utf16.count else { return }
        let raw = String(decoding: utf16[span], as: UTF16.self)
        polishing = true
        defer { polishing = false }
        // Apply only if the draft is untouched, so edits made meanwhile are never overwritten.
        guard let better = await DictationPolish.run(raw), better != raw, text == before else { return }
        var updated = Array(utf16)
        updated.replaceSubrange(span, with: Array(better.utf16))
        let result = String(decoding: updated, as: UTF16.self)
        editText(result, range: NSRange(location: span.lowerBound, length: raw.utf16.count))
        selection = TextSelection(insertionPoint: String.Index(utf16Offset: span.lowerBound + better.utf16.count, in: result))
    }

    private func submit() {
        if dictation.active { ignoresDictation = true; Task { await stopDictation() } }
        guard hasContent, !loadingPhotos else { return }
        let draft = text
        let pending = files
        let quote = reply
        guard let targets = MentionToken.targets(mentions, in: draft, members: Set(members.map(\.id))) else {
            error = "A mentioned agent left this chat. Remove or reselect the mention before sending."
            return
        }
        // The bubble appears at once and uploads run behind it, so the composer is free straight away.
        withAnimation(motion) {
            error = nil
            text = ""
            mentions = []
            files.removeAll { file in pending.contains { $0.id == file.id } }
            clearReply()
        }
        focused = true
        Task {
            let local = await Task.detached(priority: .userInitiated) { pending.map(\.local) }.value
            await send(draft, local, quote, targets)
        }
    }
}

struct AttachmentMenu<Rows: View>: View {
    @ViewBuilder let rows: Rows
    @Environment(\.dropdownClose) private var close
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 0) { rows }
            .padding(PanelStyle.cardInset)
            #if os(macOS)
            // Holds focus so Esc reaches the menu rather than the composer.
            .focusable().focused($focused).focusEffectDisabled()
            .task { focused = true }
            .onExitCommand { close() }
            #endif
    }
}

struct AttachmentRow: View {
    let title: String
    let icon: String
    let action: () -> Void
    @Environment(\.isEnabled) private var enabled
    @State private var hovered = false

    var body: some View {
        Button(action: action) {
            HStack(spacing: PanelStyle.rowInset) {
                Image(systemName: icon).frame(width: 24).accessibilityHidden(true)
                Text(title).lineLimit(1).frame(maxWidth: .infinity, alignment: .leading)
            }
            .foregroundStyle(enabled ? Color.primary : Color.muted)
            .padding(.horizontal, PanelStyle.rowInset)
            .frame(minHeight: DropdownStyle.rowHeight)
            .contentShape(.rect(cornerRadius: PanelStyle.rowRadius))
            .background(.primary.opacity(hovered && enabled ? PanelStyle.hoverTint : 0),
                        in: .rect(cornerRadius: PanelStyle.rowRadius))
        }
        .buttonStyle(.plain)
        .accessibilityLabel(title)
        .pointerHover { hovered = $0 }
    }
}

private struct MentionSize: Layout {
    let width: CGFloat
    let height: CGFloat

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        guard let labels = subviews.first else { return .zero }
        let ideal = labels.sizeThatFits(.unspecified)
        return CGSize(width: max(0, min(ideal.width, min(width, proposal.width ?? width))), height: height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        guard subviews.count > 1 else { return }
        subviews[1].place(at: bounds.origin, anchor: .topLeading, proposal: ProposedViewSize(bounds.size))
    }
}

private struct MentionAnchor: PreferenceKey {
    static var defaultValue: Anchor<CGRect>? { nil }

    static func reduce(value: inout Anchor<CGRect>?, nextValue: () -> Anchor<CGRect>?) {
        value = nextValue() ?? value
    }
}

private enum MentionRank {
    static func value(_ name: String, term: String) -> Int? {
        let options: String.CompareOptions = [.caseInsensitive, .diacriticInsensitive, .widthInsensitive]
        let candidate = name.folding(options: options, locale: .current)
        let query = term.folding(options: options, locale: .current)
        if query.isEmpty || candidate == query { return 0 }
        if candidate.hasPrefix(query) { return 1 }
        return candidate.contains(query) ? 2 : nil
    }
}

private struct MentionToken {
    let id: String
    let text: String
    var range: NSRange

    static func removing(_ tokens: [Self], range: NSRange) -> [Self] {
        tokens.filter { NSIntersectionRange($0.range, range).length == 0 }
    }

    static func updated(_ tokens: [Self], from old: String, to new: String, selection: NSRange? = nil) -> [Self] {
        let before = Array(old.utf16)
        let after = Array(new.utf16)
        guard before != after else { return tokens }
        let changed: NSRange
        let inserted: Int
        var ambiguous: NSRange?
        if let edit = nativeEdit(before, after, selection: selection) {
            changed = edit.range
            inserted = edit.inserted
        } else {
            let shared = min(before.count, after.count)
            var prefix = 0
            while prefix < shared, before[prefix] == after[prefix] { prefix += 1 }
            var suffix = 0
            while suffix < shared, before[before.count - suffix - 1] == after[after.count - suffix - 1] { suffix += 1 }
            let bounded = min(suffix, shared - prefix)
            changed = NSRange(location: prefix, length: before.count - prefix - bounded)
            inserted = after.count - prefix - bounded
            let overlap = max(0, prefix + suffix - shared)
            if overlap > 0 {
                ambiguous = NSRange(location: prefix - overlap, length: changed.length + overlap)
            }
        }
        let shift = inserted - changed.length
        return tokens.compactMap { token in
            if let ambiguous, NSIntersectionRange(token.range, ambiguous).length > 0 { return nil }
            if NSIntersectionRange(token.range, changed).length > 0 { return nil }
            if changed.length == 0, inserted > 0,
               changed.location > token.range.location, changed.location < NSMaxRange(token.range) { return nil }
            var token = token
            if NSMaxRange(changed) <= token.range.location { token.range.location += shift }
            return token.valid(in: new) ? token : nil
        }
    }

    private static func nativeEdit(_ before: [UInt16], _ after: [UInt16], selection: NSRange?) -> (range: NSRange, inserted: Int)? {
        guard let selection, selection.location != NSNotFound else { return nil }
        let delta = after.count - before.count
        var candidates = [selection]
        if selection.length == 0, delta < 0 {
            candidates = [NSRange(location: selection.location + delta, length: -delta),
                          NSRange(location: selection.location, length: -delta)]
        }
        var matches: [(range: NSRange, inserted: Int)] = []
        for range in candidates {
            let inserted = delta + range.length
            guard range.location >= 0, range.length >= 0, inserted >= 0,
                  NSMaxRange(range) <= before.count, range.location + inserted <= after.count,
                  before[..<range.location].elementsEqual(after[..<range.location]),
                  before[NSMaxRange(range)...].elementsEqual(after[(range.location + inserted)...]) else { continue }
            if !matches.contains(where: { $0.range == range }) { matches.append((range, inserted)) }
        }
        return matches.count == 1 ? matches[0] : nil
    }

    static func targets(_ tokens: [Self], in draft: String, members: Set<String>) -> [String]? {
        var ids: [String] = []
        for token in tokens where token.valid(in: draft) {
            guard members.contains(token.id) else { return nil }
            if !ids.contains(token.id) { ids.append(token.id) }
        }
        return ids
    }

    private func valid(in draft: String) -> Bool {
        guard let bounds = Range(range, in: draft), draft[bounds] == text else { return false }
        return !Self.word(draft[..<bounds.lowerBound].unicodeScalars.last)
            && !Self.word(draft[bounds.upperBound...].unicodeScalars.first)
    }

    private static func word(_ scalar: Unicode.Scalar?) -> Bool {
        guard let scalar else { return false }
        switch scalar.properties.generalCategory {
        case .uppercaseLetter, .lowercaseLetter, .titlecaseLetter, .modifierLetter, .otherLetter,
             .decimalNumber, .letterNumber, .otherNumber: return true
        default: return scalar == "_" || scalar == "-"
        }
    }
}

#if os(iOS)
struct CameraView: UIViewControllerRepresentable {
    let capture: (URL) -> Void
    @Environment(\.dismiss) private var dismiss

    func makeUIViewController(context: Context) -> UIImagePickerController {
        let picker = UIImagePickerController()
        picker.sourceType = .camera
        picker.delegate = context.coordinator
        return picker
    }

    func updateUIViewController(_ uiViewController: UIImagePickerController, context: Context) {}
    func makeCoordinator() -> Coordinator { Coordinator(parent: self) }

    final class Coordinator: NSObject, UIImagePickerControllerDelegate, UINavigationControllerDelegate {
        let parent: CameraView
        init(parent: CameraView) { self.parent = parent }

        func imagePickerController(_ picker: UIImagePickerController, didFinishPickingMediaWithInfo info: [UIImagePickerController.InfoKey: Any]) {
            guard let image = info[.originalImage] as? UIImage, let data = image.jpegData(compressionQuality: 0.85) else { return }
            let url = FileManager.default.temporaryDirectory.appending(path: "Camera-\(UUID().uuidString.prefix(8)).jpg")
            Task {
                do {
                    try await Task.detached(priority: .userInitiated) { try data.write(to: url) }.value
                    parent.capture(url)
                } catch { parent.dismiss() }
            }
        }

        func imagePickerControllerDidCancel(_ picker: UIImagePickerController) { parent.dismiss() }
    }
}
#endif

struct Waveform: View {
    let level: Float
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var samples: [Float] = []
    @State private var volume: Float = 0
    @State private var envelope: Float = 0

    var body: some View {
        GeometryReader { geometry in
            let count = max(1, Int(ceil(geometry.size.width / 5.5)))
            HStack(spacing: 2.5) {
                ForEach(0..<count, id: \.self) { index in
                    let sample = index < samples.count ? samples[index] : 0
                    Capsule().fill(Color.primary)
                        .frame(width: 3, height: 3 + 15 * CGFloat(sample))
                }
            }
            .frame(width: geometry.size.width, height: geometry.size.height, alignment: .leading)
            .clipped()
            .task(id: count) {
                samples = Array(samples.prefix(count))
                    + Array(repeating: 0, count: max(0, count - samples.count))
                while !Task.isCancelled {
                    let target = sqrt(max(0, (volume - 0.02) / 0.98))
                    envelope += (target - envelope) * (target > envelope ? 0.85 : 0.5)
                    withAnimation(reduceMotion ? nil : .linear(duration: 0.06)) {
                        samples.insert(envelope, at: 0)
                        if samples.count > count { samples.removeLast(samples.count - count) }
                    }
                    do { try await Task.sleep(for: .milliseconds(60)) }
                    catch { break }
                }
            }
        }
        .frame(height: 18)
        .onChange(of: level, initial: true) { _, value in volume = min(1, max(0, value)) }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Recording")
    }
}
