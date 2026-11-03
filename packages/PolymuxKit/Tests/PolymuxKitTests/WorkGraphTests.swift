import Foundation
import Testing
@testable import PolymuxKit

@Suite
struct WorkGraphTests {
    private func agent(_ id: String, _ name: String) -> Agent { Agent(id: id, name: name, active: true) }

    private func run(_ id: String, agent: String, chat: String, trigger: String = RunTrigger.message, status: String = RunStatus.completed,
                     parent: String? = nil, tools: [ToolActivity]? = nil) -> Run {
        Run(jobID: "j", id: id, conversationID: chat, trigger: trigger, parentRunID: parent, tools: tools, attempt: 1, seq: 1,
            createdAt: "2026-11-01T00:00:00Z", updatedAt: "2026-11-01T00:00:00Z", activity: "", agentID: agent, status: status, summary: "")
    }

    private func delivery(_ id: String, from: String, to: String, chat: String, intent: String = PeerDeliveryIntent.request,
                          status: String = PeerDeliveryStatus.completed, parent: String = "p") -> PeerDelivery {
        PeerDelivery(id: id, sourceAgentID: from, targetAgentID: to, conversationID: chat, parentRunID: parent, intent: intent, status: status,
                     createdAt: "2026-11-01T00:00:00Z")
    }

    private func thread(_ id: String, agent: String, members: [String] = []) -> PolymuxKit.Thread {
        PolymuxKit.Thread(id: id, agentID: agent, title: id, agentIDs: members)
    }

    private func tool(_ id: String, _ name: String, summary: String? = nil, status: String = ToolActivityStatus.completed) -> ToolActivity {
        ToolActivity(id: id, runID: "r", name: name, status: status, summary: summary)
    }

    private var agents: [Agent] { [agent("linus", "Linus"), agent("claude", "Claude"), agent("nova", "Nova")] }

    private func node(_ graph: WorkGraph, _ id: String) -> WorkNode? { graph.nodes.first { $0.id == id } }

    @Test func globalShowsEveryBotAndYou() {
        let graph = WorkGraphBuilder(agents: agents, threads: []).build()
        #expect(Set(graph.nodes.map(\.id)) == ["you", "agent:linus", "agent:claude", "agent:nova"])
    }

    @Test func aTaskFromOneBotToAnotherIsAWireAndADepth() throws {
        let activity = WorkActivity(build: "b", runs: [], deliveries: [delivery("d", from: "linus", to: "claude", chat: "linus", status: PeerDeliveryStatus.delivered)])
        let graph = WorkGraphBuilder(agents: agents, threads: [], activity: activity).build()
        #expect(node(graph, "agent:claude")?.parent == "agent:linus")
        let wire = try #require(graph.wires.first { $0.id == "handoff:linus>claude" })
        #expect(wire.label == "Task" && wire.state == WorkWireState.active)
    }

    @Test func aResultDoesNotMakeAParent() {
        let activity = WorkActivity(build: "b", runs: [], deliveries: [delivery("d", from: "claude", to: "linus", chat: "linus", intent: PeerDeliveryIntent.result)])
        let graph = WorkGraphBuilder(agents: agents, threads: [], activity: activity).build()
        #expect(node(graph, "agent:linus")?.parent == nil)
    }

    @Test func bouncingWorkCannotMakeACycle() {
        let activity = WorkActivity(build: "b", runs: [], deliveries: [
            delivery("newer", from: "claude", to: "linus", chat: "c"), delivery("older", from: "linus", to: "claude", chat: "c")])
        let graph = WorkGraphBuilder(agents: agents, threads: [], activity: activity).build()
        #expect(node(graph, "agent:claude")?.parent == "agent:linus")
        #expect(node(graph, "agent:linus")?.parent == nil)
    }

    @Test func toolsGroupIntoAppsWithTheirCallsBeneath() throws {
        let tools = [tool("1", "bash", summary: "git status", status: ToolActivityStatus.running), tool("2", "read", summary: "a.txt"), tool("3", "message_agent")]
        let activity = WorkActivity(build: "b", runs: [run("r", agent: "linus", chat: "linus", tools: tools)], deliveries: [])
        let graph = WorkGraphBuilder(agents: agents, threads: [], activity: activity).build()
        let terminal = try #require(node(graph, "app:linus:terminal"))
        #expect(terminal.parent == "agent:linus" && terminal.state == WorkNodeState.working)
        #expect(node(graph, "tool:1")?.parent == "app:linus:terminal" && node(graph, "tool:1")?.title == "git status")
        #expect(node(graph, "app:linus:files") != nil)
        #expect(node(graph, "tool:3") == nil, "handoffs already show as wires")
    }

