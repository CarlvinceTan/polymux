import CoreGraphics
import Testing
@testable import AvatarKit

struct MotionTests {
    @Test func pointerReversalsPreservePoseAndVelocity() {
        var spring = MotionSpring()
        spring.retarget(14.3, 0)
        for frame in 1...120 {
            let now = Double(frame) / 60
            let before = spring.sample(now)
            spring.retarget(frame.isMultiple(of: 2) ? -14.3 : 14.3, now)
            let after = spring.sample(now)
            #expect(abs(before.value - after.value) < 1e-12)
            #expect(abs(before.velocity - after.velocity) < 1e-10)
            #expect(abs(after.value) <= 14.3)
        }
        spring.retarget(0, 2)
        #expect(abs(spring.sample(3).value) < 1e-6)
        #expect(abs(spring.sample(3).velocity) < 1e-5)
    }

    @Test func repeatedTargetsDoNotDependOnFrameRate() {
        func trace(_ fps: Int) -> [Double] {
            var spring = MotionSpring(), values: [Double] = []
            for frame in 0...fps {
                let time = Double(frame) / Double(fps)
                spring.retarget(frame < fps / 2 ? 14 : -14, time)
                if frame.isMultiple(of: fps / 10) { values.append(spring.sample(time).value) }
            }
            return values
        }
        let expected = trace(120)
        for fps in [30, 60] {
            for (a, b) in zip(trace(fps), expected) { #expect(abs(a - b) < 1e-10) }
        }
    }

    @Test func wavingAcrossAvatarDoesNotRestartItsPose() {
        let engine = BotEngine(shape: ShapeID.cat.radii)
        engine.solid = ShapeID.cat.solid
        engine.character = ShapeID.cat.character
        engine.interactive = true
        engine.pet(true, 0)
        for step in 1...30 {
            let now = 0.4 + Double(step) * 0.07
            let before = engine.sample(now)
            engine.pet(step.isMultiple(of: 2), now)
            let after = engine.sample(now)
            close(transform(before.transform), transform(after.transform))
            close(before.eyes.map(\.alpha), after.eyes.map(\.alpha))
        }
        engine.pet(false, 3)
        close(transform(engine.sample(4).transform), transform(.identity))
        #expect(engine.sample(4).eyes.count == 2)
    }

    @Test func interruptedInteractionsKeepMomentum() {
        var player = MascotPlayer()
        player.pet(true, 0)
        let step = 0.0001
        for index in 0..<20 {
            let now = 0.45 + Double(index) * 0.09
            let before = player.sample(now - step, idle: true).values
            let current = player.sample(now, idle: true).values
            if index.isMultiple(of: 3) { player.spin(now) }
            else { player.pet(index.isMultiple(of: 2), now) }
            let after = player.sample(now, idle: true).values
            close(current, after)
            let next = player.sample(now + step, idle: true).values
            for i in current.indices {
                let incoming = (current[i] - before[i]) / step
                let outgoing = (next[i] - after[i]) / step
                #expect(abs(incoming - outgoing) < (i == 2 ? 12 : 0.1))
            }
        }
    }

    @Test func interruptedSmileKeepsItsCurvature() {
        var face = FacePlayer()
        face.morph(.happy)
        for frame in 0..<90 {
            face.step(1 / 60.0)
            guard frame.isMultiple(of: 4) else { continue }
            let before = face.eyes
            face.morph(frame.isMultiple(of: 8) ? .neutral : .happy)
            #expect(face.eyes == before)
        }
    }

    @Test func spinReturnsSmoothlyToHoverOrRest() {
        for petted in [false, true] {
            var player = MascotPlayer()
            player.pet(petted, 0)
            player.spin(0.6)
            let before = player.sample(1.4 - 1e-6, idle: true).transform()
            let after = player.sample(1.4, idle: true).transform()
            close(transform(before), transform(after), tolerance: 0.001)
            let settled = player.sample(2, idle: true)
            #expect(petted ? settled.x > 0.04 : abs(settled.x) < 1e-9)
            let busy = player.sample(2, idle: false)
            close(settled.values, busy.values)
            close(transform(player.sample(2.5, idle: false).transform()), transform(.identity))
        }
    }

    @Test func clipHandoffsReachTheActualStartingPose() {
        for recipe in RECIPES.values {
            var player = ClipPlayer()
            player.load(Recipe(name: "source", duration: 1, anchor: 0.4,
                               rotation: [Key(t: 0, v: [5, 10, 15])], squash: [Key(t: 0, v: [0.9, 1.1])]),
                        bridged: false)
            player.load(recipe)
            for _ in 0..<3 { player.advance(0.1) }
            player.advance(BRIDGE_TIME - 0.3 - 1e-7)
            let before = placement(player.frame)
            player.advance(2e-7)
            close(before, placement(player.frame), tolerance: 0.001)
        }
    }

    @Test func clipBridgesKeepIncomingAndOutgoingVelocity() {
        let linear = [0.0, 0, 1, 1]
        let moving = Recipe(name: "moving", duration: 1,
                            rotation: [Key(t: 0, v: [0, 0, 0], ease: linear), Key(t: 1, v: [0, 20, 0])])
        let target = Recipe(name: "target", duration: 1,
                            rotation: [Key(t: 0, v: [0, -10, 0], ease: linear), Key(t: 1, v: [0, -30, 0])])
        var player = ClipPlayer()
        player.load(moving, bridged: false)
        player.advance(0.1)
        player.advance(0.1)
        for interrupted in [false, true] {
            if interrupted { player.advance(0.08) }
            let before = player.frame.rot[1]
            player.advance(0.0001)
            let current = player.frame.rot[1]
            player.load(target)
            #expect(abs(player.frame.rot[1] - current) < 1e-9)
            player.advance(0.0001)
            let velocity = (player.frame.rot[1] - current) / 0.0001
            #expect(abs(velocity - (current - before) / 0.0001) < 0.5)
        }
        for _ in 0..<3 { player.advance(0.1) }
        player.advance(BRIDGE_TIME - 0.3 - 0.0002)
        let before = player.frame.rot[1]
        player.advance(0.0001)
        let current = player.frame.rot[1]
        player.advance(0.0001)
        #expect(abs((current - before) / 0.0001 + 20) < 0.5)
        #expect(abs((player.frame.rot[1] - current) / 0.0001 + 20) < 0.5)
    }

    @Test func bridgesDoNotDiscardTimeOrSnapSmallLoopOffsets() {
        let clip = Recipe(name: "offset", duration: 1,
                          position: [Key(t: 0, v: [0, 0]), Key(t: 1, v: [0.01, 0])])
        func run(_ fps: Int) -> ClipFrame {
            var player = ClipPlayer()
            player.load(clip, bridged: false)
            for _ in 0..<fps { player.advance(1 / Double(fps)) }
            #expect(abs(player.frame.pos[0] - 0.01) < 1e-9)
            for _ in 0..<(fps / 2) { player.advance(1 / Double(fps)) }
            return player.frame
        }
        close(placement(run(30)), placement(run(60)), tolerance: 1e-6)
    }

    private func placement(_ pose: ClipFrame) -> [Double] {
        pose.rot + [pose.pos[0], pose.pos[1] + pose.anchor * (1 - pose.squash[1])]
            + pose.squash + [pose.zoom] + pose.shift
    }

    private func transform(_ value: CGAffineTransform) -> [Double] {
        [value.a, value.b, value.c, value.d, value.tx, value.ty]
    }

    private func close(_ a: [Double], _ b: [Double], tolerance: Double = 1e-9,
                       sourceLocation: SourceLocation = #_sourceLocation) {
        #expect(a.count == b.count, sourceLocation: sourceLocation)
        for (a, b) in zip(a, b) { #expect(abs(a - b) < tolerance, sourceLocation: sourceLocation) }
    }
}
