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
