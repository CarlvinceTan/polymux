import CoreGraphics
import Testing
@testable import AvatarKit

struct MascotMotionTests {
    @Test func sourceKeyframes() {
        let jump = MascotMotion.spin(0.8 * 0.55)
        #expect(abs(jump.roll - 360) < 0.00001)
        #expect(abs(jump.y + 0.16) < 0.00001)
        let land = MascotMotion.spin(0.8 * 0.78)
        #expect(abs(land.sx - 1.08) < 0.00001)
        #expect(abs(land.sy - 0.92) < 0.00001)
        #expect(MascotMotion.spin(0.8).y == 0)
        let pet = MascotMotion.pet(0.32 + 0.78 / 2)
        #expect(abs(pet.roll - 14) < 0.00001)
        #expect(abs(pet.x - 0.08) < 0.00001)
    }

    @Test func interactionsRespectMotionAndBusyStates() {
        let engine = BotEngine(shape: ShapeID.cat.radii)
        engine.solid = ShapeID.cat.solid
        engine.character = ShapeID.cat.character
        engine.interactive = true
        engine.pet(true, 0)
        let pet = engine.sample(0.5)
        #expect(pet.eyes.count == 4)
        #expect(pet.transform != .identity)
        engine.reducedMotion = true
        #expect(engine.sample(0.5).transform == .identity)
        #expect(engine.sample(0.5).eyes.count == 2)
        engine.reducedMotion = false
        engine.setMood(.searching)
        #expect(engine.sample(0.5).eyes.count == 2)
        engine.pet(false, 0.6)
        engine.setMood(nil)
        engine.twirl(1)
        #expect(engine.sample(1.44).transform != .identity)
    }

    @Test func propsHaveStableStaticFramesAndAnimatedParts() {
        for prop in [WorkProp.keyboard, .writing, .chart, .reading, .mail, .planning] {
            let start = prop.layers(elapsed: 0)
            let animated = prop.layers(elapsed: prop == .mail ? 4.5 : 2.3)
            #expect(!start.isEmpty && !animated.isEmpty)
            #expect(start.map(\.path) != animated.map(\.path) || start.map(\.alpha) != animated.map(\.alpha))
            let still = prop.layers(elapsed: 0, reducedMotion: true)
            #expect(still.map(\.path) == prop.layers(elapsed: 3, reducedMotion: true).map(\.path))
            #expect(still.allSatisfy { !$0.path.boundingBoxOfPath.isInfinite })
        }
        #expect(WorkProp.activity(.idle) == nil)
        #expect(WorkProp.activity(.thinking) == nil)
        #expect(WorkProp.activity(.searching) == .reading)
    }

    @Test func flightsKeepSourceTimingAndEndpoints() {
        let from = CGPoint(x: 20, y: 50), to = CGPoint(x: 120, y: 150)
        let start = HandoffMotion.sample(0, from: from, to: to)
        let mid = HandoffMotion.sample(0.5, from: from, to: to)
        let end = HandoffMotion.sample(1, from: from, to: to)
        #expect(start.point == from && start.alpha == 0)
        #expect(mid.point == CGPoint(x: 70, y: 44))
        #expect(mid.scale == 1 && mid.alpha == 1)
        #expect(end.point == to && end.alpha == 0)
    }
}
