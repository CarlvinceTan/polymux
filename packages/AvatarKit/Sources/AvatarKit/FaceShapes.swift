import Foundation

// Grok Bot 0.75.8's eyes and face player (Br, yi): spines stroked to a half width, or ovals,
// in its face units — radians on a unit sphere, x outward from the face's middle, y up.

let MORPH_TIME = 0.28
let LID_TIME = 0.35
let BLINK_TIME = 0.26
private let MORPH_EASE = [0.4, 0.06, 0.18, 1.0]
private let SPINE_WIDTH = 0.1305

public enum FaceID: String, CaseIterable, Sendable {
    case neutral, happy, surprised, sad, sleepy, squinted, calm, inspecting, confused

    var eyes: [EyeShape] { FACES[self]! }
    /// Drawn as shapes rather than open eyes, so lid swaps and lid tracks leave them alone.
    var shaped: Bool { [.sad, .happy, .confused, .squinted, .sleepy].contains(self) }
    var liddable: Bool { !shaped && !eyes.contains(where: \.isOval) }
}

public struct EyeShape: Equatable, Sendable {
    var off: Point
    var tilt = 0.0
    var wid: Double
    var lidY = 1.0
    var oval = Point()
    var ovalMix = 0.0
    var pts: [Point]
    var path: [Double]?

    init(off: Point, tilt: Double = 0, wid: Double, lidY: Double = 1, oval: Point = Point(),
         ovalMix: Double = 0, pts: [Point], path: [Double]?) {
        self.off = off; self.tilt = tilt; self.wid = wid; self.lidY = lidY
        self.oval = oval; self.ovalMix = ovalMix; self.pts = pts; self.path = path
    }

    init(off: Point, tilt: Double = 0, wid: Double, spine: (Point, Point, Point), arc: Bool = false,
         oval: Point = Point()) {
        let path = spinePath(spine.0, spine.1, spine.2, arc: arc)
        self.init(off: off, tilt: tilt, wid: wid, oval: oval, pts: spinePoints(path), path: path)
    }

    /// A straight spine `w` wide and `h` long, turned `degrees` from upright.
    static func stadium(_ w: Double, _ h: Double, degrees: Double = 0, off: Point, tilt: Double = 0) -> EyeShape {
        let reach = max(0, (h - w) / 2), angle = degrees * .pi / 180
        let s = sin(angle) * reach, c = cos(angle) * reach
        return EyeShape(off: off, tilt: tilt, wid: w / 2, spine: (Point(x: -s, y: -c), Point(), Point(x: s, y: c)))
    }

    var isOval: Bool { oval.x > 1e-4 && oval.y > 1e-4 }
    var settled: EyeShape { var eye = self; eye.ovalMix = 0; return eye }
    var hidden: EyeShape { var eye = self; eye.wid = 0; eye.oval = Point(); return eye }

    var isCurved: Bool {
        guard !isOval, pts.count >= 3 else { return false }
        let first = pts[0], last = pts[pts.count - 1]
        let dx = last.x - first.x, dy = last.y - first.y, length = dx * dx + dy * dy
        guard length >= 1e-8 else { return false }
        var bend = 0.0
        for point in pts {
            let t = ((point.x - first.x) * dx + (point.y - first.y) * dy) / length
            bend = max(bend, hypot(point.x - (first.x + t * dx), point.y - (first.y + t * dy)))
        }
        return bend > 1e-8
    }

    /// Squeezed towards a slit as `amount` closes the eye.
    func closed(_ amount: Double) -> EyeShape {
        guard amount > 0 else { return self }
        let box = extent(self), width = max(box.w, 1e-4), height = max(box.h, width)
        return pill(width, height + (width * 0.42 - height) * min(1, amount), box.cx, box.cy)
    }

    /// Lowered by a lid from above, `amount` of the way shut; negative overshoots open.
    func lidded(_ amount: Double) -> EyeShape {
        if amount == 0 { return self }
        let eye = plumb(self), keep = max(0.03, 1 - amount)
        let drop = pivot(eye) * (1 - keep)
        var out = eye
        out.off = Point(x: eye.off.x - sin(-eye.tilt) * drop, y: eye.off.y + cos(-eye.tilt) * drop)
        out.lidY = eye.lidY * keep
        return out
    }

