import CoreGraphics
import Foundation

public enum ShapeCategory: String, CaseIterable, Identifiable, Sendable {
    case shapes = "Shapes", food = "Food", animals = "Animals", nature = "Nature", objects = "Objects"
    public var id: String { rawValue }
    public var shapes: [ShapeID] { ShapeID.creationShapes.filter { $0.category == self } }
}

extension ShapeID {
    public var category: ShapeCategory {
        switch self {
        case .lemon, .orangeFruit, .watermelon, .avocado, .cherry, .cookie, .cheese, .egg, .candy, .donut: .food
        case .tulip, .sunflower, .sprout, .fern, .shell, .palm, .snowman, .lightning, .planet: .nature
        case .balloon, .mug, .lantern, .gift, .pencil, .hammer, .umbrella, .wateringCan, .gamepad, .guitar, .telescope: .objects
        case .hamburger, .carrot, .pizza, .sushi, .iceCream: .food
        case .sun, .leaf, .tree, .cactus, .mushroom, .mountain, .volcano, .rainbow, .snowflake: .nature
        case .rocket, .robot, .camera, .clock, .key, .bell, .book, .teapot: .objects
        case .whale, .bull: .animals
        case .popcorn, .peanut: .food
        case .moon, .wave, .blossom, .acorn, .pebbles: .nature
        case .saucer, .beacon, .magnet, .bulb, .trophy, .gem: .objects
        case .pumpkin, .apple, .pear, .strawberry, .bun, .toast, .cupcake: .food
        case .cat, .dog, .panda, .rabbit: .animals
        case .fox, .crab, .elephant, .octopus, .pig, .deer, .koala, .giraffe, .cow, .turtle,
             .tiger, .frog, .mouse, .walrus, .beaver, .guineaPig, .chinchilla, .ferret, .monkey,
             .chick, .duck, .penguin, .bat, .axolotl, .goose: .animals
        default: .shapes
        }
    }

    public var character: CharacterModel? { CHARACTERS[self] ?? ANIMALS[self] ?? MASCOT_MODELS[self] }
}

public struct CharacterModel: Sendable {
    public let color: String
    public let radii: [Double]
    public let solid: Solid
    let parts: [CharacterPart]
    public var eyeColor = "#231A20"
    var frosting = false
    var meshID: ShapeID? = nil
    var mesh: MascotMesh? { meshID.flatMap(mascot) }
    /// Rebuilding its geometry takes a large share of a frame.
    var costly: Bool { mesh != nil || frosting || solid == .pumpkin }

    /// Scales a character so its head reads as large as a circle avatar; ears and props may
    /// reach past it, as a circle's never do. Characters only grow, and never past the frame.
    var fit: Double {
        // Keep the topping's fit independent of the liner's painted folds.
        if frosting { return 1.03 }
        let head = radii.reduce(0, +) / Double(max(1, radii.count))
        let reach = mesh?.extent ?? parts.reduce(radii.max() ?? 1) { reach, part in
            max(reach, hypot(part.center.x, part.center.y) + (part.radii.max() ?? 0))
        }
        return min(1.25, 1.15 / reach, max(1, 0.92 / head))
    }
}

struct CharacterPart: Sendable {
    var radii: [Double]
    var solid: Solid
    var center: Vec3
    var rotation: Rotation
    var color: String
    var surface = false
    var behind = false

    init(_ color: String, x: Double = 0, y: Double, z: Double = 0,
         width: Double, height: Double, depth: Double = 0.12,
          roll: Double = 0, profile: [Double]? = nil, surface: Bool = false, behind: Bool = false) {
        self.color = color
        radii = profile.map { outline in
            (0..<PROFILE_SAMPLES).map { index in
                let angle = Double(index) * TAU / Double(PROFILE_SAMPLES)
                let x = cos(angle) / width, y = sin(angle) / height
                return radiusAtAngle(outline, atan2(y, x)) / hypot(x, y)
            }
        } ?? superellipseProfile(2, width, height)
        solid = .inflated(depth: depth)
        center = Vec3(x: x, y: y, z: z)
        rotation = Rotation(yaw: 0, pitch: 0, roll: roll)
        self.surface = surface
        self.behind = behind
    }
}

public struct CharacterLayer: @unchecked Sendable {
    public var path: CGPath
    public var color: String
    public var front: Bool
    public var surface: Bool
    public var alpha: Double = 1
    /// Stack-gap silhouette override; an empty path omits an interior layer.
    public var outline: CGPath? = nil

