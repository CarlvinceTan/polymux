import SwiftUI

struct RecipientFlow: Layout {
    private struct Placement {
        let point: CGPoint
        let size: CGSize
    }

    func sizeThatFits(proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) -> CGSize {
        let width = max(80, proposal.width ?? 300)
        let rows = positions(subviews, width: width)
        let height = rows.map { $0.point.y + $0.size.height }.max() ?? 28
        return CGSize(width: width, height: height)
    }

    func placeSubviews(in bounds: CGRect, proposal: ProposedViewSize, subviews: Subviews, cache: inout ()) {
        let rows = positions(subviews, width: bounds.width)
        for (index, row) in rows.enumerated() {
            subviews[index].place(at: CGPoint(x: bounds.minX + row.point.x, y: bounds.minY + row.point.y),
                                 proposal: ProposedViewSize(row.size))
        }
    }

    private func positions(_ subviews: Subviews, width: CGFloat) -> [Placement] {
        var rows: [Placement] = []
        var x: CGFloat = 0, y: CGFloat = 0, height: CGFloat = 0
        for index in subviews.indices {
            let field = index == subviews.count - 1
            var size = subviews[index].sizeThatFits(field ? ProposedViewSize(width: max(80, width - x), height: nil) : .unspecified)
            if x > 0 && x + size.width > width {
                x = 0
                y += height + 8
                height = 0
                size = subviews[index].sizeThatFits(ProposedViewSize(width: width, height: nil))
            }
            rows.append(Placement(point: CGPoint(x: x, y: y), size: size))
            x += size.width + 8
            height = max(height, size.height)
        }
        return rows
    }
}
