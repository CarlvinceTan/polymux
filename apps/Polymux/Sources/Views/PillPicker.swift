import SwiftUI

struct PillPicker<Value: Hashable>: View {
    let title: String
    @Binding var selection: Value
    let options: [(title: String, value: Value)]
    let compact: Bool
    /// Draws an option in place of its title, which stays its accessible name.
    let label: ((Value) -> AnyView)?
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    init(_ title: String, selection: Binding<Value>, options: [(title: String, value: Value)], compact: Bool = false,
         label: ((Value) -> AnyView)? = nil) {
        self.title = title
        _selection = selection
        self.options = options
        self.compact = compact
        self.label = label
    }

    var body: some View {
        HStack(spacing: compact ? 4 : 8) {
            ForEach(options, id: \.value) { option in
                Button {
                    selection = option.value
                } label: {
                    Group {
                        if let label { label(option.value) } else { Text(option.title) }
                    }
                        .font(.subheadline)
                        .padding(.horizontal, compact ? 8 : 14).padding(.vertical, 10)
                }
                .buttonStyle(TabButton())
                .help(label == nil ? "" : option.title)
                .accessibilityLabel(option.title)
                .accessibilityAddTraits(selection == option.value ? .isSelected : [])
                .anchorPreference(key: PillAnchor.self, value: .bounds) {
                    selection == option.value ? $0 : nil
                }
            }
        }
        .backgroundPreferenceValue(PillAnchor.self) { anchor in
            GeometryReader { geometry in
                if let anchor {
                    let frame = geometry[anchor]
                    GlassPill(frame: frame)
                        .animation(Motion.move.unless(reduceMotion), value: frame)
                }
            }
            .allowsHitTesting(false)
            .accessibilityHidden(true)
        }
        .accessibilityElement(children: .contain)
        .accessibilityLabel(title)
    }
}

/// Plain button without the pressed dimming, which flashes the tab label on click.
private struct TabButton: ButtonStyle {
    func makeBody(configuration: Configuration) -> some View {
        configuration.label.contentShape(Capsule())
    }
}

private struct PillAnchor: PreferenceKey {
    static var defaultValue: Anchor<CGRect>? { nil }

    static func reduce(value: inout Anchor<CGRect>?, nextValue: () -> Anchor<CGRect>?) {
        value = nextValue() ?? value
    }
}

// Give the glass every intermediate frame instead of swapping effects between tabs.
private nonisolated struct GlassPill: View, Animatable {
    var frame: CGRect

    var animatableData: AnimatablePair<AnimatablePair<CGFloat, CGFloat>, AnimatablePair<CGFloat, CGFloat>> {
        get { .init(.init(frame.minX, frame.minY), .init(frame.width, frame.height)) }
        set {
            frame = CGRect(x: newValue.first.first, y: newValue.first.second,
                           width: newValue.second.first, height: newValue.second.second)
        }
    }

    var body: some View {
        Color.clear
            .frame(width: frame.width, height: frame.height)
            .glassEffect(.regular, in: Capsule())
            .offset(x: frame.minX, y: frame.minY)
            .transaction { $0.animation = nil }
    }
}

/// Fades a horizontal scroller wherever more lies past its edge, rather than clipping it.
private struct EdgeFade: ViewModifier {
    private struct Overflow: Equatable {
        var leading = false
        var trailing = false
    }

    @State private var overflow = Overflow()
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    func body(content: Content) -> some View {
        content
            .onScrollGeometryChange(for: Overflow.self) { geometry in
                let offset = geometry.contentOffset.x + geometry.contentInsets.leading
                return Overflow(leading: offset > 1, trailing: geometry.contentSize.width - offset - geometry.containerSize.width > 1)
            } action: { _, value in
                overflow = value
            }
            .mask {
                HStack(spacing: 0) {
                    edge(overflow.leading, from: .leading, to: .trailing)
                    Color.black
                    edge(overflow.trailing, from: .trailing, to: .leading)
                }
            }
            .animation(Motion.quick.unless(reduceMotion), value: overflow)
    }

    private func edge(_ fades: Bool, from start: UnitPoint, to end: UnitPoint) -> some View {
        LinearGradient(colors: [fades ? .clear : .black, .black], startPoint: start, endPoint: end).frame(width: 32)
    }
}

extension View {
    func edgeFade() -> some View { modifier(EdgeFade()) }
}
