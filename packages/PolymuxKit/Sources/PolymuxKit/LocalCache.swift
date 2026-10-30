import CSQLite
import Dispatch
import Foundation

private final class Connection: @unchecked Sendable {
    let db: OpaquePointer

    init(url: URL) throws {
        var pointer: OpaquePointer?
        guard sqlite3_open_v2(url.path, &pointer, SQLITE_OPEN_READWRITE | SQLITE_OPEN_CREATE | SQLITE_OPEN_FULLMUTEX, nil) == SQLITE_OK,
              let pointer else {
            if let pointer { sqlite3_close(pointer) }
            throw ClientError.cache("Could not open the database.")
        }
        db = pointer
    }

    deinit { sqlite3_close(db) }
}

public final class LocalCache: @unchecked Sendable {
    /// The folder holding this cache, which also keeps copies of files waiting to upload.
    public let folder: URL
    private let connection: Connection
    private var db: OpaquePointer { connection.db }
    private let encoder = JSONEncoder()
    private let decoder = JSONDecoder()
    private let lock = NSRecursiveLock()
    private var reads = 0
    /// Counts `threads()` calls, including the one inside `save`.
    public var threadReads: Int { lock.withLock { reads } }

    public init(url: URL) throws {
        folder = url.deletingLastPathComponent()
        try FileManager.default.createDirectory(at: url.deletingLastPathComponent(), withIntermediateDirectories: true)
        connection = try Connection(url: url)
        try execute("PRAGMA journal_mode=WAL")
        try execute("PRAGMA synchronous=NORMAL")
        try execute("CREATE TABLE IF NOT EXISTS metadata (key TEXT PRIMARY KEY, json TEXT NOT NULL)")
        try execute("CREATE TABLE IF NOT EXISTS messages (id TEXT PRIMARY KEY, thread_id TEXT NOT NULL, seq INTEGER NOT NULL, created TEXT NOT NULL, json TEXT NOT NULL)")
        try execute("CREATE INDEX IF NOT EXISTS message_order ON messages(thread_id, created, seq)")
        try execute("CREATE TABLE IF NOT EXISTS cursors (thread_id TEXT PRIMARY KEY, seq INTEGER NOT NULL)")
        try execute("CREATE TABLE IF NOT EXISTS events (thread_id TEXT NOT NULL, seq INTEGER NOT NULL, json TEXT NOT NULL, PRIMARY KEY(thread_id, seq))")
        try execute("CREATE TABLE IF NOT EXISTS outbox (id TEXT PRIMARY KEY, thread_id TEXT NOT NULL, created TEXT NOT NULL, json TEXT NOT NULL)")
        try execute("CREATE VIRTUAL TABLE IF NOT EXISTS messages_fts USING fts5(id UNINDEXED, thread_id UNINDEXED, text)")
        try execute("CREATE TABLE IF NOT EXISTS runs (id TEXT PRIMARY KEY, thread_id TEXT NOT NULL, seq INTEGER NOT NULL, json TEXT NOT NULL)")
        // Events are no longer kept; only `migrateSequences` reads what older versions saved.
        try execute("DROP INDEX IF EXISTS activity_order")
        try execute("DROP INDEX IF EXISTS recent_events")
        try migrateGroups()
        try migrateActivity()
        try migrateSequences()
    }

    private func migrateGroups() throws {
        let version: Int = try metadata("groupVersion") ?? 0
        guard version < 1 else { return }
        try transaction {
            if let raw = try rows("SELECT json FROM metadata WHERE key='threads'").first?.first,
               let data = raw.data(using: .utf8),
               var threads = try JSONSerialization.jsonObject(with: data) as? [[String: Any]] {
                let agents = try agents()
                for index in threads.indices {
                    let id = threads[index]["agentID"] as? String ?? ""
                    if threads[index]["title"] == nil { threads[index]["title"] = agents.first { $0.id == id }?.name ?? "" }
                    if threads[index]["agentIDs"] == nil { threads[index]["agentIDs"] = [id] }
                }
                let updated = try JSONSerialization.data(withJSONObject: threads)
                try execute("UPDATE metadata SET json=? WHERE key='threads'", [.text(String(decoding: updated, as: UTF8.self))])
            }
            try setMetadata("groupVersion", value: 1)
        }
    }

