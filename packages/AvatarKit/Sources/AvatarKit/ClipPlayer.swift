import Foundation

// Grok Bot 0.75.8's motion recipes and the player (oa, sa, ca, fa, wa) that plays, bridges and loops them.
// Rotations are its x, y, z turns in degrees with the eyes facing front at zero; positions are y up.

let STANDARD_EASE = [0.42, 0.0, 0.58, 1.0]
private let BRIDGE_EASE = [0.4, 0.0, 0.2, 1.0]
let BRIDGE_TIME = 0.35
let REST_ZOOM = 0.6
/// Grok Bot's resting turn, which carries the front-facing eyes up and to the right.
let REST_TURN = [-15.896271043294888, 33.29315635042974, 25.38480127460161]

struct Key: Sendable {
    var t: Double
    var v: [Double]
    var ease = STANDARD_EASE
}

struct Recipe: Sendable {
    var name: String
    var duration: Double
    var anchor = 0.0
    var restTime = 0.0
    var bias: Double?
    var follows: Bool?
    var position: [Key] = []
    var rotation: [Key] = []
    var squash: [Key] = []
    var zoom: [Key] = []
    var lids: [Key] = []
    /// Slides the eyes across the face in body radii, x right and y up, instead of turning the body.
    var shift: [Key] = []
    var faces: [FaceEvent] = []

    init(name: String, duration: Double, anchor: Double = 0, restTime: Double = 0, bias: Double? = nil,
         follows: Bool? = nil, position: [Key] = [], rotation: [Key] = [], squash: [Key] = [],
         zoom: [Key] = [], lids: [Key] = [], shift: [Key] = [], faces: [FaceEvent] = [], blinks: [Double] = []) {
        self.name = name; self.duration = duration; self.anchor = anchor; self.restTime = restTime
        self.bias = bias; self.follows = follows; self.position = position; self.rotation = rotation
        self.squash = squash; self.zoom = zoom; self.shift = shift; self.faces = faces
        // Each blink is a lid pulse that peaks 45% of the way through.
        self.lids = blinks.isEmpty ? lids : (lids + blinks.flatMap { t in
            [Key(t: t, v: [0]), Key(t: t + BLINK_TIME * 0.45, v: [1]), Key(t: t + BLINK_TIME, v: [0])]
        }).sorted { $0.t < $1.t }
    }

    static let rest = Recipe(name: "rest", duration: 10, rotation: [Key(t: 0, v: REST_TURN)])
}

struct ClipPose {
    var rot: [Double]
    var pos: [Double]
    var scl: [Double]
    var zoom: Double
    var lids: Double
    var face: FaceID
    var shift = [0.0, 0]

    var values: [Double] { rot + pos + scl + [zoom] + shift }
}

/// One frame of motion: turn, offset, squash and zoom of the body, and the eyes to draw on it.
struct ClipFrame: Sendable {
    var rot = REST_TURN
    var pos = [0.0, 0]
    var squash = [1.0, 1]
    var zoom = REST_ZOOM
    var anchor = 0.0
    var eyes = FaceID.neutral.eyes
    var shift = [0.0, 0]
}

func sample(_ keys: [Key], _ t: Double) -> [Double]? {
    guard let first = keys.first else { return nil }
    if t <= first.t { return first.v }
    let last = keys[keys.count - 1]
    if t >= last.t { return last.v }
    var index = 0
    while index < keys.count - 2 && keys[index + 1].t <= t { index += 1 }
    let a = keys[index], b = keys[index + 1]
    let amount = bezierEase((t - a.t) / max(b.t - a.t, 1e-9), a.ease)
    return zip(a.v, b.v).map { $0 + ($1 - $0) * amount }
}

func faceAt(_ events: [FaceEvent], _ t: Double) -> FaceID {
    var face = FaceID.neutral
    for event in events {
        if event.t > t + 1e-9 { break }
        face = event.face
    }
    return face
}

/// Where a clip rests: its pose at the start of its work area, with its first face.
func restPose(_ clip: Recipe) -> ClipPose {
    let t = clip.restTime
    var face = clip.faces.first?.face ?? .neutral
    for event in clip.faces where event.t <= t + 1e-9 { face = event.face }
    return ClipPose(rot: sample(clip.rotation, t) ?? [0, 0, 0], pos: sample(clip.position, t) ?? [0, 0],
                    scl: sample(clip.squash, t) ?? [1, 1], zoom: sample(clip.zoom, t)?[0] ?? REST_ZOOM,
                    lids: clamp(sample(clip.lids, t)?[0] ?? 0), face: face, shift: sample(clip.shift, t) ?? [0, 0])
}

