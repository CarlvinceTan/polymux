import CoreGraphics
import Foundation

public struct Point: Hashable, Sendable {
    public var x: Double
    public var y: Double
    public init(x: Double = 0, y: Double = 0) { self.x = x; self.y = y }
    public var cgPoint: CGPoint { CGPoint(x: x, y: y) }
}

public struct CircleSpec: Hashable, Sendable {
    public var x: Double
    public var y: Double
    public var r: Double
    public init(x: Double, y: Double, r: Double) { self.x = x; self.y = y; self.r = r }
}

public struct Silhouette: Hashable, Sendable {
    public var radii: [Double]
    public var rot: Double
    public var cx: Double
    public var cy: Double
    public var sx: Double
    public var sy: Double
    public init(radii: [Double], rot: Double = 0, cx: Double = 0, cy: Double = 0, sx: Double = 1, sy: Double = 1) {
        self.radii = radii; self.rot = rot; self.cx = cx; self.cy = cy; self.sx = sx; self.sy = sy
    }
}

private let ANGLES = (0..<PROFILE_SAMPLES).map { Double($0) / Double(PROFILE_SAMPLES) * TAU }
private let COS = ANGLES.map(cos)
private let SIN = ANGLES.map(sin)

public func silhouette(_ name: ProfileName, rot: Double = 0, cx: Double = 0, cy: Double = 0, sx: Double = 1, sy: Double = 1) -> Silhouette {
    Silhouette(radii: PROFILES[name]!, rot: rot, cx: cx, cy: cy, sx: sx, sy: sy)
}

public func circle(_ radius: Double, rot: Double = 0, cx: Double = 0, cy: Double = 0, sx: Double = 1, sy: Double = 1) -> Silhouette {
    Silhouette(radii: [Double](repeating: radius, count: PROFILE_SAMPLES), rot: rot, cx: cx, cy: cy, sx: sx, sy: sy)
}

public func blend(_ a: Silhouette, _ b: Silhouette, _ t: Double) -> Silhouette {
    var radii = [Double](repeating: 1, count: PROFILE_SAMPLES)
    for i in 0..<PROFILE_SAMPLES {
        radii[i] = lerp(i < a.radii.count ? a.radii[i] : 1, i < b.radii.count ? b.radii[i] : 1, t)
    }
    var dRot = b.rot - a.rot
    while dRot > .pi { dRot -= TAU }
    while dRot < -.pi { dRot += TAU }
    return Silhouette(radii: radii, rot: a.rot + dRot * t, cx: lerp(a.cx, b.cx, t), cy: lerp(a.cy, b.cy, t), sx: lerp(a.sx, b.sx, t), sy: lerp(a.sy, b.sy, t))
}

public func toPoints(_ s: Silhouette, _ scale: Double) -> [Point] {
    let cr = cos(s.rot), sr = sin(s.rot)
    return (0..<PROFILE_SAMPLES).map { i in
        let r = i < s.radii.count ? s.radii[i] : 1
        let x = r * COS[i], y = r * SIN[i]
        return Point(x: ((x * cr - y * sr) * s.sx + s.cx) * scale,
                     y: ((x * sr + y * cr) * s.sy + s.cy) * scale)
    }
}

public func closedPath(_ pts: [Point], _ tension: Double = 1 / 6) -> CGPath {
    let path = CGMutablePath()
    let n = pts.count
    guard n >= 3 else { return path }
    path.move(to: CGPoint(x: r2(pts[0].x), y: r2(pts[0].y)))
    for i in 0..<n {
        let p0 = pts[(i - 1 + n) % n], p1 = pts[i]
        let p2 = pts[(i + 1) % n], p3 = pts[(i + 2) % n]
        path.addCurve(to: CGPoint(x: r2(p2.x), y: r2(p2.y)),
                      control1: CGPoint(x: r2(p1.x + (p2.x - p0.x) * tension), y: r2(p1.y + (p2.y - p0.y) * tension)),
                      control2: CGPoint(x: r2(p2.x - (p3.x - p1.x) * tension), y: r2(p2.y - (p3.y - p1.y) * tension)))
    }
    path.closeSubpath()
    return path
}

public func profileFromPolygon(_ poly: [Point], _ cx: Double, _ cy: Double) -> [Double] {
    let n = poly.count
    return (0..<PROFILE_SAMPLES).map { k in
        let dx = COS[k], dy = SIN[k]
        var best = 0.0
        for i in 0..<n {
            let a = poly[i], b = poly[(i + 1) % n]
            let ex = b.x - a.x, ey = b.y - a.y
            let den = dx * ey - dy * ex
            if abs(den) < 1e-9 { continue }
            let px = a.x - cx, py = a.y - cy
            let t = (px * ey - py * ex) / den
            let u = (px * dy - py * dx) / den
            if t > best && u >= 0 && u <= 1 { best = t }
        }
        return best
    }
}

