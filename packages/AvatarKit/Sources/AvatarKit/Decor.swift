import CoreGraphics
import Foundation

public struct DotRender: @unchecked Sendable {
    public var x: Double
    public var y: Double
    public var r: Double
    public var opacity: Double
    public var color: String?
    public var depth: Double?
    public var d: CGPath?
    public var rot: Double?

    public init(x: Double, y: Double, r: Double, opacity: Double, color: String? = nil,
                depth: Double? = nil, d: CGPath? = nil, rot: Double? = nil) {
        self.x = x; self.y = y; self.r = r; self.opacity = opacity
        self.color = color; self.depth = depth; self.d = d; self.rot = rot
    }
}

public struct ArcSeed: Sendable {
    public var a, k, tilt, speed, phase, sweep, hue, hueSpan, width, cx, cy: Double
    public init(a: Double, k: Double, tilt: Double, speed: Double, phase: Double,
                sweep: Double, hue: Double, hueSpan: Double, width: Double,
                cx: Double, cy: Double) {
        self.a = a; self.k = k; self.tilt = tilt; self.speed = speed; self.phase = phase
        self.sweep = sweep; self.hue = hue; self.hueSpan = hueSpan; self.width = width
        self.cx = cx; self.cy = cy
    }
}

public struct ArcSpec: Sendable {
    public var id: String
    public var seed: ArcSeed
    public var t, opacity: Double
    public init(id: String, seed: ArcSeed, t: Double, opacity: Double) {
        self.id = id; self.seed = seed; self.t = t; self.opacity = opacity
    }
}

public struct ArcGradient: Sendable {
    public var x1, y1, x2, y2: Double
    public var stops: [String]
}

public struct ArcRender: @unchecked Sendable {
    public var id: String
    public var front, back: CGPath
    public var width, opacity: Double
    public var grad: ArcGradient
}

public func wheel(_ hue: Double, _ s: Double = 0.55, _ l: Double = 0.62) -> String {
    let h = ((hue.truncatingRemainder(dividingBy: 360)) + 360).truncatingRemainder(dividingBy: 360)
    let c = (1 - abs(2 * l - 1)) * s
    let x = c * (1 - abs((h / 60).truncatingRemainder(dividingBy: 2) - 1))
    let m = l - c / 2
    let rgb: [Double]
    if h < 60 { rgb = [c, x, 0] }
    else if h < 120 { rgb = [x, c, 0] }
    else if h < 180 { rgb = [0, c, x] }
    else if h < 240 { rgb = [0, x, c] }
    else if h < 300 { rgb = [x, 0, c] }
    else { rgb = [c, 0, x] }
    return "#" + rgb.map { String(format: "%02x", Int(floor(($0 + m) * 255 + 0.5))) }.joined()
}

public func arcRender(_ seed: ArcSeed, _ t: Double, _ scale: Double,
                      _ id: String, _ opacity: Double = 1) -> ArcRender {
    let spin = seed.phase + t * seed.speed * TAU
    let cu = cos(seed.tilt), su = sin(seed.tilt)
    let kz = sqrt(max(0, 1 - seed.k * seed.k))
    let front = CGMutablePath(), back = CGMutablePath()
    var previous: Bool?
    for i in 0...64 {
        let th = spin + Double(i) / 64 * seed.sweep * TAU
        let ct = cos(th), st = sin(th)
        let x = seed.a * (ct * cu - st * su * seed.k) + seed.cx
        let y = seed.a * (ct * su + st * cu * seed.k) + seed.cy
        let behind = seed.a * st * kz < 0
        let point = CGPoint(x: r2(x * scale), y: r2(y * scale))
        let path = behind ? back : front
        if previous != behind { path.move(to: point) } else { path.addLine(to: point) }
        previous = behind
    }
    let gx = cos(seed.tilt) * seed.a * scale
    let gy = sin(seed.tilt) * seed.a * scale
    return ArcRender(id: id, front: front.copy()!, back: back.copy()!,
                     width: seed.width * scale, opacity: opacity,
                     grad: ArcGradient(x1: r2(seed.cx * scale - gx), y1: r2(seed.cy * scale - gy),
                                       x2: r2(seed.cx * scale + gx), y2: r2(seed.cy * scale + gy),
                                       stops: [wheel(seed.hue), wheel(seed.hue + seed.hueSpan * 0.5),
                                               wheel(seed.hue + seed.hueSpan)]))
}

public let RINGS: [ArcSeed] = {
    let rng = createRng(0xa11ce)
    return (0..<6).map { i in
        ArcSeed(a: 1.3 + rng() * 0.1, k: 0.05 + rng() * 0.4,
                tilt: Double(i) / 6 * .pi + rng() * 0.5, speed: 3 + rng() * 0.7,
                phase: rng() * TAU, sweep: 0.6 + rng() * 0.25,
                hue: Double(i) * 360 / 6 + rng() * 30, hueSpan: 60 + rng() * 60,
                width: 0.05 + rng() * 0.012, cx: 0, cy: 0.1)
    }
}()

public let SWOOSH: [ArcSeed] = (0..<4).map { i in
    ArcSeed(a: 0.78 + Double(i) * 0.2, k: 0.05 + Double(i) * 0.02,
            tilt: -0.62 + Double(i) * 0.05, speed: 0.3, phase: 0.06 * Double(i),
            sweep: 0.4, hue: 95 + Double(i) * 62, hueSpan: 100,
            width: 0.05, cx: 0, cy: -0.12)
}

public let DOT_PEAK = 1.25

private struct Particle: Sendable { var birth, angle, rho: Double }
private let PARTICLES: [Particle] = {
    let rng = createRng(0xbeef)
    return (0..<5).map { Particle(birth: Double($0) * 0.2, angle: rng() * TAU, rho: 0.58 + rng() * 0.18) }
}()

public func particles(_ t: Double, _ scale: Double) -> [DotRender] {
    PARTICLES.compactMap { p in
        let u = t - p.birth
        guard u >= 0, u <= 0.62 else { return nil }
        let rho = p.rho * pow(0.75, u * 10)
        let a = p.angle + u * 100 * .pi / 180
        return DotRender(x: cos(a) * rho * scale, y: sin(a) * rho * scale,
                         r: (0.04 + 0.028 * clamp(u / 0.55)) * scale,
                         opacity: clamp(u / 0.06) * clamp((0.62 - u) / 0.08),
                         depth: clamp(1 - rho / 0.8))
    }
}

public let COMET_RIBBONS: [ArcSeed] = {
    let rng = createRng(0xc0e7)
    return (0..<4).map { i in
        let d = Double(i) - 1.5
        return ArcSeed(a: 0.85 * (1 + d * 0.03), k: (0.15 / 0.85) * (1 + d * 0.16),
                       tilt: 34 * .pi / 180 + d * 0.035, speed: 210 / 360,
                       phase: -Double(i) * 0.045 + rng() * 0.012, sweep: 0.34,
                       hue: Double(i) * 85 + rng() * 20, hueSpan: 80,
                       width: 0.095, cx: 0, cy: 0)
    }
}()

public let COMET_DOT = 0.129
public let NOTIF_ANGLE = -42.0
public let NOTIF_DIST = 1.003
public let NOTIF_R = 0.15
public let NOTIF_POP = 1.14
public let NOTIF_MARGIN = 0.054
