import AvatarKit
import SwiftUI

/// Draws the logo once while connecting, then holds it until the app takes over.
struct SplashView: View {
    let onFinish: () -> Void
    @Environment(\.colorScheme) private var scheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var progress = 0.0
    @State private var glow = 0.0

    var body: some View {
        ZStack {
            drawn
            drawn
                .blur(radius: 2)
                .opacity(glow * (scheme == .dark ? 0.9 : 0.25))
                .accessibilityHidden(true)
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .task {
            Task.detached(priority: .userInitiated) { preloadMascots() }
            #if DEBUG
            // Flows would each sit through the logo; only the ones about it ask for it.
            if VerifyFixture.enabled, !CommandLine.arguments.contains("--verify-splash"),
               !CommandLine.arguments.contains("--verify-startup") {
                // After the appearance that resets it.
                try? await Task.sleep(for: .milliseconds(10))
                onFinish()
                return
            }
            #endif
            if reduceMotion {
                progress = 1
                try? await Task.sleep(for: .seconds(0.6))
                if !Task.isCancelled { onFinish() }
                return
            }
            let draw = 1.25
            withAnimation(.linear(duration: draw)) { progress = 1 }
            try? await Task.sleep(for: .seconds(draw))
            guard !Task.isCancelled else { return }
            withAnimation(.easeOut(duration: 0.28)) { glow = 1 }
            try? await Task.sleep(for: .seconds(0.3))
            guard !Task.isCancelled else { return }
            withAnimation(.easeIn(duration: 0.42)) { glow = 0 }
            try? await Task.sleep(for: .seconds(0.5))
            guard !Task.isCancelled else { return }
            onFinish()
        }
    }

    private var drawn: some View {
        mark
            .mask {
                LogoStroke(progress: progress)
                    .stroke(style: StrokeStyle(lineWidth: 8, lineCap: .round, lineJoin: .round))
            }
    }

    private var mark: some View {
        Image("Logo")
            .renderingMode(.template)
            .resizable()
            .scaledToFit()
            .frame(width: 96, height: 96)
            .foregroundStyle(.primary)
            .accessibilityLabel("Polymux")
            .accessibilityValue("Connecting")
    }
}

/// The centreline of the logo, drawn clockwise from its top-left tip.
private struct LogoStroke: Shape {
    var progress: Double

    var animatableData: Double {
        get { progress }
        set { progress = newValue }
    }

    func path(in rect: CGRect) -> Path {
        // The logo's transparent bounds are 663 by 700.
        let width = min(rect.width, rect.height * 663.0 / 700.0)
        let height = width * 700.0 / 663.0
        let origin = CGPoint(x: rect.midX - width / 2, y: rect.midY - height / 2)
        var path = Path()
        path.move(to: point(Self.points[0], origin, width, height))
        for value in Self.points.dropFirst() {
            path.addLine(to: point(value, origin, width, height))
        }
        path.closeSubpath()
        return path.trimmedPath(from: 0, to: Self.bell(min(max(progress, 0), 1)))
    }

    private func point(_ value: CGPoint, _ origin: CGPoint, _ width: CGFloat, _ height: CGFloat) -> CGPoint {
        CGPoint(x: origin.x + value.x * width, y: origin.y + value.y * height)
    }

    /// Slow start, faster middle, slow finish, but never below half speed.
    private static func bell(_ t: Double) -> Double {
        if t <= 0 { return 0 }
        if t >= 1 { return 1 }
        let base = 0.55, amplitude = 1.0
        let eased = base * t + amplitude * (t / 2 - sin(2 * .pi * t) / (4 * .pi))
        return eased / (base + amplitude / 2)
    }

