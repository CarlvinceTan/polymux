import Foundation

/// Canvas metrics and size estimates, ported directly from `workflowLayoutMetrics.ts`.
public enum Metrics {
    public static let snapGrid = 24.0
    public static let nodeW = 240.0
    public static let nodeH = 96.0
    public static let terminalW = 96.0
    public static let terminalH = 48.0
    public static let colGap = 120.0
    public static let rowGap = 96.0
    public static let layoutOrigin = 48.0
    public static let nodeMinW = 140.0
    public static let nodeMinH = 60.0
    public static let nodeMaxW = 520.0
    public static let nodeMaxH = 260.0
    public static let nodeTextPad = 24.0
    public static let layoutGap = 8.0

    private static let sizeSnap = snapGrid * 2
    private static let averageCharWidth = 8.5
    private static let iterationsBadge = 24.0

    private static func snapSize(_ value: Double) -> Double {
        roundHalfUp(value / sizeSnap) * sizeSnap
    }

    private static func displayTitle(_ node: GraphNode, _ terminal: Terminal?) -> String {
        switch terminal {
        case .start: return "Start"
        case .end: return "End"
        case nil: break
        }
        if let title = node.title, !title.isEmpty { return title }
        if let action = node.actions?.first, !action.isEmpty { return action }
        return node.id.isEmpty ? "Untitled" : node.id
    }

    public static func estimateNodeSize(_ node: GraphNode, terminal: Terminal? = nil) -> Size {
        if terminal != nil { return Size(w: terminalW, h: terminalH) }

        // UTF-16 length, to match the reference's string length.
        let textWidth = Double(displayTitle(node, terminal).utf16.count) * averageCharWidth + nodeTextPad
        var w = nodeW
        if textWidth > nodeW {
            w = snapSize(textWidth)
            w = min(nodeMaxW, max(nodeMinW, w))
            w = snapSize(w)
        }

        var h = nodeH
        if (node.iterations ?? 0) > 1 {
            h = min(nodeMaxH, snapSize(nodeH + iterationsBadge))
            h = snapSize(h)
        }
        return Size(w: w, h: h)
    }

    public static func cellCenter(col: Double, row: Double) -> Point {
        Point(x: layoutOrigin + col * (nodeW + colGap) + nodeW / 2,
              y: layoutOrigin + row * (nodeH + rowGap) + nodeH / 2)
    }

    public static func positionFromCell(col: Double, row: Double, size: Size) -> Point {
        let center = cellCenter(col: col, row: row)
        return Point(x: center.x - size.w / 2, y: center.y - size.h / 2)
    }

    /// Keeps real nodes strictly between Start and End.
    public static func clampIntermediateColumns(_ nodes: [NodeModel], maxLayer: Double) {
        for node in nodes where node.terminal == nil {
            if node.col < 1 { node.col = 1 }
            if node.col > maxLayer { node.col = maxLayer }
        }
    }

    private static func rectsOverlap(_ a: LayoutRect, _ b: LayoutRect, gap: Double = layoutGap) -> Bool {
        a.x < b.x + b.w + gap && a.x + a.w + gap > b.x
            && a.y < b.y + b.h + gap && a.y + a.h + gap > b.y
    }

    private static func separateRects(_ a: inout LayoutRect, _ b: inout LayoutRect, gap: Double = layoutGap) -> Bool {
        let overlapX = min(a.x + a.w, b.x + b.w) - max(a.x, b.x)
        let overlapY = min(a.y + a.h, b.y + b.h) - max(a.y, b.y)
        if overlapX <= 0 || overlapY <= 0 { return false }

        if overlapX < overlapY {
            let push = overlapX + gap
            if a.x + a.w / 2 <= b.x + b.w / 2 {
                a.x -= push / 2
                b.x += push / 2
            } else {
                a.x += push / 2
                b.x -= push / 2
            }
        } else {
            let push = overlapY + gap
            if a.y + a.h / 2 <= b.y + b.h / 2 {
                a.y -= push / 2
                b.y += push / 2
            } else {
                a.y += push / 2
                b.y -= push / 2
            }
        }
        return true
    }

    public static func resolveLayoutOverlaps(_ rects: inout [LayoutRect], maxPasses: Int = 64) {
        for _ in 0..<maxPasses {
            var moved = false
            for i in rects.indices {
                for j in rects.indices where j > i {
                    var a = rects[i]
                    var b = rects[j]
                    if rectsOverlap(a, b) && separateRects(&a, &b) {
                        rects[i] = a
                        rects[j] = b
                        moved = true
                    }
                }
            }
            if !moved { break }
        }
    }

    public static func enforceIntermediateHorizontalBetween(_ rects: inout [LayoutRect]) {
        guard let start = rects.first(where: { $0.id == GraphLayout.startID }),
              let end = rects.first(where: { $0.id == GraphLayout.endID }) else { return }
        let minCenter = start.x + start.w / 2
        let maxCenter = end.x + end.w / 2
        if maxCenter <= minCenter { return }

        let lo = minCenter + 0.5
        let hi = maxCenter - 0.5
        if hi <= lo { return }
        for i in rects.indices where rects[i].terminal == nil {
            let center = rects[i].x + rects[i].w / 2
            let clamped = min(hi, max(lo, center))
            if abs(clamped - center) > 0.01 { rects[i].x += clamped - center }
        }
    }

    public static func computeAutoLayoutGeometry(_ models: [NodeModel]) -> [LayoutRect] {
        var rects = models.map { model in
            let size = model.node.size ?? estimateNodeSize(model.node, terminal: model.terminal)
            let position = positionFromCell(col: model.col, row: model.row, size: size)
            return LayoutRect(id: model.id, x: position.x, y: position.y, w: size.w, h: size.h, terminal: model.terminal)
        }
        resolveLayoutOverlaps(&rects)
        enforceIntermediateHorizontalBetween(&rects)
        resolveLayoutOverlaps(&rects)
        return rects
    }
}
