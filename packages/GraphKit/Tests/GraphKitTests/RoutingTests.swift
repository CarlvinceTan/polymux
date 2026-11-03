import Testing
@testable import GraphKit

// Ported from the old canvas's `workflowWireRouting.test.mjs`.
@Suite
struct RoutingTests {
    private func p(_ x: Double, _ y: Double) -> Point { Point(x: x, y: y) }

    @Test func pointOnSideReturnsTheCentreOfEachSide() {
        let pos = p(100, 200)
        #expect(Routing.pointOnSide(pos, .right) == p(340, 248))
        #expect(Routing.pointOnSide(pos, .left) == p(100, 248))
        #expect(Routing.pointOnSide(pos, .top) == p(220, 200))
        #expect(Routing.pointOnSide(pos, .bottom) == p(220, 296))
    }

    @Test func oppositeSideFlipsEachSide() {
        #expect(Side.allCases.map(Routing.oppositeSide) == [.left, .top, .right, .bottom])
    }

    @Test func rankSidesFollowsTheDominantDirection() {
        #expect(Routing.rankSides(10, 5) == [.right, .bottom, .top, .left])
        #expect(Routing.rankSides(-1, 9) == [.bottom, .left, .right, .top])
    }

    @Test func segmentsIntersectDetectsCrossingsAndRejectsDisjointSegments() {
        #expect(Routing.segmentsIntersect(p(0, 0), p(10, 10), p(0, 10), p(10, 0)))
        #expect(!Routing.segmentsIntersect(p(0, 0), p(1, 0), p(0, 5), p(1, 5)))
        #expect(!Routing.segmentsIntersect(p(0, 0), p(1, 1), p(5, 0), p(6, 1)))
    }

    @Test func segmentCrossesRectHandlesEdgesInsidePointsAndMisses() {
        #expect(Routing.segmentCrossesRect(p(-10, 5), p(30, 5), 0, 0, 20, 20))
        #expect(Routing.segmentCrossesRect(p(10, 10), p(50, 50), 0, 0, 20, 20))
        #expect(!Routing.segmentCrossesRect(p(-10, -50), p(30, -50), 0, 0, 20, 20))
    }

    @Test func routesAvoidObstacles() throws {
        let obstacle = Obstacle(x: 150, y: 50, w: 100, h: 60)
        let start = p(0, 0)
        let end = p(400, 200)
        let plan = try #require(Routing.planOrthogonalRoute(start, end, .right, .left, [obstacle]))
        #expect(!Routing.polylineCrossesAnyObstacle(plan, [obstacle]))
        let grid = try #require(Routing.aStarOrthogonalRoute(start, end, .right, .left, [obstacle]))
        #expect(!Routing.polylineCrossesAnyObstacle(grid, [obstacle]))
        #expect(grid.first == start && grid.last == end)
    }

    @Test func aStarTakesOverWhenEveryFixedRouteIsBlocked() throws {
        // Walls either side of the midpoint lane, leaving a gap only A* can find.
        let walls = [
            Obstacle(x: 90, y: -400, w: 40, h: 390),
            Obstacle(x: 90, y: 30, w: 40, h: 400),
            Obstacle(x: 240, y: -400, w: 40, h: 500),
            Obstacle(x: 240, y: 150, w: 40, h: 300),
        ]
        let start = p(0, 0)
        let end = p(400, 0)
        let route = try #require(Routing.aStarOrthogonalRoute(start, end, .right, .left, walls))
        #expect(!Routing.polylineCrossesAnyObstacle(route, walls))
    }

    @Test func roundedPathKeepsOneRadiusAndDropsCollinearPoints() {
        #expect(Routing.roundedPolylinePath([p(0, 0), p(50, 0), p(100, 0)]) == [.move(p(0, 0)), .line(p(100, 0))])
        #expect(Routing.roundedPolylinePath([p(0, 0), p(10, 0), p(10, 100)]) == [
            .move(p(0, 0)), .line(p(2, 0)), .quad(control: p(10, 0), to: p(10, 8)), .line(p(10, 100)),
        ])
        #expect(Routing.roundedPolylinePath([p(0, 0)]).isEmpty)
    }
}
