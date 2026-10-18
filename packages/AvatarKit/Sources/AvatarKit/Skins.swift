import Foundation

public enum ShapeID: String, CaseIterable, Codable, Identifiable, Sendable {
    case circle = "cercle", pebble = "galet", squircle, capsule, triangle
    case hexagon = "hexagone", cloud = "nuage", droplet = "goutte"
    public var id: String { rawValue }
    public var label: String {
        switch self {
        case .circle: "Circle"
        case .pebble: "Pebble"
        case .squircle: "Squircle"
        case .capsule: "Capsule"
        case .triangle: "Triangle"
        case .hexagon: "Hexagon"
        case .cloud: "Cloud"
        case .droplet: "Droplet"
        }
    }
    public var radii: [Double] { SHAPE_BY_ID[rawValue]!.radii }
}

public enum ColorID: String, CaseIterable, Codable, Identifiable, Sendable {
    case ink = "encre", brown = "brun", red = "rouge", orange, amber = "ambre", green = "vert"
    case turquoise, blue = "bleu", purple = "violet", pink = "rose", grey = "gris", cream = "creme"
    public var id: String { rawValue }
    public var label: String {
        switch self {
        case .ink: "Ink"
        case .brown: "Brown"
        case .red: "Red"
        case .orange: "Orange"
        case .amber: "Amber"
        case .green: "Green"
        case .turquoise: "Turquoise"
        case .blue: "Blue"
        case .purple: "Purple"
        case .pink: "Pink"
        case .grey: "Grey"
        case .cream: "Cream"
        }
    }
    public var hex: String { COLOR_BY_ID[rawValue]!.hex }
}

public struct BotShape: Identifiable, Hashable, Sendable {
    public var id: ShapeID
    public var radii: [Double]
    public init(id: ShapeID, radii: [Double]) { self.id = id; self.radii = radii }
    public var label: String { id.label }
}

public struct BotColor: Identifiable, Hashable, Sendable {
    public var id: ColorID
    public var hex: String
    public init(id: ColorID, hex: String) { self.id = id; self.hex = hex }
    public var label: String { id.label }
}

private func normalize(_ radii: [Double], _ maxRadius: Double = 1) -> [Double] {
    let peak = radii.max() ?? 0
    if peak <= 0 { return radii }
    let k = maxRadius / peak
    return radii.map { $0 * k }
}

public let SHAPES: [BotShape] = {
    let angles = (0..<PROFILE_SAMPLES).map { Double($0) / Double(PROFILE_SAMPLES) * TAU }
    let pebble = normalize(angles.map { 1 + 0.075 * cos(2 * $0 + 0.5) + 0.035 * cos(3 * $0 + 2.1) }, 1.02)
    let cloud = normalize(unionOfCirclesProfile([
        CircleSpec(x: -0.44, y: 0.2, r: 0.54), CircleSpec(x: 0.46, y: 0.2, r: 0.5),
        CircleSpec(x: 0.02, y: 0.3, r: 0.6), CircleSpec(x: -0.24, y: -0.3, r: 0.48),
        CircleSpec(x: 0.3, y: -0.24, r: 0.44)
    ]), 1.02)
    let droplet = normalize(profileFromPolygon(hullOfCircles(0, 0.28, 0.66, 0, -0.96, 0.05), 0, 0), 1.04)
    let capsule = profileFromPolygon(hullOfCircles(-0.42, 0, 0.62, 0.42, 0, 0.62), 0, 0)
    return [
        BotShape(id: .circle, radii: [Double](repeating: 1, count: PROFILE_SAMPLES)),
        BotShape(id: .pebble, radii: pebble),
        BotShape(id: .squircle, radii: normalize(superellipseProfile(4.2), 1.15)),
        BotShape(id: .capsule, radii: capsule),
        BotShape(id: .triangle, radii: regularPolygonProfile(3, 1.12, 0.34, -90)),
        BotShape(id: .hexagon, radii: regularPolygonProfile(6, 1.04, 0.26, 0)),
        BotShape(id: .cloud, radii: cloud), BotShape(id: .droplet, radii: droplet)
    ]
}()

public let SHAPE_BY_ID = Dictionary(uniqueKeysWithValues: SHAPES.map { ($0.id.rawValue, $0) })
public let DEFAULT_SHAPE = ShapeID.circle

public let COLORS: [BotColor] = [
    BotColor(id: .ink, hex: "#0a0a0c"), BotColor(id: .brown, hex: "#8b5e3c"),
    BotColor(id: .red, hex: "#e8483f"), BotColor(id: .orange, hex: "#f08a24"),
    BotColor(id: .amber, hex: "#f0b429"), BotColor(id: .green, hex: "#3ecf8e"),
    BotColor(id: .turquoise, hex: "#2fbfa0"), BotColor(id: .blue, hex: "#3b93f0"),
    BotColor(id: .purple, hex: "#8b5cf6"), BotColor(id: .pink, hex: "#e152b0"),
    BotColor(id: .grey, hex: "#a3a3a3"), BotColor(id: .cream, hex: "#f1efe9")
]
public let COLOR_BY_ID = Dictionary(uniqueKeysWithValues: COLORS.map { ($0.id.rawValue, $0) })
public let DEFAULT_COLOR = ColorID.ink

public func mixHex(_ from: String, _ to: String, _ t: Double) -> String {
    func parse(_ value: String) -> [Int] {
        let v = Int(value.dropFirst(), radix: 16) ?? 0
        return [(v >> 16) & 255, (v >> 8) & 255, v & 255]
    }
    let a = parse(from), b = parse(to)
    let components = zip(a, b).map { Int(floor(Double($0) + Double($1 - $0) * t + 0.5)) }
    return "#" + components.map { String(format: "%02x", $0) }.joined()
}