    /// Traced from the transparent logo and normalised to its 663x700 bounds.
    private static let points: [CGPoint] = [
        CGPoint(x: 0.1176, y: 0.0314),
        CGPoint(x: 0.6787, y: 0.0314),
        CGPoint(x: 0.7225, y: 0.0400),
        CGPoint(x: 0.7602, y: 0.0514),
        CGPoint(x: 0.8009, y: 0.0700),
        CGPoint(x: 0.8356, y: 0.0929),
        CGPoint(x: 0.8854, y: 0.1400),
        CGPoint(x: 0.9080, y: 0.1714),
        CGPoint(x: 0.9276, y: 0.2071),
        CGPoint(x: 0.9487, y: 0.2643),
        CGPoint(x: 0.9563, y: 0.2971),
        CGPoint(x: 0.9623, y: 0.3429),
        CGPoint(x: 0.9623, y: 0.4100),
        CGPoint(x: 0.9548, y: 0.4586),
        CGPoint(x: 0.9487, y: 0.4743),
        CGPoint(x: 0.9442, y: 0.4957),
        CGPoint(x: 0.9170, y: 0.5529),
        CGPoint(x: 0.8959, y: 0.5843),
        CGPoint(x: 0.8793, y: 0.6043),
        CGPoint(x: 0.8431, y: 0.6386),
        CGPoint(x: 0.8130, y: 0.6600),
        CGPoint(x: 0.7662, y: 0.6843),
        CGPoint(x: 0.7195, y: 0.7000),
        CGPoint(x: 0.6878, y: 0.7071),
        CGPoint(x: 0.6757, y: 0.7071),
        CGPoint(x: 0.6531, y: 0.7114),
        CGPoint(x: 0.4630, y: 0.7143),
        CGPoint(x: 0.4299, y: 0.7214),
        CGPoint(x: 0.4238, y: 0.7257),
        CGPoint(x: 0.4103, y: 0.7429),
        CGPoint(x: 0.2624, y: 0.9400),
        CGPoint(x: 0.2489, y: 0.9486),
        CGPoint(x: 0.2308, y: 0.9543),
        CGPoint(x: 0.2021, y: 0.9457),
        CGPoint(x: 0.0452, y: 0.7000),
        CGPoint(x: 0.0392, y: 0.6729),
        CGPoint(x: 0.0452, y: 0.6500),
        CGPoint(x: 0.0543, y: 0.6357),
        CGPoint(x: 0.2021, y: 0.4457),
        CGPoint(x: 0.2142, y: 0.4343),
        CGPoint(x: 0.2323, y: 0.4286),
        CGPoint(x: 0.2428, y: 0.4286),
        CGPoint(x: 0.2579, y: 0.4329),
        CGPoint(x: 0.2715, y: 0.4443),
        CGPoint(x: 0.4027, y: 0.6200),
        CGPoint(x: 0.4253, y: 0.6271),
        CGPoint(x: 0.4359, y: 0.6271),
        CGPoint(x: 0.4555, y: 0.6214),
        CGPoint(x: 0.4676, y: 0.6100),
        CGPoint(x: 0.6199, y: 0.4157),
        CGPoint(x: 0.6305, y: 0.3857),
        CGPoint(x: 0.6229, y: 0.3571),
        CGPoint(x: 0.4811, y: 0.1457),
        CGPoint(x: 0.4736, y: 0.1371),
        CGPoint(x: 0.4510, y: 0.1286),
        CGPoint(x: 0.4268, y: 0.1357),
        CGPoint(x: 0.4163, y: 0.1429),
        CGPoint(x: 0.2700, y: 0.3186),
        CGPoint(x: 0.2413, y: 0.3286),
        CGPoint(x: 0.2157, y: 0.3214),
        CGPoint(x: 0.2006, y: 0.3029),
        CGPoint(x: 0.0739, y: 0.0986),
        CGPoint(x: 0.0664, y: 0.0671),
        CGPoint(x: 0.0724, y: 0.0443),
        CGPoint(x: 0.0814, y: 0.0371),
        CGPoint(x: 0.1041, y: 0.0329),
        CGPoint(x: 0.1161, y: 0.0329),
    ]
}
