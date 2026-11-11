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

    /// Where boats load and wait: the faces and corners of the island, a cell off its no-go line.
    public var quays: [CGPoint] {
        let corners = [CGPoint(x: minI - 2, y: minJ - 2), CGPoint(x: maxI + 3, y: minJ - 2),
                       CGPoint(x: maxI + 3, y: maxJ + 3), CGPoint(x: minI - 2, y: maxJ + 3)]
        let middle = CGPoint(x: (Double(minI) + Double(maxI) + 1) / 2, y: (Double(minJ) + Double(maxJ) + 1) / 2)
        let faces = [CGPoint(x: middle.x, y: Double(minJ) - 2), CGPoint(x: Double(maxI) + 3, y: middle.y),
                     CGPoint(x: middle.x, y: Double(maxJ) + 3), CGPoint(x: Double(minI) - 2, y: middle.y)]
        return faces + corners
    }
}

/// The lanes boats sail: from one island to another over open water, going round the land in the way.
/// Points are in ground cells, which is where land is drawn and where the tests look.
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
        // A crowded map has crossings in plenty: forget the oldest lanes rather than every one,
        // which would leave boats re-planning their course each frame.
        if remembered.count > 2048 {
            for gone in remembered.prefix(1024) { lanes.removeValue(forKey: gone) }
            remembered.removeFirst(1024)
        }
        lanes[key] = route
        remembered.append(key)
        return route
    }

    private struct RouteKey: Hashable {
        var from: WorkIslet
        var to: WorkIslet
        var islands: [WorkIslet]
        var drift: CGFloat
    }

    nonisolated(unsafe) private static var lanes: [RouteKey: [CGPoint]] = [:]
    nonisolated(unsafe) private static var remembered: [RouteKey] = []
    nonisolated(unsafe) private static let lock = NSLock()

    /// The shortest course over open water: a search over the sea itself, then the course pulled to
    /// as few legs as still sail clear of every coast.
    private static func plan(from: WorkIslet, to: WorkIslet, around islands: [WorkIslet]) -> [CGPoint] {
        let berths = from.quays
        let calls = to.quays
        guard let first = berths.first, let last = calls.first else { return [] }
        guard let course = cross(from: berths, to: calls, around: islands) else {
            // Hemmed in on every side, which a crowded map can do: the shortest crossing rather than no boat at all.
            var shortest = [first, last]
            for berth in berths { for call in calls where shorter([berth, call], than: shortest) { shortest = [berth, call] } }
            return shortest
        }
        return course
    }

    /// The sea is searched on this step, in cells: fine enough to find a channel, coarse enough to be quick.
    private static let sound = 2.0
    /// Cells a searched lane keeps off the no-go line, so the drawn bow still clears the coast.
    private static let clearance = 1.4

    /// A search over the water from any berth to any quay.
    private static func cross(from berths: [CGPoint], to calls: [CGPoint], around islands: [WorkIslet]) -> [CGPoint]? {
        let sheet = sea(of: islands, and: berths + calls)
        let across = Int((sheet.high - sheet.low) / sound) + 1
        func place(_ index: Int) -> CGPoint {
            CGPoint(x: sheet.low + Double(index % across) * sound, y: sheet.top + Double(index / across) * sound)
        }
        func index(_ at: CGPoint) -> Int? {
            let x = Int(((at.x - sheet.low) / sound).rounded()), y = Int(((at.y - sheet.top) / sound).rounded())
            guard x >= 0, x < across, y >= 0, y * across + x < sheet.nodes else { return nil }
            return y * across + x
        }
        // The sea is searched a little way off the coast: a lane that runs along the no-go line itself
        // would brush the sand as soon as it bows.
        func afloat(_ at: CGPoint) -> Bool {
            !islands.contains { islet in
                let box = islet.extent
                return at.x > box.lowI - Self.clearance && at.x < box.highI + Self.clearance
                    && at.y > box.lowJ - Self.clearance && at.y < box.highJ + Self.clearance
            }
        }

        var known = [Int: Double]()
        var previous = [Int: Int]()
        var ahead = [Int: Double]()   // where a course may still be led on, priced by the best course through it
        for berth in berths {
            guard let start = index(berth), afloat(place(start)) else { continue }
            known[start] = 0
            ahead[start] = calls.map { distance([place(start), $0]) }.min() ?? 0
        }
        while let here = ahead.min(by: { $0.value < $1.value })?.key {
            ahead[here] = nil
            let at = place(here)
            // Close enough to a haven that the boat can run in on one last leg.
            func ashore(_ call: CGPoint) -> Double { hypot(Double(call.x - at.x), Double(call.y - at.y)) }
            if let haven = calls.min(by: { ashore($0) < ashore($1) }), ashore(haven) <= sound * 2 {
                let course = trace(here, previous: previous, place: place)
                return tighten(course + [haven], around: islands, from: course.first ?? at, berths: berths)
            }
            for step in [CGPoint(x: 1, y: 0), CGPoint(x: -1, y: 0), CGPoint(x: 0, y: 1), CGPoint(x: 0, y: -1),
                         CGPoint(x: 1, y: 1), CGPoint(x: 1, y: -1), CGPoint(x: -1, y: 1), CGPoint(x: -1, y: -1)] {
                let onward = CGPoint(x: at.x + Double(step.x) * sound, y: at.y + Double(step.y) * sound)
                guard let there = index(onward), afloat(onward) else { continue }
                // A boat may not slip between two coasts diagonally when it could only pass them one at a time.
                if step.x != 0, step.y != 0,
                   !afloat(CGPoint(x: at.x + Double(step.x) * sound, y: at.y)),
                   !afloat(CGPoint(x: at.x, y: at.y + Double(step.y) * sound)) { continue }
                let course = known[here]! + distance([at, onward])
                guard course < (known[there] ?? .infinity) else { continue }
                known[there] = course
                previous[there] = here
                ahead[there] = course + (calls.map { distance([onward, $0]) }.min() ?? 0)
            }
        }
        return nil
    }

    /// The course the search came in on, from the berth it left to the quay it reached.
    private static func trace(_ index: Int, previous: [Int: Int], place: (Int) -> CGPoint) -> [CGPoint] {
        var out: [CGPoint] = []
        var step: Int? = index
        while let here = step { out.append(place(here)); step = previous[here] }
        return out.reversed()
    }

    /// The fewest legs that still sail clear water: a waypoint goes as soon as the boat can see past it.
    private static func tighten(_ course: [CGPoint], around islands: [WorkIslet], from leaving: CGPoint, berths: [CGPoint]) -> [CGPoint] {
        var course = course
        let start = berths.min { distance([$0, leaving]) < distance([$1, leaving]) } ?? course[0]
        course.insert(start, at: 0)
        var route = [course[0]]
        var here = 0
        while here < course.count - 1 {
            var next = course.count - 1
            while next > here + 1, !clear(from: route[route.count - 1], to: course[next], around: islands) { next -= 1 }
            route.append(course[next])
            here = next
        }
        return route
    }

    /// The sheet of water worth searching: every island and quay, with room to sail outside them all.
    private static func sea(of islands: [WorkIslet], and points: [CGPoint]) -> (low: Double, high: Double, top: Double, bottom: Double, nodes: Int) {
        let margin = 14.0
        let low = (islands.map { Double($0.minI) }.min() ?? 0) - margin
        let high = (islands.map { Double($0.maxI) }.max() ?? 0) + 1 + margin
        let top = (islands.map { Double($0.minJ) }.min() ?? 0) - margin
        let bottom = (islands.map { Double($0.maxJ) }.max() ?? 0) + 1 + margin
        let across = Int((high - low) / sound) + 1, down = Int((bottom - top) / sound) + 1
        return (min(low, points.map { Double($0.x) - sound }.min() ?? low),
                max(high, points.map { Double($0.x) + sound }.max() ?? high),
                min(top, points.map { Double($0.y) - sound }.min() ?? top),
                max(bottom, points.map { Double($0.y) + sound }.max() ?? bottom),
                (across + 4) * (down + 4))
    }

    /// Whether an island lies between two points: the straight line between them runs over its ground.
    /// A line that only touches the no-go line is not over it.
    static func covers(_ a: CGPoint, _ b: CGPoint, _ islet: WorkIslet) -> Bool {
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
    /// A short leg runs straight: it is a turn round a headland, not a stretch of open sea to sweep.
    public static func laneBow(_ span: Double) -> Double {
        guard span > 90 else { return 0 }
        return min(span < 260 ? 12 : 26, span * 0.12)
    }

    /// The point a drawn lane holds at, in cells: the chord bowed as the map bows it, taken back off the screen.
    public static func lanePoint(_ from: CGPoint, _ to: CGPoint, at t: Double) -> CGPoint {
        let (ax, ay) = screenPoint(from)
        let (bx, by) = screenPoint(to)
        let span = max(1, hypot(bx - ax, by - ay))
        let rise = laneBow(span)
        // The bow is drawn on the screen, so the curve is worked out there and taken back to cells whole.
        let controlX = (ax + bx) / 2 - (by - ay) / span * rise
        let controlY = (ay + by) / 2 + (bx - ax) / span * rise
        let one = 1 - t
        return point(x: one * one * ax + 2 * one * t * controlX + t * t * bx,
                     y: one * one * ay + 2 * one * t * controlY + t * t * by)
    }

    /// Whether the lane a boat actually sails is open water at every point, bow included.
    /// Tested in cells, which is where the map's ground is: the screen's box around an island is far bigger than the island.
    public static func clear(from: CGPoint, to: CGPoint, around islands: [WorkIslet]) -> Bool {
        // A drawn lane never drifts off its chord by more than the bow, so a box grown by that
        // much bounds the islands worth testing at all.
        let (ax, ay) = screenPoint(from)
        let (bx, by) = screenPoint(to)
        let drift = laneBow(max(1, hypot(bx - ax, by - ay))) / 28
        let near = islands.filter { islet in
            let box = islet.extent
            return !(max(from.x, to.x) < box.lowI - drift || min(from.x, to.x) > box.highI + drift
                || max(from.y, to.y) < box.lowJ - drift || min(from.y, to.y) > box.highJ + drift)
        }
        if near.isEmpty { return true }
        for step in 0...12 {
            let at = lanePoint(from, to, at: Double(step) / 12)
            if near.contains(where: { onLand($0, at) }) { return false }
        }
        return true
    }

    /// Whether a point stands on an island's ground.
    private static func onLand(_ islet: WorkIslet, _ at: CGPoint) -> Bool {
        let box = islet.extent
        let gap = 0.01
        return at.x > box.lowI + gap && at.x < box.highI - gap && at.y > box.lowJ + gap && at.y < box.highJ - gap
    }

    /// A screen point taken back to the cells it was drawn from.
    private static func point(x: Double, y: Double) -> CGPoint {
        let half = 28.0
        return CGPoint(x: x / (2 * half) + y / half, y: -x / (2 * half) + y / half)
    }

    /// A ground cell as the map draws it, kept in step with the app's own projection.
    private static func screenPoint(_ at: CGPoint) -> (Double, Double) {
        let half = 28.0
        return ((Double(at.x) - Double(at.y)) * half, (Double(at.x) + Double(at.y)) * half / 2)
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
