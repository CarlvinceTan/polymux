import CoreGraphics
import Foundation

// The avatar body never leaves the screen: activity motifs are corner badges in the
// same top-right direction as the notification dot.
public let BADGE_ANGLE = -42.0 * Double.pi / 180
public let BADGE_FAR = 1.25

public func badgePoint(_ distance: Double) -> Point {
    Point(x: cos(BADGE_ANGLE) * distance, y: sin(BADGE_ANGLE) * distance)
}

func dotPulse(_ t: Double, _ index: Int) -> Double {
    let raw = ((t - Double(index) * 0.5) / 1.5).truncatingRemainder(dividingBy: 1)
    let p = (raw + 1).truncatingRemainder(dividingBy: 1)
    let k = p < 0.5 ? 0.5 - 0.5 * cos(p * TAU) : 0
    return clamp(k * 2)
}

// Thinking: three dots in a level row trailing out of the corner, pulsing left to right.
public let BADGE_DOT = 0.09
public let BADGE_ROW = 1.25
public let BADGE_STEP = 0.185

public func thinkingMarks(_ t: Double) -> [DotRender] {
    let center = badgePoint(BADGE_ROW)
    return (0..<3).map { i in
        let x = center.x + (Double(i) - 1) * BADGE_STEP
        let k = dotPulse(t, i)
        return DotRender(x: x, y: center.y, r: BADGE_DOT * (1 + (DOT_PEAK - 1) * k),
                         opacity: 0.6 + 0.4 * k)
    }
}

// Exclamation: bar and dot, leaning with the corner like the upstream alert mark.
private nonisolated(unsafe) let EXCLAIM_BAR = capsulePath(0.16, 0.375)
public let BADGE_TILT = 17.7

public func exclaimMarks(x: Double, y: Double, opacity: Double, tilt: Double = BADGE_TILT) -> [DotRender] {
    let rad = tilt * .pi / 180
    let cp = cos(rad), sp = sin(rad)
    func place(_ la: Double, _ lb: Double) -> Point {
        Point(x: x + la * cp - lb * sp, y: y + la * sp + lb * cp)
    }
    let bar = place(0, -0.0975)
    let dot = place(0, 0.21)
    return [
        DotRender(x: bar.x, y: bar.y, r: 0.19, opacity: opacity, d: EXCLAIM_BAR, rot: tilt),
        DotRender(x: dot.x, y: dot.y, r: 0.075, opacity: opacity)
    ]
}

// Question: the corner channel for an agent that needs an answer.
// Question: the corner channel for an agent that needs an answer.
public let QUESTION_FAR = 1.27

public func questionMarks(x: Double, y: Double, opacity: Double, tilt: Double = BADGE_TILT) -> [DotRender] {
    var line: [Point] = []
    let bowl = Point(x: 0, y: -0.11), radius = 0.16
    for i in 0...44 {
        let angle = (150 + 180 * Double(i) / 44) * .pi / 180
        line.append(Point(x: bowl.x + cos(angle) * radius, y: bowl.y + sin(angle) * radius))
    }
    let end = line[line.count - 1]
    let stem = Point(x: 0, y: 0.10)
    let control1 = Point(x: end.x + 0.060, y: end.y + 0.104)
    let control2 = Point(x: stem.x + 0.025, y: stem.y - 0.087)
    for i in 1...32 {
        let t = Double(i) / 32
        let u = 1 - t
        line.append(Point(x: u * u * u * end.x + 3 * u * u * t * control1.x + 3 * u * t * t * control2.x + t * t * t * stem.x,
                          y: u * u * u * end.y + 3 * u * u * t * control1.y + 3 * u * t * t * control2.y + t * t * t * stem.y))
    }
    let path = ribbon(line, 0.105)
    path.addEllipse(in: CGRect(x: -0.065, y: 0.245 - 0.065, width: 0.13, height: 0.13))
    return [DotRender(x: x, y: y, r: 0.3, opacity: opacity, d: path, rot: tilt)]
}

private func ribbon(_ line: [Point], _ thickness: Double) -> CGMutablePath {
    let path = CGMutablePath()
    guard line.count >= 2 else { return path }
    let half = thickness / 2
    var directions: [Point] = []
    for i in 0..<line.count {
        let previous = line[max(0, i - 1)], next = line[min(line.count - 1, i + 1)]
        let dx = next.x - previous.x, dy = next.y - previous.y
        let length = max(1e-6, hypot(dx, dy))
        directions.append(Point(x: dx / length, y: dy / length))
    }
    func left(_ i: Int) -> CGPoint {
        CGPoint(x: line[i].x - directions[i].y * half, y: line[i].y + directions[i].x * half)
    }
    func right(_ i: Int) -> CGPoint {
        CGPoint(x: line[i].x + directions[i].y * half, y: line[i].y - directions[i].x * half)
    }
    func cap(_ i: Int, _ angle: Double, _ forward: Bool) -> CGPoint {
        let normal = Point(x: -directions[i].y, y: directions[i].x)
        let flow = forward ? 1.0 : -1.0
        return CGPoint(x: line[i].x + half * (normal.x * cos(angle) + flow * directions[i].x * sin(angle)),
                       y: line[i].y + half * (normal.y * cos(angle) + flow * directions[i].y * sin(angle)))
    }
    let last = line.count - 1
    path.move(to: left(0))
    for i in 1...last { path.addLine(to: left(i)) }
    for i in 1...10 { path.addLine(to: cap(last, Double(i) / 10 * Double.pi, true)) }
    for i in stride(from: last - 1, through: 0, by: -1) { path.addLine(to: right(i)) }
    for i in stride(from: 10, through: 1, by: -1) { path.addLine(to: cap(0, Double(i) / 10 * Double.pi, false)) }
    path.closeSubpath()
    return path
}

// Sleep: zZ letters puffing out of the corner.
public func zMarks(x: Double, y: Double, size: Double, opacity: Double) -> [DotRender] {
    let w = 0.17 * size, th = 0.055 * size, half = 0.0775 * size
    let diagonal = hypot(w - th, 2 * half)
    let angle = atan2(2 * half, th - w) * 180 / .pi
    return [
        DotRender(x: x, y: y - half, r: 0.06 * size, opacity: opacity, d: capsulePath(w, th)),
        DotRender(x: x, y: y + half, r: 0.06 * size, opacity: opacity, d: capsulePath(w, th)),
        DotRender(x: x, y: y, r: 0.06 * size, opacity: opacity, d: capsulePath(diagonal, th), rot: angle)
    ]
}
