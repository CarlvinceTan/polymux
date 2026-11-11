import SwiftUI
import GraphKit
import PolymuxKit
#if os(macOS)
import AppKit
#endif

/// The pannable, zoomable surface. Only what's on screen is built, however large the graph.
struct GraphCanvas: View {
    let model: GraphModel
    let activity: ActivityStore
    let pulses: [GraphPulse]
    let viewport: CGSize
    @State private var band: (start: CGPoint, end: CGPoint)?
    @State private var bandBase: Set<String> = []
    @State private var dragStart: CGSize?
    @State private var pinch = 1.0
    /// The card picked up by a long press, which a touch drag then moves.
    @State private var lifted: String?
    #if os(macOS)
    @State private var events = GraphEvents()
    #endif

    private var onScreen: CGRect { CGRect(origin: .zero, size: viewport).insetBy(dx: -60, dy: -60) }

    private func rect(_ box: Box) -> CGRect {
        let corner = model.point(box.position)
        return CGRect(x: corner.x, y: corner.y, width: box.w * model.scale, height: box.h * model.scale)
    }

    private var shown: [ShownNode] { model.scene.nodes.filter { rect($0.box).intersects(onScreen) } }

    var body: some View {
        ZStack(alignment: .topLeading) {
            if model.place.grid { GraphGrid(scale: model.scale, offset: model.offset) }
            surface
            let nodes = shown
            ForEach(nodes.filter(\.isFrame)) { item($0) }
            GraphWires(model: model, pulses: pulses)
            ForEach(nodes.filter { !$0.isFrame }) { item($0) }
            ForEach(model.scene.wires) { wire in
                let anchor = model.point(GraphKit.Point(x: wire.route.anchor.x, y: wire.route.anchor.y))
                if onScreen.contains(anchor) {
                    GraphWireLabel(wire: wire, from: model.title(wire.from), to: model.title(wire.to), showsTitle: named(wire),
                                   selected: model.selectedWire == wire.id) {
                        model.selectWire(wire.id)
                    }
                    // Labels shrink with the graph, but stay big enough to read.
                    .scaleEffect(min(1, max(0.65, model.scale)))
                    .position(anchor)
                }
            }
            if let band {
                Rectangle()
                    .fill(Color.accentColor.opacity(0.08))
                    .overlay { Rectangle().strokeBorder(Color.accentColor.opacity(0.6), style: StrokeStyle(lineWidth: 1, dash: [4, 3])) }
                    .frame(width: abs(band.end.x - band.start.x), height: abs(band.end.y - band.start.y))
                    .position(x: (band.start.x + band.end.x) / 2, y: (band.start.y + band.end.y) / 2)
                    .allowsHitTesting(false)
            }
        }
        .frame(width: viewport.width, height: viewport.height, alignment: .topLeading)
        .clipped()
        .coordinateSpace(.named("graph"))
        // On the whole canvas, because a pinch often lands on a node.
        .simultaneousGesture(MagnifyGesture()
            .onChanged { value in
                model.zoom(by: value.magnification / pinch, around: value.startLocation)
                pinch = value.magnification
            }
            .onEnded { _ in pinch = 1 })
        #if os(iOS)
        // One finger pans from anywhere, cards included; a card moves only after a long press picks it up.
        .simultaneousGesture(DragGesture(minimumDistance: 6, coordinateSpace: .named("graph"))
            .onChanged { value in
                guard lifted == nil else { return }
                model.pan(by: CGSize(width: value.translation.width - (dragStart?.width ?? 0),
                                     height: value.translation.height - (dragStart?.height ?? 0)))
                dragStart = value.translation
            }
            .onEnded { _ in dragStart = nil })
        .sensoryFeedback(.impact(weight: .light), trigger: lifted) { _, new in new != nil }
        #endif
        #if os(macOS)
        .onContinuousHover(coordinateSpace: .named("graph")) { phase in
            if case .active(let location) = phase { events.pointer = location } else { events.pointer = nil }
        }
        .onAppear {
            events.pan = { model.pan(by: $0) }
            events.zoom = { factor, anchor in model.zoom(by: factor, around: anchor) }
            events.start()
        }
        .onDisappear { events.stop() }
        #endif
    }

    /// The empty canvas behind everything, which takes the gestures nothing else claimed.
    private var surface: some View {
        Color.clear
            .contentShape(Rectangle())
            .gesture(SpatialTapGesture().onEnded { value in
                if let wire = model.wire(at: value.location) {
                    model.selectWire(wire)
                } else {
                    model.clearSelection()
                }
            })
            #if os(macOS)
            .gesture(marquee)
            #endif
            .accessibilityHidden(true)
    }

