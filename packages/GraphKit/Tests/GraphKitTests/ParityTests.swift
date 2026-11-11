import Foundation
import Testing
@testable import GraphKit

// `Fixtures/parity.json` holds seeded random inputs and the output of the old canvas's
// TypeScript for them, including the side picker and wire geometry from its Vue file.
private struct Fixture: Decodable {
    struct Route: Decodable {
        let p1, p2: Point
        let fromSide, toSide: Side
        let obstacles: [Obstacle]
        let plan, astar: [Point]?
        let path: String
        let rounded: String?
    }

    struct Wire: Decodable {
        let id: String
        let path: String
        let anchor: Anchor
        let p1, p2: Point
        let fromSide, toSide: Side
    }

    struct Anchor: Decodable {
        let x, y: Double
        let orientation: String
    }

    struct Pair: Decodable {
        let fromSide, toSide: Side
    }

    struct Wiring: Decodable {
        let boxes: [Box]
        let wires: [Link]
        let sides: [String: Pair]
        let routes: [Wire]
    }

    struct Link: Decodable {
        let id, fromId, toId: String
    }

    struct Loose: Decodable {
        let boxes: [Box]
        let wires: [LooseWire]
        let routes: [Wire]
    }

    struct LooseWire: Decodable {
        let id, from_id, to_id: String
        let from_pos, to_pos: Point?
        let from_side, to_side: Side
    }

    struct Layout: Decodable {
        let graph: Input
        let columnCount, rowCount: Int
        let nodes: [Placed]
        let wires: [Styled]
        let geometry: [Rect]
    }

    struct Input: Decodable {
        let nodes: [GraphNode]
        let wires: [InputWire]
    }

    struct InputWire: Decodable {
        let id, from_id, to_id: String
        let max_iterations: Int?
    }

    struct Placed: Decodable {
        let id: String
        let col, row: Double
        let terminal: Terminal?
    }

    struct Styled: Decodable {
        let id: String
        let style: WireStyle
    }

    struct Rect: Decodable {
        let id: String
        let x, y, w, h: Double
    }

    let routes: [Route]
    let wiring: [Wiring]
    let looseEnds: [Loose]
    let layouts: [Layout]

    static func load() throws -> Fixture {
        let url = try #require(Bundle.module.url(forResource: "parity", withExtension: "json", subdirectory: "Fixtures"))
        return try JSONDecoder().decode(Fixture.self, from: Data(contentsOf: url))
    }
}

/// Writes a path the way the reference did, so results compare as exact strings.
private func svg(_ path: [PathCommand]) -> String {
    func n(_ value: Double) -> String {
        if value == 0 { return "0" }
        return value == value.rounded() ? String(format: "%.0f", value) : "\(value)"
    }
    return path.map { command in
        switch command {
        case .move(let p): "M \(n(p.x)) \(n(p.y))"
        case .line(let p): " L \(n(p.x)) \(n(p.y))"
        case .quad(let c, let p): " Q \(n(c.x)) \(n(c.y)), \(n(p.x)) \(n(p.y))"
        }
    }.joined()
}

private func expectRoute(_ route: WireRoute, _ expected: Fixture.Wire, _ context: String, _ location: SourceLocation = #_sourceLocation) {
    #expect(svg(route.path) == expected.path, "\(context) path", sourceLocation: location)
    #expect(route.p1 == expected.p1 && route.p2 == expected.p2, "\(context) ends", sourceLocation: location)
    #expect(route.anchor.x == expected.anchor.x && route.anchor.y == expected.anchor.y, "\(context) anchor", sourceLocation: location)
    #expect((route.anchor.orientation == .horizontal ? "horizontal" : "vertical") == expected.anchor.orientation, "\(context) axis", sourceLocation: location)
}

@Suite
struct ParityTests {
    private let fixture: Fixture

    init() throws {
        fixture = try Fixture.load()
    }

