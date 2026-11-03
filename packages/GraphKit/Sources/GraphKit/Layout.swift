import Foundation

public struct GraphNode: Hashable, Sendable, Codable {
    public var id: String
    public var title: String?
    public var actions: [String]?
    public var iterations: Int?
    /// Replaces the estimate from the title when a caller knows the real size.
    public var size: Size?

    public init(id: String, title: String? = nil, actions: [String]? = nil, iterations: Int? = nil, size: Size? = nil) {
        self.id = id
        self.title = title
        self.actions = actions
        self.iterations = iterations
        self.size = size
    }
}

public struct GraphWire: Hashable, Sendable {
    public var id: String
    public var fromId: String
    public var toId: String
    public var label: String?
    public var condition: String?
    public var maxIterations: Int?

    public init(id: String, fromId: String, toId: String, label: String? = nil, condition: String? = nil, maxIterations: Int? = nil) {
        self.id = id
        self.fromId = fromId
        self.toId = toId
        self.label = label
        self.condition = condition
        self.maxIterations = maxIterations
    }
}

public struct Graph: Hashable, Sendable {
    public var nodes: [GraphNode]
    public var wires: [GraphWire]

    public init(nodes: [GraphNode] = [], wires: [GraphWire] = []) {
        self.nodes = nodes
        self.wires = wires
    }
}

public enum Terminal: String, Sendable, Codable {
    case start, end
}

public enum WireStyle: String, Sendable, Codable {
    case forward
    case backWire = "back-wire"
}

/// Shared by reference: fan-out symmetry and column clamping adjust nodes in place.
public final class NodeModel {
    public let id: String
    public let node: GraphNode
    public var col: Double
    public var row: Double
    public let terminal: Terminal?

    public init(id: String, node: GraphNode, col: Double, row: Double, terminal: Terminal? = nil) {
        self.id = id
        self.node = node
        self.col = col
        self.row = row
        self.terminal = terminal
    }
}

public struct WireModel: Hashable, Sendable {
    public var id: String
    public var fromId: String
    public var toId: String
    public var style: WireStyle
    public var label: String?
    public var condition: String?
    public var maxIterations: Int?
}

public struct LayoutModel {
    public var columnCount: Int
    public var rowCount: Int
    public var nodes: [NodeModel]
    public var wires: [WireModel]
}

public struct LayoutRect: Hashable, Sendable {
    public var id: String
    public var x: Double
    public var y: Double
    public var w: Double
    public var h: Double
    public var terminal: Terminal?

    public init(id: String, x: Double, y: Double, w: Double, h: Double, terminal: Terminal? = nil) {
        self.id = id
        self.x = x
        self.y = y
        self.w = w
        self.h = h
        self.terminal = terminal
    }
}

/// Column layout by longest forward path, ported directly from `useWorkflowLayout.ts`.
public enum GraphLayout {
    public static let startID = "__workflow_start__"
    public static let endID = "__workflow_end__"

    /// Capped wires don't count, and cycles break at the node with the fewest open inputs.
    private static func assignLayers(_ nodes: [GraphNode], _ wires: [GraphWire]) -> [String: Int] {
        var incoming: [String: Int] = [:]
        var outgoing: [String: [String]] = [:]
        for node in nodes {
            incoming[node.id] = 0
            outgoing[node.id] = []
        }
        for wire in wires where (wire.maxIterations ?? 0) <= 0 {
            if wire.fromId.isEmpty || wire.toId.isEmpty { continue }
            if outgoing[wire.fromId] == nil { continue }
            incoming[wire.toId, default: 0] += 1
            outgoing[wire.fromId]?.append(wire.toId)
        }

        var layer: [String: Int] = [:]
        var order: [String] = []
        var remaining = Set<String>()
        for node in nodes where remaining.insert(node.id).inserted {
            order.append(node.id)
        }
        var current = 1

        while !remaining.isEmpty {
            order.removeAll { !remaining.contains($0) }
            var ready = order.filter { (incoming[$0] ?? 0) == 0 }
            if ready.isEmpty {
                var fewest = Int.max
                var pick: String?
                for id in order where (incoming[id] ?? 0) < fewest {
                    fewest = incoming[id] ?? 0
                    pick = id
                }
                guard let pick else { break }
                ready = [pick]
            }
            for id in ready {
                layer[id] = current
                remaining.remove(id)
                for target in outgoing[id] ?? [] where remaining.contains(target) {
                    incoming[target, default: 0] -= 1
                }
            }
            current += 1
        }
        for node in nodes where layer[node.id] == nil {
            layer[node.id] = 1
        }
        return layer
    }

    private static func sideFromLayout(_ from: NodeModel, _ to: NodeModel) -> Side? {
        if to.col > from.col { return .right }
        if to.col < from.col { return .left }
        if to.row > from.row { return .bottom }
        if to.row < from.row { return .top }
        return nil
    }

