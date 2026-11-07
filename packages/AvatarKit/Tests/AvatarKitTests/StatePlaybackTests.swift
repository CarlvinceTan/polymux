import Testing
@testable import AvatarKit

@Suite struct StatePlaybackTests {
    @Test func cueIdentityRestartsOnlyNewEntrances() throws {
        let engine = BotEngine(initial: .notify)
        var playback = StatePlayback()
        playback.update(engine, state: .notify, now: 100, controlled: false, repeating: false, cueID: "first")
        playback.update(engine, state: .notify, now: 101, controlled: false, repeating: false, cueID: "first")
        let settled = try #require(engine.sample(101).notif).r
        playback.update(engine, state: .notify, now: 101.1, controlled: false, repeating: false, cueID: "first")
        #expect(try #require(engine.sample(101.1).notif).r == settled)
        playback.update(engine, state: .notify, now: 102, controlled: false, repeating: false, cueID: "second")
        #expect(try #require(engine.sample(102).notif).r == settled)
        playback.update(engine, state: .notify, now: 102.2, controlled: false, repeating: false, cueID: "second")
        #expect(try #require(engine.sample(102.2).notif).r > settled)
    }

    @Test func frozenUnreadResumesWithLivePop() throws {
        let engine = BotEngine(initial: .notify)
        var playback = StatePlayback()
        playback.update(engine, state: .notify, now: 0.9, controlled: true, repeating: false, cueID: nil)
        let settled = try #require(engine.sample(0.9).notif).r
        playback.update(engine, state: .notify, now: 200, controlled: false, repeating: false, cueID: "notice")
        #expect(try #require(engine.sample(200).notif).r == settled)
        playback.update(engine, state: .notify, now: 200.2, controlled: false, repeating: false, cueID: "notice")
        #expect(try #require(engine.sample(200.2).notif).r > settled)
        playback.update(engine, state: .notify, now: 0.9, controlled: true, repeating: false, cueID: nil)
        #expect(try #require(engine.sample(0.9).notif).r == settled)
    }

    @Test func controlledSeekingAndReducedMotionHoldTheirPose() throws {
        let engine = BotEngine(initial: .notify)
        var playback = StatePlayback()
        for time in [1.0, 0.2, 1.0] {
            playback.update(engine, state: .notify, now: time, controlled: true, repeating: true, cueID: "same")
            let reference = BotEngine(initial: .notify)
            #expect(try #require(engine.sample(time).notif).r == #require(reference.sample(time).notif).r)
        }
    }
}
