import CoreGraphics
import Foundation

/// The 3D body a flat outline stands for, in units of the outline's radius.
public enum Solid: Hashable, Sendable {
    /// Rounded all over: the outline inflated to `depth` at its centre.
    case inflated(depth: Double)
    case loft(depth: Double)
    /// Flat front and back `depth` from the middle, with edges rounded by `bevel`.
    case slab(depth: Double, bevel: Double)
    /// The profile revolved around its vertical axis.
    case revolved
    case pill
    case pumpkin

    /// How far in front of the middle the surface sits at `fraction` of the way out to the edge.
    func front(_ fraction: Double) -> Double {
        switch self {
        case .inflated(let depth): depth * sqrt(max(0, 1 - fraction * fraction))
        case .loft(let depth): depth * sqrt(max(0, 1 - pow(fraction, 2 / 0.72)))
        case .slab(let depth, _): depth
        case .revolved: sqrt(max(0, 1 - fraction * fraction))
        case .pill: sqrt(max(0, 1 - fraction * fraction))
        case .pumpkin: Pumpkin.front(fraction, 0)
        }
    }
}

/// How far the body is turned, in degrees: yaw turns the front right, pitch turns it down.
public struct Turn: Hashable, Sendable {
    public var yaw: Double
    public var pitch: Double
    public init(yaw: Double = 0, pitch: Double = 0) { self.yaw = yaw; self.pitch = pitch }
}

/// A one-off body movement, as in Grok Bot.
public enum Poke: Hashable, Sendable {
    case spin(turns: Int)
    case bounce
    case nod

    var duration: Double {
        switch self {
        case .spin(let turns): 0.9 + 0.4 * Double(max(1, turns) - 1)
        case .bounce: 0.62
        case .nod: 0.5
        }
    }
}

extension ShapeID {
    public var solid: Solid {
        switch self {
        case .lemon, .orangeFruit, .watermelon, .avocado, .cherry, .cookie, .cheese, .egg, .candy, .donut,
             .tulip, .sunflower, .sprout, .fern, .balloon, .shell, .palm, .snowman, .lightning, .planet,
             .mug, .lantern, .gift, .pencil, .hammer, .umbrella, .wateringCan, .gamepad, .guitar, .telescope,
             .whale, .bull, .moon, .wave, .blossom, .popcorn, .peanut, .saucer, .beacon,
             .plus, .magnet, .acorn, .pebbles, .bulb, .trophy, .gem,
             .hamburger, .carrot, .pizza, .sushi, .iceCream, .sun, .leaf, .tree, .cactus,
             .mushroom, .mountain, .volcano, .rainbow, .snowflake, .rocket, .robot, .camera,
             .clock, .key, .bell, .book, .teapot:
            character!.solid
        case .circle: .inflated(depth: 1)
        case .pebble: .inflated(depth: 0.88)
        case .droplet, .wedge: .revolved
        case .capsule: .inflated(depth: 0.62)
        case .tablet: .pill
        case .cloud: .loft(depth: 0.72)
        case .triangle: .inflated(depth: 0.6)
        case .squircle: .slab(depth: 0.3, bevel: 0.12)
        case .hexagon, .pentagon, .square, .sparkle, .clover, .flower, .house:
            .slab(depth: 0.36, bevel: 0.22)
        case .star: .slab(depth: 0.3, bevel: 0.18)
        case .burst: .slab(depth: 0.32, bevel: 0.16)
        case .heart: .loft(depth: 0.62)
        case .pumpkin, .apple, .pear, .strawberry, .bun, .toast, .cupcake, .cat, .dog, .panda, .rabbit:
            character!.solid
        case .fox, .crab, .elephant, .octopus, .pig, .deer, .koala, .giraffe, .cow, .turtle,
             .tiger, .frog, .mouse, .walrus, .beaver, .guineaPig, .chinchilla, .ferret, .monkey,
             .chick, .duck, .penguin, .bat, .axolotl, .goose:
            character!.solid
        }
    }
}

