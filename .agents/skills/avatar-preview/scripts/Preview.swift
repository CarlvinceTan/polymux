import AppKit
@testable import AvatarKit
import ImageIO
import Metal
import QuartzCore
import SwiftUI
import UniformTypeIdentifiers

private enum Section: String, CaseIterable {
    case moods, states, expressions, interactions, props
}

private struct Settings {
    var shapes: [ShapeID] = []
    var sections = Section.allCases
    var moods = MoodID.allCases
    var size = 768
    var fps = 25
    var dark = false
    var color = ColorID.ink
    var seconds: Double?
    var output = URL(fileURLWithPath: "avatar-preview.gif")
    var force = false
    var list = false
    var plan = false
    var check = false

    init(_ arguments: [String]) throws {
        var args = ArraySlice(arguments)
        func value(_ flag: String) throws -> String {
            guard let next = args.popFirst(), !next.hasPrefix("--") else {
                throw Failure("Missing value for \(flag)")
            }
            return next
        }
        while let arg = args.popFirst() {
            switch arg {
            case "--output": output = URL(fileURLWithPath: try value(arg))
            case "--size": size = Int(try value(arg)) ?? 0
            case "--fps": fps = Int(try value(arg)) ?? 0
            case "--seconds": seconds = Double(try value(arg)) ?? 0
            case "--appearance":
                let appearance = try value(arg)
                guard ["light", "dark"].contains(appearance) else { throw Failure("Appearance must be light or dark") }
                dark = appearance == "dark"
            case "--color":
                let name = try value(arg)
                guard let match = ColorID.allCases.first(where: {
                    key($0.rawValue) == key(name) || key($0.avatarLabel) == key(name)
                }) else { throw Failure("Unknown colour: \(name)") }
                color = match.avatarID
            case "--sections":
                sections = try value(arg).split(separator: ",").map {
                    guard let section = Section(rawValue: String($0)) else { throw Failure("Unknown section: \($0)") }
                    return section
                }
            case "--moods":
                moods = try value(arg).split(separator: ",").map {
                    guard let mood = MoodID(rawValue: String($0)) else { throw Failure("Unknown mood: \($0)") }
                    return mood
                }
            case "--force": force = true
            case "--list": list = true
            case "--plan": plan = true
            case "--check": check = true
            default:
                guard !arg.hasPrefix("-"), let shape = ShapeID.allCases.first(where: {
                    key($0.rawValue) == key(arg) || key($0.label) == key(arg)
                }) else { throw Failure("Unknown avatar or option: \(arg). Use --list for available names.") }
                shapes.append(shape)
            }
        }
        guard (256...2048).contains(size) else { throw Failure("Size must be 256–2048 pixels per avatar") }
        guard [20, 25, 50].contains(fps) else { throw Failure("FPS must be 20, 25 or 50 for accurate GIF timing") }
        if let seconds, !seconds.isFinite || seconds <= 0 || seconds > 60 { throw Failure("Seconds must be greater than 0 and at most 60") }
        guard !sections.isEmpty, !moods.isEmpty else { throw Failure("Choose at least one section and mood") }
        guard output.pathExtension.lowercased() == "gif" else { throw Failure("Output must end in .gif") }
        guard list || check || (1...9).contains(shapes.count) else { throw Failure("Choose 1–9 avatars") }
        guard width * height <= 16_777_216 else { throw Failure("The combined image is too large; reduce --size or split the avatars") }
    }

    var columns: Int { Int(ceil(sqrt(Double(max(1, shapes.count))))) }
    var rows: Int { (max(1, shapes.count) + columns - 1) / columns }
    var width: Int { columns * size }
    var height: Int { rows * size }
    var manifest: URL { output.deletingPathExtension().appendingPathExtension("json") }
}

private func key(_ string: String) -> String { string.lowercased().filter { $0.isLetter || $0.isNumber } }
private struct Failure: LocalizedError {
    let message: String
    init(_ message: String) { self.message = message }
    var errorDescription: String? { message }
}

private struct Scene {
    var section: Section
    var name: String
    var duration: Double
    var state = StateID.idle
    var expression = ExpressionID.neutral
    var mood: MoodID?
    var prop: WorkProp?
    var moves: [Poke] = []

    func frames(_ settings: Settings) -> Int { max(1, Int(ceil((settings.seconds ?? duration) * Double(settings.fps)))) }
}

