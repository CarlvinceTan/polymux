import SwiftUI

struct WarningBubble: View {
    private static let countWidth: CGFloat = 24
    static let countInset = countWidth + 9

    let text: String
    var symbol = "exclamationmark.triangle"
    var more = 0
    let dismiss: () -> Void

    private var summary: String {
        more > 0 ? "\(text), \(more) more" : text
    }

    var body: some View {
        HStack(spacing: 10) {
            Label(text, systemImage: symbol)
            Button("Dismiss", systemImage: "xmark", action: dismiss)
                .labelStyle(.iconOnly)
                .buttonStyle(.plain)
                .tapArea(14)
                .accessibilityLabel("Dismiss warning")
                .accessibilityValue(summary)
                .help("Dismiss")
        }
        .font(.footnote)
        .foregroundStyle(.primary)
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
        .glassEffect(.regular, in: RoundedRectangle(cornerRadius: BubbleStyle.radius))
        .overlay(alignment: .trailing) {
            if more > 0 {
                Text("+\(more)")
                    .font(.subheadline.monospacedDigit())
                    .foregroundStyle(.muted)
                    .fixedSize()
                    .frame(width: Self.countWidth, alignment: .leading)
                    .offset(x: Self.countInset)
                    .allowsHitTesting(false)
                    .accessibilityHidden(true)
            }
        }
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
