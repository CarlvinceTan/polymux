import SwiftUI
import AvatarKit

struct ShapeGrid: View {
    @Binding var options: AvatarOptions
    var labels = true
    /// Fixed tiles in centred rows of this many, instead of an adaptive grid.
    var columns: Int?

    var body: some View {
        if let columns {
            CenteredRows(items: ShapeID.creationShapes, columns: columns, spacing: 8) { tile($0, size: 44).frame(width: 56) }
        } else {
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 76), spacing: 8)], spacing: 8) {
                ForEach(ShapeID.creationShapes) { tile($0) }
            }
        }
    }

    private func tile(_ shape: ShapeID, size: CGFloat = 58) -> some View {
        var choice = options
        choice.shape = shape
        return AvatarTile(choice: choice, label: shape.label, showsLabel: labels, size: size, selected: options.shape == shape) {
            options.shape = shape
        }
    }
}

struct ColorGrid: View {
    @Binding var options: AvatarOptions
    /// Swatches in centred rows of this many, instead of an adaptive grid.
    var columns: Int?

    var body: some View {
        if let columns {
            CenteredRows(items: ColorID.avatarColors, columns: columns, spacing: 12, content: swatch)
        } else {
            LazyVGrid(columns: [GridItem(.adaptive(minimum: 40), spacing: 12)], spacing: 12) {
                ForEach(ColorID.avatarColors, content: swatch)
            }
        }
    }

    private func swatch(_ color: ColorID) -> some View {
        Button { options.color = color } label: {
            AvatarSwatch(color: color)
                .frame(width: 32, height: 32)
                .overlay { Circle().stroke(.primary.opacity(0.15), lineWidth: 1) }
                .padding(4)
                .overlay {
                    Circle().stroke(options.color.avatarID == color ? Color.primary : .clear, lineWidth: 2)
                }
        }
        .buttonStyle(.plain)
        .accessibilityLabel(color.avatarLabel)
        .accessibilityValue(options.color.avatarID == color ? "Selected" : "")
        .help(color.avatarLabel)
    }
}

/// Rows of up to `columns` items, with a short last row centred.
private struct CenteredRows<Item: Identifiable, Content: View>: View {
    let items: [Item]
    let columns: Int
    let spacing: CGFloat
    @ViewBuilder let content: (Item) -> Content

    var body: some View {
        VStack(spacing: spacing) {
            ForEach(Array(stride(from: 0, to: items.count, by: columns)), id: \.self) { start in
                HStack(spacing: spacing) {
                    ForEach(items[start..<min(start + columns, items.count)], content: content)
                }
            }
        }
    }
}

struct AvatarTile: View {
    let choice: AvatarOptions
    let label: String
    var showsLabel = true
    var size: CGFloat = 58
    let selected: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            VStack(spacing: 0) {
                AvatarView(options: choice, playing: false, frozenAt: 1)
                    .frame(width: size, height: size)
                if showsLabel {
                    Text(label).font(.caption2).lineLimit(1).foregroundStyle(.primary)
                }
            }
            .frame(maxWidth: .infinity)
            .padding(.vertical, 4)
            .background(selected ? Color.primary.opacity(0.07) : .clear, in: RoundedRectangle(cornerRadius: 10))
            .overlay { RoundedRectangle(cornerRadius: 10).stroke(selected ? Color.primary.opacity(0.45) : .clear) }
            .contentShape(RoundedRectangle(cornerRadius: 10))
        }
        .buttonStyle(.plain)
        .accessibilityLabel(label)
        .accessibilityValue(selected ? "Selected" : "")
    }
}
