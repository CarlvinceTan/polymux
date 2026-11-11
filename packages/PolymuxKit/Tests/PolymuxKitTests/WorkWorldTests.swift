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
