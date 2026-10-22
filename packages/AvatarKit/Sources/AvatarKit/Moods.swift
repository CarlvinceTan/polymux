import Foundation

public enum MoodID: String, CaseIterable, Identifiable, Sendable {
    case idle, thinking, working, searching, reading, writing, sending, receiving, streaming
    case blocked, done, failed, interrupted, listening, spawning, waking, sleeping, drowsy
    case happy, excited, laughing, proud, curious, playful, shy, surprised, suspicious
    case angry, confused, sad, bored, scared, celebrate, approving, humming

    public var id: String { rawValue }
    public var label: String { rawValue.capitalized }

    public var expression: ExpressionID? {
        switch self {
        case .idle, .working, .sending, .receiving, .spawning: nil
        case .thinking, .searching, .reading, .curious: .curious
        case .writing, .listening, .blocked, .approving: .attentive
        case .streaming, .happy, .done, .humming: .happy
        case .excited, .celebrate, .playful: .excited
        case .waking, .surprised: .surprised
        case .sleeping, .drowsy: .sleepy
        case .failed, .sad: .sad
        case .interrupted, .confused: .confused
        case .laughing: .laughing
        case .proud: .proud
        case .shy: .shy
        case .suspicious: .suspicious
        case .angry: .angry
        case .bored: .unimpressed
        case .scared: .scared
        }
    }

    public init(expression: ExpressionID) {
        switch expression {
        case .neutral: self = .idle
        case .attentive: self = .listening
        case .surprised: self = .surprised
        case .excited: self = .excited
        case .happy: self = .happy
        case .laughing: self = .laughing
        case .angry: self = .angry
        case .sad: self = .sad
        case .scared: self = .scared
        case .suspicious: self = .suspicious
        case .confused: self = .confused
        case .curious: self = .curious
        case .proud: self = .proud
        case .shy: self = .shy
        case .unimpressed: self = .bored
        case .sleepy: self = .drowsy
        }
    }
}

public struct MotionPose: Equatable, Sendable {
    public var yaw, pitch, roll, x, y, stretch, zoom, lid: Double

    public init(yaw: Double = 0, pitch: Double = 0, roll: Double = 0,
                x: Double = 0, y: Double = 0, stretch: Double = 0,
                zoom: Double = 1, lid: Double = 1) {
        self.yaw = yaw; self.pitch = pitch; self.roll = roll
        self.x = x; self.y = y; self.stretch = stretch; self.zoom = zoom; self.lid = lid
    }

    static func blend(_ a: MotionPose, _ b: MotionPose, _ t: Double) -> MotionPose {
        MotionPose(yaw: lerp(a.yaw, b.yaw, t), pitch: lerp(a.pitch, b.pitch, t),
                   roll: lerp(a.roll, b.roll, t), x: lerp(a.x, b.x, t), y: lerp(a.y, b.y, t),
                   stretch: lerp(a.stretch, b.stretch, t), zoom: lerp(a.zoom, b.zoom, t),
                   lid: lerp(a.lid, b.lid, t))
    }
}

struct MotionClip: Sendable {
    let duration: Double
    let keys: [MotionPose]

    init(_ duration: Double, _ keys: MotionPose...) {
        self.duration = duration; self.keys = keys
    }

    func sample(_ time: Double) -> MotionPose {
        let cursor = clamp(time / duration) * Double(keys.count - 1)
        let index = min(keys.count - 2, Int(cursor))
        return .blend(keys[index], keys[index + 1], easings.easeInOutCubic(cursor - Double(index)))
    }
}

private let REST = MotionPose()
private let IDLE_GLANCE = MotionClip(4, REST, REST,
    MotionPose(yaw: -9, roll: -3, zoom: 1.06),
    MotionPose(yaw: 6, roll: 2, zoom: 1.03), REST)