    @Test func routesMatchTheReference() {
        for (index, route) in fixture.routes.enumerated() {
            let plan = Routing.planOrthogonalRoute(route.p1, route.p2, route.fromSide, route.toSide, route.obstacles)
            let grid = Routing.aStarOrthogonalRoute(route.p1, route.p2, route.fromSide, route.toSide, route.obstacles)
            #expect(plan == route.plan, "route \(index) plan")
            #expect(grid == route.astar, "route \(index) A*")
            #expect(svg(Routing.routedWirePath(route.p1, route.p2, route.fromSide, route.toSide, route.obstacles)) == route.path, "route \(index) path")
            if let plan, let rounded = route.rounded {
                #expect(svg(Routing.roundedPolylinePath(plan)) == rounded, "route \(index) rounded")
            }
        }
    }

    @Test func sidesAndWiresMatchTheReference() throws {
        for (index, scene) in fixture.wiring.enumerated() {
            let links = scene.wires.map { WireLink(id: $0.id, fromId: $0.fromId, toId: $0.toId) }
            let sides = Routing.assignSides(boxes: scene.boxes, wires: links)
            #expect(sides.count == scene.sides.count, "scene \(index) side count")
            for (id, expected) in scene.sides {
                #expect(sides[id] == SidePair(from: expected.fromSide, to: expected.toSide), "scene \(index) \(id) sides")
            }
            let boxes = Dictionary(scene.boxes.map { ($0.id, $0) }, uniquingKeysWith: { _, last in last })
            #expect(links.count == scene.routes.count, "scene \(index) route count")
            for (link, expected) in zip(links, scene.routes) {
                let from = try #require(boxes[link.fromId])
                let to = try #require(boxes[link.toId])
                let route = Routing.routeWire(from: .node(from), to: .node(to), fromSide: expected.fromSide, toSide: expected.toSide, boxes: scene.boxes)
                expectRoute(route, expected, "scene \(index) \(link.id)")
            }
        }
    }

    @Test func looseEndsMatchTheReference() throws {
        for (index, scene) in fixture.looseEnds.enumerated() {
            let boxes = Dictionary(scene.boxes.map { ($0.id, $0) }, uniquingKeysWith: { _, last in last })
            func end(_ id: String, _ position: Point?) throws -> WireEnd {
                if let box = boxes[id] { return .node(box) }
                return .loose(try #require(position))
            }
            #expect(scene.wires.count == scene.routes.count, "loose \(index) route count")
            for (wire, expected) in zip(scene.wires, scene.routes) {
                let route = Routing.routeWire(from: try end(wire.from_id, wire.from_pos), to: try end(wire.to_id, wire.to_pos),
                                              fromSide: wire.from_side, toSide: wire.to_side, boxes: scene.boxes)
                expectRoute(route, expected, "loose \(index) \(wire.id)")
            }
        }
    }

    @Test func layoutsMatchTheReference() {
        for (index, scene) in fixture.layouts.enumerated() {
            let graph = Graph(nodes: scene.graph.nodes, wires: scene.graph.wires.map {
                GraphWire(id: $0.id, fromId: $0.from_id, toId: $0.to_id, maxIterations: $0.max_iterations)
            })
            let layout = GraphLayout.buildLayout(graph)
            #expect(layout.columnCount == scene.columnCount, "layout \(index) columns")
            #expect(layout.rowCount == scene.rowCount, "layout \(index) rows")
            #expect(layout.nodes.map(\.id) == scene.nodes.map(\.id), "layout \(index) order")
            #expect(layout.nodes.map(\.col) == scene.nodes.map(\.col), "layout \(index) col")
            #expect(layout.nodes.map(\.row) == scene.nodes.map(\.row), "layout \(index) row")
            #expect(layout.nodes.map(\.terminal) == scene.nodes.map(\.terminal), "layout \(index) terminals")
            #expect(layout.wires.map(\.style) == scene.wires.map(\.style), "layout \(index) styles")
            let geometry = GraphLayout.buildAutoLayoutGeometry(graph)
            #expect(geometry.map(\.x) == scene.geometry.map(\.x), "layout \(index) x")
            #expect(geometry.map(\.y) == scene.geometry.map(\.y), "layout \(index) y")
            #expect(geometry.map(\.w) == scene.geometry.map(\.w), "layout \(index) w")
            #expect(geometry.map(\.h) == scene.geometry.map(\.h), "layout \(index) h")
        }
    }
}
