import CoreGraphics
import Foundation

public enum StateID: String, CaseIterable, Codable, Sendable, Identifiable {
    case idle, thinking, wink, wide, alert, notify, exclaim, sleep, egg, hexagon
    case play, orbit, burst, comet, swirl
    public var id: String { rawValue }
    public var label: String {
        switch self {
        case .wide: "Wide eyes"
        case .notify: "Notification"
        case .exclaim: "Exclamation"
        default: rawValue.capitalized
        }
    }
}

public struct Notif: Sendable {
    public var x, y, r, notch: Double
    public init(x: Double, y: Double, r: Double, notch: Double) {
        self.x = x; self.y = y; self.r = r; self.notch = notch
    }
}

public struct Pose: Sendable {
    public var sil: Silhouette
    public var offX, offY: Double
    public var gaze: HeadGaze
    public var split: Double
    public var eyes: [EyeCfg]
    public var eyeAlpha, bodyAlpha: Double
    public var dots: [DotRender]
    public var arcs: [ArcSpec]
    public var notif: Notif?
    public var dotsBehind: Bool

    public init(sil: Silhouette = circle(1), offX: Double = 0, offY: Double = 0,
                gaze: HeadGaze = REST_GAZE, split: Double = EYE_SPLIT,
                eyes: [EyeCfg] = [EyeCfg(w: EYE_W, h: EYE_H), EyeCfg(w: EYE_W, h: EYE_H)],
                eyeAlpha: Double = 1, bodyAlpha: Double = 1, dots: [DotRender] = [],
                arcs: [ArcSpec] = [], notif: Notif? = nil, dotsBehind: Bool = false) {
        self.sil = sil; self.offX = offX; self.offY = offY; self.gaze = gaze; self.split = split
        self.eyes = eyes; self.eyeAlpha = eyeAlpha; self.bodyAlpha = bodyAlpha
        self.dots = dots; self.arcs = arcs; self.notif = notif; self.dotsBehind = dotsBehind
    }
}

public struct StateDef: Sendable {
    public let id: StateID
    public let duration: Double
    public let minDuration: Double?
    public let morph: Double
    public let blinkIn, baseBody, baseFace: Bool
    private let makePose: @Sendable (Double) -> Pose

    public init(id: StateID, duration: Double, minDuration: Double? = nil, morph: Double,
                blinkIn: Bool, baseBody: Bool, baseFace: Bool,
                pose: @escaping @Sendable (Double) -> Pose) {
        self.id = id; self.duration = duration; self.minDuration = minDuration; self.morph = morph
        self.blinkIn = blinkIn; self.baseBody = baseBody; self.baseFace = baseFace; makePose = pose
    }
    public func pose(_ local: Double) -> Pose { makePose(local) }
}

private func pair(_ w: Double, _ h: Double) -> [EyeCfg] { [EyeCfg(w: w, h: h), EyeCfg(w: w, h: h)] }

private func triangle(_ rot: Double) -> Silhouette {
    silhouette(.triangle, rot: rot, cx: -0.213 * sin(rot), cy: 0.213 * cos(rot))
}

