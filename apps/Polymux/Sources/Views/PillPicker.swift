import SwiftUI

struct PillPicker<Value: Hashable>: View {
    let title: String
    @Binding var selection: Value
    let options: [(title: String, value: Value)]
    @Namespace private var pill
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    init(_ title: String, selection: Binding<Value>, options: [(title: String, value: Value)]) {
        self.title = title
        _selection = selection
        self.options = options
    }

    var body: some View {
        HStack(spacing: 8) {
            ForEach(options, id: \.value) { option in
                Button {
                    withAnimation(reduceMotion ? nil : .smooth(duration: 0.3)) { selection = option.value }
                } label: {
                    // Reserve the medium width so selecting doesn't shift neighbours.
                    Text(option.title).fontWeight(.medium).hidden()
                        .overlay { FluidLabel(title: option.title, weight: selection == option.value ? 510 : 400) }
                        .font(.subheadline)
                        .padding(.horizontal, 14).padding(.vertical, 10)
                        .matchedGeometryEffect(id: option.value, in: pill)
                }
                .buttonStyle(TabButton())
                .accessibilityLabel(option.title)
                .accessibilityAddTraits(selection == option.value ? .isSelected : [])
            }
        }
        .background {
            Capsule().fill(Color.primary.opacity(0.11))
                .matchedGeometryEffect(id: selection, in: pill, isSource: false)
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

/// Animates the system font's variable weight axis smoothly; Font.Weight only snaps.
/// Drawn at an exact centre so the text doesn't snap sideways as its width changes.
private nonisolated struct FluidLabel: View, Animatable {
    let title: String
    var weight: CGFloat
    var animatableData: CGFloat {
        get { weight }
        set { weight = newValue }
    }

    var body: some View {
        Canvas { context, size in
            context.draw(Text(title).font(Font(font)), at: CGPoint(x: size.width / 2, y: size.height / 2))
        }
    }

    private var font: CTFont {
        #if os(macOS)
        let size = NSFont.preferredFont(forTextStyle: .subheadline).pointSize
        #else
        let size = UIFont.preferredFont(forTextStyle: .subheadline).pointSize
        #endif
        let base = CTFontCreateUIFontForLanguage(.system, size, nil)!
        let axis = 0x77676874 // 'wght'
        let variation = CTFontDescriptorCreateWithAttributes([kCTFontVariationAttribute: [axis: weight]] as CFDictionary)
        return CTFontCreateCopyWithAttributes(base, size, nil, variation)
    }
}