/// Grok Bot's turn and offset for facing a gaze point: spheres turn less, and hearts and wedges less again.
func gazeTurn(_ body: ShapeID?, x: Double, y: Double) -> (rot: [Double], pos: [Double]) {
    let flat = body != .circle
    let factor = body == .heart ? 0.6 : body == .wedge ? 0.55 : 1
    return (rot: [-(flat ? 19 : 14) * factor * y, (flat ? 26 : 18) * factor * x],
            pos: [(flat ? 0.07 : 0.03) * x, (flat ? 0.055 : 0.024) * y])
}

private func jsRound(_ value: Double) -> Double { (value + 0.5).rounded(.down) }

private func degrees(_ a: Double, _ b: Double) -> Double {
    abs(((a - b).truncatingRemainder(dividingBy: 360) + 540).truncatingRemainder(dividingBy: 360) - 180)
}

private func near(_ a: ClipPose, _ b: ClipPose) -> Bool {
    (0..<3).allSatisfy { degrees(a.rot[$0], b.rot[$0]) < 1e-7 }
        && zip(a.pos + a.scl + [a.zoom, a.lids] + a.shift,
               b.pos + b.scl + [b.zoom, b.lids] + b.shift).allSatisfy { abs($0 - $1) < 1e-7 }
}

/// A short clip easing from one pose into another, turning the short way round.
private func bridge(from a: ClipPose, to b: ClipPose, anchor: Double) -> Recipe {
    func track(_ from: [Double], _ to: [Double]) -> [Key] {
        [Key(t: 0, v: from, ease: BRIDGE_EASE), Key(t: BRIDGE_TIME, v: to)]
    }
    let turn = (0..<3).map { b.rot[$0] + 360 * jsRound((a.rot[$0] - b.rot[$0]) / 360) }
    return Recipe(name: "bridge", duration: BRIDGE_TIME, anchor: anchor, position: track(a.pos, b.pos),
                  rotation: track(a.rot, turn), squash: track(a.scl, b.scl), zoom: track([a.zoom], [b.zoom]),
                  lids: a.lids == 0 && b.lids == 0 ? [] : track([a.lids], [b.lids]),
                  shift: a.shift == b.shift ? [] : track(a.shift, b.shift))
}

/// Grok Bot's clip player: plays a recipe, loops it, and bridges into the next one.
struct ClipPlayer {
    private(set) var face = FacePlayer()
    private var rot = REST_TURN, pos = [0.0, 0], scl = [1.0, 1], zoom = REST_ZOOM, anchor = 0.0, shift = [0.0, 0]
    private var bias = -1.0, follows = true
    private var clip = Recipe.rest
    private(set) var time = 0.0
    private(set) var playing = false
    private var loops = true
    private var current: (clip: Recipe, name: String)?
    private var pending: Load?
    private(set) var loopCount = 0
    private var held: FaceID?
    private var deferred: Action?
    private var lidsOff = false
    private var momentum: [MotionBlend] = []
    private var gaze = Gaze()

    /// Grok Bot's held gaze, x right and y up in -1...1, eased in `cx`/`cy` and faded in and out by `hold`.
    private struct Gaze {
        var held = false, x = 0.0, y = 0.0, cx = 0.0, cy = 0.0, hold = 0.0
        var body: ShapeID?
    }

    private struct Load {
        var clip: Recipe
        var name: String
        var playing: Bool
        var phase: Double
    }

    private enum Action {
        case play(FaceEvent), morph(FaceID), hold
    }

    var name: String? { current?.name }

    var frame: ClipFrame {
        let eyes = inspected(face.eyes, weight: face.inspectWeight, bias: bias, follows: follows, yaw: rot[1])
        let shut = blinking
        return ClipFrame(rot: rot, pos: pos, squash: scl, zoom: zoom, anchor: anchor,
                         eyes: shut <= 1e-4 ? eyes : eyes.map { $0.closed(shut) }, shift: shift)
    }

