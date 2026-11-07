import Foundation
import Observation

public enum ActivityMarker: String, Sendable {
    case attention, unread

    public var label: String { self == .attention ? "Needs attention" : "Unread" }

    public static func resolve(waiting: Bool, unread: Bool) -> Self? {
        waiting ? .attention : unread ? .unread : nil
    }
}

public struct ActivityView: Equatable, Sendable {
    public var animation = "idle"
    public var label = "Ready"
    public var typing = false
    public var working = false
    public var cueID: String?
    public var expression: String?
    public var mood: String?
    public var marker: ActivityMarker?
    public var needsAttention: Bool { marker == .attention }
    public var animating: Bool { working || mood == "streaming" || mood == "listening" }
    /// The activity line's wording for this bot; nil leaves it to typing or a working phrase.
    public var phrase: String?
    /// The model's own line for the current step, shown ahead of a tool phrase.
    public var status: String?

    public init(animation: String = "idle", label: String = "Ready", typing: Bool = false,
                working: Bool = false, cueID: String? = nil, expression: String? = nil, mood: String? = nil,
                phrase: String? = nil, status: String? = nil, marker: ActivityMarker? = nil) {
        self.animation = animation
        self.label = label
        self.typing = typing
        self.working = working
        self.cueID = cueID
        self.expression = expression
        self.mood = mood
        self.phrase = phrase
        self.status = status
        self.marker = marker
    }
}

@MainActor @Observable
public final class ActivityStore {
    public struct Handoff: Identifiable, Equatable, Sendable {
        public let id, conversationID, source, target: String
        public let result: Bool
        public let at: Date
    }
    public private(set) var handoffs: [Handoff] = []
    public private(set) var snapshots: [String: Run] = [:]
    public private(set) var cues: [String: ActivityView] = [:]
    @ObservationIgnored private var expiries: [String: Task<Void, Never>] = [:]
    @ObservationIgnored private var deliveries: [String: Set<String>] = [:]
    @ObservationIgnored private var dispatched: Set<String> = []
    @ObservationIgnored private var introduced: Set<String> = []
    private var streaming: Set<String> = []
    private var listening: Set<String> = []

    public init() {}