    private func migrateSequences() throws {
        let version: Int = try metadata("cacheVersion") ?? 0
        guard version < 1 else { return }
        try transaction {
            let messages = try rows("SELECT json FROM messages WHERE seq>0").map { try decode(Message.self, $0[0]) }
            var pending = Set(messages.map(\.id))
            var threads = try storedThreads()
            let starts = try rows("SELECT json FROM events WHERE json_extract(json,'$.type')='message.start' ORDER BY seq LIMIT 10000")
            for row in starts {
                let event = try decode(Event.self, row[0])
                guard let id = event.messageID ?? event.message?.id,
                      var message = try message(id: id), message.seq > 0 else { continue }
                message.seq = event.seq
                try write(message)
                pending.remove(id)
                if let index = threads.firstIndex(where: { $0.lastMessage?.id == id }) {
                    threads[index].lastMessage?.seq = event.seq
                }
            }
            try setMetadata("threads", value: threads)
            try setMetadata("seqRepair", value: Array(pending))
            try setMetadata("cacheVersion", value: 1)
        }
    }

    private func migrateActivity() throws {
        let version: Int = try metadata("activityVersion") ?? 0
        guard version < 1 else { return }
        try transaction {
            for key in ["threads", "threadEdits"] {
                guard let raw = try rows("SELECT json FROM metadata WHERE key=?", [.text(key)]).first?.first,
                      let data = raw.data(using: .utf8),
                      var threads = try JSONSerialization.jsonObject(with: data) as? [[String: Any]] else { continue }
                for index in threads.indices where threads[index]["runs"] == nil { threads[index]["runs"] = [] as [Any] }
                let updated = try JSONSerialization.data(withJSONObject: threads)
                try execute("UPDATE metadata SET json=? WHERE key=?", [.text(String(decoding: updated, as: UTF8.self)), .text(key)])
            }
            try setMetadata("activityVersion", value: 1)
        }
    }

    func repairIDs(threadID: String) throws -> Set<String> {
        let pending: [String] = try metadata("seqRepair") ?? []
        let ids = Set(try rows("SELECT id FROM messages WHERE thread_id=?", [.text(threadID)]).map { $0[0] })
        return Set(pending).intersection(ids)
    }

    func repairSequences(_ snapshots: [Message]) throws {
        var pending: Set<String> = Set(try metadata("seqRepair") as [String]? ?? [])
        guard !pending.isEmpty else { return }
        try transaction {
            var threads = try storedThreads()
            for snapshot in snapshots where pending.contains(snapshot.id) && snapshot.seq > 0 {
                guard var message = try message(id: snapshot.id) else { continue }
                message.seq = snapshot.seq
                try write(message)
                pending.remove(snapshot.id)
                if let index = threads.firstIndex(where: { $0.lastMessage?.id == snapshot.id }) {
                    threads[index].lastMessage?.seq = snapshot.seq
                }
            }
            try setMetadata("threads", value: threads)
            try setMetadata("seqRepair", value: Array(pending))
        }
    }

    func finishRepair(threadID: String) throws {
        let pending: Set<String> = Set(try metadata("seqRepair") as [String]? ?? [])
        let unresolved = try repairIDs(threadID: threadID)
        try setMetadata("seqRepair", value: Array(pending.subtracting(unresolved)))
    }

    /// Non-secret server state kept so screens open offline, such as settings, providers and profiles.
    public func snapshot<T: Decodable>(_ key: String) throws -> T? { try metadata("snapshot/" + key) }
    public func saveSnapshot<T: Encodable>(_ key: String, _ value: T) throws { try setMetadata("snapshot/" + key, value: value) }

    public func agents() throws -> [Agent] { try metadata("agents") ?? [] }
    public func sidebar() throws -> SidebarSnapshot? { try metadata("sidebar") }
    public func saveSidebar(_ snapshot: SidebarSnapshot) throws { try setMetadata("sidebar", value: snapshot) }
    public func threads() throws -> [Thread] {
        lock.withLock { reads += 1 }
        var threads = try storedThreads()
        let runs = try latestRuns(threadID: nil)
        for index in threads.indices { threads[index].runs = runs[threads[index].id] ?? [] }
        return threads
    }

    /// The saved rows without their runs, for changes that write the rows back.
    private func storedThreads() throws -> [Thread] { try metadata("threads") ?? [] }

    public func runs(threadID: String) throws -> [Run] {
        try latestRuns(threadID: threadID)[threadID] ?? []
    }