/// A 3D rotation, applied to points with x right, y down and z towards the viewer.
public struct Rotation: Hashable, Sendable {
    var m: [Double]

    public static let identity = Rotation(m: [1, 0, 0, 0, 1, 0, 0, 0, 1])
    public static let rest = Rotation(yaw: 33.293, pitch: -15.896, roll: -25.385)

    /// Turns the front right by `yaw` degrees, then down by `pitch` degrees.
    public init(yaw: Double, pitch: Double) {
        let cy = cos(yaw * .pi / 180), sy = sin(yaw * .pi / 180)
        let cp = cos(pitch * .pi / 180), sp = sin(pitch * .pi / 180)
        let turnYaw = Rotation(m: [cy, 0, sy, 0, 1, 0, -sy, 0, cy])
        let turnPitch = Rotation(m: [1, 0, 0, 0, cp, sp, 0, -sp, cp])
        self = turnPitch * turnYaw
    }

    public init(yaw: Double, pitch: Double, roll: Double) {
        let cy = cos(yaw * .pi / 180), sy = sin(yaw * .pi / 180)
        let cp = cos(pitch * .pi / 180), sp = sin(pitch * .pi / 180)
        let cr = cos(roll * .pi / 180), sr = sin(roll * .pi / 180)
        let turnYaw = Rotation(m: [cy, 0, sy, 0, 1, 0, -sy, 0, cy])
        let turnPitch = Rotation(m: [1, 0, 0, 0, cp, sp, 0, -sp, cp])
        let turnRoll = Rotation(m: [cr, -sr, 0, sr, cr, 0, 0, 0, 1])
        self = turnRoll * turnYaw * turnPitch
    }

    init(m: [Double]) { self.m = m }

    /// Grok Bot's x, y, z turn in degrees, made in its y-up space.
    init(turn: [Double]) { self.init(yaw: turn[1], pitch: turn[0], roll: -turn[2]) }

    /// The Grok Bot turn that makes this rotation.
    var turn: [Double] {
        [-atan2(m[7], m[8]), asin(max(-1, min(1, -m[6]))), -atan2(m[3], m[0])].map { $0 * 180 / .pi }
    }

    /// Carries a head facing `from` round to face `to`.
    init(from: HeadGaze, to: HeadGaze) {
        let a = headFrame(from), b = headFrame(to)
        func columns(_ f: (right: Vec3, down: Vec3, f: Vec3)) -> Rotation {
            Rotation(m: [f.right.x, f.down.x, f.f.x, f.right.y, f.down.y, f.f.y, f.right.z, f.down.z, f.f.z])
        }
        self = columns(b) * columns(a).transposed
    }

    var transposed: Rotation { Rotation(m: [m[0], m[3], m[6], m[1], m[4], m[7], m[2], m[5], m[8]]) }

    var isIdentity: Bool {
        zip(m, Rotation.identity.m).allSatisfy { abs($0 - $1) < 1e-6 }
    }

    static func * (a: Rotation, b: Rotation) -> Rotation {
        var out = [Double](repeating: 0, count: 9)
        for r in 0..<3 { for c in 0..<3 { for k in 0..<3 { out[r * 3 + c] += a.m[r * 3 + k] * b.m[k * 3 + c] } } }
        return Rotation(m: out)
    }

    func apply(_ v: Vec3) -> Vec3 {
        Vec3(x: m[0] * v.x + m[1] * v.y + m[2] * v.z,
             y: m[3] * v.x + m[4] * v.y + m[5] * v.z,
             z: m[6] * v.x + m[7] * v.y + m[8] * v.z)
    }
}