    #if os(macOS)
    private var marquee: some Gesture {
        DragGesture(minimumDistance: 3, coordinateSpace: .named("graph"))
            .onChanged { value in
                if band == nil { bandBase = adding ? model.selected : [] }
                band = (value.startLocation, value.location)
                model.marquee(from: value.startLocation, to: value.location, adding: bandBase)
            }
            .onEnded { _ in band = nil }
    }
    #endif

    private var adding: Bool {
        #if os(macOS)
        !NSEvent.modifierFlags.intersection([.shift, .command]).isEmpty
        #else
        false
        #endif
    }

    /// A wire names itself once it's what the viewer is looking at, or when it failed. Its parcel says the rest.
    private func named(_ wire: ShownWire) -> Bool {
        if model.selectedWire == wire.id || wire.state == .failed { return true }
        guard model.selected.count == 1, let id = model.selected.first else { return false }
        return wire.from == id || wire.to == id
    }

    @ViewBuilder private func item(_ node: ShownNode) -> some View {
        if let content = model.nodes[node.id] {
            // Whatever isn't wired to the selection steps back. A frame is ground, so it stays.
            let faded = !node.isFrame && model.focus.map { !$0.contains(node.id) } ?? false
            GraphNodeView(shown: node, node: content, selected: model.selected.contains(node.id), activity: activity,
                          select: { model.select(node.id, adding: adding) }, toggle: { model.toggle(node.id) })
                .opacity(faded ? 0.35 : 1)
                .gesture(move(node.id))
                .scaleEffect(model.scale)
                .position(x: (node.box.x + node.box.w / 2) * model.scale + model.offset.x,
                          y: (node.box.y + node.box.h / 2) * model.scale + model.offset.y)
        }
    }
}

extension GraphCanvas {
    /// The pointer drags a card straight away; a finger holds it first, so a plain swipe pans instead.
    private func move(_ id: String) -> some Gesture {
        #if os(iOS)
        LongPressGesture(minimumDuration: 0.35)
            .sequenced(before: DragGesture(minimumDistance: 0, coordinateSpace: .named("graph")))
            .onChanged { value in
                guard case .second(true, let drag) = value else { return }
                if lifted == nil {
                    lifted = id
                    dragStart = nil
                    model.beginDrag(id)
                }
                if let drag { model.drag(by: drag.translation) }
            }
            .onEnded { _ in
                if lifted != nil { model.endDrag() }
                lifted = nil
            }
        #else
        DragGesture(minimumDistance: 4, coordinateSpace: .named("graph"))
            .onChanged { value in
                model.beginDrag(id)
                model.drag(by: value.translation)
            }
            .onEnded { _ in model.endDrag() }
        #endif
    }
}

/// The dot grid. It fades out rather than turning solid when zoomed far out.
private struct GraphGrid: View {
    let scale: Double
    let offset: CGPoint

    var body: some View {
        Canvas { context, size in
            let step = 24 * scale
            guard step >= 8 else { return }
            var x = offset.x.truncatingRemainder(dividingBy: step)
            if x < 0 { x += step }
            while x < size.width {
                var y = offset.y.truncatingRemainder(dividingBy: step)
                if y < 0 { y += step }
                while y < size.height {
                    context.fill(Path(ellipseIn: CGRect(x: x - 1, y: y - 1, width: 2, height: 2)), with: .color(.primary.opacity(0.14)))
                    y += step
                }
                x += step
            }
        }
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }
}

#if os(macOS)
/// Two-finger scrolling, Command-scroll and right-button drags, which SwiftUI doesn't offer on a plain view.
/// It only answers while the pointer is over the canvas itself, so panels above keep their scrolling.
@MainActor final class GraphEvents {
    var pointer: CGPoint?
    var pan: (CGSize) -> Void = { _ in }
    var zoom: (Double, CGPoint) -> Void = { _, _ in }
    private var monitor: Any?

    func start() {
        guard monitor == nil else { return }
        monitor = NSEvent.addLocalMonitorForEvents(matching: [.scrollWheel, .rightMouseDragged, .otherMouseDragged]) { [weak self] event in
            self?.handle(event) ?? event
        }
    }

    func stop() {
        if let monitor { NSEvent.removeMonitor(monitor) }
        monitor = nil
    }

    private func handle(_ event: NSEvent) -> NSEvent? {
        guard let pointer else { return event }
        if event.type == .scrollWheel {
            if event.modifierFlags.contains(.command) {
                zoom(1 + event.scrollingDeltaY * 0.0015 * (event.hasPreciseScrollingDeltas ? 1 : 10), pointer)
            } else {
                pan(CGSize(width: event.scrollingDeltaX, height: event.scrollingDeltaY))
            }
            return nil
        }
        pan(CGSize(width: event.deltaX, height: event.deltaY))
        return nil
    }
}
#endif
