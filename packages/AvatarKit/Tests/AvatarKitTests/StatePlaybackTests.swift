import XCTest
@testable import AvatarKit

final class StatePlaybackTests: XCTestCase {
    func testCueIdentityRestartsOnlyNewEntrances() throws {
        let engine = BotEngine(initial: .notify)
        var playback = StatePlayback()
        playback.update(engine, state: .notify, now: 100, controlled: false, repeating: false, cueID: "first")
        playback.update(engine, state: .notify, now: 101, controlled: false, repeating: false, cueID: "first")
        let settled = try XCTUnwrap(engine.sample(101).notif).r
        playback.update(engine, state: .notify, now: 101.1, controlled: false, repeating: false, cueID: "first")
        XCTAssertEqual(try XCTUnwrap(engine.sample(101.1).notif).r, settled)
        playback.update(engine, state: .notify, now: 102, controlled: false, repeating: false, cueID: "second")
        XCTAssertEqual(try XCTUnwrap(engine.sample(102).notif).r, settled)
        playback.update(engine, state: .notify, now: 102.2, controlled: false, repeating: false, cueID: "second")
        XCTAssertGreaterThan(try XCTUnwrap(engine.sample(102.2).notif).r, settled)
    }

    func testFrozenUnreadResumesWithLivePop() throws {
        let engine = BotEngine(initial: .notify)
        var playback = StatePlayback()
        playback.update(engine, state: .notify, now: 0.9, controlled: true, repeating: false, cueID: nil)
        let settled = try XCTUnwrap(engine.sample(0.9).notif).r
        playback.update(engine, state: .notify, now: 200, controlled: false, repeating: false, cueID: "notice")
        XCTAssertEqual(try XCTUnwrap(engine.sample(200).notif).r, settled)
        playback.update(engine, state: .notify, now: 200.2, controlled: false, repeating: false, cueID: "notice")
        XCTAssertGreaterThan(try XCTUnwrap(engine.sample(200.2).notif).r, settled)
        playback.update(engine, state: .notify, now: 0.9, controlled: true, repeating: false, cueID: nil)
        XCTAssertEqual(try XCTUnwrap(engine.sample(0.9).notif).r, settled)
    }

    func testControlledSeekingAndReducedMotionHoldTheirPose() throws {
        let engine = BotEngine(initial: .notify)
        var playback = StatePlayback()
        for time in [1.0, 0.2, 1.0] {
            playback.update(engine, state: .notify, now: time, controlled: true, repeating: true, cueID: "same")
            let reference = BotEngine(initial: .notify)
            XCTAssertEqual(try XCTUnwrap(engine.sample(time).notif).r, try XCTUnwrap(reference.sample(time).notif).r)
        }
    }
}
