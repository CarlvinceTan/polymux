import Foundation
import Observation

public enum HubMailFilter: String, CaseIterable, Sendable {
    case all, unread, flagged, attachments
}

public enum HubMailSort: String, CaseIterable, Sendable {
    case newest = "date-desc", oldest = "date-asc", subject, sender = "from"
}

public enum HubMailBulk: Sendable {
    case read, unread, flag, unflag, archive, junk, trash, erase
}

/// A message being written. Text and signature stay apart until it leaves the app.
public struct HubMailCompose: Codable, Hashable, Sendable {
    public var localID = UUID().uuidString
    public var account: String
    public var to = ""
    public var cc = ""
    public var bcc = ""
    public var subject = ""
    public var text = ""
    public var signature = ""
    public var signatureText = ""
    public var signatureHtml = ""
    public var files: [HubMailPiece] = []
    public var important = false
    public var inReplyTo: String?
    public var references: [String] = []
    public var remote: HubMailDraft?
    public var title = "New message"

    public init(account: String) { self.account = account }
}

/// Hub's mailboxes: what's cached of them, the one on show and the message being written.
@MainActor @Observable
public final class HubMailStore {
    public nonisolated static let chainHead = 4
    private nonisolated static let pageSize = 50
    private nonisolated static let prefetchDepth = 12
    private nonisolated static let warmBodies = 4
    private nonisolated static let saveDelay = Duration.milliseconds(800)

    public private(set) var accounts: [HubMailAccount] = []
    public private(set) var folders: [String: [HubMailFolder]] = [:]
    public private(set) var envelopes: [HubMailEnvelope] = []
    public private(set) var open: HubMailEnvelope?
    public private(set) var letter: HubMailMessage?
    /// Whether the open message's body couldn't be read.
    public private(set) var unreadable = false
    public private(set) var thread: [HubMailEnvelope] = []
    public private(set) var moreToLoad = false
    public private(set) var compose: HubMailCompose?
    public private(set) var loading = false
    public private(set) var bulkBusy = false
    public private(set) var inboxes: [String: [HubMailEnvelope]] = [:]
    public var account: String?
    public var folder = "INBOX"
    public var filter = HubMailFilter.all
    public var sort = HubMailSort.newest
    public var search = ""
    public var chosen: Set<Int64> = []
    public var issue: String?
    @ObservationIgnored private let client: APIClient
    @ObservationIgnored private let cache: LocalCacheActor
    @ObservationIgnored private var page = 1
    @ObservationIgnored private var letters: [String: HubMailMessage] = [:]
    @ObservationIgnored private var anchor: Int64?
    @ObservationIgnored private var prefetching = false
    @ObservationIgnored private var saveTimer: Task<Void, Never>?
    @ObservationIgnored private var saving: Task<Void, Never>?
    @ObservationIgnored private var unsaved: HubMailCompose?
    /// The copy in Drafts each message owns, kept past the composer so a save that's still running can be replaced.
    @ObservationIgnored private var remotes: [String: HubMailDraft] = [:]
    @ObservationIgnored private var restored: Set<String> = []

    init(client: APIClient, cache: LocalCacheActor) {
        self.client = client
        self.cache = cache
    }

    private var plugin: String { HubStore.plugin }
    private nonisolated static func pageKey(_ account: String, _ folder: String) -> String { "hub-mail-page-\(account)|\(folder)" }
    private nonisolated static func letterKey(_ account: String, _ folder: String, _ id: Int64) -> String { "hub-mail-letter-\(account)|\(folder)|\(id)" }
    private nonisolated static func composeKey(_ account: String) -> String { "hub-mail-compose-\(account)" }

    public var current: HubMailAccount? { accounts.first { $0.id == account } }

    public var currentFolder: HubMailFolder? { account.flatMap { folders[$0] }?.first { $0.name == folder } }

    /// The list after its filter, and after what's typed, before the server has answered.
    public var visible: [HubMailEnvelope] {
        let needle = search.trimmingCharacters(in: .whitespacesAndNewlines).lowercased()
        return envelopes.filter { envelope in
            switch filter {
            case .all: true
            case .unread: !envelope.seen
            case .flagged: envelope.flagged
            case .attachments: envelope.attachments > 0
            }
        }.filter { envelope in
            // A query with a field in it means something only the server can answer.
            needle.isEmpty || needle.contains(" ") || [envelope.from, envelope.address, envelope.subject, envelope.preview].contains { $0.lowercased().contains(needle) }
        }
    }

