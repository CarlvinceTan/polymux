import Foundation

public enum ShapeID: String, CaseIterable, Codable, Identifiable, Sendable {
    case circle = "cercle", pebble = "galet", squircle, capsule, triangle
    case hexagon = "hexagone", cloud = "nuage", droplet = "goutte"
    case star, clover, flower, burst, pentagon
    case square, sparkle, heart, tablet, wedge, house
    // Stored IDs stay stable when the artwork changes.
    case pumpkin, apple, pear, strawberry, bun, toast, cupcake, cat, dog, panda = "bear", rabbit
    case fox, crab = "hamster", elephant = "capybara", octopus = "otter", pig, deer, koala = "sheep"
    case giraffe = "alpaca", cow, turtle = "squirrel", tiger, frog = "lion", mouse = "hedgehog"
    case walrus = "seal", beaver, guineaPig = "guinea_pig", chinchilla, ferret, monkey
    case chick, duck, penguin, bat = "owl", axolotl = "parrot", goose
    case whale, bull, moon, wave, blossom, popcorn = "gumdrop", peanut, saucer, beacon, plus, magnet, acorn, pebbles, bulb = "cushion", trophy = "ring", gem
    case hamburger = "banana", carrot, pizza, sushi, iceCream = "ice_cream"
    case sun, leaf, tree, cactus, mushroom, mountain, volcano, rainbow, snowflake
    case rocket, robot, camera, clock, key, bell, book, teapot
    case lemon, orangeFruit = "orange_fruit", watermelon, avocado, cherry, cookie, cheese, egg, candy, donut
    case tulip, sunflower = "rose", sprout, fern, balloon = "pinecone", shell, palm = "coral", snowman = "crystal", lightning = "comet", planet
    case mug, lantern, gift, pencil, hammer = "paintbrush", umbrella
    case wateringCan = "backpack", gamepad = "headphones", guitar, telescope
    public var id: String { rawValue }
    public var label: String {
        switch self {
        case .lemon: "Lemon"
        case .orangeFruit: "Orange"
        case .watermelon: "Watermelon"
        case .avocado: "Avocado"
        case .cherry: "Cherry"
        case .cookie: "Cookie"
        case .cheese: "Cheese"
        case .egg: "Egg"
        case .candy: "Candy"
        case .donut: "Donut"
        case .tulip: "Tulip"
        case .sunflower: "Sunflower"
        case .sprout: "Sprout"
        case .fern: "Fern"
        case .balloon: "Balloon"
        case .shell: "Shell"
        case .palm: "Palm"
        case .snowman: "Snowman"
        case .lightning: "Lightning"
        case .planet: "Planet"
        case .mug: "Mug"
        case .lantern: "Lantern"
        case .gift: "Gift"
        case .pencil: "Pencil"
        case .hammer: "Hammer"
        case .umbrella: "Umbrella"
        case .wateringCan: "Watering can"
        case .gamepad: "Gamepad"
        case .guitar: "Guitar"
        case .telescope: "Telescope"
        case .hamburger: "Hamburger"
        case .carrot: "Carrot"
        case .pizza: "Pizza"
        case .sushi: "Sushi"
        case .iceCream: "Ice cream"
        case .sun: "Sun"
        case .leaf: "Leaf"
        case .tree: "Tree"
        case .cactus: "Cactus"
        case .mushroom: "Mushroom"
        case .mountain: "Mountain"
        case .volcano: "Volcano"
        case .rainbow: "Rainbow"
        case .snowflake: "Snowflake"
        case .rocket: "Rocket"
        case .robot: "Robot"
        case .camera: "Camera"
        case .clock: "Clock"
        case .key: "Key"
        case .bell: "Bell"
        case .book: "Book"
        case .teapot: "Teapot"
        case .whale: "Whale"
        case .bull: "Bull"
        case .moon: "Moon"
        case .wave: "Wave"
        case .blossom: "Blossom"
        case .popcorn: "Popcorn"
        case .peanut: "Peanut"
        case .saucer: "Saucer"
        case .beacon: "Beacon"
        case .plus: "Plus"
        case .magnet: "Magnet"
        case .acorn: "Acorn"
        case .pebbles: "Pebbles"
        case .bulb: "Light bulb"
        case .trophy: "Trophy"
        case .gem: "Gem"
        case .circle: "Circle"
        case .pebble: "Pebble"
        case .squircle: "Squircle"
        case .capsule: "Capsule"
        case .triangle: "Triangle"
        case .hexagon: "Hexagon"
        case .cloud: "Cloud"
        case .droplet: "Droplet"
        case .star: "Star"
        case .clover: "Clover"
        case .flower: "Flower"
        case .burst: "Burst"
        case .pentagon: "Pentagon"
        case .square: "Square"
        case .sparkle: "Sparkle"
        case .heart: "Heart"
        case .tablet: "Tablet"
        case .wedge: "Wedge"
        case .house: "House"
        case .pumpkin: "Pumpkin"
        case .apple: "Apple"
        case .pear: "Pear"
        case .strawberry: "Strawberry"
        case .bun: "Bun"
        case .toast: "Toast"
        case .cupcake: "Cupcake"
        case .cat: "Cat"
        case .dog: "Dog"
        case .panda: "Panda"
        case .rabbit: "Rabbit"
        case .fox: "Fox"
        case .crab: "Crab"
        case .elephant: "Elephant"
        case .octopus: "Octopus"
        case .pig: "Pig"
        case .deer: "Deer"
        case .koala: "Koala"
        case .giraffe: "Giraffe"
        case .cow: "Cow"
        case .turtle: "Turtle"
        case .tiger: "Tiger"
        case .frog: "Frog"
        case .mouse: "Mouse"
        case .walrus: "Walrus"
        case .beaver: "Beaver"
        case .guineaPig: "Guinea pig"
        case .chinchilla: "Chinchilla"
        case .ferret: "Ferret"
        case .monkey: "Monkey"
        case .chick: "Chick"
        case .duck: "Duck"
        case .penguin: "Penguin"
        case .bat: "Bat"
        case .axolotl: "Axolotl"
        case .goose: "Goose"
        }
    }
    public var radii: [Double] { SHAPE_BY_ID[rawValue]!.radii }

