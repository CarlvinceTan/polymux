import SwiftUI
import PolymuxKit
import UniformTypeIdentifiers

/// One line of a mailbox: who, what, then the first of the body beside the message's marks.
private struct HubMailRow: View {
    let envelope: HubMailEnvelope
    let chain: Int

    var body: some View {
        VStack(alignment: .leading, spacing: 3) {
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(envelope.from.isEmpty ? "Unknown sender" : envelope.from).font(.body.weight(envelope.seen ? .regular : .semibold)).lineLimit(1)
                Spacer(minLength: 4)
                Text(HubRow.when(envelope.at)).font(.caption).foregroundStyle(.muted).lineLimit(1).layoutPriority(1)
            }
            Text(envelope.subject.isEmpty ? "No subject" : envelope.subject).font(.subheadline.weight(envelope.seen ? .regular : .medium)).lineLimit(1)
            HStack(alignment: .firstTextBaseline, spacing: 6) {
                Text(envelope.preview.isEmpty ? " " : envelope.preview).font(.subheadline).foregroundStyle(.muted).lineLimit(1)
                Spacer(minLength: 4)
                marks
            }
        }
        .padding(.vertical, 4)
        .contentShape(Rectangle())
        .accessibilityElement(children: .combine)
    }

    private var marks: some View {
        HStack(spacing: 5) {
            if envelope.importance == HubMailEnvelopeImportance.high { mark("bolt.fill", "Important") }
            if envelope.flagged { mark("flag.fill", "Flagged") }
            if envelope.answered { mark("arrowshape.turn.up.left", "Replied") }
            if envelope.attachments > 0 { mark("paperclip", "Attachment") }
            if chain > 1 { Text("\(chain)").font(.caption).foregroundStyle(.muted).accessibilityLabel("\(chain) messages") }
        }
    }

    private func mark(_ symbol: String, _ label: String) -> some View {
        Image(systemName: symbol).font(.caption2).foregroundStyle(.muted).accessibilityLabel(label)
    }
}

/// A mailbox in the sidebar: its folder and account, search and filters, then its messages.
struct HubMailList: View {
    @Bindable var store: HubStore
    let client: APIClient
    @State private var searched = ""
    @State private var scrolled = false
    private var mail: HubMailStore { store.mail }

    var body: some View {
        @Bindable var mail = store.mail
        VStack(spacing: PanelStyle.rowInset) {
            picker
            HStack(spacing: PanelStyle.rowInset) {
                HStack { PanelSearch("Search mail", text: $mail.search) }
                    .padding(.horizontal, PanelStyle.headingInset)
                    .frame(height: 32)
                    .glassEffect(.regular, in: Capsule())
                filters
                Button("New mail", systemImage: "square.and.pencil") { Task { await mail.start() } }
                    .labelStyle(.iconOnly).buttonStyle(.plain).help("New mail")
            }
            .padding(.horizontal, PanelStyle.headingInset)
            if !mail.chosen.isEmpty { selection }
            rows
        }
        .task(id: mail.search) {
            guard mail.search != searched else { return }
            try? await Task.sleep(for: .milliseconds(300))
            guard !Task.isCancelled else { return }
            searched = mail.search
            await mail.load()
        }
    }

    @ViewBuilder private var picker: some View {
        @Bindable var mail = store.mail
        HStack(spacing: PanelStyle.rowInset) {
            if let account = mail.account, let folders = mail.folders[account], !folders.isEmpty {
                Picker("Folder", selection: Binding(get: { mail.folder }, set: { name in Task { await mail.select(account, folder: name) } })) {
                    ForEach(folders, id: \.name) { folder in
                        Text(folder.unread > 0 ? "\(folder.label) (\(folder.unread))" : folder.label).tag(folder.name)
                    }
                }
                .labelsHidden()
            }
            Spacer(minLength: 0)
            if mail.accounts.count > 1 {
                Picker("Mailbox", selection: Binding(get: { mail.account ?? "" }, set: { id in Task { await mail.select(id) } })) {
                    ForEach(mail.accounts, id: \.id) { Text($0.email).tag($0.id) }
                }
                .labelsHidden()
            } else if let current = mail.current {
                Text(current.email).font(.caption).foregroundStyle(.muted).lineLimit(1)
            }
        }
        .padding(.horizontal, PanelStyle.headingInset)
    }

