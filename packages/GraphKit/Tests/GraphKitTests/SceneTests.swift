import Testing
@testable import GraphKit

@Suite
struct SceneTests {
    private let card = Size(w: 192, h: 96)

    private func node(_ id: String, _ parent: String? = nil, frame: Bool = false, folded: Bool = false) -> SceneNode {
        SceneNode(id: id, parent: parent, size: card, frame: frame, folded: folded)
    }

    /// Linus delegates to a worker that uses Safari with two tabs; a second bot works alone.
    private var nodes: [SceneNode] {
        [node("linus"), node("worker", "linus"), node("safari", "worker"), node("tab1", "safari"), node("tab2", "safari"), node("nova")]
    }

    private var wires: [SceneWire] {
        [SceneWire(id: "task", from: "linus", to: "worker", label: "Fix it"),
         SceneWire(id: "use", from: "worker", to: "safari"),
         SceneWire(id: "open", from: "safari", to: "tab1"),
         SceneWire(id: "open2", from: "safari", to: "tab2"),
         SceneWire(id: "ask", from: "nova", to: "tab1")]
    }

    private func ids(_ scene: GraphScene) -> Set<String> { Set(scene.nodes.map(\.id)) }

    @Test func compactStopsAtOneStep() {
        let scene = SceneBuilder.build(nodes: nodes, wires: wires, options: SceneOptions(mode: .compact))
        #expect(ids(scene) == ["linus", "worker", "nova"])
        let worker = scene.nodes.first { $0.id == "worker" }
        #expect(worker?.canExpand == true && worker?.expanded == false && worker?.hidden == 3)
    }

    @Test func fullShowsEverything() {
        let scene = SceneBuilder.build(nodes: nodes, wires: wires, options: SceneOptions(mode: .full))
        #expect(ids(scene) == Set(nodes.map(\.id)))
        let hidden = scene.nodes.map(\.hidden).reduce(0, +)
        #expect(hidden == 0)
    }

    @Test func clickingOpensOneLevelAtATime() {
        let opened = SceneBuilder.build(nodes: nodes, wires: wires, options: SceneOptions(mode: .compact, toggled: ["worker"]))
        #expect(ids(opened) == ["linus", "worker", "safari", "nova"])
        let closed = SceneBuilder.build(nodes: nodes, wires: wires, options: SceneOptions(mode: .full, toggled: ["safari"]))
        #expect(ids(closed) == ["linus", "worker", "safari", "nova"])
        let safari = closed.nodes.first { $0.id == "safari" }
        #expect(safari?.hidden == 2)
    }

    @Test func foldedNodesStartClosedInCompactOnly() {
        let nodes = [node("github", folded: true), node("pull", "github")]
        #expect(ids(SceneBuilder.build(nodes: nodes, wires: [], options: SceneOptions(mode: .compact))) == ["github"])
        #expect(ids(SceneBuilder.build(nodes: nodes, wires: [], options: SceneOptions(mode: .full))) == ["github", "pull"])
    }

    @Test func hiddenWiresRollUpToWhatsOnShow() throws {
        let scene = SceneBuilder.build(nodes: nodes, wires: wires, options: SceneOptions(mode: .compact))
        let ask = try #require(scene.wires.first { $0.wires == ["ask"] })
        #expect(Set([ask.from, ask.to]) == ["nova", "worker"])
        let inner = scene.wires.contains { $0.wires.contains("open") }
        #expect(!inner, "a wire inside a closed node has no ends to draw")
    }

    @Test func eachWayBetweenAPairKeepsItsOwnLine() throws {
        let wires = [SceneWire(id: "task", from: "a", to: "b", state: .active, label: "Do it"),
                     SceneWire(id: "result", from: "b", to: "a", label: "Done")]
        let scene = SceneBuilder.build(nodes: [node("a"), node("b")], wires: wires)
        #expect(scene.wires.count == 2)
        let task = try #require(scene.wires.first { $0.wires == ["task"] })
        let result = try #require(scene.wires.first { $0.wires == ["result"] })
        #expect(task.from == "a" && task.label == "Do it" && task.state == .active)
        #expect(result.from == "b" && result.label == "Done")
        #expect(task.route.anchor != result.route.anchor, "the two directions don't draw over each other")
    }

