import CoreGraphics
import Foundation

// DS Bot's SDF surfaces baked by scripts/avatar-mesh.mjs.
struct MascotMesh: Sendable {
    let color: String
    let radii: [Double]
    let eyes: [[Double]]
    let paints: [(String, Double)]
    let points: [Vec3]
    let faces: [[Int]]
    /// How far the mesh reaches from its centre, for sizing it like the other avatars.
    let extent: Double

    init(color: String, radii: [Double], eyes: [[Double]], paints: [(String, Double)], encoded: String) {
        self.color = color; self.radii = radii; self.eyes = eyes; self.paints = paints
        let bytes = [UInt8](Data(base64Encoded: encoded)!)
        func word(_ at: Int) -> Int { Int(bytes[at]) | Int(bytes[at + 1]) << 8 }
        let vertices = word(0) | word(2) << 16, triangles = word(4) | word(6) << 16
        points = (0..<vertices).map { index in
            let at = 8 + index * 6
            func value(_ offset: Int) -> Double { Double(Int16(bitPattern: UInt16(word(at + offset)))) / 10000 }
            return Vec3(x: value(0), y: value(2), z: value(4))
        }
        faces = (0..<triangles).map { index in
            let at = 8 + vertices * 6 + index * 8
            return [word(at), word(at + 2), word(at + 4), word(at + 6)]
        }
        extent = points.map { hypot($0.x, $0.y) }.max() ?? 1
    }

    func render(_ sil: Silhouette, rotation: Rotation, scale: Double,
                eyes: (([EyePose]) -> Void)? = nil) -> (CGPath, [CharacterLayer]) {
        let projected = points.map { rotation.apply(Vec3(x: $0.x * sil.sx, y: $0.y * sil.sy, z: $0.z)) }
        let visible = faces.map { face in
            let a = projected[face[0]], b = projected[face[1]], c = projected[face[2]]
            return (b.x - a.x) * (c.y - a.y) - (b.y - a.y) * (c.x - a.x) > 0
        }
        func screen(_ index: Int) -> CGPoint {
            let p = projected[index]
            return CGPoint(x: (sil.cx + p.x) * scale, y: (sil.cy + p.y) * scale)
        }
        var outline = MeshOutline()
        for (i, face) in faces.enumerated() where visible[i] {
            outline.add(Array(face.prefix(3)))
        }
        let path = outline.path(points.indices.map(screen)).normalized(using: .winding)
        // Materials are drawn over the body without a depth test, so clip them to what nothing nearer hides.
        let depth = DepthGrid(projected, faces: faces, visible: visible)
        eyes?(eyePoses(sil, rotation: rotation, scale: scale, exposed: depth.exposed))
        var shown = [Int8](repeating: 0, count: points.count)
        func exposed(_ index: Int) -> Bool {
            if shown[index] == 0 { shown[index] = depth.exposed(projected[index]) ? 1 : -1 }
            return shown[index] == 1
        }
        // Trims a partly hidden triangle at the edge of whatever covers it.
        func clipped(_ face: [Int], _ open: [Bool]) -> [Vec3] {
            var polygon: [Vec3] = []
            for index in 0..<3 {
                var a = projected[face[index]], b = projected[face[(index + 1) % 3]]
                let visible = open[index]
                if visible { polygon.append(a) }
                guard visible != open[(index + 1) % 3] else { continue }
                for _ in 0..<12 {
                    let mid = Vec3(x: (a.x + b.x) / 2, y: (a.y + b.y) / 2, z: (a.z + b.z) / 2)
                    if depth.exposed(mid) == visible { a = mid } else { b = mid }
                }
                polygon.append(a)
            }
            return polygon
        }
        let layers = paints.enumerated().compactMap { index, paint -> CharacterLayer? in
            let decal = CGMutablePath()
            for (i, face) in faces.enumerated() where visible[i] && face[3] == index + 1 {
                let a = exposed(face[0]), b = exposed(face[1]), c = exposed(face[2])
                if a && b && c {
                    decal.move(to: screen(face[0]))
                    decal.addLine(to: screen(face[1])); decal.addLine(to: screen(face[2]))
                } else if a || b || c {
                    let polygon = clipped(face, [a, b, c])
                    for (index, p) in polygon.enumerated() {
                        let point = CGPoint(x: (sil.cx + p.x) * scale, y: (sil.cy + p.y) * scale)
                        if index == 0 { decal.move(to: point) } else { decal.addLine(to: point) }
                    }
                } else { continue }
                decal.closeSubpath()
            }
            guard !decal.isEmpty else { return nil }
            // Mesh material paths already lie on the body; avoid repeating its complex clip.
            return CharacterLayer(path: decal, color: paint.0, front: true, surface: false,
                                  alpha: paint.1, outline: CGMutablePath())
        }
        return (path, layers)
    }

