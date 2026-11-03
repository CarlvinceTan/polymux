import SwiftUI
import AvatarKit

struct ShapeGrid: View {
    @Binding var options: AvatarOptions
    var labels = true
    /// Fixed tiles in centred rows of this many, instead of an adaptive grid.
    var columns: Int?
    var size: CGFloat = 44
    var colorSize: CGFloat = 32
    var rowSpacing: CGFloat = 8
    var paletteInset: CGFloat = 0
    @State private var category: ShapeCategory = .shapes

    var body: some View {
        VStack(spacing: 12) {
            VStack(spacing: 12) {
                ScrollView(.horizontal) {
                    PillPicker("Category", selection: $category,
                               options: ShapeCategory.allCases.map { ($0.rawValue, $0) })
                }
                .scrollIndicators(.hidden)
                .defaultScrollAnchor(.center, for: .alignment)
                if let columns {
                    ViewThatFits(in: .horizontal) {
                        CenteredRows(items: category.shapes, columns: columns, spacing: 8, rowSpacing: rowSpacing) {
                            tile($0, size: size).frame(width: size + 12)
                        }
                        grid
                    }
                } else {
                    grid
                }
            }
            if category == .shapes {
                ColorGrid(options: $options, columns: 6, size: colorSize)
                    .padding(.bottom, paletteInset)
            }
        }
        .onChange(of: options.shape, initial: true) { _, shape in category = shape.category }
    }

    private var grid: some View {
        LazyVGrid(columns: [GridItem(.adaptive(minimum: 76), spacing: 8)], spacing: 8) {
            ForEach(category.shapes) { tile($0) }
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
    var size: CGFloat = 32

    var body: some View {
        if let columns {
            ViewThatFits(in: .horizontal) {
                CenteredRows(items: ColorID.avatarColors, columns: columns, spacing: 12, content: swatch)
                grid
            }
        } else {
            grid
        }
    }

    private var grid: some View {
        LazyVGrid(columns: [GridItem(.adaptive(minimum: size + 8), spacing: 12)], spacing: 12) {
            ForEach(ColorID.avatarColors, content: swatch)
        }
    }

    private func swatch(_ color: ColorID) -> some View {
        Button { options.color = color } label: {
            AvatarSwatch(color: color)
                .frame(width: size, height: size)
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
    var rowSpacing: CGFloat? = nil
    @ViewBuilder let content: (Item) -> Content

    var body: some View {
        VStack(spacing: rowSpacing ?? spacing) {
            ForEach(Array(stride(from: 0, to: items.count, by: columns)), id: \.self) { start in
                HStack(spacing: spacing) {
                    ForEach(items[start..<min(start + columns, items.count)], content: content)
                }
            }
        }
    }
}

private struct AvatarThumbnail: View, Equatable {
    let options: AvatarOptions

    var body: some View {
        AvatarView(options: options, playing: false, frozenAt: 1)
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
                AvatarThumbnail(options: choice)
                    .equatable()
                    .allowsHitTesting(false)
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
