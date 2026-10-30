import CoreGraphics
import Foundation
import Testing
@testable import AvatarKit

struct MascotTests {
    @Test(arguments: Array(MASCOTS.keys))
    func matchesDesign(_ shape: ShapeID) {
        let mesh = MASCOTS[shape]!
        // Source order is Rz(10) * Rx(16) * Ry(34), conjugated into y-down coordinates.
        let rotation = Rotation(yaw: 0, pitch: 0, roll: -10) * Rotation(yaw: 34, pitch: 16)
        let path = mesh.render(Silhouette(radii: mesh.radii), rotation: rotation, scale: 1).0
        let expected = [UInt8](Data(base64Encoded: MASCOT_REFERENCE[shape]!)!)
        var mismatches = 0
        for y in 0..<48 {
            for x in 0..<48 {
                let point = CGPoint(x: -1.6 + (Double(x) + 0.5) / 48 * 3.2,
                                    y: -1.6 + (Double(y) + 0.5) / 48 * 3.2)
                if path.contains(point) != (expected[y * 48 + x] == 1) { mismatches += 1 }
            }
        }
        #expect(mismatches <= 12, "\(shape.label): \(mismatches) / 2304 reference pixels differ")
    }

    /// A backwards triangle is culled as a back face and leaves a hole where parts meet.
    @Test(arguments: Array(MASCOTS.keys))
    func facesWindOutward(_ shape: ShapeID) {
        let mesh = MASCOTS[shape]!
        var welded: [String: Int] = [:]
        let ids = mesh.points.map { p in
            let key = "\(Int((p.x * 10000).rounded())),\(Int((p.y * 10000).rounded())),\(Int((p.z * 10000).rounded()))"
            if let id = welded[key] { return id }
            welded[key] = welded.count
            return welded.count - 1
        }
        var edges: [Int: Int] = [:]
        for face in mesh.faces {
            let v = face.prefix(3).map { ids[$0] }
            if Set(v).count < 3 { continue }
            for i in 0..<3 { edges[v[i] << 32 | v[(i + 1) % 3], default: 0] += 1 }
        }
        let flipped = edges.filter { edge, count in count > 1 && edges[(edge & 0xFFFFFFFF) << 32 | edge >> 32] == nil }
        #expect(flipped.isEmpty, "\(shape.label): \(flipped.count) edges wound backwards")
    }

    @Test func balloonKeepsVolume() {
        let mesh = MASCOTS[.balloon]!, sil = Silhouette(radii: mesh.radii)
        for yaw in [0.0, 45, 90, 180, 270] {
            let (body, _) = mesh.render(sil, rotation: Rotation(yaw: yaw, pitch: 0), scale: 100)
            #expect(body.contains(CGPoint(x: 65, y: -40)), "The envelope flattens when turned")
            #expect(body.contains(CGPoint(x: 0, y: 120)), "The basket disappears when turned")
            #expect(!body.contains(CGPoint(x: 10, y: 85)), "The suspension gap closes")
            #expect(!body.contains(CGPoint(x: 50, y: 120)), "The basket merges into the envelope")
        }
        for pitch in [-90.0, 90] {
            let (body, _) = mesh.render(sil, rotation: Rotation(yaw: 0, pitch: pitch), scale: 100)
            #expect(body.contains(CGPoint(x: 65, y: 0)), "The envelope flattens from above or below")
        }
    }

    @Test func balloonHidesCoveredEyes() {
        let mesh = MASCOTS[.balloon]!, sil = Silhouette(radii: mesh.radii)
        for rotation in [Rotation.identity, .rest] {
            var eyes: [EyePose] = []
            _ = mesh.render(sil, rotation: rotation, scale: 100, eyes: { eyes = $0 })
            #expect(eyes.filter { $0.depth > 0.02 }.count == 2)
        }
        for rotation in [Rotation(yaw: 0, pitch: -90), Rotation(yaw: 140, pitch: 0),
                         Rotation(yaw: 180, pitch: 0), Rotation(yaw: -140, pitch: 0)] {
            var eyes: [EyePose] = []
            _ = mesh.render(sil, rotation: rotation, scale: 100, eyes: { eyes = $0 })
            #expect(eyes.allSatisfy { $0.depth <= 0.02 }, "Eyes show through the balloon")
        }
    }