    /// Active runs and each agent's latest run, per chat: one chat, or all when `threadID` is nil.
    private func latestRuns(threadID: String?) throws -> [String: [Run]] {
        let query = """
        SELECT thread_id,json FROM (SELECT thread_id,json,seq,json_extract(json,'$.status') status,
        row_number() OVER(PARTITION BY thread_id,json_extract(json,'$.agentID') ORDER BY
        julianday(json_extract(json,'$.createdAt')) DESC,json_extract(json,'$.createdAt') DESC,seq DESC) latest
        FROM runs\(threadID == nil ? "" : " WHERE thread_id=?"))
        WHERE latest=1 OR status IN('queued','running','resuming','waiting_input','waiting_peer')
        ORDER BY CASE status WHEN 'running' THEN 0 WHEN 'resuming' THEN 0 WHEN 'waiting_input' THEN 0
        WHEN 'waiting_peer' THEN 0 WHEN 'queued' THEN 1 ELSE 2 END,seq DESC
        """
        var out: [String: [Run]] = [:]
        for row in try rows(query, threadID.map { [.text($0)] } ?? []) {
            out[row[0], default: []].append(try decode(Run.self, row[1]))
        }
        return out
    }

    private func write(_ run: Run) throws {
        try execute("INSERT INTO runs(id,thread_id,seq,json) VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET seq=excluded.seq,json=excluded.json WHERE excluded.seq>=runs.seq", [.text(run.id), .text(run.conversationID), .int(run.seq), .text(try encode(run))])
    }

    public func reconcile(_ thread: Thread) throws {
        let runs = thread.runs
        try transaction {
            for run in runs { try write(run) }
            let ids = try encode(runs.map(\.id))
            try execute("DELETE FROM runs WHERE thread_id=? AND seq<=? AND id NOT IN(SELECT value FROM json_each(?))",
                        [.text(thread.id), .int(thread.lastSeq), .text(ids)])
        }
    }
    public func threadEdits() throws -> [Thread] { try metadata("threadEdits") ?? [] }
    public func saveEdits(_ threads: [Thread]) throws { try setMetadata("threadEdits", value: threads) }

    public func save(agents: [Agent], threads: [Thread]) throws {
        try transaction {
            let saved = try rows("SELECT json_extract(value,'$.id') FROM json_each((SELECT json FROM metadata WHERE key='threads'))")
            let deleted = Set(saved.map { $0[0] }).subtracting(threads.map(\.id))
            try prune(deleted)
            for thread in threads { for run in thread.runs { try write(run) } }
            try setMetadata("agents", value: agents)
            try setMetadata("threads", value: threads)
        }
    }

    private func prune(_ threads: Set<String>) throws {
        guard !threads.isEmpty else { return }
        var removed = Set<String>()
        for thread in threads {
            removed.formUnion(try rows("SELECT id FROM messages WHERE thread_id=?", [.text(thread)]).map { $0[0] })
            for table in ["messages_fts", "outbox", "events", "cursors", "messages", "runs"] {
                try execute("DELETE FROM \(table) WHERE thread_id=?", [.text(thread)])
            }
        }
        let pending: [String] = try metadata("seqRepair") ?? []
        if pending.contains(where: { removed.contains($0) }) {
            try setMetadata("seqRepair", value: pending.filter { !removed.contains($0) })
        }
    }

    public func cursor(threadID: String) throws -> Int64 {
        let value = try rows("SELECT seq FROM cursors WHERE thread_id=?", [.text(threadID)]).first?.first ?? "0"
        return Int64(value) ?? 0
    }

    public func messages(threadID: String, before: Int64? = nil, limit: Int? = nil) throws -> [Message] {
        var sql = "SELECT json FROM messages WHERE thread_id=? AND json_extract(json,'$.status')!='silent'"
        var values: [Value] = [.text(threadID)]
        if let before {
            sql += " AND seq>0 AND seq<?"
            values.append(.int(before))
        }
        sql += " ORDER BY CASE WHEN seq=0 THEN 1 ELSE 0 END DESC, seq DESC, created DESC"
        if let limit {
            sql += " LIMIT ?"
            values.append(.int(Int64(max(1, limit))))
        }
        return try rows(sql, values).reversed().map { try decode(Message.self, $0[0]) }
    }

    /// Messages still waiting on this device, oldest first.
    func unsent(threadID: String) throws -> [Message] {
        try rows("SELECT json FROM messages WHERE thread_id=? AND seq=0 ORDER BY created", [.text(threadID)]).map { try decode(Message.self, $0[0]) }
    }