    @Test func wiresTheSameWayBecomeOneLine() throws {
        let wires = [SceneWire(id: "one", from: "a", to: "b", label: "First"), SceneWire(id: "two", from: "a", to: "b", label: "Second")]
        let scene = SceneBuilder.build(nodes: [node("a"), node("b")], wires: wires)
        let line = try #require(scene.wires.first)
        #expect(scene.wires.count == 1 && line.wires == ["one", "two"] && line.label == nil && line.direction == .forward)
    }

    @Test func aWireToItsParentReplacesTheLink() {
        let scene = SceneBuilder.build(nodes: [node("safari"), node("tab", "safari")],
                                       wires: [SceneWire(id: "w", from: "tab", to: "safari")], options: SceneOptions(mode: .full))
        #expect(scene.wires.count == 1 && !scene.wires[0].isLink)
    }

    @Test func directionSurvivesEitherOrderOfEnds() {
        for (from, to) in [("a", "b"), ("b", "a")] {
            let scene = SceneBuilder.build(nodes: [node("a"), node("b")], wires: [SceneWire(id: "w", from: from, to: to)])
            let line = scene.wires[0]
            let tip = line.direction == .forward ? line.to : line.direction == .back ? line.from : ""
            #expect(tip == to)
        }
    }

    @Test func undirectedWiresStayUndirected() {
        let scene = SceneBuilder.build(nodes: [node("a"), node("b")], wires: [SceneWire(id: "w", from: "a", to: "b", direction: .none, line: .dashed)])
        #expect(scene.wires[0].direction == .none && scene.wires[0].line == .dashed)
    }

    @Test func childrenBesideAParentStayLinked() throws {
        let scene = SceneBuilder.build(nodes: [node("safari"), node("tab", "safari")], wires: [], options: SceneOptions(mode: .full))
        let link = try #require(scene.wires.first)
        #expect(link.isLink && link.direction == .none && link.wires.isEmpty)
    }

    @Test func framesHoldTheirChildren() throws {
        let nodes = [node("cvmini", frame: true), node("server", "cvmini"), node("repo", "cvmini"), node("phone")]
        let wires = [SceneWire(id: "sync", from: "phone", to: "server", direction: .both), SceneWire(id: "serve", from: "server", to: "repo")]
        let scene = SceneBuilder.build(nodes: nodes, wires: wires, options: SceneOptions(mode: .full))
        let frame = try #require(scene.nodes.first { $0.id == "cvmini" })
        #expect(scene.nodes.first?.id == "cvmini", "frames paint first")
        #expect(frame.isFrame)
        for id in ["server", "repo"] {
            let child = try #require(scene.nodes.first { $0.id == id }).box
            #expect(child.x >= frame.box.x && child.y >= frame.box.y + SceneBuilder.frameHeader - 1)
            #expect(child.x + child.w <= frame.box.x + frame.box.w && child.y + child.h <= frame.box.y + frame.box.h)
        }
        let linked = scene.wires.contains(where: \.isLink)
        #expect(!linked, "a frame already shows what it holds")
        let phone = try #require(scene.nodes.first { $0.id == "phone" }).box
        let overlapX = min(phone.x + phone.w, frame.box.x + frame.box.w) - max(phone.x, frame.box.x)
        let overlapY = min(phone.y + phone.h, frame.box.y + frame.box.h) - max(phone.y, frame.box.y)
        #expect(overlapX <= 0 || overlapY <= 0)
    }

