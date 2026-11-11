import Foundation

/// An island on the map, in ground cells, with the water that surrounds it.
public struct WorkIslet: Hashable, Sendable {
    public var id: String
    public var minI: Int
    public var minJ: Int
    public var maxI: Int
    public var maxJ: Int
    /// How many cells of land lie outside its plots: none at home, whose bounds are already the shore.
    public var reach: Int

    public init(id: String, minI: Int, minJ: Int, maxI: Int, maxJ: Int, reach: Int = 4) {
        self.id = id
        self.minI = minI
        self.minJ = minJ
        self.maxI = maxI
        self.maxJ = maxJ
        self.reach = reach
    }

    /// Its ground as a box of cells, plus a cell of water so a boat keeps off the sand.
    public var extent: (lowI: Double, highI: Double, lowJ: Double, highJ: Double) {
        let off = Double(reach) + 1
        return (Double(minI) - off, Double(maxI) + 1 + off, Double(minJ) - off, Double(maxJ) + 1 + off)
    }

    /// The same island slid across the water, which is how a crossing gets its own lane.
    public func shifted(_ cells: CGFloat) -> WorkIslet {
        var copy = self
        let off = Int(cells.rounded())
        copy.minI += off; copy.maxI += off
        copy.minJ += off; copy.maxJ += off
        return copy
    }

    /// The no-go box with a further cell of water, so a boat lying off a coast stays off it.
    public var mooring: (lowI: Double, highI: Double, lowJ: Double, highJ: Double) {
        let box = extent
        return (box.lowI - 1, box.highI + 1, box.lowJ - 1, box.highJ + 1)
    }

    /// Where boats load and wait: the faces and corners of the island, a cell off its no-go line.
    public var quays: [CGPoint] { points(at: 1) }

    /// Quays plus a wider ring, for when an island's own coast is closed on a side and a lane must pass outside.
    public var anchorages: [CGPoint] { quays + points(at: Double(reach) + 7) }

    /// The faces and corners of the island grown by a given number of cells.
    private func points(at out: Double) -> [CGPoint] {
        let corners = [CGPoint(x: Double(minI) - out, y: Double(minJ) - out),
                       CGPoint(x: Double(maxI) + 1 + out, y: Double(minJ) - out),
                       CGPoint(x: Double(maxI) + 1 + out, y: Double(maxJ) + 1 + out),
                       CGPoint(x: Double(minI) - out, y: Double(maxJ) + 1 + out)]
        let middle = CGPoint(x: (Double(minI) + Double(maxI) + 1) / 2, y: (Double(minJ) + Double(maxJ) + 1) / 2)
        let faces = [CGPoint(x: middle.x, y: Double(minJ) - out), CGPoint(x: Double(maxI) + 1 + out, y: middle.y),
                     CGPoint(x: middle.x, y: Double(maxJ) + 1 + out), CGPoint(x: Double(minI) - out, y: middle.y)]
        return faces + corners
    }
}

/// The lanes boats sail: from one island to another over open water only, going round the land in the way.
/// Points are in ground cells; the map draws them, and the tests assert on them, in the same space.
public enum WorkSea {
    /// The points of a course from one island to another, staying off every other island's ground.
    /// Lanes depend only on the map's ground, so each is worked out once and remembered.
    /// `drift` slides the whole map, islands included, to give a crossing its own lane without changing what is open.
    public static func route(from: WorkIslet, to: WorkIslet, around islands: [WorkIslet], drift: CGFloat = 0) -> [CGPoint] {
        let key = RouteKey(from: from, to: to, islands: islands.sorted { $0.id < $1.id }, drift: drift)
        lock.lock()
        defer { lock.unlock() }
        if let known = lanes[key] { return known }
        let route = plan(from: from.shifted(drift), to: to.shifted(drift), around: islands.map { $0.shifted(drift) })
        if lanes.count > 256 { lanes.removeAll() }
        lanes[key] = route
        return route
    }

