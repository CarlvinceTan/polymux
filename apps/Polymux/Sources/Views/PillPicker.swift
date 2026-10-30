import SwiftUI

struct PillPicker<Value: Hashable>: View {
    let title: String
    @Binding var selection: Value
    let options: [(title: String, value: Value)]
    let compact: Bool
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    init(_ title: String, selection: Binding<Value>, options: [(title: String, value: Value)], compact: Bool = false) {
        self.title = title
        _selection = selection
        self.options = options
        self.compact = compact
    }

    var body: some View {
        HStack(spacing: compact ? 4 : 8) {
            ForEach(options, id: \.value) { option in
                Button {
                    selection = option.value
                } label: {
                    Text(option.title)
                        .font(.subheadline)
                        .padding(.horizontal, compact ? 8 : 14).padding(.vertical, 10)
                }
                .buttonStyle(TabButton())
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
                        .animation(reduceMotion ? nil : .smooth(duration: 0.3), value: frame)
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
