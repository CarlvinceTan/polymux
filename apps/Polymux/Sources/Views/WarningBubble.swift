import SwiftUI

struct WarningBubble: View {
    let text: String
    var symbol = "exclamationmark.triangle"
    let visible: Bool
    let dismiss: () -> Void

    var body: some View {
        bubble
            .hidden()
            .accessibilityHidden(true)
            .overlay {
                ZStack {
                    if visible { bubble.transition(.opacity) }
                }
                .animation(.easeInOut(duration: 0.2), value: visible)
            }
            .frame(maxWidth: BubbleStyle.width)
    }

    private var bubble: some View {
        HStack(spacing: 10) {
            Label(text, systemImage: symbol)
            Button("Dismiss", systemImage: "xmark", action: dismiss)
                .labelStyle(.iconOnly)
                .buttonStyle(.plain)
                .accessibilityLabel("Dismiss warning")
                .help("Dismiss")
        }
        .font(.footnote)
        .foregroundStyle(.primary)
        .padding(.horizontal, 12)
        .padding(.vertical, 6)
        .glassEffect(.regular, in: RoundedRectangle(cornerRadius: BubbleStyle.radius))
    }
}
