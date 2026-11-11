import Foundation
import Testing
@testable import PolymuxKit

@Suite
struct WorkWorldTests {
    private func bot(_ id: String, state: String = WorkNodeState.idle) -> WorkNode {
        WorkNode(id: "agent:" + id, title: id, kind: WorkKind.agent, agentID: id, state: state)
    }

    private func app(_ id: String, _ title: String, of bot: String? = nil, kind: String = WorkKind.app) -> WorkNode {
        WorkNode(id: id, title: title, parent: bot.map { "agent:" + $0 }, kind: kind)
    }

    private func use(_ bot: String, _ node: String, active: Bool = true, label: String? = nil) -> WorkWire {
        WorkWire(id: "\(bot)>\(node)", from: "agent:" + bot, to: node, state: active ? WorkWireState.active : "", label: label)
    }

    @Test func everyBotsTerminalIsTheOneTerminal() throws {
        let nodes = [bot("a"), bot("b"), app("app:a:terminal", "Terminal", of: "a", kind: WorkKind.terminal),
                     app("app:b:terminal", "Terminal", of: "b", kind: WorkKind.terminal)]
        let world = WorkWorld(WorkGraph(revision: 0, nodes: nodes, wires: []))
        let station = try #require(world.stations.first)
        #expect(world.stations.count == 1 && station.members.count == 2)
    }

    @Test func someoneStandsWhereTheirWorkIsAndRestsOtherwise() {
        let nodes = [bot("a", state: WorkNodeState.working), bot("b"), app("safari", "Safari", of: "a")]
        let world = WorkWorld(WorkGraph(revision: 0, nodes: nodes, wires: [use("a", "safari", label: "Reading the docs")]))
        let safari = world.stations.first?.id ?? ""
        #expect(world.actor("agent:a")?.station == safari && world.actor("agent:a")?.doing == "Reading the docs")
        #expect(world.actor("agent:b")?.station == nil)
        #expect(world.present(at: safari).map(\.id) == ["agent:a"])
    }

    @Test func aStepHoldsSomeoneWhateverToolTheyReachFor() {
        let nodes = [bot("a", state: WorkNodeState.working), app("media", "Media", of: "a", kind: WorkKind.studio),
                     WorkNode(id: "film", title: "Film", kind: WorkKind.pipeline), WorkNode(id: "draw", title: "Draw", parent: "film", kind: WorkKind.step)]
        let world = WorkWorld(WorkGraph(revision: 0, nodes: nodes, wires: [use("a", "media"), use("a", "draw")]))
        #expect(world.actor("agent:a")?.station == "draw")
    }

    @Test func aPipelineKeepsTheOrderItsWiresGive() {
        var nodes = [WorkNode(id: "film", title: "Film", kind: WorkKind.pipeline)]
        nodes += ["edit", "script", "shots"].map { WorkNode(id: $0, title: $0, parent: "film", kind: WorkKind.step) }
        nodes.append(WorkNode(id: "frame", title: "frame.png", parent: "shots", kind: WorkKind.image, preview: "/tmp/frame.png"))
        let wires = [WorkWire(id: "1", from: "script", to: "shots"), WorkWire(id: "2", from: "shots", to: "edit")]
        for tall in [false, true] {
            let world = WorkWorld(WorkGraph(revision: 0, nodes: nodes, wires: wires), tall: tall)
            #expect(world.stations.sorted { ($0.step ?? 0) < ($1.step ?? 0) }.map(\.id) == ["script", "shots", "edit"])
            #expect(world.groups.first?.ordered == true && world.groups.first?.biome == .savanna)
            #expect(world.station("shots")?.preview == "frame" && world.item("frame")?.station.id == "shots")
        }
    }