    private var filters: some View {
        PanelMenu("Filter messages", symbol: mail.filter == .all ? "line.3.horizontal.decrease" : "line.3.horizontal.decrease.circle.fill") {
            Section("Filter") {
                filter(.all, "All", "envelope")
                filter(.unread, "Unread", "envelope.badge")
                filter(.flagged, "Flagged", "flag")
                filter(.attachments, "Attachments", "paperclip")
            }
            Section("Sort") {
                sort(.newest, "Newest first")
                sort(.oldest, "Oldest first")
                sort(.subject, "Subject")
                sort(.sender, "Sender")
            }
        }
        .frame(width: 24, height: 24)
    }

    private func filter(_ value: HubMailFilter, _ title: String, _ symbol: String) -> some View {
        Button { mail.filter = value } label: { Label(title, systemImage: mail.filter == value ? "checkmark" : symbol) }
    }

    private func sort(_ value: HubMailSort, _ title: String) -> some View {
        Button {
            mail.sort = value
            Task { await mail.load() }
        } label: {
            Label(title, systemImage: mail.sort == value ? "checkmark" : "arrow.up.arrow.down")
        }
    }

    private var erasing: Bool { ["trash", "junk"].contains(mail.currentFolder?.role) }

    private var selection: some View {
        ScrollView(.horizontal) {
            HStack(spacing: PanelStyle.rowInset) {
                Text("\(mail.chosen.count) selected").font(.subheadline).foregroundStyle(.muted)
                bulk("Mark read", "envelope.open", .read)
                bulk("Mark unread", "envelope.badge", .unread)
                bulk("Flag", "flag", .flag)
                bulk("Archive", "archivebox", .archive)
                bulk("Junk", "xmark.bin", .junk)
                bulk("Trash", "trash", .trash)
                if erasing { bulk("Delete forever", "xmark", .erase) }
                Button("Clear selection", systemImage: "xmark.circle") { mail.chosen = [] }
                    .labelStyle(.iconOnly).buttonStyle(.plain).help("Clear selection")
            }
            .padding(.horizontal, PanelStyle.headingInset)
        }
        .scrollIndicators(.hidden)
        .disabled(mail.bulkBusy)
    }

    private func bulk(_ title: String, _ symbol: String, _ action: HubMailBulk) -> some View {
        Button(title, systemImage: symbol) { Task { await mail.bulk(action) } }
            .labelStyle(.iconOnly).buttonStyle(.plain).help(title)
    }

    private func picked(_ envelope: HubMailEnvelope) {
        #if os(macOS)
        let flags = NSEvent.modifierFlags
        Task { await mail.pick(envelope, adding: flags.contains(.command), extending: flags.contains(.shift)) }
        #else
        Task { await mail.pick(envelope, adding: !mail.chosen.isEmpty) }
        #endif
    }

    private func highlighted(_ envelope: HubMailEnvelope) -> Bool {
        mail.chosen.contains(envelope.id) || (mail.chosen.isEmpty && mail.open?.id == envelope.id)
    }

    private var emptyText: String {
        if !mail.search.isEmpty { return "No results found." }
        return mail.filter == .all ? "No messages." : "None match this filter."
    }