@main @MainActor
private struct Preview {
    static func main() {
        do {
            let settings = try Settings(Array(CommandLine.arguments.dropFirst()))
            if settings.list { try printJSON(catalog()); return }
            if settings.check { try check(); return }
            if settings.plan { try printJSON(manifest(settings)); return }
            try render(settings)
            print(settings.output.path)
        } catch {
            log("Avatar preview: \(error.localizedDescription)")
            exit(1)
        }
    }

    static func scenes(_ settings: Settings) -> [Scene] {
        settings.sections.flatMap { section -> [Scene] in
            switch section {
            case .moods:
                return settings.moods.map {
                    Scene(section: section, name: $0.rawValue,
                          duration: ($0.clips.map(\.duration).max() ?? 6) + 0.7, mood: $0,
                          moves: $0 == .celebrate ? [.spin(turns: 1), .bounce] : $0 == .surprised ? [.bounce] : [])
                }
            case .states:
                return STATES.map { Scene(section: section, name: $0.id.rawValue,
                                          duration: $0.duration + $0.morph + 0.4, state: $0.id) }
            case .expressions:
                return ExpressionID.allCases.map { Scene(section: section, name: $0.label,
                                                          duration: 1.6, expression: $0) }
            case .interactions:
                return [("spin", Poke.spin(turns: 1)), ("bounce", .bounce), ("nod", .nod)].map {
                    Scene(section: section, name: $0.0, duration: $0.1.duration + 1, mood: .idle, moves: [$0.1])
                }
            case .props:
                return WorkProp.allCases.map { Scene(section: section, name: $0.rawValue,
                                                      duration: 5.5, mood: .working, prop: $0) }
            }
        }
    }

    static func catalog() -> [String: Any] {
        ["avatars": ShapeID.allCases.map { ["id": $0.rawValue, "name": $0.label] },
         "moods": MoodID.allCases.map(\.rawValue), "states": StateID.allCases.map(\.rawValue),
         "expressions": ExpressionID.allCases.map(\.label), "props": WorkProp.allCases.map(\.rawValue),
         "sections": Section.allCases.map(\.rawValue)]
    }

    static func manifest(_ settings: Settings) -> [String: Any] {
        var start = 0
        let timeline: [[String: Any]] = scenes(settings).map { scene in
            let count = scene.frames(settings)
            defer { start += count }
            return ["section": scene.section.rawValue, "name": scene.name, "startFrame": start,
                    "frames": count, "start": Double(start) / Double(settings.fps),
                    "duration": Double(count) / Double(settings.fps)]
        }
        return ["avatars": settings.shapes.map { ["id": $0.rawValue, "name": $0.label] },
                "width": settings.width, "height": settings.height, "tileSize": settings.size,
                "fps": settings.fps, "appearance": settings.dark ? "dark" : "light", "frames": start,
                "duration": Double(start) / Double(settings.fps), "timeline": timeline,
                "renderer": ["apps/Polymux/Sources/AvatarUI/AvatarCanvas.swift",
                             "apps/Polymux/Sources/AvatarUI/AvatarLayers.swift",
                             "apps/Polymux/Sources/AvatarUI/AvatarView.swift", "AvatarKit"]]
    }

    static func input(_ shape: ShapeID, scene: Scene, index: Int, settings: Settings) -> AvatarInput {
        var options = AvatarOptions()
        options.shape = shape
        options.color = settings.color
        options.expression = scene.expression
        options.followCursor = false
        let scheme: ColorScheme = settings.dark ? .dark : .light
        let character = shape.category != .shapes ? shape.character : nil
        let ink = character.map { Color(avatarHex: $0.color) } ?? options.color.avatarColor(scheme)
        let mark = character.map { Color(avatarHex: legible($0.color, on: settings.dark ? "#1E1E1E" : "#FFFFFF")) } ?? ink
        let backing = Color(nsColor: .windowBackgroundColor)
        let eye = character.map { Color(avatarHex: $0.eyeColor) }
            ?? (scene.mood == .blocked ? options.color.eyeColor(scheme) : backing)
        return AvatarInput(options: options, state: scene.state, expression: scene.expression, playing: true,
                           frozenAt: nil, timeline: nil, time: nil, repeating: false, cueID: "preview-\(index)",
                           mood: scene.mood, interactive: false, personality: "avatar-preview", showsProps: false,
                           prop: scene.prop, reduceMotion: false, running: true, settles: true, gazing: false,
                           ink: AvatarCanvasState.resolve(ink, dark: settings.dark),
                           markInk: AvatarCanvasState.resolve(mark, dark: settings.dark),
                           backing: AvatarCanvasState.resolve(backing, dark: settings.dark),
                           eye: AvatarCanvasState.resolve(eye, dark: settings.dark), dark: settings.dark)
    }

