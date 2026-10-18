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
    @ObservationIgnored public var onUpdate: (@MainActor (Event, ContinuousClock.Instant) -> Void)?
    @ObservationIgnored private let client: APIClient
    @ObservationIgnored private let cacheActor: LocalCacheActor
    @ObservationIgnored private let uploader: AttachmentUploader
    @ObservationIgnored private weak var sidebar: SidebarStore?
    @ObservationIgnored private var streamTask: Task<Void, Never>?
    @ObservationIgnored private var loading: Task<Void, Never>?
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
    }

    public func start() async {
        guard streamTask == nil else { return }
        guard let resolved = await sidebar?.resolved(threadID) ?? (threadID.hasPrefix("local-") ? nil : threadID) else { return }
        threadID = resolved
        await ready()
        streamTask = Task { [weak self] in await self?.connect() }
    }

    public func stop() {
        streamTask?.cancel()
        streamTask = nil
        isConnected = false
    }

    public func send(text: String, attachments: [Attachment] = [], reply: ReplyContext? = nil, mentions: [String] = []) async {
        await send(text: text, attachments: attachments, id: UUID().uuidString, reply: reply, mentions: mentions)
    }

    /// Shows the bubble and returns before anything is awaited; uploads keep running behind it.
    public func send(text: String, files: [LocalFile], reply: ReplyContext? = nil,
                     mentions: [String] = [], at tap: ContinuousClock.Instant = ContinuousClock().now) async {
        let text = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty || !files.isEmpty else { return }
        let id = UUID().uuidString
        let parts = (text.isEmpty ? [] : [Part(kind: "text", text: text)])
            + files.map { Part(kind: "attachment", attachment: $0.placeholder) }
        let message = Message(id: id, threadID: threadID, role: "user", parts: parts,
                              createdAt: WireDate.string(), status: "pending", seq: 0, reply: reply,
                              mentions: mentions.isEmpty ? nil : mentions)
        update(message)
        do {
            try await cacheActor.call { try $0.merge([message]) }
        } catch {
            messages.removeAll { $0.id == id }
            self.error = error.localizedDescription
            return
        }
        // The bubble and its pending record landed; the span marks that from the tap, with the
        // uploads still ahead. The ring starts on every file.
        InstantUI.interval("send.bubble", budget: InstantUI.feedbackBudget, trace: trace, from: tap) {
            onUpdate?(Event(seq: 0, conversationID: threadID, type: "message.start", message: message,
                            createdAt: message.createdAt), tap)
            for file in files { uploads[file.id] = 0 }
        }
        startUploads(message.id, files: files, reply: reply, text: text, mentions: message.mentions ?? [])
    }

    private func startUploads(_ messageID: String, files: [LocalFile], reply: ReplyContext?, text: String, mentions: [String]) {
        let uploader = self.uploader
        Task.detached(priority: .userInitiated) { [weak self] in
            guard let store = self else { return }
            do {
                try await uploader.run(files,
                    progress: { id, value in
                        Task { @MainActor in store.setUpload(id, value) }
                    },
                    finished: { id, attachment in
                        await store.attach(messageID: messageID, localID: id, with: attachment)
                    })
                await store.commit(messageID: messageID, files: files, reply: reply, text: text, mentions: mentions)
            } catch {
                await store.failUploads(messageID: messageID, files: files, error: error)
            }
        }
    }

    private func setUpload(_ localID: String, _ value: Double) {
        guard uploads[localID] != nil else { return }
        uploads[localID] = value
    }

    /// Swaps one local file ref for its server attachment in place, then drops the ring.
    private func attach(messageID: String, localID: String, with attachment: Attachment) {
        uploads[localID] = nil
        guard let index = messages.firstIndex(where: { $0.id == messageID }) else { return }
        var message = messages[index]
        for part in message.parts.indices where message.parts[part].attachment?.id == localID {
            message.parts[part].attachment = attachment
        }
        messages[index] = message
    }

    /// Uploads finished: queue the send with server attachment ids and let `flush` deliver it.
    private func commit(messageID: String, files: [LocalFile], reply: ReplyContext?, text: String, mentions: [String]) async {
        guard let message = messages.first(where: { $0.id == messageID }), message.attachments.count == files.count else { return }
        let attachments = message.attachments
        let threadID = self.threadID
        let request = SendRequest(id: messageID, text: text, attachmentIDs: attachments.map(\.id),
                                  replyToID: reply?.messageID, replyPart: reply?.partIndex,
                                  mentions: mentions.isEmpty ? nil : mentions)
        do {
            // The pending row parked local file refs; unlock it so the queue takes the server ids.
            try await cacheActor.call { try $0.setStatus(id: messageID, status: "error") }
            _ = try await cacheActor.call { try $0.enqueue(threadID: threadID, request: request, attachments: attachments, reply: reply) }
            if let stored = try await cacheActor.call({ try $0.messages(threadID: threadID) }).first(where: { $0.id == messageID }) {
                update(stored)
            }
            for file in files { uploads[file.id] = nil }
            await flush()
        } catch { self.error = error.localizedDescription }
    }

    /// SA2 owns unsent state and retry; this only stops the rings and reports why.
    private func failUploads(messageID: String, files: [LocalFile], error: any Error) async {
        for file in files { uploads[file.id] = nil }
        try? await cacheActor.call { try $0.setStatus(id: messageID, status: "failed") }
        if let index = messages.firstIndex(where: { $0.id == messageID }) {
            messages[index].status = "failed"
        }
        self.error = error.localizedDescription
    }

    public func retry(messageID: String) async {
        let threadID = self.threadID
        await ready()
        guard !flushing, let message = messages.first(where: { $0.id == messageID }), message.role == "user",
              ["pending", "failed", "error"].contains(message.status) else { return }
        if (try? await cacheActor.call({ try $0.accepted(id: messageID) })) == true { return }
        try? await cacheActor.call { try $0.setStatus(id: messageID, status: "pending") }
        if let message = try? await cacheActor.call({ try $0.messages(threadID: threadID) }).first(where: { $0.id == messageID }) { update(message) }
        if let pending = try? await cacheActor.call({ try $0.pending(threadID: threadID) }), pending.contains(where: { $0.id == messageID }) {
            await flush()
        } else if let message = messages.first(where: { $0.id == messageID }), message.role == "user" {
            await send(text: message.text, attachments: message.attachments, id: message.id, reply: message.reply, mentions: message.mentions ?? [])
        }
    }

    public func markRead() async {
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
            onUpdate?(Event(seq: seq, conversationID: threadID, type: "message.read", createdAt: WireDate.string()), tap)
            if isConnected { try await client.markRead(threadID: threadID, seq: seq) }
        } catch { self.error = error.localizedDescription }
    }

    public func loadEarlier() async {
        let threadID = self.threadID
        guard hasEarlier, let first = messages.filter({ $0.seq > 0 }).map(\.seq).min() else { return }
        do {
            var earlier = try await cacheActor.call { try $0.messages(threadID: threadID, before: first, limit: 100) }
            if earlier.count < 100 {
                let fetched = try await client.messages(threadID: threadID, before: first)
                try await cacheActor.call { try $0.merge(fetched) }
                earlier = try await cacheActor.call { try $0.messages(threadID: threadID, before: first, limit: 100) }
            }
            let history = try await client.toolRuns(threadID: threadID, before: first)
            for run in history {
                runs.removeAll { $0.id == run.id }
                runs.append(run)
            }
            activity.restore(runs)
            for message in earlier { update(message) }
            hasEarlier = earlier.count == 100
        } catch { self.error = error.localizedDescription }
    }

    private func send(text: String, attachments: [Attachment], id: String, reply: ReplyContext? = nil, mentions: [String] = []) async {
        let text = text.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !text.isEmpty || !attachments.isEmpty else { return }
        if threadID.hasPrefix("local-") {
            let parts = (text.isEmpty ? [] : [Part(kind: "text", text: text)])
                + attachments.map { Part(kind: "attachment", attachment: $0) }
            let message = Message(id: id, threadID: threadID, role: "user", parts: parts,
                                  createdAt: WireDate.string(), status: "pending", reply: reply,
                                  mentions: mentions.isEmpty ? nil : mentions)
            queued[id] = message
            update(message)
        }
        guard let resolved = await sidebar?.resolved(threadID) ?? (threadID.hasPrefix("local-") ? nil : threadID) else {
            if var message = queued[id] {
                message.status = "failed"
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
            onUpdate?(Event(seq: message.seq, conversationID: threadID, type: "message.start", message: message, createdAt: message.createdAt), ContinuousClock().now)
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
                for message in queued.values { update(message) }
                runs = try await cacheActor.call { try $0.runs(threadID: threadID) }
                let history = try await client.toolRuns(threadID: threadID)
                for run in history {
                    runs.removeAll { $0.id == run.id }
                    runs.append(run)
                }
                activity.restore(runs)
                isConnected = true
                error = nil
                delay = 1
                await flush()
                await markRead()
                cursor = try await cacheActor.call { try $0.cursor(threadID: threadID) }
                for try await event in client.stream(threadID: threadID, after: cursor) {
                    try Task.checkCancellation()
                    try await receive(event)
                    if event.type == "message.start" || event.type == "message.done" || event.type == "activity" {
                        await markRead()
                    }
                }
                throw ClientError.unavailable
            } catch {
                if Task.isCancelled { return }
                isConnected = false
                self.error = error.localizedDescription
            }
            try? await Task.sleep(for: .seconds(delay))
            delay = min(delay * 2, 15)
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
        let before = try await cacheActor.call { try $0.cursor(threadID: threadID) }
        guard event.seq > before else { return }
        let touchThreads = sidebar == nil
        if let message = try await cacheActor.call({ cache in
            let message = try cache.apply(event, touchThreads: touchThreads)
            return try message.flatMap { try cache.isHidden($0.id, threadID: threadID) ? nil : $0 }
        }) { update(message) }
        if event.type == "activity" {
            let hidden = Set(event.payload?.messageIDs ?? [])
            messages.removeAll { hidden.contains($0.id) }
        }
        activity.receive(event, live: live)
        if event.run != nil { runs = try await cacheActor.call({ try $0.runs(threadID: threadID) }) }
        if live { isConnected = true }
    }

    private func update(_ message: Message) {
        if let index = messages.firstIndex(where: { $0.id == message.id }) {
            messages[index] = message
        } else {
            messages.append(message)
            messages.sort {
                if $0.seq == 0 && $1.seq == 0 { return $0.createdAt < $1.createdAt }
                if $0.seq == 0 { return false }
                if $1.seq == 0 { return true }
                return $0.seq < $1.seq
            }
        }
    }

    private func flush() async {
        let threadID = self.threadID
        guard !flushing else { return }
        flushing = true
        defer { flushing = false }
        do {
            while let request = try await cacheActor.call({ try $0.pending(threadID: threadID) }).first {
                do {
                    try await cacheActor.call { try $0.setStatus(id: request.id, status: "pending") }
                    if let message = try await cacheActor.call({ try $0.messages(threadID: threadID) }).first(where: { $0.id == request.id }) { update(message) }
                    try await sidebar?.syncEdit(threadID)
                    try await cacheActor.deliver(client: client, threadID: threadID, request: request)
                    if let message = try await cacheActor.call({ try $0.messages(threadID: threadID) }).first(where: { $0.id == request.id }) { update(message) }
                    error = nil
                } catch {
                    if try await cacheActor.call({ try $0.accepted(id: request.id) }) {
                        try await cacheActor.call { try $0.acknowledge(id: request.id) }
                        continue
                    }
                    if case let ClientError.response(code, _) = error, (400..<500).contains(code), code != 408, code != 429 {
                        try await cacheActor.call { try $0.setStatus(id: request.id, status: "error") }
                        if let message = try await cacheActor.call({ try $0.messages(threadID: threadID) }).first(where: { $0.id == request.id }) { update(message) }
                        self.error = error.localizedDescription
                        continue
                    }
                    let pending = try await cacheActor.call { try $0.pending(threadID: threadID) }
                    for pending in pending {
                        try await cacheActor.call { try $0.setStatus(id: pending.id, status: "failed") }
                        if let message = try await cacheActor.call({ try $0.messages(threadID: threadID) }).first(where: { $0.id == pending.id }) { update(message) }
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