    public static let creationShapes: [ShapeID] = {
        let animals: Set<ShapeID> = [
            .cat, .dog, .rabbit, .pig, .deer, .koala, .turtle, .frog, .mouse,
            .monkey, .penguin, .axolotl, .whale, .bull,
            .panda, .fox, .crab, .elephant, .octopus, .giraffe, .cow, .tiger, .walrus, .bat
        ]
        return [
            .circle, .cloud, .square, .sparkle, .clover, .heart, .flower,
            .droplet, .tablet, .wedge, .house, .star, .hexagon, .plus
        ] + allCases.filter { $0.category != .shapes && ($0.category != .animals || animals.contains($0)) }
    }()
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
    let capsule = profileFromPolygon(hullOfCircles(-0.42, 0, 0.62, 0.42, 0, 0.62), 0, 0)
    let clover = normalize(unionOfCirclesProfile([-0.38, 0.38].flatMap { x in
        [-0.38, 0.38].map { y in CircleSpec(x: x, y: y, r: 0.52) }
    }), 1.04)
    let flower = normalize(unionOfCirclesProfile([CircleSpec(x: 0, y: 0, r: 0.36)] + (0..<6).map {
        let angle = -.pi / 2 + Double($0) * TAU / 6
        return CircleSpec(x: 0.58 * cos(angle), y: 0.58 * sin(angle), r: 0.33)
    }), 1.13)
    let burst = normalize(angles.map { 0.8 + 0.2 * cos(7 * $0 - .pi / 2) }, 1.1)
    let house = normalize(profileFromPolygon(roundedPolygon([
        Point(x: -0.85, y: 0.85), Point(x: -0.85, y: -0.1), Point(x: 0, y: -0.95),
        Point(x: 0.85, y: -0.1), Point(x: 0.85, y: 0.85)
    ], 0.16), 0, 0), 1.12)
    return [
        BotShape(id: .circle, radii: [Double](repeating: 1, count: PROFILE_SAMPLES)),
        BotShape(id: .pebble, radii: PEBBLE_CONTOUR),
        BotShape(id: .squircle, radii: SQUIRCLE_CONTOUR),
        BotShape(id: .capsule, radii: capsule),
        BotShape(id: .triangle, radii: regularPolygonProfile(3, 1.12, 0.34, -90)),
        BotShape(id: .hexagon, radii: HEXAGON_CONTOUR),
        BotShape(id: .cloud, radii: CLOUD_CONTOUR), BotShape(id: .droplet, radii: DROPLET_CONTOUR),
        BotShape(id: .star, radii: STAR_CONTOUR), BotShape(id: .clover, radii: clover),
        BotShape(id: .flower, radii: flower), BotShape(id: .burst, radii: burst),
        BotShape(id: .pentagon, radii: regularPolygonProfile(5, 1.08, 0.3, -90)),
        BotShape(id: .square, radii: normalize(superellipseProfile(5), 1.12)),
        BotShape(id: .sparkle, radii: SPARKLE_CONTOUR), BotShape(id: .heart, radii: HEART_CONTOUR),
        BotShape(id: .tablet, radii: TABLET_CONTOUR),
        BotShape(id: .wedge, radii: WEDGE_CONTOUR),
        BotShape(id: .house, radii: house)
    ] + ShapeID.allCases.compactMap { id in
        id.character.map { BotShape(id: id, radii: $0.radii) }
    }
}()

public let SHAPE_BY_ID = Dictionary(uniqueKeysWithValues: SHAPES.map { ($0.id.rawValue, $0) })
public let DEFAULT_SHAPE = ShapeID.circle

public let COLORS: [BotColor] = [
    BotColor(id: .ink, hex: "#000000"), BotColor(id: .brown, hex: "#936439"),
    BotColor(id: .red, hex: "#FF263C"), BotColor(id: .orange, hex: "#FF6700"),
    BotColor(id: .amber, hex: "#FF9800"), BotColor(id: .green, hex: "#00C972"),
    BotColor(id: .turquoise, hex: "#00BCA6"), BotColor(id: .blue, hex: "#1084FE"),
    BotColor(id: .purple, hex: "#9159FE"), BotColor(id: .pink, hex: "#FF309B"),
    BotColor(id: .grey, hex: "#777777"), BotColor(id: .cream, hex: "#f1efe9")
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

/// `hex`, darkened on a light `backing` or lightened on a dark one until it stands out from it.
public func legible(_ hex: String, on backing: String) -> String {
    func luminance(_ value: String) -> Double {
        let v = Int(value.dropFirst(), radix: 16) ?? 0
        let linear = [16, 8, 0].map { shift -> Double in
            let c = Double((v >> shift) & 255) / 255
            return c <= 0.04045 ? c / 12.92 : pow((c + 0.055) / 1.055, 2.4)
        }
        return 0.2126 * linear[0] + 0.7152 * linear[1] + 0.0722 * linear[2]
    }
    let back = luminance(backing)
    func contrast(_ value: String) -> Double {
        let l = luminance(value)
        return (max(l, back) + 0.05) / (min(l, back) + 0.05)
    }
    let target = back > 0.5 ? "#000000" : "#FFFFFF"
    var result = hex, t = 0.0
    while contrast(result) < 1.8 && t < 1 { t += 0.05; result = mixHex(hex, target, t) }
    return result
}
