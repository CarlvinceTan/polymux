import Foundation

/// Orthogonal wire routing, ported directly from the old canvas's `workflowWireRouting.ts`.
public enum Routing {
    public static let endpointRadius = 4.0
    public static let wireEndGap = endpointRadius
    public static let obstaclePad = 8.0
    public static let allSides = Side.allCases

    public static func pointOnSide(_ pos: Point, _ side: Side, _ size: Size = Size(w: Metrics.nodeW, h: Metrics.nodeH)) -> Point {
        switch side {
        case .right: Point(x: pos.x + size.w, y: pos.y + size.h / 2)
        case .left: Point(x: pos.x, y: pos.y + size.h / 2)
        case .top: Point(x: pos.x + size.w / 2, y: pos.y)
        case .bottom: Point(x: pos.x + size.w / 2, y: pos.y + size.h)
        }
    }

    public static func outwardNormal(_ side: Side) -> Point {
        switch side {
        case .right: Point(x: 1, y: 0)
        case .left: Point(x: -1, y: 0)
        case .top: Point(x: 0, y: -1)
        case .bottom: Point(x: 0, y: 1)
        }
    }

    /// Positive when the port faces the chord midpoint, negative when it faces away.
    public static func outwardNormalDotMid(_ side: Side, _ port: Point, _ mid: Point) -> Double {
        let n = outwardNormal(side)
        return (mid.x - port.x) * n.x + (mid.y - port.y) * n.y
    }

    public static func oppositeSide(_ side: Side) -> Side {
        switch side {
        case .top: .bottom
        case .bottom: .top
        case .left: .right
        case .right: .left
        }
    }

    public static func rankSides(_ dx: Double, _ dy: Double) -> [Side] {
        let xSide: Side = dx >= 0 ? .right : .left
        let ySide: Side = dy >= 0 ? .bottom : .top
        if abs(dx) >= abs(dy) {
            return [xSide, ySide, oppositeSide(ySide), oppositeSide(xSide)]
        }
        return [ySide, xSide, oppositeSide(xSide), oppositeSide(ySide)]
    }

    public static func sideAxis(_ side: Side) -> WireAxis {
        side == .left || side == .right ? .horizontal : .vertical
    }

    private static func sign(_ value: Double) -> Double {
        value == 0 ? 0 : (value > 0 ? 1 : -1)
    }

    /// Bend count, and whether the step route leaves outward and arrives inward.
    public static func pathInfo(_ p1: Point, _ p2: Point, _ fromSide: Side, _ toSide: Side) -> (bends: Int, feasible: Bool) {
        let n1 = outwardNormal(fromSide)
        let n2 = outwardNormal(toSide)
        let sx = sign(p2.x - p1.x)
        let sy = sign(p2.y - p1.y)
        if sideAxis(fromSide) == sideAxis(toSide) {
            if sideAxis(fromSide) == .horizontal {
                let facing = n1.x == -n2.x
                let departs = sx == n1.x || sx == 0
                return (p1.y == p2.y ? 0 : 2, facing && departs)
            }
            let facing = n1.y == -n2.y
            let departs = sy == n1.y || sy == 0
            return (p1.x == p2.x ? 0 : 2, facing && departs)
        }
        // Strict: a zero-length leg means the side isn't really emitting outward.
        if sideAxis(fromSide) == .horizontal {
            return (1, sx == n1.x && sy == -n2.y)
        }
        return (1, sy == n1.y && sx == -n2.x)
    }