public let STATES: [StateDef] = [
    StateDef(id: .idle, duration: 2.4, morph: 0.45, blinkIn: false, baseBody: true, baseFace: true) { _ in Pose() },
    StateDef(id: .thinking, duration: 2.6, morph: 0.4, blinkIn: true, baseBody: true, baseFace: true) { t in
        Pose(dots: thinkingMarks(t))
    },
    StateDef(id: .wink, duration: 1.6, morph: 0.3, blinkIn: true, baseBody: true, baseFace: false) { _ in
        Pose(gaze: HeadGaze(yaw: -5.37, pitch: 4.55, roll: 6.7), split: 16.25,
             eyes: [EyeCfg(w: 0.236, h: 0.464), EyeCfg(w: 0.447, h: 0.089)])
    },
    StateDef(id: .wide, duration: 1.8, morph: 0.55, blinkIn: true, baseBody: true, baseFace: false) { _ in
        Pose(gaze: HeadGaze(yaw: 6.92, pitch: -21.96, roll: 11.6), split: 18.43, eyes: pair(0.356, 0.875))
    },
    StateDef(id: .alert, duration: 2.4, minDuration: 2, morph: 0.45, blinkIn: false, baseBody: true, baseFace: true) { t in
        let out = easings.easeOutQuint(clamp(t / 0.4))
        let distance = 0.82 + (QUESTION_FAR - 0.82) * out + sin(max(0, t - 0.4) * 1.6 * TAU) * 0.01
        let target = badgePoint(distance)
        let tilt = BADGE_TILT + sin(t * 2.5 * TAU) * 2
        return Pose(dots: questionMarks(x: target.x, y: target.y,
                                        opacity: 0.35 + 0.65 * clamp(t / 0.25), tilt: tilt))
    },
    StateDef(id: .notify, duration: 2.2, morph: 0.5, blinkIn: true, baseBody: true, baseFace: false) { t in
        let p = clamp(t / 0.45)
        let pop = 1 + (NOTIF_POP - 1) * sin(p * .pi) * (1 - p * 0.35)
        let r = NOTIF_R * (p < 1 ? pop : 1)
        let a = NOTIF_ANGLE * Double.pi / 180
        return Pose(gaze: HeadGaze(yaw: -21.94, pitch: -5.82, roll: -12.2), split: 18.89,
                    eyes: pair(0.505, 0.498),
                    notif: Notif(x: cos(a) * NOTIF_DIST, y: sin(a) * NOTIF_DIST, r: r, notch: r + NOTIF_MARGIN))
    },
    StateDef(id: .exclaim, duration: 2, morph: 0.45, blinkIn: false, baseBody: true, baseFace: true) { t in
        let drop = easings.easeOutQuint(clamp(t / 0.5))
        let distance = 1.66 - (1.66 - BADGE_FAR) * drop + sin(t * 0.9 * TAU) * 0.01
        let target = badgePoint(distance)
        return Pose(dots: exclaimMarks(x: target.x, y: target.y, opacity: clamp(t / 0.2)))
    },
    StateDef(id: .sleep, duration: 2.4, morph: 0.5, blinkIn: false, baseBody: true, baseFace: false) { t in
        let sleepy = ExpressionID.sleepy.expression
        var dots: [DotRender] = []
        for i in 0..<2 {
            let p = (t / 1.7 + 0.5 * Double(i)).truncatingRemainder(dividingBy: 1)
            let rise = easings.easeOutCubic(clamp(p / 0.8))
            let size = (0.8 + 0.32 * Double(i)) * (0.9 + 0.15 * p)
            let target = badgePoint(1.04 + rise * 0.28)
            let alpha = clamp(p / 0.15) * (1 - clamp((p - 0.62) / 0.38))
            dots += zMarks(x: target.x, y: target.y, size: size, opacity: alpha)
        }
        return Pose(gaze: sleepy.gaze, split: sleepy.split, eyes: sleepy.eyes, dots: dots)
    },
    StateDef(id: .egg, duration: 1.8, morph: 0.4, blinkIn: true, baseBody: false, baseFace: false) { _ in
        Pose(sil: silhouette(.egg), gaze: HeadGaze(yaw: 19.97, pitch: 26.01, roll: -17.1),
             split: 11.07, eyes: pair(0.164, 0.385))
    },
    StateDef(id: .hexagon, duration: 1.6, morph: 0.4, blinkIn: true, baseBody: false, baseFace: false) { _ in
        Pose(sil: silhouette(.hexagon), gaze: HeadGaze(yaw: 23.11, pitch: 24.42, roll: -13.3),
             split: 13.37, eyes: pair(0.177, 0.411))
    },
    StateDef(id: .play, duration: 2, morph: 0.5, blinkIn: true, baseBody: false, baseFace: false) { t in
        let fade = clamp(t / 0.35) * clamp((2.2 - t) / 0.5)
        return Pose(sil: triangle(0), gaze: HeadGaze(yaw: 12, pitch: -8, roll: -6), split: 15,
                    eyes: pair(0.18, 0.34), arcs: SWOOSH.enumerated().map { i, seed in
                        var s = seed; s.cx = 0.45 - t * 0.42
                        return ArcSpec(id: "sw\(i)", seed: s, t: t, opacity: fade)
                    })
    },
    StateDef(id: .orbit, duration: 3.4, minDuration: 2.5, morph: 0.6, blinkIn: false, baseBody: false, baseFace: false) { t in
        let ramp = easings.easeInOutCubic(clamp(t / 0.35))
        let rot = -TAU * 1.25 * t * ramp
        let back = easings.easeInOutCubic(clamp((t - 1.6) / 0.9))
        let tri = triangle(rot), ball = circle(1, rot: rot)
        let sil = Silhouette(radii: tri.radii.enumerated().map { i, r in r + (ball.radii[i] - r) * back },
                             rot: rot, cx: tri.cx * (1 - back), cy: tri.cy * (1 - back))
        let fade = clamp(t / 0.8) * clamp((3.6 - t) / 0.9)
        return Pose(sil: sil, gaze: HeadGaze(yaw: REST_GAZE.yaw + sin(t * 6.5) * 65 * (1 - back),
                                           pitch: -4 + back * 32, roll: -13),
                    eyes: pair(0.18, 0.34 + back * 0.07), arcs: RINGS.enumerated().map { i, seed in
                        ArcSpec(id: "rg\(i)", seed: seed, t: t, opacity: fade * clamp((t - Double(i) * 0.13) / 0.3))
                    })
    },
    StateDef(id: .swirl, duration: 1.3, minDuration: 1.3, morph: 0.3, blinkIn: true, baseBody: true, baseFace: true) { t in
        Pose(arcs: RINGS.prefix(3).enumerated().map { i, seed in
            ArcSpec(id: "sw\(i)", seed: seed, t: t,
                    opacity: clamp((t - Double(i) * 0.06) / 0.14) * clamp((1.22 - t) / 0.34))
        })
    },
    StateDef(id: .burst, duration: 2.6, minDuration: 2.4, morph: 0.4, blinkIn: false, baseBody: false, baseFace: false) { t in
        let collapse = 1 - 0.834 * easings.easeOutQuint(clamp(t / 0.7))
        let regrow = easings.easeOutQuint(clamp((t - 1.7) / 0.7))
        return Pose(sil: circle(collapse + (1 - collapse) * regrow), eyeAlpha: clamp((t - 1.85) / 0.4),
                    dots: particles(t, 1), dotsBehind: true)
    },
    StateDef(id: .comet, duration: 2.4, minDuration: 2.4, morph: 0.45, blinkIn: false, baseBody: false, baseFace: false) { t in
        let collapse = 1 - (1 - COMET_DOT) * easings.easeOutQuint(clamp(t / 0.55))
        let regrow = easings.easeOutQuint(clamp((t - 1.85) / 0.6))
        let fade = clamp((t - 0.15) / 0.25) * clamp((1.95 - t) / 0.3)
        return Pose(sil: circle(collapse + (1 - collapse) * regrow,
                               cy: sin(clamp(t / 1.7) * .pi) * 0.035),
                    eyeAlpha: clamp((t - 2) / 0.35), arcs: COMET_RIBBONS.enumerated().map { i, seed in
                        ArcSpec(id: "cm\(i)", seed: seed, t: t, opacity: fade)
                    })
    }
]

public let STATE_BY_ID = Dictionary(uniqueKeysWithValues: STATES.map { ($0.id, $0) })
public let POSES: [StateID: Double] = [.idle: 1, .thinking: 1.1, .wink: 0.8, .wide: 0.8,
                                     .alert: 0.75, .notify: 0.9, .exclaim: 0.8, .sleep: 0.45,
                                     .egg: 0.8, .hexagon: 0.8, .play: 0.9, .orbit: 1.2,
                                     .swirl: 0.5, .burst: 0.45, .comet: 1.15]
public let SEQUENCE: [StateID] = [.idle, .thinking, .wink, .wide, .alert, .notify, .exclaim,
                                .sleep, .egg, .hexagon, .play, .orbit, .burst, .comet]
