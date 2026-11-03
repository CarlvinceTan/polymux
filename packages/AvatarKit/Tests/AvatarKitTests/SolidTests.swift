import CoreGraphics
import Testing
@testable import AvatarKit

@Suite
struct SolidTests {
    // On a sphere, eyes fixed to the turning body land where the head-direction eyes always did.
    @Test func sphereBodyKeepsTheHeadEyes() {
        let flat = BotEngine(scale: 100, shape: ShapeID.circle.radii)
        let solid = BotEngine(scale: 100, shape: ShapeID.circle.radii)
        solid.solid = ShapeID.circle.solid
        for id in [StateID.idle, .wink, .thinking] {
            flat.setState(id, 0); solid.setState(id, 0)
            for now in stride(from: 0.5, through: 6, by: 0.75) {
                let a = flat.sample(now).eyes, b = solid.sample(now).eyes
                #expect(a.count == b.count, "\(id) at \(now)")
                for (x, y) in zip(a, b) {
                    #expect(abs(x.matrix.tx - y.matrix.tx) < 0.05 && abs(x.matrix.ty - y.matrix.ty) < 0.05, "\(id) at \(now)")
                    #expect(abs(x.matrix.a - y.matrix.a) < 0.01 && abs(x.matrix.d - y.matrix.d) < 0.01, "\(id) at \(now)")
                }
            }
        }
    }

    @Test func turnedOutlinesStayWhole() {
        let turns = [Rotation(yaw: 15, pitch: 11), Rotation(yaw: -15, pitch: -11), Rotation(yaw: 120, pitch: 20)]
        for shape in SHAPES {
            let sil = Silhouette(radii: shape.radii)
            for turn in turns {
                let outline = solidOutline(sil, shape.id.solid, turn, scale: 1)
                for point in outline {
                    let radius = hypot(point.x, point.y)
                    #expect(radius.isFinite && radius > 0.1 && radius < 1.6, "\(shape.id)")
                }
            }
        }
    }

    @Test func extrusionsKeepTheirDepthAndConcaveOutline() {
        for shape in [ShapeID.sparkle, .star, .flower] {
            let sil = Silhouette(radii: shape.radii)
            let edge = solidPath(sil, shape.solid, Rotation(yaw: 90, pitch: 0), scale: 100)
            let bounds = edge.boundingBoxOfPath
            #expect(bounds.width > 50 && bounds.height > 100)
            let front = solidPath(sil, shape.solid, .identity, scale: 100)
            var inside = 0, outside = 0
            for i in 0..<120 {
                let angle = Double(i) / 120 * TAU
                if front.contains(CGPoint(x: cos(angle) * 90, y: sin(angle) * 90)) { inside += 1 }
                else { outside += 1 }
            }
            #expect(inside > 0 && outside > 0)
        }
    }

    @Test func facesStayOnTheFrontOfExtrudedBodies() {
        for shape in [ShapeID.square, .sparkle, .house, .star] {
            let engine = BotEngine(shape: shape.radii)
            engine.solid = shape.solid
            engine.setLook(Look(wander: 0), 0)
            let front = engine.sample(0.7)
            #expect(front.eyes.count == 2)
            for eye in front.eyes {
                #expect(front.bodyPath.contains(CGPoint(x: eye.matrix.tx, y: eye.matrix.ty)))
            }
            engine.setTurn(Turn(yaw: 150), 0.7)
            let back = engine.sample(1.2)
            #expect(back.eyes.isEmpty)
            #expect(back.bodyPath != front.bodyPath)
        }
    }

    @Test func eyeFitMorphsWithTheSelectedShape() {
        let engine = BotEngine(shape: ShapeID.square.radii)
        engine.solid = ShapeID.square.solid
        engine.setLook(Look(wander: 0), 0)
        let before = engine.sample(1).eyes
        engine.setShape(ShapeID.star.radii, 1)
        engine.solid = ShapeID.star.solid
        let started = engine.sample(1).eyes
        let settled = engine.sample(1.6).eyes
        #expect(before.count == 2 && started.count == 2 && settled.count == 2)
        for (a, b) in zip(before, started) {
            #expect(abs(a.matrix.tx - b.matrix.tx) < 0.01)
            #expect(abs(a.matrix.ty - b.matrix.ty) < 0.01)
            #expect(abs(a.matrix.a - b.matrix.a) < 0.01)
            #expect(abs(a.matrix.d - b.matrix.d) < 0.01)
        }
        for (a, b) in zip(started, settled) {
            #expect(abs(a.matrix.a - b.matrix.a) > 0.1)
        }
    }

    @Test func rapidShapeChangesKeepTheVisibleEyeFit() {
        let engine = BotEngine(shape: ShapeID.square.radii)
        engine.solid = ShapeID.square.solid
        engine.setLook(Look(wander: 0), 0)
        engine.setShape(ShapeID.sparkle.radii, 1)
        engine.solid = ShapeID.sparkle.solid
        for (shape, now) in [(ShapeID.house, 1.08), (.clover, 1.14)] {
            let before = engine.sample(now).eyes
            engine.setShape(shape.radii, now)
            engine.solid = shape.solid
            let after = engine.sample(now).eyes
            #expect(before.count == 2 && after.count == 2)
            for (a, b) in zip(before, after) {
                #expect(abs(a.matrix.tx - b.matrix.tx) < 0.01)
                #expect(abs(a.matrix.ty - b.matrix.ty) < 0.01)
                #expect(abs(a.matrix.a - b.matrix.a) < 0.01)
                #expect(abs(a.matrix.d - b.matrix.d) < 0.01)
            }
        }
    }
}