    public init(path: CGPath, color: String, front: Bool, surface: Bool, alpha: Double = 1, outline: CGPath? = nil) {
        self.path = path; self.color = color; self.front = front; self.surface = surface
        self.alpha = alpha; self.outline = outline
    }
}

extension CharacterModel {
    func layers(_ sil: Silhouette, rotation: Rotation, scale: Double) -> [CharacterLayer] {
        if solid == .pumpkin {
            return Pumpkin.ribs(sil, rotation: rotation, scale: scale) + Pumpkin.stem(sil, rotation: rotation, scale: scale)
        }
        let layers = parts.compactMap { part -> (Double, CharacterLayer)? in
            let center = rotation.apply(Vec3(x: part.center.x * sil.sx,
                                             y: part.center.y * sil.sy, z: part.center.z))
            let shape = Silhouette(radii: part.radii, cx: sil.cx + center.x, cy: sil.cy + center.y,
                                   sx: sil.sx, sy: sil.sy)
            let path: CGPath
            if part.surface {
                let face = surface(solid, part.center.x, part.center.y, radii)
                if rotation.apply(face.normal).z <= 0 { return nil }
                let points = toPoints(Silhouette(radii: part.radii), 1).compactMap { point -> Point? in
                    let local = part.rotation.apply(Vec3(x: point.x, y: point.y, z: 0))
                    let x = part.center.x + local.x, y = part.center.y + local.y
                    let face = surface(solid, x, y, radii)
                    guard rotation.apply(face.normal).z > 0 else { return nil }
                    let projected = rotation.apply(Vec3(x: x * sil.sx, y: y * sil.sy, z: face.z + 0.004))
                    return Point(x: (sil.cx + projected.x) * scale, y: (sil.cy + projected.y) * scale)
                }
                guard points.count >= 3 else { return nil }
                path = closedPath(points)
            } else {
                path = closedPath(solidOutline(shape, part.solid, rotation * part.rotation, scale: scale, smooth: true))
            }
            // Surface markings keep their paint order; raised parts still sort by depth.
            return (part.surface ? 0 : center.z, CharacterLayer(path: path, color: part.color,
                                             front: !part.behind && (part.surface || center.z > 0), surface: part.surface))
        }.sorted { $0.0 < $1.0 }.map(\.1)
        return layers + (frosting ? Cupcake.grooves(sil, rotation: rotation, scale: scale)
                         + Cupcake.layers(sil, rotation: rotation, scale: scale) : [])
    }
}