    /// Boundary contact counts as a crossing here, unlike `orthoSegmentCrossesRect`.
    public static func segmentCrossesRect(_ a: Point, _ b: Point, _ rx: Double, _ ry: Double, _ rw: Double, _ rh: Double) -> Bool {
        let right = rx + rw
        let bottom = ry + rh
        if (a.x < rx && b.x < rx) || (a.x > right && b.x > right) { return false }
        if (a.y < ry && b.y < ry) || (a.y > bottom && b.y > bottom) { return false }
        if a.x >= rx && a.x <= right && a.y >= ry && a.y <= bottom { return true }
        if b.x >= rx && b.x <= right && b.y >= ry && b.y <= bottom { return true }
        return segmentsIntersect(a, b, Point(x: rx, y: ry), Point(x: right, y: ry))
            || segmentsIntersect(a, b, Point(x: right, y: ry), Point(x: right, y: bottom))
            || segmentsIntersect(a, b, Point(x: right, y: bottom), Point(x: rx, y: bottom))
            || segmentsIntersect(a, b, Point(x: rx, y: bottom), Point(x: rx, y: ry))
    }

    public static func segmentsIntersect(_ p1: Point, _ p2: Point, _ p3: Point, _ p4: Point) -> Bool {
        let dx1 = p2.x - p1.x
        let dy1 = p2.y - p1.y
        let dx2 = p4.x - p3.x
        let dy2 = p4.y - p3.y
        let denom = dx2 * dy1 - dy2 * dx1
        if abs(denom) < 1e-9 { return false }
        let ua = (dx2 * (p3.y - p1.y) - dy2 * (p3.x - p1.x)) / denom
        let ub = (dx1 * (p3.y - p1.y) - dy1 * (p3.x - p1.x)) / denom
        return ua >= 0 && ua <= 1 && ub >= 0 && ub <= 1
    }

    public static func wireWaypoints(_ p1: Point, _ p2: Point, _ fromSide: Side, _ toSide: Side) -> [Point] {
        let fromHorizontal = fromSide == .left || fromSide == .right
        let toHorizontal = toSide == .left || toSide == .right
        var pts = [p1]
        if fromHorizontal && toHorizontal {
            let mx = (p1.x + p2.x) / 2
            pts.append(Point(x: mx, y: p1.y))
            pts.append(Point(x: mx, y: p2.y))
        } else if !fromHorizontal && !toHorizontal {
            let my = (p1.y + p2.y) / 2
            pts.append(Point(x: p1.x, y: my))
            pts.append(Point(x: p2.x, y: my))
        } else if fromHorizontal {
            pts.append(Point(x: p2.x, y: p1.y))
        } else {
            pts.append(Point(x: p1.x, y: p2.y))
        }
        pts.append(p2)
        return pts
    }

    /// Interior crossings only, so stubs starting on a node's edge don't count.
    public static func orthoSegmentCrossesRect(_ a: Point, _ b: Point, _ r: Obstacle) -> Bool {
        let rx2 = r.x + r.w
        let ry2 = r.y + r.h
        if abs(a.y - b.y) < 0.5 {
            if a.y <= r.y || a.y >= ry2 { return false }
            return max(a.x, b.x) > r.x && min(a.x, b.x) < rx2
        }
        if abs(a.x - b.x) < 0.5 {
            if a.x <= r.x || a.x >= rx2 { return false }
            return max(a.y, b.y) > r.y && min(a.y, b.y) < ry2
        }
        return false
    }

    public static func polylineCrossesAnyObstacle(_ pts: [Point], _ obstacles: [Obstacle]) -> Bool {
        for i in stride(from: 0, to: pts.count - 1, by: 1) {
            for obstacle in obstacles where orthoSegmentCrossesRect(pts[i], pts[i + 1], obstacle) {
                return true
            }
        }
        return false
    }

    public static func polylineLength(_ pts: [Point]) -> Double {
        var length = 0.0
        for i in stride(from: 0, to: pts.count - 1, by: 1) {
            length += hypot(pts[i + 1].x - pts[i].x, pts[i + 1].y - pts[i].y)
        }
        return length
    }

