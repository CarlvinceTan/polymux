import CoreGraphics
import Foundation

// DS Bot styles.js keyframes, in normalized body-box coordinates.
struct MascotPose: Equatable, Sendable {
    var x = 0.0, y = 0.0, roll = 0.0, sx = 1.0, sy = 1.0

    func transform(size: Double = 200, origin: CGPoint = .zero) -> CGAffineTransform {
        CGAffineTransform(translationX: origin.x + x * size, y: origin.y + y * size)
            .rotated(by: roll * .pi / 180).scaledBy(x: sx, y: sy)
            .translatedBy(x: -origin.x, y: -origin.y)
    }

    /// This flat pose, pivoting a fifth of the radius below the middle, as a turn, offset and squash.
    var clipPose: (rot: [Double], pos: [Double], scl: [Double]) {
        let angle = roll * .pi / 180
        let cx = 0.2 * sy * sin(angle) + 2 * x, cy = 0.2 - 0.2 * sy * cos(angle) + 2 * y
        return ((Rotation(yaw: 0, pitch: 0, roll: roll) * Rotation(turn: REST_TURN)).turn, [cx, -cy], [sx, sy])
    }

    var values: [Double] { [x, y, roll, sx, sy] }

    init(x: Double = 0, y: Double = 0, roll: Double = 0, sx: Double = 1, sy: Double = 1) {
        self.x = x; self.y = y; self.roll = roll; self.sx = sx; self.sy = sy
    }

    init(_ values: [Double]) {
        self.init(x: values[0], y: values[1], roll: values[2], sx: values[3], sy: values[4])
    }

    static func blend(_ a: MascotPose, _ b: MascotPose, _ t: Double) -> MascotPose {
        MascotPose(x: lerp(a.x, b.x, t), y: lerp(a.y, b.y, t), roll: lerp(a.roll, b.roll, t),
                   sx: lerp(a.sx, b.sx, t), sy: lerp(a.sy, b.sy, t))
    }
}

/// Interruptible interactions keep the pose and momentum already on screen.
struct MascotPlayer {
    private enum Move { case rest, pet, spin }
    private var move = Move.rest
    private var started = 0.0
    private var winding = 0.0
    private var blends = [MotionBlend](repeating: MotionBlend(), count: 5)
    private var petted = false
    private var idle = true
    private let duration = 0.28

    mutating func pet(_ active: Bool, _ now: Double) {
        finish(now)
        guard active != petted else { return }
        petted = active
        if move != .spin { change(idle && petted ? .pet : .rest, now) }
    }

    mutating func spin(_ now: Double) {
        finish(now)
        change(.spin, now)
    }

    func settled(_ now: Double) -> Bool { move == .rest && now - started >= duration }

    mutating func sample(_ now: Double, idle: Bool) -> MascotPose {
        finish(now)
        if idle != self.idle {
            self.idle = idle
            if move != .spin { change(idle && petted ? .pet : .rest, now) }
        }
        return pose(now)
    }

    private mutating func finish(_ now: Double) {
        if move == .spin, now >= started + 0.8 {
            change(idle && petted ? .pet : .rest, started + 0.8)
        }
    }

    private func base(_ elapsed: Double) -> MascotPose {
        switch move {
        case .rest: MascotPose()
        case .pet: MascotMotion.pet(max(0, elapsed))
        case .spin: MascotMotion.spin(max(0, elapsed))
        }
    }

    private func pose(_ now: Double) -> MascotPose {
        var values = base(now - started).values
        values[2] += winding
        return MascotPose(zip(values, blends).map { $0 + $1.sample(now - started, duration: duration) })
    }

    private mutating func change(_ next: Move, _ now: Double) {
        let step = 0.0001
        let from = pose(now).values, before = pose(now - step).values
        move = next; started = now
        let to = base(0).values, after = base(step).values
        winding = ((from[2] - to[2]) / 360).rounded() * 360
        blends = from.indices.map { index in
            MotionBlend(offset: from[index] - to[index] - (index == 2 ? winding : 0),
                        velocity: (from[index] - before[index] - after[index] + to[index]) / step)
        }
    }
}

func mascotEase(_ t: Double, _ x1: Double = 0.42, _ y1: Double = 0,
                _ x2: Double = 0.58, _ y2: Double = 1) -> Double {
    let t = clamp(t)
    func bezier(_ u: Double, _ a: Double, _ b: Double) -> Double {
        3 * (1 - u) * (1 - u) * u * a + 3 * (1 - u) * u * u * b + u * u * u
    }
    var lower = 0.0, upper = 1.0
    for _ in 0..<22 {
        let mid = (lower + upper) / 2
        if bezier(mid, x1, x2) < t { lower = mid } else { upper = mid }
    }
    return t == 0 || t == 1 ? t : bezier((lower + upper) / 2, y1, y2)
}

