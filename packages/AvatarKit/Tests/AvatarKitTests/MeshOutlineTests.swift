import CoreGraphics
import Testing
@testable import AvatarKit

struct MeshOutlineTests {
    @Test func sharedEdgesDisappearWithoutFillingHoles() {
        let points = (0...3).flatMap { y in (0...3).map { x in CGPoint(x: x, y: y) } }
        var outline = MeshOutline()
        let reference = CGMutablePath()
        for y in 0..<3 {
            for x in 0..<3 where x != 1 || y != 1 {
                let a = y * 4 + x, b = a + 1, c = a + 4, d = c + 1
                for face in [[a, b, d], [a, d, c]] {
                    outline.add(face)
                    reference.move(to: points[face[0]])
                    reference.addLine(to: points[face[1]])
                    reference.addLine(to: points[face[2]])
                    reference.closeSubpath()
                }
            }
        }
        let path = outline.path(points)
        var edges = 0
        path.applyWithBlock { _ in edges += 1 }
        #expect(edges < 24, "Internal triangle edges remain in the outline")
        for y in 0..<30 {
            for x in 0..<30 {
                let point = CGPoint(x: Double(x) / 10 + 0.031, y: Double(y) / 10 + 0.037)
                #expect(path.contains(point) == reference.contains(point))
            }
        }
        #expect(!path.contains(CGPoint(x: 1.5, y: 1.5)))
    }

    @Test func thinkingMovesGapWithPaint() throws {
        let engine = BotEngine(shape: ShapeID.cupcake.radii)
        engine.character = ShapeID.cupcake.character
        var frame = engine.sample(0)
        frame.parts = frame.parts.map {
            var part = $0
            part.outline = part.path
            return part
        }
        #expect(!frame.parts.isEmpty)
        var morph = ThinkingMorph()
        let phase = morph.sample(elapsed: 0.04, now: 0.04, reducedMotion: false)
        #expect(phase != nil)
        let result = morph.apply(frame, phase: phase!, scale: 100, displaySize: 44)
        #expect(result.parts.allSatisfy { $0.outline == $0.path })
        #expect(result.parts.first?.path != frame.parts.first?.path)
    }

    @Test func overlappingFacesKeepTheirWinding() {
        let points = [CGPoint(x: 0, y: 0), CGPoint(x: 2, y: 0), CGPoint(x: 1, y: 2),
                      CGPoint(x: 3, y: 2)]
        var outline = MeshOutline()
        outline.add([0, 1, 2])
        outline.add([0, 1, 2])
        outline.add([1, 3, 2])
        let path = outline.path(points)
        #expect(path.contains(CGPoint(x: 1, y: 0.5)))
        #expect(path.contains(CGPoint(x: 2, y: 1.5)))
        #expect(!path.contains(CGPoint(x: -1, y: 0)))
    }

    @Test(arguments: ShapeID.creationShapes.filter { $0.character?.mesh != nil })
    func meshSilhouetteMatchesTriangles(_ shape: ShapeID) throws {
        let model = try #require(shape.character)
        let mesh = try #require(model.mesh)
        for rotation in [Rotation.identity, .rest, Rotation(yaw: 70, pitch: -15)] {
            let (path, layers) = mesh.render(Silhouette(radii: model.radii), rotation: rotation, scale: 100)
            let points = mesh.points.map(rotation.apply)
            let reference = CGMutablePath()
            for face in mesh.faces {
                let a = points[face[0]], b = points[face[1]], c = points[face[2]]
                guard (b.x - a.x) * (c.y - a.y) - (b.y - a.y) * (c.x - a.x) > 0 else { continue }
                reference.move(to: CGPoint(x: a.x * 100, y: a.y * 100))
                reference.addLine(to: CGPoint(x: b.x * 100, y: b.y * 100))
                reference.addLine(to: CGPoint(x: c.x * 100, y: c.y * 100))
                reference.closeSubpath()
            }
            for y in stride(from: -140.3, through: 140, by: 11.1) {
                for x in stride(from: -140.7, through: 140, by: 11.1) {
                    let point = CGPoint(x: x, y: y)
                    #expect(path.contains(point) == reference.contains(point))
                }
            }
            #expect(layers.allSatisfy { $0.outline?.isEmpty == true }, "Materials must not stroke internal mesh edges")
        }
    }
}
