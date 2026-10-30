import CoreGraphics
import Foundation

// One continuous vanilla spiral with surface-bound sprinkles.
enum Cupcake {
    private struct Vertex {
        var point: Vec3
        var normal: Vec3
    }

    private static func vertex(_ t: Double, _ around: Double) -> Vertex {
        let angle = t * TAU * 3.15
        let radius = 0.51 * pow(1 - t, 0.82)
        let tube = 0.18 * (1 - 0.85 * t) * (1 + 0.06 * cos(5 * around))
        let reach = radius + tube * cos(around)
        return Vertex(point: Vec3(x: cos(angle) * reach + 0.09 * pow(t, 6),
                                  y: -0.54 - 0.82 * (1.7 * t - 0.7 * t * t) + tube * sin(around), z: sin(angle) * reach),
                      normal: Vec3(x: cos(angle) * cos(around), y: sin(around), z: sin(angle) * cos(around)))
    }

    private static let vertices: [Vertex] = (0...192).flatMap { row in
        (0..<16).map { col in vertex(Double(row) / 192, Double(col) * TAU / 16) }
    }

    private struct Triangle {
        let indices: [Int]
        let shaded: Bool
    }

    private static let triangles: [Triangle] = (0..<192).flatMap { row in
        (0..<16).flatMap { col in
            let next = (col + 1) % 16, a = row * 16, b = (row + 1) * 16
            return [[a + col, a + next, b + next], [a + col, b + next, b + col]].map { indices in
                Triangle(indices: indices, shaded: indices.map { vertices[$0].normal.y }.reduce(0, +) / 3 > 0.35)
            }
        }
    }

    // The solid mound joins the piping to the exposed cake above the liner.
    private static let core: [Vec3] = (-1...48).flatMap { row in
        let t = Double(max(row, 0)) / 48
        let radius = row < 0 ? 0.64 : 0.59 * pow(1 - t, 0.82)
        let y = row < 0 ? -0.36 : -0.54 - 0.82 * (1.7 * t - 0.7 * t * t)
        return (0..<32).map { col in
            let angle = Double(col) * TAU / 32
            return Vec3(x: cos(angle) * radius + 0.09 * pow(t, 6), y: y, z: sin(angle) * radius)
        }
    }

    private static let folds: [(y: Double, radius: Double)] = (0...8).map { row in
        let y = lerp(-0.16, 0.67, Double(row) / 8)
        return (y, surface(.revolved, 0, y, ShapeID.cupcake.radii).z)
    }

    static func grooves(_ sil: Silhouette, rotation: Rotation, scale: Double) -> [CharacterLayer] {
        let path = CGMutablePath()
        let slope = (folds[0].radius - folds[8].radius) / (folds[8].y - folds[0].y)
        func facing(_ angle: Double) -> Double {
            rotation.apply(Vec3(x: sin(angle) / sil.sx, y: slope / sil.sy, z: cos(angle))).z
        }
        func horizon(_ hidden: Double, _ visible: Double) -> Double {
            var a = hidden, b = visible
            for _ in 0..<16 {
                let mid = (a + b) / 2
                if facing(mid) > 0 { b = mid } else { a = mid }
            }
            return (a + b) / 2
        }
        func point(_ ring: (y: Double, radius: Double), _ angle: Double) -> CGPoint {
            let p = rotation.apply(Vec3(x: ring.radius * sin(angle) * sil.sx,
                                        y: ring.y * sil.sy, z: ring.radius * cos(angle)))
            return CGPoint(x: (sil.cx + p.x) * scale, y: (sil.cy + p.y) * scale)
        }
        for index in 0..<24 {
            let center = Double(index) * TAU / 24
            var a = center - 0.03, b = center + 0.03
            guard let visible = [center, a, b].first(where: { facing($0) > 0 }) else { continue }
            if facing(a) <= 0 { a = horizon(a, visible) }
            if facing(b) <= 0 { b = horizon(b, visible) }
            path.move(to: point(folds[0], a))
            for ring in folds.dropFirst() { path.addLine(to: point(ring, a)) }
            for ring in folds.reversed() { path.addLine(to: point(ring, b)) }
            path.closeSubpath()
        }
        return [CharacterLayer(path: path, color: "#E8D8B8", front: true, surface: true,
                               outline: CGMutablePath())]
    }