/// Body movement from active pokes at `now`: extra rotation, lift (radius units, up) and squash.
func pokeMotion(_ pokes: [(poke: Poke, at: Double)], _ now: Double) -> (rotation: Rotation, lift: Double, squash: (x: Double, y: Double)) {
    func bump(_ t: Double, _ from: Double, _ to: Double) -> Double {
        t <= from || t >= to ? 0 : pow(sin(.pi * (t - from) / (to - from)), 2)
    }
    var yaw = 0.0, pitch = 0.0, lift = 0.0, sx = 1.0, sy = 1.0
    for (poke, at) in pokes {
        let t = clamp((now - at) / poke.duration)
        switch poke {
        case .spin(let turns): yaw += 180 * Double(max(1, turns)) * (1 - cos(.pi * t))
        case .nod: pitch += 16 * bump(t, 0, 1)
        case .bounce:
            let land = bump(t, 0, 0.18) + bump(t, 0.82, 1), air = bump(t, 0.18, 0.82)
            lift += 0.3 * sin(.pi * clamp((t - 0.14) / 0.72))
            sx *= 1 + 0.08 * land - 0.04 * air
            sy *= 1 - 0.1 * land + 0.06 * air
        }
    }
    return (Rotation(yaw: yaw, pitch: pitch), lift, (sx, sy))
}

private let RAYS = 128
// The inflated tips stay inside the outline, so their rings are left out.
private let INFLATED_RINGS = [-60.0, -30, 0, 30, 60].map { $0 * .pi / 180 }
private let BEVEL_RINGS = [0.0, 22.5, 45, 67.5, 90].map { $0 * .pi / 180 }

private func section(_ outline: [Point], _ y: Double) -> Double {
    var radius = 0.0
    for i in outline.indices {
        let a = outline[i], b = outline[(i + 1) % outline.count]
        guard (a.y <= y) != (b.y <= y) else { continue }
        radius = max(radius, abs(a.x + (b.x - a.x) * (y - a.y) / (b.y - a.y)))
    }
    return radius
}

func surface(_ solid: Solid, _ x: Double, _ y: Double, _ radii: [Double]) -> (z: Double, normal: Vec3) {
    switch solid {
    case .pumpkin:
        let z = Pumpkin.front(x, y)
        let dx = (Pumpkin.front(x + 0.002, y) - Pumpkin.front(x - 0.002, y)) / 0.004
        let dy = (Pumpkin.front(x, y + 0.002) - Pumpkin.front(x, y - 0.002)) / 0.004
        return (z, Vec3(x: -dx, y: -dy, z: 1))
    case .slab(let depth, _): return (depth, Vec3(x: 0, y: 0, z: 1))
    case .inflated(let depth):
        let edge = max(0.01, radiusAtAngle(radii, atan2(y, x)))
        let z = solid.front(hypot(x, y) / edge)
        return (z, Vec3(x: x / (edge * edge), y: y / (edge * edge), z: z / (depth * depth)))
    case .loft:
        func z(_ x: Double, _ y: Double) -> Double {
            solid.front(hypot(x, y) / max(0.01, radiusAtAngle(radii, atan2(y, x))))
        }
        let dx = (z(x + 0.002, y) - z(x - 0.002, y)) / 0.004
        let dy = (z(x, y + 0.002) - z(x, y - 0.002)) / 0.004
        return (z(x, y), Vec3(x: -dx, y: -dy, z: 1))
    case .revolved:
        let outline = toPoints(Silhouette(radii: radii), 1)
        let radius = section(outline, y)
        let z = sqrt(max(0, radius * radius - x * x))
        let slope = (section(outline, y + 0.005) - section(outline, y - 0.005)) / 0.01
        return (z, Vec3(x: x, y: -radius * slope, z: z))
    case .pill:
        let points = toPoints(Silhouette(radii: radii), 1)
        let radius = points.map { abs($0.y) }.max()!
        let reach = max(0, points.map { abs($0.x) }.max()! - radius)
        let dx = x - min(reach, max(-reach, x))
        let z = sqrt(max(0, radius * radius - dx * dx - y * y))
        return (z, Vec3(x: dx, y: y, z: z))
    }
}

