import CoreGraphics
import Foundation

/// Decodes every mascot, so the first frame that shows one doesn't have to.
public func preloadMascots() {
    for id in MASCOT_MODELS.keys { _ = mascot(id) }
}

/// A triangle's three vertices and its paint, packed because a mesh has thousands.
struct MeshFace: Sendable {
    private let packed: SIMD4<UInt16>
    var corners: [Int] { [self[0], self[1], self[2]] }

    init(_ a: UInt16, _ b: UInt16, _ c: UInt16, paint: UInt16) { packed = SIMD4(a, b, c, paint) }

    subscript(index: Int) -> Int { Int(packed[index]) }
}

// DS Bot's SDF surfaces baked by scripts/avatar-mesh.mjs.
struct MascotMesh: Sendable {
    let color: String
    let radii: [Double]
    let eyes: [[Double]]
    let paints: [(String, Double)]
    let points: [Vec3]
    let faces: [MeshFace]
    /// The faces each paint covers, so drawing a paint doesn't search them all.
    let painted: [[Int]]
    /// How far the mesh reaches from its centre, for sizing it like the other avatars.
    let extent: Double

    init(color: String, radii: [Double], eyes: [[Double]], paints: [(String, Double)], encoded: String) {
        self.color = color; self.radii = radii; self.eyes = eyes; self.paints = paints
        let packed = Data(base64Encoded: encoded)! as NSData
        let bytes = [UInt8](try! packed.decompressed(using: .zlib) as Data)
        func word(_ at: Int) -> Int { Int(bytes[at]) | Int(bytes[at + 1]) << 8 }
        let vertices = word(0) | word(2) << 16, triangles = word(4) | word(6) << 16
        var start = 8
        // Each column is delta-coded with its low bytes first, so it compresses well.
        func column(_ count: Int) -> [UInt16] {
            var value: UInt16 = 0
            defer { start += count * 2 }
            return (0..<count).map { index in
                value &+= UInt16(bytes[start + index]) | UInt16(bytes[start + count + index]) << 8
                return value
            }
        }
        func scaled(_ value: UInt16) -> Double { Double(Int16(bitPattern: value)) / 10000 }
        let xs = column(vertices), ys = column(vertices), zs = column(vertices)
        points = xs.indices.map { Vec3(x: scaled(xs[$0]), y: scaled(ys[$0]), z: scaled(zs[$0])) }
        let first = column(triangles), second = column(triangles), third = column(triangles), paint = column(triangles)
        faces = first.indices.map { MeshFace(first[$0], second[$0], third[$0], paint: paint[$0]) }
        var painted = [[Int]](repeating: [], count: paints.count)
        for index in paint.indices where paint[index] > 0 { painted[Int(paint[index]) - 1].append(index) }
        self.painted = painted
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
        for (i, face) in faces.enumerated() where visible[i] { outline.add(face[0], face[1], face[2]) }
        let onScreen = points.indices.map(screen)
        let path = outline.path(onScreen)
        // Materials are drawn over the body without a depth test, so clip them to what nothing nearer hides.
        let depth = DepthGrid(projected, faces: faces, visible: visible)
        eyes?(eyePoses(sil, rotation: rotation, scale: scale, exposed: depth.exposed))
        var seen = [Bool](repeating: false, count: points.count), open = seen
        for faceIndices in painted {
            for i in faceIndices where visible[i] {
                for corner in 0..<3 where !seen[faces[i][corner]] {
                    let index = faces[i][corner]
                    seen[index] = true
                    open[index] = depth.exposed(projected[index])
                }
            }
        }
        // Trims a partly hidden triangle at the edge of whatever covers it.
        func clipped(_ face: MeshFace, _ open: [Bool]) -> [Vec3] {
            var polygon: [Vec3] = []
            for index in 0..<3 {
                var a = projected[face[index]], b = projected[face[(index + 1) % 3]]
                let visible = open[index]
                if visible { polygon.append(a) }
                guard visible != open[(index + 1) % 3] else { continue }
                for _ in 0..<6 {
                    let mid = Vec3(x: (a.x + b.x) / 2, y: (a.y + b.y) / 2, z: (a.z + b.z) / 2)
                    if depth.exposed(mid) == visible { a = mid } else { b = mid }
                }
                polygon.append(a)
            }
            return polygon
        }
        let layers = paints.enumerated().compactMap { index, paint -> CharacterLayer? in
            let decal = CGMutablePath()
            // Whole triangles merge into their outline, so Core Animation isn't handed thousands of them.
            var whole = MeshOutline()
            for i in painted[index] where visible[i] {
                let face = faces[i]
                let a = open[face[0]], b = open[face[1]], c = open[face[2]]
                if a && b && c {
                    whole.add(face[0], face[1], face[2])
                } else if a || b || c {
                    let polygon = clipped(face, [a, b, c])
                    for (index, p) in polygon.enumerated() {
                        let point = CGPoint(x: (sil.cx + p.x) * scale, y: (sil.cy + p.y) * scale)
                        if index == 0 { decal.move(to: point) } else { decal.addLine(to: point) }
                    }
                    decal.closeSubpath()
                }
            }
            decal.addPath(whole.path(onScreen))
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
    /// Each face's nearest point, so most faces are ruled out without reading their corners.
    let tops: [Double]
    let minX, minY, cell: Double
    let size = 96
    var start: [Int], members: [Int], nearest: [Double]

    init(_ projected: [Vec3], faces: [MeshFace], visible: [Bool]) {
        self.projected = projected
        corners = faces.map { ($0[0], $0[1], $0[2]) }
        tops = corners.map { max(projected[$0.0].z, projected[$0.1].z, projected[$0.2].z) }
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
            let z = tops[i]
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
        for i in start[k]..<start[k + 1] where tops[members[i]] > limit {
            let face = corners[members[i]]
            let a = projected[face.0], b = projected[face.1], c = projected[face.2]
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