    public func restore(_ runs: [Run]) {
        for var run in runs where run.seq >= (snapshots[run.id]?.seq ?? -1) {
            // Thread listings leave tools out; keep the ones the run's own events delivered.
            if run.tools?.isEmpty != false, let known = snapshots[run.id], known.seq == run.seq { run.tools = known.tools }
            snapshots[run.id] = run
            trackIntro(run)
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
        handoffs.removeAll { !conversations.contains($0.conversationID) }
        snapshots = snapshots.filter { conversations.contains($0.value.conversationID) }
        deliveries = deliveries.filter { conversations.contains($0.key) }
        let ids = Set(snapshots.keys)
        dispatched.formIntersection(ids)
        introduced.formIntersection(ids)
        streaming = streaming.filter { key in conversations.contains { key.hasPrefix($0 + ":") } }
        listening = listening.filter { key in conversations.contains { key.hasPrefix($0 + ":") } }
        for key in Array(cues.keys) where !conversations.contains(where: { key.hasPrefix($0 + ":") }) {
            expiries[key]?.cancel(); expiries[key] = nil; cues[key] = nil
        }
    }

    public func receive(_ event: Event, live: Bool) {
        if event.type == EventType.messageDelta, event.text?.isEmpty == false {
            if let id = event.runID ?? event.run?.id { introduced.insert(id) }
            else {
                for run in snapshots.values where run.conversationID == event.conversationID
                    && run.agentID == event.agentID && run.trigger == RunTrigger.created && run.status == RunStatus.running {
                    introduced.insert(run.id)
                }
            }
        }
        if live, let agent = event.agentID ?? event.run?.agentID {
            let key = key(event.conversationID, agent)
            if event.type == EventType.messageDelta, event.text?.isEmpty == false { streaming.insert(key) }
            if event.type == EventType.runStart || event.type == EventType.toolStart || event.type == EventType.messageDone
                || event.run.map({ $0.status != RunStatus.running }) == true { streaming.remove(key) }
        }
        if let delivery = event.delivery, delivery.conversationID == event.conversationID,
           event.type == EventType.peerDelivery || event.type == EventType.groupHandoff {
            let fresh = deliveries[event.conversationID, default: []].insert(delivery.id).inserted
            guard fresh, live, delivery.status == PeerDeliveryStatus.queued || delivery.status == PeerDeliveryStatus.delivered else { return }
            dispatched.insert(delivery.parentRunID)
            handoffs.removeAll { Date().timeIntervalSince($0.at) > 1 }
            handoffs.append(Handoff(id: delivery.id, conversationID: delivery.conversationID,
                                    source: delivery.sourceAgentID, target: delivery.targetAgentID,
                                    result: delivery.intent == PeerDeliveryIntent.result, at: Date()))
            handoffs = Array(handoffs.suffix(3))
            cue(key(delivery.conversationID, delivery.sourceAgentID),
                ActivityView(animation: "comet", label: "Message dispatched to peer", working: true,
                             cueID: delivery.parentRunID + ":delivery:" + delivery.id))
            return
        }
        guard let run = event.run, run.conversationID == event.conversationID,
              run.seq > (snapshots[run.id]?.seq ?? -1) else { return }
        snapshots[run.id] = run
        trackIntro(run)
        let key = key(run.conversationID, run.agentID)
        let sameCue = cues[key]?.cueID?.hasPrefix(run.id + ":") == true
        if run.status == RunStatus.cancelled || run.status == RunStatus.failed || run.status == RunStatus.interrupted
            || run.status == RunStatus.running && !sameCue {
            expiries[key]?.cancel(); cues[key] = nil
        }
        guard live else { return }
        if run.status == RunStatus.completed {
            if run.outcome == "quiet" {
                expiries[key]?.cancel(); cues[key] = nil
                return
            }
            if run.outcome == "waiting_input" { return }
            if run.outcome == "handoff", dispatched.contains(run.id) { return }
            let animation = run.outcome == "artifact" ? "burst" : run.outcome == "handoff" ? "comet" : "wink"
            cue(key, ActivityView(animation: animation, label: run.summary, cueID: run.id + ":done"))
        }
    }

    private func trackIntro(_ run: Run) {
        if run.trigger == RunTrigger.created, Self.introEnded(run) { introduced.insert(run.id) }
    }

    private static func introEnded(_ run: Run) -> Bool {
        run.status != RunStatus.queued && run.status != RunStatus.running || run.tools?.isEmpty == false
            || !["", "preparing", "generating"].contains(run.activity)
    }

    private func cue(_ key: String, _ value: ActivityView) {
        expiries[key]?.cancel()
        cues[key] = value
        expiries[key] = Task { [weak self] in
            let seconds = value.animation == "burst" || value.animation == "wink" ? 8.0
                : value.animation == "comet" ? 2.4 : 2.2
            do { try await Task.sleep(for: .seconds(seconds)) } catch { return }
            guard let self, self.cues[key]?.cueID == value.cueID else { return }
            self.cues[key] = nil
            self.expiries[key] = nil
        }
    }

    public func view(conversationID: String, agentID: String, connected: Bool = true,
                     unread: Bool = false, speaking: Bool = false) -> ActivityView {
        Self.present(runs: Array(snapshots.values), conversationID: conversationID, agentID: agentID,
                     connected: connected, unread: unread, speaking: speaking, cue: cues[key(conversationID, agentID)],
                     streaming: streaming.contains(key(conversationID, agentID)),
                     listening: listening.contains(key(conversationID, agentID)), introduced: introduced)
    }

    public func notice(for message: Message) -> String? {
        guard message.role == MessageRole.assistant else { return nil }
        return message.runID.flatMap { snapshots[$0]?.notice }
    }

    public func issue(for message: Message) -> WireError? {
        guard message.role == MessageRole.assistant, message.status == MessageStatus.error else { return nil }
        let run = message.runID.flatMap { snapshots[$0] }
        guard let agent = run?.agentID ?? message.agentID else { return message.error }
        let runs = Array(snapshots.values)
        if let run {
            guard Self.current(runs, conversationID: message.threadID, agentID: agent)?.id == run.id,
                  Self.hasIssue(run, runs: runs) else { return nil }
            return run.error ?? message.error
        }
        guard !runs.contains(where: {
            $0.agentID == agent && Self.newer($0, than: message.createdAt)
        }) else { return nil }
        return message.error
    }

    public func setListening(_ active: Bool, conversationID: String, agentID: String) {
        let key = key(conversationID, agentID)
        if active { listening.insert(key) } else { listening.remove(key) }
    }

    public func marker(conversationID: String, agentIDs: [String], unread: Bool = false) -> ActivityMarker? {
        Self.marker(runs: Array(snapshots.values), conversationID: conversationID, agentIDs: agentIDs, unread: unread)
    }

    public static func marker(runs: [Run], conversationID: String, agentIDs: [String], unread: Bool = false) -> ActivityMarker? {
        let waiting = agentIDs.contains {
            present(runs: runs, conversationID: conversationID, agentID: $0).needsAttention
        }
        return ActivityMarker.resolve(waiting: waiting, unread: unread)
    }

    public static func present(runs: [Run], conversationID: String, agentID: String, connected: Bool = true,
                               unread: Bool = false, speaking: Bool = false, cue: ActivityView? = nil,
                               streaming: Bool = false, listening: Bool = false, introduced: Set<String> = []) -> ActivityView {
        var value = presentation(runs: runs, conversationID: conversationID, agentID: agentID,
                                 speaking: speaking, cue: cue, streaming: streaming, introduced: introduced)
        value.marker = ActivityMarker.resolve(waiting: value.animation == "alert", unread: unread)
        guard connected else {
            return ActivityView(label: "Disconnected; remote activity unknown", marker: value.marker)
        }
        switch value.animation {
        case "thinking": value.mood = streaming ? "streaming" : "thinking"
        case "swirl":
            let run = runs.filter { $0.conversationID == conversationID && $0.agentID == agentID && $0.status == RunStatus.running }
                .max { $0.seq < $1.seq }
            value.mood = run.map(toolMood) ?? "working"
        case "hexagon": value.mood = "working"
        case "comet": value.mood = "sending"
        case "orbit": value.mood = "receiving"
        case "play": value.mood = "streaming"
        case "alert": value.mood = "blocked"
        case "exclaim": value.mood = value.expression == "confus" ? "interrupted" : "failed"
        case "wink": value.mood = "done"
        case "burst": value.mood = "celebrate"
        case "wide", "egg": value.mood = "spawning"
        default: value.mood = listening ? "listening" : "idle"
        }
        return value
    }

    private static func toolMood(_ run: Run) -> String {
        let tool = run.tools?.first { $0.id == run.toolID }
            ?? run.tools?.last { $0.status == ToolActivityStatus.running }
        let name = tool?.name.lowercased() ?? ""
        let parts = Set(name.split { !$0.isLetter }.map(String.init))
        if !parts.isDisjoint(with: ["search", "browse", "browser", "fetch"]) { return "searching" }
        if !parts.isDisjoint(with: ["read", "list", "recall", "history", "memory"]) { return "reading" }
        if !parts.isDisjoint(with: ["write", "edit", "patch"]) { return "writing" }
        if !parts.isDisjoint(with: ["send", "message", "handoff"]) { return "sending" }
        return "working"
    }

    private static func newer(_ run: Run, than date: String) -> Bool {
        guard let created = WireDate.parse(run.createdAt), let previous = WireDate.parse(date) else { return false }
        return created > previous
    }

    private static func hasIssue(_ run: Run, runs: [Run]) -> Bool {
        guard run.status == RunStatus.failed || run.status == RunStatus.interrupted else { return false }
        return !runs.contains {
            $0.agentID == run.agentID && newer($0, than: run.createdAt)
        }
    }

    private static func current(_ runs: [Run], conversationID: String, agentID: String) -> Run? {
        let scoped = runs.filter { $0.conversationID == conversationID && $0.agentID == agentID }
        let ordered: (Run, Run) -> Bool = { a, b in
            let first = WireDate.parse(a.createdAt) ?? .distantPast
            let second = WireDate.parse(b.createdAt) ?? .distantPast
            return first == second ? a.seq < b.seq : first < second
        }
        return scoped.filter { run in
            run.status == RunStatus.waitingInput && !scoped.contains {
                newer($0, than: run.createdAt) && $0.status != RunStatus.failed && $0.status != RunStatus.interrupted
            }
        }.max(by: ordered)
            ?? scoped.filter { $0.status == RunStatus.running || $0.status == RunStatus.resuming }.max(by: ordered)
            ?? scoped.filter { $0.status == RunStatus.waitingPeer }.max(by: ordered)
            ?? scoped.max(by: ordered)
    }

    private static func presentation(runs: [Run], conversationID: String, agentID: String,
                                     speaking: Bool, cue: ActivityView?, streaming: Bool,
                                     introduced: Set<String>) -> ActivityView {
        let current = current(runs, conversationID: conversationID, agentID: agentID)
        if let run = current {
            if let cue, cue.cueID?.hasPrefix(run.id + ":delivery:") == true,
               run.status == RunStatus.running || run.status == RunStatus.waitingPeer || run.status == RunStatus.completed && run.outcome != "waiting_input" {
                return cue
            }
            switch run.status {
            case RunStatus.waitingInput: return ActivityView(animation: "alert", label: run.summary, expression: expression(for: run))
            case RunStatus.waitingPeer: return ActivityView(animation: "orbit", label: run.summary, working: true,
                                                            phrase: run.statusText ?? ActivityPhrase.tool("message_agent"),
                                                            status: run.statusText)
            case RunStatus.running, RunStatus.resuming:
                if speaking { return ActivityView(animation: "play", label: "Speaking", working: true) }
                if run.trigger == RunTrigger.created, !introEnded(run), !introduced.contains(run.id), !streaming {
                    if run.activity == RunActivity.preparing { return ActivityView(animation: "egg", label: "Initializing", working: true) }
                    if run.activity == RunActivity.generating {
                        return ActivityView(animation: "wide", label: "Introducing", typing: true, working: true,
                                            cueID: run.id + ":intro")
                    }
                }
                switch run.activity {
                case RunActivity.tool:
                    let current = run.tools?.first { $0.id == run.toolID && $0.status == ToolActivityStatus.running }
                        ?? run.tools?.last { $0.status == ToolActivityStatus.running }
                    let status = streaming ? nil : run.statusText
                    let phrase = streaming ? nil : (status ?? ActivityPhrase.tool(current?.name ?? "")
                        ?? ActivityPhrase.draw(ActivityPhrase.working, seed: run.id))
                    return ActivityView(animation: "swirl", label: run.summary, typing: streaming, working: true,
                                        expression: expression(for: run), phrase: phrase, status: status)
                case RunActivity.rendering:
                    let status = streaming ? nil : run.statusText
                    let phrase = streaming ? nil : (status ?? ActivityPhrase.draw(ActivityPhrase.working, seed: run.id))
                    return ActivityView(animation: "hexagon", label: run.summary, typing: streaming, working: true,
                                        phrase: phrase, status: status)
                case RunActivity.handoff:
                    let status = streaming ? nil : run.statusText
                    let phrase = streaming ? nil : (status ?? ActivityPhrase.tool("message_agent"))
                    return ActivityView(animation: "comet", label: run.summary, typing: streaming, working: true,
                                        phrase: phrase, status: status)
                default:
                    let status = streaming ? nil : run.statusText
                    return ActivityView(animation: "thinking", label: run.summary, typing: run.trigger != RunTrigger.review, working: true,
                                        expression: expression(for: run),
                                        phrase: status ?? (streaming ? nil : ActivityPhrase.draw(ActivityPhrase.thinking, seed: run.id)),
                                        status: status)
                }
            case RunStatus.failed, RunStatus.interrupted:
                if hasIssue(run, runs: runs) {
                    return ActivityView(animation: "exclaim", label: run.error?.message ?? run.summary, expression: expression(for: run))
                }
            case RunStatus.completed where run.outcome == "waiting_input": return ActivityView(animation: "alert", label: run.summary, expression: expression(for: run))
            case RunStatus.queued: return ActivityView(label: "Queued", expression: expression(for: run))
            default: break
            }
        }
        if speaking { return ActivityView(animation: "play", label: "Speaking") }
        if let cue, current == nil || cue.cueID?.hasPrefix((current?.id ?? "") + ":") == true { return cue }
        return ActivityView()
    }

    /// Contextual face for a run, as ExpressionID raw values; nil keeps the user's resting expression.
    public static func expression(for run: Run) -> String? {
        switch run.status {
        case RunStatus.waitingInput, RunStatus.queued: return "attentif"
        case RunStatus.failed: return "triste"
        case RunStatus.interrupted: return "confus"
        case RunStatus.completed: return run.outcome == "waiting_input" ? "attentif" : nil
        case RunStatus.running, RunStatus.resuming: break
        default: return nil
        }
        switch run.activity {
        case RunActivity.tool: return "mefiant"
        case RunActivity.rendering, RunActivity.handoff: return nil
        case RunActivity.preparing where run.trigger == RunTrigger.created: return nil
        default: return run.trigger == RunTrigger.review ? "mefiant" : "curieux"
        }
    }

    private func key(_ conversation: String, _ agent: String) -> String { conversation + ":" + agent }
}