    /// The outline around the eye's middle, before its tilt, lid and offset.
    var outline: [Point] {
        if isOval {
            let mix = ovalMix > 1e-4 ? min(1, ovalMix) : smoothstep(0, 0.08, min(oval.x, oval.y))
            if mix < 0.999 { return ring(oval.y, oval.x, mix) }
            return (0..<48).map { index in
                let angle = Double(index) / 48 * TAU
                return Point(x: oval.x * cos(angle), y: oval.y * sin(angle))
            }
        }
        return stroke(pts, wid)
    }
}

private let FACES: [FaceID: [EyeShape]] = {
    func pair(_ eye: EyeShape) -> [EyeShape] { [eye, eye] }
    func pill(_ w: Double, _ h: Double, _ degrees: Double, _ x: Double) -> EyeShape {
        .stadium(w, h, degrees: degrees, off: Point(x: x))
    }
    return [
        .neutral: pair(pill(0.26, 0.57, 0, 0)),
        // The newer build's bean, fitted to its frames: a shallow arc bending down, so the pill bends into it.
        .happy: pair(EyeShape(off: Point(x: 0, y: 0.03), wid: 0.12,
                              spine: (Point(x: -0.115), Point(x: 0, y: -0.05), Point(x: 0.115)), arc: true)),
        .surprised: pair(pill(0.42, 0.86, 0, 0.03)),
        .inspecting: [pill(0.54, 0.54, 0, 0.04), pill(0.36, 0.36, 0, 0.04)],
        .sad: pair(pill(0.26, 0.5, -28, -0.05)),
        .sleepy: pair(EyeShape(off: Point(x: 0.01), wid: 0.13,
                               spine: (Point(x: -0.11, y: 0.025), Point(x: 0, y: -0.05), Point(x: 0.11, y: 0.025)))),
        .squinted: pair(pill(0.26, 0.5, 90, 0.01)),
        .calm: pair(EyeShape(off: Point(x: 0.05), wid: 0.247, spine: (Point(x: -0.296), Point(), Point(x: 0.296)),
                             oval: Point(x: 0.296, y: 0.247))),
        // Bottom first like the pill, so the pill bends into it instead of shrinking through a dot.
        .confused: pair(EyeShape(off: Point(x: -0.1), wid: SPINE_WIDTH,
                                 spine: (Point(x: 0.01, y: -0.16), Point(x: -0.17), Point(x: 0.01, y: 0.16))))
    ]
}()

