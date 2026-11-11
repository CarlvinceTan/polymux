import Foundation

public enum WireDirection: String, Sendable, Codable, CaseIterable {
    case forward, back, both, none
}

public enum WireLine: String, Sendable, Codable, CaseIterable {
    case solid, dashed, dotted
}

public enum WireState: String, Sendable, Codable, CaseIterable {
    case idle, active, failed
}

public enum ViewMode: String, Sendable, Codable, CaseIterable {
    case compact, full
}

/// One node of the whole graph, before a view decides what's on show.
public struct SceneNode: Hashable, Sendable {
    public var id: String
    public var parent: String?
    public var size: Size
    /// Draws its children inside it, as a device holds what runs on it.
    public var frame: Bool
    /// Stays closed in Compact even at the top level.
    public var folded: Bool

    public init(id: String, parent: String? = nil, size: Size, frame: Bool = false, folded: Bool = false) {
        self.id = id
        self.parent = parent
        self.size = size
        self.frame = frame
        self.folded = folded
    }
}

public struct SceneWire: Hashable, Sendable {
    public var id: String
    public var from: String
    public var to: String
    public var direction: WireDirection
    public var line: WireLine
    public var state: WireState
    public var label: String?

    public init(id: String, from: String, to: String, direction: WireDirection = .forward, line: WireLine = .solid,
                state: WireState = .idle, label: String? = nil) {
        self.id = id
        self.from = from
        self.to = to
        self.direction = direction
        self.line = line
        self.state = state
        self.label = label
    }
}

/// What the viewer chose. None of it belongs to the graph itself.
public struct SceneOptions: Hashable, Sendable {
    public var mode: ViewMode
    /// Nodes opened or closed against their mode's default.
    public var toggled: Set<String>
    /// Where the viewer put nodes, relative to their frame.
    public var moved: [String: Point]
    /// Where nodes landed before, so a live update doesn't rearrange them.
    public var seeds: [String: Point]
    /// Sides wires took before, so moving a node doesn't flip them.
    public var sides: [String: SidePair]
    /// Runs top to bottom, for a space taller than it is wide.
    public var tall: Bool

    public init(mode: ViewMode = .compact, toggled: Set<String> = [], moved: [String: Point] = [:],
                seeds: [String: Point] = [:], sides: [String: SidePair] = [:], tall: Bool = false) {
        self.mode = mode
        self.toggled = toggled
        self.moved = moved
        self.seeds = seeds
        self.sides = sides
        self.tall = tall
    }
}

public struct ShownNode: Identifiable, Hashable, Sendable {
    public var id: String
    public var box: Box
    public var depth: Int
    public var isFrame: Bool
    public var canExpand: Bool
    public var expanded: Bool
    /// Everything under this node that's out of view.
    public var hidden: Int
    /// The frame holding this node, which its position is relative to.
    public var container: String?
}

public struct ShownWire: Identifiable, Hashable, Sendable {
    public var id: String
    /// The graph's wires this one stands for after rolling up.
    public var wires: [String]
    public var from: String
    public var to: String
    public var direction: WireDirection
    public var line: WireLine
    public var state: WireState
    public var label: String?
    public var route: WireRoute
    /// A parent and child with nothing else connecting them.
    public var isLink: Bool
}

public struct GraphScene: Sendable {
    /// Frames come first, so they paint under what they hold.
    public var nodes: [ShownNode] = []
    public var wires: [ShownWire] = []
    public var bounds = Obstacle(x: 0, y: 0, w: 0, h: 0)
    public var seeds: [String: Point] = [:]
    public var sides: [String: SidePair] = [:]

    public init() {}
}

public enum SceneBuilder {
    static let frameHeader = 44.0
    static let framePad = 24.0
    static let groupGap = 72.0
    static let looseGap = 32.0
    static let looseWidth = 1200.0
    static let tallLooseWidth = 420.0
    static let tallColumns = 2
    static let tallGap = 32.0
    /// Room for a wire and its label between rows.
    static let tallRowGap = 88.0
    /// Room for a wire's label between columns.
    static let columnGap = 96.0
    /// Wide enough that snapping to the grid can't close it.
    static let rowGap = 40.0

    public static func build(nodes: [SceneNode], wires: [SceneWire], options: SceneOptions = SceneOptions()) -> GraphScene {
        Builder(nodes: nodes, wires: wires, options: options).scene()
    }

