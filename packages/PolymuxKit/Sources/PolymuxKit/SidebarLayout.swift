import Foundation
import Observation

@MainActor @Observable
public final class SidebarLayout {
    nonisolated public static let unassignedID = "__unassigned__"
    public private(set) var snapshot = SidebarSnapshot()
    public private(set) var isSaving = false
    public private(set) var issue: String?
    @ObservationIgnored private let client: APIClient
    @ObservationIgnored private let cache: LocalCacheActor
    @ObservationIgnored private var revision = 0
    /// Saves in flight, including ones that don't lock the sidebar.
    @ObservationIgnored private var pending = 0
    @ObservationIgnored private var retryAction: (() async -> Void)?

    public init(client: APIClient, cache: LocalCacheActor) {
        self.client = client
        self.cache = cache
    }

    public var sections: [Section] {
        snapshot.sections.sorted { $0.order == $1.order ? $0.id < $1.id : $0.order < $1.order }
    }

    public var customSections: [Section] { sections.filter { $0.id != Self.unassignedID } }

    public func visibleSections(from threads: [Thread]) -> [Section] {
        sections.filter { $0.id != Self.unassignedID || !chats(in: nil, from: threads).isEmpty }
    }

    public func chats(in sectionID: String?, from threads: [Thread]) -> [Thread] {
        let ids = Set(customSections.map(\.id))
        let target = sectionID == Self.unassignedID ? nil : sectionID
        return threads.filter {
            $0.pinned != true && ($0.sectionID == target || (target == nil && !ids.contains($0.sectionID ?? "")))
        }
    }

    public func applying(to thread: Thread) -> Thread {
        guard let placement = snapshot.placements.first(where: { $0.threadID == thread.id }) else { return thread }
        var thread = thread
        thread.pinned = placement.pinned
        thread.pinOrder = placement.pinOrder
        thread.sectionID = placement.sectionID
        thread.sectionOrder = placement.sectionOrder
        return thread
    }

    public func load() async {
        do {
            snapshot = try await cache.call { try $0.sidebar() } ?? SidebarSnapshot()
            // Upgrade cached layouts so unassigned chats remain available offline.
            if !snapshot.sections.contains(where: { $0.id == Self.unassignedID }) {
                snapshot.sections.append(Section(id: Self.unassignedID, name: "Unassigned",
                                                 order: (sections.last?.order ?? -1) + 1))
            }
        } catch { issue = "Couldn’t load saved chats." }
    }

    public func refresh() async {
        guard pending == 0 else { return }
        let revision = revision
        do {
            let next = try await client.sidebar()
            guard pending == 0, revision == self.revision, next.cursor >= snapshot.cursor else { return }
            if next != snapshot {
                snapshot = next
                try await cache.call { try $0.saveSidebar(next) }
            }
            if retryAction == nil { issue = nil }
            // A change that failed earlier goes again once the server answers.
            else if !isSaving { await retry() }
        } catch {
            // Keep cached chats; connection status is shown app-wide.
        }
    }

    @discardableResult
    public func create(_ name: String, id: String = UUID().uuidString,
                       accepted: (String) -> Void = { _ in }) async -> String? {
        guard !isSaving else { return nil }
        retryAction = { [weak self] in _ = await self?.create(name, id: id) }
        let local = Section(id: id, name: name, order: (sections.last?.order ?? -1) + 1)
        let created: Section? = await change {
            $0.sections.append(local)
        } send: {
            try await self.client.createSection(name: name)
        } accept: { created, snapshot in
            snapshot.sections.removeAll { $0.id == local.id || $0.id == created.id }
            snapshot.sections.append(created)
            accepted(created.id)
        }
        return created?.id
    }

    public func rename(_ id: String, to name: String) async {
        guard id != Self.unassignedID, !isSaving, let index = snapshot.sections.firstIndex(where: { $0.id == id }),
              snapshot.sections[index].name != name else { return }
        retryAction = { [weak self] in await self?.rename(id, to: name) }
        let _: Section? = await change {
            $0.sections[index].name = name
        } send: {
            try await self.client.updateSection(id: id, name: name)
        }
    }

    public func collapse(_ id: String, collapsed: Bool) async {
        guard !isSaving, let index = snapshot.sections.firstIndex(where: { $0.id == id }) else { return }
        retryAction = { [weak self] in await self?.collapse(id, collapsed: collapsed) }
        // Collapsing is a view preference, so it doesn't lock the sidebar while it saves.
        let _: Section? = await change(locking: false) {
            $0.sections[index].collapsed = collapsed
        } send: {
            try await self.client.updateSection(id: id, collapsed: collapsed)
        }
    }

    public func reorder(_ id: String, before target: String?) async {
        guard !isSaving, id != target, let source = sections.firstIndex(where: { $0.id == id }),
              target == nil || sections.contains(where: { $0.id == target }) else { return }
        retryAction = { [weak self] in await self?.reorder(id, before: target) }
        var ordered = sections
        let section = ordered.remove(at: source)
        ordered.insert(section, at: ordered.firstIndex(where: { $0.id == target }) ?? ordered.count)
        for index in ordered.indices { ordered[index].order = Double(index) }
        let next = ordered
        let _: Bool? = await change {
            $0.sections = next
        } send: {
            try await self.client.reorderSection(id: id, beforeID: target)
            return true
        }
    }

