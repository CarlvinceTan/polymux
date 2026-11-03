import AvatarKit
import SwiftUI
#if os(macOS)
import AppKit
#else
import UIKit
#endif

/// One avatar's drawing state: everything the renderer needs, resolved once per view update.
struct AvatarInput {
    var options: AvatarOptions
    var state: StateID
    var expression: ExpressionID?
    var playing: Bool
    var frozenAt: Double?
    var timeline: [AvatarBlock]?
    var time: Double?
    var repeating: Bool
    var cueID: String?
    var mood: MoodID?
    var interactive: Bool
    var personality: String
    var reduceMotion: Bool
    var running: Bool
    /// Awake enough to keep drawing while it settles after `running` ends.
    var settles: Bool
    var gazing: Bool
    var ink: CGColor
    var markInk: CGColor
    var backing: CGColor
    var eye: CGColor
    var dark: Bool
    var tracksPointer = false
}

/// One avatar's playback and sampled geometry, independent of its drawing surface.
@MainActor
final class AvatarCanvasState {
    let motion: AvatarMotion
    var input: AvatarInput?
    private(set) var frame: BotFrame?

    nonisolated init(motion: AvatarMotion) { self.motion = motion }

    /// Playing, gazing or still settling, so it needs drawing every frame.
    var moving: Bool { input?.running == true || motion.settling }

    func stop() {
        input?.running = false
        input?.settles = false
        motion.suspend()
    }

    func update(_ date: Date, size: CGSize) {
        frame = sample(date, size: size)
    }
}

extension AvatarCanvasState {
    func sample(_ date: Date, size: CGSize) -> BotFrame? {
        guard let input else { return nil }
        motion.size = size
        motion.interactive = input.interactive
        motion.personality = input.personality
        return motion.frame(date: date, options: input.options, state: input.state, expression: input.expression,
                            running: input.running, settles: input.settles, gazing: input.gazing,
                            still: !input.playing || input.frozenAt != nil,
                            frozenAt: input.frozenAt, time: input.time, timeline: input.timeline,
                            reduceMotion: input.reduceMotion, repeating: input.repeating, cueID: input.cueID,
                            mood: input.mood)
    }
}

#if os(macOS)
typealias AvatarSurfaceBase = NSView
#else
typealias AvatarSurfaceBase = UIView
#endif

@MainActor
final class AvatarSurfaceView: AvatarSurfaceBase {
    var tiles: [AvatarSlot] = []
    fileprivate var frameRate: Float = 0
    private var sampled: [ObjectIdentifier: CFTimeInterval] = [:]
    private(set) var clock: AvatarClock?
    private var layered: [ObjectIdentifier: AvatarLayers] = [:]
    #if os(macOS)
    private var monitor: Any?
    #endif