    public func merge(_ messages: [Message]) throws {
        try transaction {
            for message in messages where try self.message(id: message.id) == nil { try write(message) }
        }
    }

    func setReactions(id: String, previous: [Reaction]?, reactions: [Reaction]?) throws -> Message? {
        guard var message = try message(id: id) else { return nil }
        guard message.reactions == previous else { return message }
        message.reactions = reactions
        try execute("UPDATE messages SET json=? WHERE id=?", [.text(try encode(message)), .text(id)])
        return message
    }

    @discardableResult
    public func apply(_ event: Event, touchThreads: Bool = true) throws -> Message? {
        let cursor = try cursor(threadID: event.conversationID)
        guard event.seq > cursor else { return nil }
        guard event.seq == cursor + 1 else { throw ClientError.gap(expected: cursor + 1, actual: event.seq) }
        var changed: Message?
        try transaction {
            if let run = event.run, run.conversationID == event.conversationID { try write(run) }
            let stored = try event.messageID.flatMap { try message(id: $0) }
            let wasSilent = stored?.status == MessageStatus.silent
            if let full = event.message {
                var message = full
                if message.seq == 0 { message.seq = event.seq }
                try write(message)
                changed = message
            } else if var message = stored {
                switch event.type {
                case EventType.messageDelta:
                    let text = event.text ?? ""
                    if let index = message.parts.firstIndex(where: { $0.kind == PartKind.text }) {
                        message.parts[index].text = (message.parts[index].text ?? "") + text
                    } else { message.parts.append(Part(kind: PartKind.text, text: text)) }
                    message.status = MessageStatus.streaming
                case EventType.messageAttachment:
                    if let attachment = event.attachment,
                       !message.attachments.contains(where: { $0.id == attachment.id }) {
                        message.parts.append(Part(kind: PartKind.attachment, attachment: attachment))
                    }
                case EventType.messageDone: message.status = MessageStatus.done
                case EventType.messageError: message.status = MessageStatus.error
                default: break
                }
                try write(message)
                changed = message
            }
            if let message = changed, message.role == MessageRole.user {
                try execute("DELETE FROM outbox WHERE id=?", [.text(message.id)])
            }
            if event.type == EventType.agentPresence, let agentID = event.agentID, let active = event.active {
                var agents = try agents()
                if let index = agents.firstIndex(where: { $0.id == agentID }) {
                    agents[index].active = active
                    try setMetadata("agents", value: agents)
                }
            }
            if touchThreads {
                var threads = try storedThreads()
                if let index = threads.firstIndex(where: { $0.id == event.conversationID }) {
                    let isNew = event.seq > threads[index].lastSeq
                    threads[index].lastSeq = max(threads[index].lastSeq, event.seq)
                    if let message = changed, message.status == MessageStatus.silent {
                        if threads[index].lastMessage?.id == message.id {
                            threads[index].lastMessage = try messages(threadID: event.conversationID).last
                        }
                        if isNew, event.seq > threads[index].lastReadSeq, !wasSilent {
                            threads[index].unreadCount = max(0, threads[index].unreadCount - 1)
                        }
                    } else if let message = changed,
                              message.seq >= (threads[index].lastMessage?.seq ?? 0) { threads[index].lastMessage = message }
                    if isNew, event.seq > threads[index].lastReadSeq {
                        if event.type == EventType.messageStart, let changed, changed.role == MessageRole.assistant,
                           changed.status != MessageStatus.silent {
                            threads[index].unreadCount += 1
                        }
                        if event.type == EventType.activity, event.payload?.countsUnread == true {
                            threads[index].unreadCount += 1
                        }
                    }
                    try setMetadata("threads", value: threads)
                }
            }
            try execute("INSERT INTO cursors(thread_id,seq) VALUES(?,?) ON CONFLICT(thread_id) DO UPDATE SET seq=excluded.seq", [.text(event.conversationID), .int(event.seq)])
        }
        return changed
    }