    private static func bounds(_ points: [Point], _ obstacles: [Obstacle]) -> (minX: Double, maxX: Double, minY: Double, maxY: Double) {
        var minX = points.map(\.x).min() ?? 0
        var maxX = points.map(\.x).max() ?? 0
        var minY = points.map(\.y).min() ?? 0
        var maxY = points.map(\.y).max() ?? 0
        for o in obstacles {
            if o.x < minX { minX = o.x }
            if o.x + o.w > maxX { maxX = o.x + o.w }
            if o.y < minY { minY = o.y }
            if o.y + o.h > maxY { maxY = o.y + o.h }
        }
        return (minX, maxX, minY, maxY)
    }

    /// Shortest of a few fixed routes that clears every obstacle, or nil so the caller tries A*.
    public static func planOrthogonalRoute(_ p1: Point, _ p2: Point, _ fromSide: Side, _ toSide: Side, _ obstacles: [Obstacle]) -> [Point]? {
        let stub = 40.0
        let clear = 44.0
        let n1 = outwardNormal(fromSide)
        let n2 = outwardNormal(toSide)
        let stubFrom = Point(x: p1.x + n1.x * stub, y: p1.y + n1.y * stub)
        let stubTo = Point(x: p2.x + n2.x * stub, y: p2.y + n2.y * stub)
        let fromHorizontal = fromSide == .left || fromSide == .right
        let toHorizontal = toSide == .left || toSide == .right

        let box = bounds([stubFrom, stubTo, p1, p2], obstacles)
        let topLane = box.minY - clear
        let bottomLane = box.maxY + clear
        let leftLane = box.minX - clear
        let rightLane = box.maxX + clear

        var candidates: [[Point]] = []

        // Direct: stub out, connect the stubs, stub in.
        if fromHorizontal && toHorizontal {
            if abs(stubFrom.y - stubTo.y) < 0.5 {
                candidates.append([p1, stubFrom, stubTo, p2])
            } else {
                let mx = (stubFrom.x + stubTo.x) / 2
                candidates.append([p1, stubFrom, Point(x: mx, y: stubFrom.y), Point(x: mx, y: stubTo.y), stubTo, p2])
            }
        } else if !fromHorizontal && !toHorizontal {
            if abs(stubFrom.x - stubTo.x) < 0.5 {
                candidates.append([p1, stubFrom, stubTo, p2])
            } else {
                let my = (stubFrom.y + stubTo.y) / 2
                candidates.append([p1, stubFrom, Point(x: stubFrom.x, y: my), Point(x: stubTo.x, y: my), stubTo, p2])
            }
        } else if fromHorizontal {
            candidates.append([p1, stubFrom, Point(x: stubTo.x, y: stubFrom.y), stubTo, p2])
            candidates.append([p1, stubFrom, Point(x: stubFrom.x, y: stubTo.y), stubTo, p2])
        } else {
            candidates.append([p1, stubFrom, Point(x: stubFrom.x, y: stubTo.y), stubTo, p2])
            candidates.append([p1, stubFrom, Point(x: stubTo.x, y: stubFrom.y), stubTo, p2])
        }

        // Around every obstacle, along a lane outside their bounds.
        candidates.append([p1, stubFrom, Point(x: stubFrom.x, y: topLane), Point(x: stubTo.x, y: topLane), stubTo, p2])
        candidates.append([p1, stubFrom, Point(x: stubFrom.x, y: bottomLane), Point(x: stubTo.x, y: bottomLane), stubTo, p2])
        candidates.append([p1, stubFrom, Point(x: leftLane, y: stubFrom.y), Point(x: leftLane, y: stubTo.y), stubTo, p2])
        candidates.append([p1, stubFrom, Point(x: rightLane, y: stubFrom.y), Point(x: rightLane, y: stubTo.y), stubTo, p2])

        // No stubs, for close nodes whose stubs would overlap and fold back.
        if fromHorizontal && toHorizontal {
            if abs(p1.y - p2.y) < 0.5 {
                candidates.append([p1, p2])
            } else {
                let mx = (p1.x + p2.x) / 2
                candidates.append([p1, Point(x: mx, y: p1.y), Point(x: mx, y: p2.y), p2])
            }
        } else if !fromHorizontal && !toHorizontal {
            if abs(p1.x - p2.x) < 0.5 {
                candidates.append([p1, p2])
            } else {
                let my = (p1.y + p2.y) / 2
                candidates.append([p1, Point(x: p1.x, y: my), Point(x: p2.x, y: my), p2])
            }
        } else if fromHorizontal {
            candidates.append([p1, Point(x: p2.x, y: p1.y), p2])
            candidates.append([p1, Point(x: p1.x, y: p2.y), p2])
        } else {
            candidates.append([p1, Point(x: p1.x, y: p2.y), p2])
            candidates.append([p1, Point(x: p2.x, y: p1.y), p2])
        }

        var best: [Point]?
        var bestLength = Double.infinity
        for pts in candidates {
            if hasOrthogonalSpike(pts) { continue }
            if !leavesAndArrivesPerpendicular(pts, fromSide, toSide) { continue }
            if polylineCrossesAnyObstacle(pts, obstacles) { continue }
            let length = polylineLength(pts)
            if length < bestLength {
                bestLength = length
                best = pts
            }
        }
        return best
    }

