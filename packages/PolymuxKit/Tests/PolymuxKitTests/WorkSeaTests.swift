import Testing
import Foundation
@testable import PolymuxKit

@Suite
struct WorkSeaTests {
    private func islet(_ id: String, _ at: Int) -> WorkIslet {
        WorkIslet(id: id, minI: at, minJ: 0, maxI: at, maxJ: 0, reach: 0)
    }

    @Test func openWaterIsCrossedStraight() {
        let route = WorkSea.route(from: islet("a", 0), to: islet("b", 40), around: [])
        #expect(route.count == 2)
    }

    @Test func landBetweenTwoIslandsIsSailedRound() {
        let blocking = islet("middle", 20)
        let route = WorkSea.route(from: islet("a", 0), to: islet("b", 40), around: [blocking])
        #expect(route.count >= 2)
        for (one, other) in zip(route, route.dropFirst()) {
            #expect(WorkSea.clear(from: one, to: other, around: [blocking]), "a lane ran over \(blocking.id)")
        }
        // And the way round is not a wild detour: no more than half again the direct distance.
        let straight = WorkSea.distance([route[0], route[route.count - 1]])
        #expect(WorkSea.distance(route) < straight * 2)
    }

    @Test func aLineOnlyCoversAnIslandItReallyCrosses() {
        let island = islet("middle", 20)
        #expect(WorkSea.covers(CGPoint(x: -5, y: 0.5), CGPoint(x: 45, y: 0.5), island))
        #expect(!WorkSea.covers(CGPoint(x: -5, y: -40), CGPoint(x: 45, y: -40), island))
        #expect(!WorkSea.covers(CGPoint(x: -5, y: 0.5), CGPoint(x: 5, y: 0.5), island))
    }

    /// Whatever the map looks like, a boat sails over land only when nowhere else is open.
    @Test func aCrowdedMapStillSendsBoatsOverWater() throws {
        let nodes = (1...4).flatMap { index in
            [WorkNode(id: "frame\(index)", title: "Room \(index)", kind: WorkKind.group),
             WorkNode(id: "app\(index)", title: "Tool \(index)", parent: "frame\(index)", kind: WorkKind.app)]
        }
        let world = WorkWorld(WorkGraph(revision: 0, nodes: nodes, wires: []))
        var islands = world.groups.map { WorkIslet(id: $0.id, minI: $0.minI, minJ: $0.minJ, maxI: $0.maxI, maxJ: $0.maxJ) }
        islands.append(WorkIslet(id: "home", minI: -WorkWorld.homeReach, minJ: -WorkWorld.homeReach,
                                 maxI: WorkWorld.homeReach - 1, maxJ: WorkWorld.homeReach - 1, reach: 0))
        #expect(islands.count >= 4)
        var crossings = 0, open = 0, bends = 0
        for start in islands {
            for finish in islands where start.id != finish.id {
                let others = islands.filter { $0.id != start.id && $0.id != finish.id }
                // Every lane the map draws, including the drifted ones boats keep apart with.
                for drift in [-8.0, 0, 8] {
                    let route = WorkSea.route(from: start, to: finish, around: others, drift: CGFloat(drift))
                    let shifted = others.map { $0.shifted(CGFloat(drift)) }
                    crossings += 1
                    // Was any single lane between them open water?
                    let clear = start.quays.contains { berth in
                        finish.quays.contains { call in
                            WorkSea.clear(from: slide(berth, drift), to: slide(call, drift), around: shifted)
                        }
                    }
                    open += clear ? 1 : 0
                    bends += route.count > 2 ? 1 : 0
                    let crossed = zip(route, route.dropFirst()).filter { !WorkSea.clear(from: $0, to: $1, around: shifted) }
                    #expect(crossed.isEmpty, "\(start.id) to \(finish.id) at lane \(drift) sailed over land")
                }
            }
        }
        // And not vacuously: this map is crowded enough that some crossings must sail round an island.
        #expect(crossings > 20)
        #expect(bends > 0, "no crossing on this map needed a bend, so the test proved little")
        #expect(open > crossings / 2, "only \\(open) of \\(crossings) crossings had a lane straight across")
    }

    /// A berth slid along with its lane.
    private func slide(_ at: CGPoint, _ drift: Double) -> CGPoint { CGPoint(x: at.x + drift, y: at.y + drift) }
}
