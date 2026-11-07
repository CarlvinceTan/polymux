import Foundation

/// Classifies 20 ms frames against an adaptive noise floor.
/// Speech is any frame at least 12 dB above the floor and never quieter than
/// -55 dBFS; quiet is stricter (floor + 4 dB, or below -70 dBFS) so audio cut
/// as pause is never speech the margin missed. The floor falls instantly and
/// rises about 3 dB/s, so speech between word gaps cannot drag it up.
/// DC is removed first: an offset mic would pin every frame to the floor.
struct SpeechFrames {
    private static let speechMarginDb: Float = 12
    private static let quietMarginDb: Float = 4
    private static let minimumSpeechDb: Float = -55
    private static let quietMinimumDb: Float = -70
    private static let lowestFloorDb: Float = -90
    private static let floorRiseDbPerFrame: Float = 0.06

    /// Start at the lowest floor, so capture that begins mid-word still hears speech.
    private var floorDb: Float = SpeechFrames.lowestFloorDb

    mutating func classify(_ frame: [Float]) -> (quiet: Bool, speech: Bool) {
        let (levelDb, floor) = measure(frame)
        return (levelDb <= max(floor + Self.quietMarginDb, Self.quietMinimumDb),
                levelDb > max(floor + Self.speechMarginDb, Self.minimumSpeechDb))
    }

    private mutating func measure(_ frame: [Float]) -> (Float, Float) {
        guard !frame.isEmpty else { return (-120, floorDb) }
        let count = Float(frame.count)
        let mean = frame.reduce(0, +) / count
        var energy: Float = 0
        for sample in frame {
            let delta = sample - mean
            energy += delta * delta
        }
        energy /= count
        let levelDb = energy.isFinite ? 10 * log10(max(energy, 1e-18)) : 0
        let floor: Float
        if levelDb >= floorDb { floor = floorDb + Self.floorRiseDbPerFrame }
        else { floor = max(levelDb, Self.lowestFloorDb) }
        floorDb = floor
        return (levelDb, floor)
    }
}

/// A dictation feed that hears at most the head and tail of each long pause:
/// the first 2.5 s of a pause passes (so words before it finish decoding) and
/// only the last 300 ms returns when speech resumes; the middle is dropped.
/// Unlike Hex's stream wrapper, nothing is cut before the first speech frame:
/// with no retained clip to re-transcribe, a quiet mic could lose the opening.
struct PauseCap {
    let frameSamples: Int
    let headSamples: Int
    let tailSamples: Int

    private var speech = SpeechFrames()
    private var frame: [Float] = []
    /// Quiet samples since the last frame above the noise floor.
    private var pause = 0
    /// The newest pause samples past the head, released if speech resumes.
    private var tail: [Float] = []
    private(set) var dropped = 0
    private(set) var heard = false

    init(sampleRate: Int) {
        let rate = sampleRate > 0 ? Double(sampleRate) : 16_000
        frameSamples = max(1, Int(rate / 50))
        headSamples = Int(2.5 * rate)
        tailSamples = max(1, Int(0.3 * rate))
    }

    mutating func process(_ samples: [Float]) -> [Float] {
        var out: [Float] = []
        out.reserveCapacity(samples.count)
        var start = 0
        while start < samples.count {
            let take = min(frameSamples - frame.count, samples.count - start)
            frame.append(contentsOf: samples[start..<start + take])
            start += take
            if frame.count == frameSamples { completeFrame(&out) }
        }
        return out
    }

    private mutating func completeFrame(_ out: inout [Float]) {
        defer { frame.removeAll(keepingCapacity: true) }
        let level = speech.classify(frame)
        if !level.quiet {
            heard = true
            out.append(contentsOf: tail)
            tail.removeAll(keepingCapacity: true)
            pause = 0
            out.append(contentsOf: frame)
            return
        }
        pause += frame.count
        if !heard || pause <= headSamples {
            out.append(contentsOf: frame)
            return
        }
        tail.append(contentsOf: frame)
        let overflow = tail.count - tailSamples
        if overflow > 0 {
            tail.removeFirst(overflow)
            dropped += overflow
        }
    }

    /// A trailing pause keeps only its head; the rest is never heard.
    mutating func finalize() -> [Float] {
        defer { frame = [] }
        if !heard || pause <= headSamples { return frame }
        dropped += frame.count + tail.count
        tail = []
        return []
    }
}