    /// Every inbox's latest messages, for the combined list.
    public var recent: [HubMailEnvelope] { inboxes.values.flatMap { $0 }.sorted { $0.at > $1.at } }

    func update(accounts next: [HubMailAccount]) {
        accounts = next
        for id in inboxes.keys where !next.contains(where: { $0.id == id }) { inboxes[id] = nil }
    }

    func restore() async {
        if let saved: [String: [HubMailFolder]] = await cache.snapshot("hub-mail-folders"), folders.isEmpty { folders = saved }
        for (id, list) in folders {
            let inbox = list.first { $0.role == "inbox" }?.name ?? "INBOX"
            if inboxes[id] == nil, let saved: [HubMailEnvelope] = await cache.snapshot(Self.pageKey(id, inbox)) { inboxes[id] = saved }
        }
    }

    /// Readies each mailbox ahead of time. Reading a body here never marks it read.
    func warm() async {
        for item in accounts where !Task.isCancelled {
            guard let list: [HubMailFolder] = try? await client.pluginGet(plugin, "mail/\(item.id)/folders") else { continue }
            await keep(list, for: item.id)
            let inbox = list.first { $0.role == "inbox" }?.name ?? "INBOX"
            let query = [URLQueryItem(name: "folder", value: inbox), URLQueryItem(name: "sort", value: HubMailSort.newest.rawValue)]
            guard let found: [HubMailEnvelope] = try? await client.pluginGet(plugin, "mail/\(item.id)/messages", query: query) else { continue }
            inboxes[item.id] = found
            await cache.saveSnapshot(Self.pageKey(item.id, inbox), found)
            for envelope in found.prefix(Self.warmBodies) where !Task.isCancelled { _ = await body(envelope) }
        }
    }

    private func keep(_ list: [HubMailFolder], for account: String) async {
        guard folders[account] != list else { return }
        folders[account] = list
        await cache.saveSnapshot("hub-mail-folders", folders)
    }

    // MARK: Reading

    /// Opens a mailbox on what's cached, then refreshes. An unsaved message comes back the first time.
    public func select(_ id: String, folder wanted: String? = nil) async {
        if compose?.account != id { await leave() }
        let inbox = folders[id]?.first { $0.role == "inbox" }?.name ?? "INBOX"
        account = id
        folder = wanted ?? inbox
        clearReader()
        chosen = []
        search = ""
        filter = .all
        envelopes = await cache.snapshot(Self.pageKey(id, folder)) ?? []
        if restored.insert(id).inserted, compose == nil, let saved: HubMailCompose = await cache.snapshot(Self.composeKey(id)), !saved.localID.isEmpty {
            compose = saved
        }
        await load()
    }

    /// Fetches the first page, or the next one.
    public func load(more: Bool = false) async {
        guard let account else { return }
        let target = (account, folder, search, sort)
        let next = more ? page + 1 : 1
        var query = [URLQueryItem(name: "folder", value: folder), URLQueryItem(name: "page", value: String(next)),
                     URLQueryItem(name: "sort", value: sort.rawValue)]
        let needle = search.trimmingCharacters(in: .whitespacesAndNewlines)
        if !needle.isEmpty { query.append(URLQueryItem(name: "query", value: needle)) }
        loading = true
        defer { loading = false }
        async let list: [HubMailFolder]? = more ? nil : try? await client.pluginGet(plugin, "mail/\(account)/folders")
        let found: [HubMailEnvelope]?
        do {
            found = try await client.pluginGet(plugin, "mail/\(account)/messages", query: query)
        } catch {
            found = nil
        }
        if let list = await list { await keep(list, for: account) }
        guard target == (self.account ?? "", folder, search, sort), let found else { return }
        page = next
        moreToLoad = found.count >= Self.pageSize
        if more {
            let known = Set(envelopes.map(\.id))
            envelopes += found.filter { !known.contains($0.id) }
        } else {
            envelopes = found
            if needle.isEmpty, sort == .newest {
                await cache.saveSnapshot(Self.pageKey(account, folder), found)
                if currentFolder?.role == "inbox" { inboxes[account] = found }
            }
        }
    }

    /// Keeps what's on show current, without losing pages already loaded further down.
    func poll(showing mailbox: Bool) async {
        guard mailbox else { return await warm() }
        guard account != nil, search.isEmpty else { return }
        let older = envelopes.dropFirst(Self.pageSize)
        let pages = page
        await load()
        guard pages > 1 else { return }
        let known = Set(envelopes.map(\.id))
        envelopes += older.filter { !known.contains($0.id) }
        page = pages
    }

