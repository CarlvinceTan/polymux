import Foundation
import Observation

public struct ActivityView: Equatable, Sendable {
    public var animation = "idle"
    public var label = "Ready"
    public var typing = false
    public var working = false
    public var cueID: String?
    public var expression: String?

    public init(animation: String = "idle", label: String = "Ready", typing: Bool = false,
                working: Bool = false, cueID: String? = nil, expression: String? = nil) {
        self.animation = animation
        self.label = label
        self.typing = typing
        self.working = working
        self.cueID = cueID
        self.expression = expression
    }
}

@MainActor @Observable
public final class ActivityStore {
    public private(set) var snapshots: [String: Run] = [:]
    public private(set) var cues: [String: ActivityView] = [:]
    @ObservationIgnored private var expiries: [String: Task<Void, Never>] = [:]
    @ObservationIgnored private var deliveries: [String: Set<String>] = [:]
    @ObservationIgnored private var dispatched: Set<String> = []
    @ObservationIgnored private var introduced: Set<String> = []

    public init() {}

    public func restore(_ runs: [Run]) {
        for run in runs where run.seq >= (snapshots[run.id]?.seq ?? -1) {
            snapshots[run.id] = run
            if run.trigger == "created", run.activity == "generating" || run.status == "completed" { introduced.insert(run.id) }
        }
    }

    public func reconcile(_ thread: Thread) {
        let runs = thread.runs
        let ids = Set(runs.map(\.id))
        snapshots = snapshots.filter { $0.value.conversationID != thread.id || ids.contains($0.key) || $0.value.seq > thread.lastSeq }
        for agent in thread.agentIDs where cues[key(thread.id, agent)] != nil {
            let key = key(thread.id, agent)
            if !snapshots.values.contains(where: { cues[key]?.cueID?.hasPrefix($0.id + ":") == true }) {
                expiries[key]?.cancel(); expiries[key] = nil; cues[key] = nil
            }
        }
        restore(runs)
    }

    public func retain(_ conversations: Set<String>) {
        snapshots = snapshots.filter { conversations.contains($0.value.conversationID) }
        deliveries = deliveries.filter { conversations.contains($0.key) }
        let ids = Set(snapshots.keys)
        dispatched.formIntersection(ids)
        introduced.formIntersection(ids)
        for key in Array(cues.keys) where !conversations.contains(where: { key.hasPrefix($0 + ":") }) {
            expiries[key]?.cancel(); expiries[key] = nil; cues[key] = nil
        }
    }

    public func receive(_ event: Event, live: Bool) {
        if let delivery = event.delivery, delivery.conversationID == event.conversationID,
           event.type == "peer.delivery" || event.type == "group.handoff" {
            let fresh = deliveries[event.conversationID, default: []].insert(delivery.id).inserted
            guard fresh, live, delivery.status == "queued" || delivery.status == "delivered" else { return }
            dispatched.insert(delivery.parentRunID)
            cue(key(delivery.conversationID, delivery.sourceAgentID),
                ActivityView(animation: "comet", label: "Message dispatched to peer", working: true,
                             cueID: delivery.parentRunID + ":delivery:" + delivery.id))
            return
        }
        guard let run = event.run, run.conversationID == event.conversationID,
              run.seq > (snapshots[run.id]?.seq ?? -1) else { return }
        snapshots[run.id] = run
        let key = key(run.conversationID, run.agentID)
        let sameCue = cues[key]?.cueID?.hasPrefix(run.id + ":") == true
        if run.status == "cancelled" || run.status == "failed" || run.status == "interrupted"
            || run.status == "running" && (!sameCue || cues[key]?.animation == "wide" && run.activity != "generating") {
            expiries[key]?.cancel(); cues[key] = nil
        }
        guard live else {
            if run.trigger == "created", run.activity == "generating" { introduced.insert(run.id) }
            return
        }
        if run.status == "completed" {
            if run.outcome == "quiet" {
                expiries[key]?.cancel(); cues[key] = nil
                return
            }
            if run.outcome == "waiting_input" { return }
            if run.outcome == "handoff", dispatched.contains(run.id) { return }
            let animation = run.outcome == "artifact" ? "burst" : run.outcome == "handoff" ? "comet" : run.outcome == "notice" ? "notify" : "wink"
            cue(key, ActivityView(animation: animation, label: run.summary, cueID: run.id + ":done"))
        } else if run.trigger == "created", run.status == "running", run.activity == "generating",
                  introduced.insert(run.id).inserted {
            cue(key, ActivityView(animation: "wide", label: "Introducing", typing: true, working: true, cueID: run.id + ":intro"))
        }
    }