/// A spine through three points as two cubic segments: three nodes of x, y, in-handle and out-handle offsets.
private func spinePath(_ p0: Point, _ p1: Point, _ p2: Point, arc: Bool) -> [Double] {
    var u = [Double](repeating: 0, count: 18)
    func node(_ i: Int, _ p: Point) { u[i * 6] = p.x; u[i * 6 + 1] = p.y }
    func into(_ i: Int, _ p: Point) { u[i * 6 + 2] = p.x - u[i * 6]; u[i * 6 + 3] = p.y - u[i * 6 + 1] }
    func out(_ i: Int, _ p: Point) { u[i * 6 + 4] = p.x - u[i * 6]; u[i * 6 + 5] = p.y - u[i * 6 + 1] }
    let (ax, ay, bx, by, cx, cy) = (p0.x, p0.y, p1.x, p1.y, p2.x, p2.y)
    if arc {
        let det = 2 * (ax * (by - cy) + bx * (cy - ay) + cx * (ay - by))
        if abs(det) > 1e-9 {
            let a2 = ax * ax + ay * ay, b2 = bx * bx + by * by, c2 = cx * cx + cy * cy
            let mx = (a2 * (by - cy) + b2 * (cy - ay) + c2 * (ay - by)) / det
            let my = (a2 * (cx - bx) + b2 * (ax - cx) + c2 * (bx - ax)) / det
            let radius = hypot(ax - mx, ay - my)
            let start = atan2(ay - my, ax - mx), end = atan2(cy - my, cx - mx), via = atan2(by - my, bx - mx)
            let span = (end - start + TAU).truncatingRemainder(dividingBy: TAU)
            let sweep = (via - start + TAU).truncatingRemainder(dividingBy: TAU) <= span ? span : span - TAU
            let handle = 4.0 / 3 * tan(sweep / 8) * radius
            for (index, angle) in [start, start + sweep / 2, start + sweep].enumerated() {
                let p = Point(x: mx + radius * cos(angle), y: my + radius * sin(angle))
                let tx = -sin(angle), ty = cos(angle)
                node(index, p)
                if index > 0 { into(index, Point(x: p.x - handle * tx, y: p.y - handle * ty)) }
                if index < 2 { out(index, Point(x: p.x + handle * tx, y: p.y + handle * ty)) }
            }
            return u
        }
        let mid = Point(x: (ax + cx) / 2, y: (ay + cy) / 2)
        node(0, p0); node(1, mid); node(2, p2)
        out(0, Point(x: ax + (mid.x - ax) / 3, y: ay + (mid.y - ay) / 3))
        into(1, Point(x: mid.x - (mid.x - ax) / 3, y: mid.y - (mid.y - ay) / 3))
        out(1, Point(x: mid.x + (cx - mid.x) / 3, y: mid.y + (cy - mid.y) / 3))
        into(2, Point(x: cx - (cx - mid.x) / 3, y: cy - (cy - mid.y) / 3))
        return u
    }
    let m = Point(x: (ax + bx) / 2, y: (ay + by) / 2), h = Point(x: (bx + cx) / 2, y: (by + cy) / 2)
    let g = Point(x: (m.x + h.x) / 2, y: (m.y + h.y) / 2)
    node(0, p0); node(1, g); node(2, p2)
    out(0, Point(x: ax + 2 / 3 * (m.x - ax), y: ay + 2 / 3 * (m.y - ay)))
    into(1, Point(x: g.x + 2 / 3 * (m.x - g.x), y: g.y + 2 / 3 * (m.y - g.y)))
    out(1, Point(x: g.x + 2 / 3 * (h.x - g.x), y: g.y + 2 / 3 * (h.y - g.y)))
    into(2, Point(x: cx + 2 / 3 * (h.x - cx), y: cy + 2 / 3 * (h.y - cy)))
    return u
}

/// 21 points along a spine path.
private func spinePoints(_ u: [Double]) -> [Point] {
    func node(_ i: Int) -> Point { Point(x: u[i * 6], y: u[i * 6 + 1]) }
    func handle(_ i: Int, _ k: Int) -> Point { Point(x: u[i * 6] + u[i * 6 + k], y: u[i * 6 + 1] + u[i * 6 + k + 1]) }
    var points: [Point] = []
    points.reserveCapacity(21)
    for segment in 0..<2 {
        let a = node(segment), b = handle(segment, 4), c = handle(segment + 1, 2), d = node(segment + 1)
        for step in segment..<11 {
            let t = Double(step) / 10, s = 1 - t
            let wa = s * s * s, wb = 3 * s * s * t, wc = 3 * s * t * t, wd = t * t * t
            points.append(Point(x: wa * a.x + wb * b.x + wc * c.x + wd * d.x,
                                y: wa * a.y + wb * b.y + wc * c.y + wd * d.y))
        }
    }
    return points
}

private func lerp(_ a: Point, _ b: Point, _ t: Double) -> Point {
    Point(x: a.x + (b.x - a.x) * t, y: a.y + (b.y - a.y) * t)
}

private func smoothstep(_ from: Double, _ to: Double, _ value: Double) -> Double {
    let t = clamp((value - from) / (to - from))
    return t * t * (3 - 2 * t)
}

private func smooth(_ value: Double) -> Double { smoothstep(0, 1, value) }

private struct Stadium {
    var cx, cy, angle, halfLength, halfWidth: Double
}

private func stadium(_ eye: EyeShape) -> Stadium {
    if eye.isOval {
        let (w, h) = (eye.oval.x, eye.oval.y)
        return Stadium(cx: eye.off.x, cy: eye.off.y, angle: w >= h ? .pi / 2 : 0, halfLength: max(w, h), halfWidth: min(w, h))
    }
    guard eye.pts.count >= 2 else {
        return Stadium(cx: eye.off.x, cy: eye.off.y, angle: eye.tilt, halfLength: eye.wid, halfWidth: eye.wid)
    }
    let a = eye.pts[0], b = eye.pts[eye.pts.count - 1]
    return Stadium(cx: eye.off.x + (a.x + b.x) / 2, cy: eye.off.y + (a.y + b.y) / 2,
                   angle: atan2(b.x - a.x, b.y - a.y) + eye.tilt,
                   halfLength: hypot(b.x - a.x, b.y - a.y) / 2 + eye.wid, halfWidth: eye.wid)
}