    /// A body, from memory, then disk, then the server. It peeks, so this can load ahead.
    private func body(_ envelope: HubMailEnvelope) async -> HubMailMessage? {
        let key = Self.letterKey(envelope.account, envelope.folder, envelope.id)
        if let saved = letters[key] { return saved }
        if let saved: HubMailMessage = await cache.snapshot(key) {
            letters[key] = saved
            return saved
        }
        return await fetchBody(envelope)
    }

    private func fetchBody(_ envelope: HubMailEnvelope) async -> HubMailMessage? {
        let query = [URLQueryItem(name: "folder", value: envelope.folder)]
        guard let found: HubMailMessage = try? await client.pluginGet(plugin, "mail/\(envelope.account)/messages/\(envelope.id)", query: query) else { return nil }
        let key = Self.letterKey(envelope.account, envelope.folder, envelope.id)
        letters[key] = found
        await cache.saveSnapshot(key, found)
        return found
    }

    /// Opens a message straight away on what the list knows, and fills in the body as it arrives.
    public func read(_ envelope: HubMailEnvelope) async {
        await leave()
        open = envelope
        unreadable = false
        thread = []
        letter = await body(envelope)
        let fresh = await fetchBody(envelope)
        guard open?.id == envelope.id else { return }
        if let fresh { letter = fresh }
        unreadable = letter == nil
        // Reading a body peeks, so this is the one place that marks it read.
        if !envelope.seen {
            setLocal([envelope.id]) { $0.seen = true }
            open?.seen = true
            try? await perform(envelope.account, envelope.folder, HubMailChange(ids: [envelope.id], seen: true))
        }
        guard open?.id == envelope.id else { return }
        await loadThread(envelope)
        await prefetch(after: envelope)
    }

    /// Opens a row from the combined list, in its own mailbox.
    public func openRecent(_ envelope: HubMailEnvelope) async {
        await select(envelope.account, folder: envelope.folder)
        await read(envelopes.first { $0.id == envelope.id } ?? envelope)
    }

