import AvatarKit
import Foundation
import Testing
@testable import PolymuxKit

@Suite @MainActor struct ActivityTests {
    private func run(_ id: String = "r", status: String = "running", activity: String = "generating",
                     trigger: String = "message", seq: Int64 = 1, created: String = "2026-10-09T12:00:00Z") -> Run {
        Run(id: id, conversationID: "group", trigger: trigger, attempt: 1, seq: seq,
            createdAt: created, updatedAt: created, activity: activity, agentID: "a", status: status, summary: status)
    }

    @Test func statesMapToExistingAnimations() {
        let cases: [(Run, String, Bool)] = [
            (run(status: "queued"), "idle", false),
            (run(activity: "preparing", trigger: "created"), "egg", false),
            (run(activity: "preparing"), "thinking", true),
            (run(), "thinking", true),
            (run(trigger: "review"), "thinking", false),
            (run(activity: "tool"), "swirl", false),
            (run(activity: "rendering"), "hexagon", false),
            (run(activity: "handoff"), "comet", false),
            (run(status: "waiting_peer"), "orbit", false),
            (run(status: "waiting_input"), "alert", false),
            (run(status: "failed"), "exclaim", false),
            (run(status: "interrupted"), "exclaim", false),
            (run(status: "cancelled"), "idle", false),
            (run(status: "completed"), "idle", false)
        ]
        for (run, animation, typing) in cases {
            let value = ActivityStore.present(runs: [run], conversationID: "group", agentID: "a")
            #expect(value.animation == animation)
            #expect(value.typing == typing)
        }
        #expect(ActivityStore.present(runs: [], conversationID: "group", agentID: "a", speaking: true).animation == "play")
        #expect(ActivityStore.present(runs: [], conversationID: "group", agentID: "a", unread: true).animation == "notify")
        #expect(ActivityStore.present(runs: [run()], conversationID: "group", agentID: "a", connected: false).animation == "idle")
    }

    @Test func expressionsFollowRunContext() {
        let cases: [(Run, String?)] = [
            (run(status: "queued"), "attentif"),
            (run(activity: "preparing", trigger: "created"), nil),
            (run(activity: "preparing"), "curieux"),
            (run(), "curieux"),
            (run(trigger: "review"), "mefiant"),
            (run(activity: "tool"), "mefiant"),
            (run(activity: "rendering"), nil),
            (run(activity: "handoff"), nil),
            (run(status: "waiting_peer"), nil),
            (run(status: "waiting_input"), "attentif"),
            (run(status: "failed"), "triste"),
            (run(status: "interrupted"), "confus"),
            (run(status: "cancelled"), nil),
            (run(status: "completed"), nil)
        ]
        for (run, expression) in cases {
            let view = ActivityStore.present(runs: [run], conversationID: "group", agentID: "a")
            #expect(view.expression == expression, "\(run.status)/\(run.activity)/\(run.trigger)")
        }
        var waiting = run("question", status: "completed")
        waiting.outcome = "waiting_input"
        #expect(ActivityStore.present(runs: [waiting], conversationID: "group", agentID: "a").expression == "attentif")
        #expect(ActivityStore.present(runs: [], conversationID: "group", agentID: "a", speaking: true).expression == nil)
        #expect(ActivityStore.present(runs: [], conversationID: "group", agentID: "a", unread: true).expression == nil)
        #expect(ActivityStore.present(runs: [run()], conversationID: "group", agentID: "a", connected: false).expression == nil)
    }