    @ViewBuilder private var rows: some View {
        let shown = mail.visible
        if shown.isEmpty {
            Text(emptyText).foregroundStyle(.muted).multilineTextAlignment(.center)
                .padding(.horizontal, PanelStyle.contentInset)
                .frame(maxWidth: .infinity, maxHeight: .infinity)
        } else {
            let chains = HubMailFormat.chains(mail.envelopes)
            ScrollViewReader { reader in
                ScrollView {
                    LazyVStack(spacing: 0) {
                        ForEach(shown, id: \.id) { envelope in
                            Button { picked(envelope) } label: {
                                HubMailRow(envelope: envelope, chain: chains[HubMailFormat.baseSubject(envelope.subject)] ?? 1)
                                    .padding(.horizontal, PanelStyle.headingInset + PanelStyle.rowInset)
                                    .padding(.vertical, 2)
                                    .background(RoundedRectangle(cornerRadius: PanelStyle.rowRadius)
                                        .fill(.primary.opacity(highlighted(envelope) ? PanelStyle.selectedTint : 0))
                                        .padding(.horizontal, PanelStyle.rowInset))
                            }
                            .buttonStyle(.plain)
                            .accessibilityLabel(envelope.subject.isEmpty ? "No subject" : envelope.subject)
                            .accessibilityValue(envelope.from)
                            .accessibilityAddTraits(highlighted(envelope) ? .isSelected : [])
                            .contextMenu { Button("Select") { mail.chosen.insert(envelope.id) } }
                            .id(envelope.id)
                            .onAppear {
                                guard envelope.id == shown.last?.id, mail.moreToLoad, !mail.loading else { return }
                                Task { await mail.load(more: true) }
                            }
                        }
                        if mail.moreToLoad {
                            Button("Load more") { Task { await mail.load(more: true) } }
                                .buttonStyle(.plain).foregroundStyle(.muted).frame(maxWidth: .infinity)
                                .padding(.vertical, PanelStyle.rowInset).disabled(mail.loading)
                        }
                    }
                }
                .onScrollGeometryChange(for: Bool.self) { $0.contentOffset.y > Self.topOffset } action: { _, far in scrolled = far }
                .overlay(alignment: .top) {
                    if scrolled, let first = shown.first {
                        Button("Back to top", systemImage: "arrow.up") { withAnimation { reader.scrollTo(first.id, anchor: .top) } }
                            .labelStyle(.iconOnly).buttonStyle(.plain).help("Back to top")
                            .frame(width: 32, height: 32).glassEffect(.regular.interactive(), in: Circle())
                            .padding(.top, PanelStyle.rowInset)
                    }
                }
            }
        }
    }

    private static let topOffset = 400.0
}

/// An open message: who it's from, what can be done with it, then the sender's own page.
struct HubLetter: View {
    @Bindable var store: HubStore
    let envelope: HubMailEnvelope
    @Environment(\.colorScheme) private var scheme
    @State private var expanded = false
    @State private var opening: URL?

    private var mail: HubMailStore { store.mail }

    var body: some View {
        VStack(alignment: .leading, spacing: PanelStyle.contentInset) {
            heading
            actions
            Divider()
            if let letter = mail.letter, letter.envelope.id == envelope.id {
                MailPage(html: HubMailPage.document(letter, dark: scheme == .dark),
                         kind: { part in letter.files.first { $0.part == part }?.mime ?? "application/octet-stream" },
                         load: { part in await mail.file(envelope, part: part) },
                         open: { part in open(part, letter) })
                    .id(letter.envelope.id)
                if mail.thread.count > 1 { chain }
            } else if mail.unreadable {
                Text("Couldn’t open this message.").foregroundStyle(.muted).frame(maxWidth: .infinity, maxHeight: .infinity)
            } else {
                skeleton
            }
            if let issue = mail.issue { QuietNote(text: issue) }
        }
        .padding(.horizontal, PanelStyle.formInset).padding(.top, 52).padding(.bottom, PanelStyle.contentInset)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .quickLookPreview($opening)
    }

    private func line(_ label: String, _ people: [HubMailAddress]) -> some View {
        Text("\(label): \(people.map { $0.name.isEmpty ? $0.address : $0.name }.joined(separator: ", "))")
            .font(.subheadline).foregroundStyle(.muted).lineLimit(2).textSelection(.enabled)
    }

    private var location: String {
        let folder = mail.currentFolder?.label ?? envelope.folder
        return mail.current.map { "\(folder) – \($0.name)" } ?? folder
    }

    private var heading: some View {
        VStack(alignment: .leading, spacing: PanelStyle.cardInset) {
            HStack(alignment: .firstTextBaseline, spacing: PanelStyle.rowInset) {
                Text(envelope.from.isEmpty ? "Unknown sender" : envelope.from).font(.subheadline.weight(.semibold)).lineLimit(1)
                Spacer(minLength: PanelStyle.rowInset)
                Text(location).font(.caption).foregroundStyle(.muted).lineLimit(1)
                Text(HubRow.when(envelope.at)).font(.caption).foregroundStyle(.muted).lineLimit(1)
            }
            Text(envelope.subject.isEmpty ? "No subject" : envelope.subject).font(.title3.weight(.semibold)).lineLimit(3).textSelection(.enabled)
            if let letter = mail.letter, letter.envelope.id == envelope.id {
                if !letter.to.isEmpty { line("To", letter.to) }
                if !letter.cc.isEmpty { line("Cc", letter.cc) }
                if !letter.bcc.isEmpty { line("Bcc", letter.bcc) }
            }
        }
    }

