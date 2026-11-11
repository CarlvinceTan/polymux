import SwiftUI
import GraphKit

/// Every wire on show, drawn as a lane, with what's moving along it drawn as a parcel.
struct GraphWires: View {
    let model: GraphModel
    /// Things that have just been handed over, each drawn once along its wire.
    let pulses: [GraphPulse]
    @Environment(\.accessibilityReduceMotion) private var reducedMotion
    @Environment(\.scenePhase) private var scenePhase

    private var moving: Bool {
        !reducedMotion && scenePhase == .active && (!pulses.isEmpty || !model.cargo.isEmpty)
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
        let focus = model.focus
        // Parcels stay readable zoomed out, and never outgrow the lane zoomed in.
        let zoom = CGFloat(min(1.15, max(0.7, model.scale)))
        var parcels: [(point: CGPoint, kind: String?, faded: Bool)] = []
        for wire in model.scene.wires {
            let path = GraphPaths.lane(wire.route.path, model: model)
            guard path.boundingRect.intersects(view) else { continue }
            let selected = model.selectedWire == wire.id
            let faded = focus.map { !($0.contains(wire.from) && $0.contains(wire.to)) } ?? false
            var layer = context
            layer.opacity = faded ? 0.22 : 1
            let color = GraphTint.wire(wire.state, selected: selected)
            let width: CGFloat = selected ? 2 : wire.isLink ? 1 : 1.5
            let dash: [CGFloat] = switch wire.line {
            case .solid: []
            case .dashed: [6, 4]
            case .dotted: [1, 5]
            }
            if !wire.isLink {
                // The bed a lane runs in, which is what makes a route read as a path rather than a wire.
                let bed = wire.state == .active ? Color.accentColor.opacity(0.16) : Color.primary.opacity(0.06)
                layer.stroke(path, with: .color(bed), style: StrokeStyle(lineWidth: 8 * zoom, lineCap: .round, lineJoin: .round))
            }
            layer.stroke(path, with: .color(color), style: StrokeStyle(lineWidth: width, lineCap: .round, lineJoin: .round, dash: dash))
            let loads = model.cargo[wire.id] ?? []
            // A parcel already shows which way things go.
            if loads.isEmpty { heads(&layer, wire: wire, color: color, width: width) }
            for (index, load) in loads.enumerated() {
                let phase = reducedMotion ? 0.5 : (seconds / Self.trip + Double(index) / Double(loads.count)).truncatingRemainder(dividingBy: 1)
                parcels.append((point(path, at: Self.progress(phase, load: load, still: reducedMotion)), load.kind, faded))
            }
            for pulse in pulses where pulse.wire == wire.id {
                let progress = now.timeIntervalSince(pulse.at) / GraphPulse.duration
                guard (0...1).contains(progress) else { continue }
                let eased = Self.ease(progress)
                parcels.append((point(path, at: pulse.reversed ? 1 - eased : eased), pulse.kind, faded))
            }
        }
        // Over every lane, so a parcel is never cut by a line it crosses.
        for parcel in parcels { self.parcel(&context, at: parcel.point, kind: parcel.kind, zoom: zoom, faded: parcel.faded) }
    }

    /// Seconds one parcel takes from end to end.
    private static let trip = 2.4

    private static func ease(_ value: Double) -> Double { value * value * (3 - 2 * value) }

    /// How far along its lane a parcel is. One that bounces goes out for half its trip and back for the rest.
    private static func progress(_ phase: Double, load: GraphCargo, still: Bool) -> Double {
        if still { return 0.5 }
        let along = load.bounces ? ease(phase < 0.5 ? phase * 2 : 2 - phase * 2) : ease(phase)
        return load.reversed ? 1 - along : along
    }

    private func point(_ path: Path, at progress: Double) -> CGPoint {
        path.trimmedPath(from: 0, to: min(1, max(0.001, progress))).currentPoint ?? path.currentPoint ?? .zero
    }

    private func parcel(_ context: inout GraphicsContext, at point: CGPoint, kind: String?, zoom: CGFloat, faded: Bool) {
        var layer = context
        layer.opacity = faded ? 0.22 : 1
        let radius = 10 * zoom
        let box = CGRect(x: point.x - radius, y: point.y - radius, width: radius * 2, height: radius * 2)
        layer.fill(Path(ellipseIn: box.insetBy(dx: -1.5, dy: -1.5)), with: .color(.graphCanvas))
        layer.fill(Path(ellipseIn: box), with: .color(.accentColor))
        let symbol = Text(Image(systemName: GraphLook.cargo(kind))).font(.system(size: 10 * zoom, weight: .semibold)).foregroundStyle(.white)
        layer.draw(layer.resolve(symbol), at: point)
    }

    private func heads(_ context: inout GraphicsContext, wire: ShownWire, color: Color, width: CGFloat) {
        let points = GraphPaths.corners(wire.route.path).map(model.point)
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

/// Something handed over just now, in flight along one wire.
struct GraphPulse: Identifiable, Equatable {
    static let duration = 1.1
    let id: String
    let wire: String
    let reversed: Bool
    let at: Date
    var kind: String?
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
