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

/// The To: field: chosen people as removable chips, then a field to find more.
struct RecipientBox<Item: Hashable>: View {
    let items: [Item]
    let title: (Item) -> String
    let label: (Item) -> String
    @Binding var query: String
    @Binding var focused: Bool
    let placeholder: String
    let onSubmit: () -> Void
    let onDelete: () -> Void
    let remove: (Item) -> Void
    @State private var height: CGFloat = 28

    var body: some View {
        HStack(alignment: .top, spacing: 8) {
            Text("To:").foregroundStyle(.muted).frame(height: 28)
            ScrollViewReader { proxy in
                ScrollView(.vertical) {
                    RecipientFlow {
                        ForEach(items, id: \.self) { item in
                            Button { remove(item) } label: {
                                HStack(spacing: 5) {
                                    Text(title(item)).lineLimit(1)
                                    Image(systemName: "xmark").font(.caption2.weight(.semibold))
                                }
                                .padding(.horizontal, 10).padding(.vertical, 5)
                                .background(.quaternary, in: Capsule())
                            }
                            .buttonStyle(.plain)
                            .frame(maxWidth: 170)
                            .accessibilityLabel("Remove \(label(item))")
                        }
                        RecipientField(text: $query, focused: $focused, placeholder: placeholder,
                                       onSubmit: onSubmit, onDelete: onDelete)
                            .frame(minWidth: 80, minHeight: 28)
                            .accessibilityLabel("To")
                            .id("recipientInput")
                    }
                    .onGeometryChange(for: CGFloat.self, of: { $0.size.height }) { height = $0 }
                }
                .scrollIndicators(.hidden)
                .frame(height: min(136, max(28, height)))
                .onChange(of: height) { _, _ in
                    if focused { proxy.scrollTo("recipientInput", anchor: .bottom) }
                }
                .onChange(of: focused) { _, value in
                    if value { proxy.scrollTo("recipientInput", anchor: .bottom) }
                }
            }
        }
        .padding(.horizontal, 14).padding(.vertical, 8)
        .frame(maxWidth: .infinity, minHeight: 44)
        .glassEffect(.regular, in: .rect(cornerRadius: 22))
    }
}
