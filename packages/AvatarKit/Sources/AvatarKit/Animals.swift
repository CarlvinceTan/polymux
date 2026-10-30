import Foundation

// OneWorks default anatomy, adapted to the shared solid-colour renderer and animated eyes.
let ANIMALS: [ShapeID: CharacterModel] = {
    let cream = "#F0DFC0"
    let cone = regularPolygonProfile(3, 1, 0.26, -90)
    let rectangle = superellipseProfile(5)
    func head(_ color: String, _ width: Double, _ height: Double, _ depth: Double,
              taper: Double = 0, eyeColor: String = "#231A20", parts: [CharacterPart]) -> CharacterModel {
        let radii = profileFromPolygon((0..<256).map { index in
            let a = (Double(index) + 0.5) * TAU / 256, y = sin(a)
            return Point(x: width * cos(a) * (1 - taper * y), y: height * y)
        }, 0, 0)
        return CharacterModel(color: color, radii: radii, solid: .inflated(depth: depth), parts: parts, eyeColor: eyeColor)
    }
    func pair(_ color: String, x: Double, y: Double, z: Double = -0.04,
              width: Double, height: Double, depth: Double = 0.18,
              roll: Double = 0, profile: [Double]? = nil, behind: Bool = false) -> [CharacterPart] {
        [-1.0, 1.0].map { side in
            CharacterPart(color, x: x * side, y: y, z: z, width: width, height: height,
                          depth: depth, roll: roll * side, profile: profile, behind: behind)
        }
    }
    func mask(_ color: String = cream, x: Double = 0, y: Double = 0.29,
              width: Double = 0.38, height: Double = 0.27, roll: Double = 0) -> CharacterPart {
        CharacterPart(color, x: x, y: y, z: 0.62, width: width, height: height,
                      roll: roll, surface: true)
    }
    func nose(y: Double = 0.36, z: Double = 0.72, width: Double = 0.09,
              color: String = "#35251F") -> CharacterPart {
        CharacterPart(color, y: y, z: z, width: width, height: 0.06, depth: 0.04)
    }
    func tuft(_ color: String, y: Double = -0.68) -> [CharacterPart] {
        [-1.0, 0, 1].map { side in
            CharacterPart(color, x: side * 0.29, y: y - (side == 0 ? 0.1 : 0), z: 0,
                          width: 0.25, height: 0.25, depth: 0.22)
        }
    }
    func snout(_ color: String, width: Double = 0.34, y: Double = 0.39,
               z: Double = 0.57, depth: Double = 0.25) -> [CharacterPart] {
        [CharacterPart(color, y: y, z: z, width: width, height: 0.2, depth: depth)]
    }
    func nostrils(_ color: String, x: Double = 0.13, y: Double = 0.39, z: Double = 0.81) -> [CharacterPart] {
        pair(color, x: x, y: y, z: z, width: 0.035, height: 0.054, depth: 0.022)
    }
    func bill(_ color: String, width: Double, depth: Double = 0.22) -> [CharacterPart] {
        [CharacterPart(color, y: 0.35, z: 0.68, width: width, height: 0.12, depth: depth)]
    }
    let antlers: [CharacterPart] = [-1.0, 1.0].flatMap { (side: Double) -> [CharacterPart] in
        [CharacterPart("#806447", x: side * 0.36, y: -0.97, z: -0.03,
                       width: 0.065, height: 0.38, depth: 0.06, roll: side * 20),
         CharacterPart("#806447", x: side * 0.52, y: -1.04, z: -0.03,
                       width: 0.055, height: 0.2, depth: 0.055, roll: side * 55),
         CharacterPart("#806447", x: side * 0.43, y: -1.19, z: -0.03,
                       width: 0.05, height: 0.16, depth: 0.05, roll: side * 55)]
    }
    return [
        .pig: head("#D68E8C", 0.78, 0.73, 0.7, parts:
            pair("#D68E8C", x: 0.54, y: -0.52, width: 0.25, height: 0.33, depth: 0.19, roll: 27, profile: cone)
            + snout("#F8C5BF", width: 0.35) + nostrils("#713F43")),
        .deer: head("#B77A4C", 0.66, 0.82, 0.64, taper: 0.12, parts: antlers
            + pair("#B77A4C", x: 0.57, y: -0.49, width: 0.2, height: 0.35, roll: 36)
            + [mask("#F5E7CF", y: 0.25, width: 0.32, height: 0.4), nose(y: 0.42, z: 0.58)]),
        .cow: head("#B4A58E", 0.76, 0.81, 0.79, parts:
            pair("#8C7859", x: 0.4, y: -0.77, width: 0.12, height: 0.28, roll: 28, profile: cone)
            + pair("#B4A58E", x: 0.65, y: -0.41, width: 0.21, height: 0.32, roll: 58)
            + [mask("#F7EBDC", y: 0, width: 0.55, height: 0.57),
               mask("#675342", x: -0.3, y: -0.48, width: 0.19, height: 0.14, roll: -25),
               mask("#675342", x: 0.44, y: 0.17, width: 0.17, height: 0.21, roll: 15)]
            + snout("#DDA8A2", width: 0.42, y: 0.43, z: 0.64)
            + nostrils("#60403C", x: 0.17, y: 0.43, z: 0.89)),
        .tiger: head("#DF9650", 0.82, 0.74, 0.78, parts:
            pair("#DF9650", x: 0.57, y: -0.59, width: 0.24, height: 0.27, depth: 0.22)
            + [mask("#F8E8CE", y: 0.33, width: 0.48, height: 0.29), nose(z: 0.74),
               mask("#624330", y: -0.55, width: 0.055, height: 0.18),
               mask("#624330", x: -0.23, y: -0.5, width: 0.05, height: 0.15, roll: -15),
               mask("#624330", x: 0.23, y: -0.5, width: 0.05, height: 0.15, roll: 15),
               mask("#624330", x: -0.63, y: 0.04, width: 0.14, height: 0.045, roll: 20),
               mask("#624330", x: 0.63, y: 0.04, width: 0.14, height: 0.045, roll: -20),
               mask("#624330", x: -0.59, y: 0.27, width: 0.13, height: 0.04, roll: 10),
               mask("#624330", x: 0.59, y: 0.27, width: 0.13, height: 0.04, roll: -10)]),
        .beaver: head("#997252", 0.79, 0.76, 0.81, parts:
            pair("#997252", x: 0.55, y: -0.57, width: 0.17, height: 0.19)
            + pair("#DAC4A9", x: 0.19, y: 0.36, z: 0.64, width: 0.25, height: 0.23, depth: 0.2)
            + pair("#F1E8D6", x: 0.085, y: 0.61, z: 0.73, width: 0.075, height: 0.16, depth: 0.08, profile: rectangle)
            + [nose(y: 0.29, z: 0.86, width: 0.12)]),
        .guineaPig: head("#B97D50", 0.82, 0.72, 0.79, parts:
            pair("#B97D50", x: 0.6, y: -0.44, width: 0.2, height: 0.23, roll: 20)
            + pair("#F1DFC3", x: 0.45, y: 0.29, z: 0.47, width: 0.31, height: 0.3, depth: 0.25)
            + [mask("#F1DFC3", y: 0.22, width: 0.33, height: 0.3), nose(y: 0.3, z: 0.75, width: 0.08)]),
        .chinchilla: head("#969BA5", 0.76, 0.73, 0.8, parts:
            pair("#969BA5", x: 0.49, y: -0.65, width: 0.28, height: 0.37, depth: 0.2)
            + pair("#E7E5E0", x: 0.39, y: 0.32, z: 0.48, width: 0.3, height: 0.32, depth: 0.27)
            + [nose(y: 0.29, z: 0.78, width: 0.075)]),
        .ferret: head("#8B6750", 0.68, 0.84, 0.76, taper: 0.2, parts:
            pair("#8B6750", x: 0.45, y: -0.65, width: 0.17, height: 0.2)
            + [mask("#EAD7BA", y: 0.18, width: 0.5, height: 0.57),
               mask("#80624C", x: -0.25, y: -0.05, width: 0.2, height: 0.24),
               mask("#80624C", x: 0.25, y: -0.05, width: 0.2, height: 0.24), nose(y: 0.43, z: 0.66)]),
        .monkey: head("#8B6244", 0.72, 0.8, 0.79, parts:
            pair("#8B6244", x: 0.73, y: -0.04, z: 0.09, width: 0.3, height: 0.33, depth: 0.21)
            + pair("#D5A77C", x: 0.78, y: -0.04, z: 0.26, width: 0.15, height: 0.2, depth: 0.06)
            + [mask("#D5A77C", y: 0.03, width: 0.54, height: 0.6)]
            + snout("#D5A77C", width: 0.39, y: 0.4, z: 0.62, depth: 0.27)
            + nostrils("#483028", x: 0.09, y: 0.35, z: 0.89)),
        .chick: head("#E5B737", 0.77, 0.77, 0.8, parts: tuft("#E5B737", y: -0.63)
            + [mask("#F8DD72", x: -0.45, width: 0.22, height: 0.24),
               mask("#F8DD72", x: 0.45, width: 0.22, height: 0.24)]
            + bill("#E99437", width: 0.2)),
        .duck: head("#E8BC3D", 0.76, 0.78, 0.8, parts:
            [mask("#F8DC72", x: -0.45, width: 0.21, height: 0.23),
             mask("#F8DC72", x: 0.45, width: 0.21, height: 0.23)]
            + bill("#DC8738", width: 0.34, depth: 0.26)),
        .goose: head("#B9B2A2", 0.67, 0.83, 0.73, taper: 0.08, parts:
            [mask("#F7F3E9", y: 0, width: 0.48, height: 0.59)]
            + bill("#DF913D", width: 0.28, depth: 0.25))
    ]
}()