    @Test func draggingAFramesChildLeavesItsSiblingsPut() throws {
        let nodes = [node("cvmini", frame: true), node("server", "cvmini"), node("repo", "cvmini")]
        let before = SceneBuilder.build(nodes: nodes, wires: [], options: SceneOptions(mode: .full))
        var moved = before.seeds
        moved["server"] = Point(x: -200, y: -100)
        let after = SceneBuilder.build(nodes: nodes, wires: [], options: SceneOptions(mode: .full, moved: moved))
        let stayed = try #require(before.nodes.first { $0.id == "repo" })
        let still = try #require(after.nodes.first { $0.id == "repo" })
        #expect(stayed.box == still.box)
        let frame = try #require(after.nodes.first { $0.id == "cvmini" }).box
        let server = try #require(after.nodes.first { $0.id == "server" }).box
        #expect(server.x >= frame.x && server.y >= frame.y)
    }

    @Test func aClosedFrameIsAnOrdinaryNode() throws {
        let nodes = [node("cvmini", frame: true), node("server", "cvmini")]
        let scene = SceneBuilder.build(nodes: nodes, wires: [], options: SceneOptions(mode: .full, toggled: ["cvmini"]))
        let frame = try #require(scene.nodes.first)
        #expect(!frame.isFrame && frame.box.size == card && frame.hidden == 1)
    }

    @Test func nothingOverlapsAtFirstLayout() {
        let scene = SceneBuilder.build(nodes: nodes, wires: wires, options: SceneOptions(mode: .full))
        for (index, a) in scene.nodes.enumerated() {
            for b in scene.nodes[(index + 1)...] {
                let apart = a.box.x + a.box.w <= b.box.x || b.box.x + b.box.w <= a.box.x
                    || a.box.y + a.box.h <= b.box.y || b.box.y + b.box.h <= a.box.y
                #expect(apart, "\(a.id) and \(b.id)")
            }
        }
    }

    @Test func seedsKeepNodesPutWhenTheGraphGrows() throws {
        let first = SceneBuilder.build(nodes: [node("a"), node("b")], wires: [SceneWire(id: "w", from: "a", to: "b")])
        var options = SceneOptions(seeds: first.seeds)
        options.sides = first.sides
        let grown = SceneBuilder.build(nodes: [node("new"), node("a"), node("b")],
                                       wires: [SceneWire(id: "w", from: "a", to: "b"), SceneWire(id: "x", from: "new", to: "a")], options: options)
        for id in ["a", "b"] {
            let before = first.nodes.first { $0.id == id }
            let after = grown.nodes.first { $0.id == id }
            #expect(before?.box == after?.box)
        }
        let added = try #require(grown.nodes.first { $0.id == "new" }).box
        let overlapping = grown.nodes.filter { $0.id != "new" }.contains { other in
            added.x < other.box.x + other.box.w && added.x + added.w > other.box.x
                && added.y < other.box.y + other.box.h && added.y + added.h > other.box.y
        }
        #expect(!overlapping)
    }

    @Test func tallLayoutsRunTopToBottomWithoutOverlaps() {
        let chain = [node("a"), node("b"), node("c")]
        let links = [SceneWire(id: "1", from: "a", to: "b"), SceneWire(id: "2", from: "b", to: "c")]
        let scene = SceneBuilder.build(nodes: chain, wires: links, options: SceneOptions(tall: true))
        let boxes = ["a", "b", "c"].compactMap { id in scene.nodes.first { $0.id == id }?.box }
        #expect(boxes.count == 3)
        #expect(boxes[0].y + boxes[0].h <= boxes[1].y && boxes[1].y + boxes[1].h <= boxes[2].y)
        #expect(scene.bounds.h > scene.bounds.w)
    }

    @Test func tallFanOutsWrapRatherThanSpread() {
        let fan = [node("hub")] + (1...5).map { node("leaf\($0)") }
        let links = (1...5).map { SceneWire(id: "w\($0)", from: "hub", to: "leaf\($0)") }
        let scene = SceneBuilder.build(nodes: fan, wires: links, options: SceneOptions(tall: true))
        let rows = Dictionary(grouping: scene.nodes) { $0.box.y }
        #expect(rows.values.map(\.count).max() == 2)
        let right = scene.nodes.map { $0.box.x + $0.box.w }.max() ?? 0
        #expect(right <= card.w * 2 + SceneBuilder.tallGap)
    }

