import PolymuxKit
import CoreGraphics

/// The chat list, worked out once for each change to what it depends on. A list this
/// size is read many times in one update, and rebuilding it each time shows up as lag.
@MainActor
final class ChatOrder {
    private var threads: [PolymuxKit.Thread] = []
    private var agents: [Agent] = []
    private var snapshot = SidebarSnapshot()
    private var removed: Set<String> = []
    private var known: [String: Agent] = [:]
    private(set) var chats: [PolymuxKit.Thread] = []
    private(set) var repeated: Set<String> = []

    func update(threads: [PolymuxKit.Thread], agents: [Agent], layout: SidebarLayout, removed: Set<String>) {
        guard threads != self.threads || agents != self.agents || layout.snapshot != snapshot
                || removed != self.removed else { return }
        self.threads = threads
        self.agents = agents
        self.snapshot = layout.snapshot
        self.removed = removed
        known = Dictionary(agents.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        let named = Dictionary(grouping: agents, by: \.name)
        repeated = Set(named.filter { $0.value.count > 1 }.keys)
        chats = threads
            .filter { !removed.contains($0.id) && $0.participantIDs.contains { known[$0] != nil } }
            .map { (thread: layout.applying(to: $0), date: $0.lastMessage?.date ?? .distantPast) }
            .sorted { lhs, rhs in
                switch (lhs.thread.pinRank, rhs.thread.pinRank) {
                case let (left?, right?) where left != right: return left < right
                case (.some, .none): return true
                case (.none, .some): return false
                default: break
                }
                if lhs.date == rhs.date { return lhs.thread.id < rhs.thread.id }
                return lhs.date > rhs.date
            }
            .map(\.thread)
    }

    func members(_ thread: PolymuxKit.Thread) -> [Agent] {
        thread.participantIDs.compactMap { known[$0] }
    }
}

/// Row positions change every frame of a scroll, and only a drag needs them.
@MainActor
final class SidebarGeometry {
    var areas: [SidebarArea: CGRect] = [:]
    var chats: [String: CGRect] = [:]
}
