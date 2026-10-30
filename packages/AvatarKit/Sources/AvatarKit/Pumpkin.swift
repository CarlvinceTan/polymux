import CoreGraphics
import Foundation

// Grok Bot 0.75.3: Td/Dd/Pd body and ef stem, in Polymux's y-down coordinates.
enum Pumpkin {
    static func radius(_ point: Vec3) -> Double {
        let length = max(1e-12, hypot(hypot(point.x, point.y), point.z))
        let latitude = (asin(clamp(-point.y / length, -1, 1)) + .pi / 2) / .pi * 160
        let longitude = (atan2(point.z, point.x) + .pi) / TAU * 320
        let row = min(159, max(0, Int(floor(latitude))))
        let col = min(319, max(0, Int(floor(longitude))))
        let y = latitude - Double(row), x = longitude - Double(col)
        let index = row * 321 + col
        return lerp(lerp(Double(table[index]), Double(table[index + 1]), x),
                    lerp(Double(table[index + 321]), Double(table[index + 322]), x), y)
    }

    static func field(_ point: Vec3) -> Double {
        hypot(hypot(point.x, point.y), point.z) - radius(point)
    }

    static func outline(_ rotation: Rotation, scale: Double = 1) -> [Point] {
        let inverse = rotation.transposed
        return (0..<256).map { index in
            let angle = Double(index) * TAU / 256
            let x = cos(angle), y = sin(angle)
            var reach = 0.0
            for step in -24...24 {
                let elevation = Double(step) / 24 * 1.25
                let radial = cos(elevation)
                let direction = inverse.apply(Vec3(x: x * radial, y: y * radial, z: sin(elevation)))
                reach = max(reach, radius(direction) * radial)
            }
            return Point(x: x * reach * scale, y: y * reach * scale)
        }
    }

    static func front(_ x: Double, _ y: Double) -> Double {
        var lower = 0.0, upper = 1.08
        for _ in 0..<24 {
            let z = (lower + upper) / 2
            if field(Vec3(x: x, y: y, z: z)) > 0 { upper = z }
            else { lower = z }
        }
        return (lower + upper) / 2
    }

    /// The creases between the lobes, on the visible surface; a flat fill otherwise shows ribs only at the edge.
    static func ribs(_ sil: Silhouette, rotation: Rotation, scale: Double) -> [CharacterLayer] {
        func screen(_ point: Vec3) -> CGPoint {
            let p = rotation.apply(Vec3(x: point.x * sil.sx, y: point.y * sil.sy, z: point.z))
            return CGPoint(x: (sil.cx + p.x) * scale, y: (sil.cy + p.y) * scale)
        }
        func layer(_ ribbons: [[Ribbon]], _ color: String) -> CharacterLayer? {
            let path = CGMutablePath()
            for ribbon in ribbons {
                for (a, b) in zip(ribbon, ribbon.dropFirst())
                where rotation.apply(a.normal).z > 0.08 && rotation.apply(b.normal).z > 0.08 {
                    path.move(to: screen(a.left))
                    path.addLine(to: screen(b.left)); path.addLine(to: screen(b.right)); path.addLine(to: screen(a.right))
                    path.closeSubpath()
                }
            }
            return path.isEmpty ? nil : CharacterLayer(path: path, color: color, front: true, surface: true)
        }
        // A soft band shades each crease and a dark line marks its floor, so the lobes read as rounded.
        return [layer(creases, "#EC5600"), layer(grooves, "#B84000")].compactMap { $0 }
    }

    private typealias Ribbon = (left: Vec3, right: Vec3, normal: Vec3)

    // Ribbons along the eight valleys of `radial`'s lobes, from near the base to near the stem.
    private static func ribbons(_ width: Double) -> [[Ribbon]] {
        (0..<8).map { k in
            let longitude = .pi / 2 + 19 * .pi / 180 + .pi / 8 + Double(k) * .pi / 4
            return (0...36).map { row in
                let latitude = (-62 + Double(row) * 4) * .pi / 180
                func point(_ angle: Double) -> Vec3 {
                    let x = cos(latitude) * cos(angle), y = sin(latitude), z = cos(latitude) * sin(angle)
                    let r = radial(x, y, z) * 1.002
                    return Vec3(x: x * r, y: -y * r, z: z * r)
                }
                return (point(longitude - width), point(longitude + width),
                        Vec3(x: cos(latitude) * cos(longitude), y: -sin(latitude), z: cos(latitude) * sin(longitude)))
            }
        }
    }
    private static let creases = ribbons(0.18)
    private static let grooves = ribbons(0.04)

