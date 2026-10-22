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
    var poke: AvatarPoke? = nil
    var mood: MoodID? = nil

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
        .onChange(of: poke, initial: true) { _, poke in
            // A view that appears just after the poke, like a new bot's, still plays it.
            if let poke, Date().timeIntervalSince(poke.at) < 1 { motion.queue(poke.moves) }
        }
        .onChange(of: cueID, initial: true) { _, cue in
            guard cue != nil else { return }
            if mood == .celebrate { motion.queue([.spin(turns: 1), .bounce]) }
            else if mood == .surprised { motion.queue([.bounce]) }
        }
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
                            timeline: timeline, reduceMotion: reduceMotion, repeating: repeating, cueID: cueID, mood: mood)
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
        }

        if let notch = frame.notch {
            context.fill(disk(notch.x, notch.y, notch.r), with: .color(backing))
        }
        if let notice = frame.notif {
            context.fill(disk(notice.x, notice.y, notice.r), with: .color(ink))
        }

        decorate(frame, front: true, in: &context)
    }

    private func decorate(_ frame: BotFrame, front: Bool, in context: inout GraphicsContext) {
        if !front {
            for arc in frame.arcs { draw(arc, front: false, in: &context) }
        }
        if frame.dotsBehind != front { draw(frame.dots, in: &context) }
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
    private var lastPointer: CGPoint?
    private var wasStatic = false
    var hover: CGPoint?
    var size = CGSize.zero
    #if os(macOS)
    weak var host: NSView?
    #endif

    private var queued: [Poke] = []

    func suspend() { lastDate = nil }

    func queue(_ moves: [Poke]) { queued += moves }

    func frame(date: Date, options next: AvatarOptions, state: StateID, expression: ExpressionID?,
               running: Bool, frozenAt: Double?, time: Double?, timeline: [AvatarBlock]?,
               reduceMotion: Bool, repeating: Bool, cueID: String?, mood: MoodID?) -> BotFrame {
        if running, let lastDate { clock += min(0.064, max(0, date.timeIntervalSince(lastDate))) }
        lastDate = running ? date : nil
        let staticPose = reduceMotion || (!next.animates && time == nil && frozenAt == nil)
            || (mood == .spawning && !running && time == nil && frozenAt == nil)
        let controlled = frozenAt != nil || time != nil || staticPose
        let now = staticPose ? 1 : max(0, frozenAt ?? time ?? clock)
        let effective = expression ?? next.expression
        let changed = options?.shape != next.shape || appliedExpression != effective
        if engine == nil || timeline != lastBlocks || staticPose != wasStatic
            || (controlled && (changed || now < lastTime)) {
            rebuild(next, expression: effective, initial: timeline?.first?.state ?? state)
            playback = StatePlayback()
            blockIndex = 0
            appliedExpression = effective
        }
        wasStatic = staticPose
        let engine = engine!
        if !staticPose { for move in queued { engine.poke(move, now) } }
        queued = []
        let pointer = self.pointer
        let interaction = pointer != lastPointer && pointer != nil
        lastPointer = pointer
        let activeMood = staticPose && mood == .spawning ? .idle : mood
        engine.setMood(timeline == nil ? activeMood ?? (running ? MoodID(expression: effective) : nil) : nil,
                       interaction: interaction)
        if controlled, lastTime < 0 { _ = engine.sample(0) }
        if changed, !controlled {
            engine.setShape(next.shape.radii, clock)
            engine.solid = next.shape.solid
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
        engine?.solid = options.shape.solid
        aimed = false
        cycleNumber = nil
    }

    private func aim(_ engine: BotEngine, state: StateID, now: Double, follows: Bool) {
        guard follows, STATE_BY_ID[state]?.baseFace == true, let target = pointer else {
            if aimed { engine.setTurn(Turn(), now); aimed = false }
            return
        }
        let shape = options?.shape ?? .circle
        let sphere = shape == .circle
        let factor = shape == .heart ? 0.6 : shape == .wedge ? 0.55 : 1
        engine.setTurn(Turn(yaw: target.x * (sphere ? 9.9 : 14.3) * factor,
                            pitch: target.y * (sphere ? 7.7 : 10.45) * factor), now)
        aimed = true
    }

    /// Where the pointer is, in avatar widths from its centre.
    private var pointer: CGPoint? {
        #if os(macOS)
        guard let host, let window = host.window, window.isKeyWindow else { return nil }
        let point = window.mouseLocationOutsideOfEventStream
        guard window.contentView?.bounds.contains(point) == true else { return nil }
        let local = host.convert(point, from: nil)
        let width = max(1, host.bounds.width)
        return Self.reach(CGPoint(x: (local.x - host.bounds.midX) / width, y: (host.bounds.midY - local.y) / width))
        #else
        guard let hover else { return nil }
        let width = max(1, size.width)
        return Self.reach(CGPoint(x: (hover.x - size.width / 2) / width, y: (hover.y - size.height / 2) / width))
        #endif
    }

    /// Pointer reach is measured in twice the avatar's width, independently on each axis.
    private static func reach(_ offset: CGPoint) -> CGPoint {
        CGPoint(x: max(-1, min(1, offset.x / 2)), y: max(-1, min(1, offset.y / 2)))
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
