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
        case .writing, .listening, .approving: .attentive
        case .blocked: .neutral
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

/// A look away from the resting gaze, as Polymux's own clips write it.
public struct MotionPose: Equatable, Sendable {
    public var yaw, pitch, roll, x, y, stretch, zoom, lid: Double

    public init(yaw: Double = 0, pitch: Double = 0, roll: Double = 0,
                x: Double = 0, y: Double = 0, stretch: Double = 0,
                zoom: Double = 1, lid: Double = 1) {
        self.yaw = yaw; self.pitch = pitch; self.roll = roll
        self.x = x; self.y = y; self.stretch = stretch; self.zoom = zoom; self.lid = lid
    }

    /// The body turn that carries Grok Bot's front-facing eyes to this look; whole turns of yaw are kept.
    var turn: [Double] {
        let turns = (yaw / 360).rounded() * 360
        let facing = HeadGaze(yaw: REST_GAZE.yaw + yaw - turns, pitch: REST_GAZE.pitch + pitch,
                              roll: REST_GAZE.roll + roll)
        var turn = (Rotation(from: REST_GAZE, to: facing) * Rotation(turn: REST_TURN)).turn
        turn[1] += turns
        return turn
    }
}

/// Evenly spaced looks, eased in and out.
struct MotionClip: Sendable {
    let duration: Double
    let keys: [MotionPose]

    init(_ duration: Double, _ keys: MotionPose...) {
        self.duration = duration; self.keys = keys
    }

