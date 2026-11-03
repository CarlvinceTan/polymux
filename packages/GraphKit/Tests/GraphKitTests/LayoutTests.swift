import Testing
@testable import GraphKit

// Ported from the old canvas's `workflowLayout.test.mjs`.
@Suite
struct LayoutTests {
    @Test func estimateNodeSizeGrowsForLongTitles() {
        #expect(Metrics.estimateNodeSize(GraphNode(id: "a", title: "Hi")).w == Metrics.nodeW)
        let long = Metrics.estimateNodeSize(GraphNode(id: "b", title: "A much longer workflow step title that should expand the card"))
        #expect(long.w > Metrics.nodeW)
        #expect(long.w.truncatingRemainder(dividingBy: 48) == 0)
    }

    @Test func clampKeepsRealNodesOffTerminalColumns() {
        let nodes = [
            NodeModel(id: GraphLayout.startID, node: GraphNode(id: GraphLayout.startID), col: 0, row: 0, terminal: .start),
            NodeModel(id: "a", node: GraphNode(id: "a"), col: 0, row: 1),
            NodeModel(id: "b", node: GraphNode(id: "b"), col: 4, row: 0),
            NodeModel(id: GraphLayout.endID, node: GraphNode(id: GraphLayout.endID), col: 4, row: 0, terminal: .end),
        ]
        Metrics.clampIntermediateColumns(nodes, maxLayer: 3)
        #expect(nodes.map(\.col) == [0, 1, 3, 4])
    }

    @Test func enforceClampsCentresBetweenTerminals() throws {
        var rects = [
            LayoutRect(id: GraphLayout.startID, x: 0, y: 0, w: Metrics.terminalW, h: 48, terminal: .start),
            LayoutRect(id: "left", x: -200, y: 0, w: Metrics.nodeW, h: 96),
            LayoutRect(id: "right", x: 900, y: 0, w: Metrics.nodeW, h: 96),
            LayoutRect(id: GraphLayout.endID, x: 800, y: 0, w: Metrics.terminalW, h: 48, terminal: .end),
        ]
        Metrics.enforceIntermediateHorizontalBetween(&rects)
        let start = rects[0].x + rects[0].w / 2
        let end = rects[3].x + rects[3].w / 2
        for rect in rects[1...2] {
            let center = rect.x + rect.w / 2
            #expect(center > start && center < end)
        }
    }

    @Test func resolveOverlapsSeparatesStackedNodes() {
        var rects = [
            LayoutRect(id: "a", x: 100, y: 100, w: Metrics.nodeW, h: 96),
            LayoutRect(id: "b", x: 120, y: 130, w: Metrics.nodeW, h: 96),
        ]
        Metrics.resolveLayoutOverlaps(&rects)
        let overlap = rects[0].x < rects[1].x + rects[1].w && rects[0].x + rects[0].w > rects[1].x
            && rects[0].y < rects[1].y + rects[1].h && rects[0].y + rects[0].h > rects[1].y
        #expect(!overlap)
    }

    @Test func autoLayoutPlacesFanOutBetweenStartAndEnd() throws {
        let graph = Graph(
            nodes: [GraphNode(id: "source", title: "Source"), GraphNode(id: "upper", title: "Upper"), GraphNode(id: "lower", title: "Lower")],
            wires: [GraphWire(id: "w1", fromId: "source", toId: "upper"), GraphWire(id: "w2", fromId: "source", toId: "lower")])
        let geometry = GraphLayout.buildAutoLayoutGeometry(graph)
        let start = try #require(geometry.first { $0.id == GraphLayout.startID })
        let end = try #require(geometry.first { $0.id == GraphLayout.endID })
        for node in graph.nodes {
            let rect = try #require(geometry.first { $0.id == node.id })
            let center = rect.x + rect.w / 2
            #expect(center > start.x + start.w / 2)
            #expect(center < end.x + end.w / 2)
            #expect(rect.w >= Metrics.nodeW)
        }
    }

    @Test func wiresAgainstTheFlowBecomeBackWires() {
        let graph = Graph(
            nodes: [GraphNode(id: "a"), GraphNode(id: "b")],
            wires: [GraphWire(id: "go", fromId: "a", toId: "b"), GraphWire(id: "again", fromId: "b", toId: "a", maxIterations: 2)])
        #expect(GraphLayout.buildLayout(graph).wires.map(\.style) == [.forward, .backWire])
    }
}