    /// Sizes each column to its widest node and stacks a column by real heights, each node as near the middle
    /// of what leads to it as its neighbours allow. The cell grid a layout comes in would leave small nodes far apart.
    static func packed(_ models: [NodeModel], wires: [GraphWire]) -> [LayoutRect] {
        let columns = Dictionary(grouping: models) { $0.col.rounded() }
        var sources: [String: [String]] = [:]
        for wire in wires { sources[wire.toId, default: []].append(wire.fromId) }
        var centres: [String: Double] = [:]
        var out: [LayoutRect] = []
        var left = 0.0
        for key in columns.keys.sorted() {
            let column = (columns[key] ?? []).sorted { $0.row < $1.row }
            let heights = column.map { $0.node.size?.h ?? Metrics.nodeH }
            let width = column.map { $0.node.size?.w ?? Metrics.nodeW }.max() ?? 0
            // Where each node would sit with the column to itself.
            var wanted: [Double] = []
            var below = 0.0
            for (index, model) in column.enumerated() {
                let leads = (sources[model.id] ?? []).compactMap { centres[$0] }
                wanted.append(leads.isEmpty ? below + heights[index] / 2 : leads.reduce(0, +) / Double(leads.count))
                below = max(below, (wanted.last ?? 0) + heights[index] / 2 + rowGap)
            }
            // Pushed apart from the top and from the bottom; halfway between the two keeps both ends fair.
            var down = wanted
            for index in column.indices.dropFirst() {
                down[index] = max(down[index], down[index - 1] + (heights[index - 1] + heights[index]) / 2 + rowGap)
            }
            var up = wanted
            for index in column.indices.dropLast().reversed() {
                up[index] = min(up[index], up[index + 1] - (heights[index + 1] + heights[index]) / 2 - rowGap)
            }
            for (index, model) in column.enumerated() {
                let centre = (down[index] + up[index]) / 2
                let size = model.node.size ?? Size(w: Metrics.nodeW, h: Metrics.nodeH)
                centres[model.id] = centre
                // On the grid, as a drag will leave it, so the first drag moves a node by what the pointer moved.
                let grid = Metrics.snapGrid
                out.append(LayoutRect(id: model.id, x: ((left + (width - size.w) / 2) / grid).rounded() * grid, y: ((centre - size.h / 2) / grid).rounded() * grid,
                                      w: size.w, h: size.h))
            }
            left += width + columnGap
        }
        return out
    }

    /// Stacks ranks top to bottom at most two across, so a wide fan-out doesn't shrink the whole graph.
    static func wrapped(_ rects: [LayoutRect]) -> [LayoutRect] {
        let ranks = Dictionary(grouping: rects) { (($0.y + $0.h / 2) / 48).rounded() }
        var rows: [[LayoutRect]] = []
        for key in ranks.keys.sorted() {
            let rank = (ranks[key] ?? []).sorted { $0.x < $1.x }
            rows += stride(from: 0, to: rank.count, by: tallColumns).map { Array(rank[$0..<min($0 + tallColumns, rank.count)]) }
        }
        // Columns line up, so the gutter between them stays clear for wires.
        let second = (rows.compactMap { $0.first?.w }.max() ?? 0) + tallGap
        var out: [LayoutRect] = []
        var top = 0.0
        for row in rows {
            for (index, item) in row.enumerated() {
                out.append(LayoutRect(id: item.id, x: index == 0 ? 0 : second, y: top, w: item.w, h: item.h))
            }
            top += (row.map(\.h).max() ?? 0) + tallRowGap
        }
        return out
    }

    private struct Rolled {
        var id: String
        var wires: [String]
        var a: String
        var b: String
        var forward = false
        var back = false
        var line: WireLine
        var state: WireState
        var label: String?
        var isLink = false
    }

    private final class Builder {
        let nodes: [SceneNode]
        let wires: [SceneWire]
        let options: SceneOptions
        var byID: [String: SceneNode] = [:]
        var children: [String: [String]] = [:]
        var roots: [String] = []
        var visible: [String] = []
        var depths: [String: Int] = [:]
        var sizes: [String: Size] = [:]
        var relative: [String: Point] = [:]
        var shifts: [String: Point] = [:]
        var rolled: [Rolled] = []

        init(nodes: [SceneNode], wires: [SceneWire], options: SceneOptions) {
            self.nodes = nodes
            self.wires = wires
            self.options = options
        }

        func scene() -> GraphScene {
            prepare()
            reveal()
            roll()
            let top = place(container: nil)
            return finish(top)
        }

        /// Sorts nodes into a tree. A missing or circular parent makes a node a root.
        func prepare() {
            for node in nodes where byID[node.id] == nil {
                byID[node.id] = node
            }
            for node in nodes where byID[node.id] == node {
                if let parent = node.parent, byID[parent] != nil, !circular(node.id) {
                    children[parent, default: []].append(node.id)
                } else {
                    roots.append(node.id)
                }
            }
        }

