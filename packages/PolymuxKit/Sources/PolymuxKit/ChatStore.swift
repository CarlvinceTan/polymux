import Foundation
import Observation

@MainActor @Observable
public final class ChatStore {
    public let activity: ActivityStore
    public let trace = SignpostTrace()
    public private(set) var runs: [Run] = []
    public private(set) var threadID: String
    public private(set) var messages: [Message] = []
    public private(set) var isConnected = false
    public var isVisible = true
    /// Per-file progress for sends still uploading, keyed by the local attachment id.
    public private(set) var uploads: [String: Double] = [:]
    public var isSending: Bool { flushing || !uploads.isEmpty }
    public private(set) var error: String?
    public private(set) var hasEarlier = true
    public private(set) var pendingReactions: [String: ReactionRequest] = [:]
    public private(set) var reactionErrors: [String: String] = [:]
    @ObservationIgnored private var reactionVersions: [String: Int64] = [:]
    @ObservationIgnored private var loadingEarlier = false
    @ObservationIgnored public var onUpdate: (@MainActor (Event, ContinuousClock.Instant) -> Void)?
    @ObservationIgnored private let client: APIClient
    @ObservationIgnored private let cacheActor: LocalCacheActor
    @ObservationIgnored private let uploader: AttachmentUploader
    @ObservationIgnored private weak var sidebar: SidebarStore?
    @ObservationIgnored private var streamTask: Task<Void, Never>?
    @ObservationIgnored private var uploadTask: Task<Void, Never>?
    @ObservationIgnored private var loading: Task<Void, Never>?
    @ObservationIgnored private var historyTask: Task<Void, Never>?
    @ObservationIgnored private var readTask: Task<Void, Never>?
    @ObservationIgnored private var historyID = UUID()
    @ObservationIgnored private var readAgain = false
    @ObservationIgnored private var flushing = false
    @ObservationIgnored private var queued: [String: Message] = [:]

    public init(client: APIClient, actor: LocalCacheActor, threadID: String, sidebar: SidebarStore? = nil,
                uploadClient: (any UploadClient)? = nil) {
        self.client = client
        self.cacheActor = actor
        self.threadID = threadID
        self.sidebar = sidebar
        uploader = AttachmentUploader(client: uploadClient ?? client)
        activity = sidebar?.activity ?? ActivityStore()
        loading = Task { [weak self] in await self?.prepare() }
    }

    /// Waits for the opening cache read. Later reads go through the same actor.
    public func ready() async {
        await loading?.value
    }

    private func prepare() async {
        let threadID = self.threadID
        let stored = (try? await cacheActor.call { try $0.runs(threadID: threadID) }) ?? []
        runs = stored
        activity.restore(stored)
        messages = (try? await cacheActor.call { try $0.messages(threadID: threadID, limit: 100) }) ?? []
        for message in queued.values { update(message) }
        Task {
            await sweepUploads()
            await resumeUploads()
        }
    }

    public func start() async {
        guard streamTask == nil else { return }
        guard let resolved = await sidebar?.resolved(threadID) ?? (threadID.hasPrefix("local-") ? nil : threadID) else { return }
        threadID = resolved
        await ready()
        streamTask = Task { [weak self] in await self?.connect() }
        uploadTask = Task { [weak self] in
            await self?.sweepUploads()
            while !Task.isCancelled {
                if self?.isConnected == true { await self?.resumeUploads() }
                do { try await Task.sleep(for: .seconds(3)) } catch { return }
            }
        }
    }

    public func stop() {
        streamTask?.cancel()
        streamTask = nil
        uploadTask?.cancel()
        uploadTask = nil
        historyID = UUID()
        historyTask?.cancel()
        historyTask = nil
        readTask?.cancel()
        readTask = nil
        isConnected = false
    }

    public func reactions(for message: Message) -> [Reaction] {
        var reactions = message.reactions ?? []
        if let pending = pendingReactions[message.id] {
            reactions.removeAll { $0.agentID == nil && $0.emoji == pending.emoji }
            if !pending.remove { reactions.append(Reaction(emoji: pending.emoji)) }
        }
        return reactions
    }

