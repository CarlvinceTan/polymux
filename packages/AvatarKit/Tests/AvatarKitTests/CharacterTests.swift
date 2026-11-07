import CoreGraphics
import Foundation
import Testing
@testable import AvatarKit

struct CharacterTests {
    // Grok Bot 0.75.3's Pd outline, made squatter with deeper lobes in Pumpkin.radial; y-up source reversed below.
    @Test func pumpkinMatchesReference() {
        let samples: [(Double, [Double])] = [
            (0, [1.0377634576807664, 0.858342548273498, 0.6283066909787544, 0.8583425482734983,
                 1.0377634576807664, 0.8482272042521175, 0.6484457850456238, 0.8482272042521175]),
            (35, [1.0503769667591045, 0.8619585293971307, 0.6374678588233011, 0.8619585293971309,
                  1.0503769667591047, 0.8525055989251684, 0.6604562141409035, 0.8525055989251684])
        ]
        for (yaw, expected) in samples {
            let points = solidOutline(Silhouette(radii: ShapeID.pumpkin.radii), ShapeID.pumpkin.solid,
                                      Rotation(yaw: yaw, pitch: 0), scale: 1)
            for index in 0..<8 {
                let point = points[index * 16]
                #expect(abs(hypot(point.x, point.y) - expected[(8 - index) % 8]) < 1e-6)
            }
        }
        #expect(ShapeID.pumpkin.character?.parts.isEmpty == true)
    }

