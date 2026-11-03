import AppKit
import AvatarKit
import Metal
import QuartzCore

@main
@MainActor
struct RenderChecks {
    static let size = CGSize(width: 200, height: 200)

    static func main() {
        tiled()
        stacks()
        meshStacks()
        gapPlacement()
        rasters()
        updated()
        resized()
        clipping()
        clocks()
        interactions()
        proximity()
        interrupted()
        restyled()
        stateFaces()
        released()
        print("Avatar rendering: placement, stacks, layer reuse, resize, clipping, clocks, proximity, state expressions and teardown passed")
    }

    static func input(_ hex: String = "#E05020") -> AvatarInput {
        let ink = AvatarCanvasState.colour(hex)
        let paper = CGColor(gray: 1, alpha: 1)
        return AvatarInput(options: .default, state: .idle, expression: nil, playing: false,
                           frozenAt: 0, timeline: nil, time: nil, repeating: false, cueID: nil,
                           mood: nil, interactive: false, personality: "test",
                           reduceMotion: false, running: false, settles: false, gazing: false, ink: ink, markInk: ink,
                           backing: paper, eye: paper, dark: false)
    }

    static func slot(_ rect: CGRect, hex: String = "#E05020", group: UUID? = nil,
                     order: Int = 0, gap: CGFloat = 0) -> AvatarSlot {
        AvatarSlot(canvas: AvatarCanvasState(motion: AvatarMotion()), input: input(hex),
                   rect: rect, group: group, order: order, gap: gap)
    }

    static func bitmap(_ root: CALayer) -> Data {
        let device = MTLCreateSystemDefaultDevice()!
        let queue = device.makeCommandQueue()!
        let descriptor = MTLTextureDescriptor.texture2DDescriptor(pixelFormat: .rgba8Unorm, width: Int(size.width),
                                                                  height: Int(size.height), mipmapped: false)
        descriptor.usage = [.renderTarget, .shaderRead]
        descriptor.storageMode = .shared
        let texture = device.makeTexture(descriptor: descriptor)!
        let renderer = CARenderer(mtlTexture: texture, options: [kCARendererMetalCommandQueue: queue])
        renderer.bounds = CGRect(origin: .zero, size: size)
        root.frame = renderer.bounds
        root.isGeometryFlipped = true
        renderer.layer = root
        CATransaction.flush()
        renderer.beginFrame(atTime: CACurrentMediaTime(), timeStamp: nil)
        renderer.addUpdate(renderer.bounds)
        renderer.render()
        renderer.endFrame()
        let fence = queue.makeCommandBuffer()!
        fence.commit()
        fence.waitUntilCompleted()
        precondition(fence.status == .completed, "Avatar compositing failed")
        var bytes = [UInt8](repeating: 0, count: Int(size.width * size.height) * 4)
        texture.getBytes(&bytes, bytesPerRow: Int(size.width) * 4,
                         from: MTLRegionMake2D(0, 0, Int(size.width), Int(size.height)), mipmapLevel: 0)
        return Data(bytes)
    }

    static func batch(_ slots: [AvatarSlot]) -> Data {
        let view = AvatarSurfaceView()
        view.frame = CGRect(origin: .zero, size: size)
        view.update(slots)
        return bitmap(view.layer!)
    }

    static func layer(_ tile: AvatarSlot) -> CALayer {
        tile.canvas.input = tile.input
        tile.canvas.update(Date(timeIntervalSince1970: 0), size: tile.rect.size)
        let layers = AvatarLayers()
        layers.apply(tile.canvas, in: tile.rect, scale: 2)
        return layers.root
    }