    /// Centres each source's single-parent targets around it.
    private static func preferFanoutSymmetry(_ placement: [String: NodeModel], _ wires: [GraphWire], _ nodeOrder: [String: Int]) {
        var forwardIncoming: [String: Int] = [:]
        for wire in wires {
            if wire.fromId.isEmpty || wire.toId.isEmpty || (wire.maxIterations ?? 0) > 0 { continue }
            if placement[wire.fromId] == nil || placement[wire.toId] == nil { continue }
            forwardIncoming[wire.toId, default: 0] += 1
        }

        // Sides keep first-seen order, because later groups see earlier moves.
        var groups: [String: [(side: Side, targets: [NodeModel])]] = [:]
        var seen = Set<String>()
        for wire in wires {
            if wire.fromId.isEmpty || wire.toId.isEmpty || (wire.maxIterations ?? 0) > 0 { continue }
            if (forwardIncoming[wire.toId] ?? 0) > 1 { continue }
            guard let from = placement[wire.fromId], let to = placement[wire.toId], from.id != to.id,
                  let side = sideFromLayout(from, to) else { continue }
            if !seen.insert("\(from.id):\(side):\(to.id)").inserted { continue }
            if let index = groups[from.id]?.firstIndex(where: { $0.side == side }) {
                groups[from.id]?[index].targets.append(to)
            } else {
                groups[from.id, default: []].append((side, [to]))
            }
        }

        let order = { (id: String) in nodeOrder[id] ?? 0 }
        let sources = groups.keys.sorted { a, b in
            guard let sourceA = placement[a], let sourceB = placement[b] else { return order(a) < order(b) }
            if sourceA.col != sourceB.col { return sourceA.col < sourceB.col }
            if sourceA.row != sourceB.row { return sourceA.row < sourceB.row }
            return order(a) < order(b)
        }

        for sourceID in sources {
            guard let source = placement[sourceID], let sides = groups[sourceID] else { continue }
            for group in sides where group.targets.count >= 2 {
                let horizontal = group.side == .left || group.side == .right
                let targets = group.targets.sorted { a, b in
                    let delta = horizontal ? a.row - b.row : a.col - b.col
                    return delta != 0 ? delta < 0 : order(a.id) < order(b.id)
                }
                for (index, target) in targets.enumerated() {
                    let offset = Double(index) - Double(targets.count - 1) / 2
                    if horizontal {
                        target.row = source.row + offset
                    } else {
                        target.col = source.col + offset
                    }
                }
            }
        }
    }

    public static func buildLayout(_ graph: Graph) -> LayoutModel {
        var nodes: [NodeModel] = []
        var wires: [WireModel] = []
        let nodeOrder = Dictionary(graph.nodes.enumerated().map { ($0.element.id, $0.offset) }, uniquingKeysWith: { _, last in last })

        let layer = assignLayers(graph.nodes, graph.wires)
        var byLayer: [Int: [GraphNode]] = [:]
        for node in graph.nodes {
            byLayer[layer[node.id] ?? 1, default: []].append(node)
        }

        nodes.append(NodeModel(id: startID, node: GraphNode(id: startID), col: 0, row: 0, terminal: .start))

        let maxLayer = byLayer.keys.max() ?? 0
        var placement: [String: NodeModel] = [:]
        for key in byLayer.keys.sorted() {
            for (row, node) in (byLayer[key] ?? []).enumerated() {
                let model = NodeModel(id: node.id, node: node, col: Double(key), row: Double(row))
                nodes.append(model)
                placement[node.id] = model
            }
        }

        preferFanoutSymmetry(placement, graph.wires, nodeOrder)
        Metrics.clampIntermediateColumns(nodes, maxLayer: Double(maxLayer))

        let endCol = max(maxLayer + 1, 1)
        nodes.append(NodeModel(id: endID, node: GraphNode(id: endID), col: Double(endCol), row: 0, terminal: .end))

        for wire in graph.wires {
            var back = (wire.maxIterations ?? 0) > 0
            if let from = placement[wire.fromId], let to = placement[wire.toId], to.col <= from.col {
                back = true
            }
            wires.append(WireModel(id: "wire:\(wire.id)", fromId: wire.fromId, toId: wire.toId,
                                   style: back ? .backWire : .forward, label: wire.label,
                                   condition: wire.condition, maxIterations: wire.maxIterations))
        }

        var rowCount = 1.0
        for node in nodes where node.row + 1 > rowCount {
            rowCount = (node.row + 1).rounded(.up)
        }
        return LayoutModel(columnCount: endCol + 1, rowCount: Int(rowCount), nodes: nodes, wires: wires)
    }

    /// Layered layout plus content-sized boxes and overlap separation.
    public static func buildAutoLayoutGeometry(_ graph: Graph) -> [LayoutRect] {
        Metrics.computeAutoLayoutGeometry(buildLayout(graph).nodes)
    }
}