    /// Whether the route's first and last real displacements follow the ports' normals.
    public static func leavesAndArrivesPerpendicular(_ pts: [Point], _ fromSide: Side, _ toSide: Side) -> Bool {
        guard pts.count >= 2 else { return false }
        let n1 = outwardNormal(fromSide)
        let n2 = outwardNormal(toSide)
        let start = pts[0]
        var forward = 1
        while forward < pts.count, abs(pts[forward].x - start.x) < 0.5, abs(pts[forward].y - start.y) < 0.5 {
            forward += 1
        }
        if forward >= pts.count { return false }
        if (pts[forward].x - start.x) * n1.x + (pts[forward].y - start.y) * n1.y <= 0 { return false }
        let end = pts[pts.count - 1]
        var back = pts.count - 2
        while back >= 0, abs(end.x - pts[back].x) < 0.5, abs(end.y - pts[back].y) < 0.5 {
            back -= 1
        }
        if back < 0 { return false }
        return (pts[back].x - end.x) * n2.x + (pts[back].y - end.y) * n2.y > 0
    }

    /// Whether any interior corner is a 180° backtrack, which renders as a spike.
    public static func hasOrthogonalSpike(_ pts: [Point]) -> Bool {
        for i in stride(from: 1, to: pts.count - 1, by: 1) {
            let v1x = pts[i - 1].x - pts[i].x
            let v1y = pts[i - 1].y - pts[i].y
            let v2x = pts[i + 1].x - pts[i].x
            let v2y = pts[i + 1].y - pts[i].y
            if hypot(v1x, v1y) < 0.5 || hypot(v2x, v2y) < 0.5 { continue }
            if v1x * v2x + v1y * v2y > 0 { return true }
        }
        return false
    }

    private final class Cell {
        let cx: Int
        let cy: Int
        let g: Int
        let f: Int
        let dir: Int
        let parent: Cell?

        init(cx: Int, cy: Int, g: Int, f: Int, dir: Int, parent: Cell?) {
            self.cx = cx
            self.cy = cy
            self.g = g
            self.f = f
            self.dir = dir
            self.parent = parent
        }
    }