    public func enqueue(threadID: String, request: SendRequest, attachments: [Attachment], reply: ReplyContext? = nil) throws -> Message {
        if let existing = try message(id: request.id), existing.status != MessageStatus.error { return existing }
        let parts = (request.text.isEmpty ? [] : [Part(kind: PartKind.text, text: request.text)])
            + attachments.map { Part(kind: PartKind.attachment, attachment: $0) }
        let message = Message(id: request.id, threadID: threadID, role: MessageRole.user, parts: parts, createdAt: WireDate.string(), status: MessageStatus.pending, seq: 0, reply: reply, mentions: request.mentions)
        try transaction {
            try execute("INSERT OR IGNORE INTO outbox(id,thread_id,created,json) VALUES(?,?,?,?)", [.text(request.id), .text(threadID), .text(message.createdAt), .text(try encode(request))])
            try write(message)
        }
        return message
    }

    public func pending(threadID: String) throws -> [SendRequest] {
        try rows("SELECT o.json FROM outbox o JOIN messages m ON m.id=o.id WHERE o.thread_id=? AND json_extract(m.json,'$.status')!='error' ORDER BY o.created", [.text(threadID)]).map { try decode(SendRequest.self, $0[0]) }
    }

    public func queued() throws -> [(threadID: String, request: SendRequest)] {
        try rows("SELECT o.thread_id,o.json FROM outbox o JOIN messages m ON m.id=o.id WHERE json_extract(m.json,'$.status')!='error' ORDER BY o.created").map {
            (threadID: $0[0], request: try decode(SendRequest.self, $0[1]))
        }
    }

    public func accepted(id: String) throws -> Bool {
        guard let message = try message(id: id) else { return false }
        return message.seq > 0 && message.role == MessageRole.user
    }

    /// Drops a message that never reached the server, so it is not sent later.
    public func discard(id: String) throws {
        try transaction {
            try execute("DELETE FROM outbox WHERE id=?", [.text(id)])
            try execute("DELETE FROM messages WHERE id=? AND seq=0", [.text(id)])
            try execute("DELETE FROM messages_fts WHERE id=?", [.text(id)])
        }
    }

    public func acknowledge(id: String) throws {
        try transaction {
            try execute("DELETE FROM outbox WHERE id=?", [.text(id)])
            if var message = try message(id: id), message.seq == 0 {
                message.status = MessageStatus.done
                try write(message)
            }
        }
    }

    /// Changes the current row, never a stale view's copy or a deleted/accepted message.
    func updateUnsent(id: String, _ update: (inout Message) -> Void) throws -> Message? {
        var result: Message?
        try transaction {
            guard var message = try message(id: id), message.seq == 0, message.status != MessageStatus.done else { return }
            update(&message)
            try write(message)
            result = message
        }
        return result
    }

    func pendingUpload(id: String) throws -> Message? {
        guard let message = try message(id: id), message.seq == 0, message.role == MessageRole.user,
              [MessageStatus.pending, MessageStatus.failed].contains(message.status),
              try !queued(id: id) else { return nil }
        return message
    }

    /// Atomically queues a finished upload, including one interrupted just before enqueueing.
    func enqueueUpload(id: String) throws -> Message? {
        var result: Message?
        try transaction {
            guard var message = try message(id: id), message.seq == 0, message.role == MessageRole.user,
                  [MessageStatus.pending, MessageStatus.failed].contains(message.status),
                  !message.attachments.contains(where: { $0.localURL != nil }) else { return }
            let request = SendRequest(id: id, text: message.text, attachmentIDs: message.attachments.map(\.id),
                                      replyToID: message.reply?.messageID, replyPart: message.reply?.partIndex,
                                      mentions: message.mentions)
            try execute("INSERT OR IGNORE INTO outbox(id,thread_id,created,json) VALUES(?,?,?,?)",
                        [.text(id), .text(message.threadID), .text(message.createdAt), .text(try encode(request))])
            message.status = MessageStatus.pending
            try write(message)
            result = message
        }
        return result
    }

    /// Whether a message is still waiting on this device, not yet accepted by the server.
    public func unsent(id: String) throws -> Bool {
        try message(id: id)?.seq == 0
    }

    /// Whether a send is already queued, so its files finished uploading.
    public func queued(id: String) throws -> Bool {
        !(try rows("SELECT 1 FROM outbox WHERE id=?", [.text(id)])).isEmpty
    }

    public func setStatus(id: String, status: String) throws {
        guard var message = try message(id: id) else { return }
        message.status = status
        try transaction { try write(message) }
    }