    static func layers(_ sil: Silhouette, rotation: Rotation, scale: Double) -> [CharacterLayer] {
        func project(_ point: Vec3) -> Vec3 {
            rotation.apply(Vec3(x: point.x * sil.sx, y: point.y * sil.sy, z: point.z))
        }
        func screen(_ point: Vec3) -> CGPoint {
            CGPoint(x: (sil.cx + point.x) * scale, y: (sil.cy + point.y) * scale)
        }
        let cream = "#FFF0CC", crease = "#F5E1B5"
        func winding(_ indices: [Int], _ points: [Vec3]) -> [Int] {
            let a = points[indices[0]], b = points[indices[1]], c = points[indices[2]]
            let area = (b.x - a.x) * (c.y - a.y) - (b.y - a.y) * (c.x - a.x)
            return area < 0 ? Array(indices.reversed()) : indices
        }
        func padded(_ path: CGPath) -> CGPath {
            guard !path.isEmpty else { return path }
            let result = path.mutableCopy()!
            result.addPath(path.copy(strokingWithWidth: 0.006 * scale, lineCap: .round,
                                     lineJoin: .round, miterLimit: 1))
            return result
        }
        let rings = [(0.73, -0.23), (0.73, -0.3), (0.63, -0.43), (0.0, -0.47)]
        let cakePoints = rings.flatMap { radius, y in
            (0..<64).map { col in
                let angle = Double(col) * TAU / 64
                return project(Vec3(x: cos(angle) * radius, y: y, z: sin(angle) * radius))
            }
        }
        var cake = MeshOutline()
        for row in 0..<(rings.count - 1) {
            for col in 0..<64 {
                let next = (col + 1) % 64, a = row * 64, b = (row + 1) * 64
                cake.add(winding([a + col, a + next, b + next, b + col], cakePoints))
            }
        }
        let points = vertices.map { project($0.point) }
        let projected = points.map(screen)
        let normals = vertices.map { rotation.apply($0.normal).z }
        var faces: [(depth: Double, color: String, indices: [Int], path: CGPath?)] = []
        var backing = MeshOutline(), underlay = MeshOutline(), moundOutline = MeshOutline(), visibleOutline = MeshOutline()
        let mound = core.map(project)
        for row in 0..<49 {
            for col in 0..<32 {
                let next = (col + 1) % 32, a = row * 32, b = (row + 1) * 32
                moundOutline.add(winding([a + col, a + next, b + next, b + col], mound))
            }
        }
        faces.reserveCapacity(triangles.count / 2)
        for triangle in triangles {
            let ids = triangle.indices
            let facing = normals[ids[0]] + normals[ids[1]] + normals[ids[2]]
            // Faces just past the outline still fill the backing, so its edge doesn't step.
            guard facing > -0.5 else { continue }
            let indices = winding(ids, points)
            backing.add(indices)
            guard facing > 0 else { continue }
            visibleOutline.add(indices)
            if triangle.shaded { underlay.add(indices) }
            let depth = (points[ids[0]].z + points[ids[1]].z + points[ids[2]].z) / 3
            faces.append((depth, triangle.shaded ? crease : cream, indices, nil))
        }
        let colors = ["#EF476F", "#FF8B21", "#28AE83", "#7860B9", "#F2CB2E"]
        for index in 0..<32 {
            let t = 0.035 + Double(index) / 32 * 0.88
            let around = -0.7 + Double(index % 3) * 0.5
            let sample = vertex(t, around)
            guard rotation.apply(sample.normal).z > 0.08 else { continue }
            let point = Vec3(x: sample.point.x + sample.normal.x * 0.015,
                             y: sample.point.y + sample.normal.y * 0.015,
                             z: sample.point.z + sample.normal.z * 0.015)
            let angle = t * TAU * 3.15
            let direction = index % 2 == 0
                ? Vec3(x: -sin(angle), y: 0.25, z: cos(angle))
                : Vec3(x: cos(angle) * sin(around), y: -cos(around), z: sin(angle) * sin(around))
            let path = CGMutablePath()
            path.move(to: screen(project(Vec3(x: point.x - direction.x * 0.04,
                                             y: point.y - direction.y * 0.04, z: point.z - direction.z * 0.04))))
            path.addLine(to: screen(project(Vec3(x: point.x + direction.x * 0.04,
                                                y: point.y + direction.y * 0.04, z: point.z + direction.z * 0.04))))
            let capsule = path.copy(strokingWithWidth: 0.035 * scale, lineCap: .round, lineJoin: .round, miterLimit: 1)
            faces.append((project(point).z + 0.01, colors[index % colors.count], [], capsule))
        }
        // Creases also sit under every face, so seams between layers can't show cream through them at small sizes.
        let backdrop = moundOutline.path(mound.map(screen)).mutableCopy()!
        backdrop.addPath(backing.path(projected))
        var layers = [CharacterLayer(path: cake.path(cakePoints.map(screen)), color: "#D9A667", front: true, surface: false),
                      CharacterLayer(path: backdrop.normalized(using: .winding), color: cream, front: true, surface: false),
                      CharacterLayer(path: padded(underlay.path(projected)), color: crease, front: true, surface: false)]
        // Creases join the cream run they fall in, drawn over it, so shading adds no extra layers.
        var paint: String?, outline = MeshOutline(), shade = MeshOutline(), sprinkles = CGMutablePath()
        func flush() {
            let path = padded(outline.path(projected)).mutableCopy()!
            path.addPath(sprinkles)
            let shadow = padded(shade.path(projected))
            if let paint, !path.isEmpty { layers.append(CharacterLayer(path: path, color: paint, front: true, surface: false)) }
            if !shadow.isEmpty { layers.append(CharacterLayer(path: shadow, color: crease, front: true, surface: false)) }
        }
        for face in faces.sorted(by: { $0.depth < $1.depth }) {
            let color = face.color == crease ? cream : face.color
            if paint != color {
                flush()
                paint = color
                outline = MeshOutline()
                shade = MeshOutline()
                sprinkles = CGMutablePath()
            }
            if let path = face.path { sprinkles.addPath(path) }
            else if face.color == crease { shade.add(face.indices) }
            else { outline.add(face.indices) }
        }
        flush()
        // The cake and frosting are above the liner's rim, so the top plane determines their draw order.
        let front = rotation.apply(Vec3(x: 0, y: -1, z: 0)).z >= 0
        var boundary = backdrop.union(cake.path(cakePoints.map(screen)))
        boundary = boundary.union(padded(visibleOutline.path(projected).normalized(using: .winding)))
        for face in faces { if let path = face.path { boundary = boundary.union(path) } }
        let empty = CGMutablePath()
        return layers.enumerated().map { index, layer in
            var layer = layer
            layer.front = front
            layer.outline = index == 0 ? boundary : empty
            return layer
        }
    }
}