        func circular(_ id: String) -> Bool {
            var seen = [id]
            var current = byID[id]?.parent
            while let next = current {
                if seen.contains(next) { return true }
                seen.append(next)
                current = byID[next]?.parent
            }
            return false
        }

        func opened(_ id: String, depth: Int) -> Bool {
            let open = options.mode == .full || (depth == 0 && byID[id]?.folded != true)
            return open != options.toggled.contains(id)
        }

        func reveal() {
            func walk(_ id: String, depth: Int) {
                visible.append(id)
                depths[id] = depth
                guard opened(id, depth: depth) else { return }
                for child in children[id] ?? [] { walk(child, depth: depth + 1) }
            }
            for root in roots { walk(root, depth: 0) }
        }

        /// The nearest ancestor on show, which a hidden node's wires roll up to.
        func shown(_ id: String) -> String? {
            var current: String? = id
            let on = Set(visible)
            while let next = current {
                if on.contains(next) { return next }
                current = byID[next]?.parent
            }
            return nil
        }

        /// The frame a node sits in, passing through parents drawn beside their children.
        func container(_ id: String) -> String? {
            var current = byID[id]?.parent
            while let next = current, byID[next] != nil, !circular(id) {
                if byID[next]?.frame == true { return next }
                current = byID[next]?.parent
            }
            return nil
        }

        func ancestors(_ id: String) -> Set<String> {
            var out = Set<String>()
            var current = byID[id]?.parent
            while let next = current, !out.contains(next) {
                out.insert(next)
                current = byID[next]?.parent
            }
            return out
        }

        func roll() {
            var index: [String: Int] = [:]
            for wire in wires {
                guard let from = shown(wire.from), let to = shown(wire.to), from != to else { continue }
                // One line each way between a pair, so a loop keeps both of its labels.
                let backward = wire.direction == .back
                let directed = wire.direction == .forward || backward
                let key = directed ? (backward ? "\(to)>\(from)" : "\(from)>\(to)") : (from < to ? "\(from)|\(to)" : "\(to)|\(from)")
                if let at = index[key] {
                    rolled[at].wires.append(wire.id)
                    rolled[at].label = nil
                    merge(wire, flipped: rolled[at].a != from, into: &rolled[at])
                } else {
                    // Ends stay in the direction of travel, which decides the sides a wire uses.
                    var item = Rolled(id: wire.id, wires: [wire.id], a: backward ? to : from, b: backward ? from : to,
                                      line: wire.line, state: wire.state, label: wire.label)
                    merge(wire, flipped: backward, into: &item)
                    index[key] = rolled.count
                    rolled.append(item)
                }
            }
            // A child beside its parent stays attached to it, even with no wire between them.
            let on = Set(visible)
            for id in visible {
                guard let parent = byID[id]?.parent, on.contains(parent), byID[parent]?.frame != true else { continue }
                let key = id < parent ? "\(id)|\(parent)" : "\(parent)|\(id)"
                guard index[key] == nil, index["\(id)>\(parent)"] == nil, index["\(parent)>\(id)"] == nil else { continue }
                index[key] = rolled.count
                rolled.append(Rolled(id: "link:\(id)", wires: [], a: parent, b: id, line: .dotted, state: .idle, isLink: true))
            }
        }

        func merge(_ wire: SceneWire, flipped: Bool, into item: inout Rolled) {
            if wire.direction == .both || (wire.direction == .forward) != flipped && wire.direction != .none { item.forward = true }
            if wire.direction == .both || (wire.direction == .back) != flipped && wire.direction != .none { item.back = true }
            if wire.state == .active || item.state == .active {
                item.state = .active
            } else if wire.state == .failed {
                item.state = .failed
            }
        }

