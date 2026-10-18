import SwiftUI
import AvatarKit
#if os(macOS)
import AppKit
#endif

struct AvatarView: View {
    var options: AvatarOptions = .default
    var state: StateID = .idle
    var expression: ExpressionID? = nil
    var playing = true
    var visible = true
    var frozenAt: Double? = nil
    var paper: Color? = nil
    var timeline: [AvatarBlock]? = nil
    var time: Double? = nil
    var overlapGap: CGFloat = 0
    var repeating = false
    var cueID: String? = nil

    @Environment(\.colorScheme) private var scheme
    @Environment(\.scenePhase) private var scene
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var motion = AvatarMotion()
    @State private var appeared = false

    private var running: Bool {
        playing && visible && appeared && options.animates && !reduceMotion && scene == .active
            && frozenAt == nil && time == nil
    }

    private var backing: Color {
        if let paper { return paper }
        #if os(macOS)
        return Color(nsColor: .windowBackgroundColor)
        #else
        return Color(uiColor: .systemBackground)
        #endif
    }

    private var ink: Color {
        options.color.avatarColor(scheme)
    }

    var body: some View {
        TimelineView(.animation(minimumInterval: 1 / 30, paused: !running)) { tick in
            Canvas { context, size in
                let frame = sample(tick.date, size: size)
                context.translateBy(x: size.width / 2, y: size.height / 2)
                let scale = min(size.width, size.height) / 316
                context.scaleBy(x: scale, y: scale)
                draw(frame, in: &context)
            }
            .background {
                if overlapGap > 0 {
                    Canvas { context, size in
                        let frame = sample(tick.date, size: size)
                        let scale = min(size.width, size.height) / 316
                        guard scale > 0 else { return }
                        context.translateBy(x: size.width / 2, y: size.height / 2)
                        context.scaleBy(x: scale, y: scale)
                        context.opacity = frame.bodyAlpha
                        let path = Path(frame.bodyPath)
                        context.fill(path, with: .color(.white))
                        context.stroke(path, with: .color(.white),
                                       style: StrokeStyle(lineWidth: overlapGap * 2 / scale,
                                                          lineCap: .round, lineJoin: .round))
                    }
                    .blendMode(.destinationOut)
                }
            }
        }
        .aspectRatio(1, contentMode: .fit)
        #if os(macOS)
        .background {
            if options.followCursor && frozenAt == nil && !reduceMotion {
                AvatarPointer(motion: motion)
            }
        }
        #else
        .onContinuousHover { phase in
            switch phase {
            case .active(let point): motion.hover = point
            case .ended: motion.hover = nil
            }
        }
        #endif
        .onAppear { appeared = true; motion.suspend() }
        .onDisappear { appeared = false; motion.suspend() }
        .onChange(of: running) { _, _ in motion.suspend() }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(options.shape.label) avatar, \(state.label)")
    }

    private func sample(_ date: Date, size: CGSize) -> BotFrame {
        motion.size = size
        return motion.frame(date: date, options: options, state: state, expression: expression,
                            running: running, frozenAt: frozenAt, time: time,
                            timeline: timeline, reduceMotion: reduceMotion, repeating: repeating, cueID: cueID)
    }

    private func draw(_ frame: BotFrame, in context: inout GraphicsContext) {
        decorate(frame, front: false, in: &context)

        var layer = context
        layer.opacity *= frame.bodyAlpha
        layer.drawLayer { body in
            body.opacity = 1
            body.fill(Path(frame.bodyPath), with: .color(ink))
            body.blendMode = .sourceAtop
            for eye in frame.eyes {
                body.fill(Path(eye.d).applying(eye.matrix),
                          with: .color(backing.opacity(eye.alpha)))
            }
            if let notch = frame.notch {
                body.fill(disk(notch.x, notch.y, notch.r), with: .color(backing))
            }
        }

        decorate(frame, front: true, in: &context)
    }

    private func decorate(_ frame: BotFrame, front: Bool, in context: inout GraphicsContext) {
        if !front {
            for arc in frame.arcs { draw(arc, front: false, in: &context) }
        }
        if frame.dotsBehind != front { draw(frame.dots, in: &context) }
        if front, let dot = frame.notif {
            context.fill(disk(dot.x, dot.y, dot.r), with: .color(Color(avatarHex: "#2496e8")))
        }
        if front {
            for arc in frame.arcs { draw(arc, front: true, in: &context) }
        }
    }

    private func draw(_ arc: ArcRender, front: Bool, in context: inout GraphicsContext) {
        let gradient = Gradient(colors: arc.grad.stops.map { Color(avatarHex: $0) })
        var layer = context
        layer.opacity *= arc.opacity
        layer.stroke(Path(front ? arc.front : arc.back),
                     with: .linearGradient(gradient,
                                           startPoint: CGPoint(x: arc.grad.x1, y: arc.grad.y1),
                                           endPoint: CGPoint(x: arc.grad.x2, y: arc.grad.y2)),
                     style: StrokeStyle(lineWidth: arc.width, lineCap: .round))
    }

    private func draw(_ dots: [DotRender], in context: inout GraphicsContext) {
        for dot in dots.sorted(by: { ($0.depth ?? 1) < ($1.depth ?? 1) }) {
            let color: Color
            if let hex = dot.color { color = Color(avatarHex: hex) }
            else if let depth = dot.depth {
                color = backing.mix(with: ink, by: depth, in: .device)
            } else { color = ink }
            let path: Path
            if let shape = dot.d {
                let transform = CGAffineTransform.identity
                    .translatedBy(x: dot.x, y: dot.y)
                    .rotated(by: (dot.rot ?? 0) * .pi / 180)
                    .scaledBy(x: 100, y: 100)
                path = Path(shape).applying(transform)
            } else { path = disk(dot.x, dot.y, dot.r) }
            context.fill(path, with: .color(color.opacity(dot.opacity)))
        }
    }

