import Foundation

public let EYE_SPLIT = 15.46
public let EYE_W = 0.186
public let EYE_H = 0.412
public let REST_GAZE = HeadGaze(yaw: 28.49, pitch: 28.62, roll: -13)

public struct HeadGaze: Hashable, Sendable {
    public var yaw: Double
    public var pitch: Double
    public var roll: Double
    public init(yaw: Double = 0, pitch: Double = 0, roll: Double = 0) {
        self.yaw = yaw; self.pitch = pitch; self.roll = roll
    }
}

public struct EyePose: Hashable, Sendable {
    public var x: Double
    public var y: Double
    public var a: Double
    public var b: Double
    public var c: Double
    public var d: Double
    public var depth: Double
    public init(x: Double, y: Double, a: Double, b: Double, c: Double, d: Double, depth: Double) {
        self.x = x; self.y = y; self.a = a; self.b = b; self.c = c; self.d = d; self.depth = depth
    }
}

public struct EyeCfg: Hashable, Sendable {
    public var w: Double
    public var h: Double
    public var open: Double
    public var tilt: Double
    public init(w: Double, h: Double, open: Double = 1, tilt: Double = 0) {
        self.w = w; self.h = h; self.open = open; self.tilt = tilt
    }
}

private struct Vec3 {
    var x: Double
    var y: Double
    var z: Double
}

private func spin(_ u: Vec3, _ v: Vec3, _ angle: Double) -> (Vec3, Vec3) {
    let c = cos(angle), s = sin(angle)
    return (Vec3(x: u.x * c + v.x * s, y: u.y * c + v.y * s, z: u.z * c + v.z * s),
            Vec3(x: v.x * c - u.x * s, y: v.y * c - u.y * s, z: v.z * c - u.z * s))
}

public func eyePoses(_ gaze: HeadGaze, _ scale: Double, _ split: Double = EYE_SPLIT) -> [EyePose] {
    var f = Vec3(x: 0, y: 0, z: 1)
    var right = Vec3(x: 1, y: 0, z: 0)
    var down = Vec3(x: 0, y: 1, z: 0)
    (f, right) = spin(f, right, gaze.yaw * .pi / 180)
    (down, f) = spin(down, f, gaze.pitch * .pi / 180)
    (right, down) = spin(right, down, gaze.roll * .pi / 180)
    return [-1.0, 1.0].map { side in
        let (ef, er) = spin(f, right, split * side * .pi / 180)
        return EyePose(x: ef.x * scale, y: ef.y * scale, a: er.x, b: er.y, c: down.x, d: down.y, depth: ef.z)
    }
}

public struct Liveliness: Hashable, Sendable {
    public var dYaw: Double
    public var dPitch: Double
    public var dRoll: Double
    public var lid: Double
    public var driftX: Double
    public var driftY: Double
    public var breath: Double
}

private let BLINKS: [Double] = {
    let rng = createRng(0x5eed)
    var out: [Double] = []
    var t = 1.4
    while t < 900 {
        out.append(t)
        t += 1.9 + rng() * 2.7
        if rng() < 0.18 { out.append(t); t += 0.24 }
    }
    return out
}()

private let BLINK_DUR = 0.18

private func blinkLid(_ t: Double) -> Double {
    for start in BLINKS {
        if t < start { break }
        let k = (t - start) / BLINK_DUR
        if k >= 0 && k <= 1 { return k < 0.45 ? 1 - k / 0.45 : (k - 0.45) / 0.55 }
    }
    return 1
}

public func liveliness(_ t: Double, wander: Double = 1, blink: Bool = true, float: Bool = true) -> Liveliness {
    Liveliness(dYaw: (loopNoise(t, 11.3, 0.4) * 5.5 + loopNoise(t, 3.7, 2.1) * 1.6) * wander,
               dPitch: (loopNoise(t, 9.1, 1.3) * 4.2 + loopNoise(t, 4.3, 0.7) * 1.3) * wander,
               dRoll: loopNoise(t, 13.7, 3.2) * 2.2 * wander,
               lid: blink ? blinkLid(t) : 1,
               driftX: float ? loopNoise(t, 7.9, 1.9) * 0.006 : 0,
               driftY: float ? loopNoise(t, 5.3, 0.3) * 0.007 : 0,
               breath: float ? 1 + sin(t / 3.4 * TAU) * 0.005 : 1)
}

public func blinkScale(_ lid: Double) -> Double { 0.06 + 0.94 * clamp(lid) }