    /// Grid A* for when every fixed route hits an obstacle. Nil when a stub cell is blocked.
    public static func aStarOrthogonalRoute(_ p1: Point, _ p2: Point, _ fromSide: Side, _ toSide: Side, _ obstacles: [Obstacle]) -> [Point]? {
        let step = 24.0
        let stub = 40.0
        let n1 = outwardNormal(fromSide)
        let n2 = outwardNormal(toSide)
        let stubFrom = Point(x: p1.x + n1.x * stub, y: p1.y + n1.y * stub)
        let stubTo = Point(x: p2.x + n2.x * stub, y: p2.y + n2.y * stub)

        var box = bounds([stubFrom, stubTo, p1, p2], obstacles)
        let pad = step * 4
        box.minX -= pad
        box.maxX += pad
        box.minY -= pad
        box.maxY += pad

        // Aligned so the stub lands exactly on a cell.
        let originX = stubFrom.x - ((stubFrom.x - box.minX) / step).rounded(.up) * step
        let originY = stubFrom.y - ((stubFrom.y - box.minY) / step).rounded(.up) * step
        let cols = Int(((box.maxX - originX) / step).rounded(.up)) + 1
        let rows = Int(((box.maxY - originY) / step).rounded(.up)) + 1
        if cols * rows > 50000 { return nil }

        // Cells on a boundary stay open, so a stub on a node's edge isn't trapped.
        var blocked = [Bool](repeating: false, count: cols * rows)
        for o in obstacles {
            let cxLo = max(0, Int(((o.x - originX) / step).rounded(.up)))
            let cxHi = min(cols - 1, Int(((o.x + o.w - originX) / step).rounded(.down)))
            let cyLo = max(0, Int(((o.y - originY) / step).rounded(.up)))
            let cyHi = min(rows - 1, Int(((o.y + o.h - originY) / step).rounded(.down)))
            for cy in stride(from: cyLo, through: cyHi, by: 1) {
                for cx in stride(from: cxLo, through: cxHi, by: 1) {
                    let px = originX + Double(cx) * step
                    let py = originY + Double(cy) * step
                    if px > o.x && px < o.x + o.w && py > o.y && py < o.y + o.h {
                        blocked[cy * cols + cx] = true
                    }
                }
            }
        }

        let startCx = Int(roundHalfUp((stubFrom.x - originX) / step))
        let startCy = Int(roundHalfUp((stubFrom.y - originY) / step))
        let goalCx = Int(roundHalfUp((stubTo.x - originX) / step))
        let goalCy = Int(roundHalfUp((stubTo.y - originY) / step))
        if startCx < 0 || startCx >= cols || startCy < 0 || startCy >= rows { return nil }
        if goalCx < 0 || goalCx >= cols || goalCy < 0 || goalCy >= rows { return nil }
        if blocked[startCy * cols + startCx] { return nil }
        if blocked[goalCy * cols + goalCx] { return nil }

        let directions = [(1, 0), (-1, 0), (0, 1), (0, -1)]
        let initial = directions.firstIndex { $0.0 == Int(n1.x) && $0.1 == Int(n1.y) } ?? -1
        func heuristic(_ cx: Int, _ cy: Int) -> Int { abs(cx - goalCx) + abs(cy - goalCy) }
        // Each extra bend must save this many cells, which keeps paths tidy.
        let turnCost = 4

        var open = [Cell(cx: startCx, cy: startCy, g: 0, f: heuristic(startCx, startCy), dir: initial, parent: nil)]
        var closed = [Bool](repeating: false, count: cols * rows)
        var expansions = 0
        let maxExpansions = cols * rows * 2

        while !open.isEmpty {
            expansions += 1
            if expansions > maxExpansions { return nil }
            var bestIndex = 0
            for i in stride(from: 1, to: open.count, by: 1) where open[i].f < open[bestIndex].f {
                bestIndex = i
            }
            let current = open.remove(at: bestIndex)
            let key = current.cy * cols + current.cx
            if closed[key] { continue }
            closed[key] = true

            if current.cx == goalCx && current.cy == goalCy {
                var cells: [Point] = []
                var node: Cell? = current
                while let cell = node {
                    cells.append(Point(x: originX + Double(cell.cx) * step, y: originY + Double(cell.cy) * step))
                    node = cell.parent
                }
                cells.reverse()
                var out = [p1, stubFrom]
                for i in stride(from: 1, to: cells.count - 1, by: 1) {
                    let sameX = cells[i].x == cells[i - 1].x && cells[i].x == cells[i + 1].x
                    let sameY = cells[i].y == cells[i - 1].y && cells[i].y == cells[i + 1].y
                    if !sameX && !sameY { out.append(cells[i]) }
                }
                // The grid follows stubFrom, so stubTo can sit a fraction of a cell off.
                let last = out[out.count - 1]
                if toSide == .left || toSide == .right {
                    if last.y != stubTo.y { out.append(Point(x: last.x, y: stubTo.y)) }
                } else if last.x != stubTo.x {
                    out.append(Point(x: stubTo.x, y: last.y))
                }
                out.append(stubTo)
                out.append(p2)
                return out
            }

            for d in 0..<4 {
                let nx = current.cx + directions[d].0
                let ny = current.cy + directions[d].1
                if nx < 0 || nx >= cols || ny < 0 || ny >= rows { continue }
                let nextKey = ny * cols + nx
                if closed[nextKey] || blocked[nextKey] { continue }
                let heading = current.parent == nil ? initial : current.dir
                let g = current.g + 1 + (d != heading ? turnCost : 0)
                open.append(Cell(cx: nx, cy: ny, g: g, f: g + heuristic(nx, ny), dir: d, parent: current))
            }
        }
        return nil
    }