private let IDLE_CLIPS = [
    IDLE_GLANCE, IDLE_GLANCE, IDLE_GLANCE,
    MotionClip(6, REST, MotionPose(yaw: -16, pitch: -18, roll: -10),
               MotionPose(yaw: -16, pitch: -18, roll: -10),
               MotionPose(yaw: -30, pitch: 8), MotionPose(yaw: 8, pitch: 20), REST),
    MotionClip(6, REST, MotionPose(yaw: -30, pitch: -16, roll: -11, x: -0.14, y: 0.03),
               MotionPose(yaw: -30, pitch: -14, roll: -12, x: -0.14, y: 0.03),
               MotionPose(yaw: 14, pitch: 10, roll: 11, x: -0.07, y: -0.16),
               MotionPose(yaw: 32, pitch: -24, roll: 15),
               MotionPose(yaw: 360), MotionPose(yaw: 360))
]
private let WORK_CLIPS = [
    MotionClip(2.8, REST, MotionPose(yaw: -17, pitch: 7, roll: -7, x: -0.04), REST),
    MotionClip(3.4, REST, MotionPose(yaw: 14, pitch: -11, roll: 6, y: -0.045), REST),
    MotionClip(2.6, REST, MotionPose(pitch: 13, y: 0.04, stretch: -0.04), REST),
    MotionClip(3.2, REST, MotionPose(yaw: -11, roll: 9), MotionPose(yaw: 9, roll: -5), REST),
    MotionClip(2.4, REST, MotionPose(pitch: -12, zoom: 1.035), MotionPose(pitch: 5), REST),
    MotionClip(3.6, REST, MotionPose(yaw: 18, pitch: 6, x: 0.055), REST)
]
private let TALK_CLIPS = [
    MotionClip(1.4, REST, MotionPose(pitch: 9, y: -0.055, stretch: 0.045), REST),
    MotionClip(1.8, REST, MotionPose(yaw: 10, roll: 6, x: 0.045), MotionPose(yaw: -6, roll: -4), REST),
    MotionClip(1.6, REST, MotionPose(pitch: -6, y: -0.035, zoom: 1.04), REST)
]
private let HAPPY_CLIPS = [
    MotionClip(2.1, REST, MotionPose(pitch: -9, roll: -9, y: -0.13, stretch: 0.08), REST),
    MotionClip(2.4, REST, MotionPose(yaw: -16, roll: -10, x: -0.06), MotionPose(yaw: 16, roll: 10, x: 0.06), REST),
    MotionClip(2.8, REST, MotionPose(pitch: -12, y: -0.07, zoom: 1.07), REST)
]
private let WAIT_CLIPS = [
    MotionClip(4.6, REST, MotionPose(yaw: -14, roll: -8, x: -0.035), REST),
    MotionClip(5.1, REST, MotionPose(pitch: -10, zoom: 1.035), REST),
    MotionClip(4.2, REST, MotionPose(yaw: 12, roll: 6, lid: 0.65), REST)
]

extension MoodID {
    var clips: [MotionClip] {
        switch self {
        case .idle: IDLE_CLIPS
        case .spawning: [IDLE_GLANCE]
        case .working, .thinking, .searching, .writing: WORK_CLIPS
        case .reading: [MotionClip(3.2, REST, MotionPose(yaw: -12, pitch: 9, x: -0.04),
                                  MotionPose(yaw: 12, pitch: 9, x: 0.04), REST)]
        case .streaming, .humming: TALK_CLIPS
        case .done, .happy, .excited, .celebrate, .playful: HAPPY_CLIPS
        case .blocked, .listening, .curious: WAIT_CLIPS
        case .sending: [MotionClip(1.5, REST, MotionPose(yaw: 20, roll: 8, x: 0.12, stretch: -0.04), REST)]
        case .receiving: [MotionClip(1.7, REST, MotionPose(yaw: -18, roll: -8, x: -0.1), REST)]
        case .waking: [MotionClip(1.6, MotionPose(pitch: 16, y: 0.06, lid: 0.1),
                                 MotionPose(pitch: -16, y: -0.1, stretch: 0.1), REST)]
        case .sleeping: [MotionClip(5, MotionPose(roll: 10, y: 0.08, lid: 0),
                                   MotionPose(pitch: 4, roll: 12, y: 0.1, stretch: -0.025, lid: 0),
                                   MotionPose(roll: 10, y: 0.08, lid: 0))]
        case .drowsy, .bored: [MotionClip(5.8, REST, MotionPose(pitch: 14, roll: 8, y: 0.07, lid: 0.22), REST)]
        case .failed, .sad: [MotionClip(4.5, REST, MotionPose(pitch: 15, roll: -8, y: 0.09, stretch: -0.05), REST)]
        case .interrupted, .confused: [MotionClip(3.5, REST, MotionPose(yaw: -14, roll: -14),
                                               MotionPose(yaw: 10, roll: 12), REST)]
        case .laughing: [MotionClip(1.2, REST, MotionPose(pitch: -12, y: -0.08, stretch: -0.05),
                                   MotionPose(pitch: 10, y: 0.03, stretch: 0.05), REST)]
        case .proud: [MotionClip(4, REST, MotionPose(pitch: -15, y: -0.04, zoom: 1.05), REST)]
        case .shy: [MotionClip(4, REST, MotionPose(yaw: -20, pitch: 10, roll: -12, zoom: 0.94), REST)]
        case .surprised: [MotionClip(2, REST, MotionPose(pitch: -14, y: -0.12, stretch: 0.12), REST)]
        case .suspicious: [MotionClip(4, REST, MotionPose(yaw: 20, roll: -7, lid: 0.65), REST)]
        case .angry: [MotionClip(2.2, REST, MotionPose(yaw: -9, pitch: 12, stretch: -0.05),
                                MotionPose(yaw: 9, pitch: 12, zoom: 1.04), REST)]
        case .scared: [MotionClip(1.5, REST, MotionPose(yaw: -8, roll: -6, x: -0.035, zoom: 0.93),
                                 MotionPose(yaw: 8, roll: 6, x: 0.035, zoom: 0.93), REST)]
        case .approving: [MotionClip(1.8, REST, MotionPose(pitch: 18, y: 0.035), REST)]
        }
    }
}

