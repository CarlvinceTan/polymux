import CoreGraphics
import Testing
@testable import PolymuxKit

@Suite
struct WorldMotionTests {
    private let route = WorldRoute(from: CGPoint(x: 0, y: 0), control: CGPoint(x: 50, y: -80), to: CGPoint(x: 100, y: 0))

    /// Between one moment and the next, something on a crossing has moved and not gone backwards.
    @Test func aTravellerAdvancesAlongItsRoute() {
        var last = WorldMotion.position(route, seconds: 0, index: 0)
        var moved = 0.0
        for step in 1...8 {
            let next = WorldMotion.position(route, seconds: Double(step) * 0.25, index: 0)
            let advance = hypot(next.x - last.x, next.y - last.y)
            #expect(advance > 0.5, "stood still at step \(step)")
            moved += advance
            last = next
        }
        #expect(moved > 60, "travelled only \(moved) points in two seconds")
        // Stay on the route rather than drifting off it.
        for step in 0...20 {
            let at = WorldMotion.position(route, seconds: Double(step) * 0.2, index: 0)
            #expect(at.x >= -0.5 && at.x <= 100.5)
            #expect(at.y >= -45 && at.y <= 1)
        }
    }

    @Test func travellersAreOffsetAndReversalMirrors() {
        let first = WorldMotion.position(route, seconds: 1, index: 0)
        let second = WorldMotion.position(route, seconds: 1, index: 1)
        #expect(hypot(first.x - second.x, first.y - second.y) > 4, "two travellers sat on top of each other")
        let forward = WorldMotion.position(route, seconds: 0.8, index: 0)
        let back = WorldMotion.position(route, seconds: 0.8, index: 0, reversed: true)
        let start = route.from
        let end = route.to
        #expect(abs(hypot(forward.x - start.x, forward.y - start.y) - hypot(back.x - end.x, back.y - end.y)) < 0.001)
        // Standing still holds a place rather than falling back to the start.
        #expect(WorldMotion.position(route, seconds: 3, index: 2, still: true) == route.point(at: 0.6))
    }
}