    static func rasters() {
        for dark in [false, true] {
            var input = input()
            input.dark = dark
            input.ink = CGColor(gray: dark ? 1 : 0, alpha: 1)
            let tile = AvatarSlot(canvas: AvatarCanvasState(motion: AvatarMotion()), input: input,
                                  rect: CGRect(x: 10, y: 20, width: 60, height: 60))
            let image = AvatarSurfaceView.image([tile], size: size, scale: 2)!
            precondition(image.width == 400 && image.height == 400, "Avatar raster lost its scale")
            let bytes = image.dataProvider!.data! as Data
            var above = 0, below = 0
            for y in 0..<image.height {
                for x in 0..<image.width where bytes[y * image.bytesPerRow + x * 4 + 3] > 0 {
                    if y < image.height / 2 { above += 1 }
                    else { below += 1 }
                }
            }
            precondition(above > 0 && below == 0, "Avatar raster is empty or vertically flipped")
        }
    }

    static func tiled() {
        let tiles = [slot(CGRect(x: 5, y: 10, width: 60, height: 60)),
                     slot(CGRect(x: 80, y: 90, width: 110, height: 110), hex: "#2080D0")]
        let root = CALayer()
        root.sublayers = tiles.map(layer)
        let expected = bitmap(root)
        precondition(batch(tiles) == expected, "Batch placement differs from individual rendering")
        let offscreen = slot(CGRect(x: -200, y: -200, width: 50, height: 50))
        precondition(batch(tiles + [offscreen]) == expected, "Offscreen tiles alter the image")
        precondition(offscreen.canvas.frame == nil, "Offscreen tiles are sampled")
    }

    static func stacks() {
        let id = UUID()
        let back = slot(CGRect(x: 55, y: 55, width: 120, height: 120), group: id, order: 1)
        let front = slot(CGRect(x: 20, y: 20, width: 120, height: 120), group: id, gap: 3)
        let background = slot(CGRect(x: 0, y: 0, width: 200, height: 200), hex: "#2080D0")
        let stacked = batch([background, front, back])
        precondition(stacked == batch([background, back, front]), "Stack order depends on input order")
        var touching = front
        touching.gap = 0
        let gapped = batch([back, front]), joined = batch([back, touching])
        precondition(gapped != joined, "Stack gaps do not erase siblings")
        precondition(batch([background, front]) == batch([background, touching]),
                     "A single visible stack member erases outside its group")
        let paper = batch([background])
        var revealed = 0
        for p in stride(from: 0, to: gapped.count, by: 4) where gapped[p + 3] == 0 && joined[p + 3] == 255 {
            precondition(stacked[p..<(p + 4)] == paper[p..<(p + 4)], "A gap erased another group")
            revealed += 1
        }
        precondition(revealed > 10, "No background was revealed through the stack gap")
    }

    /// Every avatar in front of another: cutting its gap must stay quick, and later updates must cost nothing.
    static func meshStacks() {
        // CI's virtual GPU is far slower at the gap mask, so there one shape is checked, with more time.
        let ci = ProcessInfo.processInfo.environment["CI"] != nil
        let limit = ci ? 20.0 : 2.0
        for shape in ci ? Array(ShapeID.allCases.prefix(1)) : Array(ShapeID.allCases) {
            let start = Date()
            for side in [44.0, 120.0] {
                var choice = input()
                choice.options.shape = shape
                let id = UUID()
                let rect = CGRect(x: 20, y: 20, width: side, height: side)
                let tile = AvatarSlot(canvas: AvatarCanvasState(motion: AvatarMotion()), input: choice,
                                      rect: rect, group: id, gap: 2)
                let behind = slot(rect.offsetBy(dx: side / 4, dy: side / 4), group: id, order: 1)
                let view = AvatarSurfaceView()
                view.frame = CGRect(origin: .zero, size: size)
                view.update([tile, behind])
                view.update([tile, behind])
                precondition(bitmap(view.layer!).contains { $0 != 0 }, "\(shape.label) rendered an empty image")
            }
            precondition(Date().timeIntervalSince(start) < limit, "\(shape.label) stack rendering stalled")
        }
    }