    @discardableResult
    public func delete(_ id: String, chats: Bool) async -> Bool {
        guard id != Self.unassignedID, !isSaving, snapshot.sections.contains(where: { $0.id == id }) else { return false }
        retryAction = { [weak self] in await self?.delete(id, chats: chats) }
        let deleted: Bool? = await change {
            $0.sections.removeAll { $0.id == id }
            for index in $0.placements.indices where $0.placements[index].sectionID == id {
                $0.placements[index].sectionID = nil
                $0.placements[index].sectionOrder = nil
            }
        } send: {
            try await self.client.deleteSection(id: id, chats: chats)
            return true
        }
        return deleted == true
    }

    public func shift(_ id: String, by distance: Int, among visible: [Section]? = nil) async {
        let ordered = visible ?? sections
        guard let index = ordered.firstIndex(where: { $0.id == id }), ordered.indices.contains(index + distance) else { return }
        if distance < 0 { await reorder(id, before: ordered[index + distance].id) }
        else { await reorder(ordered[index + distance].id, before: id) }
    }

    public func move(_ thread: Thread, to sectionID: String?) async {
        guard !isSaving else { return }
        let sectionID = sectionID == Self.unassignedID ? nil : sectionID
        retryAction = { [weak self] in await self?.move(thread, to: sectionID) }
        let order = snapshot.placements.filter { $0.sectionID == sectionID }.compactMap(\.sectionOrder).max() ?? -1
        let placement = SidebarPlacement(threadID: thread.id, sectionID: sectionID, sectionOrder: order + 1)
        let _: Thread? = await change {
            Self.place(placement, in: &$0)
        } send: {
            try await self.client.placeThread(id: thread.id, sectionID: sectionID)
        } accept: { updated, snapshot in
            Self.place(Self.placement(updated), in: &snapshot)
        }
    }

    public func pin(_ thread: Thread, pinned: Bool, before target: String? = nil) async {
        guard !isSaving, target != thread.id else { return }
        let pins = snapshot.placements.filter { $0.pinned && $0.threadID != thread.id }
            .sorted { ($0.pinOrder ?? 0) < ($1.pinOrder ?? 0) }
        guard target == nil || pins.contains(where: { $0.threadID == target }) else { return }
        let index = pins.firstIndex(where: { $0.threadID == target }) ?? pins.count
        var ids = pins.map(\.threadID)
        if pinned { ids.insert(thread.id, at: index) }
        retryAction = { [weak self] in await self?.pin(thread, pinned: pinned, before: target) }
        let _: Thread? = await change { snapshot in
            Self.place(SidebarPlacement(threadID: thread.id), in: &snapshot)
            for (order, id) in ids.enumerated() {
                Self.place(SidebarPlacement(threadID: id, pinned: true, pinOrder: Double(order)), in: &snapshot)
            }
        } send: {
            try await self.client.placeThread(id: thread.id, pinned: pinned, before: pinned ? target : nil,
                                              after: pinned && target == nil ? pins.last?.threadID : nil)
        } accept: { updated, snapshot in
            Self.place(Self.placement(updated), in: &snapshot)
        }
    }

    public func retry() async {
        if let retryAction { await retryAction() }
        else { await refresh() }
    }

    private static func place(_ placement: SidebarPlacement, in snapshot: inout SidebarSnapshot) {
        snapshot.placements.removeAll { $0.threadID == placement.threadID }
        snapshot.placements.append(placement)
    }

    private static func placement(_ thread: Thread) -> SidebarPlacement {
        SidebarPlacement(threadID: thread.id, pinned: thread.pinned == true, pinOrder: thread.pinOrder,
                         sectionID: thread.sectionID, sectionOrder: thread.sectionOrder)
    }

    private func change<T: Sendable>(locking: Bool = true, _ update: (inout SidebarSnapshot) -> Void,
                                    send: () async throws -> T,
                                    accept: (T, inout SidebarSnapshot) -> Void = { _, _ in }) async -> T? {
        if locking { isSaving = true }
        pending += 1
        defer { pending -= 1 }
        revision += 1
        let previous = snapshot
        update(&snapshot)
        issue = nil
        let result: T
        do { result = try await send() }
        catch {
            snapshot = previous
            // Only a lost connection retries; a change the server refused would fail again.
            if error.isTransient { issue = "Couldn’t save that change yet. It will try again." }
            else { retryAction = nil; issue = error.reason(or: "Couldn’t save that change.") }
            if locking { isSaving = false }
            return nil
        }
        retryAction = nil
        accept(result, &snapshot)
        // A later save still in flight owns the snapshot; its own refresh will settle it.
        if pending == 1 {
            do {
                let next = try await client.sidebar()
                if pending == 1 { snapshot = next }
            } catch {
                // Saved; the next refresh settles the list.
            }
        }
        do {
            let saved = snapshot
            try await cache.call { try $0.saveSidebar(saved) }
        } catch {
            issue = "Saved, but couldn’t keep a copy on this device."
        }
        if locking { isSaving = false }
        return result
    }
}