private func turn(_ a: Stadium, _ b: Stadium) -> Double {
    var delta = b.angle - a.angle
    while delta > .pi / 2 { delta -= .pi }
    while delta < -.pi / 2 { delta += .pi }
    return delta
}

/// 48 points blending an upright stadium into the ellipse with the same extent.
private func ring(_ halfLength: Double, _ halfWidth: Double, _ mix: Double) -> [Point] {
    let straight = max(0, halfLength - halfWidth), amount = clamp(mix)
    return (0..<48).map { index in
        let angle = Double(index) / 48 * TAU, c = cos(angle), s = sin(angle)
        let side = halfWidth / max(abs(c), 1e-6)
        var reach: Double
        if side * abs(s) <= straight + 1e-6 && side.isFinite { reach = side }
        else {
            let along = (s >= 0 ? 1.0 : -1.0) * straight * s
            reach = along + sqrt(max(0, along * along - straight * straight + halfWidth * halfWidth))
        }
        let x = reach * c, y = reach * s
        return Point(x: x + (halfWidth * c - x) * amount, y: y + (halfLength * s - y) * amount)
    }
}

private func upright(_ w: Double, _ h: Double, _ angle: Double, _ off: Point) -> EyeShape {
    .stadium(max(w, 1e-4), max(h, w), off: off, tilt: angle)
}

private func pill(_ w: Double, _ h: Double, _ cx: Double, _ cy: Double) -> EyeShape {
    h >= w ? upright(w, h, 0, Point(x: cx, y: cy)) : upright(h, w, .pi / 2, Point(x: cx, y: cy))
}

private func ovalMorph(_ a: EyeShape, _ b: EyeShape, _ t: Double) -> EyeShape {
    if t <= 1e-4 { return a.settled }
    if t >= 0.995 { return b.settled }
    let from = stadium(a), to = stadium(b)
    let toOval = b.isOval
    let shape = toOval ? 1 - pow(1 - t, 3) : t * t * t
    let length = max(from.halfLength + (to.halfLength - from.halfLength) * t, 1e-4)
    let width = max(from.halfWidth + (to.halfWidth - from.halfWidth) * t, 1e-4)
    let start = a.isOval ? 1.0 : 0
    let mix = start + ((toOval ? 1 : 0) - start) * shape
    var eye = upright(2 * width, 2 * length, from.angle + turn(from, to) * t,
                      Point(x: from.cx + (to.cx - from.cx) * t, y: from.cy + (to.cy - from.cy) * t))
    if mix > 1e-4 { eye.oval = Point(x: width, y: length); eye.ovalMix = mix }
    return eye
}

private func stadiumMorph(_ a: EyeShape, _ b: EyeShape, _ t: Double) -> EyeShape {
    if t >= 0.9999 { return b.settled }
    let from = stadium(a), to = stadium(b)
    let angle = from.angle + turn(from, to) * t
    let width = max(1e-4, from.halfWidth + (to.halfWidth - from.halfWidth) * t)
    let straight = max(0, from.halfLength - from.halfWidth) * (1 - t) + max(0, to.halfLength - to.halfWidth) * t
    return .stadium(2 * width, 2 * (straight + width), degrees: angle * 180 / .pi,
                    off: Point(x: from.cx + (to.cx - from.cx) * t, y: from.cy + (to.cy - from.cy) * t))
}

private func spineMorph(_ a: EyeShape, _ b: EyeShape, _ t: Double) -> EyeShape {
    var eye = EyeShape(off: lerp(a.off, b.off, t), tilt: a.tilt + (b.tilt - a.tilt) * t, wid: a.wid + (b.wid - a.wid) * t,
                       lidY: a.lidY + (b.lidY - a.lidY) * t, oval: lerp(a.oval, b.oval, t), pts: [], path: nil)
    if let from = a.path, let to = b.path, from.count == to.count {
        let path = zip(from, to).map { $0 + ($1 - $0) * t }
        eye.path = path
        eye.pts = spinePoints(path)
        return eye
    }
    let count = max(a.pts.count, b.pts.count, 2)
    eye.pts = (0..<count).map { lerp(resample(a.pts, $0, count), resample(b.pts, $0, count), t) }
    return eye
}

