import CoreGraphics
import Foundation

// Grok Bot's dots morph, normalized from its 114.2705-point body radius.
struct ThinkingPhase {
    let amount: Double
    let time: Double
    let core: DotRender
    let reducedMotion: Bool
}

struct ThinkingMorph {
    /// Parts have faded out by this amount, so their detail need not be drawn or built.
    static let hidden = 0.22

    private var amount = 0.0
    private var from = 0.0
    private var exited: Double?
    private var clock = 0.0
    private var entering = 0.0
    private var offset = 0.0
    private var active = false

    /// Not still springing back to the body.
    var settled: Bool { exited == nil || amount <= 0.0001 }

    mutating func sample(elapsed: Double?, now: Double, reducedMotion: Bool) -> ThinkingPhase? {
        if let elapsed {
            if !active {
                entering = amount
                offset = exited.map { clock + now - $0 - elapsed } ?? 0
            }
            amount = reducedMotion ? 1 : lerp(entering, 1, springStep(elapsed))
            clock = elapsed + offset
            exited = nil
        } else if amount > 0.0001 {
            if exited == nil { exited = now; from = amount }
            amount = reducedMotion ? 0 : from * (1 - springStep(now - exited!))
        }
        active = elapsed != nil
        guard amount > 0.0001 else { return nil }
        let time = elapsed == nil ? clock + now - (exited ?? now) : clock
        return ThinkingPhase(amount: amount, time: time,
                             core: thinkingDot(1, time: time, amount: amount, reducedMotion: reducedMotion),
                             reducedMotion: reducedMotion)
    }

    func apply(_ frame: BotFrame, phase: ThinkingPhase, scale: Double, displaySize: Double) -> BotFrame {
        let amount = phase.amount, core = phase.core
        let compact = clamp((displaySize - 44) / 90)
        let zoom = 1 + 0.5 * amount * (1 - compact * compact * (3 - 2 * compact))
        let bounds = frame.bodyPath.boundingBoxOfPath
        let center = Point(x: bounds.midX, y: bounds.midY)
        let hub = core.r
        var transform = CGAffineTransform(a: zoom, b: 0, c: 0, d: zoom,
                                          tx: -center.x * amount * zoom,
                                          ty: (-center.y * amount + core.y * scale * amount) * zoom)
        var result = frame
        // Fading parts read as a ghost of the avatar, so they go early.
        let fade = max(0, 1 - amount / Self.hidden)
        result.parts = frame.parts.compactMap { part in
            var part = part
            part.alpha *= fade
            guard part.alpha > 0.0001 else { return nil }
            part.path = part.path.copy(using: &transform)!
            part.outline = part.outline?.copy(using: &transform)
            return part
        }
        let body = amount >= 0.999
            ? CGPath(ellipseIn: CGRect(x: center.x - scale * hub, y: center.y - scale * hub,
                                       width: 2 * scale * hub, height: 2 * scale * hub), transform: nil)
            : spawnPath(frame.bodyPath, center: center, radius: scale, amount: amount, hub: hub)
        result.bodyPath = body.copy(using: &transform)!
        result.bodyAlpha *= 1 - (1 - core.opacity) * amount
        result.bare = max(result.bare, amount)
        result.eyes = amount > Self.hidden ? [] : frame.eyes.map { eye in
            var eye = eye
            eye.alpha *= fade
            let size = lerp(1, hub, amount)
            eye.matrix = eye.matrix.concatenating(CGAffineTransform(a: size, b: 0, c: 0, d: size,
                tx: center.x * (1 - size), ty: center.y * (1 - size))).concatenating(transform)
            return eye
        }
        result.dots = frame.dots.map { dot in
            var dot = dot; dot.opacity *= 1 - amount; return dot
        } + [0, 2].compactMap { index in
            let progress = clamp((amount - (index == 0 ? 0 : 0.12)) / (index == 0 ? 1 : 0.88))
            guard progress > 0.004 else { return nil }
            let visible = easings.easeOutCubic(progress)
            let spread = 1 + 2.70158 * pow(progress - 1, 3) + 1.70158 * pow(progress - 1, 2)
            var dot = thinkingDot(index, time: phase.time, amount: amount, reducedMotion: phase.reducedMotion)
            dot.x = Double(index - 1) * 62 / 114.2705 * spread * scale * zoom
            dot.y *= scale * zoom
            dot.r *= visible * 1.02 * scale * zoom
            dot.opacity *= visible
            return dot
        }
        return result
    }
}

func thinkingDot(_ index: Int, time: Double, amount: Double, reducedMotion: Bool) -> DotRender {
    let phase = (time / 1.4 + 0.119).truncatingRemainder(dividingBy: 1)
    let distance = abs(phase - Double(index) / 3)
    let wave = reducedMotion ? 1 : exp(-pow(min(distance, 1 - distance), 2) / 0.045)
    return DotRender(x: Double(index - 1) * 62 / 114.2705,
                     y: reducedMotion ? 0 : -wave * 9 / 114.2705 * amount,
                     r: 22 / 114.2705 * (reducedMotion ? 1 : 0.84 + 0.22 * wave),
                     opacity: reducedMotion ? 1 : 0.5 + 0.5 * wave)
}
