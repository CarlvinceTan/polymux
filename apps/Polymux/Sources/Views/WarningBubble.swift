import SwiftUI

struct WarningBubble: View {
    let text: String
    var symbol = "exclamationmark.triangle"
    var behind = 0
    let dismiss: () -> Void

    private var depth: Int { min(behind, 2) }
    private var shape: RoundedRectangle { RoundedRectangle(cornerRadius: BubbleStyle.radius) }

    var body: some View {
        HStack(spacing: 10) {
            Label(text, systemImage: symbol)
            Button("Dismiss", systemImage: "xmark", action: dismiss)
                .labelStyle(.iconOnly)
                .buttonStyle(.plain)
                .tapArea(14)
                .accessibilityLabel("Dismiss warning")
                .accessibilityValue(behind == 0 ? text : "\(text), \(behind) more")
                .help("Dismiss")
        }
        .font(.footnote)
        .foregroundStyle(.primary)
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
        .glassEffect(.regular, in: shape)
        .background {
            ZStack {
                ForEach((0..<depth).reversed(), id: \.self) { index in
                    Color.clear.glassEffect(.regular, in: shape)
                        .offset(x: CGFloat(index + 1) * 6, y: CGFloat(index + 1) * 5)
                }
            }
            .allowsHitTesting(false)
            .accessibilityHidden(true)
        }
        .padding(.trailing, CGFloat(depth) * 6)
        .padding(.bottom, CGFloat(depth) * 5)
        .frame(maxWidth: BubbleStyle.width)
    }
}

/// A short note floating over the content, so its arrival never pushes anything aside.
struct QuietNote: View {
    let text: String

    var body: some View {
        Text(text).font(.footnote).foregroundStyle(.primary)
            .multilineTextAlignment(.center)
            .padding(.horizontal, 12).padding(.vertical, 6)
            .glassEffect(.regular, in: RoundedRectangle(cornerRadius: BubbleStyle.radius))
            .transition(.opacity)
    }
}
