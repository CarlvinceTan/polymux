import CoreGraphics
import Foundation

func spawnPath(_ path: CGPath, center: Point, radius: Double, amount: Double, hub: Double = 19 / 114.5) -> CGPath {
    var edges: [(CGPoint, CGPoint)] = []
    var first = CGPoint.zero, current = CGPoint.zero
    path.applyWithBlock { item in
        let element = item.pointee
        let next: CGPoint
        switch element.type {
        case .moveToPoint: first = element.points[0]; current = first; return
        case .addLineToPoint: next = element.points[0]
        case .addQuadCurveToPoint: next = element.points[1]
        case .addCurveToPoint: next = element.points[2]
        case .closeSubpath: next = first
        @unknown default: return
        }
        edges.append((current, next)); current = next
    }
    let round = easings.easeInOutCubic(clamp(amount / 0.62))
    let size = lerp(1, hub, amount)
    let points = (0..<PROFILE_SAMPLES).map { index in
        let angle = Double(index) / Double(PROFILE_SAMPLES) * TAU
        let dx = cos(angle), dy = sin(angle)
        var reach = 0.0
        for (a, b) in edges {
            let ex = b.x - a.x, ey = b.y - a.y
            let denominator = dx * ey - dy * ex
            if abs(denominator) < 1e-9 { continue }
            let px = a.x - center.x, py = a.y - center.y
            let distance = (px * ey - py * ex) / denominator
            let along = (px * dy - py * dx) / denominator
            if distance > reach, along >= 0, along <= 1 { reach = distance }
        }
        let length = lerp(reach, radius, round) * size
        return Point(x: center.x + dx * length, y: center.y + dy * length)
    }
    return closedPath(points)
}

func moodMarks(_ mood: MoodID, _ t: Double) -> [DotRender] {
    switch mood {
    case .searching:
        let sweep = t * 1.8
        var marks = (0..<20).map { index in
            let a = sweep - Double(index) * 0.055
            return DotRender(x: cos(a) * 1.25, y: sin(a) * 1.25, r: 0.026,
                             opacity: 1 - Double(index) / 22)
        }
        for index in 0..<3 {
            let a = sweep + Double(index) * 0.18
            marks.append(DotRender(x: cos(a) * 1.02, y: sin(a) * 1.02, r: 0.035,
                                   opacity: 0.65 - Double(index) * 0.15))
        }
        return marks
    case .working:
        return (0..<18).map { index in
            let a = Double(index) / 18 * TAU - .pi / 2
            let phase = (Double(index) / 18 - t * 0.65).truncatingRemainder(dividingBy: 1)
            return DotRender(x: cos(a) * 1.23, y: sin(a) * 1.23, r: 0.025,
                             opacity: 0.35 + 0.65 * (1 + cos(phase * TAU)) / 2)
        }
    case .writing:
        let path = CGMutablePath()
        path.move(to: CGPoint(x: -0.025, y: 0.1))
        path.addLine(to: CGPoint(x: -0.07, y: -0.18))
        path.addLine(to: CGPoint(x: 0.045, y: -0.2))
        path.addLine(to: CGPoint(x: 0.09, y: 0.08))
        path.addLine(to: CGPoint(x: 0.045, y: 0.18))
        path.closeSubpath()
        return [DotRender(x: 1.03 + sin(t * 7) * 0.065, y: 0.55 + cos(t * 7) * 0.025,
                          r: 1, opacity: 1, d: path, rot: 28 + sin(t * 7) * 8)]
    case .sending, .receiving:
        let phase = (t / 1.5).truncatingRemainder(dividingBy: 1)
        let distance = mood == .sending ? 0.95 + phase * 0.4 : 1.35 - phase * 0.4
        return [DotRender(x: distance, y: -0.5, r: 0.055,
                          opacity: sin(phase * .pi))]
    case .failed, .interrupted:
        let point = badgePoint(BADGE_FAR)
        return exclaimMarks(x: point.x, y: point.y, opacity: 1)
    case .celebrate, .done:
        return particles(t.truncatingRemainder(dividingBy: 2.8), 1.5)
    case .spawning:
        let cycle = t.truncatingRemainder(dividingBy: 3.5)
        return (0..<5).compactMap { index in
            let phase = clamp((cycle / 2 - Double(index) * 0.09) / 0.62)
            guard phase < 1 else { return nil }
            let progress = easings.easeOutCubic(phase)
            let angle = Double(index) * 2.4 + phase * 2.2
            let radius = 0.96 * (1 - progress)
            return DotRender(x: cos(angle) * radius, y: sin(angle) * radius * 0.8,
                             r: 0.045 * (1 + progress),
                             opacity: clamp(phase * 5) * (1 - progress * 0.25))
        }
    case .sleeping:
        let rise = (t / 2).truncatingRemainder(dividingBy: 1)
        return zMarks(x: 0.9 + rise * 0.2, y: -0.75 - rise * 0.35,
                      size: 0.8, opacity: sin(rise * .pi))
    default: return []
    }
}
