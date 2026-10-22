import Foundation
import Testing
@testable import AvatarKit

@Suite struct MoodTests {
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

    @Test func idleIncludesGlancesAndAnOccasionalFullTurn() {
        var player = MoodPlayer(seed: 17)
        var turned = false, glanced = false
        for now in stride(from: 0.0, through: 30, by: 1 / 30.0) {
            let motion = player.sample(.idle, now: now).motion
            if motion.yaw > 180 { turned = true }
            if abs(motion.yaw) > 5 && motion.zoom > 1.02 { glanced = true }
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
        var prior = MotionPose()
        var varied = false
        for now in stride(from: 0.0, through: 30, by: 1 / 30.0) {
            let a = first.sample(.working, now: now).motion
            let b = second.sample(.working, now: now).motion
            #expect(a == b)
            #expect(abs(a.yaw - prior.yaw) < 2)
            #expect(abs(a.pitch - prior.pitch) < 2)
            if abs(a.yaw) > 5 || abs(a.pitch) > 5 { varied = true }
            prior = a
        }
        #expect(varied)
        let switching = first.sample(.sad, now: 30).motion
        #expect(abs(switching.yaw - prior.yaw) < 0.01)
    }

    @Test func everyMoodRendersOnEveryShapeWithoutLosingTheFace() {
        for shape in ShapeID.allCases {
            let engine = BotEngine(shape: shape.radii)
            engine.solid = shape.solid
            for mood in MoodID.allCases {
                engine.setMood(mood)
                _ = engine.sample(0)
                for time in [0.6, 1.3, 3.0] {
                    let frame = engine.sample(time)
                    let bounds = frame.bodyPath.boundingBoxOfPath
                    let hub = mood == .spawning && time < 2
                    #expect(bounds.width > (hub ? 25 : 50) && bounds.width < 300, "\(shape)/\(mood)")
                    #expect(bounds.height > (hub ? 25 : 50) && bounds.height < 300, "\(shape)/\(mood)")
                    if hub { #expect(frame.eyes.isEmpty, "\(shape)/\(mood)") }
                    else if mood != .idle { #expect(!frame.eyes.isEmpty, "\(shape)/\(mood)") }
                    #expect(frame.eyes.allSatisfy { $0.matrix.tx.isFinite && $0.matrix.ty.isFinite })
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