private func resample(_ points: [Point], _ index: Int, _ count: Int) -> Point {
    if points.isEmpty { return Point() }
    if points.count == 1 || count <= 1 { return points[0] }
    let cursor = Double(index) / Double(count - 1) * Double(points.count - 1)
    let i = min(Int(cursor), points.count - 2)
    return lerp(points[i], points[i + 1], cursor - Double(i))
}

private func extent(_ eye: EyeShape) -> (w: Double, h: Double, cx: Double, cy: Double) {
    var width = 0.0, height = 0.0, degrees = 0.0, cx = eye.off.x, cy = eye.off.y
    if eye.isOval {
        width = 2 * eye.oval.x; height = 2 * eye.oval.y * eye.lidY; degrees = eye.tilt * 180 / .pi
    } else if eye.wid > 0, eye.pts.count >= 2 {
        let a = eye.pts[0], b = eye.pts[eye.pts.count - 1]
        let dx = b.x - a.x, dy = b.y - a.y
        width = 2 * eye.wid
        height = (hypot(dx, dy) + width) * eye.lidY
        degrees = 180 / .pi * atan2(dx, dy) + eye.tilt * 180 / .pi
        cx = eye.off.x + (a.x + b.x) / 2
        cy = eye.off.y + (a.y + b.y) / 2 * eye.lidY
    } else {
        return (0.26, 0.57, eye.off.x, eye.off.y)
    }
    let angle = degrees * .pi / 180, c = abs(cos(angle)), s = abs(sin(angle))
    return (width * c + height * s, width * s + height * c, cx, cy)
}

/// Where a lid folds an eye: 40% of the way down from its top.
private func pivot(_ eye: EyeShape) -> Double {
    var top: Double, bottom: Double
    if eye.isOval { top = -eye.oval.y; bottom = eye.oval.y }
    else {
        top = (eye.pts.map(\.y).min() ?? 0) - eye.wid
        bottom = (eye.pts.map(\.y).max() ?? 0) + eye.wid
    }
    return top + 0.4 * (bottom - top)
}

/// Turns a steep spine upright, carrying its slant in the tilt.
private func plumb(_ eye: EyeShape) -> EyeShape {
    guard eye.pts.count >= 2 else { return eye }
    let a = eye.pts[0], b = eye.pts[eye.pts.count - 1], dx = b.x - a.x, dy = b.y - a.y
    if abs(dy) < abs(dx) { return eye }
    let angle = atan2(dx, dy)
    if abs(angle) < 1e-5 { return eye }
    let c = cos(angle), s = sin(angle)
    var out = eye
    out.tilt += angle
    out.pts = eye.pts.map { Point(x: $0.x * c - $0.y * s, y: $0.x * s + $0.y * c) }
    return out
}

/// The inspecting face's eyes trade sizes as the head turns, so the near eye looks bigger.
func inspected(_ eyes: [EyeShape], weight: Double, bias: Double, follows: Bool, yaw: Double) -> [EyeShape] {
    if weight < 0.001 { return eyes }
    let side = follows ? clamp(yaw / 20, -1, 1) : bias
    let pair = FaceID.inspecting.eyes
    let big = max(1e-4, pair[0].wid), small = max(1e-4, pair[1].wid), share = (1 + side) / 2
    func scaled(_ eye: EyeShape, _ factor: Double) -> EyeShape {
        let k = 1 + (factor - 1) * weight
        var out = eye.settled
        out.wid = eye.wid * k
        out.pts = eye.pts.map { Point(x: $0.x * k, y: $0.y * k) }
        return out
    }
    return [scaled(eyes[0], pow(small / big, share)), scaled(eyes[1], pow(big / small, share))]
}