public func hullOfCircles(_ x1: Double, _ y1: Double, _ r1: Double, _ x2: Double, _ y2: Double, _ r2v: Double, _ steps: Int = 96) -> [Point] {
    let dx = x2 - x1, dy = y2 - y1
    let dist = hypot(dx, dy) == 0 ? 1e-6 : hypot(dx, dy)
    let base = atan2(dy, dx)
    let spread = acos(clamp((r1 - r2v) / dist, -1, 1))
    let half = Double(steps) / 2
    var pts: [Point] = []
    for i in 0...Int(half) {
        let a = base + spread + (TAU - 2 * spread) * Double(i) / half
        pts.append(Point(x: x1 + cos(a) * r1, y: y1 + sin(a) * r1))
    }
    for i in 0...Int(half) {
        let a = base - spread + 2 * spread * Double(i) / half
        pts.append(Point(x: x2 + cos(a) * r2v, y: y2 + sin(a) * r2v))
    }
    return pts
}

public func radiusAtAngle(_ radii: [Double], _ angle: Double) -> Double {
    let n = radii.count
    guard n > 0 else { return 1 }
    let wrapped = ((angle / TAU).truncatingRemainder(dividingBy: 1) + 1).truncatingRemainder(dividingBy: 1)
    let t = wrapped * Double(n)
    let i = Int(floor(t))
    return lerp(radii[i % n], radii[(i + 1) % n], t - Double(i))
}

public func superellipseProfile(_ n: Double, _ sx: Double = 1, _ sy: Double = 1) -> [Double] {
    (0..<PROFILE_SAMPLES).map { i in pow(pow(abs(COS[i] / sx), n) + pow(abs(SIN[i] / sy), n), -1 / n) }
}

public func unionOfCirclesProfile(_ circles: [CircleSpec]) -> [Double] {
    (0..<PROFILE_SAMPLES).map { i in
        let dx = COS[i], dy = SIN[i]
        var best = 0.0
        for c in circles {
            let b = dx * c.x + dy * c.y
            let disc = b * b - (c.x * c.x + c.y * c.y - c.r * c.r)
            if disc < 0 { continue }
            let t = b + sqrt(disc)
            if t > best { best = t }
        }
        return best
    }
}

private func roundedPolygon(_ verts: [Point], _ rc: Double, _ arcSteps: Int = 10) -> [Point] {
    let n = verts.count
    func normal(_ a: Point, _ b: Point) -> Double {
        let dx = b.x - a.x, dy = b.y - a.y
        let length = hypot(dx, dy) == 0 ? 1 : hypot(dx, dy)
        return atan2(-dx / length, dy / length)
    }
    var out: [Point] = []
    for i in 0..<n {
        let prev = verts[(i - 1 + n) % n], cur = verts[i], next = verts[(i + 1) % n]
        let a0 = normal(prev, cur), a1 = normal(cur, next)
        var d = a1 - a0
        while d > .pi { d -= TAU }
        while d < -.pi { d += TAU }
        for k in 0...arcSteps {
            let a = a0 + d * Double(k) / Double(arcSteps)
            out.append(Point(x: cur.x + cos(a) * rc, y: cur.y + sin(a) * rc))
        }
    }
    return out
}

public func regularPolygonProfile(_ sides: Int, _ radius: Double, _ rc: Double, _ rotationDeg: Double = 0) -> [Double] {
    let rot = rotationDeg * .pi / 180
    let verts = (0..<sides).map { i in
        let a = rot + Double(i) / Double(sides) * TAU
        return Point(x: cos(a) * (radius - rc), y: sin(a) * (radius - rc))
    }
    return profileFromPolygon(roundedPolygon(verts, rc), 0, 0)
}

private func svgArc(_ path: CGMutablePath, _ from: Point, _ to: Point, _ radius: Double) {
    let dx = to.x - from.x, dy = to.y - from.y
    let chord = hypot(dx, dy)
    guard chord > 0 else { return }
    guard radius > 0 else { path.addLine(to: to.cgPoint); return }
    let r = max(radius, chord / 2)
    let height = sqrt(max(0, r * r - chord * chord / 4))
    let cx = (from.x + to.x) / 2 - dy / chord * height
    let cy = (from.y + to.y) / 2 + dx / chord * height
    let start = atan2(from.y - cy, from.x - cx)
    var end = atan2(to.y - cy, to.x - cx)
    while end < start { end += TAU }
    path.addArc(center: CGPoint(x: cx, y: cy), radius: r, startAngle: start, endAngle: end, clockwise: false)
}

public func capsulePath(_ w: Double, _ h: Double) -> CGPath {
    let hw = max(w, 0.01) / 2, hh = max(h, 0.01) / 2
    let r = min(hw, hh)
    let points = [
        Point(x: r2(-hw), y: r2(-hh + r)), Point(x: r2(-hw + r), y: r2(-hh)),
        Point(x: r2(hw - r), y: r2(-hh)), Point(x: r2(hw), y: r2(-hh + r)),
        Point(x: r2(hw), y: r2(hh - r)), Point(x: r2(hw - r), y: r2(hh)),
        Point(x: r2(-hw + r), y: r2(hh)), Point(x: r2(-hw), y: r2(hh - r))
    ]
    let path = CGMutablePath()
    path.move(to: points[0].cgPoint)
    svgArc(path, points[0], points[1], r2(r))
    path.addLine(to: points[2].cgPoint)
    svgArc(path, points[2], points[3], r2(r))
    path.addLine(to: points[4].cgPoint)
    svgArc(path, points[4], points[5], r2(r))
    path.addLine(to: points[6].cgPoint)
    svgArc(path, points[6], points[7], r2(r))
    path.closeSubpath()
    return path
}