    init() {
        super.init(frame: .zero)
        #if os(macOS)
        wantsLayer = true
        #else
        isOpaque = false
        backgroundColor = .clear
        isUserInteractionEnabled = false
        #endif
    }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError() }

    private func rect(_ tile: AvatarSlot) -> CGRect { tile.rect.isNull ? bounds : tile.rect }

    private var visible: [AvatarSlot] {
        tiles.filter { rect($0).width > 0 && rect($0).height > 0 && rect($0).intersects(bounds) }
    }

    func update(_ tiles: [AvatarSlot]) {
        self.tiles = tiles
        sampled.removeAll(keepingCapacity: true)
        let date = Date()
        #if os(macOS)
        trackPointer()
        #endif
        for tile in visible {
            tile.canvas.input = tile.input
            #if os(macOS)
            point(tile, at: pointer)
            #endif
            tile.canvas.update(date, size: rect(tile).size)
        }
        place()
        schedule()
    }

    /// Pointer movement can wake a resting surface without a running display link.
    func point(_ point: CGPoint?, date: Date = Date()) {
        var changed = false
        for tile in visible {
            let gazing = tile.canvas.input?.gazing
            self.point(tile, at: point)
            if tile.canvas.input?.gazing != gazing {
                tile.canvas.update(date, size: rect(tile).size)
                changed = true
            }
        }
        if changed { place(); schedule() }
    }

    private func point(_ tile: AvatarSlot, at point: CGPoint?) {
        let box = rect(tile)
        let nearby = tile.input.tracksPointer && point.map {
            hypot($0.x - box.midX, $0.y - box.midY) <= max(24, box.width)
        } == true
        tile.canvas.motion.hover = nearby ? point.map { CGPoint(x: $0.x - box.minX, y: $0.y - box.minY) } : nil
        tile.canvas.input?.gazing = nearby
        tile.canvas.input?.running = tile.input.running || nearby
    }

    func stop() {
        clock?.remove(self)
        clock = nil
        frameRate = 0
    }

    private func schedule() {
        let moving = visible.filter { $0.canvas.moving }
        guard let window, !moving.isEmpty else { return stop() }
        if clock?.windowID != ObjectIdentifier(window) { stop() }
        frameRate = moving.contains { min(rect($0).width, rect($0).height) >= 48 } ? 60 : 30
        clock = AvatarClock.join(self)
    }

    fileprivate func step(_ link: CADisplayLink) {
        let date = Date()
        #if os(macOS)
        // Leaving the window sends no event here, so a gazing avatar checks that the pointer is still around.
        if pointer == nil, visible.contains(where: { $0.canvas.input?.gazing == true }) { point(nil, date: date) }
        #endif
        var changed = false
        for tile in visible where tile.canvas.moving {
            let id = ObjectIdentifier(tile.canvas)
            let interval = min(rect(tile).width, rect(tile).height) >= 48 ? 1.0 / 60 : 1.0 / 30
            guard link.timestamp - (sampled[id] ?? -.infinity) >= interval * 0.9 else { continue }
            sampled[id] = link.timestamp
            tile.canvas.update(date, size: rect(tile).size)
            changed = true
        }
        if changed { place() }
        else { schedule() }
    }

    private func place() {
        #if os(macOS)
        guard let host = layer else { return }
        let scale = window?.backingScaleFactor ?? 2
        #else
        let host = layer
        let scale = traitCollection.displayScale
        #endif
        CATransaction.begin()
        CATransaction.setDisableActions(true)
        var order: [CALayer] = []
        var kept: [ObjectIdentifier: AvatarLayers] = [:]
        // Back tiles first, so the ones in front overlap them.
        let tiles = visible.enumerated().sorted { ($0.element.order, $1.offset) > ($1.element.order, $0.offset) }
        for tile in tiles.map(\.element) {
            let id = ObjectIdentifier(tile.canvas)
            let layers = layered[id] ?? AvatarLayers()
            layers.apply(tile.canvas, in: rect(tile), scale: scale)
            kept[id] = layers
            order.append(layers.root)
        }
        for (id, layers) in layered where kept[id] == nil { layers.root.removeFromSuperlayer() }
        layered = kept
        cut(tiles.map(\.element), scale: scale)
        if !(host.sublayers ?? []).elementsEqual(order, by: ===) { host.sublayers = order }
        CATransaction.commit()
    }

    /// In a stack, each avatar cuts a gap out of every one drawn before it.
    private func cut(_ drawn: [AvatarSlot], scale: CGFloat) {
        for (index, tile) in drawn.enumerated() {
            guard let layers = layered[ObjectIdentifier(tile.canvas)] else { continue }
            let box = rect(tile)
            let covers = drawn[(index + 1)...].compactMap { front -> AvatarCover? in
                guard tile.group != nil, front.group == tile.group, front.gap > 0,
                      let pieces = layered[ObjectIdentifier(front.canvas)]?.silhouette, !pieces.isEmpty else { return nil }
                return AvatarCover(pieces: pieces, offset: CGPoint(x: rect(front).minX - box.minX, y: rect(front).minY - box.minY),
                                   gap: front.gap)
            }
            layers.cut(covers, scale: scale)
        }
    }

    #if os(macOS)
    private var pointer: CGPoint? {
        guard let window, window.isKeyWindow, let content = window.contentView else { return nil }
        let point = window.mouseLocationOutsideOfEventStream
        guard content.bounds.contains(content.convert(point, from: nil)) else { return nil }
        return convert(point, from: nil)
    }

    private func trackPointer() {
        guard window != nil, visible.contains(where: { $0.input.tracksPointer }) else {
            untrackPointer()
            return
        }
        guard monitor == nil else { return }
        monitor = NSEvent.addLocalMonitorForEvents(matching: [.mouseMoved, .mouseExited, .leftMouseDragged]) { [weak self] event in
            if let self { self.point(event.window === self.window ? self.pointer : nil) }
            return event
        }
    }

    func untrackPointer() {
        if let monitor { NSEvent.removeMonitor(monitor) }
        monitor = nil
    }

    isolated deinit { untrackPointer() }

    override func hitTest(_ point: NSPoint) -> NSView? { nil }
    override var isFlipped: Bool { true }
    override func viewDidMoveToWindow() { super.viewDidMoveToWindow(); update(tiles) }
    override func layout() { super.layout(); update(tiles) }
    #else
    override func didMoveToWindow() { super.didMoveToWindow(); update(tiles) }
    override func layoutSubviews() { super.layoutSubviews(); update(tiles) }
    #endif
}