    public func react(messageID: String, emoji: String) async {
        guard pendingReactions[messageID] == nil, Reaction.isEmoji(emoji),
              let message = messages.first(where: { $0.id == messageID }), message.seq > 0,
              [MessageStatus.done, MessageStatus.error].contains(message.status) else { return }
        let remove = (message.reactions ?? []).contains { $0.emoji == emoji && $0.agentID == nil }
        let request = ReactionRequest(emoji: emoji, remove: remove)
        let version = reactionVersions[messageID]
        pendingReactions[messageID] = request
        reactionErrors[messageID] = nil
        defer { pendingReactions[messageID] = nil }
        do {
            let accepted = try await client.react(threadID: threadID, messageID: messageID, request: request)
            if reactionVersions[messageID] == version,
               let index = messages.firstIndex(where: { $0.id == messageID }) {
                messages[index].reactions = accepted.reactions
                let cached = try? await cacheActor.call {
                    try $0.setReactions(id: messageID, previous: message.reactions, reactions: accepted.reactions)
                }
                if reactionVersions[messageID] == version, let cached,
                   let index = messages.firstIndex(where: { $0.id == messageID }) {
                    messages[index].reactions = cached.reactions
                }
            }
        } catch {
            reactionErrors[messageID] = "Couldn’t update this reaction."
        }
    }

    public func send(text: String, attachments: [Attachment] = [], reply: ReplyContext? = nil, mentions: [String] = []) async {
        await send(text: text, attachments: attachments, id: UUID().uuidString, reply: reply, mentions: mentions)
    }

    /// Shows the bubble immediately; returns once files are kept, without waiting for uploads.
    public func send(text: String, files: [LocalFile], reply: ReplyContext? = nil,
                     mentions: [String] = [], at tap: ContinuousClock.Instant = ContinuousClock().now) async {
        let text = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty || !files.isEmpty else { return }
        let id = UUID().uuidString
        let parts = (text.isEmpty ? [] : [Part(kind: PartKind.text, text: text)])
            + files.map { Part(kind: PartKind.attachment, attachment: $0.placeholder) }
        var message = Message(id: id, threadID: threadID, role: MessageRole.user, parts: parts,
                              createdAt: WireDate.string(), status: MessageStatus.pending, seq: 0, reply: reply,
                              mentions: mentions.isEmpty ? nil : mentions)
        queued[id] = message
        update(message)
        if threadID.hasPrefix("local-") {
            guard let resolved = await sidebar?.resolved(threadID) else {
                message.status = MessageStatus.failed
                queued[id] = message
                update(message)
                return
            }
            threadID = resolved
            message.threadID = resolved
            queued[id] = message
            update(message)
        }
        await ready()
        queued[id] = nil
        guard await cacheActor.claim(id) else { return }
        let stored = message
        do {
            try await cacheActor.call { try $0.merge([stored]) }
        } catch {
            await cacheActor.unclaim(id)
            messages.removeAll { $0.id == id }
            self.error = error.localizedDescription
            return
        }
        // The bubble and its pending record landed; the span marks that from the tap, with the
        // uploads still ahead. The ring starts on every file.
        InstantUI.interval("send.bubble", budget: InstantUI.feedbackBudget, trace: trace, from: tap) {
            onUpdate?(Event(seq: 0, conversationID: threadID, type: EventType.messageStart, message: message,
                            createdAt: message.createdAt), tap)
            for file in files { uploads[file.id] = 0 }
        }
        do {
            let folder = cacheActor.uploads.appending(path: id, directoryHint: .isDirectory)
            let kept = try await Task.detached(priority: .userInitiated) { try Self.keep(files, in: folder) }.value
            try await relocate(messageID: id, files: kept)
            startUploads(id, files: kept)
        } catch {
            await failUploads(messageID: id, files: files, error: error)
            await cacheActor.unclaim(id)
        }
    }