    /// The gap falls where the front avatar is, on all sides, and not mirrored or offset.
    static func gapPlacement() {
        let id = UUID()
        let front = slot(CGRect(x: 30, y: 20, width: 100, height: 100), group: id, gap: 6)
        let back = slot(CGRect(x: 30, y: 20, width: 140, height: 170), group: id, order: 1)
        let image = [UInt8](batch([front, back]))
        let shape = front.canvas.frame!.bodyPath
        let scale = 100 / BotEngine.FRAME_SIZE
        func inside(_ x: Double, _ y: Double, _ grow: Double) -> Bool {
            let reach = shape.boundingBoxOfPath.width / 2 * scale + grow
            let center = shape.boundingBoxOfPath
            return hypot(x - (80 + center.midX * scale), y - (70 + center.midY * scale)) <= reach
        }
        var hidden = 0
        for y in stride(from: 21, to: 189, by: 3) {
            for x in stride(from: 31, to: 169, by: 3) {
                // The compositor's rows run from the bottom up.
                let alpha = image[((Int(size.height) - 1 - y) * Int(size.width) + x) * 4 + 3]
                // Just outside the front avatar is the gap; clear of it, the back avatar shows as usual.
                if inside(Double(x), Double(y), 4), !inside(Double(x), Double(y), 1) {
                    precondition(alpha == 0, "The stack gap isn't around the front avatar at \(x), \(y)")
                    hidden += 1
                }
            }
        }
        precondition(hidden > 5, "The stack gap wasn't checked")
    }

    static func updated() {
        let tile = slot(CGRect(origin: .zero, size: size))
        let view = AvatarSurfaceView()
        view.frame = tile.rect
        view.update([tile])
        let first = view.layer!.sublayers!.first!
        view.update([tile])
        precondition(view.layer!.sublayers!.first === first, "An update replaced the tile's layers")
        view.update([])
        precondition(view.layer!.sublayers?.isEmpty != false, "Removed tiles left stale layers")
    }

    static func resized() {
        let tile = slot(.null)
        let view = AvatarSurfaceView()
        view.update([tile])
        view.frame = CGRect(x: 0, y: 0, width: 120, height: 120)
        view.layout()
        precondition(tile.canvas.motion.size == view.bounds.size, "A still avatar did not resize")
        precondition(tile.canvas.frame != nil, "A zero-sized initial avatar stayed blank")
    }

    static func clipping() {
        // A cupcake spin reaches past the frame; the compositor must shrink it back inside.
        let canvas = AvatarCanvasState(motion: AvatarMotion())
        var moving = input()
        moving.options.shape = .cupcake
        moving.playing = true
        moving.frozenAt = nil
        moving.running = true
        moving.interactive = true
        canvas.input = moving
        let layers = AvatarLayers()
        let box = CGRect(origin: .zero, size: size)
        var spun = false
        for frame in 0..<60 {
            if frame == 6 { canvas.motion.twirling = true }
            canvas.update(Date(timeIntervalSince1970: 1_700_000_000 + Double(frame) / 60), size: size)
            spun = spun || canvas.frame?.transform != .identity
            guard frame >= 8 else { continue }
            CATransaction.begin()
            CATransaction.setDisableActions(true)
            layers.apply(canvas, in: box, scale: 2)
            CATransaction.commit()
            let data = bitmap(layers.root)
            let width = Int(size.width), height = Int(size.height)
            for y in 0..<height {
                for x in 0..<width where x == 0 || y == 0 || x == width - 1 || y == height - 1 {
                    precondition(data[(y * width + x) * 4 + 3] == 0, "Avatar is clipped at frame \(frame)")
                }
            }
        }
        precondition(spun, "The avatar never spun")
    }

