import Foundation

public struct WireLink: Hashable, Sendable {
    public var id: String
    public var fromId: String
    public var toId: String

    public init(id: String, fromId: String, toId: String) {
        self.id = id
        self.fromId = fromId
        self.toId = toId
    }
}

public struct SidePair: Hashable, Sendable, Codable {
    public var from: Side
    public var to: Side

    public init(from: Side, to: Side) {
        self.from = from
        self.to = to
    }
}

public enum WireEnd: Hashable, Sendable {
    case node(Box)
    case loose(Point)
}

/// Where a wire's badge sits: the middle of its longest straight segment.
public struct WireAnchor: Hashable, Sendable {
    public var x: Double
    public var y: Double
    public var orientation: WireAxis
}

public struct WireRoute: Hashable, Sendable {
    public var p1: Point
    public var p2: Point
    public var path: [PathCommand]
    public var anchor: WireAnchor
}

/// Ported directly from `wireSidesMap` and `wireGeoms` in the old canvas.
extension Routing {
    public static func wireDrawnEnd(_ sideCenter: Point, _ side: Side) -> Point {
        let n = outwardNormal(side)
        return Point(x: sideCenter.x + n.x * wireEndGap, y: sideCenter.y + n.y * wireEndGap)
    }

    /// Picks each wire's sides: feasibility, then obstacles, alignment, bends and distance.
    public static func assignSides(boxes: [Box], wires: [WireLink]) -> [String: SidePair] {
        let nodes = Dictionary(boxes.map { ($0.id, $0) }, uniquingKeysWith: { _, last in last })
        var result: [String: SidePair] = [:]
        var inSides: [String: Set<Side>] = [:]
        var outSides: [String: Set<Side>] = [:]

        func strength(_ wire: WireLink) -> Double {
            guard let a = nodes[wire.fromId], let b = nodes[wire.toId] else { return 0 }
            let dx = abs(b.center.x - a.center.x)
            let dy = abs(b.center.y - a.center.y)
            return max(dx, dy) / max(1, min(dx, dy))
        }

        // Straight connections choose first, since they prefer their primary side most.
        let ordered = wires.enumerated()
            .filter { nodes[$0.element.fromId] != nil && nodes[$0.element.toId] != nil }
            .map { (index: $0.offset, wire: $0.element, strength: strength($0.element)) }
            .sorted { $0.strength != $1.strength ? $0.strength > $1.strength : $0.index < $1.index }

        for (_, wire, _) in ordered {
            guard let a = nodes[wire.fromId], let b = nodes[wire.toId] else { continue }
            let dx = b.center.x - a.center.x
            let dy = b.center.y - a.center.y
            let aRanking = rankSides(dx, dy)
            let bRanking = rankSides(-dx, -dy)

            var aCandidates = allSides.filter { !(inSides[a.id]?.contains($0) ?? false) }
            var bCandidates = allSides.filter { !(outSides[b.id]?.contains($0) ?? false) }
            if aCandidates.isEmpty { aCandidates = allSides }
            if bCandidates.isEmpty { bCandidates = allSides }

            let xOverlap = min(a.x + a.w, b.x + b.w) - max(a.x, b.x) > 0
            let yOverlap = min(a.y + a.h, b.y + b.h) - max(a.y, b.y) > 0
            let dominant: WireAxis = if xOverlap && !yOverlap {
                .vertical
            } else if yOverlap && !xOverlap {
                .horizontal
            } else {
                abs(dx) >= abs(dy) ? .horizontal : .vertical
            }
            let alignmentWeight = xOverlap != yOverlap ? 2e9 : 1.5e9

            var bestScore = Double.infinity
            var bestFrom = aCandidates[0]
            var bestTo = bCandidates[0]

            for fromSide in aCandidates {
                let p1 = pointOnSide(a.position, fromSide, a.size)
                for toSide in bCandidates {
                    let p2 = pointOnSide(b.position, toSide, b.size)
                    let info = pathInfo(p1, p2, fromSide, toSide)
                    let ex = p2.x - p1.x
                    let ey = p2.y - p1.y
                    let dist2 = ex * ex + ey * ey
                    var crossings = 0.0
                    if boxes.count > 2 {
                        let samples = wireWaypoints(p1, p2, fromSide, toSide)
                        for box in boxes where box.id != a.id && box.id != b.id {
                            let ox = box.x - obstaclePad
                            let oy = box.y - obstaclePad
                            let ow = box.w + 2 * obstaclePad
                            let oh = box.h + 2 * obstaclePad
                            for i in stride(from: 0, to: samples.count - 1, by: 1)
                            where segmentCrossesRect(samples[i], samples[i + 1], ox, oy, ow, oh) {
                                crossings += 1
                                break
                            }
                        }
                    }
                    let alignment = (sideAxis(fromSide) == dominant ? 0.0 : 1.0) + (sideAxis(toSide) == dominant ? 0.0 : 1.0)
                    var score = info.feasible ? 0.0 : 1e15
                    score += crossings * 1e12
                    score += alignment * alignmentWeight
                    score += Double(info.bends) * 1e9
                    score += dist2 * 100
                    score += Double(bRanking.firstIndex(of: toSide) ?? 0) * 10
                    score += Double(aRanking.firstIndex(of: fromSide) ?? 0)
                    if score < bestScore {
                        bestScore = score
                        bestFrom = fromSide
                        bestTo = toSide
                    }
                }
            }

            outSides[a.id, default: []].insert(bestFrom)
            inSides[b.id, default: []].insert(bestTo)
            result[wire.id] = SidePair(from: bestFrom, to: bestTo)
        }
        return result
    }

