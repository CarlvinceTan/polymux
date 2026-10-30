import CoreGraphics
import Foundation
import Testing
@testable import AvatarKit

@Suite struct MoodTests {
    @Test func restingAvatarsFaceTheGazeAndEaseBack() {
        var player = MoodPlayer(seed: 3)
        player.shape = .squircle
        var time = 0.0
        func step(_ frames: Int) -> ClipFrame {
            for _ in 0..<frames { time += 1.0 / 60; _ = player.sample(.idle, now: time, resting: true, live: true) }
            return player.sample(.idle, now: time, resting: true, live: true).pose
        }
        #expect(step(1).rot == REST_TURN)
        player.setGaze([1, -0.5])
        #expect(!player.settled)
        let target = gazeTurn(.squircle, x: 1, y: -0.5)
        let facing = step(120)
        #expect(abs(facing.rot[0] - target.rot[0]) < 1e-9 && abs(facing.rot[1] - target.rot[1]) < 1e-9)
        #expect(facing.rot[2] == 0 && player.settled)
        player.setGaze(nil)
        #expect(step(120).rot == REST_TURN && player.settled)
    }

    @Test func liveAvatarsEaseIntoTheirRestAndStillOnesSnap() {
        func rest(live: Bool) -> (start: ClipFrame, end: ClipFrame, settled: Bool) {
            var player = MoodPlayer(seed: 3)
            player.firstPhase = 0.5
            _ = player.sample(.working, now: 0, live: live)
            _ = player.sample(.working, now: 1, live: live)
            let start = player.sample(.idle, now: 1, resting: true, live: live).pose
            return (start, player.sample(.idle, now: 2, resting: true, live: live).pose, player.settled)
        }
        let live = rest(live: true), still = rest(live: false)
        #expect(live.start.rot != REST_TURN)
        #expect(zip(live.end.rot, REST_TURN).allSatisfy { abs($0 - $1) < 1e-9 } && live.settled)
        #expect(still.start.rot == REST_TURN)
    }

    @Test func restingMoodsDoNotDriftIntoUnrelatedStates() {
        for mood in [MoodID.idle, .blocked, .failed, .interrupted, .done, .celebrate] {
            var player = MoodPlayer(seed: 7)
            let first = player.sample(mood, now: 0, resting: true).pose
            let later = player.sample(mood, now: 300, resting: true)
            #expect(later.mood == mood)
            #expect(later.pose.rot == first.rot)
            #expect(later.pose.eyes == first.eyes)
        }
    }

    @Test func quietAvatarsSleepAndWakeOnInteraction() {
        var player = MoodPlayer(seed: 7)
        #expect(player.sample(.idle, now: 0).mood == .idle)
        #expect(player.sample(.idle, now: 179).mood == .idle)
        #expect(player.sample(.idle, now: 180).mood == .sleeping)
        #expect(player.sample(.idle, now: 181, interaction: true).mood == .waking)
        #expect(player.sample(.idle, now: 182).mood == .waking)
        #expect(player.sample(.idle, now: 183).mood == .idle)
        #expect(player.sample(.working, now: 400).mood == .working)
        #expect(player.sample(.working, now: 1000).mood == .working)
    }

    @Test func waitingAndTransientMoodsChainWithoutRestarting() {
        var player = MoodPlayer(seed: 2)
        #expect(player.sample(.blocked, now: 10).mood == .blocked)
        #expect(player.sample(.blocked, now: 129).mood == .blocked)
        #expect(player.sample(.blocked, now: 130).mood == .drowsy)
        #expect(player.sample(.done, now: 131).mood == .done)
        #expect(player.sample(.done, now: 138.9).mood == .done)
        #expect(player.sample(.done, now: 139).mood == .idle)
        #expect(player.sample(.failed, now: 140).mood == .failed)
        #expect(player.sample(.failed, now: 146).mood == .idle)
        #expect(player.sample(.spawning, now: 147).mood == .spawning)
        #expect(player.sample(.spawning, now: 180).mood == .spawning)
    }

