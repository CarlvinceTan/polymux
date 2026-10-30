import Foundation

public let TAU = Double.pi * 2

public func clamp(_ v: Double, _ lo: Double = 0, _ hi: Double = 1) -> Double {
    v < lo ? lo : v > hi ? hi : v
}

public func lerp(_ a: Double, _ b: Double, _ t: Double) -> Double { a + (b - a) * t }

public enum easings {
    public static func easeOutCubic(_ t: Double) -> Double { 1 - pow(1 - t, 3) }
    public static func easeInOutCubic(_ t: Double) -> Double {
        t < 0.5 ? 4 * pow(t, 3) : 1 - pow(-2 * t + 2, 3) / 2
    }
    public static func easeOutQuint(_ t: Double) -> Double { 1 - pow(1 - t, 5) }
}

public func loopNoise(_ t: Double, _ period: Double, _ seed: Double = 0) -> Double {
    let p = t / period * TAU
    return 0.55 * sin(p + seed) + 0.3 * sin(2 * p + seed * 1.7 + 1.1)
        + 0.15 * sin(3 * p + seed * 2.3 + 2.4)
}

public func createRng(_ seed: UInt32) -> () -> Double {
    var a = seed
    return {
        a = a &+ 0x6d2b79f5
        var t = (a ^ (a >> 15)) &* (1 | a)
        t = (t &+ ((t ^ (t >> 7)) &* (61 | t))) ^ t
        return Double(t ^ (t >> 14)) / 4294967296
    }
}

public func r2(_ v: Double) -> Double { floor(v * 100 + 0.5) / 100 }

/// CSS cubic-bezier(x1, y1, x2, y2) easing at `t`, solved with Newton's method.
func bezierEase(_ t: Double, _ curve: [Double]) -> Double {
    func value(_ u: Double, _ a: Double, _ b: Double) -> Double {
        let s = 1 - u
        return 3 * s * s * u * a + 3 * s * u * u * b + u * u * u
    }
    var u = t
    for _ in 0..<8 {
        let error = value(u, curve[0], curve[2]) - t
        let s = 1 - u
        let slope = 3 * s * s * curve[0] + 6 * s * u * (curve[2] - curve[0]) + 3 * u * u * (1 - curve[2])
        if abs(error) < 1e-6 || abs(slope) < 1e-6 { break }
        u = clamp(u - error / slope)
    }
    return value(u, curve[1], curve[3])
}
