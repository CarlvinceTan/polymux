import Foundation

public enum ExpressionID: String, CaseIterable, Codable, Identifiable, Sendable {
    case neutral = "neutre", attentive = "attentif", surprised = "surpris", excited = "excite"
    case happy = "heureux", laughing = "hilare", angry = "colere", sad = "triste", scared = "effraye"
    case suspicious = "mefiant", confused = "confus", curious = "curieux", proud = "fier"
    case shy = "timide", unimpressed = "blase", sleepy = "somnolent"
    public var id: String { rawValue }
    public var label: String {
        switch self {
        case .neutral: "Neutral"
        case .attentive: "Attentive"
        case .surprised: "Surprised"
        case .excited: "Excited"
        case .happy: "Happy"
        case .laughing: "Laughing"
        case .angry: "Angry"
        case .sad: "Sad"
        case .scared: "Scared"
        case .suspicious: "Suspicious"
        case .confused: "Confused"
        case .curious: "Curious"
        case .proud: "Proud"
        case .shy: "Shy"
        case .unimpressed: "Unimpressed"
        case .sleepy: "Sleepy"
        }
    }
    public var expression: BotExpression { EXPRESSION_BY_ID[rawValue]! }
}

public struct BotExpression: Identifiable, Hashable, Sendable {
    public var id: ExpressionID
    public var gaze: HeadGaze
    public var split: Double
    public var eyes: [EyeCfg]
    public init(id: ExpressionID, gaze: HeadGaze, split: Double, eyes: [EyeCfg]) {
        self.id = id; self.gaze = gaze; self.split = split; self.eyes = eyes
    }
    public var label: String { id.label }
}

private func eye(_ w: Double, _ h: Double, _ tilt: Double = 0, _ open: Double = 1) -> EyeCfg {
    EyeCfg(w: w, h: h, open: open, tilt: tilt)
}
private func pair(_ w: Double, _ h: Double, _ tilt: Double = 0, _ open: Double = 1) -> [EyeCfg] {
    [eye(w, h, tilt, open), eye(w, h, -tilt, open)]
}

public let EXPRESSIONS: [BotExpression] = [
    BotExpression(id: .neutral, gaze: REST_GAZE, split: EYE_SPLIT, eyes: [eye(EYE_W, EYE_H), eye(EYE_W, EYE_H)]),
    BotExpression(id: .attentive, gaze: HeadGaze(yaw: 4, pitch: 5, roll: -4), split: 16, eyes: pair(0.21, 0.44)),
    BotExpression(id: .surprised, gaze: HeadGaze(yaw: 3, pitch: -3, roll: 0), split: 19, eyes: pair(0.45, 0.47)),
    BotExpression(id: .excited, gaze: HeadGaze(yaw: 6, pitch: -14, roll: 0), split: 19.5, eyes: pair(0.4, 0.56, -10)),
    BotExpression(id: .happy, gaze: HeadGaze(yaw: 5, pitch: 9, roll: 0), split: 17, eyes: pair(0.27, 0.17, 14)),
    BotExpression(id: .laughing, gaze: HeadGaze(yaw: 4, pitch: 14, roll: 0), split: 18, eyes: pair(0.34, 0.13, 20)),
    BotExpression(id: .angry, gaze: HeadGaze(yaw: 3, pitch: 7, roll: 0), split: 17, eyes: pair(0.34, 0.15, 30)),
    BotExpression(id: .sad, gaze: HeadGaze(yaw: 3, pitch: -13, roll: 0), split: 16, eyes: pair(0.22, 0.4, -28)),
    BotExpression(id: .scared, gaze: HeadGaze(yaw: 2, pitch: -20, roll: 0), split: 20.5, eyes: pair(0.4, 0.6)),
    BotExpression(id: .suspicious, gaze: HeadGaze(yaw: 12, pitch: 6, roll: -6), split: 16, eyes: [eye(0.21, 0.4), eye(0.22, 0.15)]),
    BotExpression(id: .confused, gaze: HeadGaze(yaw: -14, pitch: 3, roll: 8), split: 16.5, eyes: [eye(0.2, 0.44, -18), eye(0.28, 0.17, 14)]),
    BotExpression(id: .curious, gaze: HeadGaze(yaw: 16, pitch: -9, roll: -15), split: 16.5, eyes: [eye(0.24, 0.46, -8), eye(0.2, 0.38, -8)]),
    BotExpression(id: .proud, gaze: HeadGaze(yaw: 5, pitch: 17, roll: 0), split: 17, eyes: pair(0.3, 0.15, 18)),
    BotExpression(id: .shy, gaze: HeadGaze(yaw: -19, pitch: -14, roll: -7), split: 14, eyes: pair(0.17, 0.3)),
    BotExpression(id: .unimpressed, gaze: HeadGaze(yaw: -22, pitch: 2, roll: 0), split: 16, eyes: pair(0.3, 0.12)),
    BotExpression(id: .sleepy, gaze: HeadGaze(yaw: 6, pitch: -9, roll: -3), split: 16, eyes: pair(0.2, 0.42, 0, 0.42))
]
public let EXPRESSION_BY_ID = Dictionary(uniqueKeysWithValues: EXPRESSIONS.map { ($0.id.rawValue, $0) })
public let DEFAULT_EXPRESSION = ExpressionID.neutral

public func blendExpression(_ a: BotExpression, _ b: BotExpression, _ t: Double) -> BotExpression {
    func eyeCfg(_ a: EyeCfg, _ b: EyeCfg) -> EyeCfg {
        EyeCfg(w: lerp(a.w, b.w, t), h: lerp(a.h, b.h, t), open: lerp(a.open, b.open, t), tilt: lerp(a.tilt, b.tilt, t))
    }
    return BotExpression(id: b.id,
                         gaze: HeadGaze(yaw: lerp(a.gaze.yaw, b.gaze.yaw, t), pitch: lerp(a.gaze.pitch, b.gaze.pitch, t), roll: lerp(a.gaze.roll, b.gaze.roll, t)),
                         split: lerp(a.split, b.split, t), eyes: [eyeCfg(a.eyes[0], b.eyes[0]), eyeCfg(a.eyes[1], b.eyes[1])])
}