    // Samples evaluated with sa, ei and ro from Grok Bot 0.75.3's shipped renderer.
    @Test func blockedTracksMatchTheReferenceBetweenKeys() {
        let clip = RECIPES["Blocked_A"]!
        let cases: [(Double, [Double])] = [
            (0.6, [-0.09132577577969436, 0.044715912522755225, 5.225355442910166, 4.710951536888793, -1.4715957139521425, 0.5605961813145779]),
            (0.9, [-0.018055900549132562, 0.04973287955856661, 10.00661556077191, 10.940548606271696, 5.435850798338834, 0.5065733753111008]),
            (1.1494959879909783, [-0.037998949715928196, 0.04829443513309427, 9.616907890376693, 7.001708121233538, 2.718956129095087, 0.5])
        ]
        for (time, expected) in cases {
            let actual = sample(clip.position, time)! + sample(clip.rotation, time)! + sample(clip.zoom, time)!
            for (a, b) in zip(actual, expected) { #expect(abs(a - b) < 1e-10) }
        }
    }

    @Test func blockedFaceFollowsTheClipKeysThenRepeats() {
        var player = MoodPlayer(seed: 2)
        player.firstPhase = 0
        func face(at time: Double) -> [EyeShape] { player.sample(.blocked, now: time).pose.eyes }
        _ = face(at: 0)
        #expect(face(at: 0.4) == FaceID.neutral.eyes)
        #expect(face(at: 1.0) == FaceID.confused.eyes)
        #expect(face(at: 1.5) == FaceID.confused.eyes)
        #expect(face(at: 2.0) == FaceID.neutral.eyes)
        #expect(face(at: 2.12 + 1.0) == FaceID.confused.eyes)
        #expect(player.sample(.blocked, now: 1.15 + 2.12).pose.zoom < 0.52)
        #expect(player.sample(.blocked, now: 120).mood == .drowsy)
    }

    @Test func faceMorphsBendRatherThanShrinkThroughADot() {
        for face in [FaceID.confused, .happy, .sleepy, .calm, .surprised] {
            var player = FacePlayer()
            player.morph(face)
            for _ in 0..<30 {
                player.step(MORPH_TIME / 30)
                let reach = player.eyes.map { eye in
                    let box = placed(eye)
                    return max(box.width, box.height)
                }.min()!
                #expect(reach > 0.33, "\(face) \(reach)")
            }
            #expect(player.eyes == face.eyes)
        }
    }

    @Test func lidTracksBlinkOpenEyesButLeaveShapedFacesAlone() {
        var player = ClipPlayer()
        player.load(RECIPES["Idle_B"]!, bridged: false)
        var shut = 1.0
        for _ in 0..<96 {
            player.advance(1 / 60.0)
            shut = min(shut, placed(player.frame.eyes[0]).height / 0.57)
        }
        #expect(shut < 0.3)
        player.hold(.happy)
        for _ in 0..<30 { player.advance(1 / 60.0) }
        let happy = placed(player.frame.eyes[0]).height
        for _ in 0..<240 {
            player.advance(1 / 60.0)
            #expect(abs(placed(player.frame.eyes[0]).height - happy) < 1e-9)
        }
    }

    @Test func moodsMixGrokAndPolymuxClips() {
        var player = MoodPlayer(seed: 5)
        var seen = Set<String>()
        for now in stride(from: 0.0, through: 240, by: 1 / 30.0) {
            _ = player.sample(.idle, now: now, interaction: Int(now) % 60 == 0)
            if let name = player.playing { seen.insert(name) }
        }
        #expect(seen.contains { $0.hasPrefix("Idle_") })
        #expect(seen.contains { $0.hasPrefix("idle-") })
        #expect(seen.contains { $0.hasPrefix("hop-") })
        var small = MoodPlayer(seed: 5)
        var compact = Set<String>()
        for now in stride(from: 0.0, through: 120, by: 1 / 30.0) {
            _ = small.sample(.idle, now: now, compact: true)
            if let name = small.playing { compact.insert(name) }
        }
        #expect(!compact.contains("Idle_A") && !compact.contains("Idle_D"))
    }

    @Test func restingAvatarsHoldTheirMoodFaceWithoutMoving() {
        var player = MoodPlayer(seed: 2)
        let first = player.sample(.blocked, now: 0, resting: true).pose
        let later = player.sample(.blocked, now: 5, resting: true).pose
        #expect(first.eyes == FaceID.confused.eyes && later.eyes == first.eyes)
        #expect(first.rot == REST_TURN && later.rot == REST_TURN)
        let engine = BotEngine(shape: ShapeID.circle.radii)
        engine.solid = ShapeID.circle.solid
        engine.resting = true
        engine.setMood(.idle)
        let a = engine.sample(0), b = engine.sample(3)
        #expect(a.eyes.count == 2 && a.eyes.map(\.d) == b.eyes.map(\.d))
        // At rest the eyes look up and to the right.
        let middle = a.eyes.map { $0.d.boundingBoxOfPath }.reduce(CGPoint()) { CGPoint(x: $0.x + $1.midX / 2, y: $0.y + $1.midY / 2) }
        #expect(middle.x > 15 && middle.y < -15)
    }

    @Test func grokTurnsRoundTripThroughRotations() {
        for turn in [REST_TURN, [12.0, -40, 7], [-30.0, 20, -15]] {
            for (a, b) in zip(Rotation(turn: turn).turn, turn) { #expect(abs(a - b) < 1e-9) }
        }
        for (a, b) in zip(MotionPose().turn, REST_TURN) { #expect(abs(a - b) < 1e-6) }
        #expect(abs(MotionPose(yaw: 360).turn[1] - REST_TURN[1] - 360) < 1e-6)
    }

    @Test func idleIncludesGlancesAndAnOccasionalFullTurn() {
        var player = MoodPlayer(seed: 17)
        var turned = false, glanced = false
        for now in stride(from: 0.0, through: 120, by: 1 / 30.0) {
            let pose = player.sample(.idle, now: now).pose
            if abs(pose.rot[1]) > 300 { turned = true }
            if abs(pose.rot[1] - REST_TURN[1]) > 20 && abs(pose.rot[1]) < 300 { glanced = true }
        }
        #expect(turned)
        #expect(glanced)
    }

    @Test func spawningGathersInwardThenRestsBeforeRepeating() {
        let early = moodMarks(.spawning, 0.3), later = moodMarks(.spawning, 0.6)
        #expect(early.count == 5 && later.count == 5)
        for (a, b) in zip(early, later) {
            #expect(hypot(b.x, b.y) <= hypot(a.x, a.y))
            #expect(b.r >= a.r)
        }
        #expect(moodMarks(.spawning, 2.1).isEmpty)
        #expect(moodMarks(.spawning, 3.3).isEmpty)
        #expect(!moodMarks(.spawning, 3.8).isEmpty)
    }

    @Test func spawningMorphsTheBodyIntoAHubAndRestoresItsFace() {
        let engine = BotEngine(shape: ShapeID.star.radii)
        engine.solid = ShapeID.star.solid
        engine.setMood(.spawning)
        _ = engine.sample(0)
        let hub = engine.sample(0.8)
        let small = hub.bodyPath.boundingBoxOfPath
        #expect(small.width > 30 && small.width < 36)
        #expect(abs(small.width - small.height) < 1)
        #expect(hub.eyes.isEmpty)
        let resting = engine.sample(3)
        #expect(resting.bodyPath.boundingBoxOfPath.width > small.width * 3)
        #expect(!resting.eyes.isEmpty)
        _ = engine.sample(4.2)
        engine.setMood(.idle)
        _ = engine.sample(4.3)
        let ready = engine.sample(4.9)
        #expect(ready.bodyPath.boundingBoxOfPath.width > small.width * 3)
        #expect(!ready.eyes.isEmpty)
    }

    @Test func clipsMoveSmoothlyAndRemainReproducible() {
        var first = MoodPlayer(seed: 17), second = MoodPlayer(seed: 17)
        var prior: [Double]?
        var varied = false
        func step(_ a: [Double], _ b: [Double]) -> Double { zip(a, b).map { abs($0 - $1) }.max()! }
        for now in stride(from: 0.0, through: 30, by: 1 / 60.0) {
            let a = first.sample(.working, now: now).pose
            let b = second.sample(.working, now: now).pose
            #expect(a.rot == b.rot && a.pos == b.pos && a.eyes == b.eyes)
            if let prior { #expect(step(a.rot, prior) < 10) }
            if step(a.rot, REST_TURN) > 15 { varied = true }
            prior = a.rot
        }
        #expect(varied)
        let switching = first.sample(.sad, now: 30 + 1 / 60.0).pose
        #expect(step(switching.rot, prior!) < 10)
    }

    @Test func everyMoodRendersOnEveryShapeWithoutLosingTheFace() {
        for shape in ShapeID.allCases {
            let engine = BotEngine(shape: shape.radii)
            engine.solid = shape.solid
            engine.character = shape.character
            for mood in MoodID.allCases {
                engine.setMood(mood)
                _ = engine.sample(0)
                for time in [0.6, 1.3, 3.0] {
                    let frame = engine.sample(time)
                    let bounds = frame.bodyPath.boundingBoxOfPath
                    let hub = mood == .thinking || mood == .spawning && time < 2
                    #expect(bounds.width > (hub ? 25 : 50) && bounds.width < 300, "\(shape)/\(mood)")
                    #expect(bounds.height > (hub ? 25 : 50) && bounds.height < 300, "\(shape)/\(mood)")
                    if hub { #expect(frame.eyes.isEmpty, "\(shape)/\(mood)") }
                    #expect(frame.eyes.allSatisfy { $0.matrix.tx.isFinite && $0.matrix.ty.isFinite })
                }
                guard ![.idle, .thinking, .spawning].contains(mood),
                      [.circle, .hexagon, .cloud, .sparkle, .cat, .magnet].contains(shape) else { continue }
                // Clips turn the body away at times, but the face keeps coming back.
                engine.setMood(mood)
                _ = engine.sample(0)
                let seen = stride(from: 0.5, through: 6, by: 0.5).filter { !engine.sample($0).eyes.isEmpty }.count
                #expect(seen > ([.celebrate, .playful].contains(mood) ? 0 : 6), "\(shape)/\(mood)")
            }
        }
    }

    @Test func activityRingsStayCenteredOnTheVisibleAvatar() throws {
        for shape in ShapeID.allCases {
            for mood in [MoodID.working, .searching] {
                let engine = BotEngine(shape: shape.radii)
                engine.solid = shape.solid
                engine.character = shape.character
                engine.moods.firstPhase = 0
                engine.setMood(mood)
                _ = engine.sample(0)
                let reach = shape == .wedge ? 0.55 : shape == .heart ? 0.6 : 1
                engine.setTurn(Turn(yaw: 14.3 * reach, pitch: -10.45 * reach), 0)
                for time in [0.2, 0.7, 1.6, 3.0] {
                    let frame = engine.sample(time)
                    let bounds = frame.parts.filter { !$0.surface && $0.alpha > 0.01 }
                        .reduce(frame.bodyPath.boundingBoxOfPath) { $0.union(($1.outline ?? $1.path).boundingBoxOfPath) }
                    let marks = moodMarks(mood, time)
                    #expect(frame.dots.count == marks.count)
                    let first = try #require(frame.dots.first), mark = try #require(marks.first)
                    let radius = hypot(first.x - bounds.midX, first.y - bounds.midY) / hypot(mark.x, mark.y)
                    #expect(radius > 0 && radius <= engine.scale + 1e-8)
                    #expect(zip(frame.dots, marks).allSatisfy { dot, mark in
                        abs(dot.x - mark.x * radius - bounds.midX) < 1e-8
                            && abs(dot.y - mark.y * radius - bounds.midY) < 1e-8
                    }, "\(shape)/\(mood) at \(time)")
                    let edge = BotEngine.FRAME_SIZE * engine.scale / 200
                    #expect(frame.dots.allSatisfy { max(abs($0.x), abs($0.y)) + $0.r <= edge + 1e-8 },
                            "\(shape)/\(mood) ring is clipped at \(time)")
                }
            }
        }
    }

    @Test func workingOverlaysAreDistinctAndFollowTime() {
        for mood in [MoodID.searching, .working, .writing, .sending, .receiving] {
            let marks = moodMarks(mood, 0.4)
            let later = moodMarks(mood, 0.9)
            #expect(!marks.isEmpty)
            #expect(marks.contains { $0.opacity > 0 && $0.r > 0 })
            #expect(zip(marks, later).contains { $0.x != $1.x || $0.y != $1.y || $0.opacity != $1.opacity })
        }
    }
}

/// An eye's extent once tilted and lidded, in Grok Bot's face units.
private func placed(_ eye: EyeShape) -> CGRect {
    let c = cos(eye.tilt), s = sin(eye.tilt)
    let points = eye.outline.map { p -> CGPoint in
        let y = p.y * eye.lidY
        return CGPoint(x: c * p.x + s * y, y: -s * p.x + c * y)
    }
    let xs = points.map(\.x), ys = points.map(\.y)
    guard let x0 = xs.min(), let x1 = xs.max(), let y0 = ys.min(), let y1 = ys.max() else { return .zero }
    return CGRect(x: x0, y: y0, width: x1 - x0, height: y1 - y0)
}
