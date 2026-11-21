import CoreGraphics
import Foundation

/// Where something travelling between two places is, at a moment. Pure maths, so it can be checked without a screen.
public struct WorldRoute: Hashable, Sendable {
    public var from: CGPoint
    public var control: CGPoint
    public var to: CGPoint

    public init(from: CGPoint, control: CGPoint, to: CGPoint) {
        self.from = from
        self.control = control
        self.to = to
    }

    public func point(at t: Double) -> CGPoint {
        let one = 1 - t
        return CGPoint(x: one * one * from.x + 2 * one * t * control.x + t * t * to.x,
                       y: one * one * from.y + 2 * one * t * control.y + t * t * to.y)
    }
}

public enum WorldMotion {
    /// Seconds one trip takes, end to end.
    public static let trip = 4.2

    /// How far along its trip something is at a time, offset so several never move in lockstep.
    public static func along(seconds: Double, index: Int, spiral: Int = 3) -> Double {
        let phase = (seconds / trip + Double(index % max(1, spiral)) * 0.37).truncatingRemainder(dividingBy: 1)
        return phase < 0 ? phase + 1 : phase
    }

    /// The point something occupies, which is where it is drawn.
    public static func position(_ route: WorldRoute, seconds: Double, index: Int, reversed: Bool = false, still: Bool = false) -> CGPoint {
        let t = still ? 0.6 : along(seconds: seconds, index: index)
        return route.point(at: reversed ? 1 - t : t)
    }
}