    private struct RouteKey: Hashable {
        var from: WorkIslet
        var to: WorkIslet
        var islands: [WorkIslet]
        var drift: CGFloat
    }

    nonisolated(unsafe) private static var lanes: [RouteKey: [CGPoint]] = [:]
    nonisolated(unsafe) private static let lock = NSLock()

    /// The shortest all-water course, sought over anchorages as a graph: getting round a cluster can take several legs.
    private static func plan(from: WorkIslet, to: WorkIslet, around islands: [WorkIslet]) -> [CGPoint] {
        let berths = from.quays
        let calls = to.quays
        guard let first = berths.first, let last = calls.first else { return [] }
        let anchorages = islands.flatMap(\.anchorages) + openSea(of: islands + [from, to])
        let nodes = berths + anchorages + calls
        let sources = 0..<berths.count
        let targets = (nodes.count - calls.count)..<nodes.count

        var best = Array(repeating: Double.infinity, count: nodes.count)
        var previous = Array(repeating: -1, count: nodes.count)
        var ahead = Set(sources)
        for source in sources { best[source] = 0 }
        while !ahead.isEmpty {
            let here = ahead.min { best[$0] < best[$1] }!
            ahead.remove(here)
            if targets.contains(here) { break }
            for there in nodes.indices where there != here && !ahead.contains(there)
                && best[here].isFinite && clear(from: nodes[here], to: nodes[there], around: islands) {
                let step = best[here] + distance([nodes[here], nodes[there]])
                guard step < best[there] else { continue }
                best[there] = step
                previous[there] = here
                ahead.insert(there)
            }
        }
        guard let finish = targets.min(by: { best[$0] < best[$1] }), best[finish].isFinite else {
            // Hemmed in on every side, which a crowded map can do: the shortest crossing rather than no boat at all.
            var shortest = [first, last]
            for berth in berths { for call in calls where shorter([berth, call], than: shortest) { shortest = [berth, call] } }
            return shortest
        }
        var route: [CGPoint] = []
        var step = finish
        while step >= 0 { route.append(nodes[step]); step = previous[step] }
        return route.reversed()
    }

    /// Waypoints on the open sea beyond every island, so a boat can always get outside a cluster.
    private static func openSea(of islands: [WorkIslet]) -> [CGPoint] {
        guard let lowI = islands.map({ Double($0.minI) }).min(), let highI = islands.map({ Double($0.maxI) }).max(),
              let lowJ = islands.map({ Double($0.minJ) }).min(), let highJ = islands.map({ Double($0.maxJ) }).max()
        else { return [] }
        let margin = 16.0
        let box = (lowI - margin, highI + 1 + margin, lowJ - margin, highJ + 1 + margin)
        let corners = [CGPoint(x: box.0, y: box.2), CGPoint(x: box.1, y: box.2),
                       CGPoint(x: box.1, y: box.3), CGPoint(x: box.0, y: box.3)]
        let middle = CGPoint(x: (box.0 + box.1) / 2, y: (box.2 + box.3) / 2)
        return corners + [CGPoint(x: middle.x, y: box.2), CGPoint(x: box.1, y: middle.y),
                          CGPoint(x: middle.x, y: box.3), CGPoint(x: box.0, y: middle.y)]
    }

    /// Whether an island lies between two points: the straight line between them runs over its ground.
    /// A line that only touches the no-go line is not over it.
    public static func covers(_ a: CGPoint, _ b: CGPoint, _ islet: WorkIslet) -> Bool {
        let box = islet.extent
        let gap = 0.01
        var entered = 0.0
        var left = 1.0
        for lane in [(Double(a.x), Double(b.x - a.x), box.lowI + gap, box.highI - gap),
                     (Double(a.y), Double(b.y - a.y), box.lowJ + gap, box.highJ - gap)] {
            let (origin, delta, low, high) = lane
            if abs(delta) < 1e-9 {
                if origin <= low || origin >= high { return false }
                continue
            }
            var near = (low - origin) / delta
            var far = (high - origin) / delta
            if near > far { swap(&near, &far) }
            entered = max(entered, near)
            left = min(left, far)
            if entered >= left { return false }
        }
        return entered < 0.999 && left > 0.001
    }