/// The outline of a stroked polyline: a hull when the stroke swallows it, else offset sides with round caps.
private func stroke(_ input: [Point], _ radius: Double) -> [Point] {
    var points: [Point] = []
    for point in input where points.last.map({ hypot(point.x - $0.x, point.y - $0.y) > 1e-6 }) ?? true {
        points.append(point)
    }
    guard !points.isEmpty, radius > 0 else { return [] }
    if let blob = hull(points, radius) { return blob }
    let count = points.count
    guard count >= 2 else { return [] }
    func unit(_ a: Point, _ b: Point) -> Point {
        let length = hypot(b.x - a.x, b.y - a.y)
        return length > 0 ? Point(x: (b.x - a.x) / length, y: (b.y - a.y) / length) : Point()
    }
    var normals: [Point] = []
    for index in 0..<count {
        let along = unit(points[max(0, index - 1)], points[min(count - 1, index + 1)])
        var normal = Point(x: -along.y, y: along.x)
        if let last = normals.last, normal.x * last.x + normal.y * last.y < 0 { normal = Point(x: -normal.x, y: -normal.y) }
        normals.append(normal)
    }
    let head = unit(points[0], points[1]), tail = unit(points[count - 2], points[count - 1])
    let left = zip(points, normals).map { Point(x: $0.x + $1.x * radius, y: $0.y + $1.y * radius) }
    let right = zip(points, normals).map { Point(x: $0.x - $1.x * radius, y: $0.y - $1.y * radius) }
    let end = Point(x: points[count - 1].x + tail.x * radius, y: points[count - 1].y + tail.y * radius)
    let start = Point(x: points[0].x - head.x * radius, y: points[0].y - head.y * radius)
    return left + cap(points[count - 1], left[count - 1], end, radius) + right.reversed() + cap(points[0], right[0], start, radius)
}

private func cap(_ center: Point, _ from: Point, _ toward: Point, _ radius: Double) -> [Point] {
    let angle = atan2(from.y - center.y, from.x - center.x)
    var delta = atan2(toward.y - center.y, toward.x - center.x) - angle
    while delta > .pi { delta -= TAU }
    while delta < -.pi { delta += TAU }
    let sign = delta >= 0 ? 1.0 : -1.0
    return (1..<10).map { step in
        let phase = angle + Double(step) / 10 * sign * .pi
        return Point(x: center.x + radius * cos(phase), y: center.y + radius * sin(phase))
    }
}

private func hull(_ points: [Point], _ radius: Double) -> [Point]? {
    let cx = points.map(\.x).reduce(0, +) / Double(points.count)
    let cy = points.map(\.y).reduce(0, +) / Double(points.count)
    let reach = points.map { hypot($0.x - cx, $0.y - cy) }.max() ?? 0
    guard radius > reach * 1.05 else { return nil }
    return (0..<48).map { index in
        let angle = Double(index) / 48 * TAU, c = cos(angle), s = sin(angle)
        let edge = points.max { $0.x * c + $0.y * s < $1.x * c + $1.y * s }!
        return Point(x: edge.x + c * radius, y: edge.y + s * radius)
    }
}

private enum MorphPath { case oval, spine, stadium }

private func blinkWindow(_ blink: Bool, _ duration: Double) -> (open: Double, shut: Double) {
    let hold = min(0.28, (blink ? 2.0 / 60 : 4.25 / 60) / max(duration, 0.05))
    let start = (1 - hold) * 0.42
    return (start, start + hold)
}

private func blinkCurve(_ t: Double, _ window: (open: Double, shut: Double)) -> Double {
    t < window.open ? smooth(t / window.open)
        : t <= window.shut ? 1 : 1 - smooth((t - window.shut) / max(1e-6, 1 - window.shut))
}

/// Reopening overshoots 6% before settling.
private func reopen(_ value: Double) -> Double {
    let t = clamp(value)
    if t < 0.7 { let k = t / 0.7; return (1 - (1 - k) * (1 - k)) * 1.06 }
    return 1.06 - 0.06 * smooth((t - 0.7) / 0.3)
}

/// One face event on a clip's timeline.
struct FaceEvent: Sendable {
    var t: Double
    var face: FaceID
    var throughBlink = false
}

/// Grok Bot's face player: morphs, lid swaps and blinks between faces.
struct FacePlayer {
    private(set) var name = FaceID.neutral
    private var rest = FaceID.neutral.eyes
    private var inspect = 0.0
    private var change: Change?

