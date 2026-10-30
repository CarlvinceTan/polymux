import Foundation
import Observation

public struct ChatDraft: Codable, Equatable, Sendable {
    public struct Mention: Codable, Equatable, Sendable {
        public var id: String
        public var text: String
        public var location: Int
        public var length: Int

        public init(id: String, text: String, location: Int, length: Int) {
            self.id = id; self.text = text; self.location = location; self.length = length
        }
    }

    public var text: String
    public var mentions: [Mention]
    public var reply: ReplyContext?

    public init(text: String = "", mentions: [Mention] = [], reply: ReplyContext? = nil) {
        self.text = text; self.mentions = mentions; self.reply = reply
    }
}

public struct DraftState: Codable, Equatable, Sendable {
    public var revision: Int64 = 0
    public var writeID = ""
    public var value = ChatDraft()

    public init(revision: Int64 = 0, writeID: String = "", value: ChatDraft = ChatDraft()) {
        self.revision = revision; self.writeID = writeID; self.value = value
    }
}

public struct DraftConflict: Error, Sendable {
    public let current: DraftState
}

@MainActor @Observable
public final class DraftStore {
    public private(set) var value = ChatDraft()
    public private(set) var issue: String?
    @ObservationIgnored private let client: APIClient
    @ObservationIgnored private let cache: LocalCacheActor
    @ObservationIgnored private let threadID: String
    @ObservationIgnored private var revision: Int64 = 0
    @ObservationIgnored private var writeID = ""
    @ObservationIgnored private var dirty = false
    @ObservationIgnored private var loaded = false
    @ObservationIgnored private var edited = false
    @ObservationIgnored private var saving = false
    @ObservationIgnored private var blocked = false
    @ObservationIgnored private var pending: DraftState?
    @ObservationIgnored private var lastEdit: ContinuousClock.Instant?
    @ObservationIgnored private var debounce: Task<Void, Never>?
    @ObservationIgnored private var loading: Task<Saved?, Never>?
    private var key: String { "draft-sync/" + threadID }

    private struct Saved: Codable, Sendable {
        var state: DraftState
        var dirty: Bool
        var blocked: Bool?
        var pending: DraftState?
    }

    public init(client: APIClient, cache: LocalCacheActor, threadID: String) {
        self.client = client; self.cache = cache; self.threadID = threadID
    }

    public func restore() async {
        guard !loaded else { return }
        if loading == nil {
            let cache = cache, key = key, threadID = threadID
            loading = Task {
                if let saved: Saved = await cache.snapshot(key) { return saved }
                var draft: ChatDraft = await cache.snapshot("draft/" + threadID) ?? ChatDraft()
                if draft.reply == nil { draft.reply = await cache.snapshot("reply/" + threadID) }
                let dirty = !draft.text.isEmpty || draft.reply != nil
                return Saved(state: DraftState(writeID: dirty ? UUID().uuidString : "", value: draft), dirty: dirty)
            }
        }
        let saved = await loading?.value
        guard !loaded else { return }
        if let saved {
            revision = saved.state.revision
            pending = saved.pending
            if !edited {
                value = saved.state.value; writeID = saved.state.writeID; dirty = saved.dirty
                blocked = saved.blocked ?? false
                if blocked { issue = "Draft changed on another device. Your text is kept here." }
            }
        }
        loaded = true
        loading = nil
    }

    /// The editor changes locally; neither disk nor the network is on the typing path.
    public func edit(_ next: ChatDraft, after delay: Duration = .milliseconds(400)) {
        guard next != value else { return }
        value = next
        edited = true
        dirty = true
        blocked = false
        issue = nil
        writeID = UUID().uuidString
        lastEdit = .now
        schedule(after: delay)
    }

    public func watch() async {
        await restore()
        while !Task.isCancelled {
            await refresh()
            if debounce == nil { await save() }
            do { try await Task.sleep(for: .seconds(1)) }
            catch { return }
        }
    }

    public func flush() {
        guard dirty else { return }
        schedule(after: .zero)
    }

    public func refresh() async {
        guard !dirty else { return }
        do {
            let fresh = try await client.draft(threadID)
            guard !Task.isCancelled, !dirty, fresh.revision >= revision else { return }
            if let lastEdit, lastEdit.duration(to: .now) < .seconds(2) { return }
            guard fresh != DraftState(revision: revision, writeID: writeID, value: value) else { return }
            revision = fresh.revision
            writeID = fresh.writeID
            if value != fresh.value { value = fresh.value }
            issue = nil
            await persist()
        } catch { }
    }

    private func schedule(after delay: Duration) {
        debounce?.cancel()
        debounce = Task {
            do { if delay > .zero { try await Task.sleep(for: delay) } }
            catch { return }
            debounce = nil
            await restore()
            await persist()
            await save()
        }
    }

    private func save() async {
        guard loaded, dirty, !blocked, !saving, !Task.isCancelled else { return }
        saving = true
        let outgoing = pending ?? DraftState(revision: revision, writeID: writeID, value: value)
        pending = outgoing
        await persist()
        defer { saving = false }
        do {
            let accepted = try await client.saveDraft(threadID, outgoing)
            revision = accepted.revision
            pending = nil
            if writeID == outgoing.writeID {
                dirty = false
                issue = nil
            }
            await persist()
            if dirty, debounce == nil { schedule(after: .milliseconds(400)) }
        } catch let conflict as DraftConflict {
            revision = conflict.current.revision
            pending = nil
            blocked = true
            issue = "Draft changed on another device. Your text is kept here."
            await persist()
        } catch { }
    }

    private func persist() async {
        let state = DraftState(revision: revision, writeID: writeID, value: value)
        await cache.saveSnapshot(key, Saved(state: state, dirty: dirty, blocked: blocked, pending: pending))
    }
}