    func recipe(_ name: String, face: FaceID) -> Recipe {
        let ease = [0.65, 0, 0.35, 1.0]
        let times = keys.indices.map { duration * Double($0) / Double(keys.count - 1) }
        func track(_ value: (MotionPose) -> [Double]) -> [Key] {
            zip(times, keys).map { Key(t: $0, v: value($1), ease: ease) }
        }
        return Recipe(name: name, duration: duration, position: track { [$0.x, -$0.y] }, rotation: track(\.turn),
                      squash: track { [1 - $0.stretch, 1 + $0.stretch] }, zoom: track { [REST_ZOOM * $0.zoom] },
                      lids: keys.allSatisfy { $0.lid == 1 } ? [] : track { [1 - $0.lid] },
                      faces: [FaceEvent(t: 0, face: face)], blinks: duration >= 2.5 ? [duration * 0.45] : [])
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
        case .blocked: []
        case .listening, .curious: WAIT_CLIPS
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
    let pose: ClipFrame
    public let elapsed: Double
}

struct SpawnPose {
    var amount = 0.0
    var spin = 0.0
}

func springStep(_ time: Double) -> Double {
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

extension MoodID {
    /// Grok Bot's clips for this mood, and the face it holds over them.
    var grok: (clips: [String], face: FaceID?) {
        let working = ["Working_A", "Working_B", "Working_C", "Working_D", "Working_E", "Working_F"]
        return switch self {
        case .idle: (["Idle_B", "Idle_B", "Idle_D", "Idle_B", "Idle_A"], nil)
        case .listening: (["Listen_A"], nil)
        case .thinking, .searching, .writing, .sending, .receiving, .spawning: (["Idle_B"], nil)
        case .streaming: (["Streaming_A", "Streaming_B", "Streaming_C"], nil)
        case .reading, .working: (working, nil)
        case .blocked, .confused, .interrupted: (["Blocked_A"], nil)
        case .done: (["Done_B", "Done_A", "Done_D"], .happy)
        case .failed, .sad: (["Blocked_B"], nil)
        case .sleeping, .bored: (["Idle_B"], .sleepy)
        case .waking, .surprised: (["Idle_B"], .surprised)
        case .happy: (["Done_D", "Done_A"], nil)
        case .excited: (["Done_C"], nil)
        case .laughing: (["Done_B"], nil)
        case .proud: (["Marketing_B"], nil)
        case .curious: (["Working_B"], .inspecting)
        case .playful: (["Bodymoves_A"], nil)
        case .shy: (["Idle_D"], .calm)
        case .suspicious: (["Idle_D"], .squinted)
        case .angry: (["Blocked_A"], .squinted)
        case .drowsy: (["Blocked_C"], nil)
        case .scared: (["Idle_D"], .surprised)
        case .celebrate: (["Rotation_A"], .happy)
        case .approving: (["Rotation_C"], nil)
        case .humming: (["Idle_B"], .calm)
        }
    }

    /// Every clip this mood can play: Grok Bot's and Polymux's own, picked between at random.
    /// Small avatars keep to the gentle ones.
    func pool(compact: Bool, seed: UInt32, shape: ShapeID?) -> [Recipe] {
        let face = grok.face ?? expression?.face ?? .neutral
        var pool = (compact && self == .idle ? ["Idle_B"] : grok.clips).compactMap { RECIPES[$0] }
        let own = compact && self == .idle ? [IDLE_GLANCE] : clips
        pool += own.enumerated().map { $1.recipe("\(rawValue)-\($0)", face: face) }
        if self == .idle { pool.append(MascotMotion.idleRecipe(seed)) }
        if self == .working { pool.append(MascotMotion.workingRecipe(shape)) }
        return pool
    }
}

extension ExpressionID {
    var face: FaceID {
        switch self {
        case .neutral, .attentive: .neutral
        case .surprised, .scared: .surprised
        case .excited, .happy, .laughing, .proud: .happy
        case .angry, .suspicious: .squinted
        case .sad: .sad
        case .confused: .confused
        case .curious: .inspecting
        case .shy: .calm
        case .unimpressed, .sleepy: .sleepy
        }
    }
}

/// Picks a mood's clips and plays them, after Grok Bot's state player.
public struct MoodPlayer {
    private var requested: MoodID?
    private var active: MoodID?
    private var entered = 0.0
    private var touched = 0.0
    private var transition = 0.0
    private var clock: Double?
    private var player = ClipPlayer()
    private var pool: [Recipe] = []
    private var bag: [String] = []
    private(set) var playing: String?
    private var started = false
    private var loopMark = 0
    private var compact = false
    private var resting = false
    private var live = false
    private var face: FaceID?
    private var random: UInt32
    /// The avatar's own seed, as Grok Bot seeds from the agent: it picks the idle hop and the clip order.
    var seed: UInt32 = 0 { didSet { if seed != oldValue { random = seed } } }
    var shape: ShapeID?
    /// Where the first clip starts, as a fraction; random by default so avatars side by side fall out of step.
    var firstPhase: Double?

    public init(seed: UInt64 = UInt64.random(in: 1...UInt64.max)) { random = UInt32(truncatingIfNeeded: seed) }

    /// Holds a resting avatar's gaze on a point, x right and y up in -1...1; nil lets it go.
    mutating func setGaze(_ point: [Double]?) { player.setGaze(point, body: shape) }

    var settled: Bool { player.settled }

    /// `live` avatars advance in real time, so they ease into a rest instead of being placed there.
    public mutating func sample(_ mood: MoodID, now: Double, interaction: Bool = false,
                                compact: Bool = false, resting: Bool = false, petted: Bool = false,
                                live: Bool = false) -> MoodFrame {
        self.live = live
        if requested != mood {
            requested = mood; entered = now; touched = now
        }
        if interaction { touched = now }
        let age = max(0, now - entered)
        var target = mood
        if !resting {
            if mood == .idle {
                if active == .sleeping, interaction { target = .waking }
                else if active == .waking, now - transition < 1.6 { target = .waking }
                else if now - touched >= 180 { target = .sleeping }
            } else if mood == .blocked, age >= 120 { target = .drowsy }
            else if [.done, .celebrate].contains(mood), age >= 8 { target = .idle }
            else if [.failed, .interrupted].contains(mood), age >= 6 { target = .idle }
            else if mood == .waking, age >= 1.6 { target = .idle }
        }

        if let clock, now < clock {
            player = ClipPlayer(); active = nil; started = false; self.clock = nil
        }
        if active != target || compact != self.compact || resting != self.resting {
            if active != target { transition = now }
            active = target; self.compact = compact; self.resting = resting
            enter(target)
        }
        player.hold(petted ? .happy : face)
        var remaining = clock.map { max(0, now - $0) } ?? 0
        clock = now
        while remaining > 1e-9 {
            let step = min(remaining, 0.1)
            player.advance(step)
            if !resting && player.loopCount != loopMark { next() }
            remaining -= step
        }
        return MoodFrame(mood: target, pose: player.frame, elapsed: age)
    }

    private mutating func enter(_ mood: MoodID) {
        pool = mood.pool(compact: compact, seed: seed, shape: shape)
        bag = []
        playing = nil
        if resting {
            // A still avatar rests facing up and to the right, wearing its mood's face.
            face = mood.grok.face ?? pool.first.map { restPose($0).face }
            player.hold(face)
            player.rest()
            if !live || !started { for _ in 0..<12 { player.advance(0.1) } }
            return
        }
        face = mood.grok.face
        player.hold(face)
        next()
    }

    private mutating func next() {
        if bag.isEmpty { bag = shuffled(pool.map(\.name)) }
        loopMark = player.loopCount
        let name = bag.removeFirst()
        guard name != playing, let recipe = pool.first(where: { $0.name == name }) else { return }
        player.load(recipe, phase: started ? 0 : firstPhase ?? roll())
        started = true
        playing = name
        loopMark = player.loopCount
    }

    /// Shuffled, never starting with the clip that just played.
    private mutating func shuffled(_ names: [String]) -> [String] {
        var out = names
        for index in out.indices.reversed() where index > 0 {
            out.swapAt(index, Int(roll() * Double(index + 1)))
        }
        if let first = out.firstIndex(where: { $0 != playing }), first > 0 { out.swapAt(0, first) }
        return out
    }

    private mutating func roll() -> Double {
        random = random &+ 0x6d2b79f5
        var t = (random ^ (random >> 15)) &* (1 | random)
        t = (t &+ ((t ^ (t >> 7)) &* (61 | t))) ^ t
        return Double(t ^ (t >> 14)) / 4294967296
    }
}