    private func disk(_ x: Double, _ y: Double, _ radius: Double) -> Path {
        Path(ellipseIn: CGRect(x: x - radius, y: y - radius, width: radius * 2, height: radius * 2))
    }
}

@MainActor private final class AvatarMotion {
    private var engine: BotEngine?
    private var options: AvatarOptions?
    private var appliedExpression: ExpressionID?
    private var playback = StatePlayback()
    private var lastDate: Date?
    private var lastTime = -1.0
    private var lastBlocks: [AvatarBlock]?
    private var blockIndex = 0
    private var cycleNumber: Int?
    private var clock = 0.0
    private var aimed = false
    var hover: CGPoint?
    var size = CGSize.zero
    #if os(macOS)
    weak var host: NSView?
    #endif

    func suspend() { lastDate = nil }

    func frame(date: Date, options next: AvatarOptions, state: StateID, expression: ExpressionID?,
               running: Bool, frozenAt: Double?, time: Double?, timeline: [AvatarBlock]?,
               reduceMotion: Bool, repeating: Bool, cueID: String?) -> BotFrame {
        if running, let lastDate { clock += min(0.064, max(0, date.timeIntervalSince(lastDate))) }
        lastDate = running ? date : nil
        let staticPose = reduceMotion || (!next.animates && time == nil && frozenAt == nil)
        let controlled = frozenAt != nil || time != nil || staticPose
        let now = staticPose ? 1 : max(0, frozenAt ?? time ?? clock)
        let effective = expression ?? next.expression
        let changed = options?.shape != next.shape || appliedExpression != effective
        if engine == nil || timeline != lastBlocks || (controlled && (changed || now < lastTime)) {
            rebuild(next, expression: effective, initial: timeline?.first?.state ?? state)
            playback = StatePlayback()
            blockIndex = 0
            appliedExpression = effective
        }
        let engine = engine!
        if changed, !controlled {
            engine.setShape(next.shape.radii, clock)
            engine.setExpression(effective.expression, clock)
            appliedExpression = effective
        }
        if let blocks = timeline, !blocks.isEmpty {
            let total = blocks.reduce(0) { $0 + $1.duration }
            let cycle = total > 0 ? Int(floor(now / total)) : 0
            let origin = Double(cycle) * total
            let cursor = max(0, now - origin)
            if cycleNumber != cycle {
                self.engine!.reset(blocks[0].state, origin)
                blockIndex = 0
                cycleNumber = cycle
            }
            var offset = 0.0
            for (index, block) in blocks.enumerated() {
                if cursor < offset + block.duration || index == blocks.count - 1 {
                    if index < blockIndex {
                        self.engine!.reset(blocks[0].state, origin)
                        blockIndex = 0
                    }
                    if index > blockIndex {
                        var start = origin + blocks.prefix(blockIndex + 1).reduce(0) { $0 + $1.duration }
                        for prior in blocks[(blockIndex + 1)...index] {
                            self.engine!.setState(prior.state, start)
                            start += prior.duration
                        }
                    }
                    blockIndex = index
                    lastTime = now
                    lastBlocks = blocks
                    self.options = next
                    aim(self.engine!, state: block.state, now: now, follows: next.followCursor && !reduceMotion)
                    return self.engine!.sample(now)
                }
                offset += block.duration
            }
        }
        playback.update(engine, state: state, now: now, controlled: controlled,
                        repeating: repeating, cueID: cueID)
        aim(engine, state: state, now: now, follows: next.followCursor && !reduceMotion && frozenAt == nil)
        self.options = next
        lastTime = now
        lastBlocks = timeline
        return engine.sample(now)
    }

    private func rebuild(_ options: AvatarOptions, expression: ExpressionID, initial: StateID) {
        engine = BotEngine(scale: 100, initial: initial, shape: options.shape.radii,
                           expression: expression.expression)
        aimed = false
        cycleNumber = nil
    }

    private func aim(_ engine: BotEngine, state: StateID, now: Double, follows: Bool) {
        guard follows, STATE_BY_ID[state]?.baseFace == true, let target = pointer else {
            if aimed { engine.setLook(nil, now); aimed = false }
            return
        }
        engine.setLook(Look(yaw: -26 + target.x * 16, pitch: 10 - target.y * 13,
                            mix: 1, spin: 0, wander: 0), now)
        aimed = true
    }

    private var pointer: CGPoint? {
        #if os(macOS)
        guard let host, let window = host.window, window.isKeyWindow else { return nil }
        let point = window.mouseLocationOutsideOfEventStream
        guard window.contentView?.bounds.contains(point) == true else { return nil }
        let local = host.convert(point, from: nil)
        return CGPoint(x: max(-1, min(1, (local.x - host.bounds.midX) / max(1, window.frame.width / 2))),
                       y: max(-1, min(1, (host.bounds.midY - local.y) / max(1, window.frame.height / 2))))
        #else
        guard let hover else { return nil }
        return CGPoint(x: max(-1, min(1, (hover.x - size.width / 2) / max(1, size.width))),
                       y: max(-1, min(1, (hover.y - size.height / 2) / max(1, size.height))))
        #endif
    }
}

#if os(macOS)
private final class AvatarHost: NSView {
    override func hitTest(_ point: NSPoint) -> NSView? { nil }
}

private struct AvatarPointer: NSViewRepresentable {
    let motion: AvatarMotion
    func makeNSView(context: Context) -> NSView {
        let view = AvatarHost()
        motion.host = view
        return view
    }
    func updateNSView(_ view: NSView, context: Context) { motion.host = view }
}
#endif