    private func loadThread(_ envelope: HubMailEnvelope) async {
        let base = HubMailFormat.baseSubject(envelope.subject)
        guard !base.isEmpty else { return }
        let query = [URLQueryItem(name: "folder", value: envelope.folder), URLQueryItem(name: "query", value: "subject \"\(base.replacingOccurrences(of: "\"", with: ""))\"")]
        guard let found: [HubMailEnvelope] = try? await client.pluginGet(plugin, "mail/\(envelope.account)/messages", query: query),
              open?.id == envelope.id else { return }
        thread = HubMailFormat.chain(found, subject: envelope.subject)
    }

    private func prefetch(after envelope: HubMailEnvelope) async {
        guard !prefetching, let index = envelopes.firstIndex(where: { $0.id == envelope.id }) else { return }
        prefetching = true
        defer { prefetching = false }
        let start = max(0, index - 1)
        for item in envelopes[start..<min(envelopes.count, index + Self.prefetchDepth)] {
            guard folder == envelope.folder, !Task.isCancelled else { return }
            _ = await body(item)
        }
    }

    public func clearReader() {
        open = nil
        letter = nil
        unreadable = false
        thread = []
    }

    // MARK: Changes

    private func perform(_ account: String, _ folder: String, _ change: HubMailChange) async throws {
        try await client.pluginPerform(plugin, "mail/\(account)/messages", method: "PATCH", query: [URLQueryItem(name: "folder", value: folder)], body: change)
    }

    private func setLocal(_ ids: Set<Int64>, _ update: (inout HubMailEnvelope) -> Void) {
        for index in envelopes.indices where ids.contains(envelopes[index].id) { update(&envelopes[index]) }
        if let account, var list = inboxes[account] {
            for index in list.indices where ids.contains(list[index].id) && list[index].folder == folder { update(&list[index]) }
            inboxes[account] = list
        }
    }

    private func removeLocal(_ ids: Set<Int64>) {
        envelopes.removeAll { ids.contains($0.id) }
        if let account { inboxes[account]?.removeAll { ids.contains($0.id) && $0.folder == folder } }
        if let open, ids.contains(open.id) { clearReader() }
    }

    /// Applies a change now and undoes it if the server refuses.
    private func apply(_ ids: [Int64], _ change: HubMailChange, removing: Bool) async {
        guard let account else { return }
        let before = (envelopes, inboxes, open, letter, thread)
        let set = Set(ids)
        if removing {
            removeLocal(set)
        } else {
            setLocal(set) { envelope in
                if let seen = change.seen { envelope.seen = seen }
                if let flagged = change.flagged { envelope.flagged = flagged }
            }
            if let open, set.contains(open.id) {
                if let seen = change.seen { self.open?.seen = seen }
                if let flagged = change.flagged { self.open?.flagged = flagged }
            }
        }
        do {
            try await perform(account, folder, change)
        } catch {
            (envelopes, inboxes, open, letter, thread) = before
            issue = "Couldn’t update that message."
        }
    }

    private func roleFolder(_ role: String) -> String? {
        guard let name = account.flatMap({ folders[$0] })?.first(where: { $0.role == role })?.name else {
            issue = "This mailbox has no \(role) folder."
            return nil
        }
        return name
    }

    public func move(_ envelope: HubMailEnvelope, role: String) async {
        if let name = roleFolder(role) { await move(envelope, to: name) }
    }

    public func move(_ envelope: HubMailEnvelope, to name: String) async {
        await apply([envelope.id], HubMailChange(ids: [envelope.id], folder: name), removing: true)
    }

    public func toggleFlag(_ envelope: HubMailEnvelope) async {
        await apply([envelope.id], HubMailChange(ids: [envelope.id], flagged: !envelope.flagged), removing: false)
    }

    public func markUnread(_ envelope: HubMailEnvelope) async {
        clearReader()
        await apply([envelope.id], HubMailChange(ids: [envelope.id], seen: false), removing: false)
    }

    public func erase(_ ids: [Int64]) async {
        await apply(ids, HubMailChange(ids: ids, erase: true), removing: true)
        chosen = []
    }

    /// Applies one action to everything selected, then drops the selection.
    public func bulk(_ action: HubMailBulk) async {
        let ids = Array(chosen)
        guard !ids.isEmpty else { return }
        bulkBusy = true
        defer { bulkBusy = false }
        switch action {
        case .read: await apply(ids, HubMailChange(ids: ids, seen: true), removing: false)
        case .unread: await apply(ids, HubMailChange(ids: ids, seen: false), removing: false)
        case .flag: await apply(ids, HubMailChange(ids: ids, flagged: true), removing: false)
        case .unflag: await apply(ids, HubMailChange(ids: ids, flagged: false), removing: false)
        case .archive, .junk, .trash:
            let role = action == .archive ? "archive" : action == .junk ? "junk" : "trash"
            guard let name = roleFolder(role) else { return }
            await apply(ids, HubMailChange(ids: ids, folder: name), removing: true)
        case .erase: await apply(ids, HubMailChange(ids: ids, erase: true), removing: true)
        }
        chosen = []
    }

    /// Picks a row. With a modifier it selects, otherwise it opens: a draft goes back to the composer.
    public func pick(_ envelope: HubMailEnvelope, adding: Bool = false, extending: Bool = false) async {
        if adding {
            if chosen.contains(envelope.id) { chosen.remove(envelope.id) } else { chosen.insert(envelope.id) }
            anchor = envelope.id
            return
        }
        if extending, let anchor {
            let ids = visible.map(\.id)
            if let from = ids.firstIndex(of: anchor), let to = ids.firstIndex(of: envelope.id) {
                chosen.formUnion(ids[min(from, to)...max(from, to)])
                return
            }
        }
        chosen = []
        anchor = envelope.id
        if envelope.draft { await edit(envelope) } else { await read(envelope) }
    }

    /// One attachment's bytes, saved so it stays viewable offline.
    public func file(_ envelope: HubMailEnvelope, part: String) async -> Data? {
        let folder = FileManager.default.urls(for: .cachesDirectory, in: .userDomainMask)[0].appending(path: "hub-mail", directoryHint: .isDirectory)
        let name = "\(envelope.account)|\(envelope.folder)|\(envelope.id)|\(part)".addingPercentEncoding(withAllowedCharacters: .alphanumerics) ?? UUID().uuidString
        let path = folder.appending(path: name)
        if let saved = try? Data(contentsOf: path) { return saved }
        guard let data = try? await client.pluginCall(plugin, "mail/\(envelope.account)/messages/\(envelope.id)/files/\(part)", method: "GET",
                                                      query: [URLQueryItem(name: "folder", value: envelope.folder)], body: nil) else { return nil }
        try? FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        try? data.write(to: path)
        return data
    }

    // MARK: Writing

    private func addresses(_ list: [HubMailAddress]) -> String { list.map(\.address).joined(separator: ", ") }

    private func signature(for draft: inout HubMailCompose) {
        guard let saved = accounts.first(where: { $0.id == draft.account }),
              let chosen = saved.signatures?.first(where: { $0.id == saved.defaultSignature }) else { return }
        draft.signature = chosen.id
        draft.signatureText = chosen.text
        draft.signatureHtml = chosen.html
    }

    /// Starts a message from the mailbox on show, answering the open one where that's asked for.
    public func start(_ mode: HubMailMode = .new) async {
        guard let id = account ?? accounts.first?.id else { return }
        await leave()
        var draft = HubMailCompose(account: id)
        if mode != .new, let letter {
            let mine = accounts.first { $0.id == id }?.email ?? ""
            if mode != .forward { draft.to = HubMailFormat.recipients(letter, all: mode == .replyAll, mine: mine).joined(separator: ", ") }
            if mode == .replyAll { draft.cc = letter.cc.map(\.address).filter { $0.lowercased() != mine.lowercased() }.joined(separator: ", ") }
            draft.subject = HubMailFormat.prefixed(letter.envelope.subject, mode: mode)
            draft.text = "\n\n" + HubMailFormat.quote(letter)
            // Only an answer continues a chain; a forward starts its own.
            if mode != .forward {
                draft.title = "Reply"
                if !letter.messageId.isEmpty {
                    draft.inReplyTo = letter.messageId
                    draft.references = letter.references + [letter.messageId]
                }
            } else {
                draft.title = "Forward"
            }
        }
        signature(for: &draft)
        compose = draft
    }

    /// Opens a saved draft. Saving or sending replaces the copy in the folder, and its
    /// signature is ordinary text by now, so the default isn't added again.
    public func edit(_ envelope: HubMailEnvelope) async {
        await leave()
        guard let message = await fetchBody(envelope) else {
            issue = "Couldn’t open that draft."
            return
        }
        clearReader()
        var draft = HubMailCompose(account: envelope.account)
        draft.title = "Edit draft"
        draft.to = addresses(message.to)
        draft.cc = addresses(message.cc)
        draft.bcc = addresses(message.bcc)
        draft.subject = envelope.subject
        draft.text = message.text
        draft.important = envelope.importance == HubMailEnvelopeImportance.high
        draft.references = message.references
        draft.inReplyTo = message.references.last
        draft.remote = HubMailDraft(id: envelope.id, folder: envelope.folder)
        for file in message.files {
            guard let data = await self.file(envelope, part: file.part), let id = await stage(name: file.name, mime: file.mime, data: data) else { continue }
            draft.files.append(HubMailPiece(id: id, name: file.name, contentID: Self.contentID(), offset: draft.text.count))
        }
        compose = draft
    }

    private nonisolated static func contentID() -> String { "mail-\(UUID().uuidString)@polymux.local" }

    private func stage(name: String, mime: String, data: Data) async -> String? {
        let saved: HubMailAttach? = try? await client.pluginWrite(plugin, "mail/uploads", body: HubMailUpload(name: name, mime: mime, data: data.base64EncodedString()))
        return saved?.id
    }

    /// Keeps a file on the server and places it where the caret is.
    public func attach(name: String, mime: String, data: Data, at offset: Int) async {
        guard let id = await stage(name: name, mime: mime, data: data) else {
            issue = "Couldn’t attach \(name)."
            return
        }
        guard var draft = compose else { return }
        draft.files.append(HubMailPiece(id: id, name: name, contentID: Self.contentID(), offset: min(max(offset, 0), draft.text.count)))
        edited(draft)
    }

    /// Takes the latest text. It's kept here at once, and in the mailbox after a pause.
    public func edited(_ draft: HubMailCompose) {
        guard draft != compose else { return }
        compose = draft
        unsaved = draft
        saveTimer?.cancel()
        saveTimer = Task { [weak self] in
            try? await Task.sleep(for: Self.saveDelay)
            guard !Task.isCancelled else { return }
            await self?.keepLocally(draft)
            await self?.flush()
        }
    }

    private func keepLocally(_ draft: HubMailCompose) async {
        await cache.saveSnapshot(Self.composeKey(draft.account), draft)
    }

    private func forget(_ account: String) async {
        await cache.saveSnapshot(Self.composeKey(account), HubMailCompose(account: "").blank)
    }

    private func payload(_ draft: HubMailCompose, saving: Bool) -> HubMailSend {
        HubMailSend(account: draft.account, to: HubMailFormat.addresses(draft.to), cc: HubMailFormat.addresses(draft.cc), bcc: HubMailFormat.addresses(draft.bcc),
                    subject: draft.subject, text: HubMailFormat.body(draft.text, signature: draft.signatureText),
                    html: HubMailFormat.html(draft.text, signature: draft.signatureHtml, files: draft.files), draft: saving ? true : nil,
                    importance: draft.important ? HubMailEnvelopeImportance.high : nil, inReplyTo: draft.inReplyTo, references: draft.references,
                    files: draft.files.map { HubMailAttach(id: $0.id, contentId: $0.contentID) }, replaces: draft.remote)
    }

    /// Saves to Drafts one at a time: two saves at once would both replace the same copy and leave
    /// a duplicate. Typing during a save collapses into one more save straight after it.
    private func flush() async {
        while let saving { await saving.value }
        guard unsaved != nil else { return }
        let run = Task { [weak self] in
            guard let self else { return }
            defer { self.saving = nil }
            while var draft = self.unsaved {
                self.unsaved = nil
                draft.remote = self.remotes[draft.localID] ?? draft.remote
                do {
                    let result: HubMailSent = try await self.client.pluginWrite(self.plugin, "mail/send", body: self.payload(draft, saving: true))
                    if let saved = result.draft {
                        self.remotes[draft.localID] = saved
                        if self.compose?.localID == draft.localID { self.compose?.remote = saved }
                    }
                    if self.unsaved == nil { await self.forget(draft.account) }
                    if self.account == draft.account, self.currentFolder?.role == "drafts" { await self.load() }
                } catch {
                    // It's still saved on this device, and the next edit tries again.
                    self.unsaved = self.unsaved ?? draft
                    return
                }
            }
        }
        saving = run
        await run.value
    }

    /// Saves now rather than after the pause.
    public func saveDraft() async {
        saveTimer?.cancel()
        if let compose, unsaved == nil { unsaved = compose }
        await flush()
        if unsaved != nil { issue = "Draft not saved." }
    }

    /// Closes the composer, leaving what was written in Drafts.
    public func leave() async {
        guard let draft = compose else { return }
        compose = nil
        saveTimer?.cancel()
        if unsaved != nil {
            await keepLocally(draft)
            await flush()
        }
    }

    /// Throws away what's being written, including its copy in Drafts.
    public func discard() async {
        guard let draft = compose else { return }
        saveTimer?.cancel()
        unsaved = nil
        compose = nil
        await saving?.value
        await forget(draft.account)
        if let remote = remotes.removeValue(forKey: draft.localID) ?? draft.remote {
            try? await perform(draft.account, remote.folder, HubMailChange(ids: [remote.id], erase: true))
            if account == draft.account { await load() }
        }
    }

    /// Sends now and closes the composer; a failure brings it back.
    public func send() async {
        guard var draft = compose else { return }
        saveTimer?.cancel()
        unsaved = nil
        compose = nil
        // A save already running finishes first, so sending can replace the copy it made.
        await saving?.value
        draft.remote = remotes[draft.localID] ?? draft.remote
        do {
            let _: HubMailSent = try await client.pluginWrite(plugin, "mail/send", body: payload(draft, saving: false))
            remotes[draft.localID] = nil
            await forget(draft.account)
            if account == draft.account { await load() }
        } catch {
            compose = draft
            issue = "Mail not sent."
        }
    }

    // MARK: Accounts

    /// Checks a mailbox is reachable; what it finds shows with the account.
    func test(_ id: String) async -> Bool {
        let result: HubMailAccount? = try? await client.pluginGet(plugin, "mail/\(id)/health")
        return result != nil
    }

    func saveSignatures(_ id: String, _ value: HubMailSignatures) async -> Bool {
        (try? await client.pluginPerform(plugin, "mail/\(id)/signatures", method: "PUT", body: value)) != nil
    }

    func editMailbox(_ id: String, _ setup: HubMailSetup) async -> Bool {
        let saved: HubMailAccount? = try? await client.pluginWrite(plugin, "mail/\(id)", method: "PATCH", body: setup)
        return saved != nil
    }
}

private extension HubMailCompose {
    /// What's kept once nothing is waiting to be saved. Its empty ID says there's nothing to bring back.
    var blank: HubMailCompose {
        var out = self
        out.localID = ""
        return out
    }
}
