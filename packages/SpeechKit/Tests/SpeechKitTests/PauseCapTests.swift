import Testing
@testable import SpeechKit

private let rate = 16_000

/// An alternating signal, because a constant one is DC and DC is removed before measuring.
private func signal(_ amplitude: Float, _ samples: Int) -> [Float] {
    (0..<samples).map { $0.isMultiple(of: 2) ? amplitude : -amplitude }
}
private func speech(_ samples: Int) -> [Float] { signal(0.1, samples) }
private func silence(_ samples: Int) -> [Float] { signal(1e-7, samples) }

private func run(_ cap: inout PauseCap, _ samples: [Float]) -> Int {
    cap.process(samples).count
}

@Test func aLongPauseKeepsItsHeadAndTail() {
    var cap = PauseCap(sampleRate: rate)
    var emitted = run(&cap, speech(rate))
    emitted += run(&cap, silence(4 * rate))
    emitted += run(&cap, speech(rate))
    // A pause hands over its first 2.5 s and last 300 ms, give or take the frame that crossed.
    let kept = 4 * rate - cap.dropped
    #expect(kept <= cap.headSamples + cap.tailSamples)
    #expect(kept >= cap.headSamples + cap.tailSamples - cap.frameSamples)
    #expect(emitted == 2 * rate + kept)
}

@Test func silenceBeforeTheFirstWordIsNeverCut() {
    var cap = PauseCap(sampleRate: rate)
    var emitted = run(&cap, silence(8 * rate))
    emitted += run(&cap, speech(rate))
    #expect(cap.dropped == 0)
    #expect(emitted == 9 * rate)
}

@Test func shortPausesPassThroughWhole() {
    var cap = PauseCap(sampleRate: rate)
    var emitted = run(&cap, speech(rate))
    emitted += run(&cap, silence(2 * rate))
    emitted += run(&cap, speech(rate))
    #expect(cap.dropped == 0)
    #expect(emitted == 4 * rate)
}

@Test func aTrailingPauseKeepsOnlyItsHead() {
    var cap = PauseCap(sampleRate: rate)
    _ = run(&cap, speech(rate))
    _ = run(&cap, silence(4 * rate))
    let rest = cap.finalize()
    // The head went out during the pause; what is left was never heard.
    #expect(rest.isEmpty)
    #expect(cap.dropped >= 4 * rate - cap.headSamples)
}

@Test func aDcOffsetMicrophoneIsNotAllSpeech() {
    var frames = SpeechFrames()
    // A constant offset would pin every frame loud without DC removal.
    #expect(frames.classify(Array(repeating: 0.3, count: 320)).quiet)
}

@Test func quietRoomNoiseLeavesSpeechAboveIt() {
    var frames = SpeechFrames()
    for _ in 0..<100 { _ = frames.classify(signal(0.0002, 320)) }
    let level = frames.classify(signal(0.05, 320))
    #expect(level.speech && !level.quiet)
}
