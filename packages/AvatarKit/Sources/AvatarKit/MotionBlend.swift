import Foundation

/// A critically damped target that keeps its velocity when redirected.
struct MotionSpring {
    private var value = 0.0
    private var velocity = 0.0
    private var target = 0.0
    private var changed = 0.0

    func sample(_ now: Double) -> (value: Double, velocity: Double) {
        let time = max(0, now - changed), rate = 24.0
        let offset = value - target, carry = velocity + rate * offset
        let decay = exp(-rate * time)
        return (target + (offset + carry * time) * decay,
                (velocity - rate * carry * time) * decay)
    }

    mutating func retarget(_ target: Double, _ now: Double) {
        guard target != self.target else { return }
        let current = sample(now)
        value = current.value; velocity = current.velocity
        self.target = target; changed = now
    }
}

/// Removes a pose offset gradually, preserving the incoming velocity at a handoff.
struct MotionBlend {
    var offset = 0.0
    var velocity = 0.0
    var ending = 0.0

    func sample(_ elapsed: Double, duration: Double) -> Double {
        let t = clamp(elapsed / duration), rest = 1 - t
        return offset * rest * rest * (1 + 2 * t)
            + duration * (velocity * t * rest * rest - ending * t * t * rest)
    }
}