    /// Uploads a claimed message's files; the claim ends with the task.
    private func startUploads(_ messageID: String, files: [LocalFile]) {
        let uploader = self.uploader, cache = cacheActor
        Task.detached(priority: .userInitiated) { [weak self] in
            guard let store = self else { await cache.unclaim(messageID); return }
            do {
                // A copy this app owns survives a crash or quit, so the upload can pick up where it stopped.
                let kept = try Self.keep(files, in: cache.uploads.appending(path: messageID, directoryHint: .isDirectory))
                try await store.relocate(messageID: messageID, files: kept)
                try await uploader.run(kept,
                    progress: { id, value in
                        Task { @MainActor in store.setUpload(id, value) }
                    },
                    finished: { id, attachment in
                        try await store.attach(messageID: messageID, localID: id, with: attachment)
                    })
                await store.commit(messageID: messageID, files: kept)
            } catch {
                await store.failUploads(messageID: messageID, files: files, error: error)
            }
            await cache.unclaim(messageID)
        }
    }

    /// Copies files into this device's upload folder unless they're already there. The caller's file is never moved.
    nonisolated static func keep(_ files: [LocalFile], in folder: URL) throws -> [LocalFile] {
        let manager = FileManager.default
        return try files.map { file in
            if file.url.path.hasPrefix(folder.path + "/") { return file }
            let destination = folder.appending(path: file.id, directoryHint: .isDirectory)
            let copy = destination.appending(path: file.url.lastPathComponent)
            if !manager.fileExists(atPath: copy.path) {
                try manager.createDirectory(at: destination, withIntermediateDirectories: true)
                // Copied under another name first, so an interrupted copy is never taken for a whole file.
                let partial = destination.appending(path: ".partial")
                try? manager.removeItem(at: partial)
                try manager.copyItem(at: file.url, to: partial)
                try manager.moveItem(at: partial, to: copy)
            }
            return LocalFile(id: file.id, url: copy, name: file.name, kind: file.kind, mimeType: file.mimeType, size: file.size)
        }
    }

    /// A file still waiting to upload: its attachment points at a copy on this device.
    nonisolated static func local(_ attachment: Attachment) -> LocalFile? {
        guard attachment.indexingState == AttachmentIndexingState.pending,
              let url = URL(string: attachment.url), url.isFileURL else { return nil }
        return LocalFile(id: attachment.id, url: url, name: attachment.name, kind: attachment.kind,
                         mimeType: attachment.mimeType, size: attachment.size)
    }

    /// Points the message at the kept copies and saves it, so a relaunch finds them.
    private func relocate(messageID: String, files: [LocalFile]) async throws {
        let urls = Dictionary(files.map { ($0.id, $0.url.absoluteString) }, uniquingKeysWith: { first, _ in first })
        guard let message = try await cacheActor.call({ cache in
            try cache.updateUnsent(id: messageID) { message in
                for part in message.parts.indices {
                    if let attachment = message.parts[part].attachment, Self.local(attachment) != nil, let url = urls[attachment.id] {
                        message.parts[part].attachment?.url = url
                    }
                }
            }
        }) else { throw CancellationError() }
        update(message)
    }

    /// Picks up uploads that a crash, quit or lost connection interrupted, unless they're already running.
    private func resumeUploads() async {
        let threadID = self.threadID
        guard let stored = try? await cacheActor.call({ try $0.unsent(threadID: threadID) }) else { return }
        let waiting = stored.filter {
            $0.role == MessageRole.user && [MessageStatus.pending, MessageStatus.failed].contains($0.status)
        }
        for candidate in waiting {
            guard await cacheActor.claim(candidate.id) else { continue }
            guard let message = try? await cacheActor.call({ try $0.pendingUpload(id: candidate.id) }) else {
                await cacheActor.unclaim(candidate.id)
                continue
            }
            let files = message.attachments.compactMap(Self.local)
            if files.isEmpty {
                await commit(messageID: message.id, files: [])
                await cacheActor.unclaim(message.id)
                continue
            }
            for file in files { uploads[file.id] = 0 }
            if message.status != MessageStatus.pending {
                if let pending = try? await cacheActor.call({ try $0.updateUnsent(id: message.id) { $0.status = MessageStatus.pending } }) {
                    update(pending)
                }
            }
            startUploads(message.id, files: files)
        }
    }