    @Test func expressionTableIsTotalDeterministicAndScoped() {
        let statuses = ["queued", "running", "waiting_input", "waiting_peer", "completed", "failed", "cancelled", "interrupted"]
        let activities = ["", "preparing", "generating", "tool", "rendering", "handoff"]
        let triggers = ["message", "created", "peer", "handoff", "routine", "review", "follow_up", "task", "event"]
        let allowed: Set<String?> = [nil, "attentif", "curieux", "mefiant", "triste", "confus"]
        let animated: Set<String> = ["idle", "thinking", "swirl", "alert", "exclaim"]
        for status in statuses {
            for activity in activities {
                for trigger in triggers {
                    var probe = run(status: status, activity: activity, trigger: trigger)
                    if status == "completed" { probe.outcome = "waiting_input" }
                    let expression = ActivityStore.expression(for: probe)
                    #expect(allowed.contains(expression), "\(status)/\(activity)/\(trigger)")
                    #expect(ActivityStore.expression(for: probe) == expression)
                    let view = ActivityStore.present(runs: [probe], conversationID: "group", agentID: "a")
                    if view.expression != nil {
                        #expect(animated.contains(view.animation),
                                "\(status)/\(activity)/\(trigger) -> \(view.animation) + \(view.expression ?? "nil")")
                    }
                    if animated.contains(view.animation) {
                        #expect(view.expression == expression,
                                "\(status)/\(activity)/\(trigger) -> \(view.animation) + \(view.expression ?? "nil")")
                    }
                }
            }
        }
    }

    @Test func presentationNamesExistInAvatarKit() {
        let statuses = ["queued", "running", "waiting_input", "waiting_peer", "completed", "failed", "cancelled", "interrupted"]
        let activities = ["", "preparing", "generating", "tool", "rendering", "handoff"]
        let triggers = ["message", "created", "peer", "handoff", "routine", "review", "follow_up", "task", "event"]
        var animations: Set<String> = []
        var expressions: Set<String> = []
        for status in statuses {
            for activity in activities {
                for trigger in triggers {
                    var probe = run(status: status, activity: activity, trigger: trigger)
                    if status == "completed" { probe.outcome = "waiting_input" }
                    let view = ActivityStore.present(runs: [probe], conversationID: "group", agentID: "a")
                    animations.insert(view.animation)
                    if let expression = view.expression { expressions.insert(expression) }
                }
            }
        }
        for view in [ActivityStore.present(runs: [], conversationID: "group", agentID: "a", speaking: true),
                     ActivityStore.present(runs: [], conversationID: "group", agentID: "a", unread: true),
                     ActivityStore.present(runs: [run()], conversationID: "group", agentID: "a", connected: false)] {
            animations.insert(view.animation)
        }
        let store = ActivityStore()
        var intro = run("intro", trigger: "created", seq: 1)
        store.receive(Event(seq: 1, conversationID: "group", type: "run.update", createdAt: intro.updatedAt, run: intro), live: true)
        animations.insert(store.view(conversationID: "group", agentID: "a").animation)
        intro.status = "completed"
        intro.outcome = "reply"
        intro.seq = 2
        store.receive(Event(seq: 2, conversationID: "group", type: "run.done", createdAt: intro.updatedAt, run: intro), live: true)
        animations.insert(store.view(conversationID: "group", agentID: "a").animation)
        var latest = intro.id
        for (index, outcome) in [("handoff", "handoff"), ("artifact", "artifact"), ("notice", "notice")].enumerated() {
            var finished = run("done-\(outcome.0)", status: "completed", seq: Int64(index + 3))
            finished.outcome = outcome.1
            store.receive(Event(seq: Int64(index + 3), conversationID: "group", type: "run.done",
                                createdAt: finished.updatedAt, run: finished), live: true)
            animations.insert(store.view(conversationID: "group", agentID: "a").animation)
            latest = finished.id
        }
        let delivery = PeerDelivery(id: "delivery", sourceAgentID: "a", targetAgentID: "b", conversationID: "group",
                                    parentRunID: latest, intent: "request", status: "queued")
        store.receive(Event(seq: 7, conversationID: "group", type: "peer.delivery", delivery: delivery), live: true)
        animations.insert(store.view(conversationID: "group", agentID: "a").animation)

        for name in animations { #expect(StateID(rawValue: name) != nil, "unknown animation \(name)") }
        for name in expressions { #expect(ExpressionID(rawValue: name) != nil, "unknown expression \(name)") }
        #expect(animations.contains("exclaim"), "\(animations.sorted())")
        #expect(animations.contains("burst"), "\(animations.sorted())")
        #expect(animations.contains("comet"), "\(animations.sorted())")
        #expect(expressions.contains("triste") && expressions.contains("curieux"))
    }

    @Test func disconnectedAvatarsKeepTheirRestingIdentity() {
        for runs in [[], [run()], [run(status: "waiting_peer")], [run(status: "completed")]] {
            let value = ActivityStore.present(runs: runs, conversationID: "group", agentID: "a", connected: false,
                                             unread: true, speaking: true)
            #expect(value.animation == "idle")
            #expect(value.label == "Disconnected; remote activity unknown")
            #expect(!value.typing)
            #expect(!value.working)
        }
    }

    @Test func peerDispatchIsLiveScopedAndSurvivesSameRunUpdates() {
        let store = ActivityStore()
        var source = run()
        store.restore([source])
        let delivery = PeerDelivery(id: "delivery", sourceAgentID: "a", targetAgentID: "b", conversationID: "group",
                                    parentRunID: source.id, intent: "request", status: "queued", createdAt: source.createdAt)
        let event = Event(seq: 2, conversationID: "group", type: "peer.delivery", createdAt: source.createdAt, delivery: delivery)
        store.receive(event, live: true)
        let cue = store.view(conversationID: "group", agentID: "a")
        #expect(cue.animation == "comet")
        #expect(!cue.typing)
        #expect(store.view(conversationID: "group", agentID: "b").animation == "idle")
        #expect(store.view(conversationID: "private", agentID: "a").animation == "idle")
        source.seq = 3
        source.activity = "generating"
        store.receive(Event(seq: 3, conversationID: "group", type: "tool.done", createdAt: source.createdAt, run: source), live: true)
        #expect(store.view(conversationID: "group", agentID: "a") == cue)
        store.receive(event, live: true)
        #expect(store.view(conversationID: "group", agentID: "a") == cue)
        let next = run("next", seq: 4, created: "2026-10-09T12:01:00Z")
        store.receive(Event(seq: 4, conversationID: "group", type: "run.update", createdAt: next.createdAt, run: next), live: true)
        #expect(store.view(conversationID: "group", agentID: "a").animation == "thinking")
        let restored = ActivityStore()
        restored.restore([source])
        restored.receive(event, live: false)
        restored.receive(event, live: true)
        #expect(restored.cues.isEmpty)
        var receipt = delivery
        receipt.id = "receipt-only"
        receipt.status = "completed"
        restored.receive(Event(seq: 5, conversationID: "group", type: "peer.delivery", createdAt: source.createdAt, delivery: receipt), live: true)
        #expect(restored.cues.isEmpty)
    }

    @Test func waitingIsDescriptiveAndDoesNotFreezeNewWork() {
        var waiting = run("question", status: "completed")
        waiting.outcome = "waiting_input"
        let view = ActivityStore.present(runs: [waiting], conversationID: "group", agentID: "a")
        #expect(view.animation == "alert")
        #expect(!view.typing && !view.working)
        let next = run("independent", seq: 2, created: "2026-10-09T12:01:00Z")
        #expect(ActivityStore.present(runs: [waiting, next], conversationID: "group", agentID: "a").animation == "thinking")
    }

    @Test func artifactsPreemptDispatchButHandoffsDoNotReplayComets() {
        for outcome in ["handoff", "artifact"] {
            let store = ActivityStore()
            var source = run()
            store.restore([source])
            let delivery = PeerDelivery(id: "delivery", sourceAgentID: "a", targetAgentID: "b", conversationID: "group",
                                        parentRunID: source.id, intent: "request", status: "queued")
            store.receive(Event(seq: 2, conversationID: "group", type: "peer.delivery", delivery: delivery), live: true)
            let cueID = store.view(conversationID: "group", agentID: "a").cueID
            source.status = "completed"
            source.outcome = outcome
            source.seq = 3
            store.receive(Event(seq: 3, conversationID: "group", type: "run.done", run: source), live: true)
            let view = store.view(conversationID: "group", agentID: "a")
            if outcome == "handoff" {
                #expect(view.animation == "comet" && view.cueID == cueID)
            } else {
                #expect(view.animation == "burst" && view.cueID != cueID)
            }
        }
    }

    @Test func liveNoticeAnimatesButRestoredUnreadIsStatic() {
        let store = ActivityStore()
        var notice = run("notice", status: "completed")
        notice.outcome = "notice"
        store.receive(Event(seq: 1, conversationID: "group", type: "run.done", createdAt: notice.createdAt, run: notice), live: true)
        #expect(store.view(conversationID: "group", agentID: "a").cueID != nil)
        #expect(ActivityStore.present(runs: [notice], conversationID: "group", agentID: "a", unread: true).cueID == nil)
    }

    @Test func introductionNeverRearmsOrMasksToolWork() {
        let store = ActivityStore()
        var intro = run("intro", trigger: "created")
        store.receive(Event(seq: 1, conversationID: "group", type: "run.update", run: intro), live: true)
        #expect(store.view(conversationID: "group", agentID: "a").animation == "wide")
        intro.seq = 2
        intro.activity = "tool"
        store.receive(Event(seq: 2, conversationID: "group", type: "tool.start", run: intro), live: true)
        #expect(store.view(conversationID: "group", agentID: "a").animation == "swirl")
        intro.seq = 3
        intro.activity = "generating"
        store.receive(Event(seq: 3, conversationID: "group", type: "run.update", run: intro), live: true)
        #expect(store.view(conversationID: "group", agentID: "a").animation == "thinking")
        let restored = ActivityStore()
        restored.restore([intro])
        intro.seq = 4
        restored.receive(Event(seq: 4, conversationID: "group", type: "run.update", run: intro), live: true)
        #expect(restored.cues.isEmpty)
        store.retain([])
        #expect(store.cues.isEmpty)
    }

    @Test func replayNeverCelebratesAndCuesDeduplicate() {
        let store = ActivityStore()
        var finished = run(status: "completed", seq: 5)
        finished.outcome = "artifact"
        store.receive(Event(seq: 5, conversationID: "group", type: "run.done", createdAt: finished.updatedAt, run: finished), live: false)
        #expect(store.view(conversationID: "group", agentID: "a").animation == "idle")
        #expect(store.cues.isEmpty)
        finished.seq = 6
        store.receive(Event(seq: 6, conversationID: "group", type: "run.done", createdAt: finished.updatedAt, run: finished), live: true)
        #expect(store.view(conversationID: "group", agentID: "a").animation == "burst")
        let count = store.cues.count
        store.receive(Event(seq: 6, conversationID: "group", type: "run.done", createdAt: finished.updatedAt, run: finished), live: true)
        #expect(store.cues.count == count)
        let next = run("next", seq: 7, created: "2026-10-09T12:01:00Z")
        store.receive(Event(seq: 7, conversationID: "group", type: "run.update", createdAt: next.updatedAt, run: next), live: true)
        #expect(store.view(conversationID: "group", agentID: "a").animation == "thinking")
        finished.seq = 8
        store.receive(Event(seq: 8, conversationID: "group", type: "run.done", createdAt: finished.updatedAt, run: finished), live: true)
        #expect(store.view(conversationID: "group", agentID: "a").animation == "thinking")
    }

    @Test func quietReviewsNeverPromiseAReply() {
        let store = ActivityStore()
        var review = run("review", trigger: "review")
        store.receive(Event(seq: 1, conversationID: "group", type: "run.update", createdAt: review.updatedAt, run: review), live: true)
        #expect(!store.view(conversationID: "group", agentID: "a").typing)
        review.status = "completed"
        review.outcome = "quiet"
        review.seq = 2
        store.receive(Event(seq: 2, conversationID: "group", type: "run.done", createdAt: review.updatedAt, run: review), live: true)
        #expect(store.cues.isEmpty)
        #expect(store.view(conversationID: "group", agentID: "a").animation == "idle")
        review.id = "notice"
        review.outcome = "notice"
        review.seq = 3
        store.receive(Event(seq: 3, conversationID: "group", type: "run.done", createdAt: review.updatedAt, run: review), live: true)
        #expect(store.view(conversationID: "group", agentID: "a").animation == "notify")
    }

    @Test func allTransientMappingsAndIsolation() {
        let store = ActivityStore()
        for (index, outcome, animation) in [(1, "reply", "wink"), (2, "handoff", "comet"), (3, "artifact", "burst")] {
            var done = run("\(index)", status: "completed", seq: Int64(index))
            done.outcome = outcome
            store.receive(Event(seq: Int64(index), conversationID: "group", type: "run.done", createdAt: done.updatedAt, run: done), live: true)
            #expect(store.view(conversationID: "group", agentID: "a").animation == animation)
            #expect(store.view(conversationID: "private", agentID: "a").animation == "idle")
            #expect(store.view(conversationID: "group", agentID: "b").animation == "idle")
        }
        let intro = run("intro", trigger: "created", seq: 4)
        store.receive(Event(seq: 4, conversationID: "group", type: "run.update", createdAt: intro.updatedAt, run: intro), live: true)
        #expect(store.view(conversationID: "group", agentID: "a").animation == "wide")
    }

    @Test func cacheRestoresActivityAndRejectsStaleRegistry() async throws {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).appendingPathComponent("db")
        let cache = try await CacheWork.open(url)
        let agent = Agent(id: "a", name: "A")
        let thread = Thread(id: "group", agentID: "a", title: "Group", agentIDs: ["a"])
        try await cache.offMain { try $0.save(agents: [agent], threads: [thread]) }
        let tool = run(activity: "tool", seq: 1)
        let event = Event(seq: 1, conversationID: "group", type: "tool.start", createdAt: tool.updatedAt, run: tool)
        try await cache.offMain { try $0.apply(event) }
        try await cache.offMain { try $0.apply(event) }
        var stale = thread
        stale.runs = [run(status: "queued", seq: 0)]
        let queued = stale
        try await cache.offMain { try $0.save(agents: [agent], threads: [queued]) }
        let reopened = try await CacheWork.open(url)
        #expect(try await reopened.offMain { try $0.runs(threadID: "group") }.first?.activity == "tool")
        #expect(try await reopened.offMain { try $0.cursor(threadID: "group") } == 1)
        #expect(try await reopened.offMain { try $0.threads() }.first?.unreadCount == 0)
        try await reopened.offMain { try $0.save(agents: [], threads: []) }
        #expect(try await reopened.offMain { try $0.runs(threadID: "group") }.isEmpty)
    }

    @Test func authoritativeSnapshotsRemoveStaleWorkButKeepNewerEvents() async throws {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).appendingPathComponent("db")
        let cache = try await CacheWork.open(url)
        let agent = Agent(id: "a", name: "A")
        var thread = Thread(id: "group", agentID: "a", title: "Group", agentIDs: ["a"])
        let seeded = thread
        try await cache.offMain { try $0.save(agents: [agent], threads: [seeded]) }
        let activity = ActivityStore()
        let old = run("old", seq: 1)
        try await cache.offMain { try $0.apply(Event(seq: 1, conversationID: "group", type: "run.update", createdAt: old.updatedAt, run: old)) }
        activity.restore([old])
        let done = run("done", status: "completed", seq: 2, created: "2026-10-09T12:01:00Z")
        thread.lastSeq = 2
        thread.runs = [done]
        let finished = thread
        try await cache.offMain { try $0.reconcile(finished) }
        activity.reconcile(finished)
        #expect(try await cache.offMain { try $0.runs(threadID: "group") }.map(\.id) == ["done"])
        #expect(activity.view(conversationID: "group", agentID: "a").animation == "idle")
        try await cache.offMain { try $0.apply(Event(seq: 2, conversationID: "group", type: "run.done", createdAt: done.updatedAt, run: done)) }
        let next = run("next", activity: "tool", seq: 3, created: "2026-10-09T12:02:00Z")
        let live = Event(seq: 3, conversationID: "group", type: "run.update", createdAt: next.updatedAt, run: next)
        try await cache.offMain { try $0.apply(live) }
        activity.receive(live, live: true)
        let current = thread
        try await cache.offMain { try $0.reconcile(current) }
        activity.reconcile(current)
        #expect(activity.view(conversationID: "group", agentID: "a").animation == "swirl")
        #expect(try await cache.offMain { try $0.runs(threadID: "group") }.contains { $0.id == "next" })
        thread.lastSeq = 4
        thread.runs = []
        let cleared = thread
        try await cache.offMain { try $0.reconcile(cleared) }
        activity.reconcile(cleared)
        #expect(try await cache.offMain { try $0.threads() }.first?.runs.isEmpty == true)
        #expect(activity.view(conversationID: "group", agentID: "a").animation == "idle")
    }
}