    static func png(_ image: CGImage, at url: URL) throws {
        guard let destination = CGImageDestinationCreateWithURL(url as CFURL, UTType.png.identifier as CFString, 1, nil)
        else { throw Failure("Could not create a preview frame") }
        CGImageDestinationAddImage(destination, image, nil)
        guard CGImageDestinationFinalize(destination) else { throw Failure("Could not save a preview frame") }
    }

    static func render(_ settings: Settings) throws {
        let manager = FileManager.default
        for url in [settings.output, settings.manifest] where manager.fileExists(atPath: url.path) && !settings.force {
            throw Failure("\(url.lastPathComponent) already exists; choose another --output or use --force")
        }
        let folder = URL(fileURLWithPath: CommandLine.arguments[0]).deletingLastPathComponent()
            .appendingPathComponent("frames-\(UUID().uuidString)")
        try manager.createDirectory(at: folder, withIntermediateDirectories: true)
        defer { try? manager.removeItem(at: folder) }
        let canvases = settings.shapes.map { _ in AvatarCanvasState(motion: AvatarMotion()) }
        let tiles = canvases.map { _ in AvatarLayers() }
        let compositor = try Compositor(width: settings.width, height: settings.height)
        compositor.root.sublayers = tiles.map(\.root)
        let size = CGSize(width: settings.size, height: settings.size)
        var frameIndex = 0
        for (index, scene) in scenes(settings).enumerated() {
            log("\(scene.section.rawValue): \(scene.name)")
            for (shape, canvas) in zip(settings.shapes, canvases) {
                canvas.input = input(shape, scene: scene, index: index, settings: settings)
                canvas.motion.queue(scene.moves)
            }
            for _ in 0..<scene.frames(settings) {
                try autoreleasepool {
                    let date = Date(timeIntervalSince1970: 1_700_000_000 + Double(frameIndex) / Double(settings.fps))
                    CATransaction.begin()
                    CATransaction.setDisableActions(true)
                    compositor.root.backgroundColor = canvases[0].input!.backing
                    for (tile, (canvas, layers)) in zip(canvases, tiles).enumerated() {
                        canvas.update(date, size: size)
                        let origin = CGPoint(x: tile % settings.columns * settings.size, y: tile / settings.columns * settings.size)
                        layers.apply(canvas, in: CGRect(origin: origin, size: size), scale: 1)
                    }
                    CATransaction.commit()
                    try png(try compositor.image(), at: folder.appendingPathComponent(String(format: "%06d.png", frameIndex)))
                }
                frameIndex += 1
            }
        }
        let pattern = folder.appendingPathComponent("%06d.png").path
        let palette = folder.appendingPathComponent("palette.png").path
        let movie = folder.appendingPathComponent("preview.gif")
        let source = ["-framerate", String(settings.fps), "-i", pattern]
        try ffmpeg(source + ["-vf", "palettegen=stats_mode=diff", "-frames:v", "1", "-y", palette])
        try ffmpeg(source + ["-i", palette, "-lavfi", "paletteuse=dither=sierra2_4a:diff_mode=rectangle",
                             "-loop", "0", "-y", movie.path])
        let data = try JSONSerialization.data(withJSONObject: manifest(settings), options: [.prettyPrinted, .sortedKeys])
        try manager.createDirectory(at: settings.output.deletingLastPathComponent(), withIntermediateDirectories: true)
        if settings.force, manager.fileExists(atPath: settings.output.path) { try manager.removeItem(at: settings.output) }
        try manager.moveItem(at: movie, to: settings.output)
        try data.write(to: settings.manifest, options: .atomic)
    }

    static func ffmpeg(_ arguments: [String]) throws {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/env")
        process.arguments = ["ffmpeg", "-hide_banner", "-loglevel", "error", "-nostdin"] + arguments
        try process.run()
        process.waitUntilExit()
        guard process.terminationStatus == 0 else { throw Failure("GIF encoding failed; check that ffmpeg is installed") }
    }

