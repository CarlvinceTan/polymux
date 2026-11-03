import SwiftUI
import GraphKit
import PolymuxKit

/// What one viewer chose for one graph. None of it reaches the server.
struct GraphPlace: Codable, Sendable, Equatable {
    var mode: ViewMode
    var toggled: [String: Set<String>] = [:]
    var moved: [String: [String: GraphKit.Point]] = [:]
    var seeds: [String: [String: GraphKit.Point]] = [:]
    var sides: [String: [String: SidePair]] = [:]
    var scale = 1.0
    var x = 0.0
    var y = 0.0
    /// The viewer moved the camera, so it stops following the graph until they fit it again.
    var framed = false
    var grid = true
    var snap = true
}

extension CGSize {
    /// Taller than wide by enough that a top-to-bottom graph reads better.
    var isTall: Bool { height > width * 1.15 }
}

/// One graph on screen: its data, the viewer's choices, and what they lay out to.
@MainActor @Observable
final class GraphModel {
    static let minimumScale = 0.25
    static let maximumScale = 2.0
    /// Below this, card titles and wire labels are too small to read.
    static let readableScale = 0.5
    private static let historyLimit = 100
    private static let snapGrid = 24.0
    private static let magnetReach = 12.0

    private(set) var scene = GraphScene()
    private(set) var graph = WorkGraph(revision: 0, nodes: [], wires: [])
    private(set) var loaded = false
    private(set) var place: GraphPlace
    private(set) var nodes: [String: WorkNode] = [:]
    var selected: Set<String> = []
    var selectedWire: String?
    /// Whether the inspector is open for the selection.
    var inspecting = false
    /// The space the graph has on screen.
    var viewport = CGSize.zero {
        didSet {
            if tall != oldValue.isTall, viewport != .zero { rebuild() }
            follow()
        }
    }

    private struct Step: Equatable {
        var moved: [String: [String: GraphKit.Point]]
        var toggled: [String: Set<String>]
    }

    private var history: [Step] = []
    private var pointer = -1
    private var dragOrigins: [String: GraphKit.Point] = [:]
    private var settled = false
    @ObservationIgnored private let key: String
    @ObservationIgnored private let cache: LocalCacheActor
    @ObservationIgnored private var saving: Task<Void, Never>?

    init(key: String, mode: ViewMode, cache: LocalCacheActor) {
        self.key = "graph-view-" + key
        self.cache = cache
        place = GraphPlace(mode: mode)
    }

    var scale: Double { place.scale }
    var offset: CGPoint { CGPoint(x: place.x, y: place.y) }
    var mode: ViewMode { place.mode }
    var canUndo: Bool { pointer > 0 }
    var canRedo: Bool { pointer < history.count - 1 }
    private var modeKey: String { place.mode.rawValue }
    private var tall: Bool { viewport.isTall }
    /// Arrangement belongs to one shape of space, because a tall graph runs the other way.
    private var layoutKey: String { modeKey + (tall ? "-tall" : "") }

    func restore() async {
        if let saved: GraphPlace = await cache.snapshot(key) {
            place = saved
            rebuild()
        }
        record()
    }