public struct MoodFrame: Sendable {
    public let mood: MoodID
    public let motion: MotionPose
    public let elapsed: Double
}

struct SpawnPose {
    var amount = 0.0
    var spin = 0.0
}

private func springStep(_ time: Double) -> Double {
    guard time > 0 else { return 0 }
    let phase = time * 14
    return 1 - (1 + phase) * exp(-phase)
}

struct SpawnBlend {
    private var last = SpawnPose()
    private var from = SpawnPose()
    private var exited: Double?

    mutating func sample(elapsed: Double?, now: Double) -> SpawnPose {
        if let elapsed {
            let cycle = max(0, Int(elapsed / 3.5))
            var amount = 0.0
            for index in max(0, cycle - 3)...cycle {
                let time = elapsed - Double(index) * 3.5
                amount += springStep(time) - springStep(time - 2)
            }
            let time = elapsed - Double(cycle) * 3.5
            last = SpawnPose(amount: clamp(amount),
                             spin: 180 * (springStep(time) + springStep(time - 2)))
            exited = nil
        } else if last.amount > 0.0001 {
            if exited == nil { exited = now; from = last }
            let progress = springStep(now - exited!)
            last = SpawnPose(amount: from.amount * (1 - progress),
                             spin: lerp(from.spin, 360, progress))
        } else { last = SpawnPose() }
        return last
    }
}

public struct MoodPlayer {
    private var requested: MoodID?
    private var active: MoodID?
    private var entered = 0.0
    private var touched = 0.0
    private var began = 0.0
    private var transition = 0.0
    private var previous = REST
    private var sampled = REST
    private var clip = IDLE_CLIPS[0]
    private var bag: [Int] = []
    private var lastIndex: Int?
    private var seed: UInt64

    public init(seed: UInt64 = UInt64.random(in: 1...UInt64.max)) { self.seed = seed }

    public mutating func sample(_ mood: MoodID, now: Double, interaction: Bool = false) -> MoodFrame {
        if requested != mood {
            requested = mood; entered = now; touched = now
        }
        if interaction { touched = now }
        let age = max(0, now - entered)
        var target = mood
        if mood == .idle {
            if active == .sleeping, interaction { target = .waking }
            else if active == .waking, now - transition < 1.6 { target = .waking }
            else if now - touched >= 180 { target = .sleeping }
        } else if mood == .blocked, age >= 120 { target = .drowsy }
        else if [.done, .celebrate].contains(mood), age >= 8 { target = .idle }
        else if [.failed, .interrupted].contains(mood), age >= 6 { target = .idle }
        else if mood == .waking, age >= 1.6 { target = .idle }

        if active != target || now < began {
            previous = sampled; active = target; transition = now; began = now
            bag = []; lastIndex = nil; clip = pick(target)
        }
        while now - began >= clip.duration {
            began += clip.duration
            clip = pick(target)
        }
        sampled = .blend(previous, clip.sample(now - began),
                         easings.easeOutQuint(clamp((now - transition) / 0.3)))
        return MoodFrame(mood: target, motion: sampled, elapsed: age)
    }

    private mutating func pick(_ mood: MoodID) -> MotionClip {
        let clips = mood.clips
        if bag.isEmpty {
            bag = Array(clips.indices)
            for index in bag.indices.reversed() where index > 0 {
                seed = seed &* 6364136223846793005 &+ 1442695040888963407
                bag.swapAt(index, Int(seed % UInt64(index + 1)))
            }
            if bag.count > 1, bag.last == lastIndex { bag.swapAt(0, bag.count - 1) }
        }
        let index = bag.removeLast()
        lastIndex = index
        return clips[index]
    }
}