#if os(macOS)
struct AvatarSurface: NSViewRepresentable {
    let tiles: [AvatarSlot]
    func makeNSView(context: Context) -> AvatarSurfaceView { AvatarSurfaceView() }
    func updateNSView(_ view: AvatarSurfaceView, context: Context) { view.update(tiles) }
    static func dismantleNSView(_ view: AvatarSurfaceView, coordinator: ()) { view.stop(); view.untrackPointer() }
}
#else
struct AvatarSurface: UIViewRepresentable {
    let tiles: [AvatarSlot]
    func makeUIView(context: Context) -> AvatarSurfaceView { AvatarSurfaceView() }
    func updateUIView(_ view: AvatarSurfaceView, context: Context) { view.update(tiles) }
    static func dismantleUIView(_ view: AvatarSurfaceView, coordinator: ()) { view.stop() }
}
#endif

@MainActor
final class AvatarClock: NSObject {
    private struct Member {
        weak var surface: AvatarSurfaceView?
    }

    private static var clocks: [ObjectIdentifier: AvatarClock] = [:]
    let windowID: ObjectIdentifier
    private var members: [ObjectIdentifier: Member] = [:]
    private var link: CADisplayLink?
    private(set) var rate: Float = 0

    private init(windowID: ObjectIdentifier) { self.windowID = windowID }

    static func join(_ surface: AvatarSurfaceView) -> AvatarClock? {
        guard let window = surface.window else { return nil }
        let id = ObjectIdentifier(window)
        let clock = clocks[id] ?? AvatarClock(windowID: id)
        clocks[id] = clock
        clock.members[ObjectIdentifier(surface)] = Member(surface: surface)
        clock.schedule()
        return clock
    }

    func remove(_ surface: AvatarSurfaceView) {
        members.removeValue(forKey: ObjectIdentifier(surface))
        schedule()
    }

    private func schedule() {
        members = members.filter { $0.value.surface != nil }
        let surfaces = members.values.compactMap(\.surface)
        guard let surface = surfaces.first else {
            link?.invalidate()
            link = nil
            Self.clocks.removeValue(forKey: windowID)
            return
        }
        let wanted = surfaces.map(\.frameRate).max() ?? 30
        guard link == nil || rate != wanted else { return }
        link?.invalidate()
        #if os(macOS)
        let next = surface.window!.displayLink(target: self, selector: #selector(step(_:)))
        #else
        let next = CADisplayLink(target: self, selector: #selector(step(_:)))
        #endif
        next.preferredFrameRateRange = CAFrameRateRange(minimum: 8, maximum: wanted, preferred: wanted)
        next.add(to: .main, forMode: .common)
        link = next
        rate = wanted
    }

    @objc func step(_ link: CADisplayLink) {
        for member in Array(members.values) { member.surface?.step(link) }
        if members.values.contains(where: { $0.surface == nil }) { schedule() }
    }
}

extension AvatarCanvasState {
    private static var colours: [String: CGColor] = [:]
    static func colour(_ hex: String, alpha: Double = 1) -> CGColor {
        let key = "\(hex)#\(((alpha * 255).rounded()))"
        if let cached = colours[key] { return cached }
        let resolved = resolve(Color(avatarHex: hex), alpha: alpha)
        colours[key] = resolved
        return resolved
    }

    static func colours(_ from: CGColor, _ to: CGColor, _ amount: Double, alpha: Double = 1) -> CGColor {
        guard let a = components(from), let b = components(to) else { return from }
        let t = max(0, min(1, amount))
        return CGColor(colorSpace: CGColorSpaceCreateDeviceRGB(),
                       components: [a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t,
                                    a[2] + (b[2] - a[2]) * t, alpha]) ?? from
    }

    private static func components(_ colour: CGColor) -> [CGFloat]? {
        guard let space = colour.colorSpace, space.model == .rgb else { return nil }
        return colour.components
    }

    /// Resolves a SwiftUI colour for the scheme being drawn, not whichever appearance happens to be current.
    static func resolve(_ colour: Color, alpha: Double = 1, dark: Bool = false) -> CGColor {
        #if os(macOS)
        var resolved = NSColor(colour).withAlphaComponent(alpha).cgColor
        NSAppearance(named: dark ? .darkAqua : .aqua)?.performAsCurrentDrawingAppearance {
            resolved = NSColor(colour).withAlphaComponent(alpha).cgColor
        }
        return resolved
        #else
        return UIColor(colour).withAlphaComponent(alpha).resolvedColor(with: UITraitCollection(
            userInterfaceStyle: dark ? .dark : .light)).cgColor
        #endif
    }
}
