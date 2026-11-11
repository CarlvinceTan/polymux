import Foundation

public struct Point: Hashable, Sendable, Codable {
    public var x: Double
    public var y: Double

    public init(x: Double, y: Double) {
        self.x = x
        self.y = y
    }
}

public struct Size: Hashable, Sendable, Codable {
    public var w: Double
    public var h: Double

    public init(w: Double, h: Double) {
        self.w = w
        self.h = h
    }
}

public struct Obstacle: Hashable, Sendable, Codable {
    public var x: Double
    public var y: Double
    public var w: Double
    public var h: Double

    public init(x: Double, y: Double, w: Double, h: Double) {
        self.x = x
        self.y = y
        self.w = w
        self.h = h
    }
}

public enum Side: String, CaseIterable, Sendable, Codable {
    // This order breaks ties in the side picker.
    case right, bottom, left, top
}

public enum WireAxis: Sendable {
    case horizontal, vertical
}

public enum PathCommand: Hashable, Sendable {
    case move(Point)
    case line(Point)
    case quad(control: Point, to: Point)
}

/// `Math.round`: halves go up, where Swift's default goes away from zero.
func roundHalfUp(_ value: Double) -> Double {
    let floor = value.rounded(.down)
    return value - floor >= 0.5 ? floor + 1 : floor
}

/// A node's box on the canvas.
public struct Box: Hashable, Sendable, Codable {
    public var id: String
    public var x: Double
    public var y: Double
    public var w: Double
    public var h: Double

    public init(id: String, x: Double, y: Double, w: Double, h: Double) {
        self.id = id
        self.x = x
        self.y = y
        self.w = w
        self.h = h
    }

    public var position: Point { Point(x: x, y: y) }
    public var size: Size { Size(w: w, h: h) }
    var center: Point { Point(x: x + w / 2, y: y + h / 2) }
}