    @Test func noTwoStationsShareAPlotHoweverManyThereAre() {
        var nodes = (0..<14).map { app("app\($0)", "App \($0)") }
        nodes += (0..<5).map { WorkNode(id: "repo\($0)", title: "repo\($0)", kind: WorkKind.repo) }
        nodes += [WorkNode(id: "mac", title: "Mac", kind: WorkKind.device, shape: WorkNodeShape.frame), WorkNode(id: "db", title: "db", parent: "mac", kind: WorkKind.data)]
        for tall in [false, true] {
            let world = WorkWorld(WorkGraph(revision: 0, nodes: nodes, wires: []), tall: tall)
            #expect(world.stations.count == 20)
            for (index, a) in world.stations.enumerated() {
                #expect(max(abs(a.i), abs(a.j)) >= WorkWorld.homeReach + 3, "\(a.id) stands on home")
                for b in world.stations[(index + 1)...] {
                    #expect(max(abs(a.i - b.i), abs(a.j - b.j)) >= WorkWorld.pitch, "\(a.id) and \(b.id) overlap")
                    // Two districts keep water between them.
                    if a.group != b.group { #expect(max(abs(a.i - b.i), abs(a.j - b.j)) > WorkWorld.pitch + 1) }
                }
            }
        }
    }

    @Test func aBotsWorkSharesOneDistrict() {
        let nodes = [bot("a"), bot("b"), app("a:mail", "Mail", of: "a", kind: WorkKind.app), app("a:files", "Files", of: "a", kind: WorkKind.files),
                     app("b:studio", "Media", of: "b", kind: WorkKind.studio), app("shared", "Safari")]
        let world = WorkWorld(WorkGraph(revision: 0, nodes: nodes, wires: []))
        #expect(world.groups.count == 3)
        #expect(world.groups.first { $0.title == "a" }?.biome == .meadow)
        #expect(world.station("shared")?.group == "shared")
        #expect(world.station("a:mail")?.group == world.station("a:files")?.group)
    }

    @Test func aMachineHoldsWhatIsOnIt() throws {
        let nodes = [WorkNode(id: "mac", title: "Studio Mac", kind: WorkKind.device, shape: WorkNodeShape.frame),
                     WorkNode(id: "repo", title: "site", parent: "mac", kind: WorkKind.repo), app("safari", "Safari")]
        let world = WorkWorld(WorkGraph(revision: 0, nodes: nodes, wires: []))
        let group = try #require(world.groups.first { $0.title == "Studio Mac" })
        #expect(world.station("repo")?.group == group.id && group.biome == .slate)
        #expect(world.station("safari")?.group == "shared")
    }
}

@Suite
struct WorkWorldCoverageTests {
    /// Every kind the map can meet draws as something recognisable, and an unknown one still draws.
    @Test func everyKindHasALook() {
        let kinds = ["person", "agent", "worker", "app", "terminal", "web", "tab", "tool", "plugin", "file", "document", "code", "pdf", "archive",
                     "folder", "design", "image", "video", "audio", "data", "table", "link", "repo", "github", "pull", "issue", "device", "phone",
                     "tablet", "laptop", "desktop", "server", "task", "pipeline", "step", "message", "question", "result", "status", "command"]
        for kind in kinds {
            let look = WorkKind.look(kind)
            #expect(WorkKind.known(kind), "\(kind) isn't known")
            #expect(!look.symbol.isEmpty && !look.title.isEmpty)
        }
        #expect(!WorkKind.known("quantum-flux"))
        #expect(WorkKind.look("quantum-flux").shape == .thing, "an unknown kind still lands somewhere")
        #expect(WorkKind.file(at: "a/b.swift") == WorkKind.code && WorkKind.file(at: "notes/") == WorkKind.folder)
        #expect(WorkKind.device(named: "Carlvince's iPhone") == WorkKind.phone && WorkKind.device(named: "Studio MacBook Pro") == WorkKind.laptop)
    }

    /// Every state a run can be in reaches the map, and only the busy ones say so.
    @Test func everyStateReachesTheMap() {
        for state in [WorkNodeState.idle, WorkNodeState.working, WorkNodeState.waiting, WorkNodeState.done, WorkNodeState.failed] {
            let nodes = [WorkNode(id: "agent:a", title: "A", kind: WorkKind.agent, agentID: "a", state: state),
                         WorkNode(id: "safari", title: "Safari", parent: "agent:a", kind: WorkKind.app, state: state)]
            let world = WorkWorld(WorkGraph(revision: 0, nodes: nodes, wires: [
                WorkWire(id: "w", from: "agent:a", to: "safari", state: state == WorkNodeState.working ? WorkWireState.active : "", carries: WorkKind.image)]))
            let safari = world.stations.first
            #expect(safari?.node.title == "Safari")
            #expect(world.station("safari") == nil && safari != nil, "a bot's app is shared, under its own ID")
            if state == WorkNodeState.working {
                #expect(world.actor("agent:a")?.station == safari?.id)
                #expect(world.trails.contains { $0.active })
            } else {
                #expect(world.actor("agent:a")?.station == nil)
            }
        }
        // Picked out of a station, the newest picture is the one the map shows.
        let nodes = [WorkNode(id: "film", title: "Film", kind: WorkKind.pipeline),
                     WorkNode(id: "s", title: "Shots", parent: "film", kind: WorkKind.step),
                     WorkNode(id: "a.png", title: "a.png", parent: "s", kind: WorkKind.image, preview: "/tmp/a.png")]
        #expect(WorkWorld(WorkGraph(revision: 0, nodes: nodes, wires: [])).station("s")?.preview == "a.png")
    }

    /// A day's work builds fast enough to stay in the frame loop on a phone.
    @Test func aLargeDayBuildsQuickly() {
        var nodes: [WorkNode] = []
        var wires: [WorkWire] = []
        for index in 0..<120 {
            let bot = "agent:b\(index % 40)"
            if index < 40 { nodes.append(WorkNode(id: bot, title: "B\(index)", kind: WorkKind.agent, agentID: "b\(index)", state: WorkNodeState.working)) }
            nodes.append(WorkNode(id: "app\(index)", title: "App \(index)", parent: bot, kind: WorkKind.app))
            wires.append(WorkWire(id: "u\(index)", from: bot, to: "app\(index)", direction: .none, state: WorkWireState.active, carries: WorkKind.file))
            for item in 0..<6 {
                nodes.append(WorkNode(id: "item\(index)-\(item)", title: "f\(item).png", parent: "app\(index)", kind: WorkKind.image, preview: "/tmp/f\(item).png"))
            }
        }
        for index in 0..<200 { wires.append(WorkWire(id: "h\(index)", from: "agent:b\(index % 40)", to: "agent:b\((index + 1) % 40)", carries: WorkKind.task)) }
        let graph = WorkGraph(revision: 0, nodes: nodes, wires: wires)
        let clock = ContinuousClock()
        var built = WorkWorld()
        let took = clock.measure { built = WorkWorld(graph) }
        #expect(built.stations.count == 120 && built.actors.count == 40)
        #expect(took < .milliseconds(250), "building the map took \(took)")
    }
}
