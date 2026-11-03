import SwiftUI
import GraphKit

/// Every wire on show: its line, arrowheads, and whatever is moving along it.
struct GraphWires: View {
    let model: GraphModel
    /// Handoffs that have just happened, drawn once along their wire.
    let pulses: [GraphPulse]
    @Environment(\.accessibilityReduceMotion) private var reducedMotion
    @Environment(\.scenePhase) private var scenePhase

    private var moving: Bool {
        !reducedMotion && scenePhase == .active && (!pulses.isEmpty || model.scene.wires.contains { $0.state == .active })
    }

    var body: some View {
        TimelineView(.animation(minimumInterval: 1 / 30, paused: !moving)) { tick in
            Canvas { context, size in
                draw(&context, size: size, now: tick.date)
            }
        }
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }

    private func draw(_ context: inout GraphicsContext, size: CGSize, now: Date) {
        let view = CGRect(origin: .zero, size: size).insetBy(dx: -40, dy: -40)
        let seconds = now.timeIntervalSinceReferenceDate
        for wire in model.scene.wires {
            let path = GraphPaths.path(wire.route.path, model: model)
            guard path.boundingRect.intersects(view) else { continue }
            let selected = model.selectedWire == wire.id
            let color = GraphTint.wire(wire.state, selected: selected)
            let width: CGFloat = selected ? 2 : wire.isLink ? 1 : 1.5
            let dash: [CGFloat] = switch wire.line {
            case .solid: []
            case .dashed: [6, 4]
            case .dotted: [1, 5]
            }
            context.stroke(path, with: .color(color), style: StrokeStyle(lineWidth: width, lineCap: .round, lineJoin: .round, dash: dash))
            heads(&context, wire: wire, color: color, width: width)
            guard moving || !pulses.isEmpty else { continue }
            if wire.state == .active {
                comet(&context, path: path, wire: wire, progress: (seconds / 1.6).truncatingRemainder(dividingBy: 1), color: .accentColor)
            }
            for pulse in pulses where pulse.wire == wire.id {
                let progress = now.timeIntervalSince(pulse.at) / GraphPulse.duration
                if (0...1).contains(progress) {
                    comet(&context, path: path, wire: wire, progress: pulse.reversed ? 1 - progress : progress, color: .accentColor, reversed: pulse.reversed)
                }
            }
        }
    }

    private func comet(_ context: inout GraphicsContext, path: Path, wire: ShownWire, progress: Double, color: Color, reversed: Bool? = nil) {
        let backwards = reversed ?? (wire.direction == .back)
        let head = backwards ? 1 - progress : progress
        let tail = backwards ? min(1, head + 0.1) : max(0, head - 0.1)
        let piece = path.trimmedPath(from: min(head, tail), to: max(head, tail))
        context.stroke(piece, with: .color(color), style: StrokeStyle(lineWidth: 3, lineCap: .round))
    }

    private func heads(_ context: inout GraphicsContext, wire: ShownWire, color: Color, width: CGFloat) {
        let points = GraphPaths.points(wire.route.path).map(model.point)
        guard points.count >= 2 else { return }
        let length = max(5, 7 * CGFloat(min(1, model.scale)))
        let style = StrokeStyle(lineWidth: width, lineCap: .round, lineJoin: .round)
        if wire.direction == .forward || wire.direction == .both, let tip = points.last {
            context.stroke(GraphPaths.arrow(tip: tip, from: points[points.count - 2], length: length), with: .color(color), style: style)
        }
        if wire.direction == .back || wire.direction == .both, let tip = points.first {
            context.stroke(GraphPaths.arrow(tip: tip, from: points[1], length: length), with: .color(color), style: style)
        }
    }
}

/// A handoff in flight along one wire.
struct GraphPulse: Identifiable, Equatable {
    static let duration = 0.7
    let id: String
    let wire: String
    let reversed: Bool
    let at: Date
}

/// A wire's label, which also carries its name for VoiceOver and its tap target.
struct GraphWireLabel: View {
    let wire: ShownWire
    let from: String
    let to: String
    /// Zoomed far out, the label keeps its tap target and VoiceOver name but draws nothing.
    var showsTitle = true
    let selected: Bool
    let action: () -> Void

    private var name: String {
        wire.wires.count > 1 ? "\(wire.wires.count) links" : wire.label ?? ""
    }

    private var title: String { showsTitle ? name : "" }

    private var description: String {
        let arrow = wire.direction == .both ? "between" : "from"
        let name = name.isEmpty ? "Link" : name
        let (first, second) = wire.direction == .back ? (to, from) : (from, to)
        return wire.direction == .both ? "\(name) \(arrow) \(first) and \(second)" : "\(name) from \(first) to \(second)"
    }

    var body: some View {
        Button(action: action) {
            Group {
                if title.isEmpty {
                    Color.clear.frame(width: 16, height: 16)
                } else {
                    Text(title)
                        .font(.caption2.weight(.medium))
                        .foregroundStyle(selected ? Color.accentColor : Color.primary)
                        .lineLimit(1)
                        .padding(.horizontal, 7).frame(height: 20)
                        .background(Color.graphCard, in: Capsule())
                        .overlay { Capsule().strokeBorder(selected ? Color.accentColor : Color.primary.opacity(0.2), lineWidth: 1) }
                        // The gap between columns, so a long label can't run over a card.
                        .frame(maxWidth: 104)
                }
            }
            #if os(iOS)
            .frame(minWidth: 44, minHeight: 44)
            #endif
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel(description)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}
