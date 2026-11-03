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
    public var parts: [CharacterLayer] = []
    public var transform = CGAffineTransform.identity
    /// How far the body has shrunk to a plain dot (thinking, spawning), where it reads like the decor dots.
    public var bare = 0.0
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
    public static let FRAME_SIZE = 316.0
    public let scale: Double
    public var state: StateID { cur }
    public var displaySize = FRAME_SIZE
    public var reducedMotion = false
    public var character: CharacterModel?
    public var interactive = false
    public var personality: UInt32 = 0 { didSet { if personality != oldValue { moods.seed = personality } } }
    /// Holds a still pose: the resting look, wearing the mood's face.
    public var resting = false
    /// Advances in real time, so a still pose eases in instead of being placed.
    public var live = false
    private var petted = false
    private var mascot = MascotPlayer()
    private var petFace = MotionSpring()

    public func pet(_ active: Bool, _ now: Double) {
        petted = active
        mascot.pet(active, now)
        petFace.retarget(active ? 1 : 0, now)
    }

    public func twirl(_ now: Double) {
        guard !reducedMotion else { return }
        mascot.spin(now)
    }

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
    private var yaw = MotionSpring()
    private var pitch = MotionSpring()
    /// The body drawn when turned; nil keeps the flat outline. Set it with the shape so the two morph together.
    public var solid: Solid? {
        didSet { if solid != oldValue { solidPrev = oldValue } }
    }
    private var solidPrev: Solid?
    private var pokes: [(poke: Poke, at: Double)] = []
    private var mood: MoodID?
    var moods = MoodPlayer(seed: 0)
    private var interaction = false
    private var spawning = SpawnBlend()
    private var thinking = ThinkingMorph()

    public init(scale: Double = 100, initial: StateID = .idle,
                shape: [Double]? = nil, expression: BotExpression? = nil) {
        self.scale = scale; cur = initial; self.shape = shape; expr = expression
        moods.shape = SHAPES.first { $0.radii == shape }?.id
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
        moods.shape = SHAPES.first { $0.radii == radii }?.id
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

    /// Turns a resting avatar to face a point, x right and y up in -1...1; nil eases it back.
    public func setGaze(_ point: [Double]?) { moods.setGaze(point) }

    /// Nothing is still easing, so a resting avatar can stop drawing.
    public func settled(_ now: Double) -> Bool {
        moods.settled && mascot.settled(now) && pokes.allSatisfy { now - $0.at > $0.poke.duration }
            && (shapePrev == nil || now - shapeAt >= Self.SHAPE_MORPH)
            && (exprPrev == nil || now - exprAt >= Self.SHAPE_MORPH)
            && thinking.settled && spawning.settled
    }

    private struct MeshKey: Equatable {
        var mesh: ShapeID?
        var frosting: Bool
        var turn: [Int]
        var sx, sy, cx, cy, scale: Int
    }
    private var meshCache: (key: MeshKey, frame: (CGPath, [CharacterLayer]), eyes: [EyePose])?

    /// A costly character is expensive to build, so a pose within a degree reuses the last one.
    private func meshRender(_ sil: Silhouette, _ body: Rotation, _ r: Double, now: Double) -> (CGPath, [CharacterLayer])? {
        guard let character else { return nil }
        // A morph or a flat pose draws its own body.
        let built = character.costly && !body.isIdentity && now - shapeAt >= Self.SHAPE_MORPH
        guard character.mesh != nil || built else { return nil }
        func quantised(_ value: Double, _ step: Double) -> Int { Int((value / step).rounded()) }
        let turn = body.turn
        let key = MeshKey(mesh: character.meshID, frosting: character.frosting, turn: turn.map { quantised($0, 0.5) },
                          sx: quantised(sil.sx, 0.01), sy: quantised(sil.sy, 0.01),
                          cx: quantised(sil.cx, 0.01), cy: quantised(sil.cy, 0.01), scale: quantised(r, 0.5))
        if let meshCache, meshCache.key == key { return meshCache.frame }
        var eyes: [EyePose] = []
        let frame = character.mesh?.render(sil, rotation: body, scale: r, eyes: { eyes = $0 })
            ?? (closedPath(solidOutline(sil, character.solid, body, scale: r, smooth: true)),
                character.layers(sil, rotation: body, scale: r))
        meshCache = (key, frame, eyes)
        return frame
    }

    public func setMood(_ mood: MoodID?, interaction: Bool = false) {
        self.mood = mood
        self.interaction = self.interaction || interaction
    }

    /// Eyes placed where they sit on the body at rest, then carried by its rotation.
    private func fixedEyes(_ pose: Pose, _ body: Rotation, _ decalage: Point, _ offX: Double, _ offY: Double,
                           stretch: (x: Double, y: Double), now: Double) -> [EyePose] {
        guard let solid else { return [] }
        if character?.mesh != nil { return meshCache?.eyes ?? [] }
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
        case .wedge: fit = (0.19, 0.42, 0.42, 0.22)
        case .triangle: fit = (0.213, 0.467, 0.468, 0.18)
        case .house: fit = (0.23, 0.503, 0.513, 0.185)
        case .star: fit = (0.185, 0.45, 0.419, 0.12)
        case .hexagon: fit = (0.26, 0.57, 0.64, 0.12)
        case .cupcake: fit = (0.18, 0.33, 0.44, 0.24)
        case .deer: fit = (0.16, 0.3, 0.4, 0)
        case .fox, .crab, .elephant, .octopus, .pig, .koala, .giraffe, .cow, .turtle,
             .tiger, .frog, .mouse, .walrus, .beaver, .guineaPig, .chinchilla, .ferret, .monkey,
             .chick, .duck, .penguin, .bat, .axolotl, .goose:
            fit = (0.18, 0.34, 0.46, -0.04)
        case .pear: fit = (0.14, 0.29, 0.28, 0.38)
        case .pumpkin, .apple, .strawberry, .bun, .toast, .cat, .dog, .panda, .rabbit:
            fit = (0.19, 0.39, 0.47, 0.08)
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
        yaw.retarget(target.yaw, now); pitch.retarget(target.pitch, now)
    }

    private func turnAtTime(_ now: Double) -> Turn {
        Turn(yaw: yaw.sample(now).value, pitch: pitch.sample(now).value)
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
        var thinkingTime: Double? = mood == nil && cur == .thinking ? max(0, since) : nil
        var clip: ClipFrame?
        if let mood {
            let frame = moods.sample(mood, now: now, interaction: interaction, compact: displaySize < 48,
                                     resting: resting, petted: interactive && petted, live: live)
            if frame.mood == .spawning { spawnTime = frame.elapsed }
            if frame.mood == .thinking { thinkingTime = frame.elapsed }
            interaction = false
            clip = frame.pose
            let zoom = frame.pose.zoom / REST_ZOOM, squash = frame.pose.squash
            pose = Pose(sil: Silhouette(radii: shape ?? pose.sil.radii, sx: zoom * squash[0], sy: zoom * squash[1]),
                        offX: frame.pose.pos[0] * zoom,
                        offY: -(frame.pose.anchor * (1 - squash[1]) + frame.pose.pos[1]) * zoom,
                        dots: moodMarks(frame.mood, frame.elapsed), notif: pose.notif)
        }
        let thinkingPhase = thinking.sample(elapsed: thinkingTime, now: now, reducedMotion: reducedMotion)
        let thinkingHidden = (thinkingPhase?.amount ?? 0) > ThinkingMorph.hidden
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
        // Clips carry their own blinks and drift.
        let life = clip == nil ? liveliness(now, wander: alive ? look.wander : 0, blink: alive)
            : liveliness(now, wander: 0, blink: false, float: false)
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
            body = Rotation(yaw: turned.yaw, pitch: turned.pitch) * motion.rotation
            if let clip { body = body * Rotation(turn: clip.rot) }
            else {
                body = body * Rotation(from: REST_GAZE, to: gaze)
                if shape != ShapeID.circle.radii { body = body * Rotation.rest }
            }
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
            if let clip { body = body * Rotation(turn: clip.rot.map { $0 * keep }) }
            else {
                body = body * Rotation(from: REST_GAZE, to: facing)
                if shape != ShapeID.circle.radii {
                    body = body * Rotation(yaw: 33.293 * keep, pitch: -15.896 * keep, roll: -25.385 * keep)
                }
            }
        }
        if let fit = character?.fit { sil.sx *= fit; sil.sy *= fit }
        sil.cx += offX; sil.cy += offY; sil.sy *= life.breath
        var bodyPath: CGPath
        let meshFrame = thinkingHidden ? nil : meshRender(sil, body, r, now: now)
        // A round sphere looks the same from every side.
        let sphere = solid == .inflated(depth: 1) && sil.radii.allSatisfy { abs($0 - sil.radii[0]) < 1e-9 }
        if let meshFrame {
            bodyPath = meshFrame.0
        } else if let solid, case .slab = solid, solidPrev == nil || solidPrev == solid || now - shapeAt >= Self.SHAPE_MORPH {
            bodyPath = solidPath(sil, solid, body, scale: r)
        } else if let solid, !body.isIdentity, !sphere {
            var outline = solidOutline(sil, solid, body, scale: r, smooth: character != nil)
            let k = (now - shapeAt) / Self.SHAPE_MORPH
            if k < 1, let solidPrev, solidPrev != solid {
                // Both outlines sample the same rays, so they blend point by point.
                let from = solidOutline(sil, solidPrev, body, scale: r, smooth: character != nil)
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
        // Eyes sit on the body at the size the character is drawn.
        var fitted = pose
        if let fit = character?.fit { fitted.sil.sx *= fit; fitted.sil.sy *= fit }
        var spawnScale = CGAffineTransform.identity
        if spawn.amount > 0 {
            let size = lerp(1, 19 / 114.5, spawn.amount)
            spawnScale = CGAffineTransform(a: size, b: 0, c: 0, d: size, tx: sil.cx * r * (1 - size), ty: sil.cy * r * (1 - size))
        }
        if pose.eyeAlpha > 0.01, let clip {
            let placed = solid == nil || (sphere && character == nil)
                ? sphereEyes(clip, body: body, center: Point(x: sil.cx * r, y: sil.cy * r), size: clip.zoom / REST_ZOOM * r)
                : solidEyes(clip, poses: fixedEyes(fitted, body, Point(), offX, offY, stretch: motion.squash, now: now))
            // A clip may slide the eyes across the face instead of turning the body.
            let shift = Point(x: clip.shift[0] * r, y: -clip.shift[1] * r)
            eyes = placed.map { eye in
                let points = shift == Point() ? eye.points : eye.points.map { Point(x: $0.x + shift.x, y: $0.y + shift.y) }
                return RenderedEye(d: closedPath(points), matrix: spawnScale, alpha: pose.eyeAlpha * eye.alpha)
            }
        } else if pose.eyeAlpha > 0.01 {
            let poses = solid == nil ? eyePoses(gaze, r, pose.split)
                : fixedEyes(fitted, body, decalage, offX, offY, stretch: motion.squash, now: now)
            for i in 0..<2 {
                let e = poses[i]
                if e.depth <= 0.02 { continue }
                let cfg = pose.eyes[i], fit = solid == nil ? bodyRadius(e.x, e.y) : 1
                let phi = cfg.tilt * Double.pi / 180, cp = cos(phi), sp = sin(phi)
                let ax = e.a * cp + e.c * sp, ay = e.b * cp + e.d * sp
                let cx2 = -e.a * sp + e.c * cp, cy2 = -e.b * sp + e.d * cp
                let k = blinkScale(min(lid, cfg.open))
                let shift = solid == nil ? Point(x: (offX + decalage.x) * r, y: (offY + decalage.y) * r) : Point()
                let matrix = CGAffineTransform(a: r2(ax), b: r2(ay * k), c: r2(cx2), d: r2(cy2 * k),
                                               tx: r2(e.x * fit + shift.x), ty: r2(e.y * fit + shift.y))
                    .concatenating(spawnScale)
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
        var frame = BotFrame(bodyPath: bodyPath, bodyAlpha: pose.bodyAlpha, eyes: eyes, dots: dots,
                             dotsBehind: pose.dotsBehind,
                             arcs: pose.arcs.filter { $0.opacity > 0.01 }.map { arcRender($0.seed, $0.t, r, $0.id, $0.opacity) },
                             notif: notif, notch: notch)
        frame.parts = thinkingHidden ? [] : (meshFrame?.1 ?? character?.layers(sil, rotation: body, scale: r))?.map { part in
            var part = part
            part.alpha *= 1 - spawn.amount
            return part
        } ?? []
        if mood == .working || mood == .searching {
            let bounds = frame.parts.filter { !$0.surface && $0.alpha > 0.01 }
                .reduce(bodyPath.boundingBoxOfPath) { $0.union(($1.outline ?? $1.path).boundingBoxOfPath) }
            let reach = frame.dots.map { hypot($0.x - offX * r, $0.y - offY * r) + $0.r }.max() ?? 0
            let room = Self.FRAME_SIZE * r / 200 - max(abs(bounds.midX), abs(bounds.midY))
            let fit = clamp(room / max(reach, 1e-9))
            // Centre on the visible silhouette, fitting the ring inside its canvas when needed.
            frame.dots = frame.dots.map { dot in
                var dot = dot
                dot.x = (dot.x - offX * r) * fit + bounds.midX
                dot.y = (dot.y - offY * r) * fit + bounds.midY
                dot.r *= fit
                return dot
            }
        }
        frame.bare = spawn.amount
        if let thinkingPhase {
            frame = thinking.apply(frame, phase: thinkingPhase, scale: r, displaySize: displaySize)
        }
        if interactive && !reducedMotion {
            let idle = (mood == nil || mood == .idle) && cur == .idle
            let motion = mascot.sample(now, idle: idle)
            petFace.retarget(idle && petted ? 1 : 0, now)
            // Clips morph to a happy face themselves; timelines fade in and out of arches.
            let weight = clip == nil ? clamp(petFace.sample(now).value) : 0
            if weight > 0.0001 { frame.eyes = frame.eyes.flatMap { eye in
                var normal = eye; normal.alpha *= 1 - weight
                var happy = eye; happy.alpha *= weight
                let arch = CGMutablePath()
                arch.move(to: CGPoint(x: -EYE_W * r / 2, y: 0))
                arch.addQuadCurve(to: CGPoint(x: EYE_W * r / 2, y: 0), control: CGPoint(x: 0, y: -EYE_H * r * 0.45))
                happy.d = arch.copy(strokingWithWidth: EYE_W * r * 0.4, lineCap: .round, lineJoin: .round, miterLimit: 1)
                return [normal, happy]
            } }
            frame.transform = motion.transform(size: r * 2, origin: CGPoint(x: 0, y: r * 0.2))
        }
        return frame
    }

    /// Clip eyes wrapped round the sphere, as Grok Bot draws them: each outline point is carried
    /// its arc length from the eye's middle along the surface, then seen straight on.
    private func sphereEyes(_ clip: ClipFrame, body: Rotation, center: Point, size: Double) -> [(points: [Point], alpha: Double)] {
        zip(clip.eyes, SPHERE_EYES).compactMap { eye, frame in
            let outline = eye.outline
            guard outline.count >= 3 else { return nil }
            let m = body.apply(frame.m), c = body.apply(frame.c), t = body.apply(frame.t)
            let ct = cos(eye.tilt), st = sin(eye.tilt)
            let surface = outline.map { p -> Vec3 in
                let y = p.y * eye.lidY
                let u = eye.off.x + ct * p.x + st * y, v = eye.off.y - st * p.x + ct * y
                let arc = hypot(u, v), k = arc > 1e-12 ? sin(arc) / arc : 1, h = cos(arc)
                return Vec3(x: h * m.x + k * (u * c.x + v * t.x), y: h * m.y + k * (u * c.y + v * t.y),
                            z: h * m.z + k * (u * c.z + v * t.z))
            }
            guard let near = nearSide(surface, facing: atan2(m.y, m.x)) else { return nil }
            return (near.map { Point(x: center.x + $0.x * size, y: center.y + $0.y * size) }, 1)
        }
    }

    /// Clip eyes laid on a solid's face through its eye poses, scaled from Grok Bot's units to the fitted eye.
    private func solidEyes(_ clip: ClipFrame, poses: [EyePose]) -> [(points: [Point], alpha: Double)] {
        let sx = scale * EYE_W / 0.26, sy = scale * EYE_H / 0.57
        let placed: [(points: [Point], alpha: Double)] = zip(clip.eyes, zip(poses, [-1.0, 1.0])).compactMap { eye, placement in
            let (e, side) = placement
            let outline = eye.outline
            guard e.depth > 0.02, outline.count >= 3 else { return nil }
            let ct = cos(eye.tilt), st = sin(eye.tilt)
            let points = outline.map { p -> Point in
                let y = p.y * eye.lidY
                let u = (eye.off.x + ct * p.x + st * y) * side * sx, v = -(eye.off.y - st * p.x + ct * y) * sy
                return Point(x: e.x + e.a * u + e.c * v, y: e.y + e.b * u + e.d * v)
            }
            return (points, clamp(e.depth / 0.12))
        }
        guard placed.count == 2 else { return placed }
        let dx = poses[1].x - poses[0].x, dy = poses[1].y - poses[0].y
        let gap = hypot(dx, dy)
        guard gap > 1e-6 else { return placed }
        var reach = 0.0
        for (index, eye) in placed.enumerated() {
            let direction = (index == 0 ? 1.0 : -1.0) / gap
            var inward = 0.0
            for point in eye.points {
                let x = (point.x - poses[index].x) * dx
                let y = (point.y - poses[index].y) * dy
                inward = max(inward, (x + y) * direction)
            }
            reach += inward
        }
        // Wide, inward-curving expressions keep a gap instead of merging on narrow faces.
        let fit = min(1, gap * 0.88 / max(reach, 1e-6))
        return placed.enumerated().map { index, eye in
            (eye.points.map { Point(x: poses[index].x + ($0.x - poses[index].x) * fit,
                                    y: poses[index].y + ($0.y - poses[index].y) * fit) }, eye.alpha)
        }
    }
}

/// Where Grok Bot's two eyes sit on its sphere facing front: middle, outward and down, in this y-down space.
private let SPHERE_EYES: [(m: Vec3, c: Vec3, t: Vec3)] = [
    (Vec3(x: -0.3153457572898143, y: -0.000413547232064401, z: 0.948976755425548),
     Vec3(x: -0.948976051487804, y: 0.0014310514033447285, z: -0.31534489974391217),
     Vec3(x: -0.0012276245071586862, y: -0.9999988905346685, z: -0.000843722407896131)),
    (Vec3(x: 0.3153457572898147, y: 0.000413547232064142, z: 0.948976755425548),
     Vec3(x: 0.9489760638921817, y: 0.0011460633475971695, z: -0.31534602692677954),
     Vec3(x: 0.0012179979536934692, y: -0.9999992577584695, z: 0.000031040210269326975))
]

/// The near half of a loop on the unit sphere, with hidden runs folded along the rim.
private func nearSide(_ points: [Vec3], facing: Double) -> [Point]? {
    let count = points.count
    guard let start = (0..<count).first(where: { points[$0].z <= 0 && points[($0 + 1) % count].z > 0 }) else {
        return count == 0 || points[0].z <= 0 ? nil : points.map { Point(x: $0.x, y: $0.y) }
    }
    func crossing(_ a: Vec3, _ b: Vec3) -> Point {
        let k = a.z / (a.z - b.z), x = a.x + (b.x - a.x) * k, y = a.y + (b.y - a.y) * k
        let length = max(1e-12, hypot(x, y))
        return Point(x: x / length, y: y / length)
    }
    func rim(_ from: Point, _ to: Point) -> [Point] {
        func wrap(_ angle: Double) -> Double { angle - TAU * (angle / TAU).rounded() }
        let begin = atan2(from.y, from.x)
        let sweep = wrap(atan2(to.y, to.x) - facing) - wrap(begin - facing)
        let steps = max(1, Int((abs(sweep) / (.pi / 48)).rounded(.up)))
        return (1...steps).map { step in
            let angle = begin + sweep * Double(step) / Double(steps)
            return step == steps ? to : Point(x: cos(angle), y: sin(angle))
        }
    }
    var out: [Point] = [], entry = Point(x: 1, y: 0), exit: Point?
    for step in 0..<count {
        let a = points[(start + step) % count], b = points[(start + step + 1) % count]
        if a.z > 0 && b.z > 0 { out.append(Point(x: b.x, y: b.y)) }
        else if a.z > 0 { let point = crossing(a, b); exit = point; out.append(point) }
        else if b.z > 0 {
            let point = crossing(a, b)
            if let exit { out += rim(exit, point) } else { entry = point }
            out.append(Point(x: b.x, y: b.y))
        }
    }
    if let exit { out += rim(exit, entry) }
    return out
}