    @Test func outsideWorkersComeFromTheTerminal() throws {
        let tools = [tool("1", "bash", summary: "cd repo && claude -p 'Fix the bug'", status: ToolActivityStatus.running), tool("2", "bash", summary: "/usr/local/bin/codex exec 'Review'"), tool("3", "bash", summary: "grep claude -p notes.txt | wc")]
        let activity = WorkActivity(build: "b", runs: [run("r", agent: "linus", chat: "linus", tools: tools)], deliveries: [])
        let graph = WorkGraphBuilder(agents: agents, threads: [], activity: activity).build()
        let claude = try #require(node(graph, "worker:1"))
        #expect(claude.title == "Claude Code" && claude.parent == "agent:linus" && claude.state == WorkNodeState.working)
        #expect(node(graph, "worker:2")?.title == "Codex")
        let start = graph.wires.first { $0.id == "start:1" }
        #expect(start?.state == WorkWireState.active && start?.label == "Task")
    }

    @Test func aDelegatedRunToTheSameBotIsASubagent() throws {
        let runs = [run("child", agent: "linus", chat: "other", trigger: RunTrigger.child, status: RunStatus.running, parent: "main"), run("main", agent: "linus", chat: "linus")]
        let graph = WorkGraphBuilder(agents: agents, threads: [], activity: WorkActivity(build: "b", runs: runs, deliveries: [])).build()
        #expect(node(graph, "run:child")?.parent == "agent:linus")
        #expect(graph.wires.first { $0.id == "delegate:child" }?.state == WorkWireState.active)
    }

    @Test func aChatKeepsItsOwnWorkAndWhatItStarted() {
        let runs = [run("mine", agent: "linus", chat: "linus"), run("child", agent: "claude", chat: "other", trigger: RunTrigger.child, parent: "mine"), run("theirs", agent: "nova", chat: "nova")]
        let activity = WorkActivity(build: "b", runs: runs, deliveries: [])
        let graph = WorkGraphBuilder(agents: agents, threads: [thread("linus", agent: "linus")], activity: activity, chat: "linus").build()
        #expect(Set(graph.nodes.map(\.id)) == ["you", "agent:linus", "agent:claude"])
    }

    @Test func aChatReachesTheEnvironmentThroughReportedLinks() {
        let reported = WorkGraph(revision: 1, nodes: [
            WorkNode(id: "repo:1", title: "polymux", parent: WorkGraphBuilder.serverDevice, kind: "repo"),
            WorkNode(id: "repo:2", title: "other", parent: WorkGraphBuilder.serverDevice, kind: "repo"),
            WorkNode(id: "github:a/b", title: "a/b", kind: "github"),
            WorkNode(id: "github:a/b#1", title: "Pull", parent: "github:a/b", kind: "pull"),
            WorkNode(id: "safari", title: "Safari", parent: "agent:linus", chats: ["linus"]),
        ], wires: [
            WorkWire(id: "repo>github", from: "repo:1", to: "github:a/b"),
            WorkWire(id: "linus>repo", from: "agent:linus", to: "repo:1", chats: ["linus"]),
            WorkWire(id: "nova>repo", from: "agent:nova", to: "repo:2", chats: ["nova"]),
        ])
        let network = NetworkInfo.fixture()
        let graph = WorkGraphBuilder(agents: agents, threads: [thread("linus", agent: "linus")], reported: reported, network: network, chat: "linus").build()
        let ids = Set(graph.nodes.map(\.id))
        #expect(ids.isSuperset(of: ["agent:linus", "repo:1", "device:server", "github:a/b", "github:a/b#1", "safari"]))
        #expect(!ids.contains("repo:2") && !ids.contains("agent:nova"))
        let known = graph.wires.map(\.id)
        #expect(known.contains("linus>repo") && known.contains("repo>github") && !known.contains("nova>repo"))
    }

    @Test func wiresToMissingNodesAreDropped() {
        let reported = WorkGraph(revision: 1, nodes: [], wires: [WorkWire(id: "w", from: "agent:gone", to: "you")])
        #expect(WorkGraphBuilder(agents: agents, threads: [], reported: reported).build().wires.isEmpty)
    }
}

extension NetworkInfo {
    static func fixture() -> NetworkInfo {
        let json = #"{"server":{"name":"cvmini","build":"abc","platform":"darwin","role":"server"},"devices":[{"id":"phone","name":"iPhone"},{"id":"host","name":"cvmini"}],"host":"host","current":"phone","online":["phone"]}"#
        return try! JSONDecoder().decode(NetworkInfo.self, from: Data(json.utf8))
    }
}
