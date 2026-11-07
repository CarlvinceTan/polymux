import CoreGraphics
import Testing
@testable import AvatarKit

struct SurfaceTests {
    @Test func linerFoldsWrap() throws {
        let model = try #require(ShapeID.cupcake.character)
        let rotations = [Rotation.identity, .rest, Rotation(yaw: -75, pitch: 25),
                         Rotation(yaw: 35, pitch: -30), Rotation(yaw: 90, pitch: 15),
                         Rotation(yaw: 180, pitch: 0)]
        for (sx, sy) in [(1.0, 1.0), (1.2, 0.8)] {
            let sil = Silhouette(radii: model.radii, sx: sx, sy: sy)
            for rotation in rotations {
                let folds = model.layers(sil, rotation: rotation, scale: 100).filter { $0.color == "#E8D8B8" }
                for index in 0..<24 {
                    for y in [-0.14, 0.1, 0.4, 0.65] {
                        let face = surface(.revolved, 0, y, model.radii)
                        for offset in [0.0, 0.5] {
                            let angle = (Double(index) + offset) * TAU / 24
                            let normal = Vec3(x: sin(angle) / sx, y: face.normal.y / face.z / sy, z: cos(angle))
                            guard rotation.apply(normal).z > 0.3 else { continue }
                            let p = rotation.apply(Vec3(x: face.z * sin(angle) * sx, y: y * sy, z: face.z * cos(angle)))
                            let point = CGPoint(x: p.x * 100, y: p.y * 100)
                            #expect(folds.contains { $0.path.contains(point) } == (offset == 0),
                                    "Liner fold or gap breaks at \(rotation), band \(index), height \(y)")
                        }
                    }
                }
            }
        }
    }

    @Test func leafSharesAxis() throws {
        let mesh = try #require(MASCOTS[.leaf])
        // The blade must taper onto the stem's axis in depth as well as horizontally.
        for range in [0.8...0.88, 0.88...0.96, 0.96...1.04, 1.04...1.17] {
            let section = mesh.points.filter { range.contains($0.y) }
            #expect(!section.isEmpty)
            let minX = try #require(section.map(\.x).min()), maxX = try #require(section.map(\.x).max())
            let minZ = try #require(section.map(\.z).min()), maxZ = try #require(section.map(\.z).max())
            for yaw in [-60.0, 0, 60, 180] {
                let center = Rotation(yaw: yaw, pitch: 0).apply(Vec3(x: (minX + maxX) / 2, y: 0, z: (minZ + maxZ) / 2))
                #expect(abs(center.x) < 0.015, "Leaf forks away from its stem at \(range), yaw \(yaw)")
            }
        }
    }

    @Test func cameraHasBarrel() throws {
        let mesh = try #require(MASCOTS[.camera])
        let barrel = try #require(mesh.paints.firstIndex { $0.0 == "#526478" }) + 1
        let glass = try #require(mesh.paints.firstIndex { $0.0 == "#9CD5D6" }) + 1
        func vertices(_ material: Int) -> [Vec3] {
            mesh.faces.filter { $0[3] == material }.flatMap { $0.corners.map { mesh.points[$0] } }
        }
        let casing = vertices(0).filter { hypot($0.x - 0.1, $0.y - 0.1) > 0.55 }
        let bodyDepth = try #require(casing.map(\.z).max())
        let glassDepth = try #require(vertices(glass).map(\.z).min())
        #expect(glassDepth > bodyDepth, "The lens is only painted onto the camera body")
        let sides = vertices(barrel).filter { $0.z > bodyDepth && $0.z < glassDepth }
        #expect(sides.contains { $0.x < -0.25 })
        #expect(sides.contains { $0.x > 0.45 })
        #expect(sides.contains { $0.y < -0.25 })
        #expect(sides.contains { $0.y > 0.45 })
        for yaw in [-60.0, 0, 60] {
            let rotation = Rotation(yaw: yaw, pitch: 0)
            let (_, layers) = mesh.render(Silhouette(radii: mesh.radii), rotation: rotation, scale: 100)
            let center = rotation.apply(Vec3(x: 0.1, y: 0.1, z: 0.845))
            let paint = layers.last { $0.path.contains(CGPoint(x: center.x * 100, y: center.y * 100)) }?.color
            #expect(paint == "#9CD5D6", "Camera body covers the raised lens at yaw \(yaw)")
        }
    }
}