    static func clocks() {
        _ = NSApplication.shared
        let window = NSWindow(contentRect: CGRect(origin: .zero, size: size), styleMask: .borderless,
                              backing: .buffered, defer: false)
        window.isReleasedWhenClosed = false
        defer { window.contentView = nil; window.close() }
        let small = AvatarSurfaceView()
        let large = AvatarSurfaceView()
        small.frame = CGRect(x: 0, y: 0, width: 32, height: 32)
        large.frame = CGRect(x: 40, y: 40, width: 120, height: 120)
        window.contentView!.addSubview(small)
        window.contentView!.addSubview(large)
        var moving = input()
        moving.running = true
        let tile = AvatarSlot(canvas: AvatarCanvasState(motion: AvatarMotion()), input: moving, rect: .null)
        small.update([tile])
        large.update([AvatarSlot(canvas: AvatarCanvasState(motion: AvatarMotion()), input: moving, rect: .null)])
        precondition(small.clock != nil && small.clock === large.clock, "Avatars do not share a window clock")
        precondition(small.clock?.rate == 60, "A large avatar did not raise the shared cadence")
        large.stop()
        precondition(small.clock?.rate == 30, "The clock stayed at 60 fps after its large avatar stopped")
        weak let clock = small.clock
        small.stop()
        precondition(clock == nil, "The last subscriber did not release the display link")
        var still = tile.input
        still.running = false
        small.update([AvatarSlot(canvas: tile.canvas, input: still, rect: .null)])
        precondition(small.clock == nil, "A still avatar registered an animation clock")
    }

    static func interactions() {
        for shape in [ShapeID.circle, .heart, .cat, .cupcake] {
            let canvas = AvatarCanvasState(motion: AvatarMotion())
            var moving = input()
            moving.options.shape = shape
            moving.options.followCursor = false
            moving.frozenAt = nil
            moving.playing = true
            moving.running = true
            moving.interactive = true
            canvas.input = moving
            let layers = AvatarLayers(), root = CALayer()
            root.addSublayer(layers.root)
            func image(_ date: Date) -> Data {
                canvas.update(date, size: size)
                CATransaction.begin()
                CATransaction.setDisableActions(true)
                layers.apply(canvas, in: CGRect(origin: .zero, size: size), scale: 2)
                CATransaction.commit()
                return bitmap(root)
            }
            canvas.motion.petted = true
            for frame in 0..<80 {
                let date = Date(timeIntervalSince1970: 1_700_000_000 + Double(frame) / 60)
                canvas.update(date, size: size)
                guard frame > 20, frame.isMultiple(of: 4) else { continue }
                let before = image(date)
                canvas.motion.petted.toggle()
                if frame.isMultiple(of: 12) { canvas.motion.twirling = true }
                let after = image(date)
                let changed = zip(before, after).filter { abs(Int($0) - Int($1)) > 2 }.count
                precondition(changed < 30, "\(shape.label) snapped at frame \(frame): \(changed) changed channels")
            }
        }
    }