    /// Deletes kept copies that no unsent message needs any more.
    private func sweepUploads() async {
        let folder = cacheActor.uploads
        let names = await Task.detached { (try? FileManager.default.contentsOfDirectory(atPath: folder.path)) ?? [] }.value
        for name in names {
            guard await !cacheActor.claimed(name), (try? await cacheActor.call({ try $0.unsent(id: name) })) == false else { continue }
            let stale = folder.appending(path: name, directoryHint: .isDirectory)
            await Task.detached { try? FileManager.default.removeItem(at: stale) }.value
        }
    }

    private func setUpload(_ localID: String, _ value: Double) {
        guard uploads[localID] != nil else { return }
        uploads[localID] = value
    }

    /// Swaps one local file ref for its server attachment in place, then drops the ring.
    private func attach(messageID: String, localID: String, with attachment: Attachment) async throws {
        guard let message = try await cacheActor.call({ cache in
            try cache.updateUnsent(id: messageID) { message in
                for part in message.parts.indices where message.parts[part].attachment?.id == localID {
                    message.parts[part].attachment = attachment
                }
            }
        }) else { throw CancellationError() }
        uploads[localID] = nil
        update(message)
    }

    /// Uploads finished: queue the send with server attachment ids and let `flush` deliver it.
    private func commit(messageID: String, files: [LocalFile]) async {
        do {
            guard let message = try await cacheActor.call({ try $0.enqueueUpload(id: messageID) }) else { return }
            update(message)
            for file in files { uploads[file.id] = nil }
            let folder = cacheActor.uploads.appending(path: messageID, directoryHint: .isDirectory)
            await Task.detached { try? FileManager.default.removeItem(at: folder) }.value
            await flush()
        } catch { self.error = error.localizedDescription }
    }

    /// SA2 owns unsent state and retry; this only stops the rings and reports why.
    private func failUploads(messageID: String, files: [LocalFile], error: any Error) async {
        for file in files { uploads[file.id] = nil }
        // A refusal or a missing file won't change on its own, so only network trouble is tried again later.
        let missing = (error as? CocoaError).map { [.fileNoSuchFile, .fileReadNoSuchFile].contains($0.code) } ?? false
        var permanent = missing
        if case let ClientError.response(code, _) = error, (400..<500).contains(code), code != 408, code != 429 { permanent = true }
        let status = permanent ? MessageStatus.error : MessageStatus.failed
        if let message = try? await cacheActor.call({ try $0.updateUnsent(id: messageID) { $0.status = status } }) {
            update(message)
        }
        self.error = missing ? "A file in this message is no longer on this device." : error.localizedDescription
    }

    public func retry(messageID: String) async {
        let threadID = self.threadID
        await ready()
        guard !flushing, let message = messages.first(where: { $0.id == messageID }), message.role == MessageRole.user,
              [MessageStatus.pending, MessageStatus.failed, MessageStatus.error].contains(message.status) else { return }
        if (try? await cacheActor.call({ try $0.accepted(id: messageID) })) == true { return }
        try? await cacheActor.call { try $0.setStatus(id: messageID, status: MessageStatus.pending) }
        if let message = try? await cacheActor.call({ try $0.message(id: messageID) }) { update(message) }
        if let pending = try? await cacheActor.call({ try $0.pending(threadID: threadID) }), pending.contains(where: { $0.id == messageID }) {
            await flush()
        } else {
            await resumeUploads()
        }
    }

