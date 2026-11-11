import Foundation

/// Turns Polymux's activity into graph nodes and wires, for every chat or for one.
public struct WorkGraphBuilder {
    public var agents: [Agent]
    public var threads: [Thread]
    public var activity: WorkActivity?
    public var reported: WorkGraph?
    public var network: NetworkInfo?
    /// The chat to show, or nil for everything.
    public var chat: String?

    public static let you = "you"
    public static let serverDevice = "device:server"
    public static let server = "server:this"
    private static let callLimit = 6

    public init(agents: [Agent], threads: [Thread], activity: WorkActivity? = nil, reported: WorkGraph? = nil,
                network: NetworkInfo? = nil, chat: String? = nil) {
        self.agents = agents
        self.threads = threads
        self.activity = activity
        self.reported = reported
        self.network = network
        self.chat = chat
    }

    public static func agentNode(_ id: String) -> String { "agent:" + id }

    public func build() -> WorkGraph {
        var graph = Draft()
        let runs = scopedRuns()
        let deliveries = scopedDeliveries(runs)
        addPeople(&graph, runs: runs, deliveries: deliveries)
        addHandoffs(&graph, deliveries: deliveries)
        addDelegation(&graph, runs: runs)
        addTools(&graph, runs: runs)
        var environment = Draft()
        addDevices(&environment)
        for node in reported?.nodes ?? [] { environment.add(node) }
        for wire in reported?.wires ?? [] { environment.add(wire) }
        return merged(graph, environment)
    }

    private struct Draft {
        var nodes: [WorkNode] = []
        var wires: [WorkWire] = []
        var ids = Set<String>()
        var wireIDs = Set<String>()

        mutating func add(_ node: WorkNode) {
            guard ids.insert(node.id).inserted else { return }
            nodes.append(node)
        }

        mutating func add(_ wire: WorkWire) {
            guard wireIDs.insert(wire.id).inserted else { return }
            wires.append(wire)
        }
    }

    // MARK: Scope