    static func proximity() {
        let surface = AvatarSurfaceView()
        surface.frame = CGRect(origin: .zero, size: size)
        var resting = input()
        resting.frozenAt = nil
        resting.settles = true
        resting.tracksPointer = true
        resting.interactive = true
        let first = AvatarSlot(canvas: AvatarCanvasState(motion: AvatarMotion()), input: resting,
                               rect: CGRect(x: 10, y: 10, width: 40, height: 40))
        let second = AvatarSlot(canvas: AvatarCanvasState(motion: AvatarMotion()), input: resting,
                                rect: CGRect(x: 10, y: 100, width: 40, height: 40))
        surface.update([first, second])
        let start = Date(timeIntervalSince1970: 1_700_000_000)
        func advance(_ count: Int, from time: Double) {
            for frame in 0..<count {
                first.canvas.update(start.addingTimeInterval(time + Double(frame) / 60), size: first.rect.size)
            }
        }
        surface.point(CGPoint(x: 180, y: 30), date: start)
        precondition(!first.canvas.moving && !second.canvas.moving, "Hovering a distant row label wakes avatars")
        let still = first.canvas.frame!.eyes.map(\.d)
        advance(60, from: 0)
        precondition(first.canvas.frame!.eyes.map(\.d) == still, "An idle avatar moves without a nearby pointer")
        surface.point(CGPoint(x: 60, y: 30), date: start.addingTimeInterval(1))
        precondition(first.canvas.input?.gazing == true && first.canvas.moving, "Nearby pointer did not wake the avatar")
        precondition(!second.canvas.moving, "Nearby pointer woke another row's avatar")
        advance(120, from: 1)
        precondition(first.canvas.frame!.eyes.map(\.d) != still, "The nearby avatar did not face the pointer")
        surface.point(nil, date: start.addingTimeInterval(3))
        advance(180, from: 3)
        precondition(!first.canvas.moving, "The avatar never settled after the pointer left")
        let settled = first.canvas.frame!.eyes.map(\.d)
        advance(60, from: 6)
        precondition(first.canvas.frame!.eyes.map(\.d) == settled, "The avatar kept moving after settling")

        resting.playing = true
        resting.running = true
        resting.state = .swirl
        resting.mood = .working
        let working = AvatarSlot(canvas: first.canvas, input: resting, rect: first.rect)
        surface.update([working])
        surface.point(nil)
        precondition(first.canvas.moving, "An active agent stopped when the pointer left")
        advance(60, from: 7)
        let workingEyes = first.canvas.frame!.eyes.map(\.d)
        advance(60, from: 8)
        precondition(first.canvas.frame!.eyes.map(\.d) != workingEyes, "An active agent did not animate")
        surface.update([first])
        advance(180, from: 9)
        precondition(!first.canvas.moving, "The avatar kept animating after work ended")
    }

    /// A move cut off by the window going inactive finishes when it comes back, instead of holding that pose.
    static func interrupted() {
        let surface = AvatarSurfaceView()
        surface.frame = CGRect(origin: .zero, size: size)
        let canvas = AvatarCanvasState(motion: AvatarMotion())
        let rect = CGRect(x: 10, y: 10, width: 60, height: 60)
        var active = input()
        active.frozenAt = nil
        active.settles = true
        active.playing = true
        active.running = true
        active.state = .swirl
        active.mood = .working
        surface.update([AvatarSlot(canvas: canvas, input: active, rect: rect)])
        let start = Date(timeIntervalSince1970: 1_700_000_000)
        func advance(_ count: Int, from time: Double) {
            for frame in 0..<count { canvas.update(start.addingTimeInterval(time + Double(frame) / 60), size: rect.size) }
        }
        advance(30, from: 0)
        var inactive = input()
        inactive.frozenAt = nil
        var back = inactive
        back.settles = true
        surface.update([AvatarSlot(canvas: canvas, input: back, rect: rect)])
        advance(3, from: 1)
        precondition(canvas.moving, "An avatar didn't ease to rest when work ended")
        surface.update([AvatarSlot(canvas: canvas, input: inactive, rect: rect)])
        precondition(!canvas.moving, "An inactive window kept animating")
        surface.update([AvatarSlot(canvas: canvas, input: back, rect: rect)])
        precondition(canvas.moving, "An interrupted avatar stayed stuck in its pose")
        advance(240, from: 2)
        precondition(!canvas.moving, "An interrupted avatar never settled")
    }