        /// Lays out a level, inner frames first because their contents decide their size.
        func place(container level: String?) -> Size {
            let members = visible.filter { container($0) == level }
            for id in members {
                let inner = visible.contains { container($0) == id }
                if byID[id]?.frame == true, inner {
                    let size = place(container: id)
                    sizes[id] = Size(w: size.w + SceneBuilder.framePad * 2, h: size.h + SceneBuilder.frameHeader + SceneBuilder.framePad)
                } else {
                    sizes[id] = byID[id]?.size
                }
            }
            let automatic = arrange(members)
            var placed: [String: Point] = [:]
            var free: [String] = []
            for id in members {
                if let point = options.moved[id] ?? options.seeds[id] {
                    placed[id] = point
                } else {
                    free.append(id)
                }
            }
            for id in free {
                placed[id] = clear(automatic[id] ?? Point(x: 0, y: 0), size: sizes[id] ?? Size(w: 0, h: 0), placed: placed)
            }
            var minX = Double.infinity
            var minY = Double.infinity
            var maxX = -Double.infinity
            var maxY = -Double.infinity
            for id in members {
                guard let point = placed[id], let size = sizes[id] else { continue }
                minX = min(minX, point.x)
                minY = min(minY, point.y)
                maxX = max(maxX, point.x + size.w)
                maxY = max(maxY, point.y + size.h)
            }
            guard minX.isFinite else { return Size(w: 0, h: 0) }
            for id in members {
                relative[id] = placed[id]
            }
            // A frame hugs its contents wherever they were dragged, without moving them.
            if let level { shifts[level] = Point(x: minX, y: minY) }
            return Size(w: maxX - minX, h: maxY - minY)
        }

        /// Keeps a newcomer off what's already placed.
        func clear(_ point: Point, size: Size, placed: [String: Point]) -> Point {
            var y = point.y
            for _ in 0..<512 {
                let hit = placed.contains { id, other in
                    guard let box = sizes[id] else { return false }
                    return point.x < other.x + box.w + Metrics.layoutGap && point.x + size.w + Metrics.layoutGap > other.x
                        && y < other.y + box.h + Metrics.layoutGap && y + size.h + Metrics.layoutGap > other.y
                }
                if !hit { break }
                y += Metrics.snapGrid
            }
            return Point(x: point.x, y: y)
        }

        /// Connected groups first, each in columns, then the loose nodes in rows beneath.
        func arrange(_ members: [String]) -> [String: Point] {
            var owner: [String: String] = [:]
            func find(_ id: String) -> String {
                var current = id
                while let next = owner[current], next != current { current = next }
                return current
            }
            for id in members { owner[id] = id }
            let inLevel = Set(members)
            var links: [GraphWire] = []
            for item in rolled {
                guard let from = member(item.a, inLevel), let to = member(item.b, inLevel), from != to else { continue }
                owner[find(from)] = find(to)
                links.append(GraphWire(id: item.id, fromId: item.back && !item.forward ? to : from, toId: item.back && !item.forward ? from : to))
            }
            var groups: [String: [String]] = [:]
            var order: [String] = []
            for id in members {
                let key = find(id)
                if groups[key] == nil { order.append(key) }
                groups[key, default: []].append(id)
            }
            let connected = order.filter { (groups[$0]?.count ?? 0) > 1 }
                .sorted { (groups[$0]?.count ?? 0) > (groups[$1]?.count ?? 0) }
            var out: [String: Point] = [:]
            var top = 0.0
            for key in connected {
                let group = groups[key] ?? []
                let inside = Set(group)
                let graph = Graph(nodes: group.map { GraphNode(id: $0, size: sizes[$0]) },
                                  wires: links.filter { inside.contains($0.fromId) && inside.contains($0.toId) })
                let rects = columns(graph)
                let minX = rects.map(\.x).min() ?? 0
                let minY = rects.map(\.y).min() ?? 0
                for rect in rects { out[rect.id] = Point(x: rect.x - minX, y: rect.y - minY + top) }
                top += (rects.map { $0.y + $0.h }.max() ?? minY) - minY + SceneBuilder.groupGap
            }
            var x = 0.0
            var row = 0.0
            for key in order where (groups[key]?.count ?? 0) == 1 {
                guard let id = groups[key]?.first, let size = sizes[id] else { continue }
                if x > 0, x + size.w > (options.tall ? SceneBuilder.tallLooseWidth : SceneBuilder.looseWidth) {
                    x = 0
                    top += row + SceneBuilder.looseGap
                    row = 0
                }
                out[id] = Point(x: x, y: top)
                x += size.w + SceneBuilder.looseGap
                row = max(row, size.h)
            }
            return out
        }

        func member(_ id: String, _ level: Set<String>) -> String? {
            var current: String? = id
            while let next = current {
                if level.contains(next) { return next }
                current = byID[next]?.parent
            }
            return nil
        }

        func columns(_ graph: Graph) -> [LayoutRect] {
            var input = graph
            if options.tall {
                input.nodes = graph.nodes.map { node in
                    var turned = node
                    turned.size = node.size.map { Size(w: $0.h, h: $0.w) }
                    return turned
                }
            }
            let rects = SceneBuilder.packed(GraphLayout.buildLayout(input).nodes.filter { $0.terminal == nil }, wires: input.wires)
            guard options.tall else { return rects }
            return SceneBuilder.wrapped(rects.map { LayoutRect(id: $0.id, x: $0.y, y: $0.x, w: $0.h, h: $0.w) })
        }