    /// Deletes the user's message before it reaches the server, so it is never sent.
    public func discard(messageID: String) async {
        guard !flushing, let message = messages.first(where: { $0.id == messageID }), message.role == MessageRole.user,
              [MessageStatus.pending, MessageStatus.failed, MessageStatus.error].contains(message.status) else { return }
        if (try? await cacheActor.call({ try $0.accepted(id: messageID) })) == true { return }
        do { try await cacheActor.call { try $0.discard(id: messageID) } } catch { return }
        let folder = cacheActor.uploads.appending(path: messageID, directoryHint: .isDirectory)
        await Task.detached { try? FileManager.default.removeItem(at: folder) }.value
        queued[messageID] = nil
        messages.removeAll { $0.id == messageID }
        sidebar?.withdraw(messageID, in: threadID, previous: messages.last)
    }

    public func markRead() async {
        await markRead(acknowledge: isConnected)
    }

    private func markRead(acknowledge: Bool) async {
        let threadID = self.threadID
        guard isVisible else { return }
        let tap = ContinuousClock().now
        let known = max(sidebar?.lastSeq(threadID) ?? 0, messages.map(\.seq).max() ?? 0)
        sidebar?.clearUnread(threadID, seq: known)
        do {
            let stored = try await cacheActor.call { try $0.cursor(threadID: threadID) }
            let seq = max(known, stored)
            if seq != known { sidebar?.clearUnread(threadID, seq: seq) }
            try await cacheActor.markRead(threadID: threadID, seq: seq)
            onUpdate?(Event(seq: seq, conversationID: threadID, type: EventType.messageRead, createdAt: WireDate.string()), tap)
            if acknowledge, !Task.isCancelled { try await client.markRead(threadID: threadID, seq: seq) }
        } catch {
            if !Task.isCancelled, !error.isOffline { self.error = "Couldn’t update read status." }
        }
    }

    public func loadEarlier() async {
        let threadID = self.threadID
        guard hasEarlier, !loadingEarlier, let first = messages.filter({ $0.seq > 0 }).map(\.seq).min() else { return }
        loadingEarlier = true
        defer { loadingEarlier = false }
        do {
            var earlier = try await cacheActor.call { try $0.messages(threadID: threadID, before: first, limit: 100) }
            for message in earlier { update(message) }
            if earlier.count < 100 {
                let fetched = try await client.messages(threadID: threadID, before: first)
                try await cacheActor.call { try $0.merge(fetched) }
                earlier = try await cacheActor.call { try $0.messages(threadID: threadID, before: first, limit: 100) }
                for message in earlier { update(message) }
            }
            hasEarlier = earlier.count == 100
            loadHistory(before: first)
        } catch { if !error.isOffline { self.error = "Couldn’t load earlier messages." } }
    }

    private func send(text: String, attachments: [Attachment], id: String, reply: ReplyContext? = nil, mentions: [String] = []) async {
        let text = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty || !attachments.isEmpty else { return }
        if threadID.hasPrefix("local-") {
            let parts = (text.isEmpty ? [] : [Part(kind: PartKind.text, text: text)])
                + attachments.map { Part(kind: PartKind.attachment, attachment: $0) }
            let message = Message(id: id, threadID: threadID, role: MessageRole.user, parts: parts,
                                  createdAt: WireDate.string(), status: MessageStatus.pending, reply: reply,
                                  mentions: mentions.isEmpty ? nil : mentions)
            queued[id] = message
            update(message)
        }
        guard let resolved = await sidebar?.resolved(threadID) ?? (threadID.hasPrefix("local-") ? nil : threadID) else {
            if var message = queued[id] {
                message.status = MessageStatus.failed
                queued[id] = message
                update(message)
            }
            self.error = "Couldn’t send this message. Check your connection and try again."
            return
        }
        threadID = resolved
        for (key, var message) in queued {
            message.threadID = resolved
            queued[key] = message
            update(message)
        }
        let threadID = resolved
        do {
            let request = SendRequest(id: id, text: text, attachmentIDs: attachments.map(\.id), replyToID: reply?.messageID,
                                      replyPart: reply?.partIndex, mentions: mentions.isEmpty ? nil : mentions)
            let message = try await cacheActor.call { try $0.enqueue(threadID: threadID, request: request, attachments: attachments, reply: reply) }
            queued[id] = nil
            update(message)
            onUpdate?(Event(seq: message.seq, conversationID: threadID, type: EventType.messageStart, message: message, createdAt: message.createdAt), ContinuousClock().now)
            await flush()
        } catch {
            self.error = error.localizedDescription
        }
    }