// Animal proportions and bun crown adapted from oneworks-ai/avatar; see Resources/Characters-LICENSE.txt.
private let CHARACTERS: [ShapeID: CharacterModel] = {
    let leaf = "#387A36", stem = "#77502F"
    let cone = regularPolygonProfile(3, 1, 0.24, -90)
    func ellipse(_ x: Double, _ y: Double) -> [Double] { superellipseProfile(2, x, y) }
    func polygon(_ points: [(Double, Double)]) -> [Double] {
        profileFromPolygon(roundedPolygon(points.map { Point(x: $0.0, y: $0.1) }, 0.16), 0, 0)
    }
    func stalk(_ y: Double) -> CharacterPart {
        CharacterPart(stem, x: 0.03, y: y, width: 0.07, height: 0.22, depth: 0.07, roll: 12)
    }
    func foliage(_ y: Double) -> CharacterPart {
        CharacterPart(leaf, x: 0.25, y: y, z: 0.04, width: 0.29, height: 0.13,
                      depth: 0.04, roll: -25, profile: superellipseProfile(1.45))
    }
    let apple = profileFromPolygon((0..<256).map { i in
        let a = Double(i) * TAU / 256, y = sin(a)
        return Point(x: 0.88 * cos(a) * (1 - 0.08 * y),
                     y: 0.84 * y + (y < 0 ? 0.21 : -0.07) * pow(abs(y), 12))
    }, 0, 0)
    let pear = profileFromPolygon((0..<256).map { i in
        let a = Double(i) * TAU / 256, y = sin(a)
        return Point(x: cos(a) * (0.59 + 0.22 * y + 0.12 * cos(2 * a)), y: y * 0.95)
    }, 0, 0)
    let berry = profileFromPolygon((0..<256).map { i in
        let a = Double(i) * TAU / 256, y = sin(a)
        return Point(x: cos(a) * (0.76 - 0.23 * y), y: y * 0.88)
    }, 0, 0)
    let toast = polygon([(-0.74, 0.82), (-0.74, -0.22), (-0.92, -0.45), (-0.84, -0.8),
                         (-0.4, -0.96), (0.4, -0.96), (0.84, -0.8), (0.92, -0.45),
                         (0.74, -0.22), (0.74, 0.82)])
    let cake = profileFromPolygon(roundedPolygon([
        Point(x: -0.73, y: -0.23), Point(x: 0.73, y: -0.23),
        Point(x: 0.51, y: 0.74), Point(x: -0.51, y: 0.74)
    ], 0.06), 0, 0)
    var seeds: [CharacterPart] = []
    for (x, y) in [(-0.53, -0.4), (0.0, -0.57), (0.53, -0.4), (-0.56, 0.05),
                   (0.56, 0.05), (-0.35, 0.45), (0.35, 0.45), (0, 0.7)] {
        let z = surface(Solid.inflated(depth: 0.6), x, y, berry).z
        seeds.append(CharacterPart("#FFE1A3", x: x, y: y, z: z + 0.015,
                                   width: 0.028, height: 0.053, depth: 0.012, surface: true))
    }
    var catParts = [-1.0, 1.0].flatMap { (side: Double) -> [CharacterPart] in [
        CharacterPart("#8796AC", x: side * 0.51, y: -0.6, z: -0.03,
                      width: 0.3, height: 0.48, depth: 0.22, roll: side * 12, profile: cone),
        CharacterPart("#DAB0B5", x: side * 0.51, y: -0.64, z: 0.15,
                      width: 0.14, height: 0.27, depth: 0.035, roll: side * 12, profile: cone),
        CharacterPart("#EDF0E9", x: side * 0.17, y: 0.31, width: 0.23, height: 0.19, surface: true),
        CharacterPart("#445066", x: side * 0.58, y: 0.23, width: 0.2, height: 0.025,
                      roll: side * -12, surface: true),
        CharacterPart("#445066", x: side * 0.56, y: 0.4, width: 0.18, height: 0.025,
                      roll: side * 12, surface: true)
    ] }
    catParts += [
        CharacterPart("#445066", y: -0.5, width: 0.07, height: 0.15, surface: true),
        CharacterPart("#9B5E6D", y: 0.26, width: 0.09, height: 0.07, roll: 180, profile: cone, surface: true)
    ]
    return [
        .pumpkin: CharacterModel(color: "#FF6700", radii: Pumpkin.outline(.identity).enumerated().compactMap {
            $0.offset % 4 == 0 ? hypot($0.element.x, $0.element.y) : nil
        }, solid: .pumpkin, parts: []),
        .apple: CharacterModel(color: "#CE3545", radii: apple, solid: .inflated(depth: 0.72),
                               parts: [stalk(-0.83), foliage(-0.97)]),
        .pear: CharacterModel(color: "#709531", radii: pear, solid: .revolved,
                              parts: [stalk(-1.03), foliage(-1.12)]),
        .strawberry: CharacterModel(color: "#D93250", radii: berry, solid: .inflated(depth: 0.6), parts:
            [-45.0, 0, 45].map { angle in
                CharacterPart(leaf, x: angle / 150, y: -0.76, z: 0.13,
                              width: 0.14, height: 0.35, depth: 0.06, roll: angle, profile: cone)
            } + seeds),
        .bun: CharacterModel(color: "#F1E1C3", radii: ellipse(0.91, 0.7), solid: .inflated(depth: 0.76), parts: [
            CharacterPart("#F1E1C3", y: -0.58, z: -0.03, width: 0.4, height: 0.25, depth: 0.32),
            CharacterPart("#C9A26E", x: -0.24, y: -0.47, width: 0.025, height: 0.15, roll: -35, surface: true),
            CharacterPart("#C9A26E", x: 0, y: -0.52, width: 0.025, height: 0.13, surface: true),
            CharacterPart("#C9A26E", x: 0.24, y: -0.47, width: 0.025, height: 0.15, roll: 35, surface: true)
        ]),
        .toast: CharacterModel(color: "#A86632", radii: toast, solid: .inflated(depth: 0.23), parts: [
            CharacterPart("#EAC18A", y: 0, z: 0.24, width: 0.8, height: 0.8, depth: 0.018, profile: toast, surface: true)
        ]),
        .cupcake: CharacterModel(color: "#C7AF85", radii: cake, solid: .revolved, parts: [], frosting: true),
        .cat: CharacterModel(color: "#8796AC", radii: ellipse(0.81, 0.69), solid: .inflated(depth: 0.62), parts: catParts)
    ]
}()