    /// Routes one wire around every box except its own ends.
    public static func routeWire(from: WireEnd, to: WireEnd, fromSide: Side, toSide: Side, boxes: [Box]) -> WireRoute {
        let fromBox: Box? = if case .node(let box) = from { box } else { nil }
        let toBox: Box? = if case .node(let box) = to { box } else { nil }
        let zero = Size(w: 0, h: 0)
        let pa = switch from {
        case .node(let box): box.position
        case .loose(let point): point
        }
        let pb = switch to {
        case .node(let box): box.position
        case .loose(let point): point
        }

        // p1 and p2 are the side centres; the drawn path stops short of them.
        let p1 = pointOnSide(pa, fromSide, fromBox?.size ?? zero)
        let p2 = pointOnSide(pb, toSide, toBox?.size ?? zero)
        let drawn1 = wireDrawnEnd(p1, fromSide)
        let drawn2 = wireDrawnEnd(p2, toSide)
        let mid = Point(x: (drawn1.x + drawn2.x) / 2, y: (drawn1.y + drawn2.y) / 2)

        var obstacles = boxes
            .filter { $0.id != fromBox?.id && $0.id != toBox?.id }
            .map { Obstacle(x: $0.x - obstaclePad, y: $0.y - obstaclePad, w: $0.w + 2 * obstaclePad, h: $0.h + 2 * obstaclePad) }
        // An end node only blocks when the wire leaves through its far side.
        if let fromBox, outwardNormalDotMid(fromSide, p1, mid) < 0 {
            obstacles.append(Obstacle(x: pa.x, y: pa.y, w: fromBox.w, h: fromBox.h))
        }
        if let toBox, outwardNormalDotMid(toSide, p2, mid) < 0 {
            obstacles.append(Obstacle(x: pb.x, y: pb.y, w: toBox.w, h: toBox.h))
        }

        let route = planOrthogonalRoute(drawn1, drawn2, fromSide, toSide, obstacles)
            ?? aStarOrthogonalRoute(drawn1, drawn2, fromSide, toSide, obstacles)
        let waypoints = route ?? wireWaypoints(drawn1, drawn2, fromSide, toSide)
        return WireRoute(
            p1: p1, p2: p2,
            path: route.map(roundedPolylinePath) ?? buildWirePath(drawn1, drawn2, fromSide, toSide),
            anchor: anchor(waypoints, fallback: mid))
    }

    private static func anchor(_ pts: [Point], fallback: Point) -> WireAnchor {
        var bestLength = -1.0
        var orientation = WireAxis.horizontal
        var midpoints: [Point] = []
        for i in stride(from: 0, to: pts.count - 1, by: 1) {
            let dx = abs(pts[i + 1].x - pts[i].x)
            let dy = abs(pts[i + 1].y - pts[i].y)
            let length = hypot(dx, dy)
            if length <= 0 { continue }
            let segment: WireAxis = dx >= dy ? .horizontal : .vertical
            let segmentMid = Point(x: (pts[i].x + pts[i + 1].x) / 2, y: (pts[i].y + pts[i + 1].y) / 2)
            if length > bestLength + 0.5 {
                bestLength = length
                orientation = segment
                midpoints = [segmentMid]
            } else if abs(length - bestLength) <= 0.5 && segment == orientation {
                midpoints.append(segmentMid)
            }
        }
        guard !midpoints.isEmpty else { return WireAnchor(x: fallback.x, y: fallback.y, orientation: orientation) }
        // Averaged along the segment, pinned to the outer edge across it.
        let count = Double(midpoints.count)
        return switch orientation {
        case .horizontal:
            WireAnchor(x: midpoints.reduce(0) { $0 + $1.x } / count, y: midpoints.map(\.y).min() ?? fallback.y, orientation: orientation)
        case .vertical:
            WireAnchor(x: midpoints.map(\.x).min() ?? fallback.x, y: midpoints.reduce(0) { $0 + $1.y } / count, orientation: orientation)
        }
    }
}