/// Outline of `sil` as a rotated solid, seen from the front.
private func projectedRings(_ sil: Silhouette, _ solid: Solid, _ rotation: Rotation, smooth: Bool = false) -> [[Point]] {
    let outline = toPoints(sil, 1)
    let center = Point(x: sil.cx, y: sil.cy)
    let base = outline.map { Point(x: $0.x - center.x, y: $0.y - center.y) }

    // Each ring is the outline scaled towards the centre and set at a depth. A value type keeps the
    // per-point work a multiply rather than a closure call, which this loop cannot afford.
    var rings: [(k: Double, z: Double)] = []
    switch solid {
    case .pumpkin:
        return [Pumpkin.outline(rotation).map { Point(x: $0.x * sil.sx + sil.cx, y: $0.y * sil.sy + sil.cy) }]
    case .inflated(let depth):
        let steps = 24
        let angles = smooth ? (0...steps).map { -.pi / 2 + Double($0) * .pi / Double(steps) } : INFLATED_RINGS
        for phi in angles { rings.append((cos(phi), depth * sin(phi))) }
    case .loft(let depth):
        for i in 0...16 {
            let phi = -.pi / 2 + Double(i) * .pi / 16
            rings.append((pow(max(0, cos(phi)), 0.72), depth * sin(phi)))
        }
    case .slab(let depth, let bevel):
        let core = max(0, depth - bevel)
        var lowest = base[0].x, highest = base[0].x, top = base[0].y, bottom = base[0].y
        for p in base {
            lowest = min(lowest, p.x); highest = max(highest, p.x)
            top = min(top, p.y); bottom = max(bottom, p.y)
        }
        let radius = max(0.01, min((highest - lowest) / 2, (bottom - top) / 2))
        for (side, phis) in [(-1.0, BEVEL_RINGS.reversed()), (1.0, BEVEL_RINGS)] {
            for phi in phis {
                rings.append((max(0, 1 - bevel / radius * (1 - cos(phi))), side * (core + bevel * sin(phi))))
            }
        }
    case .revolved:
        var top = base[0].y, bottom = base[0].y
        for p in base { top = min(top, p.y); bottom = max(bottom, p.y) }
        return (0...40).map { row in
            let y = lerp(top + 1e-6, bottom - 1e-6, Double(row) / 40)
            let radius = section(base, y)
            return (0..<64).map { i in
                let angle = Double(i) / 64 * TAU
                let p = rotation.apply(Vec3(x: radius * cos(angle), y: y, z: radius * sin(angle)))
                return Point(x: p.x + center.x, y: p.y + center.y)
            }
        }
    case .pill:
        var radius = 0.0, extent = 0.0
        for p in base { radius = max(radius, abs(p.y)); extent = max(extent, abs(p.x)) }
        let reach = max(0, extent - radius)
        return (0...40).map { row in
            let x = lerp(-extent, extent, Double(row) / 40)
            let dx = x - min(reach, max(-reach, x))
            let r = (max(0, radius * radius - dx * dx)).squareRoot()
            return (0..<64).map { i in
                let angle = Double(i) / 64 * TAU
                let p = rotation.apply(Vec3(x: x, y: r * cos(angle), z: r * sin(angle)))
                return Point(x: p.x + center.x, y: p.y + center.y)
            }
        }
    }
    var mesh = [[Point]](repeating: [], count: rings.count)
    for (index, ring) in rings.enumerated() {
        var projected = [Point](repeating: Point(), count: base.count)
        for (i, p) in base.enumerated() {
            let v = rotation.apply(Vec3(x: p.x * ring.k, y: p.y * ring.k, z: ring.z))
            projected[i] = Point(x: v.x + center.x, y: v.y + center.y)
        }
        mesh[index] = projected
    }
    return mesh
}

