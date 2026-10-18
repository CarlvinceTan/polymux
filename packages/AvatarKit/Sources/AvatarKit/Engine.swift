import CoreGraphics
import Foundation

public struct RenderedEye: @unchecked Sendable {
    public var d: CGPath
    public var matrix: CGAffineTransform
    public var alpha: Double
}

public struct FrameDot: Sendable {
    public var x, y, r: Double
}

public struct BotFrame: @unchecked Sendable {
    public var bodyPath: CGPath
    public var bodyAlpha: Double
    public var eyes: [RenderedEye]
    public var dots: [DotRender]
    public var dotsBehind: Bool
    public var arcs: [ArcRender]
    public var notif, notch: FrameDot?
}

public struct Look: Equatable, Codable, Sendable {
    public var yaw, pitch, mix, spin, wander: Double
    public init(yaw: Double = 0, pitch: Double = 0, mix: Double = 0,
                spin: Double = 0, wander: Double = 1) {
        self.yaw = yaw; self.pitch = pitch; self.mix = mix; self.spin = spin; self.wander = wander
    }
}

private func lerpLook(_ a: Look, _ b: Look, _ t: Double) -> Look {
    Look(yaw: lerp(a.yaw, b.yaw, t), pitch: lerp(a.pitch, b.pitch, t),
         mix: lerp(a.mix, b.mix, t), spin: lerp(a.spin, b.spin, t), wander: lerp(a.wander, b.wander, t))
}

private func lerpEye(_ a: EyeCfg, _ b: EyeCfg, _ t: Double) -> EyeCfg {
    EyeCfg(w: lerp(a.w, b.w, t), h: lerp(a.h, b.h, t), open: lerp(a.open, b.open, t),
           tilt: lerp(a.tilt, b.tilt, t))
}

private func blendPose(_ a: Pose, _ b: Pose, _ t: Double) -> Pose {
    let out = 1 - t
    return Pose(sil: blend(a.sil, b.sil, t), offX: lerp(a.offX, b.offX, t), offY: lerp(a.offY, b.offY, t),
                gaze: HeadGaze(yaw: lerp(a.gaze.yaw, b.gaze.yaw, t), pitch: lerp(a.gaze.pitch, b.gaze.pitch, t),
                               roll: lerp(a.gaze.roll, b.gaze.roll, t)),
                split: lerp(a.split, b.split, t),
                eyes: [lerpEye(a.eyes[0], b.eyes[0], t), lerpEye(a.eyes[1], b.eyes[1], t)],
                eyeAlpha: lerp(a.eyeAlpha, b.eyeAlpha, t), bodyAlpha: lerp(a.bodyAlpha, b.bodyAlpha, t),
                dots: a.dots.map { var d = $0; d.opacity *= out; return d }
                      + b.dots.map { var d = $0; d.opacity *= t; return d },
                arcs: a.arcs.map { var r = $0; r.id = "a" + r.id; r.opacity *= out; return r }
                      + b.arcs.map { var r = $0; r.id = "b" + r.id; r.opacity *= t; return r },
                notif: t < 0.5 ? a.notif : b.notif, dotsBehind: t < 0.5 ? a.dotsBehind : b.dotsBehind)
}

public final class BotEngine {
    public static let SHAPE_MORPH = 0.45
    public static let LOOK_MORPH = 0.24
    public let scale: Double
    public var state: StateID { cur }

    private var cur: StateID
    private var prev: StateID?
    private var departFige: Pose?
    private var tCur = 0.0
    private var tPrev = 0.0
    private var blinkAt = -10.0
    private var shape: [Double]?
    private var shapePrev: [Double]?
    private var shapeAt = -10.0
    private var expr: BotExpression?
    private var exprPrev: BotExpression?
    private var exprAt = -10.0
    private var look = Look()
    private var lookPrev = Look()
    private var lookAt = -10.0
    private var lookMorph = LOOK_MORPH

    public init(scale: Double = 100, initial: StateID = .idle,
                shape: [Double]? = nil, expression: BotExpression? = nil) {
        self.scale = scale; cur = initial; self.shape = shape; expr = expression
    }