    /// Nothing is pinned in place until `settled`, or the first partial graph would shape the final one.
    func show(_ next: WorkGraph, settled: Bool) {
        loaded = true
        let settling = settled && !self.settled
        self.settled = self.settled || settled
        guard next != graph || settling else { return }
        graph = next
        nodes = Dictionary(next.nodes.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        rebuild()
        follow()
    }

    private func rebuild() {
        let items = graph.nodes.map { node in
            SceneNode(id: node.id, parent: node.parent?.isEmpty == false ? node.parent : nil, size: GraphCard.size(node),
                      frame: node.shape == WorkNodeShape.frame, folded: node.kind == "github")
        }
        let links = graph.wires.map { wire in
            SceneWire(id: wire.id, from: wire.from, to: wire.to, direction: WireDirection(rawValue: wire.direction ?? "") ?? .forward,
                      line: WireLine(rawValue: wire.line ?? "") ?? .solid, state: WireState(rawValue: wire.state ?? "") ?? .idle,
                      label: wire.label?.isEmpty == false ? wire.label : nil)
        }
        let pinned = settled || place.seeds[layoutKey]?.isEmpty == false
        let options = SceneOptions(mode: place.mode, toggled: place.toggled[modeKey] ?? [], moved: place.moved[layoutKey] ?? [:],
                                   seeds: pinned ? place.seeds[layoutKey] ?? [:] : [:], sides: pinned ? place.sides[layoutKey] ?? [:] : [:], tall: tall)
        scene = SceneBuilder.build(nodes: items, wires: links, options: options)
        if settled {
            place.seeds[layoutKey] = scene.seeds
            place.sides[layoutKey] = scene.sides
        }
        save()
    }

    private func save() {
        saving?.cancel()
        let place = place
        saving = Task { [cache, key] in
            try? await Task.sleep(for: .seconds(1))
            guard !Task.isCancelled else { return }
            await cache.saveSnapshot(key, place)
        }
    }

    // MARK: Modes and depth

    func setMode(_ mode: ViewMode) {
        guard mode != place.mode else { return }
        place.mode = mode
        clearSelection()
        rebuild()
        fit()
    }

    func toggle(_ id: String) {
        var open = place.toggled[modeKey] ?? []
        if !open.insert(id).inserted { open.remove(id) }
        place.toggled[modeKey] = open
        rebuild()
        record()
        follow()
    }

    // MARK: Arranging

    private func record() {
        let step = Step(moved: place.moved, toggled: place.toggled)
        if pointer >= 0, history[pointer] == step { return }
        history = Array(history.prefix(pointer + 1)) + [step]
        if history.count > Self.historyLimit { history.removeFirst(history.count - Self.historyLimit) }
        pointer = history.count - 1
    }

    func undo() {
        guard canUndo else { return }
        pointer -= 1
        apply(history[pointer])
    }

    func redo() {
        guard canRedo else { return }
        pointer += 1
        apply(history[pointer])
    }

    private func apply(_ step: Step) {
        place.moved = step.moved
        place.toggled = step.toggled
        rebuild()
        follow()
    }

    func beginDrag(_ id: String) {
        guard dragOrigins.isEmpty else { return }
        if !selected.contains(id) {
            selected = [id]
            selectedWire = nil
        }
        let seeds = place.moved[layoutKey, default: [:]].merging(place.seeds[layoutKey] ?? [:]) { moved, _ in moved }
        for item in selected { dragOrigins[item] = seeds[item] }
    }

    /// Moves what's selected, snapping to the grid and then to neighbouring nodes.
    func drag(by translation: CGSize) {
        guard !dragOrigins.isEmpty else { return }
        let dx = translation.width / place.scale
        let dy = translation.height / place.scale
        var moved = place.moved[layoutKey] ?? [:]
        for (id, origin) in dragOrigins {
            var point = GraphKit.Point(x: origin.x + dx, y: origin.y + dy)
            if place.snap { point = snapped(point, id: id) }
            moved[id] = point
        }
        place.moved[layoutKey] = moved
        rebuild()
    }

    private func snapped(_ point: GraphKit.Point, id: String) -> GraphKit.Point {
        let grid = Self.snapGrid
        var x = roundedHalfUp(point.x / grid) * grid
        var y = roundedHalfUp(point.y / grid) * grid
        guard let shown = scene.nodes.first(where: { $0.id == id }) else { return GraphKit.Point(x: x, y: y) }
        let reach = Self.magnetReach + 1
        var bestX = reach
        var bestY = reach
        var magnetX = x
        var magnetY = y
        /// Its near edge, centre and far edge, which are what line up with a neighbour's.
        func edges(_ start: Double, _ length: Double) -> [Double] { [start, start + length / 2, start + length] }
        for other in scene.nodes where !selected.contains(other.id) && other.container == shown.container {
            let origin = place.moved[layoutKey]?[other.id] ?? place.seeds[layoutKey]?[other.id] ?? other.box.position
            for (mine, theirs) in zip(edges(x, shown.box.w), edges(origin.x, other.box.w)) where abs(mine - theirs) < bestX {
                bestX = abs(mine - theirs)
                magnetX = x + theirs - mine
            }
            for (mine, theirs) in zip(edges(y, shown.box.h), edges(origin.y, other.box.h)) where abs(mine - theirs) < bestY {
                bestY = abs(mine - theirs)
                magnetY = y + theirs - mine
            }
        }
        x = magnetX
        y = magnetY
        return GraphKit.Point(x: x, y: y)
    }

    private func roundedHalfUp(_ value: Double) -> Double { (value + 0.5).rounded(.down) }

    func endDrag() {
        dragOrigins = [:]
        // Someone arranging the graph shouldn't have it refit under them.
        place.framed = true
        record()
    }

    func autoLayout() {
        place.moved[layoutKey] = [:]
        place.seeds[layoutKey] = [:]
        place.sides[layoutKey] = [:]
        rebuild()
        record()
        fit()
    }

    // MARK: Camera

    func point(_ world: GraphKit.Point) -> CGPoint {
        CGPoint(x: world.x * place.scale + place.x, y: world.y * place.scale + place.y)
    }

    func world(_ screen: CGPoint) -> GraphKit.Point {
        GraphKit.Point(x: (screen.x - place.x) / place.scale, y: (screen.y - place.y) / place.scale)
    }

    /// Shows every node, leaving room for the toolbar. It never magnifies past 100%.
    func fit() {
        guard viewport.width > 0, viewport.height > 0 else { return }
        // A side panel has no width to spare, so the margins give way to the graph.
        let narrow = viewport.width < 640
        let leading = narrow ? 48.0 : 64.0
        let margin = narrow ? 10.0 : 32.0
        var bounds = scene.bounds
        // A label centres on its wire, so one on the outermost wire hangs past the graph's edge.
        let anchors = scene.wires.filter { $0.wires.count > 1 || $0.label?.isEmpty == false }.map(\.route.anchor.x)
        if let low = anchors.min(), let high = anchors.max() {
            let reach = 56.0
            let left = min(bounds.x, low - reach)
            bounds.w = max(bounds.x + bounds.w, high + reach) - left
            bounds.x = left
        }
        let roomX = max(1, Double(viewport.width) - leading - margin * 2)
        let roomY = max(1, Double(viewport.height) - margin * 2)
        var scale = 1.0
        if bounds.w > 0, bounds.h > 0 {
            // A phone or side panel fits the width and scrolls down, and never opens too small to read.
            scale = tall ? max(Self.readableScale, min(roomX / bounds.w, 1)) : min(roomX / bounds.w, roomY / bounds.h, 1)
        }
        scale = min(Self.maximumScale, max(Self.minimumScale, scale))
        place.scale = scale
        // Wider than the room, the graph keeps its start in view rather than its middle.
        place.x = leading + margin + max(0, roomX - bounds.w * scale) / 2 - bounds.x * scale
        place.y = margin + max(0, roomY - bounds.h * scale) / 2 - bounds.y * scale
        place.framed = false
        save()
    }

    /// Keeps a graph that's still growing in view, without pulling it from under a drag.
    private func follow() {
        if !place.framed, dragOrigins.isEmpty, !scene.nodes.isEmpty { fit() }
    }

    func pan(by delta: CGSize) {
        place.x += delta.width
        place.y += delta.height
        place.framed = true
        save()
    }

    /// Keeps the point under `anchor` still, and lands on 100% when a step would cross it.
    func zoom(by factor: Double, around anchor: CGPoint, snaps: Bool = false) {
        let old = place.scale
        var next = min(Self.maximumScale, max(Self.minimumScale, old * factor))
        if snaps, (old < 1 && next > 1) || (old > 1 && next < 1) { next = 1 }
        guard next != old else { return }
        let target = world(anchor)
        place.scale = next
        place.x = anchor.x - target.x * next
        place.y = anchor.y - target.y * next
        place.framed = true
        save()
    }

    func setGrid(_ on: Bool) {
        place.grid = on
        save()
    }

    func setSnap(_ on: Bool) {
        place.snap = on
        save()
    }

    // MARK: Selection

    func select(_ id: String, adding: Bool) {
        selectedWire = nil
        if adding {
            if !selected.insert(id).inserted { selected.remove(id) }
        } else {
            selected = [id]
        }
        inspecting = selected.count == 1
    }

    func selectWire(_ id: String) {
        selected = []
        selectedWire = id
        inspecting = true
    }

    func clearSelection() {
        selected = []
        selectedWire = nil
        inspecting = false
    }

    /// Selects every node the rectangle touches.
    func marquee(from start: CGPoint, to end: CGPoint, adding base: Set<String>) {
        let first = world(start)
        let last = world(end)
        let left = min(first.x, last.x)
        let right = max(first.x, last.x)
        let top = min(first.y, last.y)
        let bottom = max(first.y, last.y)
        var next = base
        for node in scene.nodes where !node.isFrame {
            if node.box.x < right, node.box.x + node.box.w > left, node.box.y < bottom, node.box.y + node.box.h > top {
                next.insert(node.id)
            }
        }
        selected = next
        selectedWire = nil
        inspecting = false
    }

    /// The wire near a point, for a click on the empty canvas.
    func wire(at screen: CGPoint) -> String? {
        let point = world(screen)
        let reach = 10 / place.scale
        return scene.wires.first { wire in
            let pts = GraphPaths.points(wire.route.path)
            return zip(pts, pts.dropFirst()).contains { GraphPaths.distance(point, $0, $1) <= reach }
        }?.id
    }

    /// The line that a handoff between two nodes travels along, wherever they've rolled up to.
    func wire(from source: String, to target: String) -> (id: String, reversed: Bool)? {
        let on = Set(scene.nodes.map(\.id))
        func shown(_ id: String) -> String? {
            var current: String? = id
            while let next = current {
                if on.contains(next) { return next }
                current = nodes[next]?.parent
            }
            return nil
        }
        guard let from = shown(source), let to = shown(target), from != to else { return nil }
        let lines = scene.wires.filter { !$0.isLink }
        guard let line = lines.first(where: { $0.from == from && $0.to == to })
            ?? lines.first(where: { $0.from == to && $0.to == from }) else { return nil }
        return (line.id, line.from != from)
    }

    func wireDetails(_ shown: ShownWire) -> [WorkWire] {
        shown.wires.compactMap { id in graph.wires.first { $0.id == id } }
    }

    func title(_ id: String) -> String { nodes[id]?.title ?? id }
}