    func eyePoses(_ sil: Silhouette, rotation: Rotation, scale: Double,
                  exposed: (Vec3) -> Bool = { _ in true }) -> [EyePose] {
        eyes.map { eye in
            func project(_ x: Double, _ y: Double, _ z: Double) -> Vec3 {
                rotation.apply(Vec3(x: x * sil.sx, y: y * sil.sy, z: z))
            }
            let p = project(eye[0], -eye[1], eye[2])
            let u = project(eye[3], -eye[4], eye[5]), v = project(-eye[6], eye[7], -eye[8])
            let n = rotation.apply(Vec3(x: eye[9], y: -eye[10], z: eye[11]))
            return EyePose(x: (sil.cx + p.x) * scale, y: (sil.cy + p.y) * scale,
                           a: u.x * eye[12] / EYE_W, b: u.y * eye[12] / EYE_W,
                           c: v.x * eye[13] / EYE_H, d: v.y * eye[13] / EYE_H, depth: exposed(p) ? n.z : 0)
        }
    }
}

/// Front-facing triangles bucketed by screen cell, to find the nearest surface over a point.
private struct DepthGrid {
    let projected: [Vec3]
    let corners: [(Int, Int, Int)]
    let minX, minY, cell: Double
    let size = 96
    var start: [Int], members: [Int], nearest: [Double]

    init(_ projected: [Vec3], faces: [[Int]], visible: [Bool]) {
        self.projected = projected
        corners = faces.map { ($0[0], $0[1], $0[2]) }
        let xs = projected.map(\.x), ys = projected.map(\.y)
        minX = xs.min() ?? 0; minY = ys.min() ?? 0
        cell = max((xs.max() ?? 0) - minX, (ys.max() ?? 0) - minY, 1e-6) / Double(size) * 1.0001
        let count = size * size
        var ranges: [(Int, Int, Int, Int)] = []
        var totals = [Int](repeating: 0, count: count + 1)
        nearest = [Double](repeating: -.infinity, count: count)
        for (i, face) in corners.enumerated() {
            guard visible[i] else { ranges.append((1, 0, 1, 0)); continue }
            let a = projected[face.0], b = projected[face.1], c = projected[face.2]
            let range = (Self.index(min(a.x, b.x, c.x), minX, cell, size), Self.index(max(a.x, b.x, c.x), minX, cell, size),
                         Self.index(min(a.y, b.y, c.y), minY, cell, size), Self.index(max(a.y, b.y, c.y), minY, cell, size))
            ranges.append(range)
            let z = max(a.z, b.z, c.z)
            for y in range.2...range.3 {
                for x in range.0...range.1 { totals[y * size + x + 1] += 1; nearest[y * size + x] = max(nearest[y * size + x], z) }
            }
        }
        for k in 0..<count { totals[k + 1] += totals[k] }
        start = totals
        var fill = totals
        members = [Int](repeating: 0, count: totals[count])
        for (i, range) in ranges.enumerated() where range.0 <= range.1 {
            for y in range.2...range.3 { for x in range.0...range.1 { members[fill[y * size + x]] = i; fill[y * size + x] += 1 } }
        }
    }

    private static func index(_ v: Double, _ origin: Double, _ cell: Double, _ size: Int) -> Int {
        min(size - 1, max(0, Int((v - origin) / cell)))
    }

    /// Whether no other front-facing surface lies in front of `p`, allowing for neighbours along creases.
    func exposed(_ p: Vec3) -> Bool {
        let k = Self.index(p.y, minY, cell, size) * size + Self.index(p.x, minX, cell, size)
        let limit = p.z + 0.02
        if nearest[k] <= limit { return true }
        for i in start[k]..<start[k + 1] {
            let face = corners[members[i]]
            let a = projected[face.0], b = projected[face.1], c = projected[face.2]
            guard max(a.z, b.z, c.z) > limit else { continue }
            let area = (b.x - a.x) * (c.y - a.y) - (b.y - a.y) * (c.x - a.x)
            guard area > 1e-12 else { continue }
            let u = ((b.x - p.x) * (c.y - p.y) - (b.y - p.y) * (c.x - p.x)) / area
            let v = ((c.x - p.x) * (a.y - p.y) - (c.y - p.y) * (a.x - p.x)) / area
            let w = 1 - u - v
            guard u >= 0, v >= 0, w >= 0 else { continue }
            if u * a.z + v * b.z + w * c.z > limit { return false }
        }
        return true
    }
}