        func finish(_ top: Size) -> GraphScene {
            var scene = GraphScene()
            var absolute: [String: Point] = [:]
            // Parents before children, because a child's place depends on its frame's.
            func locate(_ id: String) -> Point {
                if let known = absolute[id] { return known }
                var point = relative[id] ?? Point(x: 0, y: 0)
                if let frame = container(id) {
                    let origin = locate(frame)
                    point = Point(x: origin.x + point.x + SceneBuilder.framePad, y: origin.y + point.y + SceneBuilder.frameHeader)
                }
                absolute[id] = point
                return point
            }
            func corner(_ id: String) -> Point {
                let point = locate(id)
                guard let shift = shifts[id] else { return point }
                return Point(x: point.x + shift.x, y: point.y + shift.y)
            }
            let on = Set(visible)
            var counts: [String: Int] = [:]
            func under(_ id: String) -> Int {
                if let known = counts[id] { return known }
                let total = (children[id] ?? []).reduce(0) { $0 + 1 + under($1) }
                counts[id] = total
                return total
            }
            func missing(_ id: String) -> Int {
                (children[id] ?? []).reduce(0) { $0 + (on.contains($1) ? missing($1) : 1 + under($1)) }
            }
            var shown: [ShownNode] = []
            for id in visible {
                guard let node = byID[id] else { continue }
                let point = corner(id)
                let size = sizes[id] ?? node.size
                let opensAny = !(children[id] ?? []).isEmpty
                let open = opensAny && (children[id] ?? []).contains { on.contains($0) }
                shown.append(ShownNode(id: id, box: Box(id: id, x: point.x, y: point.y, w: size.w, h: size.h), depth: depths[id] ?? 0,
                                       isFrame: node.frame && open, canExpand: opensAny, expanded: open, hidden: missing(id), container: container(id)))
                // A move sits on top of the seed, so undoing it can return there.
                scene.seeds[id] = options.moved[id] == nil ? relative[id] : options.seeds[id]
            }
            scene.nodes = shown.sorted { a, b in
                let first = a.isFrame ? a.depth : Int.max
                let second = b.isFrame ? b.depth : Int.max
                return first < second
            }
            let boxes = shown.map(\.box)
            let lookup = Dictionary(uniqueKeysWithValues: shown.map { ($0.id, $0.box) })
            let pairs = rolled.map { WireLink(id: $0.id, fromId: $0.a, toId: $0.b) }
            let auto = Routing.assignSides(boxes: boxes, wires: pairs)
            for item in rolled {
                guard let from = lookup[item.a], let to = lookup[item.b] else { continue }
                let pair = options.sides[item.id] ?? auto[item.id] ?? SidePair(from: .right, to: .left)
                scene.sides[item.id] = pair
                // What holds either end can't be in the way of its wire.
                let holders = ancestors(item.a).union(ancestors(item.b))
                let route = Routing.routeWire(from: .node(from), to: .node(to), fromSide: pair.from, toSide: pair.to,
                                              boxes: boxes.filter { !holders.contains($0.id) })
                let direction: WireDirection = item.isLink ? .none : item.forward && item.back ? .both : item.forward ? .forward : item.back ? .back : .none
                scene.wires.append(ShownWire(id: item.id, wires: item.wires, from: item.a, to: item.b, direction: direction, line: item.line,
                                             state: item.state, label: item.label, route: route, isLink: item.isLink))
            }
            if !boxes.isEmpty {
                // Wires count too: a route around the graph mustn't run off the edge.
                let corners = scene.wires.flatMap { wire in
                    wire.route.path.compactMap { command -> Point? in
                        switch command {
                        case .move(let point), .line(let point): point
                        case .quad(_, let point): point
                        }
                    }
                }
                let minX = min(boxes.map(\.x).min() ?? 0, corners.map(\.x).min() ?? .infinity)
                let minY = min(boxes.map(\.y).min() ?? 0, corners.map(\.y).min() ?? .infinity)
                let maxX = max(boxes.map { $0.x + $0.w }.max() ?? 0, corners.map(\.x).max() ?? -.infinity)
                let maxY = max(boxes.map { $0.y + $0.h }.max() ?? 0, corners.map(\.y).max() ?? -.infinity)
                scene.bounds = Obstacle(x: minX, y: minY, w: maxX - minX, h: maxY - minY)
            }
            return scene
        }
    }
}