/// Projected faces retain concave gaps when an extrusion turns edge-on.
public func solidPath(_ sil: Silhouette, _ solid: Solid, _ rotation: Rotation, scale: Double) -> CGPath {
    let rings = projectedRings(sil, solid, rotation)
    let path = CGMutablePath()
    func add(_ points: [Point]) {
        let area = points.indices.reduce(0.0) { sum, i in
            let a = points[i], b = points[(i + 1) % points.count]
            return sum + a.x * b.y - a.y * b.x
        }
        let loop = area < 0 ? Array(points.reversed()) : points
        if let first = loop.first { path.move(to: CGPoint(x: first.x * scale, y: first.y * scale)) }
        for point in loop.dropFirst() { path.addLine(to: CGPoint(x: point.x * scale, y: point.y * scale)) }
        path.closeSubpath()
    }
    if let first = rings.first, let last = rings.last { add(first); add(last) }
    for row in 1..<rings.count {
        let a = rings[row - 1], b = rings[row]
        for i in a.indices {
            let next = (i + 1) % a.count
            add([a[i], a[next], b[next], b[i]])
        }
    }
    return path
}

public func solidOutline(_ sil: Silhouette, _ solid: Solid, _ rotation: Rotation, scale: Double,
                         smooth: Bool = false) -> [Point] {
    if solid == .pumpkin {
        return Pumpkin.outline(rotation, rays: 128).map {
            Point(x: ($0.x * sil.sx + sil.cx) * scale, y: ($0.y * sil.sy + sil.cy) * scale)
        }
    }
    let center = Point(x: sil.cx, y: sil.cy)
    let mesh = projectedRings(sil, solid, rotation, smooth: smooth).map { ring in
        ring.map { Point(x: $0.x - center.x, y: $0.y - center.y) }
    }

    let rayCount = RAYS
    // One pass per edge straight into the ray maxima, so there are no per-ray edge lists to build.
    var cosines = [Double](repeating: 0, count: rayCount)
    var sines = [Double](repeating: 0, count: rayCount)
    for i in 0..<rayCount {
        let angle = Double(i) / Double(rayCount) * TAU
        cosines[i] = cos(angle)
        sines[i] = sin(angle)
    }
    var best = [Double](repeating: 0, count: rayCount)
    let near = 1e-6
    // Each vertex is shared by four edges, so its angle and radius are worked out once per frame.
    let angles = mesh.map { ring in ring.map { atan2($0.y, $0.x) } }
    let radii = mesh.map { ring in ring.map { ($0.x * $0.x + $0.y * $0.y).squareRoot() } }
    let n = mesh[0].count
    func cast(_ p: Point, _ q: Point, _ angleA: Double, _ ra: Double, _ angleB: Double, _ rb: Double) {
        if ra < near && rb < near { return }
        let start = ra < near ? angleB : angleA
        var span = (rb < near ? angleA : angleB) - start
        while span > .pi { span -= TAU }
        while span < -.pi { span += TAU }
        let from = start + min(0, span), to = start + max(0, span)
        let first = Int(floor(from / TAU * Double(rayCount))) - 1
        let last = Int(floor(to / TAU * Double(rayCount))) + 1
        let ex = q.x - p.x, ey = q.y - p.y
        for i in first...last {
            let s = ((i % rayCount) + rayCount) % rayCount
            let dx = cosines[s], dy = sines[s]
            let den = dx * ey - dy * ex
            if abs(den) < 1e-12 { continue }
            let t = (p.x * ey - p.y * ex) / den
            let u = (p.x * dy - p.y * dx) / den
            if t > best[s] && u >= -1e-9 && u <= 1 + 1e-9 { best[s] = t }
        }
    }
    for (k, ring) in mesh.enumerated() {
        let ringAngles = angles[k], ringRadii = radii[k]
        for i in 0..<n {
            let next = (i + 1) % n
            cast(ring[i], ring[next], ringAngles[i], ringRadii[i], ringAngles[next], ringRadii[next])
            if k > 0 {
                cast(mesh[k - 1][i], ring[i], angles[k - 1][i], radii[k - 1][i], ringAngles[i], ringRadii[i])
            }
        }
    }
    return (0..<rayCount).map { s in
        let dx = cosines[s], dy = sines[s]
        return Point(x: (center.x + dx * best[s]) * scale, y: (center.y + dy * best[s]) * scale)
    }
}
