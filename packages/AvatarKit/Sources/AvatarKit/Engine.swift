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

private typealias FaceFit = (width: Double, height: Double, gap: Double, y: Double)

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
    private var fitPrev: FaceFit?
    private var expr: BotExpression?
    private var exprPrev: BotExpression?
    private var exprAt = -10.0
    private var look = Look()
    private var lookPrev = Look()
    private var lookAt = -10.0
    private var lookMorph = LOOK_MORPH
    private var turn = Turn()
    private var turnPrev = Turn()
    private var turnAt = -10.0
    /// The body drawn when turned; nil keeps the flat outline. Set it with the shape so the two morph together.
    public var solid: Solid? {
        didSet { if solid != oldValue { solidPrev = oldValue } }
    }
    private var solidPrev: Solid?
    private var pokes: [(poke: Poke, at: Double)] = []
    private var mood: MoodID?
    private var moods = MoodPlayer()
    private var interaction = false
    private var moodFace: BotExpression?
    private var facePrev: BotExpression?
    private var faceAt = 0.0
    private var spawning = SpawnBlend()

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
        fitPrev = fitAtTime(now)
        solidPrev = solid
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

    public func poke(_ poke: Poke, _ now: Double) {
        pokes.append((poke, now))
    }

    public func setMood(_ mood: MoodID?, interaction: Bool = false) {
        self.mood = mood
        self.interaction = self.interaction || interaction
        if mood == nil { moodFace = nil; facePrev = nil }
    }

    private func faceAtTime(_ now: Double) -> BotExpression? {
        guard let moodFace, let facePrev else { return moodFace }
        return blendExpression(facePrev, moodFace, easings.easeOutQuint(clamp((now - faceAt) / 0.3)))
    }

    /// Eyes placed where they sit on the body at rest, then carried by its rotation.
    private func fixedEyes(_ pose: Pose, _ body: Rotation, _ decalage: Point, _ offX: Double, _ offY: Double,
                           stretch: (x: Double, y: Double), now: Double) -> [EyePose] {
        guard let solid else { return [] }
        let id = SHAPES.first { $0.radii == shape }?.id
        if id != .circle { return surfaceEyes(pose, body, offX, offY, stretch: stretch, now: now) }
        let r = scale
        return eyeFrames(REST_GAZE, pose.split).map { eye in
            let rx = eye.f.x, ry = eye.f.y
            let fit = radiusAtAngle(pose.sil.radii, atan2(ry, rx) - pose.sil.rot)
            let ux = rx * fit + decalage.x - pose.sil.cx, uy = ry * fit + decalage.y - pose.sil.cy
            let edge = max(0.01, radiusAtAngle(pose.sil.radii, atan2(uy, ux) - pose.sil.rot))
            let z = solid.front(hypot(ux, uy) / edge)
            var normal = Vec3(x: 0, y: 0, z: 1)
            if case .inflated(let depth) = solid {
                normal = Vec3(x: ux / (edge * edge), y: uy / (edge * edge), z: z / (depth * depth))
            }
            let sx = pose.sil.sx * stretch.x, sy = pose.sil.sy * stretch.y
            let p = body.apply(Vec3(x: ux * sx, y: uy * sy, z: z))
            func stretched(_ v: Vec3) -> Vec3 { body.apply(Vec3(x: v.x * sx, y: v.y * sy, z: v.z)) }
            let right = stretched(eye.right), down = stretched(eye.down), n = body.apply(normal)
            let length = max(1e-9, sqrt(n.x * n.x + n.y * n.y + n.z * n.z))
            return EyePose(x: (p.x + pose.sil.cx + offX) * r, y: (p.y + pose.sil.cy + offY) * r,
                           a: right.x, b: right.y, c: down.x, d: down.y, depth: n.z / length)
        }
    }

    private func faceFit(_ radii: [Double]?) -> FaceFit {
        let fit: FaceFit
        switch SHAPES.first(where: { $0.radii == radii })?.id {
        case .cloud: fit = (0.213, 0.467, 0.47, 0.07)
        case .square: fit = (0.255, 0.56, 0.529, 0.12)
        case .sparkle: fit = (0.184, 0.403, 0.412, 0)
        case .clover: fit = (0.233, 0.511, 0.559, -0.02)
        case .heart: fit = (0.187, 0.409, 0.446, 0.052)
        case .flower: fit = (0.193, 0.423, 0.383, 0.12)
        case .droplet: fit = (0.208, 0.456, 0.453, 0.18)
        case .tablet: fit = (0.213, 0.467, 0.588, 0.02)
        case .wedge, .triangle: fit = (0.213, 0.467, 0.468, 0.18)
        case .house: fit = (0.23, 0.503, 0.513, 0.185)
        case .star: fit = (0.185, 0.45, 0.419, 0.12)
        case .hexagon: fit = (0.26, 0.57, 0.64, 0.12)
        default: fit = (0.239, 0.524, 0.546, 0.12)
        }
        return fit
    }

    private func fitAtTime(_ now: Double) -> FaceFit {
        let target = faceFit(shape)
        guard let from = fitPrev else { return target }
        let amount = easings.easeOutQuint(clamp((now - shapeAt) / Self.SHAPE_MORPH))
        return (width: lerp(from.width, target.width, amount),
                height: lerp(from.height, target.height, amount),
                gap: lerp(from.gap, target.gap, amount), y: lerp(from.y, target.y, amount))
    }

    private func surfaceEyes(_ pose: Pose, _ body: Rotation, _ offX: Double, _ offY: Double,
                             stretch: (x: Double, y: Double), now: Double) -> [EyePose] {
        let fit = fitAtTime(now)
        let amount = easings.easeOutQuint(clamp((now - shapeAt) / Self.SHAPE_MORPH))
        func unit(_ v: Vec3) -> Vec3 {
            let length = max(1e-9, sqrt(v.x * v.x + v.y * v.y + v.z * v.z))
            return Vec3(x: v.x / length, y: v.y / length, z: v.z / length)
        }
        let sx = pose.sil.sx * stretch.x, sy = pose.sil.sy * stretch.y
        return [-1.0, 1.0].map { side in
            let x = side * fit.gap / 2, y = sin(fit.y)
            var face = surface(solid!, x, y, pose.sil.radii)
            if amount < 1, let solidPrev, solidPrev != solid {
                let before = surface(solidPrev, x, y, pose.sil.radii)
                face = (lerp(before.z, face.z, amount),
                        Vec3(x: lerp(before.normal.x, face.normal.x, amount),
                             y: lerp(before.normal.y, face.normal.y, amount),
                             z: lerp(before.normal.z, face.normal.z, amount)))
            }
            let p = body.apply(Vec3(x: x * sx, y: y * sy, z: face.z))
            let front = max(0.01, face.normal.z)
            let right = unit(Vec3(x: 1, y: 0, z: -face.normal.x / front))
            let down = unit(Vec3(x: 0, y: 1, z: -face.normal.y / front))
            let a = body.apply(Vec3(x: right.x * sx, y: right.y * sy, z: right.z))
            let b = body.apply(Vec3(x: down.x * sx, y: down.y * sy, z: down.z))
            let normal = unit(body.apply(face.normal))
            return EyePose(x: (p.x + pose.sil.cx + offX) * scale,
                           y: (p.y + pose.sil.cy + offY) * scale,
                           a: a.x * fit.width / EYE_W, b: a.y * fit.width / EYE_W,
                           c: b.x * fit.height / EYE_H, d: b.y * fit.height / EYE_H,
                           depth: normal.z)
        }
    }

    public func setTurn(_ target: Turn, _ now: Double) {
        guard target.yaw.isFinite, target.pitch.isFinite else { return }
        turnPrev = turnAtTime(now); turn = target; turnAt = now
    }

    private func turnAtTime(_ now: Double) -> Turn {
        let k = (now - turnAt) / Self.LOOK_MORPH
        if k >= 1 { return turn }
        let t = easings.easeOutQuint(clamp(k))
        return Turn(yaw: lerp(turnPrev.yaw, turn.yaw, t), pitch: lerp(turnPrev.pitch, turn.pitch, t))
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

        var spawnTime: Double?
        if let mood {
            let frame = moods.sample(mood, now: now, interaction: interaction)
            if frame.mood == .spawning { spawnTime = frame.elapsed }
            interaction = false
            let target = frame.mood.expression?.expression ?? expr ?? ExpressionID.neutral.expression
            if moodFace != target {
                facePrev = faceAtTime(now); moodFace = target; faceAt = now
            }
            let face = faceAtTime(now) ?? target
            let motion = frame.motion
            let notice = pose.notif
            pose = Pose(sil: Silhouette(radii: shape ?? pose.sil.radii,
                                        sx: motion.zoom * (1 - motion.stretch),
                                        sy: motion.zoom * (1 + motion.stretch)),
                        offX: motion.x, offY: motion.y,
                        gaze: HeadGaze(yaw: face.gaze.yaw + motion.yaw,
                                       pitch: face.gaze.pitch + motion.pitch,
                                       roll: face.gaze.roll + motion.roll),
                        split: face.split,
                        eyes: face.eyes.map { eye in
                            var value = eye; value.open = min(value.open, motion.lid); return value
                        }, dots: moodMarks(frame.mood, frame.elapsed), notif: notice)
            decalage = decalageDesYeux(shape, .idle, face.id.rawValue)
        }
        let spawn = spawning.sample(elapsed: spawnTime, now: now)
        if spawn.amount >= 0.5 { pose.eyeAlpha = 0 }
        if spawnTime != nil {
            let visible = easings.easeInOutCubic(spawn.amount)
            pose.dots = pose.dots.map { dot in
                var value = dot; value.r *= visible; value.opacity *= visible; return value
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
        // With a solid, the eyes are fixed on the body and the head's turn from rest turns it all.
        let turned = turnAtTime(now)
        pokes.removeAll { now - $0.at > $0.poke.duration }
        let motion = pokeMotion(pokes, now)
        var offX = pose.offX + life.driftX, offY = pose.offY + life.driftY
        var sil = pose.sil
        var body = Rotation.identity
        if solid != nil {
            body = Rotation(yaw: turned.yaw, pitch: turned.pitch) * motion.rotation * Rotation(from: REST_GAZE, to: gaze)
            if shape != ShapeID.circle.radii { body = body * .rest }
            // A turned body leans a little towards where it faces.
            offX += sin(turned.yaw * .pi / 180) * 0.15
            offY += sin(turned.pitch * .pi / 180) * 0.12 - motion.lift
            sil.sx *= motion.squash.x; sil.sy *= motion.squash.y
        }
        if spawn.amount > 0 {
            let keep = 1 - spawn.amount
            offX *= keep; offY *= keep
            let facing = HeadGaze(yaw: lerp(REST_GAZE.yaw, gaze.yaw, keep),
                                  pitch: lerp(REST_GAZE.pitch, gaze.pitch, keep),
                                  roll: lerp(REST_GAZE.roll, gaze.roll, keep))
            body = Rotation(yaw: turned.yaw * keep + spawn.spin, pitch: turned.pitch * keep)
                * Rotation(from: REST_GAZE, to: facing)
            if shape != ShapeID.circle.radii {
                body = body * Rotation(yaw: 33.293 * keep, pitch: -15.896 * keep, roll: -25.385 * keep)
            }
        }
        sil.cx += offX; sil.cy += offY; sil.sy *= life.breath
        var bodyPath: CGPath
        // A round sphere looks the same from every side.
        let sphere = solid == .inflated(depth: 1) && sil.radii.allSatisfy { abs($0 - sil.radii[0]) < 1e-9 }
        if let solid, case .slab = solid, solidPrev == nil || solidPrev == solid || now - shapeAt >= Self.SHAPE_MORPH {
            bodyPath = solidPath(sil, solid, body, scale: r)
        } else if let solid, !body.isIdentity, !sphere {
            var outline = solidOutline(sil, solid, body, scale: r)
            let k = (now - shapeAt) / Self.SHAPE_MORPH
            if k < 1, let solidPrev, solidPrev != solid {
                // Both outlines sample the same rays, so they blend point by point.
                let from = solidOutline(sil, solidPrev, body, scale: r)
                let t = easings.easeOutQuint(clamp(k))
                outline = zip(from, outline).map { Point(x: lerp($0.x, $1.x, t), y: lerp($0.y, $1.y, t)) }
            }
            bodyPath = closedPath(outline)
        } else {
            bodyPath = closedPath(toPoints(sil, r))
        }
        if spawn.amount > 0 {
            bodyPath = spawnPath(bodyPath, center: Point(x: sil.cx * r, y: sil.cy * r),
                                 radius: r, amount: spawn.amount)
        }
        func bodyRadius(_ x: Double, _ y: Double) -> Double {
            radiusAtAngle(pose.sil.radii, atan2(y, x) - pose.sil.rot)
        }

        var eyes: [RenderedEye] = []
        if pose.eyeAlpha > 0.01 {
            let poses = solid == nil ? eyePoses(gaze, r, pose.split)
                : fixedEyes(pose, body, decalage, offX, offY, stretch: motion.squash, now: now)
            for i in 0..<2 {
                let e = poses[i]
                if e.depth <= 0.02 { continue }
                let cfg = pose.eyes[i], fit = solid == nil ? bodyRadius(e.x, e.y) : 1
                let phi = cfg.tilt * Double.pi / 180, cp = cos(phi), sp = sin(phi)
                let ax = e.a * cp + e.c * sp, ay = e.b * cp + e.d * sp
                let cx2 = -e.a * sp + e.c * cp, cy2 = -e.b * sp + e.d * cp
                let k = blinkScale(min(lid, cfg.open))
                let shift = solid == nil ? Point(x: (offX + decalage.x) * r, y: (offY + decalage.y) * r) : Point()
                var matrix = CGAffineTransform(a: r2(ax), b: r2(ay * k), c: r2(cx2), d: r2(cy2 * k),
                                               tx: r2(e.x * fit + shift.x), ty: r2(e.y * fit + shift.y))
                if spawn.amount > 0 {
                    let size = lerp(1, 19 / 114.5, spawn.amount)
                    matrix.a *= size; matrix.b *= size; matrix.c *= size; matrix.d *= size
                    matrix.tx = sil.cx * r + (matrix.tx - sil.cx * r) * size
                    matrix.ty = sil.cy * r + (matrix.ty - sil.cy * r) * size
                }
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