    public func setExpression(_ expression: BotExpression?, _ now: Double = 0) {
        if expression == expr { return }
        exprPrev = expr; expr = expression; exprAt = now
    }

    private func exprAtTime(_ now: Double) -> BotExpression? {
        guard let to = expr, let from = exprPrev else { return expr }
        let k = (now - exprAt) / Self.SHAPE_MORPH
        if k >= 1 { return to }
        return blendExpression(from, to, easings.easeOutQuint(clamp(k)))
    }

    public func setShape(_ radii: [Double]?, _ now: Double = 0) {
        if radii == shape { return }
        shapePrev = shape; shape = radii; shapeAt = now
    }

    private func shapeAtTime(_ now: Double) -> [Double]? {
        guard let to = shape, let from = shapePrev else { return shape }
        let k = (now - shapeAt) / Self.SHAPE_MORPH
        if k >= 1 { return to }
        let t = easings.easeOutQuint(clamp(k))
        return to.enumerated().map { i, r in lerp(i < from.count ? from[i] : r, r, t) }
    }

    public func setLook(_ target: Look?, _ now: Double, morph: Double = LOOK_MORPH) {
        if let target, !(target.yaw + target.pitch + target.mix + target.spin + target.wander).isFinite { return }
        lookPrev = lookAtTime(now); look = target ?? Look(); lookAt = now; lookMorph = morph
    }

    private func lookAtTime(_ now: Double) -> Look {
        let k = (now - lookAt) / lookMorph
        if k >= 1 { return look }
        return lerpLook(lookPrev, look, easings.easeOutQuint(clamp(k)))
    }

    private func posed(_ def: StateDef, _ t: Double, _ shape: [Double]?, _ expression: BotExpression?) -> Pose {
        var pose = def.pose(t)
        if def.baseBody, let shape { pose.sil.radii = shape }
        if def.baseFace, let expression {
            pose.gaze = expression.gaze; pose.split = expression.split; pose.eyes = expression.eyes
        }
        return pose
    }

    private func decalageAtTime(_ now: Double, _ state: StateID) -> Point {
        func axis(_ debut: Double, _ duree: Double, _ a: Point, _ b: Point) -> Point {
            let k = (now - debut) / duree
            if k >= 1 { return b }
            let t = easings.easeOutQuint(clamp(k))
            return Point(x: lerp(a.x, b.x, t), y: lerp(a.y, b.y, t))
        }
        func parForme(_ radii: [Double]?) -> Point {
            axis(exprAt, Self.SHAPE_MORPH,
                 decalageDesYeux(radii, state, exprPrev?.id.rawValue),
                 decalageDesYeux(radii, state, expr?.id.rawValue))
        }
        return axis(shapeAt, Self.SHAPE_MORPH, parForme(shapePrev), parForme(shape))
    }

    public func reset(_ id: StateID, _ now: Double) {
        cur = id; prev = nil; departFige = nil; tCur = now; tPrev = now; blinkAt = -10
    }

    private func origine(_ now: Double, _ shape: [Double]?, _ expr: BotExpression?) -> Pose? {
        if let departFige { return departFige }
        guard let prev else { return nil }
        return posed(STATE_BY_ID[prev]!, max(0, now - tPrev), shape, expr)
    }

    private func poseComposee(_ now: Double) -> Pose {
        let def = STATE_BY_ID[cur]!
        let shape = shapeAtTime(now), expr = exprAtTime(now)
        let pose = posed(def, max(0, now - tCur), shape, expr)
        let since = now - tCur
        guard since < def.morph, let from = origine(now, shape, expr) else { return pose }
        return blendPose(from, pose, easings.easeOutQuint(clamp(since / def.morph)))
    }

    public func setState(_ id: StateID, _ now: Double, restart: Bool = false) {
        if id == cur && !restart { return }
        let morph = STATE_BY_ID[cur]!.morph
        let interrupted = prev != nil && now - tCur < morph
        departFige = interrupted || restart ? poseComposee(now) : nil
        prev = cur; tPrev = tCur; cur = id; tCur = now
        if STATE_BY_ID[id]!.blinkIn { blinkAt = now }
    }

