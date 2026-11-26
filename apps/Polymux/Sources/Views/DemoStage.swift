import SwiftUI

/// A live preview of a setting: the real control centred on a soft backdrop, so glass reads as it does in the app.
struct DemoStage<Content: View>: View {
    @ViewBuilder let content: () -> Content
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        content()
            .frame(maxWidth: .infinity, minHeight: 112)
            .padding(.horizontal, 20)
            .background { backdrop }
            .clipShape(.rect(cornerRadius: 14, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: 14, style: .continuous)
                    .strokeBorder(.primary.opacity(0.08), lineWidth: 0.5)
            }
            .allowsHitTesting(false)
            .accessibilityHidden(true)
    }

    /// A blurred wash with two light bands, like a desktop seen through a window.
    private var backdrop: some View {
        let dark = scheme == .dark
        return ZStack {
            LinearGradient(colors: dark ? [Color(white: 0.20), Color(red: 0.17, green: 0.16, blue: 0.20)]
                                        : [Color(red: 0.86, green: 0.82, blue: 0.79), Color(red: 0.78, green: 0.76, blue: 0.80)],
                           startPoint: .topLeading, endPoint: .bottomTrailing)
            ForEach([0.3, 0.72], id: \.self) { spot in
                Capsule()
                    .fill(.white.opacity(dark ? 0.06 : 0.35))
                    .frame(width: 900, height: 10)
                    .rotationEffect(.degrees(-18))
                    .offset(y: (spot - 0.5) * 140)
                    .blur(radius: 6)
            }
        }
    }
}
