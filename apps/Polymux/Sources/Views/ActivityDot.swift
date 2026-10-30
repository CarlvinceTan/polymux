import SwiftUI
import PolymuxKit

struct ActivityDot: View {
    let marker: ActivityMarker
    var size: CGFloat = 8
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var appeared = false

    var body: some View {
        Circle()
            .fill(Color(marker == .attention ? "Attention" : "Unread"))
            .frame(width: size, height: size)
            .opacity(reduceMotion || appeared ? 1 : 0)
            .onAppear {
                withAnimation(reduceMotion ? nil : .timingCurve(0.22, 1, 0.36, 1, duration: 0.13)) {
                    appeared = true
                }
            }
            .accessibilityLabel(marker.label)
    }
}

extension View {
    func activityBadge(_ marker: ActivityMarker?) -> some View {
        overlay {
            if let marker {
                GeometryReader { geometry in
                    ActivityDot(marker: marker, size: geometry.size.width > 60 ? 10 : 8)
                        .frame(maxWidth: .infinity, maxHeight: .infinity, alignment: .bottomTrailing)
                        .padding(2)
                }
                .allowsHitTesting(false)
                .accessibilityHidden(true)
            }
        }
    }
}
