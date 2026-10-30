import CoreGraphics
import Testing
@testable import AvatarKit

struct ExpressionFitTests {
    // These moods exercise all nine clip faces, including the wide oval and inward-curving eyes.
    private let moods: [MoodID] = [.idle, .happy, .surprised, .sad, .drowsy, .angry, .shy, .curious, .confused]

    @Test(arguments: ShapeID.creationShapes)
    func facesFit(_ shape: ShapeID) {
        for mood in moods {
            let engine = BotEngine(shape: shape.radii)
            engine.solid = shape.solid
            engine.character = shape.character
            engine.resting = true
            engine.setMood(mood)
            _ = engine.sample(0)
            let frame = engine.sample(1)
            let eyes = frame.eyes.map { eye in
                var matrix = eye.matrix
                return eye.d.copy(using: &matrix)!
            }
            #expect(eyes.count == 2, "\(shape.label), \(mood): missing eye")
            let areas = eyes.map { coverage($0) }
            for (index, eye) in eyes.enumerated() {
                #expect(areas[index] > 0)
                // Allow antialiasing and a small amount of natural occlusion at the far edge.
                #expect(coverage(eye, clip: frame.bodyPath) >= areas[index] * 95 / 100,
                        "\(shape.label), \(mood): eye \(index) clips at rest")
            }
            if eyes.count == 2 {
                #expect(coverage(eyes[0], clip: eyes[1]) <= min(areas[0], areas[1]) / 100,
                        "\(shape.label), \(mood): eyes overlap")
            }
        }
    }

    @Test func robotMouthStaysClean() {
        let mesh = MASCOTS[.robot]!
        let (_, layers) = mesh.render(Silhouette(radii: mesh.radii), rotation: .identity, scale: 100)
        func paint(_ x: Double, _ y: Double) -> String {
            layers.last { $0.path.contains(CGPoint(x: x * 100, y: y * 100)) }?.color ?? mesh.color
        }
        for y in [0.28, 0.34, 0.4] {
            for x in [-0.25, -0.21, -0.09, -0.05, 0.05, 0.09, 0.21, 0.25] {
                #expect(paint(x, y) == "#E2EEE8", "A gap splits the robot's mouth")
            }
            for x in [-0.6, -0.4, 0.4, 0.6] {
                #expect(paint(x, y) == mesh.color, "Mouth stripes escape onto the face")
            }
        }
    }

    private func coverage(_ path: CGPath, clip: CGPath? = nil) -> Int {
        let side = 320
        var pixels = [UInt8](repeating: 0, count: side * side)
        pixels.withUnsafeMutableBytes { bytes in
            let context = CGContext(data: bytes.baseAddress, width: side, height: side,
                                    bitsPerComponent: 8, bytesPerRow: side,
                                    space: CGColorSpaceCreateDeviceGray(), bitmapInfo: 0)!
            context.translateBy(x: 160, y: 160)
            if let clip { context.addPath(clip); context.clip() }
            context.setFillColor(gray: 1, alpha: 1)
            context.addPath(path)
            context.fillPath()
        }
        return pixels.reduce(0) { $0 + Int($1) }
    }
}
