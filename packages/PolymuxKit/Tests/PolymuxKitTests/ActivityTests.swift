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

    @Test func supersededFailuresClearBothSurfaces() {
        for status in [RunStatus.queued, RunStatus.running, RunStatus.completed, RunStatus.cancelled] {
            for conversation in ["group", "other"] {
                let store = ActivityStore()
                var failed = run("failed", status: RunStatus.failed)
                failed.error = WireError(code: "provider", message: "Provider unavailable")
                let message = Message(runID: failed.id, error: failed.error, id: "reply", threadID: "group",
                                      role: MessageRole.assistant, createdAt: failed.createdAt,
                                      status: MessageStatus.error, agentID: "a")
                store.restore([failed])
                #expect(store.issue(for: message) == failed.error)
                #expect(store.view(conversationID: "group", agentID: "a").animation == "exclaim")
                var next = run("next", status: status, seq: 2, created: "2026-10-09T12:01:00Z")
                next.conversationID = conversation
                next.outcome = "quiet"
                store.restore([next])
                #expect(store.issue(for: message) == nil, "\(conversation)/\(status)")
                #expect(store.view(conversationID: "group", agentID: "a").animation != "exclaim")
                #expect(store.snapshots[failed.id] == failed)
                // A delayed update to the older failure must not make it current again.
                failed.seq = 100
                failed.updatedAt = "2026-10-09T12:02:00Z"
                store.receive(Event(conversationID: "group", type: EventType.runUpdate, run: failed), live: true)
                #expect(store.issue(for: message) == nil)
                #expect(store.view(conversationID: "group", agentID: "a").animation != "exclaim")
            }
        }
    }

    @Test func recoverySurvivesReloadAndLaterFailuresStillWarn() {
        let failed = run("failed", status: RunStatus.failed)
        var recovered = run("recovered", status: RunStatus.completed, created: "2026-10-09T12:01:00Z")
        recovered.conversationID = "other"
        let store = ActivityStore()
        store.restore([failed, recovered])
        let message = Message(runID: failed.id, error: WireError(code: "provider", message: "Old failure"),
                              id: "old-reply", threadID: "group", role: MessageRole.assistant,
                              createdAt: failed.createdAt, status: MessageStatus.error, agentID: "a")
        #expect(store.issue(for: message) == nil)
        #expect(store.view(conversationID: "group", agentID: "a").animation == "idle")
        var next = run("failed-again", status: RunStatus.failed, seq: 3, created: "2026-10-09T12:02:00Z")
        next.error = WireError(code: "provider", message: "New failure")
        let nextMessage = Message(runID: next.id, id: "new-reply", threadID: "group", role: MessageRole.assistant,
                                  createdAt: next.createdAt, status: MessageStatus.error)
        store.receive(Event(conversationID: "group", type: EventType.runDone, run: next), live: true)
        #expect(store.issue(for: message) == nil)
        #expect(store.issue(for: nextMessage) == next.error)
        #expect(store.view(conversationID: "group", agentID: "a").animation == "exclaim")
    }

    @Test func answeredChoicesYieldToNewerRuns() {
        let waiting = run("choice", status: RunStatus.waitingInput)
        for status in [RunStatus.running, RunStatus.waitingPeer, RunStatus.completed, RunStatus.cancelled] {
            let store = ActivityStore()
            store.restore([waiting])
            #expect(store.view(conversationID: "group", agentID: "a").needsAttention)
            let next = run("answer", status: status, seq: 2, created: "2026-10-09T12:01:00Z")
            store.restore([next])
            #expect(!store.view(conversationID: "group", agentID: "a").needsAttention, "\(status)")
            store.restore([waiting, next])
            #expect(!store.view(conversationID: "group", agentID: "a").needsAttention, "restored \(status)")
        }
    }

    @Test func currentFailuresAndApprovalsRemainVisible() {
        let store = ActivityStore()
        var failed = run("failed", status: RunStatus.failed, created: "2026-10-09T12:02:00Z")
        failed.error = WireError(code: "provider", message: "Provider unavailable")
        let message = Message(runID: failed.id, error: failed.error, id: "reply", threadID: "group",
                              role: MessageRole.assistant, createdAt: failed.createdAt,
                              status: MessageStatus.error, agentID: "a")
        var other = run("other", status: RunStatus.completed, created: "2026-10-09T12:03:00Z")
        other.agentID = "b"
        store.restore([run("previous", status: RunStatus.completed), failed, other])
        #expect(store.issue(for: message) == failed.error)
        #expect(store.view(conversationID: "group", agentID: "a").animation == "exclaim")
        var pending = run("approval", status: RunStatus.waitingInput)
        store.restore([pending])
        #expect(store.view(conversationID: "group", agentID: "a").needsAttention)
        pending.status = RunStatus.completed
        pending.outcome = "waiting_input"
        pending.conversationID = "approval-chat"
        store.restore([pending])
        #expect(store.view(conversationID: "approval-chat", agentID: "a").needsAttention)
    }

    @Test func messageOnlyErrorsAlsoYieldToNewerRuns() {
        let store = ActivityStore()
        let message = Message(error: WireError(code: "provider", message: "Provider unavailable"),
                              id: "reply", threadID: "group", role: MessageRole.assistant,
                              createdAt: "2026-10-09T12:00:00Z", status: MessageStatus.error, agentID: "a")
        #expect(store.issue(for: message) == message.error)
        store.restore([run("recovered", status: RunStatus.completed, created: "2026-10-09T12:01:00Z")])
        #expect(store.issue(for: message) == nil)
    }

    @Test func onlyLiveActivityAnimates() {
        for status in ["running", "resuming", "waiting_peer", "queued", "waiting_input", "failed", "interrupted", "completed", "cancelled"] {
            let value = ActivityStore.present(runs: [run(status: status)], conversationID: "group", agentID: "a")
            #expect(value.animating == ["running", "resuming", "waiting_peer"].contains(status), "\(status)")
        }
        #expect(!ActivityStore.present(runs: [], conversationID: "group", agentID: "a", unread: true).animating)
        #expect(ActivityStore.present(runs: [], conversationID: "group", agentID: "a", speaking: true).animating)
        #expect(ActivityStore.present(runs: [], conversationID: "group", agentID: "a", listening: true).animating)
        #expect(!ActivityStore.present(runs: [run()], conversationID: "group", agentID: "a",
                                      connected: false, speaking: true, listening: true).animating)
        let cue = ActivityView(animation: "burst", cueID: "r:done")
        let done = ActivityStore.present(runs: [], conversationID: "group", agentID: "a", cue: cue)
        #expect(done.mood == "celebrate" && !done.animating)
    }

    @Test func toolsUseStructuredNamesForScenarioMoods() {
        for (name, mood) in [("web_search", "searching"), ("read_file", "reading"),
                             ("apply_patch", "writing"), ("message_agent", "sending"),
                             ("bash", "working")] {
            var probe = run(activity: "tool")
            probe.tools = [ToolActivity(id: "old", name: "web_search", status: "completed"),
                           ToolActivity(id: "current", name: name, status: "running")]
            probe.toolID = "current"
            let value = ActivityStore.present(runs: [probe], conversationID: "group", agentID: "a")
            #expect(value.mood == mood)
            #expect(value.working && !value.typing)
        }
    }

    @Test func streamingAndListeningAreLiveAndConversationScoped() {
        let store = ActivityStore()
        var probe = run()
        store.restore([probe])
        store.setListening(true, conversationID: "private", agentID: "a")
        #expect(store.view(conversationID: "private", agentID: "a").mood == "listening")
        #expect(store.view(conversationID: "group", agentID: "a").mood == "thinking")
        let delta = Event(seq: 2, conversationID: "group", type: "message.delta", agentID: "a", text: "Hello")
        store.receive(delta, live: false)
        #expect(store.view(conversationID: "group", agentID: "a").mood == "thinking")
        store.receive(delta, live: true)
        #expect(store.view(conversationID: "group", agentID: "a").mood == "streaming")
        #expect(store.view(conversationID: "group", agentID: "b").mood == "idle")
        probe.activity = "tool"; probe.seq = 3
        store.receive(Event(seq: 3, conversationID: "group", type: "tool.start", run: probe), live: true)
        #expect(store.view(conversationID: "group", agentID: "a").mood == "working")
        probe.activity = "generating"; probe.seq = 4
        store.receive(Event(seq: 4, conversationID: "group", type: "run.update", run: probe), live: true)
        #expect(store.view(conversationID: "group", agentID: "a").mood == "thinking")
        store.retain([])
        #expect(store.view(conversationID: "private", agentID: "a").mood == "idle")
    }

    @Test func contextualMoodsPreserveRunPriorityAndRestingIdentity() {
        for (status, mood) in [("waiting_input", "blocked"), ("failed", "failed"),
                               ("interrupted", "interrupted"), ("waiting_peer", "receiving")] {
            let view = ActivityStore.present(runs: [run(status: status)], conversationID: "group", agentID: "a", listening: true)
            #expect(view.mood == mood)
        }
        #expect(ActivityStore.present(runs: [], conversationID: "group", agentID: "a", connected: false, listening: true).mood == nil)
        let store = ActivityStore()
        var probe = run(status: "completed")
        probe.outcome = "artifact"
        store.receive(Event(seq: 1, conversationID: "group", type: "run.done", run: probe), live: true)
        #expect(store.view(conversationID: "group", agentID: "a").mood == "celebrate")
        #expect(ActivityStore.present(runs: [probe], conversationID: "group", agentID: "a").mood == "idle")
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
        #expect(ActivityStore.present(runs: [], conversationID: "group", agentID: "a", unread: true).animation == "idle")
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
        #expect(store.handoffs.count == 1)
        #expect(store.handoffs.first?.source == "a")
        #expect(store.handoffs.first?.target == "b")
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
        #expect(store.handoffs.count == 1)
        let next = run("next", seq: 4, created: "2026-10-09T12:01:00Z")
        store.receive(Event(seq: 4, conversationID: "group", type: "run.update", createdAt: next.createdAt, run: next), live: true)
        #expect(store.view(conversationID: "group", agentID: "a").animation == "thinking")
        let restored = ActivityStore()
        restored.restore([source])
        restored.receive(event, live: false)
        restored.receive(event, live: true)
        #expect(restored.cues.isEmpty)
        #expect(restored.handoffs.isEmpty)
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

    @Test func attentionSurvivesReadingButClearsWhenWaitingEnds() {
        for status in [RunStatus.waitingInput, RunStatus.completed] {
            var waiting = run("question", status: status)
            waiting.outcome = "waiting_input"
            for unread in [true, false] {
                let view = ActivityStore.present(runs: [waiting], conversationID: "group", agentID: "a", unread: unread)
                #expect(view.needsAttention && view.mood == "blocked")
            }
            #expect(!ActivityStore.present(runs: [waiting], conversationID: "group", agentID: "b").needsAttention)
            #expect(!ActivityStore.present(runs: [waiting], conversationID: "private", agentID: "a").needsAttention)
            #expect(ActivityStore.present(runs: [waiting], conversationID: "group", agentID: "a", connected: false).needsAttention)
            let store = ActivityStore()
            store.restore([waiting])
            #expect(store.view(conversationID: "group", agentID: "a").needsAttention)
            waiting.seq += 1
            waiting.status = RunStatus.resuming
            store.receive(Event(seq: waiting.seq, conversationID: "group", type: EventType.runUpdate, run: waiting), live: true)
            #expect(!store.view(conversationID: "group", agentID: "a").needsAttention)
            waiting.seq += 1
            waiting.status = RunStatus.completed
            waiting.outcome = "reply"
            store.receive(Event(seq: waiting.seq, conversationID: "group", type: EventType.runDone, run: waiting), live: true)
            #expect(!store.view(conversationID: "group", agentID: "a", unread: true).needsAttention)
        }
    }

    @Test func markersFollowGrokPriorityWithoutChangingTheAvatar() {
        for status in [RunStatus.queued, RunStatus.running, RunStatus.waitingPeer, RunStatus.failed,
                       RunStatus.interrupted, RunStatus.cancelled, RunStatus.completed] {
            let probe = run(status: status)
            let ready = ActivityStore.present(runs: [probe], conversationID: "group", agentID: "a")
            var unread = ActivityStore.present(runs: [probe], conversationID: "group", agentID: "a", unread: true)
            #expect(ready.marker == nil)
            #expect(unread.marker == .unread)
            unread.marker = nil
            #expect(unread == ready, "unread does not replace \(status)'s face or work")
        }
        let store = ActivityStore()
        var waiting = run(status: RunStatus.waitingInput)
        store.restore([waiting])
        #expect(store.view(conversationID: "group", agentID: "a", unread: true).marker == .attention)
        let offline = store.view(conversationID: "group", agentID: "a", connected: false, unread: true)
        #expect(offline.marker == .attention)
        #expect(offline.mood == nil && !offline.working && offline.animation == "idle")
        waiting.seq += 1
        waiting.status = RunStatus.resuming
        store.restore([waiting])
        #expect(store.view(conversationID: "group", agentID: "a", unread: true).marker == .unread)
        #expect(store.view(conversationID: "group", agentID: "a").marker == nil)
        #expect(store.view(conversationID: "group", agentID: "a", connected: false, unread: true).marker == .unread)
    }

    @Test func groupMarkersUseOneConversationScopedPriority() {
        let store = ActivityStore()
        let waiting = run(status: RunStatus.waitingInput)
        store.restore([waiting])
        #expect(store.marker(conversationID: "group", agentIDs: ["b", "a"], unread: true) == .attention)
        #expect(store.marker(conversationID: "private", agentIDs: ["a"], unread: true) == .unread)
        #expect(store.marker(conversationID: "group", agentIDs: ["b"], unread: false) == nil)
        #expect(ActivityStore.marker(runs: [waiting], conversationID: "group", agentIDs: ["a"], unread: true) == .attention)
        #expect(store.marker(conversationID: "group", agentIDs: ["a"], unread: false) == .attention)
    }

    @Test func readingClearsPinnedAndRowMarkersButNotWaiting() async throws {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).appendingPathComponent("db")
        let cache = try await CacheWork.open(url)
        let sidebar = SidebarStore(client: APIClient(baseURL: URL(string: "http://fixture.invalid")!), cache: cache)
        for pinned in [false, true] {
            for waiting in [false, true] {
                let id = "\(pinned)-\(waiting)"
                var thread = Thread(id: id, agentID: "a", lastSeq: 1, unreadCount: 2, agentIDs: ["a"], pinned: pinned)
                var probe = run(status: RunStatus.waitingInput)
                probe.conversationID = id
                thread.runs = waiting ? [probe] : []
                sidebar.reveal(thread)
                #expect(ActivityStore.marker(runs: thread.runs, conversationID: id, agentIDs: ["a"], unread: true)
                        == (waiting ? .attention : .unread))
                sidebar.clearUnread(id, seq: 1)
                let read = try #require(sidebar.displayed(id))
                #expect(read.pinned == pinned && read.unreadCount == 0)
                #expect(ActivityStore.marker(runs: read.runs, conversationID: id, agentIDs: ["a"], unread: read.unreadCount > 0)
                        == (waiting ? .attention : nil))
            }
        }
    }

    @Test func resumingRunsStayCurrentAndWorking() {
        let resuming = run("crashed", status: RunStatus.resuming, activity: "")
        let later = run("done", status: RunStatus.completed, seq: 2, created: "2026-10-09T12:01:00Z")
        let view = ActivityStore.present(runs: [resuming, later], conversationID: "group", agentID: "a")
        #expect(view.animation == "thinking")
        #expect(view.working)
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

    @Test func liveNoticeAnimatesButUnreadOnlyAddsAMarker() {
        let store = ActivityStore()
        var notice = run("notice", status: "completed")
        notice.outcome = "notice"
        store.receive(Event(seq: 1, conversationID: "group", type: "run.done", createdAt: notice.createdAt, run: notice), live: true)
        #expect(store.view(conversationID: "group", agentID: "a").cueID != nil)
        let live = store.view(conversationID: "group", agentID: "a", unread: true)
        #expect(live.animation == "wink" && live.mood == "done" && live.marker == .unread)
        let unread = ActivityStore.present(runs: [notice], conversationID: "group", agentID: "a", unread: true)
        #expect(unread.cueID == nil && unread.animation == "idle" && unread.mood == "idle")
        #expect(unread.marker == .unread)
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

    @Test func creationSpawnsFromSnapshotsUntilTheReplyActuallyStreams() {
        let store = ActivityStore()
        var intro = run("intro", trigger: "created")
        store.restore([intro])
        let initial = store.view(conversationID: "group", agentID: "a")
        #expect(initial.animation == "wide" && initial.mood == "spawning")
        #expect(initial.typing && initial.working)
        #expect(store.cues.isEmpty)
        #expect(ActivityStore.present(runs: [intro], conversationID: "group", agentID: "a").mood == "spawning")
        intro.seq = 2
        intro.updatedAt = "2026-10-09T12:00:10Z"
        store.restore([intro])
        store.receive(Event(seq: 2, conversationID: "group", type: "run.update", run: intro), live: true)
        #expect(store.view(conversationID: "group", agentID: "a") == initial)
        store.receive(Event(seq: 3, conversationID: "group", type: "message.delta", agentID: "a",
                            text: "Hello", runID: intro.id), live: true)
        #expect(store.view(conversationID: "group", agentID: "a").mood == "streaming")
        store.receive(Event(seq: 4, conversationID: "group", type: "message.done", agentID: "a", runID: intro.id), live: true)
        #expect(store.view(conversationID: "group", agentID: "a").mood == "thinking")
        intro.seq = 5
        store.restore([intro])
        #expect(store.view(conversationID: "group", agentID: "a").mood == "thinking")
        intro.seq = 6
        intro.status = "completed"
        intro.outcome = "reply"
        store.receive(Event(seq: 6, conversationID: "group", type: "run.done", run: intro), live: true)
        #expect(store.view(conversationID: "group", agentID: "a").mood == "done")
    }

    @Test func restoredCreationDoesNotMaskOtherWorkOrReplayOutput() {
        for activity in ["preparing", "generating"] {
            let store = ActivityStore()
            let intro = run("intro", activity: activity, trigger: "created")
            store.receive(Event(seq: 1, conversationID: "group", type: "run.update", run: intro), live: false)
            #expect(store.view(conversationID: "group", agentID: "a").mood == "spawning")
            #expect(store.view(conversationID: "private", agentID: "a").mood == "idle")
            #expect(store.view(conversationID: "group", agentID: "b").mood == "idle")
        }
        let restored = ActivityStore()
        var intro = run("intro", trigger: "created")
        intro.tools = [ToolActivity(id: "finished", name: "read_file", status: "completed")]
        restored.restore([intro])
        #expect(restored.view(conversationID: "group", agentID: "a").mood == "thinking")
        let replayed = ActivityStore()
        intro.tools = nil
        replayed.restore([intro])
        replayed.receive(Event(seq: 2, conversationID: "group", type: "message.delta", agentID: "a",
                               text: "Hello", runID: intro.id), live: false)
        #expect(replayed.view(conversationID: "group", agentID: "a").mood == "thinking")
        #expect(replayed.cues.isEmpty)
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
        #expect(store.view(conversationID: "group", agentID: "a").animation == "wink")
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