    private func scopedRuns() -> [Run] {
        let all = activity?.runs ?? []
        guard let chat else { return all }
        let tasks = Set(threads.filter { $0.isTask && $0.agentID == chat }.map(\.id))
        let byID = Dictionary(all.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        func inScope(_ run: Run) -> Bool {
            var current: Run? = run
            var seen = Set<String>()
            while let next = current, seen.insert(next.id).inserted {
                if next.conversationID == chat || tasks.contains(next.conversationID) { return true }
                current = next.parentRunID.flatMap { byID[$0] }
            }
            return false
        }
        return all.filter(inScope)
    }

    private func scopedDeliveries(_ runs: [Run]) -> [PeerDelivery] {
        let all = activity?.deliveries ?? []
        guard let chat else { return all }
        let inside = Set(runs.map(\.id))
        return all.filter { $0.conversationID == chat || inside.contains($0.parentRunID) }
    }

    // MARK: People and bots

    private func addPeople(_ graph: inout Draft, runs: [Run], deliveries: [PeerDelivery]) {
        var shown: [String] = []
        if let chat, let thread = threads.first(where: { $0.id == chat }) {
            shown = thread.agentIDs.isEmpty ? [thread.agentID] : thread.agentIDs
        } else if chat == nil {
            shown = agents.map(\.id)
        }
        shown += runs.map(\.agentID) + deliveries.flatMap { [$0.sourceAgentID, $0.targetAgentID] }
        let parents = delegators(runs: runs, deliveries: deliveries)
        var told = false
        for id in shown where !graph.ids.contains(Self.agentNode(id)) {
            guard let agent = agents.first(where: { $0.id == id }) else { continue }
            let state = state(of: id, runs: runs)
            var details: [WorkDetail] = []
            if !agent.role.isEmpty { details.append(WorkDetail(label: "Role", value: agent.role)) }
            if !agent.model.isEmpty { details.append(WorkDetail(label: "Model", value: agent.model)) }
            graph.add(WorkNode(id: Self.agentNode(id), title: agent.name, parent: parents[id].map(Self.agentNode),
                               kind: WorkKind.agent, subtitle: caption(agent, state: state, runs: runs), agentID: id, state: state,
                               details: details.isEmpty ? nil : details))
            let asked = runs.filter { $0.agentID == id && $0.trigger == RunTrigger.message }
            guard let latest = asked.first else { continue }
            told = true
            graph.add(WorkWire(id: "you>\(id)", from: Self.you, to: Self.agentNode(id), state: Self.running(latest.status) ? WorkWireState.active : "",
                               label: "Messages", carries: WorkKind.message, details: [WorkDetail(label: "Messages", value: String(asked.count))]))
        }
        if told || chat == nil {
            graph.add(WorkNode(id: Self.you, title: "You", kind: WorkKind.person))
        }
    }

    /// What a bot is doing while it works, and what it's for the rest of the time.
    private func caption(_ agent: Agent, state: String, runs: [Run]) -> String {
        if state == WorkNodeState.working, let summary = runs.first(where: { $0.agentID == agent.id })?.summary, !summary.isEmpty { return summary }
        return agent.role
    }

    /// The mark an outside agent wears, from whichever of its names is one the map knows.
    static func mark(_ external: ExternalAgent) -> String {
        let words = [external.agent, external.name, external.command].compactMap { $0 }.joined(separator: " ").lowercased()
        let known = [("claude", "sparkle"), ("codex", "curlybraces"), ("gemini", "diamond"), ("opencode", "chevron.left.forwardslash.chevron.right"),
                     ("goose", "bird"), ("copilot", "airplane"), ("cursor", "cursorarrow")]
        return known.first { words.contains($0.0) }?.1 ?? "sparkle"
    }

    /// Who first handed a bot its work, which places it one level under them.
    private func delegators(runs: [Run], deliveries: [PeerDelivery]) -> [String: String] {
        var parents: [String: String] = [:]
        func assign(_ child: String, to parent: String) {
            guard child != parent, parents[child] == nil else { return }
            var current: String? = parent
            while let next = current {
                if next == child { return }
                current = parents[next]
            }
            parents[child] = parent
        }
        let byID = Dictionary(runs.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        for run in runs.reversed() where run.trigger == RunTrigger.child {
            if let parent = run.parentRunID.flatMap({ byID[$0] }) { assign(run.agentID, to: parent.agentID) }
        }
        let asks = [PeerDeliveryIntent.request, PeerDeliveryIntent.handoff, PeerDeliveryIntent.question]
        for delivery in deliveries.reversed() where asks.contains(delivery.intent) {
            assign(delivery.targetAgentID, to: delivery.sourceAgentID)
        }
        return parents
    }

    private static func running(_ status: String) -> Bool {
        [RunStatus.queued, RunStatus.running, RunStatus.waitingPeer, RunStatus.resuming].contains(status)
    }

    private func state(of agent: String, runs: [Run]) -> String {
        guard let latest = runs.first(where: { $0.agentID == agent }) else { return WorkNodeState.idle }
        if latest.status == RunStatus.waitingInput { return WorkNodeState.waiting }
        if Self.running(latest.status) { return WorkNodeState.working }
        return latest.status == RunStatus.failed ? WorkNodeState.failed : WorkNodeState.idle
    }

    // MARK: Handoffs

    private func addHandoffs(_ graph: inout Draft, deliveries: [PeerDelivery]) {
        var order: [String] = []
        var groups: [String: [PeerDelivery]] = [:]
        for delivery in deliveries {
            let key = "\(delivery.sourceAgentID)>\(delivery.targetAgentID)"
            if groups[key] == nil { order.append(key) }
            groups[key, default: []].append(delivery)
        }
        for key in order {
            guard let group = groups[key], let latest = group.first else { continue }
            let open = group.contains { [PeerDeliveryStatus.queued, PeerDeliveryStatus.delivered].contains($0.status) }
            var details = [WorkDetail(label: "Sent", value: String(group.count))]
            details.append(WorkDetail(label: "Latest", value: Self.label(latest.intent)))
            graph.add(WorkWire(id: "handoff:\(key)", from: Self.agentNode(latest.sourceAgentID), to: Self.agentNode(latest.targetAgentID),
                               state: open ? WorkWireState.active : latest.status == PeerDeliveryStatus.failed ? WorkWireState.failed : "",
                               label: Self.label(latest.intent), carries: Self.cargo(latest.intent), details: details,
                               chats: [latest.conversationID], updatedAt: latest.createdAt))
        }
    }

    private static func label(_ intent: String) -> String {
        switch intent {
        case PeerDeliveryIntent.request: "Task"
        case PeerDeliveryIntent.question: "Question"
        case PeerDeliveryIntent.result: "Result"
        case PeerDeliveryIntent.status: "Status"
        case PeerDeliveryIntent.handoff: "Handoff"
        default: "Note"
        }
    }

    /// What a handoff carries, which decides the parcel drawn on its wire.
    public static func cargo(_ intent: String) -> String {
        switch intent {
        case PeerDeliveryIntent.request, PeerDeliveryIntent.handoff: WorkKind.task
        case PeerDeliveryIntent.question: WorkKind.question
        case PeerDeliveryIntent.result: WorkKind.result
        case PeerDeliveryIntent.status: WorkKind.status
        default: WorkKind.message
        }
    }

    // MARK: Delegated tasks

    private func addDelegation(_ graph: inout Draft, runs: [Run]) {
        let byID = Dictionary(runs.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        for run in runs where run.trigger == RunTrigger.child {
            guard let parent = run.parentRunID.flatMap({ byID[$0] }) else { continue }
            let open = Self.running(run.status)
            let wireState = open ? WorkWireState.active : run.status == RunStatus.failed ? WorkWireState.failed : ""
            let nodeState = open ? WorkNodeState.working : run.status == RunStatus.failed ? WorkNodeState.failed : WorkNodeState.done
            if let external = run.external {
                // A task handed to an outside agent, which stands on the map as itself.
                let id = "run:\(run.id)"
                // The engine's own logo when the app ships one, which names it faster than any word.
                let logo = external.agent.map { "acp:" + $0 } ?? Self.mark(external)
                graph.add(WorkNode(id: id, title: external.name, parent: Self.agentNode(parent.agentID), kind: WorkKind.worker, subtitle: run.summary,
                                   symbol: logo, state: nodeState,
                                   details: [WorkDetail(label: "Started by", value: agents.first { $0.id == parent.agentID }?.name ?? "A bot")]))
                graph.add(WorkWire(id: "delegate:\(run.id)", from: Self.agentNode(parent.agentID), to: id, state: wireState, label: "Task", carries: WorkKind.task))
            } else if parent.agentID == run.agentID {
                let id = "run:\(run.id)"
                graph.add(WorkNode(id: id, title: "Subagent", parent: Self.agentNode(run.agentID), kind: WorkKind.agent, subtitle: run.summary,
                                   symbol: "arrow.triangle.branch", state: nodeState))
                graph.add(WorkWire(id: "delegate:\(run.id)", from: Self.agentNode(run.agentID), to: id, state: wireState, label: "Task", carries: WorkKind.task))
            } else {
                graph.add(WorkWire(id: "delegate:\(parent.agentID)>\(run.agentID)", from: Self.agentNode(parent.agentID), to: Self.agentNode(run.agentID),
                                   state: wireState, label: "Task", carries: WorkKind.task))
            }
        }
    }

    // MARK: Tools

    private struct App {
        let key: String
        let title: String
        let kind: String
    }

    /// Coordination that the wires already show, or that touches nothing outside Polymux.
    private static let quiet: Set<String> = [
        "status", "message_agent", "task", "create_task", "read_task", "update_task", "track_work", "save_memory", "recall_memory", "forget_memory",
        "update_group_chat", "create_group_chat", "create_agent", "update_agent", "delete_agent", "ask_user", "send_message", "tool_search",
    ]

    private static func app(_ tool: String) -> App? {
        if quiet.contains(tool) || tool.hasPrefix("graph_visualiser__") { return nil }
        if let range = tool.range(of: "__") {
            let name = String(tool[..<range.lowerBound])
            return App(key: name, title: name.prefix(1).uppercased() + name.dropFirst(), kind: WorkKind.plugin)
        }
        switch tool {
        case "bash", "shell": return App(key: "terminal", title: "Terminal", kind: WorkKind.terminal)
        case "read", "write", "edit", "grep", "find", "ls", "attach_file": return App(key: "files", title: "Files", kind: WorkKind.files)
        case "http_request": return App(key: "web", title: "Web", kind: WorkKind.web)
        case "run_device": return App(key: "device", title: "Device", kind: WorkKind.device)
        case "generate_image", "generate_video": return App(key: "media", title: "Media", kind: WorkKind.studio)
        default: return App(key: tool, title: tool, kind: WorkKind.app)
        }
    }

    private static let verbs = ["read": "Read", "write": "Wrote", "edit": "Edited", "grep": "Searched", "find": "Searched", "ls": "Listed", "attach_file": "Attached"]

    /// One tool call as the thing it touched: a file by its type, a command, a link or a picture.
    static func item(_ tool: ToolActivity) -> (kind: String, title: String, subtitle: String) {
        let summary = tool.summary?.isEmpty == false ? tool.summary ?? tool.name : tool.name
        switch tool.name {
        case "read", "write", "edit", "attach_file", "grep", "find", "ls":
            let path = summary.split(separator: " ").first.map(String.init) ?? summary
            let name = path.split(separator: "/").last.map(String.init) ?? path
            let searched = ["grep", "find", "ls"].contains(tool.name)
            return (searched ? WorkKind.folder : WorkKind.file(at: path), searched ? summary : name, verbs[tool.name] ?? tool.name)
        case "bash", "shell": return (WorkKind.command, summary, "Command")
        case "http_request": return (WorkKind.link, summary, "Request")
        case "generate_image": return (WorkKind.image, summary, "Generated")
        case "generate_video": return (WorkKind.video, summary, "Generated")
        default: return (WorkKind.tool, summary, tool.name)
        }
    }

    /// Claude Code and Codex run through a bot's terminal, which is all Polymux sees of them.
    static func worker(_ command: String) -> (title: String, symbol: String)? {
        let words = command.split(whereSeparator: { " ;&|(\n".contains($0) }).map(String.init)
        for (index, word) in words.enumerated() {
            let name = word.split(separator: "/").last.map(String.init) ?? word
            let rest = words[(index + 1)...]
            if name == "claude", rest.contains(where: { $0 == "-p" || $0 == "--print" }) { return ("Claude Code", "sparkle") }
            if name == "codex", rest.first == "exec" { return ("Codex", "curlybraces") }
        }
        return nil
    }

    private func addTools(_ graph: inout Draft, runs: [Run]) {
        var calls: [String: Int] = [:]
        for run in runs {
            let owner = Self.agentNode(run.agentID)
            guard graph.ids.contains(owner) else { continue }
            for tool in run.tools ?? [] {
                guard let app = Self.app(tool.name) else { continue }
                let open = tool.status == ToolActivityStatus.running
                let failed = tool.status == ToolActivityStatus.failed
                let state = open ? WorkNodeState.working : failed ? WorkNodeState.failed : WorkNodeState.done
                if app.key == "terminal", let worker = Self.worker(tool.summary ?? "") {
                    let id = "worker:\(tool.id)"
                    graph.add(WorkNode(id: id, title: worker.title, parent: owner, kind: WorkKind.worker, subtitle: tool.summary, symbol: worker.symbol, state: state,
                                       details: [WorkDetail(label: "Started by", value: agents.first { $0.id == run.agentID }?.name ?? "A bot")]))
                    graph.add(WorkWire(id: "start:\(tool.id)", from: owner, to: id, state: open ? WorkWireState.active : failed ? WorkWireState.failed : "",
                                       label: "Task", carries: WorkKind.task))
                    continue
                }
                let node = "app:\(run.agentID):\(app.key)"
                let wire = "use:\(run.agentID):\(app.key)"
                let item = Self.item(tool)
                // A bot and its app trade things both ways, so the line has no arrow and its parcel shows what.
                graph.add(WorkNode(id: node, title: app.title, parent: owner, kind: app.kind))
                graph.add(WorkWire(id: wire, from: owner, to: node, direction: WorkWireDirection.none, carries: item.kind))
                if open {
                    Self.mark(&graph, node: node, wire: wire, carries: item.kind, doing: Self.doing(tool, item.title))
                }
                guard calls[node, default: 0] < Self.callLimit else { continue }
                calls[node, default: 0] += 1
                graph.add(WorkNode(id: "tool:\(tool.id)", title: item.title, parent: node, kind: item.kind, subtitle: item.subtitle, state: state,
                                   details: [WorkDetail(label: "Tool", value: tool.name), WorkDetail(label: item.subtitle, value: tool.summary ?? tool.name)]))
            }
        }
    }

    private static func mark(_ graph: inout Draft, node: String, wire: String, carries: String, doing: String) {
        if let index = graph.nodes.firstIndex(where: { $0.id == node }) { graph.nodes[index].state = WorkNodeState.working }
        if let index = graph.wires.firstIndex(where: { $0.id == wire }) {
            graph.wires[index].state = WorkWireState.active
            graph.wires[index].carries = carries
            graph.wires[index].label = doing
        }
    }

    /// A running tool call in a few words, such as "Editing auth.go".
    static func doing(_ tool: ToolActivity, _ what: String) -> String {
        switch tool.name {
        case "read": "Reading \(what)"
        case "write": "Writing \(what)"
        case "edit": "Editing \(what)"
        case "attach_file": "Attaching \(what)"
        case "grep", "find", "ls": "Searching files"
        case "bash", "shell", "run_device": "Running \(what)"
        case "http_request": "Fetching \(what)"
        case "generate_image": "Drawing \(what)"
        case "generate_video": "Rendering \(what)"
        default: "Using \(what)"
        }
    }

    // MARK: Devices

    private func addDevices(_ graph: inout Draft) {
        guard let network else { return }
        graph.add(WorkNode(id: Self.serverDevice, title: network.server.name, kind: WorkKind.device, symbol: "desktopcomputer", shape: WorkNodeShape.frame))
        var details = [WorkDetail(label: "Platform", value: network.server.platform)]
        if let build = activity?.build ?? Optional(network.server.build), !build.isEmpty { details.insert(WorkDetail(label: "Build", value: build), at: 0) }
        graph.add(WorkNode(id: Self.server, title: "Polymux server", parent: Self.serverDevice, kind: WorkKind.server, details: details))
        let online = Set(network.online ?? [])
        for device in network.devices where device.paired != false && device.id != network.host {
            let id = "device:\(device.id)"
            graph.add(WorkNode(id: id, title: device.title, kind: WorkKind.device(named: device.name + " " + device.title),
                               subtitle: device.id == network.current ? "This device" : nil))
            graph.add(WorkWire(id: "link:\(device.id)", from: id, to: Self.server, direction: WorkWireDirection.both, line: WorkWireLine.dashed,
                               state: online.contains(device.id) ? WorkWireState.active : ""))
        }
    }

    // MARK: Merging

    /// In a chat, the environment only shows where something in the chat reaches it.
    private func merged(_ core: Draft, _ environment: Draft) -> WorkGraph {
        var graph = core
        guard let chat else {
            environment.nodes.forEach { graph.add($0) }
            environment.wires.forEach { graph.add($0) }
            return finished(graph)
        }
        var reached = core.ids
        for node in environment.nodes where node.chats?.contains(chat) == true { reached.insert(node.id) }
        var moved = true
        while moved {
            moved = false
            for wire in environment.wires where wire.chats?.contains(chat) != false || reached.contains(wire.from) || reached.contains(wire.to) {
                guard wire.chats == nil || wire.chats?.contains(chat) == true || reached.contains(wire.from) && reached.contains(wire.to) else { continue }
                if reached.contains(wire.from) || reached.contains(wire.to) {
                    moved = reached.insert(wire.from).inserted || moved
                    moved = reached.insert(wire.to).inserted || moved
                }
            }
            for node in environment.nodes {
                if reached.contains(node.id), let parent = node.parent { moved = reached.insert(parent).inserted || moved }
                if let parent = node.parent, reached.contains(parent), node.chats?.contains(chat) != false, parent != Self.serverDevice {
                    moved = reached.insert(node.id).inserted || moved
                }
            }
        }
        environment.nodes.filter { reached.contains($0.id) }.forEach { graph.add($0) }
        environment.wires.filter { reached.contains($0.from) && reached.contains($0.to) }.forEach { graph.add($0) }
        return finished(graph)
    }

    private func finished(_ graph: Draft) -> WorkGraph {
        let known = graph.ids
        let wires = graph.wires.filter { known.contains($0.from) && known.contains($0.to) }
        return WorkGraph(revision: 0, nodes: graph.nodes, wires: wires)
    }
}