    private func connect() async {
        let threadID = self.threadID
        var delay: UInt64 = 1
        while !Task.isCancelled {
            do {
                try await repairSequences()
                var cursor = try await cacheActor.call { try $0.cursor(threadID: threadID) }
                let visibleCount = max(100, messages.count)
                while true {
                    let replay = try await client.events(threadID: threadID, after: cursor)
                    try Task.checkCancellation()
                    if replay.isEmpty { break }
                    for event in replay { try await receive(event, live: false) }
                    cursor = try await cacheActor.call { try $0.cursor(threadID: threadID) }
                }
                messages = try await cacheActor.call { try $0.messages(threadID: threadID, limit: visibleCount) }
                // The replay filled the cache, so a page short of the limit holds the whole history.
                hasEarlier = messages.count >= visibleCount
                for message in queued.values { update(message) }
                runs = try await cacheActor.call { try $0.runs(threadID: threadID) }
                activity.restore(runs)
                isConnected = true
                error = nil
                loadHistory()
                Task { await flush(); await resumeUploads() }
                scheduleRead()
                cursor = try await cacheActor.call { try $0.cursor(threadID: threadID) }
                for try await event in client.stream(threadID: threadID, after: cursor) {
                    try Task.checkCancellation()
                    try await receive(event)
                    if event.type == EventType.messageStart || event.type == EventType.messageDone || event.type == EventType.activity {
                        scheduleRead()
                    }
                    delay = 1
                }
                throw ClientError.unavailable
            } catch {
                if Task.isCancelled { return }
                isConnected = false
                self.error = error.isOffline ? nil : "Couldn’t update this chat."
            }
            do { try await Task.sleep(for: .milliseconds(Int64(delay * 1000) + Int64.random(in: 0...250))) }
            catch { return }
            delay = min(delay * 2, 15)
        }
    }

    private func scheduleRead() {
        readAgain = true
        guard readTask == nil else { return }
        readTask = Task {
            while readAgain, !Task.isCancelled {
                readAgain = false
                await markRead(acknowledge: true)
            }
            readTask = nil
        }
    }

    private func loadHistory(before: Int64? = nil) {
        guard historyTask == nil else { return }
        let id = UUID()
        historyID = id
        historyTask = Task {
            defer { if historyID == id { historyTask = nil } }
            guard let history = try? await client.toolRuns(threadID: threadID, before: before),
                  historyID == id, !Task.isCancelled else { return }
            for run in history {
                guard run.seq >= (runs.first(where: { $0.id == run.id })?.seq ?? -1) else { continue }
                runs.removeAll { $0.id == run.id }
                runs.append(run)
            }
            activity.restore(runs)
        }
    }

    private func repairSequences() async throws {
        let threadID = self.threadID
        let outstanding = try await cacheActor.call { try $0.repairIDs(threadID: threadID) }
        guard !outstanding.isEmpty else { return }
        var before: Int64?
        while true {
            try Task.checkCancellation()
            let snapshots = try await client.messages(threadID: threadID, before: before)
            try await cacheActor.call { try $0.repairSequences(snapshots) }
            if try await cacheActor.call({ try $0.repairIDs(threadID: threadID).isEmpty }) { return }
            guard let next = snapshots.map(\.seq).min(), next > 0, next != before else {
                try await cacheActor.call { try $0.finishRepair(threadID: threadID) }
                return
            }
            before = next
        }
    }

