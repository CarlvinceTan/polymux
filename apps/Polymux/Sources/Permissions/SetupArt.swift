import SwiftUI

/// A tinted banner holding a small mock of a screen, so a choice reads before its text does.
struct SetupBanner<Mock: View>: View {
    let tint: Color
    var width: CGFloat = 180
    var height: CGFloat = 86
    @ViewBuilder let mock: () -> Mock
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        ZStack {
            tint.opacity(scheme == .dark ? 0.22 : 0.12)
            mock()
                .frame(width: width, height: height)
                .background(PanelSurface.color(scheme), in: RoundedRectangle(cornerRadius: 10, style: .continuous))
                .overlay(RoundedRectangle(cornerRadius: 10, style: .continuous).strokeBorder(.separator))
        }
        .frame(maxWidth: .infinity, maxHeight: .infinity)
        .accessibilityHidden(true)
    }
}

/// The small screens drawn inside setup banners.
enum SetupMock {
    /// This Mac running the bots itself.
    static func local(_ tint: Color) -> some View {
        VStack(spacing: 6) {
            Image(systemName: "laptopcomputer").font(.system(size: 34, weight: .light)).foregroundStyle(tint)
            HStack(spacing: 4) {
                ForEach(0..<3) { _ in Circle().fill(tint.opacity(0.45)).frame(width: 6, height: 6) }
            }
        }
    }

    /// A server elsewhere that this device connects to.
    static func remote(_ tint: Color) -> some View {
        HStack(spacing: 10) {
            Image(systemName: "server.rack").font(.system(size: 26, weight: .light))
            line(tint)
            Image(systemName: "laptopcomputer").font(.system(size: 26, weight: .light))
        }
        .foregroundStyle(tint)
    }

    private static func line(_ tint: Color) -> some View {
        Rectangle().fill(tint.opacity(0.4)).frame(width: 14, height: 1.5)
    }
}

/// Where setup is: one dot per step, the current one wider.
struct SetupProgress: View {
    let count: Int
    let index: Int

    var body: some View {
        HStack(spacing: 6) {
            ForEach(0..<count, id: \.self) { step in
                Capsule()
                    .fill(step == index ? Color.blue : Color.secondary.opacity(0.3))
                    .frame(width: step == index ? 18 : 6, height: 6)
            }
        }
        .animation(.snappy, value: index)
        .accessibilityElement()
        .accessibilityLabel("Step \(index + 1) of \(count)")
    }
}