    /// A new look on a resting avatar finishes its morph, or lands at once where nothing can draw it.
    static func restyled() {
        let rect = CGRect(x: 10, y: 10, width: 120, height: 120)
        var cupcake = AvatarOptions.default
        cupcake.shape = .cupcake
        func resting(_ options: AvatarOptions, settles: Bool) -> AvatarInput {
            var value = input()
            value.frozenAt = nil
            value.options = options
            value.settles = settles
            value.mood = .failed
            return value
        }
        func picture(_ canvas: AvatarCanvasState) -> [UInt8] {
            let layers = AvatarLayers()
            layers.apply(canvas, in: rect, scale: 2)
            return [UInt8](bitmap(layers.root))
        }
        func matches(_ a: [UInt8], _ b: [UInt8]) -> Bool {
            zip(a, b).filter { abs(Int($0) - Int($1)) > 64 }.count < 200
        }
        let start = Date(timeIntervalSince1970: 1_700_000_000)
        let expected = AvatarCanvasState(motion: AvatarMotion())
        expected.input = resting(cupcake, settles: false)
        expected.update(start, size: rect.size)
        let wanted = picture(expected)

        let surface = AvatarSurfaceView()
        surface.frame = CGRect(origin: .zero, size: size)
        for settles in [true, false] {
            let canvas = AvatarCanvasState(motion: AvatarMotion())
            surface.update([AvatarSlot(canvas: canvas, input: resting(.default, settles: settles), rect: rect)])
            precondition(!canvas.moving, "A resting avatar kept drawing")
            surface.update([AvatarSlot(canvas: canvas, input: resting(cupcake, settles: settles), rect: rect)])
            precondition(canvas.moving == settles, "A restyled avatar didn't match its window's state")
            var frame = 0
            while canvas.moving && frame < 120 {
                frame += 1
                canvas.update(start.addingTimeInterval(Double(frame) / 60), size: rect.size)
            }
            precondition(!canvas.moving, "A restyled avatar never settled")
            precondition(matches(picture(canvas), wanted), "A restyled avatar kept its old body")
        }

        let canvas = AvatarCanvasState(motion: AvatarMotion())
        var thinking = resting(cupcake, settles: true)
        thinking.mood = .thinking
        thinking.playing = true
        thinking.running = true
        surface.update([AvatarSlot(canvas: canvas, input: thinking, rect: rect)])
        for frame in 0..<60 { canvas.update(start.addingTimeInterval(Double(frame) / 60), size: rect.size) }
        precondition(canvas.frame?.parts.isEmpty == true, "A thinking avatar kept its parts")
        surface.update([AvatarSlot(canvas: canvas, input: resting(cupcake, settles: true), rect: rect)])
        precondition(canvas.moving, "A bot that stopped thinking stayed as dots")
        var frame = 60
        while canvas.moving && frame < 240 {
            frame += 1
            canvas.update(start.addingTimeInterval(Double(frame) / 60), size: rect.size)
        }
        precondition(!canvas.moving && canvas.frame?.parts.isEmpty == false, "A bot never came back from thinking")
    }

    static func stateFaces() {
        for mood in [MoodID.thinking, .working, .listening, .blocked, .failed] {
            let normal = AvatarCanvasState(motion: AvatarMotion())
            let hovered = AvatarCanvasState(motion: AvatarMotion())
            var value = input()
            value.frozenAt = nil
            value.mood = mood
            value.running = true
            value.interactive = true
            value.playing = [.thinking, .working, .listening].contains(mood)
            normal.input = value
            hovered.input = value
            hovered.motion.petted = true
            for frame in 0..<120 {
                let date = Date(timeIntervalSince1970: 1_700_000_000 + Double(frame) / 60)
                normal.update(date, size: size)
                hovered.update(date, size: size)
                precondition(normal.frame!.eyes.map(\.d) == hovered.frame!.eyes.map(\.d),
                             "Petting replaced the \(mood.rawValue) expression")
            }
        }
    }

    static func released() {
        weak var released: AvatarSurfaceView?
        autoreleasepool {
            let view = AvatarSurfaceView()
            released = view
            view.frame = CGRect(origin: .zero, size: size)
            var moving = input()
            moving.running = true
            view.update([AvatarSlot(canvas: AvatarCanvasState(motion: AvatarMotion()),
                                    input: moving, rect: view.bounds)])
        }
        precondition(released == nil, "The display-link target retains the surface")
    }
}