    func receive(_ event: Event, live: Bool = true) async throws {
        let threadID = self.threadID
        guard event.conversationID == threadID else { return }
        let tap = ContinuousClock().now
        onUpdate?(event, tap)
        let touchThreads = sidebar == nil
        let applied = try await cacheActor.call { cache -> (message: Message?, runs: [Run]?) in
            let message: Message?
            if event.seq > (try cache.cursor(threadID: threadID)) {
                message = try cache.apply(event, touchThreads: touchThreads)
            } else {
                // A shared-cache reader may have applied this event before this view received it.
                message = try (event.messageID ?? event.message?.id).flatMap { try cache.message(id: $0) }
            }
            return (message, event.run == nil ? nil : try cache.runs(threadID: threadID))
        }
        // Catch-up publishes one cached page, not the historical messages passing through it.
        if live, let message = applied.message, message.status != MessageStatus.silent { update(message) }
        if event.type == EventType.messageReactions, let id = event.messageID ?? event.message?.id {
            reactionVersions[id] = event.seq
            reactionErrors[id] = nil
        }
        if live, let message = applied.message, message.status == MessageStatus.silent {
            messages.removeAll { $0.id == message.id }
        }
        activity.receive(event, live: live)
        if live, let runs = applied.runs { self.runs = runs }
        if live { isConnected = true }
    }

    private func update(_ message: Message) {
        let index: Int
        if let existing = messages.lastIndex(where: { $0.id == message.id }) {
            guard messages[existing].seq <= message.seq else { return }
            messages[existing] = message
            index = existing
        } else {
            messages.append(message)
            index = messages.endIndex - 1
        }
        // A streamed update keeps its place; sort only when the message moved past a neighbour.
        let afterPrevious = index == 0 || !Self.precedes(messages[index], messages[index - 1])
        let beforeNext = index == messages.endIndex - 1 || !Self.precedes(messages[index + 1], messages[index])
        if !(afterPrevious && beforeNext) { messages.sort(by: Self.precedes) }
    }

    /// Transcript order: accepted messages by sequence, then unsent ones by creation time.
    private static func precedes(_ lhs: Message, _ rhs: Message) -> Bool {
        if lhs.seq == 0 && rhs.seq == 0 { return lhs.createdAt < rhs.createdAt }
        if lhs.seq == 0 { return false }
        if rhs.seq == 0 { return true }
        return lhs.seq < rhs.seq
    }

    private func flush() async {
        let threadID = self.threadID
        guard !flushing else { return }
        flushing = true
        defer { flushing = false }
        do {
            while let request = try await cacheActor.call({ try $0.pending(threadID: threadID) }).first {
                do {
                    try await cacheActor.call { try $0.setStatus(id: request.id, status: MessageStatus.pending) }
                    if let message = try await cacheActor.call({ try $0.message(id: request.id) }) { update(message) }
                    try await sidebar?.syncEdit(threadID)
                    try await cacheActor.deliver(client: client, threadID: threadID, request: request)
                    if let message = try await cacheActor.call({ try $0.message(id: request.id) }) { update(message) }
                    error = nil
                } catch {
                    if try await cacheActor.call({ try $0.accepted(id: request.id) }) {
                        try await cacheActor.call { try $0.acknowledge(id: request.id) }
                        continue
                    }
                    if case let ClientError.response(code, _) = error, (400..<500).contains(code), code != 408, code != 429 {
                        try await cacheActor.call { try $0.setStatus(id: request.id, status: MessageStatus.error) }
                        if let message = try await cacheActor.call({ try $0.message(id: request.id) }) { update(message) }
                        self.error = error.localizedDescription
                        continue
                    }
                    let pending = try await cacheActor.call { try $0.pending(threadID: threadID) }
                    for pending in pending {
                        try await cacheActor.call { try $0.setStatus(id: pending.id, status: MessageStatus.failed) }
                        if let message = try await cacheActor.call({ try $0.message(id: pending.id) }) { update(message) }
                    }
                    isConnected = false
                    self.error = error.localizedDescription
                    break
                }
            }
        } catch {
            self.error = error.localizedDescription
        }
    }
}