    private func cue(_ key: String, _ value: ActivityView) {
        expiries[key]?.cancel()
        cues[key] = value
        expiries[key] = Task { [weak self] in
            let seconds = value.animation == "burst" ? 2.6 : value.animation == "comet" ? 2.4 : value.animation == "wide" ? 1.8 : value.animation == "notify" ? 2.2 : 1.6
            do { try await Task.sleep(for: .seconds(seconds)) } catch { return }
            guard let self, self.cues[key]?.cueID == value.cueID else { return }
            self.cues[key] = nil
            self.expiries[key] = nil
        }
    }

    public func view(conversationID: String, agentID: String, connected: Bool = true,
                     unread: Bool = false, speaking: Bool = false) -> ActivityView {
        Self.present(runs: Array(snapshots.values), conversationID: conversationID, agentID: agentID,
                     connected: connected, unread: unread, speaking: speaking, cue: cues[key(conversationID, agentID)])
    }

    public static func present(runs: [Run], conversationID: String, agentID: String, connected: Bool = true,
                               unread: Bool = false, speaking: Bool = false, cue: ActivityView? = nil) -> ActivityView {
        let scoped = runs.filter { $0.conversationID == conversationID && $0.agentID == agentID }
        let ordered: (Run, Run) -> Bool = { a, b in
            let first = WireDate.parse(a.createdAt) ?? .distantPast
            let second = WireDate.parse(b.createdAt) ?? .distantPast
            return first == second ? a.seq < b.seq : first < second
        }
        let current = scoped.filter { $0.status == "waiting_input" }.max(by: ordered)
            ?? scoped.filter { $0.status == "running" }.max(by: ordered)
            ?? scoped.filter { $0.status == "waiting_peer" }.max(by: ordered)
            ?? scoped.max(by: ordered)
        guard connected else { return ActivityView(label: "Disconnected; remote activity unknown") }
        if let run = current {
            if let cue, cue.cueID?.hasPrefix(run.id + ":delivery:") == true,
               run.status == "running" || run.status == "waiting_peer" || run.status == "completed" && run.outcome != "waiting_input" {
                return cue
            }
            switch run.status {
            case "waiting_input": return ActivityView(animation: "alert", label: run.summary, expression: expression(for: run))
            case "waiting_peer": return ActivityView(animation: "orbit", label: run.summary, working: true)
            case "running":
                if speaking { return ActivityView(animation: "play", label: "Speaking", working: true) }
                if run.trigger == "created", run.activity == "preparing" { return ActivityView(animation: "egg", label: "Initializing", working: true) }
                if run.trigger == "created", let cue, cue.animation == "wide" { return cue }
                switch run.activity {
                case "tool": return ActivityView(animation: "swirl", label: run.summary, working: true, expression: expression(for: run))
                case "rendering": return ActivityView(animation: "hexagon", label: run.summary, working: true)
                case "handoff": return ActivityView(animation: "comet", label: run.summary, working: true)
                default: return ActivityView(animation: "thinking", label: run.summary, typing: run.trigger != "review", working: true,
                                             expression: expression(for: run))
                }
            case "failed", "interrupted":
                return ActivityView(animation: "exclaim", label: run.error?.message ?? run.summary, expression: expression(for: run))
            case "completed" where run.outcome == "waiting_input": return ActivityView(animation: "alert", label: run.summary, expression: expression(for: run))
            case "queued": return ActivityView(label: "Queued", expression: expression(for: run))
            default: break
            }
        }
        if speaking { return ActivityView(animation: "play", label: "Speaking") }
        if let cue, current == nil || cue.cueID?.hasPrefix((current?.id ?? "") + ":") == true { return cue }
        if unread { return ActivityView(animation: "notify", label: "Unread result") }
        return ActivityView()
    }

    /// Contextual face for a run, as ExpressionID raw values; nil keeps the user's resting expression.
    public static func expression(for run: Run) -> String? {
        switch run.status {
        case "waiting_input", "queued": return "attentif"
        case "failed": return "triste"
        case "interrupted": return "confus"
        case "completed": return run.outcome == "waiting_input" ? "attentif" : nil
        case "running": break
        default: return nil
        }
        switch run.activity {
        case "tool": return "mefiant"
        case "rendering", "handoff": return nil
        case "preparing" where run.trigger == "created": return nil
        default: return run.trigger == "review" ? "mefiant" : "curieux"
        }
    }

    private func key(_ conversation: String, _ agent: String) -> String { conversation + ":" + agent }
}