enum MascotMotion {
    static func keys(_ time: Double, _ keys: [(Double, MascotPose)], poke: Bool = false) -> MascotPose {
        let t = clamp(time)
        for index in 1..<keys.count where t <= keys[index].0 {
            let a = keys[index - 1], b = keys[index]
            let k = (t - a.0) / (b.0 - a.0)
            return .blend(a.1, b.1, poke ? mascotEase(k, 0.3, 0.7, 0.4, 1) : mascotEase(k))
        }
        return keys.last!.1
    }

    static func spin(_ elapsed: Double) -> MascotPose {
        keys(elapsed / 0.8, [(0, MascotPose()), (0.55, MascotPose(y: -0.16, roll: 360)),
                             (0.78, MascotPose(roll: 360, sx: 1.08, sy: 0.92)), (1, MascotPose(roll: 360))], poke: true)
    }

    static func pet(_ elapsed: Double) -> MascotPose {
        let rest = MascotPose(x: 0.05, y: -0.01, roll: 8)
        if elapsed < 0.32 { return .blend(MascotPose(), rest, mascotEase(elapsed / 0.32)) }
        let t = (elapsed - 0.32).truncatingRemainder(dividingBy: 0.78) / 0.78
        return keys(t, [(0, rest), (0.5, MascotPose(x: 0.08, y: -0.03, roll: 14, sx: 1.03, sy: 0.97)), (1, rest)])
    }

    static func idleKeys(_ seed: UInt32) -> [(Double, MascotPose)] {
        switch seed % 3 {
        case 0:
            [(0, MascotPose()), (0.03, MascotPose(y: -0.1, sx: 0.97, sy: 1.04)),
             (0.055, MascotPose(sx: 1.05, sy: 0.95)), (0.08, MascotPose()), (1, MascotPose())]
        case 1:
            [(0, MascotPose()), (0.025, MascotPose(roll: -8)), (0.05, MascotPose(roll: 7)),
             (0.075, MascotPose(roll: -3)), (0.1, MascotPose()), (1, MascotPose())]
        default:
            [(0, MascotPose()), (0.04, MascotPose(sx: 0.94, sy: 1.08)),
             (0.07, MascotPose(sx: 1.03, sy: 0.97)), (0.09, MascotPose()), (1, MascotPose())]
        }
    }

    /// The avatar's idle hop as a clip: the move, then a short rest.
    static func idleRecipe(_ seed: UInt32) -> Recipe {
        let move = idleKeys(seed).dropLast().map { ($0.0 * 9, $0.1) }
        return recipe("hop-\(seed % 3)", duration: (move.last?.0 ?? 0) + 1.6, keys: Array(move))
    }

    static func workingRecipe(_ shape: ShapeID?) -> Recipe {
        let loop = workingLoop(shape)
        return recipe("wiggle", duration: loop.period, keys: loop.keys.map { ($0.0 * loop.period, $0.1) })
    }

    /// Flat keyframes as a Grok-style clip around the resting look.
    private static func recipe(_ name: String, duration: Double, keys: [(Double, MascotPose)]) -> Recipe {
        let poses = keys.map { ($0.0, $0.1.clipPose) }
        func track(_ value: ((rot: [Double], pos: [Double], scl: [Double])) -> [Double]) -> [Key] {
            poses.map { Key(t: $0.0, v: value($0.1)) }
        }
        return Recipe(name: name, duration: duration, position: track(\.pos), rotation: track(\.rot),
                      squash: track(\.scl))
    }

    static func workingLoop(_ shape: ShapeID?) -> (period: Double, keys: [(Double, MascotPose)]) {
        switch shape {
        case .star, .blossom, .plus, .gem, .magnet, .trophy:
            (1.4, [(0, MascotPose()), (0.25, MascotPose(roll: -12, sx: 1.04, sy: 1.04)),
                   (0.75, MascotPose(roll: 12, sx: 1.04, sy: 1.04)), (1, MascotPose())])
        case .popcorn, .peanut, .squircle, .moon, .wave:
            (1.2, [(0, MascotPose()), (0.3, MascotPose(sx: 1.08, sy: 0.9)),
                   (0.55, MascotPose(y: -0.04, sx: 0.94, sy: 1.08)),
                   (0.8, MascotPose(sx: 1.02, sy: 0.98)), (1, MascotPose())])
        default:
            (1.6, [(0, MascotPose()), (0.3, MascotPose(y: -0.04, roll: -5)),
                   (0.65, MascotPose(roll: 4)), (1, MascotPose())])
        }
    }
}