    private var answerable: Bool { mail.letter?.envelope.id == envelope.id }

    private var actions: some View {
        ScrollView(.horizontal) {
            HStack(spacing: PanelStyle.rowInset) {
                action("arrowshape.turn.up.left", "Reply") { await mail.start(.reply) }.disabled(!answerable)
                action("arrowshape.turn.up.left.2", "Reply all") { await mail.start(.replyAll) }.disabled(!answerable)
                action("arrowshape.turn.up.right", "Forward") { await mail.start(.forward) }.disabled(!answerable)
                action("archivebox", "Archive") { await mail.move(envelope, role: "archive") }
                action("xmark.bin", "Junk") { await mail.move(envelope, role: "junk") }
                action("trash", "Trash") { await mail.move(envelope, role: "trash") }
                PanelMenu("Move to folder", symbol: "folder") {
                    ForEach((mail.folders[envelope.account] ?? []).filter { $0.name != envelope.folder }, id: \.name) { folder in
                        Button(folder.label) { Task { await mail.move(envelope, to: folder.name) } }
                    }
                }
                .frame(width: 28, height: 28)
                .glassEffect(.regular, in: Circle())
                action(envelope.flagged ? "flag.fill" : "flag", envelope.flagged ? "Unflag" : "Flag") { await mail.toggleFlag(envelope) }
                action("envelope.badge", "Mark unread") { await mail.markUnread(envelope) }
                if ["trash", "junk"].contains(mail.currentFolder?.role) {
                    action("xmark", "Delete forever") { await mail.erase([envelope.id]) }
                }
            }
        }
        .scrollIndicators(.hidden)
    }

    private func action(_ symbol: String, _ label: String, run: @escaping () async -> Void) -> some View {
        PanelNavButton(symbol: symbol, label: label) { Task { await run() } }
    }

    private var chain: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text("Conversation · \(mail.thread.count) messages").font(.caption).foregroundStyle(.muted)
            ForEach(expanded ? mail.thread : Array(mail.thread.prefix(HubMailStore.chainHead)), id: \.id) { item in
                Button { Task { await mail.read(item) } } label: {
                    HStack {
                        Text(item.from).font(.subheadline.weight(item.id == envelope.id ? .semibold : .regular)).lineLimit(1)
                        Spacer()
                        Text(HubRow.when(item.at)).font(.caption).foregroundStyle(.muted).lineLimit(1).layoutPriority(1)
                    }
                    .contentShape(.rect)
                }
                .buttonStyle(.plain)
            }
            if mail.thread.count > HubMailStore.chainHead {
                Button(expanded ? "Show less" : "Show all \(mail.thread.count)") { expanded.toggle() }
                    .buttonStyle(.plain).font(.caption).foregroundStyle(.muted)
            }
        }
    }

    /// The shape of a message, so the pane a click opened only fills in.
    private var skeleton: some View {
        VStack(alignment: .leading, spacing: 10) {
            ForEach([0.92, 0.78, 0.86, 0.6, 0.72], id: \.self) { width in
                GeometryReader { size in
                    Capsule().fill(.primary.opacity(PanelStyle.hoverTint)).frame(width: size.size.width * width, height: 10)
                }
                .frame(height: 10)
            }
            Spacer()
        }
        .accessibilityLabel("Loading message")
    }

    /// Saves a file somewhere Quick Look can read it, under the name it arrived with.
    private func open(_ part: String, _ letter: HubMailMessage) {
        guard let file = letter.files.first(where: { $0.part == part }) else { return }
        Task {
            guard let data = await mail.file(envelope, part: part) else {
                mail.issue = "Couldn’t open \(file.name)."
                return
            }
            let folder = FileManager.default.temporaryDirectory.appending(path: "hub-mail-\(UUID().uuidString)", directoryHint: .isDirectory)
            try? FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
            let path = folder.appending(path: URL(filePath: file.name).lastPathComponent)
            if (try? data.write(to: path)) != nil { opening = path }
        }
    }
}

