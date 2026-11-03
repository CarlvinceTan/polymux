import CoreGraphics
import Foundation
import Testing
@testable import AvatarKit

// The reference build's clips slide the eyes across the face instead of turning the body, so the
// eyes keep their size while they move.
@Suite struct ShiftTests {
    private func shifting(_ shift: [(Double, [Double])], duration: Double = 2) -> Recipe {
        Recipe(name: "shift", duration: duration, shift: shift.map { Key(t: $0.0, v: $0.1) })
    }

    @Test func clipFramesCarryTheShiftTrack() {
        var player = ClipPlayer()
        player.load(shifting([(0, [-0.1, 0.05]), (2, [0.1, -0.05])]), phase: 0)
        #expect(player.frame.shift == [-0.1, 0.05])
        for _ in 0..<20 { player.advance(0.1) }
        let shift = player.frame.shift
        #expect(abs(shift[0] - 0.1) < 0.02 && abs(shift[1] + 0.05) < 0.02)
    }

    @Test func bridgesCarryTheShiftIntoTheNextClip() {
        var player = ClipPlayer()
        player.load(shifting([(0, [0.3, 0])]), phase: 0)
        player.load(Recipe(name: "still", duration: 2), phase: 0)
        // Midway through the bridge the eyes are still on their way back to the face.
        player.advance(0.15)
        #expect(player.frame.shift[0] > 0.05)
        for _ in 0..<20 { player.advance(0.1) }
        #expect(player.frame.shift == [0, 0])
    }

    @Test func fittedClipsKeepTheReferenceTiming() throws {
        let listen = try #require(RECIPES["Listen_A"])
        #expect(listen.duration == 10)
        #expect(listen.faces.map(\.face) == [.calm, .neutral])
        #expect(listen.faces.map(\.t) == [0.85, 8.42])
        #expect(listen.shift.first?.t == 0)
        #expect(abs((listen.shift.last?.t ?? 0) - 10) < 1e-9)

        let done = try #require(RECIPES["Done_D"])
        #expect(done.duration == 6)
        #expect(done.faces.map(\.face) == [.happy, .neutral])
        #expect(done.lids.count == 3)
        // The clip starts and ends at rest, so it loops without a jump.
        let ends = [done.shift.first!.v, done.shift.last!.v]
        for (a, b) in zip(ends[0], ends[1]) { #expect(abs(a - b) < 1e-3) }
    }

    /// A seed whose first pick for listening is the fitted clip, so the engine plays it alone.
    private func fittedSeed() -> UInt64? {
        for seed in UInt64(1)...2000 {
            var player = MoodPlayer(seed: seed)
            player.firstPhase = 0
            _ = player.sample(.listening, now: 0)
            if player.playing == "Listen_A" { return seed }
        }
        return nil
    }

    @Test func listeningEyesSlideWithoutForeshortening() throws {
        let seed = try #require(fittedSeed())
        let engine = BotEngine(scale: 100, initial: .idle, shape: ShapeID.circle.radii,
                               expression: ExpressionID.neutral.expression)
        engine.solid = ShapeID.circle.solid
        engine.moods = MoodPlayer(seed: seed)
        engine.moods.firstPhase = 0
        engine.setMood(.listening)
        var mids: [Double] = []
        var widths: [Double] = []
        for step in 0...40 {
            let frame = engine.sample(Double(step) / 10)
            guard step >= 15 else { continue }
            guard frame.eyes.count == 2 else { continue }
            let boxes = frame.eyes.map { $0.d.boundingBoxOfPath }
            mids.append(Double(boxes[0].midX + boxes[1].midX) / 2)
            widths.append(Double(boxes.map(\.width).max() ?? 0))
        }
        // In engine units the body is 100 across, so the reference's sweep is tens of points.
        #expect((mids.max() ?? 0) - (mids.min() ?? 0) > 15)
        // A body turn would shrink the far eye; a slide keeps both the same size.
        #expect((widths.max() ?? 0) - (widths.min() ?? 0) < 0.08 * (widths.max() ?? 1))
    }
}