    static func printJSON(_ value: [String: Any]) throws {
        let data = try JSONSerialization.data(withJSONObject: value, options: [.prettyPrinted, .sortedKeys])
        FileHandle.standardOutput.write(data)
        print()
    }

    static func log(_ text: String) { FileHandle.standardError.write(Data((text + "\n").utf8)) }

    static func check() throws {
        func pixels(_ image: CGImage) throws -> Data {
            guard let context = CGContext(data: nil, width: image.width, height: image.height, bitsPerComponent: 8,
                                          bytesPerRow: image.width * 4, space: CGColorSpace(name: CGColorSpace.sRGB)!,
                                          bitmapInfo: CGBitmapInfo.byteOrder32Big.rawValue
                                              | CGImageAlphaInfo.premultipliedLast.rawValue)
            else { throw Failure("Could not decode a preview frame") }
            context.draw(image, in: CGRect(x: 0, y: 0, width: image.width, height: image.height))
            return Data(bytes: context.data!, count: context.bytesPerRow * image.height)
        }
        let folder = URL(fileURLWithPath: CommandLine.arguments[0]).deletingLastPathComponent()
            .appendingPathComponent("checks-\(UUID().uuidString)")
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: folder) }
        let compositor = try Compositor(width: 16, height: 24)
        compositor.root.backgroundColor = CGColor(gray: 1, alpha: 1)
        let marker = CALayer()
        marker.frame = CGRect(x: 0, y: 0, width: 8, height: 8)
        marker.backgroundColor = CGColor(red: 1, green: 0, blue: 0, alpha: 1)
        compositor.root.addSublayer(marker)
        let oriented = try pixels(compositor.image())
        precondition(oriented[(2 * 16 + 2) * 4 + 1] < 100 && oriented[(20 * 16 + 2) * 4 + 1] > 240,
                     "The compositor inverted the image")
        let complete = scenes(try Settings(["Balloon"]))
        precondition(complete.filter { $0.section == .moods }.compactMap(\.mood) == MoodID.allCases)
        precondition(complete.filter { $0.section == .states }.map(\.state) == STATES.map(\.id))
        precondition(complete.filter { $0.section == .expressions }.map(\.expression) == ExpressionID.allCases)
        precondition(complete.filter { $0.section == .props }.compactMap(\.prop) == WorkProp.allCases)
        precondition(complete.filter { $0.section == .interactions }.count == 3)
        let named = try Settings(["Balloon"]), stored = try Settings(["pinecone"])
        precondition(named.shapes == [.balloon] && stored.shapes == [.balloon])
        for args in [["missing-avatar"], ["balloon", "--moods", "missing"], ["balloon", "--fps", "10"], ["balloon", "--seconds", "nan"]] {
            precondition((try? Settings(args)) == nil, "Invalid arguments were accepted")
        }
        for appearance in ["light", "dark"] {
            let settings = try Settings(["balloon", "circle", "cupcake", "--size", "256", "--fps", "20", "--seconds", "0.6",
                                         "--sections", "moods", "--moods", "happy,thinking", "--appearance", appearance,
                                         "--output", folder.appendingPathComponent("\(appearance).gif").path])
            try render(settings)
            guard let source = CGImageSourceCreateWithURL(settings.output as CFURL, nil),
                  let first = CGImageSourceCreateImageAtIndex(source, 0, nil),
                  let last = CGImageSourceCreateImageAtIndex(source, 23, nil) else { throw Failure("GIF cannot be decoded") }
            precondition(CGImageSourceGetCount(source) == 24 && first.width == 512 && first.height == 512)
            let a = try pixels(first), b = try pixels(last)
            for cell in 0..<3 {
                let left = cell % 2 * 256, top = cell / 2 * 256
                var visible = 0, changed = 0
                for y in top..<(top + 256) { for x in left..<(left + 256) {
                    let p = (y * 512 + x) * 4
                    if a[p] != a[0] || a[p + 1] != a[1] || a[p + 2] != a[2] { visible += 1 }
                    if a[p] != b[p] || a[p + 1] != b[p + 1] || a[p + 2] != b[p + 2] { changed += 1 }
                } }
                precondition(visible > 100 && changed > 100, "A grid cell is blank or does not animate")
            }
            precondition(appearance == "light" ? a[0] > 180 : a[0] < 100)
            let properties = CGImageSourceCopyProperties(source, nil) as? [String: Any]
            let gif = properties?[kCGImagePropertyGIFDictionary as String] as? [String: Any]
            precondition(gif?[kCGImagePropertyGIFLoopCount as String] as? Int == 0)
            let frame = CGImageSourceCopyPropertiesAtIndex(source, 0, nil) as? [String: Any]
            let timing = frame?[kCGImagePropertyGIFDictionary as String] as? [String: Any]
            precondition(abs((timing?[kCGImagePropertyGIFUnclampedDelayTime as String] as? Double ?? 0) - 0.05) < 0.001)
            do { try render(settings); throw Failure("An existing preview was overwritten") }
            catch let error as Failure { precondition(error.message.contains("already exists")) }
        }
        print("Avatar preview checks passed: complete coverage, argument validation, orientation, light/dark grids, animation, looping, timing and overwrite protection")
    }
}