    @Test func trophyHandlesStayOpen() {
        let mesh = MASCOTS[.trophy]!
        for yaw in [-25.0, 0, 25] {
            let rotation = Rotation(yaw: yaw, pitch: 0)
            let path = mesh.render(Silhouette(radii: mesh.radii), rotation: rotation, scale: 100).0
            for side in [-1.0, 1.0] {
                let hole = rotation.apply(Vec3(x: 0.69 * side, y: -0.38, z: 0))
                let rim = rotation.apply(Vec3(x: 0.85 * side, y: -0.38, z: 0))
                #expect(!path.contains(CGPoint(x: hole.x * 100, y: hole.y * 100)))
                #expect(path.contains(CGPoint(x: rim.x * 100, y: rim.y * 100)))
            }
        }
    }

    /// Paint on a far surface must not show through a nearer one, so a cup's inside shows only from above.
    @Test(arguments: [(ShapeID.trophy, 0.4, "#8A6830"), (.mug, 0.1, "#4F7F88")])
    func cupsHideTheirInside(_ shape: ShapeID, _ y: Double, _ inside: String) {
        let mesh = MASCOTS[shape]!
        func color(_ rotation: Rotation, _ point: Vec3) -> String {
            let (_, layers) = mesh.render(Silhouette(radii: mesh.radii), rotation: rotation, scale: 100)
            let p = rotation.apply(point)
            return layers.last { $0.path.contains(CGPoint(x: p.x * 100, y: p.y * 100)) }?.color ?? mesh.color
        }
        for rotation in [Rotation.rest, .identity, Rotation(yaw: 40, pitch: 0)] {
            #expect(color(rotation, Vec3(x: 0, y: -y, z: 0.62)) != inside, "\(shape.label)'s inside shows through its front")
        }
        #expect(color(Rotation(yaw: 0, pitch: 70), Vec3(x: 0, y: -y, z: 0)) == inside)
    }

    @Test func planetHidesBackRing() {
        let mesh = MASCOTS[.planet]!, ring = mesh.paints.last!.0
        for pitch in [-35.0, -20, 20, 35] {
            for roll in [-25.0, 0, 25] {
                let rotation = Rotation(yaw: 0, pitch: pitch, roll: roll)
                let (_, layers) = mesh.render(Silhouette(radii: mesh.radii), rotation: rotation, scale: 100)
                func color(z: Double) -> String {
                    let point = rotation.apply(Vec3(x: 0, y: 0, z: z))
                    return layers.last { $0.path.contains(CGPoint(x: point.x * 100, y: point.y * 100)) }?.color ?? mesh.color
                }
                #expect(color(z: -0.94) != ring, "Back ring shows through the planet at \(pitch), \(roll)")
                #expect(color(z: 0.94) == ring, "Front ring is hidden at \(pitch), \(roll)")
            }
        }
    }

    @Test func bulbKeepsVolume() {
        let mesh = MASCOTS[.bulb]!
        for yaw in [0.0, 90, 180] {
            let (body, layers) = mesh.render(Silhouette(radii: mesh.radii), rotation: Rotation(yaw: yaw, pitch: 0), scale: 100)
            #expect(body.contains(CGPoint(x: 55, y: -30)), "The globe becomes flat when turned")
            #expect(body.contains(CGPoint(x: 22, y: 75)))
            #expect(!body.contains(CGPoint(x: 50, y: 75)), "The socket merges into the globe")
            let base = CGPoint(x: 0, y: 75)
            #expect(["#81929C", "#C6D0CE"].contains(layers.last { $0.path.contains(base) }?.color ?? ""))
        }
    }

