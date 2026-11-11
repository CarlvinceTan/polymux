import SwiftUI
import GraphKit

/// Turns routed wires into what SwiftUI draws.
@MainActor enum GraphPaths {
    static func path(_ commands: [PathCommand], model: GraphModel) -> Path {
        var path = Path()
        for command in commands {
            switch command {
            case .move(let point): path.move(to: model.point(point))
            case .line(let point): path.addLine(to: model.point(point))
            case .quad(let control, let point): path.addQuadCurve(to: model.point(point), control: model.point(control))
            }
        }
        return path
    }

    /// A route redrawn with wide, soft corners, which makes a lane of what was a wire.
    static func lane(_ commands: [PathCommand], model: GraphModel) -> Path {
        let corners = corners(commands).map(model.point)
        var path = Path()
        guard let first = corners.first, let last = corners.last, corners.count >= 2 else { return path }
        path.move(to: first)
        let wanted = 22 * CGFloat(model.scale)
        for index in corners.indices.dropFirst().dropLast() {
            let before = corners[index - 1]
            let corner = corners[index]
            let after = corners[index + 1]
            let inLength = hypot(corner.x - before.x, corner.y - before.y)
            let outLength = hypot(after.x - corner.x, after.y - corner.y)
            // Two corners on one segment share it, so neither arc can take more than half.
            let radius = min(wanted, inLength / 2, outLength / 2)
            guard radius > 0.5 else {
                path.addLine(to: corner)
                continue
            }
            path.addLine(to: CGPoint(x: corner.x - (corner.x - before.x) / inLength * radius, y: corner.y - (corner.y - before.y) / inLength * radius))
            path.addQuadCurve(to: CGPoint(x: corner.x + (after.x - corner.x) / outLength * radius, y: corner.y + (after.y - corner.y) / outLength * radius),
                              control: corner)
        }
        path.addLine(to: last)
        return path
    }

    /// Where a route starts, turns and ends.
    nonisolated static func corners(_ commands: [PathCommand]) -> [GraphKit.Point] {
        var out: [GraphKit.Point] = []
        for (index, command) in commands.enumerated() {
            switch command {
            case .move(let point): out.append(point)
            case .quad(let control, _): out.append(control)
            case .line(let point): if index == commands.count - 1 { out.append(point) }
            }
        }
        return out
    }

    /// The corners and ends of a route, enough to test a click against.
    nonisolated static func points(_ commands: [PathCommand]) -> [GraphKit.Point] {
        commands.flatMap { command -> [GraphKit.Point] in
            switch command {
            case .move(let point), .line(let point): [point]
            case .quad(let control, let point): [control, point]
            }
        }
    }

    nonisolated static func distance(_ point: GraphKit.Point, _ a: GraphKit.Point, _ b: GraphKit.Point) -> Double {
        let dx = b.x - a.x
        let dy = b.y - a.y
        let length = dx * dx + dy * dy
        let t = length == 0 ? 0 : min(1, max(0, ((point.x - a.x) * dx + (point.y - a.y) * dy) / length))
        return hypot(point.x - (a.x + t * dx), point.y - (a.y + t * dy))
    }

    /// An open arrowhead ending at the tip, pointing away from `from`.
    nonisolated static func arrow(tip: CGPoint, from: CGPoint, length: CGFloat) -> Path {
        let angle = atan2(tip.y - from.y, tip.x - from.x)
        let spread = CGFloat.pi / 5
        var path = Path()
        path.move(to: CGPoint(x: tip.x - length * cos(angle - spread), y: tip.y - length * sin(angle - spread)))
        path.addLine(to: tip)
        path.addLine(to: CGPoint(x: tip.x - length * cos(angle + spread), y: tip.y - length * sin(angle + spread)))
        return path
    }
}
