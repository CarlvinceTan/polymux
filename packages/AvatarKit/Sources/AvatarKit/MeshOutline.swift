import CoreGraphics

/// Cancels shared mesh edges without changing the nonzero fill of its faces.
struct MeshOutline {
    private struct Edge: Hashable {
        let from: Int
        let to: Int
    }

    private var indices: [Edge: Int] = [:]
    private var edges: [Edge] = []
    private var counts: [Int] = []

    mutating func add(_ face: [Int]) {
        guard let last = face.last else { return }
        var prior = last
        for vertex in face {
            let edge = Edge(from: min(prior, vertex), to: max(prior, vertex))
            let direction = prior < vertex ? 1 : -1
            if let index = indices[edge] {
                counts[index] += direction
            } else {
                indices[edge] = edges.count
                edges.append(edge)
                counts.append(direction)
            }
            prior = vertex
        }
    }

    func path(_ points: [CGPoint]) -> CGPath {
        var outgoing: [Int: [Int]] = [:]
        var starts: [Int] = []
        for (edge, count) in zip(edges, counts) where count != 0 {
            let from = count > 0 ? edge.from : edge.to
            let to = count > 0 ? edge.to : edge.from
            for _ in 0..<abs(count) { outgoing[from, default: []].append(to) }
            starts.append(from)
        }
        let path = CGMutablePath()
        for start in starts {
            while let next = outgoing[start]?.popLast() {
                path.move(to: points[start])
                var cursor = next
                while cursor != start {
                    path.addLine(to: points[cursor])
                    guard let next = outgoing[cursor]?.popLast() else {
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