    @Test func tallColumnsLineUpSoWiresKeepAGutter() {
        let wide = SceneNode(id: "b", size: Size(w: card.w + 60, h: card.h))
        let items = [node("hub"), wide] + (1...3).map { node("leaf\($0)") }
        let links = (1...4).map { SceneWire(id: "w\($0)", from: "hub", to: $0 == 1 ? "b" : "leaf\($0 - 1)") }
        let scene = SceneBuilder.build(nodes: items, wires: links, options: SceneOptions(tall: true))
        let rows = Dictionary(grouping: scene.nodes) { $0.box.y }.values.filter { $0.count == 2 }
        let starts = Set(rows.map { $0.map(\.box.x).max() ?? 0 })
        #expect(starts.count == 1)
        let left = rows.flatMap { $0 }.filter { !starts.contains($0.box.x) }.map { $0.box.x + $0.box.w }.max() ?? 0
        #expect((starts.first ?? 0) >= left + SceneBuilder.tallGap)
    }

    @Test func aMoveLeavesTheSeedToReturnTo() {
        let seed = Point(x: 0, y: 0)
        let options = SceneOptions(moved: ["a": Point(x: 900, y: 40)], seeds: ["a": seed])
        let scene = SceneBuilder.build(nodes: [SceneNode(id: "a", size: Size(w: 100, h: 60))], wires: [], options: options)
        #expect(scene.seeds["a"] == seed)
    }

    @Test func movedNodesWinOverSeeds() {
        let options = SceneOptions(moved: ["a": Point(x: 900, y: 40)], seeds: ["a": Point(x: 0, y: 0)])
        let scene = SceneBuilder.build(nodes: [node("a")], wires: [], options: options)
        #expect(scene.nodes[0].box.position == Point(x: 900, y: 40))
    }

    @Test func circularParentsDoNotHang() {
        let scene = SceneBuilder.build(nodes: [node("a", "b"), node("b", "a"), node("c", "missing")], wires: [])
        #expect(ids(scene) == ["a", "b", "c"])
    }

    @Test func routesFollowFrozenSides() {
        let options = SceneOptions(sides: ["w": SidePair(from: .top, to: .top)])
        let scene = SceneBuilder.build(nodes: [node("a"), node("b")], wires: [SceneWire(id: "w", from: "a", to: "b")], options: options)
        #expect(scene.sides["w"] == SidePair(from: .top, to: .top))
    }

    @Test func wireSidesFollowTravelRatherThanNames() {
        // Milo and Nova sort after Linus, which used to turn their wires around.
        let names = ["aria", "nova", "milo", "fern", "chief"]
        let links = names.map { SceneWire(id: $0, from: $0, to: "linus") }
        let scene = SceneBuilder.build(nodes: (names + ["linus"]).map { node($0) }, wires: links, options: SceneOptions(mode: .full))
        #expect(scene.wires.allSatisfy { $0.to == "linus" && $0.direction == .forward })
        #expect(scene.sides.values.allSatisfy { $0.from != .left && $0.to != .right })
    }

    @Test func fanOutsStayCentredOnTheirHub() throws {
        let fan = [node("hub")] + (1...5).map { node("leaf\($0)") }
        let links = (1...5).map { SceneWire(id: "w\($0)", from: "hub", to: "leaf\($0)") }
        let scene = SceneBuilder.build(nodes: fan, wires: links, options: SceneOptions(mode: .full))
        let hub = try #require(scene.nodes.first { $0.id == "hub" }).box
        let leaves = scene.nodes.filter { $0.id != "hub" }.map(\.box)
        let top = try #require(leaves.map(\.y).min())
        let bottom = try #require(leaves.map { $0.y + $0.h }.max())
        #expect(abs(hub.y + hub.h / 2 - (top + bottom) / 2) <= 1)
    }
}