/// Writes a message in Hub's content pane. Everything typed is kept in Drafts as it goes.
struct HubCompose: View {
    @Bindable var store: HubStore
    let draft: HubMailCompose
    @Environment(\.colorScheme) private var scheme
    @State private var copies = false
    @State private var picking = false
    @State private var caret = 0
    @State private var sending = false

    private static let limit = 25 << 20
    private var mail: HubMailStore { store.mail }
    private var signatures: [HubMailSignature] { mail.accounts.first { $0.id == draft.account }?.signatures ?? [] }

    private func field(_ path: WritableKeyPath<HubMailCompose, String>) -> Binding<String> {
        Binding(get: { draft[keyPath: path] }, set: { value in
            var next = draft
            next[keyPath: path] = value
            mail.edited(next)
        })
    }

    var body: some View {
        VStack(spacing: PanelStyle.rowInset) {
            HStack(spacing: PanelStyle.rowInset) {
                #if os(macOS)
                PanelNavButton(symbol: "chevron.backward", label: "Back") { Task { await mail.leave() } }
                #endif
                Text(draft.title).font(.headline).lineLimit(1)
                Spacer()
                if let current = mail.accounts.first(where: { $0.id == draft.account }) {
                    Text(current.email).font(.caption).foregroundStyle(.muted).lineLimit(1)
                }
            }
            fields
            Divider()
            ScrollView { message }
            if !draft.signatureText.isEmpty || !draft.signatureHtml.isEmpty { signaturePreview }
            if let issue = mail.issue { QuietNote(text: issue) }
            footer
        }
        .padding(.horizontal, PanelStyle.formInset).padding(.top, 52).padding(.bottom, PanelStyle.contentInset)
        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .topLeading)
        .onAppear { copies = !draft.cc.isEmpty || !draft.bcc.isEmpty; caret = draft.text.count }
        .fileImporter(isPresented: $picking, allowedContentTypes: [.item], allowsMultipleSelection: true) { result in
            guard case let .success(urls) = result else { return }
            add(urls)
        }
    }

    private func label(_ title: String) -> some View {
        Text(title).font(.subheadline).foregroundStyle(.muted).lineLimit(1).frame(width: 76, alignment: .leading)
    }

    private func line(_ title: String, text: Binding<String>, prompt: String = "") -> some View {
        HStack(spacing: PanelStyle.rowInset) {
            label(title)
            TextField(title, text: text, prompt: Text(prompt)).textFieldStyle(.plain).labelsHidden().autocorrectionDisabled()
                .accessibilityLabel(title)
        }
    }

    private var fields: some View {
        VStack(spacing: PanelStyle.rowInset) {
            HStack {
                line("To", text: field(\.to), prompt: "name@example.com")
                if !copies {
                    Button("Cc/Bcc") { copies = true }.buttonStyle(.plain).font(.subheadline).foregroundStyle(.muted)
                }
            }
            if copies {
                line("Cc", text: field(\.cc), prompt: "name@example.com")
                line("Bcc", text: field(\.bcc), prompt: "name@example.com")
            }
            line("Subject", text: field(\.subject))
            if !signatures.isEmpty {
                HStack(spacing: PanelStyle.rowInset) {
                    label("Signature")
                    Picker("Signature", selection: Binding(get: { draft.signature }, set: chooseSignature)) {
                        Text("None").tag("")
                        ForEach(signatures, id: \.id) { Text($0.name).tag($0.id) }
                    }
                    .labelsHidden().fixedSize()
                    Spacer()
                }
            }
        }
    }

    private func chooseSignature(_ id: String) {
        var next = draft
        let chosen = signatures.first { $0.id == id }
        next.signature = chosen?.id ?? ""
        next.signatureText = chosen?.text ?? ""
        next.signatureHtml = chosen?.html ?? ""
        mail.edited(next)
    }

    /// Text in runs, with each file drawn where the caret was when it was attached.
    private var message: some View {
        let segments = HubMailFormat.segments(draft.text, files: draft.files)
        return VStack(alignment: .leading, spacing: PanelStyle.rowInset) {
            ForEach(Array(segments.enumerated()), id: \.offset) { index, segment in
                MailTextRun(text: Binding(get: { segment.text }, set: { value in
                    var next = draft
                    HubMailFormat.edit(&next, segment: index, text: value)
                    mail.edited(next)
                }), label: index == 0 ? "Message" : "Message \(index + 1)", minimum: index == segments.count - 1 ? 180 : 28) { offset in
                    caret = segment.from + offset
                }
                if let file = segment.file { card(file) }
            }
        }
    }

    private func card(_ file: HubMailPiece) -> some View {
        HStack(spacing: 6) {
            Label(file.name, systemImage: "doc").font(.subheadline).lineLimit(1)
            Spacer(minLength: 4)
            Button("Remove \(file.name)", systemImage: "xmark") {
                var next = draft
                next.files.removeAll { $0.id == file.id }
                mail.edited(next)
            }
            .labelStyle(.iconOnly).buttonStyle(.plain).foregroundStyle(.muted)
        }
        .padding(.horizontal, 10).padding(.vertical, 6)
        .background(.primary.opacity(0.05), in: .rect(cornerRadius: PanelStyle.rowRadius))
    }

    private var signaturePreview: some View {
        VStack(alignment: .leading, spacing: 2) {
            Text("Signature").font(.caption).foregroundStyle(.muted)
            MailPage(html: HubMailPage.signature(draft.signatureHtml, text: draft.signatureText, dark: scheme == .dark))
                .frame(height: 72)
                .accessibilityLabel("Signature preview")
        }
    }

    private var footer: some View {
        HStack(spacing: PanelStyle.rowInset) {
            Button("Attach", systemImage: "paperclip") { picking = true }.buttonStyle(.bordered).labelStyle(.iconOnly).help("Attach")
            Toggle(isOn: Binding(get: { draft.important }, set: { value in
                var next = draft
                next.important = value
                mail.edited(next)
            })) {
                Label("Mark important", systemImage: "bolt").labelStyle(.iconOnly)
            }
            .toggleStyle(.button).help("Mark important").accessibilityLabel("Mark important")
            Spacer()
            Button("Discard", systemImage: "trash") { Task { await mail.discard() } }.buttonStyle(.bordered).labelStyle(.iconOnly).help("Discard")
            Button("Save") { Task { await mail.saveDraft() } }.buttonStyle(.bordered).lineLimit(1).fixedSize()
            Button("Send") {
                sending = true
                Task {
                    defer { sending = false }
                    await mail.send()
                }
            }
            .buttonStyle(.borderedProminent)
            .lineLimit(1).fixedSize()
            .disabled(draft.to.trimmingCharacters(in: .whitespaces).isEmpty || sending)
        }
    }

    private func add(_ urls: [URL]) {
        var total = 0
        for url in urls {
            let access = url.startAccessingSecurityScopedResource()
            defer { if access { url.stopAccessingSecurityScopedResource() } }
            guard let data = try? Data(contentsOf: url, options: .mappedIfSafe), total + data.count <= Self.limit else {
                mail.issue = "Those files are too large to send."
                return
            }
            total += data.count
            let kind = UTType(filenameExtension: url.pathExtension)?.preferredMIMEType ?? "application/octet-stream"
            Task { await mail.attach(name: url.lastPathComponent, mime: kind, data: data, at: caret) }
        }
    }
}

/// A run of a message's text that's as tall as what's in it, and reports where the caret is.
private struct MailTextRun: View {
    @Binding var text: String
    let label: String
    let minimum: CGFloat
    let caret: (Int) -> Void
    @State private var selection: TextSelection?

    var body: some View {
        TextEditor(text: $text, selection: $selection)
            .scrollDisabled(true)
            .scrollContentBackground(.hidden)
            .font(.body)
            .frame(minHeight: minimum, maxHeight: .infinity)
            .fixedSize(horizontal: false, vertical: true)
            .accessibilityLabel(label)
            .onChange(of: selection) { _, value in
                guard case let .selection(range)? = value?.indices, range.lowerBound <= text.endIndex else { return }
                caret(text.distance(from: text.startIndex, to: range.lowerBound))
            }
    }
}