/// Draws the avatar layers offscreen with Core Animation, the compositor the app itself uses.
@MainActor
private final class Compositor {
    let root = CALayer()
    private let renderer: CARenderer
    private let texture: MTLTexture
    private let queue: MTLCommandQueue

    init(width: Int, height: Int) throws {
        guard let device = MTLCreateSystemDefaultDevice(), let queue = device.makeCommandQueue() else {
            throw Failure("Metal is unavailable for the preview compositor")
        }
        let descriptor = MTLTextureDescriptor.texture2DDescriptor(pixelFormat: .bgra8Unorm, width: width, height: height,
                                                                  mipmapped: false)
        descriptor.usage = [.renderTarget, .shaderRead]
        descriptor.storageMode = .shared
        guard let texture = device.makeTexture(descriptor: descriptor) else { throw Failure("Could not allocate the preview image") }
        self.queue = queue
        self.texture = texture
        renderer = CARenderer(mtlTexture: texture, options: [kCARendererMetalCommandQueue: queue,
                                                              kCARendererColorSpace: CGColorSpace(name: CGColorSpace.sRGB)!])
        let bounds = CGRect(x: 0, y: 0, width: width, height: height)
        renderer.bounds = bounds
        root.frame = bounds
        root.isGeometryFlipped = true
        renderer.layer = root
    }

    func image() throws -> CGImage {
        CATransaction.flush()
        renderer.beginFrame(atTime: CACurrentMediaTime(), timeStamp: nil)
        renderer.addUpdate(renderer.bounds)
        renderer.render()
        renderer.endFrame()
        // The renderer encodes on this queue, so an empty buffer completes only after the frame has.
        guard let fence = queue.makeCommandBuffer() else { throw Failure("Could not finish a preview frame") }
        fence.commit()
        fence.waitUntilCompleted()
        guard fence.status == .completed else { throw Failure("Could not finish a preview frame") }
        let width = texture.width, height = texture.height
        var bytes = [UInt8](repeating: 0, count: width * height * 4)
        texture.getBytes(&bytes, bytesPerRow: width * 4, from: MTLRegionMake2D(0, 0, width, height), mipmapLevel: 0)
        // CARenderer's texture is bottom-up; image scanlines are top-down.
        var pixels = Data(count: bytes.count)
        bytes.withUnsafeBytes { source in
            pixels.withUnsafeMutableBytes { target in
                for y in 0..<height {
                    target.baseAddress!.advanced(by: y * width * 4).copyMemory(
                        from: source.baseAddress!.advanced(by: (height - y - 1) * width * 4), byteCount: width * 4)
                }
            }
        }
        guard let provider = CGDataProvider(data: pixels as CFData),
              let image = CGImage(width: width, height: height, bitsPerComponent: 8, bitsPerPixel: 32, bytesPerRow: width * 4,
                                  space: CGColorSpace(name: CGColorSpace.sRGB)!,
                                  bitmapInfo: CGBitmapInfo(rawValue: CGBitmapInfo.byteOrder32Little.rawValue
                                      | CGImageAlphaInfo.premultipliedFirst.rawValue),
                                  provider: provider, decode: nil, shouldInterpolate: false, intent: .defaultIntent)
        else { throw Failure("Could not draw a preview frame") }
        return image
    }
}
