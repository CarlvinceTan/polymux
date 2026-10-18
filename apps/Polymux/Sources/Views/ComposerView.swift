import SwiftUI
import PhotosUI
import UniformTypeIdentifiers
import PolymuxKit

struct DraftFile: Identifiable {
    let id = UUID()
    let url: URL
    let kind: UTType
}

struct ComposerView: View {
    let client: APIClient
    let isVoiceActive: Bool
    let send: (String, [Attachment], ReplyContext?, [String]) async -> Void
    var reply: ReplyContext? = nil
    var agentName = ""
    var clearReply: () -> Void = {}
    var drops: Binding<[URL]> = .constant([])
    var members: [Agent] = []
    @Environment(\.colorScheme) private var colorScheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.scenePhase) private var scenePhase
    @State private var dictation = DictationInput()
    @State private var dictationDraft = DictationDraft()
    @State private var ignoresDictation = false
    @State private var dictated: Range<Int>?
    @State private var polishing = false
    @State private var mentions: [MentionToken] = []
    @State private var text = ""
    @State private var selection: TextSelection?
    @State private var files: [DraftFile] = []
    @State private var photos: [PhotosPickerItem] = []
    @State private var attachments = false
    @State private var importing = false
    @State private var pickingPhotos = false
    @State private var camera = false
    @State private var uploading = false
    @State private var loadingPhotos = false
    @State private var error: String?
    @State private var unsent = false
    @FocusState private var focused: Bool

    var body: some View {
        VStack(alignment: .leading, spacing: 10) {
            controls
            if unsent {
                Button("Unsent", systemImage: "arrow.clockwise", action: submit)
                    .font(.caption).buttonStyle(.plain).foregroundStyle(.secondary)
                    .accessibilityLabel("Unsent. Resend message")
                    .padding(.leading, controlSize + 12)
            } else if let error {
                Text(error).font(.caption2).foregroundStyle(.secondary).padding(.leading, controlSize + 12)
            }
        }
        .disabled(uploading || loadingPhotos)
        .onChange(of: isVoiceActive) { _, active in
            focused = !active
            if active {
                attachments = false
                Task { await stopDictation() }
            }
        }
        .onDisappear { Task { await stopDictation() } }
        .onChange(of: scenePhase) { _, phase in
            if phase == .background { Task { await stopDictation() } }
        }
        .onChange(of: dictation.active) { _, active in
            if !active { dictationDraft.stop() }
        }
        .onChange(of: reply?.messageID) { _, value in
            if value != nil { focused = true }
        }
        .onChange(of: drops.wrappedValue) { _, urls in
            guard !urls.isEmpty else { return }
            drops.wrappedValue = []
            error = nil
            for url in urls {
                do { try addFile(url) }
                catch { self.error = error.localizedDescription }
            }
            focused = true
        }
        .fileImporter(isPresented: $importing, allowedContentTypes: [.item], allowsMultipleSelection: true) { result in
            do {
                for url in try result.get() { try addFile(url) }
            } catch { self.error = error.localizedDescription }
        }
        .photosPicker(isPresented: $pickingPhotos, selection: $photos, maxSelectionCount: 8, matching: .any(of: [.images, .videos]))
        .onChange(of: photos) { _, items in
            guard !items.isEmpty else { return }
            loadingPhotos = true
            Task {
                do {
                    for item in items {
                        let type = item.supportedContentTypes.first ?? .data
                        if let data = try await item.loadTransferable(type: Data.self) {
                            let file = FileManager.default.temporaryDirectory
                                .appending(path: "Photo-\(UUID().uuidString.prefix(8)).\(type.preferredFilenameExtension ?? "bin")")
                            try data.write(to: file, options: .atomic)
                            withAnimation { files.append(DraftFile(url: file, kind: type)) }
                        }
                    }
                    photos = []
                } catch { self.error = error.localizedDescription }
                loadingPhotos = false
            }
        }
        #if os(macOS)
        .panel(isPresented: $camera) {
            CameraView { url in
                withAnimation { files.append(DraftFile(url: url, kind: .jpeg)) }
                camera = false
            }
        }
        #else
        .sheet(isPresented: $camera) {
            CameraView { url in
                withAnimation { files.append(DraftFile(url: url, kind: .jpeg)) }
                camera = false
            }
            .ignoresSafeArea()
        }
        #endif
    }

    private var controls: some View {
        HStack(alignment: .bottom, spacing: 12) {
            Button { attachments.toggle() } label: {
                Image(systemName: "plus").font(.system(size: 17, weight: .regular))
                    .frame(width: controlSize, height: controlSize)
                    .contentShape(Circle())
            }
            .buttonStyle(.plain).frame(width: controlSize, height: controlSize)
            .glassEffect(.regular.interactive(), in: Circle())
            .accessibilityLabel("Add attachment")
            .popover(isPresented: $attachments, attachmentAnchor: .rect(.bounds), arrowEdge: .bottom) {
                VStack(alignment: .leading, spacing: 0) {
                    attachmentButton("Files", icon: "doc") { importing = true }
                    attachmentButton("Photos", icon: "photo") { pickingPhotos = true }
                    attachmentButton("Camera", icon: "camera") { camera = true }
                        #if os(iOS)
                        .disabled(!UIImagePickerController.isSourceTypeAvailable(.camera))
                        #endif
                }
                .padding(6).frame(minWidth: 190)
                .presentationCompactAdaptation(.popover)
            }
            VStack(alignment: .leading, spacing: 10) {
                if let reply {
                    HStack(alignment: .top, spacing: 8) {
                        ReplyPreview(reply: reply, agentName: agentName, client: client)
                            .frame(maxWidth: .infinity, alignment: .leading)
                        Button("Cancel reply", systemImage: "xmark") { clearReply() }
                            .labelStyle(.iconOnly).buttonStyle(.plain)
                            .foregroundStyle(.secondary)
                    }
                }
                if !files.isEmpty {
                    ScrollView(.horizontal) {
                        HStack(alignment: .center, spacing: 10) {
                            ForEach(files) { file in
                                DraftPreview(file: file) {
                                    withAnimation { files.removeAll { $0.id == file.id } }
                                }
                            }
                        }
                    }
                    .scrollIndicators(.hidden)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                }
                TextField("Message", text: draftText, selection: $selection, axis: .vertical)
                    .lineLimit(1...7).textFieldStyle(.plain)
                    .scrollIndicators(.hidden).focused($focused)
                    .opacity(polishing ? 0.5 : 1)
                    .onSubmit {
                        if let first = mentionChoices.first { mention(first) }
                        else { submit() }
                    }
                    .accessibilityLabel("Message")
                    .anchorPreference(key: MentionAnchor.self, value: .bounds) { $0 }
            }
            .padding(.horizontal, 15).padding(.vertical, isExpanded ? 12 : 7)
            .frame(minHeight: controlSize)
            .glassEffect(.regular, in: RoundedRectangle(cornerRadius: isExpanded ? 24 : controlSize / 2))
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
            if uploading || loadingPhotos {
                ProgressView().controlSize(.small).frame(width: controlSize, height: controlSize)
            } else {
                Button { toggleDictation() } label: {
                    Group {
                        if dictation.active || polishing {
                            Waveform(level: dictation.level, animating: polishing)
                        } else {
                            Image(systemName: dictation.denied ? "mic.slash.fill" : "mic.fill").font(.system(size: 15))
                        }
                    }
                        .foregroundStyle(dictation.active ? (colorScheme == .dark ? Color.black : Color.white) : Color.primary)
                        .frame(width: controlSize, height: controlSize)
                        .background(dictation.active ? Color.accentColor : .clear, in: Circle())
                        .overlay {
                            if dictation.active {
                                Circle().stroke(Color.primary, lineWidth: 2)
                                    .scaleEffect(reduceMotion ? 1 : 1 + CGFloat(dictation.level) * 0.18)
                            }
                        }
                }
                .buttonStyle(.plain)
                .glassEffect(.regular.interactive(), in: Circle())
                .accessibilityLabel(dictation.active ? "Stop dictation" : "Start dictation")
                .accessibilityValue(dictation.denied ? "Permission denied" : dictation.active ? "Listening" : "")
                .disabled(dictation.stopping || isVoiceActive)
                if hasContent {
                    Button { submit() } label: {
                        Image(systemName: "arrow.up").font(.system(size: 15, weight: .semibold))
                            .frame(width: controlSize, height: controlSize)
                    }
                    .buttonStyle(.plain)
                    .glassEffect(.regular.interactive(), in: Circle())
                    .accessibilityLabel("Send message")
                }
            }
        }
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
                                .background(member.id == mentionChoices.first?.id ? Color.primary.opacity(0.1) : .clear,
                                            in: .rect(cornerRadius: 10))
                                .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel(members.filter({ $0.name == member.name }).count > 1
                                                ? "Mention \(member.name), \(member.role), \(member.id.prefix(6))"
                                                : "Mention \(member.name)")
                        .accessibilityValue(member.id == mentionChoices.first?.id ? "Top match" : "")
                        .accessibilityHint(member.id == mentionChoices.first?.id ? "Press Return to select" : "")
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
        let first = member.id == mentionChoices.first?.id
        return HStack(spacing: 0) {
            Text("@\(member.name)").lineLimit(1).layoutPriority(1)
            if member.id == "everyone" || first {
                if fill { Spacer(minLength: 8) }
                else { Color.clear.frame(width: 8) }
                HStack(spacing: 8) {
                    if member.id == "everyone" {
                        Text("All members").font(.caption).foregroundStyle(.secondary)
                            .lineLimit(1).layoutPriority(-1)
                    }
                    if first {
                        Image(systemName: "return").font(.caption).foregroundStyle(.secondary)
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
        else if case let .selection(bounds) = selection?.indices,
                bounds.lowerBound >= text.startIndex, bounds.upperBound <= text.endIndex {
            selected = NSRange(bounds, in: text)
        } else { selected = nil }
        mentions = MentionToken.updated(mentions, from: text, to: updated, selection: selected)
        text = updated
    }

    private func attachmentButton(_ title: String, icon: String, action: @escaping () -> Void) -> some View {
        Button {
            attachments = false
            action()
        } label: {
            HStack(spacing: 10) {
                Image(systemName: icon).frame(width: 24, alignment: .center)
                Text(title)
            }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 10).padding(.vertical, 9)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    private var hasContent: Bool { !text.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty || !files.isEmpty }
    private var isExpanded: Bool { !files.isEmpty || reply != nil }

    private var controlSize: CGFloat {
        #if os(macOS)
        32
        #else
        44
        #endif
    }

    private func addFile(_ url: URL) throws {
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
        let kind = UTType(filenameExtension: source.pathExtension) ?? .data
        withAnimation { files.append(DraftFile(url: copy, kind: kind)) }
    }

    private func toggleDictation() {
        if dictation.active {
            Task { await stopDictation(polish: true) }
            return
        }
        guard !dictation.denied else { error = "Allow microphone and speech recognition in Settings."; return }
        error = nil
        focused = true
        ignoresDictation = false
        dictationDraft.stop()
        dictated = nil
        dictation.start { value in
            guard !ignoresDictation else { return }
            let range: NSRange
            if case let .selection(bounds) = selection?.indices,
               bounds.lowerBound >= text.startIndex, bounds.upperBound <= text.endIndex {
                range = NSRange(bounds, in: text)
            } else { range = NSRange(location: text.utf16.count, length: 0) }
            if let result = dictationDraft.update(value.text, start: value.start, final: value.final,
                                                  text: text, selection: range) {
                editText(result.text, range: result.range)
                dictated = (dictated?.lowerBound ?? result.range.location)..<result.cursor
                let cursor = String.Index(utf16Offset: result.cursor, in: text)
                selection = TextSelection(insertionPoint: cursor)
            }
        } failure: { failure in
            error = dictation.denied ? "Allow microphone and speech recognition in Settings." : failure.localizedDescription
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
        guard hasContent, !uploading, !loadingPhotos else { return }
        let draft = text
        let pending = files
        let quote = reply
        guard let targets = MentionToken.targets(mentions, in: draft, members: Set(members.map(\.id))) else {
            error = "A mentioned agent left this chat. Remove or reselect the mention before sending."
            return
        }
        uploading = true
        error = nil
        unsent = false
        Task {
            do {
                var attachments: [Attachment] = []
                for file in pending { attachments.append(try await client.upload(file: file.url)) }
                text = ""
                mentions = []
                let submitted = Set(pending.map(\.id))
                withAnimation { files.removeAll { submitted.contains($0.id) } }
                await send(draft, attachments, quote, targets)
            } catch { unsent = true }
            uploading = false
            focused = true
        }
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
            do {
                try data.write(to: url)
                parent.capture(url)
            } catch { parent.dismiss() }
        }

        func imagePickerControllerDidCancel(_ picker: UIImagePickerController) { parent.dismiss() }
    }
}
#endif

/// Level bars shown in the mic button while listening; pulses while the text is being polished.
private struct Waveform: View {
    let level: Float
    let animating: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    private let shape: [CGFloat] = [0.5, 0.8, 1, 0.7, 0.4]

    var body: some View {
        TimelineView(.animation(paused: reduceMotion || !animating)) { tick in
            let time = tick.date.timeIntervalSinceReferenceDate
            HStack(spacing: 2.5) {
                ForEach(shape.indices, id: \.self) { index in
                    let wave = animating ? 0.5 + 0.5 * sin(time * 6 + Double(index)) : Double(level)
                    Capsule().frame(width: 3, height: 5 + 13 * shape[index] * max(0.15, wave))
                }
            }
        }
        .animation(.easeOut(duration: 0.08), value: level)
        .accessibilityHidden(true)
    }
}
