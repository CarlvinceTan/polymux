import SwiftUI
import AvatarKit
import PolymuxKit

struct AvatarAnchors: PreferenceKey {
    static var defaultValue: [String: Anchor<CGRect>] { [:] }
    static func reduce(value: inout [String: Anchor<CGRect>], nextValue: () -> [String: Anchor<CGRect>]) {
        value.merge(nextValue(), uniquingKeysWith: { first, _ in first })
    }
}

struct HandoffOverlay: View {
    let handoffs: [ActivityStore.Handoff]
    let anchors: [String: Anchor<CGRect>]
    @Environment(\.accessibilityReduceMotion) private var reducedMotion
    @Environment(\.scenePhase) private var scene
    @State private var visible = false

    var body: some View {
        GeometryReader { geometry in
            TimelineView(.animation(minimumInterval: 1 / 30, paused: !visible || reducedMotion || scene != .active)) { tick in
                Canvas { context, _ in
                    guard !reducedMotion, visible, scene == .active else { return }
                    let viewport = CGRect(origin: .zero, size: geometry.size)
                    for handoff in handoffs {
                        let duration = handoff.result ? 0.62 : 0.52
                        let elapsed = tick.date.timeIntervalSince(handoff.at)
                        guard elapsed >= 0, elapsed < duration,
                              let source = anchors[handoff.source], let target = anchors[handoff.target] else { continue }
                        let from = geometry[source], to = geometry[target]
                        guard viewport.contains(from), viewport.contains(to) else { continue }
                        let flight = HandoffMotion.sample(elapsed / duration,
                            from: CGPoint(x: from.midX, y: from.midY), to: CGPoint(x: to.midX, y: to.midY))
                        var layer = context
                        layer.opacity = flight.alpha
                        layer.translateBy(x: flight.point.x, y: flight.point.y)
                        layer.scaleBy(x: flight.scale * 0.34, y: flight.scale * 0.34)
                        layer.translateBy(x: -50, y: -50)
                        for part in (handoff.result ? WorkProp.plane : .card).layers(elapsed: 1, reducedMotion: true) {
                            layer.fill(Path(part.path), with: .color(Color(avatarHex: part.color).opacity(part.alpha)),
                                       style: FillStyle(eoFill: true))
                        }
                    }
                }
            }
        }
        .allowsHitTesting(false)
        .accessibilityHidden(true)
        .onChange(of: handoffs, initial: true) { _, _ in visible = !handoffs.isEmpty }
        .task(id: handoffs.last?.id) {
            try? await Task.sleep(for: .seconds(0.7))
            if !Task.isCancelled { visible = false }
        }
    }
}
