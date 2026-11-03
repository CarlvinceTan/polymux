import CoreGraphics

/// Cancels shared mesh edges without changing the nonzero fill of its faces.
struct MeshOutline {
    private var from: [Int32] = []
    private var to: [Int32] = []

    mutating func add(_ a: Int, _ b: Int, _ c: Int) {
        from.append(Int32(a)); to.append(Int32(b))
        from.append(Int32(b)); to.append(Int32(c))
        from.append(Int32(c)); to.append(Int32(a))
    }

    mutating func add(_ face: [Int]) {
        guard var prior = face.last else { return }
        for vertex in face {
            from.append(Int32(prior))
            to.append(Int32(vertex))
            prior = vertex
        }
    }

    func path(_ points: [CGPoint]) -> CGPath {
        let path = CGMutablePath()
        guard !from.isEmpty else { return path }
        // Each edge once, chained through its lower vertex, with its net direction.
        var lists = [Int32](repeating: -1, count: points.count)
        var lower: [Int32] = [], upper: [Int32] = [], counts: [Int32] = [], chain: [Int32] = []
        lower.reserveCapacity(from.count); upper.reserveCapacity(from.count)
        counts.reserveCapacity(from.count); chain.reserveCapacity(from.count)
        for (a, b) in zip(from, to) {
            let low = min(a, b), high = max(a, b), direction: Int32 = a < b ? 1 : -1
            var cursor = lists[Int(low)]
            while cursor >= 0, upper[Int(cursor)] != high { cursor = chain[Int(cursor)] }
            if cursor >= 0 {
                counts[Int(cursor)] += direction
            } else {
                chain.append(lists[Int(low)])
                lists[Int(low)] = Int32(lower.count)
                lower.append(low); upper.append(high); counts.append(direction)
            }
        }
        // What's left, as lists of where each vertex leads.
        var heads = [Int32](repeating: -1, count: points.count)
        var targets: [Int32] = [], links: [Int32] = [], starts: [Int32] = []
        for index in lower.indices where counts[index] != 0 {
            let count = counts[index]
            let start = count > 0 ? lower[index] : upper[index]
            let end = count > 0 ? upper[index] : lower[index]
            for _ in 0..<abs(count) {
                links.append(heads[Int(start)])
                heads[Int(start)] = Int32(targets.count)
                targets.append(end)
            }
            starts.append(start)
        }
        func pop(_ vertex: Int32) -> Int32? {
            let edge = heads[Int(vertex)]
            guard edge >= 0 else { return nil }
            heads[Int(vertex)] = links[Int(edge)]
            return targets[Int(edge)]
        }
        for start in starts {
            while let next = pop(start) {
                path.move(to: points[Int(start)])
                var cursor = next
                while cursor != start {
                    path.addLine(to: points[Int(cursor)])
                    guard let next = pop(cursor) else {
                        assertionFailure("Mesh boundary is not closed")
                        break
                    }
                    cursor = next
                }
                path.closeSubpath()
            }
        }
        return path
    }
}