    private struct Change {
        enum Kind { case blink, morph(MorphPath), lid }
        var kind: Kind
        var fromInspect: Double
        var to: FaceID
        var from: [EyeShape]
        var target: [EyeShape]
        var t = 0.0
        var duration: Double
        var held = false

        var eased: Double { bezierEase(clamp(t / duration), MORPH_EASE) }
        var isBlink: Bool { if case .blink = kind { true } else { false } }
    }

    var isMorphing: Bool { change.map { !$0.isBlink && !$0.held } ?? false }

    var eyes: [EyeShape] {
        guard let change else { return rest }
        switch change.kind {
        case .blink: return change.from
        case .morph(let path):
            let t = clamp(change.eased)
            return zip(change.from, change.target).map { a, b in
                switch path {
                case .oval: ovalMorph(a, b, t)
                case .spine: spineMorph(a, b, t)
                case .stadium: stadiumMorph(a, b, t)
                }
            }
        case .lid:
            let t = min(change.t / change.duration, 1), window = blinkWindow(false, change.duration)
            if t < window.open { return change.from.map { $0.lidded(smooth(t / window.open)) } }
            if t <= window.shut { return change.target.map(\.hidden) }
            let opened = reopen((t - window.shut) / max(1e-6, 1 - window.shut))
            return change.target.map { $0.lidded(1 - opened) }
        }
    }

    var inspectWeight: Double {
        guard let change else { return inspect }
        if change.isBlink { return change.fromInspect }
        let start = change.fromInspect, end = change.to == .inspecting ? 1.0 : 0
        if case .morph = change.kind { return start + (end - start) * change.eased }
        let t = min(change.t / change.duration, 1), window = blinkWindow(false, change.duration)
        return t <= window.open ? start : t < window.shut ? 0 : end
    }

    var blinkAmount: Double? {
        guard let change, change.isBlink, !change.held else { return nil }
        return clamp(blinkCurve(min(change.t / change.duration, 1), blinkWindow(true, change.duration)))
    }

    mutating func morph(_ face: FaceID) {
        let from = eyes.map(\.settled), to = face.eyes
        let curved = from.contains(where: \.isCurved) || to.contains(where: \.isCurved)
        let oval = from.contains(where: \.isOval) || to.contains(where: \.isOval)
        let path: MorphPath = !curved && oval ? .oval
            : curved || name == .sleepy || face == .sleepy ? .spine : .stadium
        change = Change(kind: .morph(path), fromInspect: inspectWeight, to: face, from: from, target: to, duration: MORPH_TIME)
        name = face
    }

    mutating func lidMorph(_ face: FaceID) {
        change = Change(kind: .lid, fromInspect: inspectWeight, to: face, from: eyes.map(\.settled), target: face.eyes,
                        duration: LID_TIME)
        name = face
    }

    mutating func blink() {
        guard change == nil || change!.held else { return }
        change = Change(kind: .blink, fromInspect: inspect, to: name, from: rest, target: name.eyes, duration: BLINK_TIME)
    }

    mutating func play(_ face: FaceID, throughBlink: Bool) {
        guard face != name || (change != nil && change!.to != face) else { return }
        if throughBlink && !face.shaped && !name.shaped { lidMorph(face) } else { morph(face) }
    }

    mutating func step(_ dt: Double) {
        guard let change, !change.held else { return }
        advance(change, change.t + dt)
    }

    mutating func release() { change?.held = false }

    /// Replays `events` up to `time` and holds the result there.
    mutating func seek(_ events: [FaceEvent], _ time: Double) {
        name = .neutral; rest = FaceID.neutral.eyes; inspect = 0; change = nil
        var origin = 0.0
        for event in events {
            if event.t > time + 1e-9 { break }
            if let change { advance(change, event.t - origin) }
            play(event.face, throughBlink: event.throughBlink)
            origin = event.t
        }
        if let change { advance(change, time - origin) }
        change?.held = true
    }

    private mutating func advance(_ current: Change, _ time: Double) {
        if time >= current.duration {
            rest = current.target.map(\.settled)
            inspect = current.to == .inspecting ? 1 : 0
            change = nil
        } else {
            var next = current
            next.t = time
            change = time < 0 ? nil : next
        }
    }
}
