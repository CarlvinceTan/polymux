import Testing
@testable import PolymuxKit

@Suite
struct WorldTerracesTests {
    private func world() -> WorkWorld {
        let nodes = [
            WorkNode(id: "frame", title: "Studio", kind: WorkKind.group),
            WorkNode(id: "a", title: "One", parent: "frame", kind: WorkKind.app),
            WorkNode(id: "b", title: "Two", parent: "frame", kind: WorkKind.app),
            WorkNode(id: "solo", title: "Alone", kind: WorkKind.device),
        ]
        return WorkWorld(WorkGraph(revision: 0, nodes: nodes, wires: []))
    }

    @Test func anIslandRisesInTerracesFromItsSand() throws {
        let terraces = WorldTerraces(world())
        // Home steps up three times from its own sand to the hearth at the middle.
        let hearth = WorldCell(i: 0, j: 0)
        #expect(terraces.paved.contains(hearth) && terraces.rise[hearth] == 3)
        // On a district island, the paving stands three terraces above its own sand.
        let station = try #require(world().stations.max(by: { $0.id < $1.id }))
        let middle = WorldCell(i: station.i - 1, j: station.j - 1)
        let coast = WorldCell(i: station.i - 4, j: station.j - 4)
        let ring = WorldCell(i: station.i - 3, j: station.j - 3)
        #expect(terraces.rise[middle] == 3 && terraces.rise[ring] == 1 && terraces.rise[coast] == 0)
        #expect(terraces.maxRise == 3)
    }

    @Test func nothingRisesWhereThereIsNoGround() {
        let terraces = WorldTerraces(WorkWorld())
        // Nothing but sea and the home slab rises.
        let home = WorldCell(i: 0, j: 0)
        #expect(terraces.rise[home] == 3)
        #expect(terraces.rise[WorldCell(i: 99, j: 99)] == nil)
        // A lift asked for off the map is no lift at all.
        #expect(terraces.lift(99, 99, scale: 2) == 0)
        #expect(terraces.ocean.isSubset(of: Set(WorkWorld().bridges)))
    }

    @Test func sandRingsEveryIslandAndEveryRiseStandsOnLand() {
        let terraces = WorldTerraces(world())
        for (cell, level) in terraces.rise {
            #expect(terraces.land[cell] != nil)
            if level == 0 {
                let coast = [(1, 0), (-1, 0), (0, 1), (0, -1)].contains { terraces.land[WorldCell(i: cell.i + $0.0, j: cell.j + $0.1)] == nil }
                #expect(coast)
            }
        }
    }
}