    @Test func palmLeavesCoverTrunk() {
        let mesh = MASCOTS[.palm]!
        for rotation in [Rotation.rest, .identity, Rotation(yaw: -40, pitch: 15), Rotation(yaw: 40, pitch: -15)] {
            let (body, layers) = mesh.render(Silhouette(radii: mesh.radii), rotation: rotation, scale: 100)
            for y in [-0.43, -0.52] {
                let crown = rotation.apply(Vec3(x: 0.04, y: y, z: 0.2))
                let point = CGPoint(x: crown.x * 100, y: crown.y * 100)
                #expect(body.contains(point))
                #expect(["#4D9969", "#337650"].contains(layers.last { $0.path.contains(point) }?.color ?? ""),
                        "The trunk shows through the leaf crown")
            }
        }
    }

    @Test(arguments: [(ShapeID.fox, 0.16, 0.78, "#2B201D"), (.koala, 0.2, 0.62, "#2E2A2F")])
    func noseStaysVisible(_ shape: ShapeID, _ y: Double, _ z: Double, _ color: String) {
        let mesh = MASCOTS[shape]!
        for rotation in [Rotation.rest, .identity, Rotation(yaw: -40, pitch: 15), Rotation(yaw: 40, pitch: -15)] {
            let (_, layers) = mesh.render(Silhouette(radii: mesh.radii), rotation: rotation, scale: 100)
            let nose = rotation.apply(Vec3(x: 0, y: y, z: z))
            let point = CGPoint(x: nose.x * 100, y: nose.y * 100)
            #expect(layers.last { $0.path.contains(point) }?.color == color, "\(shape.label)'s nose is covered")
        }
    }

    @Test func magnetTipsWrapAround() {
        let mesh = MASCOTS[.magnet]!
        for yaw in [0.0, 180] {
            let (body, layers) = mesh.render(Silhouette(radii: mesh.radii), rotation: Rotation(yaw: yaw, pitch: 0), scale: 100)
            for x in [-58.0, 58] {
                let tip = CGPoint(x: x, y: -65)
                #expect(body.contains(tip))
                #expect(layers.last { $0.path.contains(tip) }?.color == "#DCE5EB")
                let stem = CGPoint(x: x, y: -10)
                #expect((layers.last { $0.path.contains(stem) }?.color ?? mesh.color) == mesh.color)
            }
        }
    }

    @Test func redesignedFeaturesStayOpen() {
        let moon = MASCOTS[.moon]!
        let moonPath = moon.render(Silhouette(radii: moon.radii), rotation: Rotation(yaw: 0, pitch: 0), scale: 100).0
        #expect(!moonPath.contains(CGPoint(x: 45, y: -20)))
        #expect(moonPath.contains(CGPoint(x: -65, y: 0)))
        let magnet = MASCOTS[.magnet]!
        let magnetPath = magnet.render(Silhouette(radii: magnet.radii), rotation: Rotation(yaw: 0, pitch: 0), scale: 100).0
        #expect(!magnetPath.contains(CGPoint(x: 0, y: -65)))
        #expect(magnetPath.contains(CGPoint(x: 0, y: 58)))
    }

    @Test func watermelonHasCurvedRind() {
        let mesh = MASCOTS[.watermelon]!
        let (body, layers) = mesh.render(Silhouette(radii: mesh.radii), rotation: .identity, scale: 100)
        func point(_ radius: Double, _ angle: Double) -> CGPoint {
            CGPoint(x: cos(angle) * radius * 100, y: -(0.35 + sin(angle) * radius) * 100)
        }
        func color(_ point: CGPoint) -> String {
            layers.last { $0.path.contains(point) }?.color ?? mesh.color
        }
        for degrees in stride(from: -160.0, through: -20, by: 10) {
            let angle = degrees * .pi / 180
            #expect(body.contains(point(1.05, angle)))
            #expect(!body.contains(point(1.16, angle)))
            #expect(color(point(0.97, angle)) == "#398257")
            #expect(color(point(0.84, angle)) == "#DCE8A5")
        }
        #expect(color(CGPoint(x: 0, y: 15)) == mesh.color)
        #expect(!body.contains(CGPoint(x: 0, y: -55)))
    }

    @Test(arguments: [ShapeID.hamburger, .watermelon, .planet, .palm, .snowman, .lightning, .bulb, .hammer,
                      .wateringCan, .gamepad, .panda, .crab, .elephant, .octopus, .giraffe, .walrus, .turtle, .bat,
                      .koala, .frog, .fox, .trophy, .dog, .rabbit, .penguin, .axolotl, .mouse, .balloon])
    func previewEyesStayVisible(_ shape: ShapeID) {
        let engine = BotEngine(shape: shape.radii)
        engine.solid = shape.solid
        engine.character = shape.character
        _ = engine.sample(0)
        let frame = engine.sample(1)
        func eyePixels(clipped: Bool) -> Int {
            let size = 320
            var pixels = [UInt8](repeating: 0, count: size * size)
            pixels.withUnsafeMutableBytes { bytes in
                let context = CGContext(data: bytes.baseAddress, width: size, height: size,
                                        bitsPerComponent: 8, bytesPerRow: size,
                                        space: CGColorSpaceCreateDeviceGray(), bitmapInfo: 0)!
                context.translateBy(x: 160, y: 160)
                if clipped { context.addPath(frame.bodyPath); context.clip() }
                context.setFillColor(gray: 1, alpha: 1)
                for eye in frame.eyes {
                    context.saveGState()
                    context.concatenate(eye.matrix)
                    context.addPath(eye.d)
                    context.fillPath()
                    context.restoreGState()
                }
            }
            return pixels.filter { $0 > 127 }.count
        }
        let total = eyePixels(clipped: false)
        #expect(total > 0)
        #expect(eyePixels(clipped: true) >= total * 98 / 100, "\(shape.label) clips its eyes in the picker")
    }

    @Test func mugHandleStaysOpen() {
        let mesh = MASCOTS[.mug]!
        for yaw in [-25.0, 0, 25] {
            let rotation = Rotation(yaw: yaw, pitch: 0)
            let path = mesh.render(Silhouette(radii: mesh.radii), rotation: rotation, scale: 100).0
            let hole = rotation.apply(Vec3(x: 0.78, y: 0.03, z: 0))
            let rim = rotation.apply(Vec3(x: 1.03, y: 0.03, z: 0))
            #expect(!path.contains(CGPoint(x: hole.x * 100, y: hole.y * 100)))
            #expect(path.contains(CGPoint(x: rim.x * 100, y: rim.y * 100)))
        }
    }

    @Test func teapotHandleStaysOpen() {
        let mesh = MASCOTS[.teapot]!
        for yaw in [-30.0, 0, 30] {
            let rotation = Rotation(yaw: yaw, pitch: 0)
            let path = mesh.render(Silhouette(radii: mesh.radii), rotation: rotation, scale: 100).0
            let center = rotation.apply(Vec3(x: 0.88, y: 0.02, z: 0))
            let rim = rotation.apply(Vec3(x: 1.04, y: 0.02, z: 0))
            #expect(!path.contains(CGPoint(x: center.x * 100, y: center.y * 100)))
            #expect(path.contains(CGPoint(x: rim.x * 100, y: rim.y * 100)))
        }
    }

    @Test func bellAndConeHaveRoundVolumes() {
        for yaw in [0.0, 90] {
            let rotation = Rotation(yaw: yaw, pitch: 0)
            let bell = MASCOTS[.bell]!
            let bellPath = bell.render(Silhouette(radii: bell.radii), rotation: rotation, scale: 100).0
            #expect(bellPath.contains(CGPoint(x: 70, y: 57)))
            #expect(!bellPath.contains(CGPoint(x: 70, y: -20)))
            #expect(!bellPath.contains(CGPoint(x: 0, y: 80)))
            let cone = MASCOTS[.iceCream]!
            let conePath = cone.render(Silhouette(radii: cone.radii), rotation: rotation, scale: 100).0
            #expect(conePath.contains(CGPoint(x: 26, y: 60)))
            #expect(!conePath.contains(CGPoint(x: 26, y: 95)))
        }
    }

    @Test func iceCreamHasTwoDistinctScoops() {
        let mesh = MASCOTS[.iceCream]!
        let (_, layers) = mesh.render(Silhouette(radii: mesh.radii), rotation: Rotation(yaw: 0, pitch: 0), scale: 100)
        func color(_ point: CGPoint) -> String {
            layers.last { $0.path.contains(point) }?.color ?? mesh.color
        }
        #expect(color(CGPoint(x: 0, y: -65)) == mesh.color)
        #expect(color(CGPoint(x: 0, y: 10)) == "#FFF0CC")
        #expect(color(CGPoint(x: 16, y: -4)) == mesh.color)
    }

    @Test(arguments: Array(MASCOTS.keys))
    func eyesFollowSurface(_ shape: ShapeID) {
        let mesh = MASCOTS[shape]!, sil = Silhouette(radii: shape.radii)
        for yaw in [-45.0, 0, 45] {
            let rotation = Rotation(yaw: yaw, pitch: 0)
            let path = mesh.render(sil, rotation: rotation, scale: 100).0
            for eye in mesh.eyePoses(sil, rotation: rotation, scale: 100) where eye.depth > 0.2 {
                #expect(path.contains(CGPoint(x: eye.x, y: eye.y)), "\(shape.label) eye at \(yaw)")
            }
        }
    }
}
