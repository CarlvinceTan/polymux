import Foundation
import Observation

@MainActor @Observable
public final class SidebarLayout {
    public private(set) var snapshot = SidebarSnapshot()
    public private(set) var isSaving = false
    public private(set) var issue: String?
    @ObservationIgnored private let client: APIClient
    @ObservationIgnored private let cache: LocalCacheActor
    @ObservationIgnored private var revision = 0
    @ObservationIgnored private var retryAction: (() async -> Void)?

    public init(client: APIClient, cache: LocalCacheActor) {
        self.client = client
        self.cache = cache
    }

    public var sections: [Section] {
        snapshot.sections.sorted { $0.order == $1.order ? $0.id < $1.id : $0.order < $1.order }
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
        do { snapshot = try await cache.call { try $0.sidebar() } ?? SidebarSnapshot() }
        catch { issue = "Couldn’t load the saved sidebar. \(error.localizedDescription)" }
    }

    public func refresh() async {
        guard !isSaving else { return }
        let revision = revision
        do {
            let next = try await client.sidebar()
            guard !isSaving, revision == self.revision, next.cursor >= snapshot.cursor else { return }
            snapshot = next
            try await cache.call { try $0.saveSidebar(next) }
            if retryAction == nil { issue = nil }
        } catch {
            guard !isSaving, revision == self.revision else { return }
            if retryAction == nil { issue = "Couldn’t refresh the sidebar. \(error.localizedDescription)" }
        }
    }

    @discardableResult
    public func create(_ name: String, id: String = UUID().uuidString,
                       accepted: (String) -> Void = { _ in }) async -> String? {
        guard !isSaving else { return nil }
        retryAction = { [weak self] in _ = await self?.create(name) }
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
        guard !isSaving, let index = snapshot.sections.firstIndex(where: { $0.id == id }),
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
        let _: Section? = await change {
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

    public func shift(_ id: String, by distance: Int) async {
        let ordered = sections
        guard let index = ordered.firstIndex(where: { $0.id == id }), ordered.indices.contains(index + distance) else { return }
        if distance < 0 { await reorder(id, before: ordered[index + distance].id) }
        else { await reorder(ordered[index + distance].id, before: id) }
    }

    public func move(_ thread: Thread, to sectionID: String?) async {
        guard !isSaving else { return }
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

    public func pin(_ thread: Thread, pinned: Bool) async {
        guard !isSaving else { return }
        retryAction = { [weak self] in await self?.pin(thread, pinned: pinned) }
        let order = (snapshot.placements.compactMap(\.pinOrder).max() ?? -1) + 1
        let placement = SidebarPlacement(threadID: thread.id, pinned: pinned, pinOrder: pinned ? order : nil)
        let _: Thread? = await change {
            Self.place(placement, in: &$0)
        } send: {
            try await self.client.updateThread(id: thread.id, pinned: pinned)
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

    private func change<T: Sendable>(_ update: (inout SidebarSnapshot) -> Void,
                                    send: () async throws -> T,
                                    accept: (T, inout SidebarSnapshot) -> Void = { _, _ in }) async -> T? {
        isSaving = true
        revision += 1
        let previous = snapshot
        update(&snapshot)
        issue = nil
        let result: T
        do { result = try await send() }
        catch {
            snapshot = previous
            issue = "Couldn’t update the sidebar. \(error.localizedDescription)"
            isSaving = false
            return nil
        }
        retryAction = nil
        accept(result, &snapshot)
        do {
            snapshot = try await client.sidebar()
        } catch {
            issue = "The change was saved, but the sidebar couldn’t refresh. \(error.localizedDescription)"
        }
        do {
            let saved = snapshot
            try await cache.call { try $0.saveSidebar(saved) }
        } catch {
            issue = "The change was saved, but couldn’t be cached on this device. \(error.localizedDescription)"
        }
        isSaving = false
        return result
    }
}
