import CoreGraphics
import Testing
@testable import AvatarKit

@Suite struct ThinkingTests {
    @Test func pulsesMatchTheShippedReference() {
        let expected = [
            [-0.0038213792267136823, 0.16377658200716014, 0.524259495273677],
            [-0.07656517016575012, 0.2028965762454864, 0.9860633487458528],
            [-0.010994570225039704, 0.16763416472181547, 0.5697975020500222]
        ]
        for index in 0..<3 {
            let dot = thinkingDot(index, time: 0.35, amount: 1, reducedMotion: false)
            for (actual, reference) in zip([dot.y, dot.r, dot.opacity], expected[index]) {
                #expect(abs(actual - reference) < 1e-12)
            }
            let repeated = thinkingDot(index, time: 1.75, amount: 1, reducedMotion: false)
            #expect(abs(dot.y - repeated.y) < 1e-12)
            #expect(abs(dot.r - repeated.r) < 1e-12)
            #expect(abs(dot.opacity - repeated.opacity) < 1e-12)
        }
    }

    @Test func everyShapeBecomesThreeSeparateDots() {
        for shape in SHAPES {
            let engine = BotEngine(initial: .thinking, shape: shape.radii)
            engine.solid = shape.id.solid
            engine.displaySize = 76
            for time in stride(from: 1.0, through: 2.4, by: 0.1) {
                let frame = engine.sample(time)
                let core = frame.bodyPath.boundingBoxOfPath
                #expect(frame.eyes.isEmpty)
                #expect(frame.dots.count == 2)
                #expect(abs(core.width - core.height) < 0.01)
                #expect(core.width < 60 && core.width > 30)
                let left = frame.dots[0], right = frame.dots[1]
                #expect(left.x + left.r < core.minX)
                #expect(right.x - right.r > core.maxX)
                #expect(abs(left.y - right.y) < 15)
                #expect(frame.arcs.isEmpty && frame.notif == nil)
            }
        }
    }

    @Test func thinkingQuicklyDropsCharacterDetail() {
        let engine = BotEngine(shape: ShapeID.cupcake.radii)
        engine.character = ShapeID.cupcake.character
        engine.solid = ShapeID.cupcake.solid
        engine.setMood(.thinking)
        _ = engine.sample(0)
        #expect(!engine.sample(0).parts.isEmpty)
        #expect(engine.sample(0.1).parts.isEmpty, "A ghost of the avatar lingers while it becomes dots")
    }

    @Test func thinkingMoodMorphsBackToTheStreamingFace() {
        let engine = BotEngine(shape: ShapeID.hexagon.radii)
        engine.solid = ShapeID.hexagon.solid
        engine.setMood(.thinking)
        let start = engine.sample(0)
        #expect(start.eyes.count == 2 && start.dots.isEmpty)
        let dots = engine.sample(1)
        #expect(dots.eyes.isEmpty && dots.dots.count == 2)
        engine.setMood(.streaming)
        let departing = engine.sample(1)
        #expect(departing.eyes.isEmpty && departing.dots.count == 2)
        #expect(abs(departing.bodyPath.boundingBoxOfPath.width - dots.bodyPath.boundingBoxOfPath.width) < 0.01)
        let streaming = engine.sample(1.8)
        #expect(streaming.eyes.count == 2 && streaming.dots.isEmpty)
        #expect(streaming.bodyPath.boundingBoxOfPath.width > 100)
    }

    @Test func reducedMotionShowsThreeEqualStillDots() {
        let engine = BotEngine(initial: .thinking, shape: ShapeID.star.radii)
        engine.solid = ShapeID.star.solid
        engine.reducedMotion = true
        let first = engine.sample(0), later = engine.sample(10)
        #expect(first.eyes.isEmpty && first.dots.count == 2)
        #expect(abs(first.bodyPath.boundingBoxOfPath.width - later.bodyPath.boundingBoxOfPath.width) < 1e-9)
        for (a, b) in zip(first.dots, later.dots) {
            #expect(a.x == b.x && a.y == b.y && a.r == b.r && a.opacity == b.opacity)
            #expect(a.y == 0 && a.opacity == 1)
        }
        engine.setState(.idle, 10)
        #expect(engine.sample(10).dots.isEmpty)
    }

    @Test func interruptedExitKeepsTheVisibleMorphAndPulse() {
        let engine = BotEngine(shape: ShapeID.hexagon.radii)
        engine.solid = ShapeID.hexagon.solid
        engine.setMood(.thinking)
        _ = engine.sample(0)
        _ = engine.sample(1)
        engine.setMood(.streaming)
        _ = engine.sample(1)
        let leaving = engine.sample(1.12)
        engine.setMood(.thinking)
        let returning = engine.sample(1.12)
        #expect(abs(leaving.bodyPath.boundingBoxOfPath.width - returning.bodyPath.boundingBoxOfPath.width) < 0.01)
        for (a, b) in zip(leaving.dots, returning.dots) {
            #expect(abs(a.x - b.x) < 1e-9 && abs(a.y - b.y) < 1e-9)
            #expect(abs(a.r - b.r) < 1e-9 && abs(a.opacity - b.opacity) < 1e-9)
        }
        #expect(engine.sample(2).eyes.isEmpty)
    }
}