    public func sample(_ now: Double) -> BotFrame {
        let r = scale, def = STATE_BY_ID[cur]!
        let shape = shapeAtTime(now), expr = exprAtTime(now)
        var pose = posed(def, max(0, now - tCur), shape, expr)
        var decalage = decalageAtTime(now, cur)
        let since = now - tCur
        if since < def.morph, let from = origine(now, shape, expr) {
            let ratio = easings.easeOutQuint(clamp(since / def.morph))
            pose = blendPose(from, pose, ratio)
            if let prev {
                let before = decalageAtTime(now, prev)
                decalage = Point(x: lerp(before.x, decalage.x, ratio), y: lerp(before.y, decalage.y, ratio))
            }
        }

        let alive = pose.eyeAlpha > 0.01
        let look = lookAtTime(now)
        let life = liveliness(now, wander: alive ? look.wander : 0, blink: alive)
        let gaze = HeadGaze(yaw: lerp(pose.gaze.yaw, look.yaw, look.mix) + life.dYaw - look.spin,
                            pitch: lerp(pose.gaze.pitch, look.pitch, look.mix) + life.dPitch,
                            roll: pose.gaze.roll + life.dRoll)
        let forced = clamp((now - blinkAt) / 0.2)
        let forcedLid = forced < 1 ? abs(forced * 2 - 1) : 1
        let lid = min(life.lid, forcedLid)
        let offX = pose.offX + life.driftX, offY = pose.offY + life.driftY
        var sil = pose.sil
        sil.cx += offX; sil.cy += offY; sil.sy *= life.breath
        let bodyPath = closedPath(toPoints(sil, r))
        func bodyRadius(_ x: Double, _ y: Double) -> Double {
            radiusAtAngle(pose.sil.radii, atan2(y, x) - pose.sil.rot)
        }

        var eyes: [RenderedEye] = []
        if pose.eyeAlpha > 0.01 {
            let poses = eyePoses(gaze, r, pose.split)
            for i in 0..<2 {
                let e = poses[i]
                if e.depth <= 0.02 { continue }
                let cfg = pose.eyes[i], fit = bodyRadius(e.x, e.y)
                let phi = cfg.tilt * Double.pi / 180, cp = cos(phi), sp = sin(phi)
                let ax = e.a * cp + e.c * sp, ay = e.b * cp + e.d * sp
                let cx2 = -e.a * sp + e.c * cp, cy2 = -e.b * sp + e.d * cp
                let k = blinkScale(min(lid, cfg.open))
                let matrix = CGAffineTransform(a: r2(ax), b: r2(ay * k), c: r2(cx2), d: r2(cy2 * k),
                                               tx: r2(e.x * fit + (offX + decalage.x) * r),
                                               ty: r2(e.y * fit + (offY + decalage.y) * r))
                eyes.append(RenderedEye(d: capsulePath(cfg.w * r, cfg.h * r), matrix: matrix,
                                        alpha: pose.eyeAlpha * clamp(e.depth / 0.12)))
            }
        }

        let dots = pose.dots.filter { $0.opacity > 0.01 && $0.r > 0.0005 }.map { p in
            var d = p; d.x = (p.x + offX) * r; d.y = (p.y + offY) * r; d.r = p.r * r; return d
        }
        var notif: FrameDot?, notch: FrameDot?
        if let n = pose.notif {
            let fit = bodyRadius(n.x, n.y), nx = (n.x * fit + offX) * r, ny = (n.y * fit + offY) * r
            notif = FrameDot(x: nx, y: ny, r: n.r * r); notch = FrameDot(x: nx, y: ny, r: n.notch * r)
        }
        return BotFrame(bodyPath: bodyPath, bodyAlpha: pose.bodyAlpha, eyes: eyes, dots: dots,
                        dotsBehind: pose.dotsBehind,
                        arcs: pose.arcs.filter { $0.opacity > 0.01 }.map { arcRender($0.seed, $0.t, r, $0.id, $0.opacity) },
                        notif: notif, notch: notch)
    }
}