    mutating func load(_ recipe: Recipe, playing: Bool = true, bridged: Bool = true, phase: Double = 0) {
        let next = Load(clip: recipe, name: recipe.name, playing: playing,
                        phase: clamp(phase).truncatingRemainder(dividingBy: 1))
        if bridged, let current, pending != nil || current.name != recipe.name { bridge(next) } else { start(next) }
    }

    mutating func rest() {
        let next = Load(clip: .rest, name: Recipe.rest.name, playing: false, phase: 0)
        if pending == nil && (current == nil || current?.name == Recipe.rest.name) {
            if current == nil { start(next) }
            return
        }
        bridge(next)
    }

    /// Holds `face` over whatever the clips ask for; nil hands the face back to them.
    mutating func hold(_ face: FaceID?) {
        guard face != held else { return }
        held = face
        queue(.hold)
    }

    mutating func blink() { face.blink() }

    /// Holds the gaze on a point, x right and y up in -1...1; nil lets it go.
    mutating func setGaze(_ point: [Double]?, body: ShapeID?) {
        gaze.body = body
        guard let point else { gaze.held = false; return }
        let x = min(1, max(-1, point[0])), y = min(1, max(-1, point[1]))
        if gaze.hold == 0 { gaze = Gaze(held: true, x: x, y: y, cx: x, cy: y, hold: 0, body: body) }
        else { gaze.held = true; gaze.x = x; gaze.y = y }
    }

    /// No bridge, face morph or gaze is still easing.
    var settled: Bool {
        pending == nil && !face.isMorphing && gaze.hold == (gaze.held ? 1 : 0)
            && (!gaze.held || (gaze.cx == gaze.x && gaze.cy == gaze.y))
    }

    mutating func advance(_ dt: Double) {
        let step = min(dt, 0.1)
        look(step)
        if playing { tick(step) }
        face.step(step)
        if let action = deferred, blinking == 0 {
            deferred = nil
            perform(action)
        }
        updateLids()
    }

    private var lidsTrack: Double { sample(clip.lids, time).map { clamp($0[0]) } ?? 0 }
    private var lids: Double { lidsOff ? 0 : lidsTrack }
    private var blinking: Double { face.blinkAmount ?? lids }
    private var pose: ClipPose {
        ClipPose(rot: rot, pos: pos, scl: scl, zoom: zoom, lids: lids, face: face.name, shift: shift)
    }

    private func values(_ time: Double) -> [Double] {
        let rotation = sample(clip.rotation, time) ?? [0, 0, 0]
        let position = sample(clip.position, time) ?? [0, 0]
        let squash = sample(clip.squash, time) ?? [1, 1]
        let zoom = sample(clip.zoom, time)?[0] ?? zoom
        let shift = sample(clip.shift, time) ?? [0, 0]
        let values = rotation + position + squash + [zoom] + shift
        guard !momentum.isEmpty else { return values }
        return zip(values, momentum).map { $0 + $1.sample(time, duration: BRIDGE_TIME) }
    }

    private mutating func apply() {
        let values = values(time)
        rot = Array(values[0..<3]); pos = Array(values[3..<5]); scl = Array(values[5..<7])
        zoom = min(2.5, max(0.25, values[7])); shift = Array(values[8..<10])
    }

    /// Face changes wait for a blink in progress to finish.
    private mutating func queue(_ action: Action) {
        if blinking > 0 { deferred = action; return }
        perform(action)
        updateLids()
    }

    private mutating func perform(_ action: Action) {
        switch action {
        case .play(let event): face.play(event.face, throughBlink: event.throughBlink)
        case .morph(let target): face.morph(target)
        case .hold:
            let target = held ?? pending.map { faceAt($0.clip.faces, 0) } ?? faceAt(clip.faces, time)
            if target != face.name { face.morph(target) }
        }
    }

    private mutating func fire(_ from: Double, _ to: Double) {
        guard held == nil else { return }
        for event in clip.faces where event.t > from && event.t <= to { queue(.play(event)) }
    }

    private mutating func seek(_ t: Double) {
        time = t
        deferred = nil
        apply()
        face.seek(clip.faces, t)
        // A held face is already in place wherever the clip is sought to.
        if let held { face.seek([FaceEvent(t: 0, face: held)], MORPH_TIME) }
    }