    @Test func categoriesPartitionThePicker() {
        let shapes = ShapeCategory.allCases.flatMap(\.shapes)
        #expect(Set(shapes) == Set(ShapeID.creationShapes))
        #expect(shapes.count == Set(shapes).count)
        #expect(ShapeCategory.allCases == [.shapes, .food, .animals, .nature, .objects])
        #expect(ShapeCategory.food.shapes == [.pumpkin, .apple, .pear, .strawberry, .bun, .toast, .cupcake, .popcorn, .peanut,
                                             .hamburger, .carrot, .pizza, .sushi, .iceCream,
                                             .lemon, .orangeFruit, .watermelon, .avocado, .cherry, .cookie, .cheese, .egg, .candy, .donut])
        #expect(ShapeCategory.shapes.shapes.count == 14)
        #expect(ShapeCategory.food.shapes.count == 24)
        #expect(ShapeCategory.nature.shapes.count == 23)
        #expect(ShapeCategory.objects.shapes.count == 25)
        #expect(ShapeCategory.animals.shapes == [
            .cat, .dog, .panda, .rabbit, .fox, .crab, .elephant, .octopus, .pig, .deer, .koala,
            .giraffe, .cow, .turtle, .tiger, .frog, .mouse, .walrus, .monkey, .penguin, .bat, .axolotl, .whale, .bull
        ])
    }

    @Test func savedDesignsUseReplacements() throws {
        let replacements: [String: ShapeID] = [
            "bear": .panda, "hamster": .crab, "capybara": .elephant, "otter": .octopus,
            "alpaca": .giraffe, "seal": .walrus, "squirrel": .turtle, "owl": .bat,
            "coral": .palm, "crystal": .snowman, "comet": .lightning, "cushion": .bulb,
            "paintbrush": .hammer, "backpack": .wateringCan, "headphones": .gamepad,
            "sheep": .koala, "lion": .frog, "parrot": .axolotl,
            "banana": .hamburger, "hedgehog": .mouse, "gumdrop": .popcorn, "rose": .sunflower, "pinecone": .balloon
        ]
        for (stored, replacement) in replacements {
            let data = try JSONEncoder().encode(stored)
            let shape = try JSONDecoder().decode(ShapeID.self, from: data)
            #expect(shape == replacement)
            #expect(try JSONEncoder().encode(shape) == data)
            #expect(ShapeID.creationShapes.contains(shape))
            #expect(shape.character != nil)
        }
    }

    @Test func muzzleKeepsNoseVisible() throws {
        let model = try #require(ShapeID.cat.character)
        for rotation in [Rotation.rest, .identity, Rotation(yaw: -55, pitch: 15), Rotation(yaw: 55, pitch: -15)] {
            let face = surface(model.solid, 0, 0.26, model.radii)
            let center = rotation.apply(Vec3(x: 0, y: 0.26, z: face.z + 0.004))
            let point = CGPoint(x: center.x * 100, y: center.y * 100)
            let layers = model.layers(Silhouette(radii: model.radii), rotation: rotation, scale: 100)
            #expect(layers.last { $0.front && $0.path.contains(point) }?.color == "#9B5E6D",
                    "Cat's muzzle covers its nose")
        }
    }

    @Test(arguments: [ShapeID.panda, .koala, .balloon])
    func eyesContrastWithFaces(_ shape: ShapeID) throws {
        let model = try #require(shape.character)
        func luminance(_ hex: String) -> Double {
            let value = UInt32(hex.dropFirst(), radix: 16)!
            let channels = [16, 8, 0].map { shift in
                let channel = Double((value >> shift) & 255) / 255
                return channel <= 0.04045 ? channel / 12.92 : pow((channel + 0.055) / 1.055, 2.4)
            }
            return zip(channels, [0.2126, 0.7152, 0.0722]).reduce(0) { $0 + $1.0 * $1.1 }
        }
        for yaw in [-40.0, 0, 40] {
            let engine = BotEngine(shape: shape.radii)
            engine.solid = shape.solid
            engine.character = model
            engine.setTurn(Turn(yaw: yaw), 0)
            let frame = engine.sample(1)
            let eyes = frame.eyes.filter { $0.alpha > 0.95 }
            #expect(!eyes.isEmpty)
            for eye in eyes {
                let point = CGPoint(x: eye.matrix.tx, y: eye.matrix.ty)
                let face = frame.parts.last { $0.front && $0.path.contains(point) }?.color ?? model.color
                let a = luminance(model.eyeColor), b = luminance(face)
                #expect((max(a, b) + 0.05) / (min(a, b) + 0.05) >= 4.5,
                        "\(shape.label)'s eyes disappear into its face at yaw \(yaw)")
            }
        }
    }

    @Test func frostingHasNoGapsBetweenCoils() {
        for yaw in [-70.0, -35, 0, 35, 70, 180] {
            let rotation = Rotation(yaw: yaw, pitch: 15)
            let layers = Cupcake.layers(Silhouette(radii: ShapeID.cupcake.radii), rotation: rotation, scale: 100)
            for step in 0..<40 {
                let t = Double(step) / 40
                let center = rotation.apply(Vec3(x: 0.09 * pow(t, 6),
                    y: -0.54 - 0.82 * (1.7 * t - 0.7 * t * t), z: 0))
                let point = CGPoint(x: center.x * 100, y: center.y * 100)
                #expect(layers.contains { $0.path.contains(point) }, "Frosting gap at \(yaw), \(t)")
            }
        }
    }

    @Test func frostingMeetsCake() {
        for rotation in [Rotation.identity, Rotation(yaw: -35, pitch: 0), Rotation(yaw: 35, pitch: 0), .rest,
                         Rotation(yaw: 0, pitch: 15) * .rest] {
            let layers = Cupcake.layers(Silhouette(radii: ShapeID.cupcake.radii), rotation: rotation, scale: 100)
            // The band between the frosting's spiral base and the cake top.
            let seam = [-0.3, -0.25].flatMap { y in
                (0..<16).map { Vec3(x: cos(Double($0) * TAU / 16) * 0.6, y: y, z: sin(Double($0) * TAU / 16) * 0.6) }
            }
            for sample in seam {
                let center = rotation.apply(sample)
                let point = CGPoint(x: center.x * 100, y: center.y * 100)
                #expect(layers.contains { $0.path.contains(point) }, "Frosting gap at \(rotation), \(sample)")
            }
        }
    }

    @Test func frostingClearsLiner() {
        for rotation in [Rotation.identity, .rest, Rotation(yaw: -50, pitch: 0) * .rest,
                         Rotation(yaw: 35, pitch: 0) * .rest, Rotation(yaw: 0, pitch: -40) * .rest] {
            let radii = ShapeID.cupcake.radii
            let layers = Cupcake.layers(Silhouette(radii: radii), rotation: rotation, scale: 100)
            for x in [-0.4, 0, 0.4] {
                for y in [-0.12, 0.1, 0.4] {
                    let face = surface(ShapeID.cupcake.solid, x, y, radii)
                    guard rotation.apply(face.normal).z > 0.25 else { continue }
                    let point = rotation.apply(Vec3(x: x, y: y, z: face.z))
                    let screen = CGPoint(x: point.x * 100, y: point.y * 100)
                    #expect(!layers.contains { $0.front && $0.path.contains(screen) }, "Topping covers the liner at \(rotation), \(x), \(y)")
                }
            }
        }
    }

    @Test func sideContoursStayRound() {
        let silhouette = Silhouette(radii: superellipseProfile(2, 0.83, 0.77))
        let outline = solidOutline(silhouette, .inflated(depth: 0.65), Rotation(yaw: 90, pitch: 0),
                                   scale: 1, smooth: true)
        for (index, point) in outline.enumerated() {
            let angle = Double(index) * TAU / Double(outline.count)
            let expected = 1 / sqrt(pow(cos(angle) / 0.65, 2) + pow(sin(angle) / 0.77, 2))
            #expect(abs(hypot(point.x, point.y) - expected) < 0.003)
        }
    }

    @Test(arguments: ShapeID.creationShapes.filter { $0.category != .shapes })
    func appendagesStayAttached(_ shape: ShapeID) {
        // Mesh bodies have source-defined topology (including separated blossom petals), not appendages.
        guard shape.character?.mesh == nil else { return }
        for yaw in [-60.0, 0, 55, 155] {
            let engine = BotEngine(shape: shape.radii)
            engine.solid = shape.solid
            engine.character = shape.character
            engine.setTurn(Turn(yaw: yaw), 0)
            let frame = engine.sample(1)
            let size = 160
            var pixels = [UInt8](repeating: 0, count: size * size)
            pixels.withUnsafeMutableBytes { bytes in
                let context = CGContext(data: bytes.baseAddress, width: size, height: size,
                                        bitsPerComponent: 8, bytesPerRow: size,
                                        space: CGColorSpaceCreateDeviceGray(), bitmapInfo: 0)!
                context.translateBy(x: 80, y: 80)
                context.scaleBy(x: 0.5, y: 0.5)
                context.setFillColor(gray: 1, alpha: 1)
                context.addPath(frame.bodyPath)
                context.fillPath()
                for part in frame.parts where !part.surface {
                    context.addPath(part.path)
                    context.fillPath()
                }
            }
            var components = 0
            for seed in pixels.indices where pixels[seed] > 127 {
                var queue = [seed], cursor = 0
                pixels[seed] = 0
                while cursor < queue.count {
                    let index = queue[cursor]
                    cursor += 1
                    for next in [index - 1, index + 1, index - size, index + size] {
                        guard pixels.indices.contains(next), pixels[next] > 127 else { continue }
                        pixels[next] = 0
                        queue.append(next)
                    }
                }
                if queue.count > 2 { components += 1 }
            }
            #expect(components == 1, "\(shape.label) has a detached part at yaw \(yaw)")
        }
    }

    @Test(arguments: ShapeID.creationShapes.filter { $0.category != .shapes })
    func charactersTurnAndExpress(_ shape: ShapeID) throws {
        let model = try #require(shape.character)
        #expect(model.radii.count == PROFILE_SAMPLES)
        #expect(model.radii.allSatisfy { $0.isFinite && $0 > 0 })
        for part in model.parts {
            #expect(part.radii.count == PROFILE_SAMPLES)
            #expect(part.radii.allSatisfy { $0.isFinite && $0 > 0 })
        }
        for expression in ExpressionID.allCases {
            let engine = BotEngine(shape: shape.radii, expression: expression.expression)
            engine.solid = shape.solid
            engine.character = model
            for yaw in [-75.0, 0, 75, 180] {
                engine.setTurn(Turn(yaw: yaw), 0)
                let frame = engine.sample(1)
                if yaw == 0 && model.mesh == nil { #expect(!frame.parts.isEmpty) }
                #expect(!frame.bodyPath.isEmpty)
                for part in frame.parts {
                    let box = part.path.boundingBoxOfPath
                    #expect(!box.isNull && !box.isInfinite)
                    #expect(box.width > 0 && box.height > 0)
                    #expect(box.minX > -180 && box.maxX < 180)
                    #expect(box.minY > -180 && box.maxY < 180)
                }
            }
        }
        let engine = BotEngine(shape: shape.radii)
        engine.solid = shape.solid
        engine.character = model
        let rest = engine.sample(1)
        if model.mesh == nil { #expect(!rest.parts.isEmpty) }
        engine.setTurn(Turn(yaw: 70), 1)
        let turned = engine.sample(2)
        if model.mesh == nil { #expect(rest.parts.first?.path != turned.parts.first?.path) }
        else { #expect(rest.bodyPath != turned.bodyPath) }
        engine.reducedMotion = true
        engine.setMood(.thinking)
        _ = engine.sample(3)
        let thinking = engine.sample(4)
        #expect(thinking.parts.allSatisfy { $0.alpha < 0.01 })
    }
}