    /// How far a drawn lane bows off its chord, in screen pixels for a leg of that many pixels.
    /// The map draws its courses with this same figure, so what is planned is what sails.
    public static func laneBow(_ span: Double) -> Double { min(span < 260 ? 14 : 30, span * 0.14) }

    /// The point a drawn lane holds at, for the leg between two waypoints: the chord bowed as the map bows it.
    public static func lanePoint(_ from: CGPoint, _ to: CGPoint, at t: Double) -> CGPoint {
        let (ax, ay) = screenPoint(from)
        let (bx, by) = screenPoint(to)
        let span = max(1, hypot(bx - ax, by - ay))
        let rise = laneBow(span)
        let controlX = (ax + bx) / 2 - (by - ay) / span * rise
        let controlY = (ay + by) / 2 + (bx - ax) / span * rise
        let one = 1 - t
        return CGPoint(x: one * one * ax + 2 * one * t * controlX + t * t * bx,
                       y: one * one * ay + 2 * one * t * controlY + t * t * by)
    }

    /// Whether the lane a boat actually sails is open water at every point, bow included.
    public static func clear(from: CGPoint, to: CGPoint, around islands: [WorkIslet]) -> Bool {
        // A drawn lane never drifts off its chord by more than the bow, so a box grown by that
        // much bounds the islands worth testing at all.
        let (ax, ay) = screenPoint(from)
        let (bx, by) = screenPoint(to)
        let margin = laneBow(max(1, hypot(bx - ax, by - ay)))
        let near = islands.filter { islet in
            let box = extentInScreen(islet)
            return !(max(ax, bx) < box.low - margin || min(ax, bx) > box.high + margin
                || max(ay, by) < box.top - margin || min(ay, by) > box.bottom + margin)
        }
        if near.isEmpty { return true }
        for step in 0...12 {
            let at = lanePoint(from, to, at: Double(step) / 12)
            if near.contains(where: { touches(extentInScreen($0), at) }) { return false }
        }
        return true
    }

    /// An island's no-go box in screen pixels, which is where the map draws and bows its lanes.
    private static func extentInScreen(_ islet: WorkIslet) -> (low: Double, high: Double, top: Double, bottom: Double) {
        let box = islet.extent
        let corners = [(box.lowI, box.lowJ), (box.highI, box.lowJ), (box.highI, box.highJ), (box.lowI, box.highJ)]
            .map { screenPoint(CGPoint(x: $0.0, y: $0.1)) }
        let xs = corners.map { $0.0 }, ys = corners.map { $0.1 }
        return (xs.min()!, xs.max()!, ys.min()!, ys.max()!)
    }

    /// A ground cell as the map draws it. Kept in step with the app's own projection.
    private static func screenPoint(_ at: CGPoint) -> (Double, Double) {
        let half = 28.0
        return ((Double(at.x) - Double(at.y)) * half, (Double(at.x) + Double(at.y)) * half / 2)
    }

    private static func touches(_ box: (low: Double, high: Double, top: Double, bottom: Double), _ at: CGPoint) -> Bool {
        !(at.x < box.low || at.x > box.high || at.y < box.top || at.y > box.bottom)
    }

    /// Distance as the map measures it, in screen pixels.
    public static func distance(_ points: [CGPoint]) -> Double {
        zip(points, points.dropFirst()).reduce(0) { sum, step in
            let (ax, ay) = screenPoint(step.0)
            let (bx, by) = screenPoint(step.1)
            return sum + hypot(bx - ax, by - ay)
        }
    }

    private static func shorter(_ points: [CGPoint], than other: [CGPoint]) -> Bool { distance(points) < distance(other) }
}