    /// Lid tracks only close plain open eyes, and wait for a morph to settle.
    private mutating func updateLids() {
        if !face.name.liddable || face.isMorphing { lidsOff = true }
        else if lidsOff && lidsTrack == 0 { lidsOff = false }
    }

    private mutating func use(_ recipe: Recipe) {
        playing = false
        clip = recipe
        momentum = []
        time = 0
        anchor = recipe.anchor
        bias = recipe.bias ?? bias
        follows = recipe.follows ?? follows
    }

    private mutating func start(_ next: Load) {
        pending = nil
        use(next.clip)
        current = (next.clip, next.name)
        loops = true
        if !next.playing {
            seek(0)
            if next.name == Recipe.rest.name { gaze.hold = 0 }
            return
        }
        playing = true
        if next.phase > 0 { seek(next.phase * next.clip.duration) }
        face.release()
        if next.phase <= 0 { fire(-1e-9, 0); apply() }
    }

    private func entry(_ next: Load, offset: Double = 0) -> ClipPose {
        var clip = next.clip
        clip.restTime = next.playing ? next.phase * clip.duration + offset : 0
        var pose = restPose(clip)
        pose.face = faceAt(clip.faces, clip.restTime)
        return pose
    }

    private mutating func bridge(_ next: Load) {
        let from = pose
        var to = entry(next)
        let step = time > 0.0001 ? -0.0001 : 0.0001
        let current = values(time), adjacent = values(time + step)
        let velocity = zip(current, adjacent).map { playing ? ($1 - $0) / step : 0 }
        // Changing the squash pivot must not move the body when the next clip starts.
        to.pos[1] += (next.clip.anchor - anchor) * (1 - to.scl[1])
        var ahead = entry(next, offset: 0.0001)
        ahead.pos[1] += (next.clip.anchor - anchor) * (1 - ahead.scl[1])
        let ending = zip(to.values, ahead.values).map { ($1 - $0) / 0.0001 }
        pending = next
        use(AvatarKit.bridge(from: from, to: to, anchor: anchor))
        momentum = zip(velocity, ending).map { MotionBlend(velocity: $0, ending: $1) }
        loops = false
        playing = true
        if held == nil && from.face != to.face { queue(.morph(to.face)) }
    }

    /// A clip that ends away from where it starts eases back before looping.
    private mutating func rebridge() -> Bool {
        guard pending == nil, let current else { return false }
        let next = Load(clip: current.clip, name: current.name, playing: true, phase: 0)
        guard !near(pose, entry(next)) else { return false }
        bridge(next)
        return true
    }

    /// Eases the gaze towards its point and, while not playing, turns the pose to face it.
    private mutating func look(_ dt: Double) {
        guard gaze.held || gaze.hold != 0 else { return }
        let rate = 1 - pow(0.86, dt * 60)
        func ease(_ from: Double, _ to: Double) -> Double {
            let next = from + (to - from) * rate
            return abs(to - next) < 0.001 ? to : next
        }
        gaze.cx = ease(gaze.cx, gaze.x); gaze.cy = ease(gaze.cy, gaze.y)
        gaze.hold = ease(gaze.hold, gaze.held ? 1 : 0)
        if playing { return }
        apply()
        let turn = gazeTurn(gaze.body, x: gaze.cx, y: gaze.cy), hold = gaze.hold
        rot = [rot[0] + (turn.rot[0] - rot[0]) * hold, rot[1] + (turn.rot[1] - rot[1]) * hold, rot[2] * (1 - hold)]
        pos = [pos[0] + (turn.pos[0] - pos[0]) * hold, pos[1] + (turn.pos[1] - pos[1]) * hold]
    }

    private mutating func tick(_ dt: Double) {
        let from = time
        var to = from + dt
        if to >= clip.duration {
            fire(from, clip.duration)
            time = clip.duration
            apply()
            if loops {
                loopCount += 1
                let remainder = to - clip.duration
                if rebridge() { tick(remainder); return }
                to -= clip.duration
                if to >= clip.duration { to = 0 }
                fire(-1e-9, to)
                time = to
            } else {
                let remainder = to - clip.duration
                playing = false
                if let pending {
                    start(pending)
                    if playing && remainder > 0 { tick(remainder) }
                }
                return
            }
        } else {
            fire(from, to)
            time = to
        }
        apply()
    }
}