    static func stem(_ sil: Silhouette, rotation: Rotation, scale: Double) -> [CharacterLayer] {
        let vertices = stemVertices.map { point in
            rotation.apply(Vec3(x: point.x * sil.sx, y: point.y * sil.sy, z: point.z))
        }
        let side = CGMutablePath(), cap = CGMutablePath()
        func add(_ indices: [Int], to path: CGMutablePath) {
            let points = indices.map { vertices[$0] }
            let center = Vec3(x: points.map(\.x).reduce(0, +) / 3,
                              y: points.map(\.y).reduce(0, +) / 3,
                              z: points.map(\.z).reduce(0, +) / 3)
            let inverse = rotation.transposed
            // Occlude the stem root against the actual ribbed body, not its bounding sphere.
            if center.x * center.x + center.y * center.y < 1.08 * 1.08 {
                var z = center.z + 0.012
                while z <= 1.08 {
                    let point = inverse.apply(Vec3(x: center.x, y: center.y, z: z))
                    if field(point) < 0 { return }
                    z += 0.02
                }
            }
            let area = (points[1].x - points[0].x) * (points[2].y - points[0].y)
                - (points[1].y - points[0].y) * (points[2].x - points[0].x)
            let loop = area < 0 ? Array(points.reversed()) : points
            path.move(to: CGPoint(x: (sil.cx + loop[0].x) * scale, y: (sil.cy + loop[0].y) * scale))
            for point in loop.dropFirst() {
                path.addLine(to: CGPoint(x: (sil.cx + point.x) * scale, y: (sil.cy + point.y) * scale))
            }
            path.closeSubpath()
        }
        for row in 0..<72 {
            for col in 0..<48 {
                let next = (col + 1) % 48, a = row * 48, b = (row + 1) * 48
                add([a + col, a + next, b + next], to: side)
                add([a + col, b + next, b + col], to: side)
            }
        }
        let tip = -65 * Double.pi / 180
        if rotation.apply(Vec3(x: sin(tip), y: -cos(tip), z: 0)).z > 0 {
            for col in 0..<48 { add([3504, 3456 + col, 3456 + (col + 1) % 48], to: cap) }
        }
        return [CharacterLayer(path: side, color: "#4F7A34", front: true, surface: false),
                CharacterLayer(path: cap, color: "#6A9646", front: true, surface: false)].filter { !$0.path.isEmpty }
    }

    private static func radial(_ x: Double, _ y: Double, _ z: Double) -> Double {
        // Squatter, with deeper and sharper lobes and a wider stem dip than Grok Bot's, so it reads as a pumpkin.
        let base = 1 / pow(pow(hypot(x, z) / 1.08, 2.2) + pow(abs(y) / 0.726, 2.2), 1 / 2.2)
        let rib = abs(cos(4 * (atan2(z, x) - .pi / 2 - 19 * .pi / 180)))
        let taper = y < 0 ? pow(max(0, 1 - y * y), 0.2) : sqrt(max(0, 1 - y * y)) * (1 - 0.45 * y)
        let lobes = 1 - 0.32 * (1 - pow(rib, 0.45)) * taper
        let ring = pow(abs(cos(3 * asin(clamp(y, -1, 1)))), 0.6)
        let bottom = 1 - 0.16 * 0.55 * 0.9 * (1 - ring) * (y < 0 ? 1 : max(0, 1 - 2.2 * y))
        let dimple = y > 0 ? 0.2 * exp(-(1 - y) / 0.09) : 0.03 * exp(-(1 + y) / 0.03)
        return base * lobes * bottom * (1 - dimple)
    }

    private static let table: [Float] = (0...160).flatMap { row in
        let latitude = -.pi / 2 + Double(row) / 160 * .pi
        return (0...320).map { col in
            let longitude = -.pi + Double(col) / 320 * TAU
            return Float(radial(cos(latitude) * cos(longitude), sin(latitude), cos(latitude) * sin(longitude)))
        }
    }

    private static let stemVertices: [Vec3] = {
        let top = radial(0, 1, 0)
        var shoulder = top
        for row in 1...24 {
            let latitude = Double(row) / 24 * 0.6
            for col in 0..<32 {
                let angle = Double(col) * .pi / 16
                shoulder = max(shoulder, radial(sin(latitude) * cos(angle), cos(latitude),
                                                sin(latitude) * sin(angle)) * cos(latitude))
            }
        }
        var x = 0.0, y = top - 0.08
        var vertices: [Vec3] = []
        for row in 0...72 {
            let t = Double(row) / 72, angle = -65 * .pi / 180 * pow(t, 2.2)
            let flare = clamp(1 - (y - shoulder - 0.01) / 0.09)
            let radius = 0.115 + 0.055 * (1 - t) * (1 - t) + 0.06 * flare * flare
            for col in 0..<48 {
                let a = Double(col) * TAU / 48
                vertices.append(Vec3(x: x + radius * cos(a) * cos(angle),
                                     y: -(y - radius * cos(a) * sin(angle)), z: radius * sin(a)))
            }
            if row < 72 {
                for step in 0..<8 {
                    let a = -65 * .pi / 180 * pow(t + (Double(step) + 0.5) / 8 / 72, 2.2)
                    x += sin(a) * (0.68 / 72 / 8)
                    y += cos(a) * (0.68 / 72 / 8)
                }
            }
        }
        vertices.append(Vec3(x: x, y: -y, z: 0))
        return vertices
    }()
}