    public func markRead(threadID: String, seq: Int64) throws {
        var threads = try storedThreads()
        guard let index = threads.firstIndex(where: { $0.id == threadID }) else { return }
        threads[index].lastReadSeq = max(threads[index].lastReadSeq, seq)
        threads[index].unreadCount = 0
        try setMetadata("threads", value: threads)
    }

    public func search(threadID: String? = nil, query: String, kind: String = "all") throws -> [SearchHit] {
        var tokens = query.split(whereSeparator: { $0.isWhitespace }).map { "\"" + $0.replacingOccurrences(of: "\"", with: "\"\"") + "\"" }
        guard !tokens.isEmpty else { return [] }
        tokens[tokens.count - 1] += "*"
        var predicate = "(?='' OR messages_fts.thread_id=?)"
        if kind == "media" || kind == "files" {
            let media = "substr(json_extract(p.value,'$.attachment.mimeType'),1,6) IN ('image/','video/','audio/')"
            predicate += " AND EXISTS(SELECT 1 FROM json_each(m.json,'$.parts') p WHERE json_type(p.value,'$.attachment')='object' AND \(kind == "files" ? "NOT (" + media + ")" : media))"
        }
        let scope = Value.text(threadID ?? "")
        let matches = try rows("SELECT m.json, snippet(messages_fts,2,'','','…',24), bm25(messages_fts) FROM messages_fts JOIN messages m ON m.id=messages_fts.id WHERE messages_fts MATCH ? AND " + predicate + " ORDER BY bm25(messages_fts) LIMIT 50", [.text(tokens.joined(separator: " OR ")), scope, scope])
        return try matches.map { SearchHit(message: try decode(Message.self, $0[0]), score: -(Double($0[2]) ?? 0), snippet: $0[1], indexingState: SearchHitIndexingState.ready) }
    }

    public func cacheSearch(_ results: [SearchResult], query: String, kind: String, scope: String? = nil) throws {
        try merge(results.compactMap(\.message))
        try setMetadata(Self.searchKey(query: query, kind: kind, scope: scope), value: results)
    }

    private static func searchKey(query: String, kind: String, scope: String?) -> String {
        scope.map { "search/\(kind)@\($0)/\(query)" } ?? "search/\(kind)/\(query)"
    }

    /// Searches downloaded content; `scope` limits it to one chat's messages and attachments.
    public func globalSearch(query: String, kind: String, scope: String? = nil) throws -> [SearchResult] {
        let threads = try threads()
        let agents = try agents()
        let live = Set(threads.map(\.id)).filter { scope == nil || $0 == scope }
        let cached: [SearchResult] = try metadata(Self.searchKey(query: query, kind: kind, scope: scope)) ?? []
        var results = cached.filter { live.contains($0.threadID) }
        func distinct() -> [SearchResult] {
            var ids = Set<String>()
            return results.filter { ids.insert($0.id).inserted }
        }
        // A chat-scoped search lists no chats or bots, only what was said and shared.
        for thread in threads where scope == nil {
            if (kind == "conversations" || kind == "all" && thread.id != thread.agentID) && (query.isEmpty || thread.title.localizedStandardContains(query)) {
                results.append(SearchResult(id: "conversation/" + thread.id, kind: "conversations", title: thread.title,
                                            threadID: thread.id, agentID: thread.agentID))
            }
            if thread.id == thread.agentID, let agent = agents.first(where: { $0.id == thread.agentID }),
               (kind == "all" || kind == "bots"),
               query.isEmpty || (agent.name + " " + agent.role).localizedStandardContains(query) {
                results.append(SearchResult(id: "bot/" + agent.id, kind: "bots", title: agent.name, snippet: agent.role,
                                            threadID: thread.id, agentID: agent.id))
            }
        }
        guard !query.isEmpty, kind != "bots", kind != "conversations" else { return distinct() }
        var seen = Set<String>()
        for hit in try search(threadID: scope, query: query, kind: kind) {
            let message = hit.message
            guard live.contains(message.threadID) else { continue }
            let title = threads.first { $0.id == message.threadID }?.title ?? "Conversation"
            if kind == "all" || kind == "messages" {
                results.append(SearchResult(id: "message/" + message.id, kind: "messages", title: title,
                                            snippet: hit.snippet, threadID: message.threadID, messageID: message.id))
            }
            for attachment in message.attachments {
                let media = ["image/", "video/", "audio/"].contains { attachment.mimeType.hasPrefix($0) }
                let category = media ? "media" : "files"
                guard (kind == "all" || kind == category), seen.insert(attachment.id).inserted else { continue }
                results.append(SearchResult(id: "attachment/" + attachment.id, kind: category, title: attachment.name,
                                            snippet: hit.snippet, threadID: message.threadID, messageID: message.id,
                                            attachment: attachment))
            }
        }
        return distinct()
    }