    /// A polyline with one corner radius throughout, shrunk to fit its tightest segment.
    public static func roundedPolylinePath(_ input: [Point]) -> [PathCommand] {
        guard input.count >= 2 else { return [] }
        var pts = [input[0]]
        for i in stride(from: 1, to: input.count - 1, by: 1) {
            let prev = pts[pts.count - 1]
            let v1x = input[i].x - prev.x
            let v1y = input[i].y - prev.y
            let v2x = input[i + 1].x - input[i].x
            let v2y = input[i + 1].y - input[i].y
            if abs(v1x * v2y - v1y * v2x) < 1e-6 { continue }
            pts.append(input[i])
        }
        pts.append(input[input.count - 1])
        if pts.count == 2 { return [.move(pts[0]), .line(pts[1])] }

        var radius = 8.0
        for i in 0..<(pts.count - 1) {
            let corners = (i > 0 ? 1 : 0) + (i < pts.count - 2 ? 1 : 0)
            if corners == 0 { continue }
            radius = min(radius, hypot(pts[i + 1].x - pts[i].x, pts[i + 1].y - pts[i].y) / Double(corners))
        }

        var path: [PathCommand] = [.move(pts[0])]
        for i in 1..<(pts.count - 1) {
            let curr = pts[i]
            let v1x = pts[i - 1].x - curr.x
            let v1y = pts[i - 1].y - curr.y
            let v2x = pts[i + 1].x - curr.x
            let v2y = pts[i + 1].y - curr.y
            let len1 = max(1, hypot(v1x, v1y))
            let len2 = max(1, hypot(v2x, v2y))
            path.append(.line(Point(x: curr.x + (v1x / len1) * radius, y: curr.y + (v1y / len1) * radius)))
            path.append(.quad(control: curr, to: Point(x: curr.x + (v2x / len2) * radius, y: curr.y + (v2y / len2) * radius)))
        }
        path.append(.line(pts[pts.count - 1]))
        return path
    }

    public static func routedWirePath(_ p1: Point, _ p2: Point, _ fromSide: Side, _ toSide: Side, _ obstacles: [Obstacle]) -> [PathCommand] {
        if let route = planOrthogonalRoute(p1, p2, fromSide, toSide, obstacles) ?? aStarOrthogonalRoute(p1, p2, fromSide, toSide, obstacles) {
            return roundedPolylinePath(route)
        }
        return buildWirePath(p1, p2, fromSide, toSide)
    }

    public static func buildWirePath(_ p1: Point, _ p2: Point, _ fromSide: Side, _ toSide: Side) -> [PathCommand] {
        roundedPolylinePath(wireWaypoints(p1, p2, fromSide, toSide))
    }
}
