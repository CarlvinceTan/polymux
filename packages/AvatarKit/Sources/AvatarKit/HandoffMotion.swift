import CoreGraphics

public enum HandoffMotion {
    public static func sample(_ progress: Double, from: CGPoint, to: CGPoint) -> (point: CGPoint, scale: Double, alpha: Double) {
        let t = clamp(progress)
        let mid = CGPoint(x: (from.x + to.x) / 2, y: (from.y + to.y) / 2 - 56)
        let first = t < 0.5
        let k = mascotEase(first ? t * 2 : (t - 0.5) * 2, 0.4, 0.1, 0.4, 1)
        let a = first ? from : mid, b = first ? mid : to
        let alpha = t < 0.15 ? mascotEase(t / 0.15, 0.4, 0.1, 0.4, 1)
            : t > 0.82 ? 1 - mascotEase((t - 0.82) / 0.18, 0.4, 0.1, 0.4, 1) : 1
        return (CGPoint(x: lerp(a.x, b.x, k), y: lerp(a.y, b.y, k)), first ? lerp(0.6, 1, k) : lerp(1, 0.45, k), alpha)
    }
}