    func message(id: String) throws -> Message? {
        guard let value = try rows("SELECT json FROM messages WHERE id=?", [.text(id)]).first?.first else { return nil }
        return try decode(Message.self, value)
    }

    private func write(_ message: Message) throws {
        try execute("INSERT INTO messages(id,thread_id,seq,created,json) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET seq=excluded.seq,created=excluded.created,json=excluded.json", [.text(message.id), .text(message.threadID), .int(message.seq), .text(message.createdAt), .text(try encode(message))])
        // Search indexes the finished text, not every streamed delta.
        guard message.status != MessageStatus.streaming else { return }
        try execute("DELETE FROM messages_fts WHERE id=?", [.text(message.id)])
        let text = message.text + " " + message.attachments.map { $0.name + " " + ($0.extractedText ?? "") }.joined(separator: " ")
        try execute("INSERT INTO messages_fts(id,thread_id,text) VALUES(?,?,?)", [.text(message.id), .text(message.threadID), .text(text)])
    }

    private func metadata<T: Decodable>(_ key: String) throws -> T? {
        guard let value = try rows("SELECT json FROM metadata WHERE key=?", [.text(key)]).first?.first else { return nil }
        return try decode(T.self, value)
    }

    private func setMetadata<T: Encodable>(_ key: String, value: T) throws {
        try execute("INSERT INTO metadata(key,json) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET json=excluded.json", [.text(key), .text(try encode(value))])
    }

    private func encode<T: Encodable>(_ value: T) throws -> String {
        String(decoding: try encoder.encode(value), as: UTF8.self)
    }

    private func decode<T: Decodable>(_ type: T.Type, _ value: String) throws -> T {
        try decoder.decode(type, from: Data(value.utf8))
    }

    private func transaction(_ body: () throws -> Void) throws {
        // Held across the whole transaction so a main-thread reader cannot slip between statements.
        lock.lock()
        defer { lock.unlock() }
        try executeUnlocked("BEGIN IMMEDIATE")
        do { try body(); try executeUnlocked("COMMIT") }
        catch { try? executeUnlocked("ROLLBACK"); throw error }
    }

    private enum Value {
        case text(String)
        case int(Int64)
    }

    private func execute(_ sql: String, _ values: [Value] = []) throws {
        _ = try rows(sql, values)
    }

    private func executeUnlocked(_ sql: String, _ values: [Value] = []) throws {
        _ = try query(sql, values)
    }

    private func rows(_ sql: String, _ values: [Value] = []) throws -> [[String]] {
        lock.lock()
        defer { lock.unlock() }
        return try query(sql, values)
    }

    private func query(_ sql: String, _ values: [Value] = []) throws -> [[String]] {
        #if DEBUG
        // Every SQLite call enters here. Debug builds, including tests, trap on the main queue.
        dispatchPrecondition(condition: .notOnQueue(.main))
        #endif
        var statement: OpaquePointer?
        guard sqlite3_prepare_v2(db, sql, -1, &statement, nil) == SQLITE_OK, let statement else {
            throw ClientError.cache(String(cString: sqlite3_errmsg(db)))
        }
        defer { sqlite3_finalize(statement) }
        for (offset, value) in values.enumerated() {
            let index = Int32(offset + 1)
            let code: Int32
            switch value {
            case let .text(text): code = sqlite3_bind_text(statement, index, text, -1, unsafeBitCast(-1, to: sqlite3_destructor_type.self))
            case let .int(number): code = sqlite3_bind_int64(statement, index, number)
            }
            guard code == SQLITE_OK else { throw ClientError.cache(String(cString: sqlite3_errmsg(db))) }
        }
        var result: [[String]] = []
        while true {
            let code = sqlite3_step(statement)
            if code == SQLITE_DONE { break }
            guard code == SQLITE_ROW else { throw ClientError.cache(String(cString: sqlite3_errmsg(db))) }
            var row: [String] = []
            for index in 0..<sqlite3_column_count(statement) {
                row.append(sqlite3_column_text(statement, index).map { String(cString: $0) } ?? "")
            }
            result.append(row)
        }
        return result
    }
}
