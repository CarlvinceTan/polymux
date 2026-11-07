import SwiftUI
import AvatarKit
#if os(macOS)
import AppKit
#endif

struct AvatarView: View {
    var options: AvatarOptions = .default
    var state: StateID = .idle
    var expression: ExpressionID? = nil
    var playing = false
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
    var interactive = false
    var personality = ""

    @Environment(\.colorScheme) private var scheme
    @Environment(\.scenePhase) private var scene
    @Environment(\.appearsActive) private var active
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var canvas = AvatarCanvasState(motion: AvatarMotion())
    @State private var appeared = false
    @State private var hovering = false
    @Environment(\.avatarPlane) private var batched
    @Environment(\.avatarGroup) private var group

    // Idle avatars rest while their window is inactive; each animating one updates its layers every frame.
    // A working bot keeps moving, so its progress doesn't look hung beside another app.
    private var awake: Bool {
        visible && appeared && options.animates && !reduceMotion && scene == .active
            && (active || playing || Self.alwaysMoving) && frozenAt == nil && time == nil
    }

    private var running: Bool { awake && (playing || hovering) }

    #if DEBUG
    /// Benchmarks and UI checks run the app in the background, where avatars would otherwise rest.
    private static let alwaysMoving = CommandLine.arguments.contains("--verify-motion")
    #else
    private static let alwaysMoving = false
    #endif

    private var backing: Color {
        if let paper { return paper }
        #if os(macOS)
        return Color(nsColor: .windowBackgroundColor)
        #else
        return Color(uiColor: .systemBackground)
        #endif
    }

    private var ink: Color {
        if options.shape.category != .shapes, let character = options.shape.character {
            return Color(avatarHex: character.color)
        }
        return options.color.avatarColor(scheme)
    }

    /// Dots and marks sit on the background, so a body colour close to it is nudged until it shows.
    private var markInk: Color {
        guard options.shape.category != .shapes, let character = options.shape.character else { return ink }
        return Color(avatarHex: legible(character.color, on: scheme == .dark ? "#1E1E1E" : "#FFFFFF"))
    }

    var body: some View {
        Group {
            if batched {
                Color.clear.anchorPreference(key: AvatarTiles.self, value: .bounds) {
                    [AvatarAnchor(tile: AvatarSlot(canvas: canvas, input: input, rect: .null,
                                                  group: group?.id, order: group?.order ?? 0, gap: overlapGap),
                                  bounds: $0)]
                }
            } else {
                AvatarSurface(tiles: [AvatarSlot(canvas: canvas, input: input, rect: .null)])
            }
        }
        .aspectRatio(1, contentMode: .fit)
        .onHover { inside in canvas.motion.petted = interactive && inside }
        .simultaneousGesture(TapGesture().onEnded {
            if interactive && canvas.moving { canvas.motion.twirling = true }
        })
        #if !os(macOS)
        .onContinuousHover { phase in
            switch phase {
            case .active(let point):
                canvas.motion.hover = point
                hovering = options.followCursor || interactive
            case .ended:
                canvas.motion.hover = nil
                hovering = false
            }
        }
        #endif
        .onChange(of: poke, initial: true) { _, poke in
            // A view that appears just after the poke, like a new bot's, still plays it.
            if let poke, Date().timeIntervalSince(poke.at) < 1 { canvas.motion.queue(poke.moves) }
        }
        .onChange(of: cueID, initial: true) { _, cue in
            guard cue != nil else { return }
            if mood == .celebrate { canvas.motion.queue([.spin(turns: 1), .bounce]) }
            else if mood == .surprised { canvas.motion.queue([.bounce]) }
        }
        .onAppear { appeared = true; canvas.motion.suspend() }
        .onDisappear { appeared = false; canvas.stop() }
        .onChange(of: running) { _, _ in canvas.motion.suspend() }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("\(options.shape.label) avatar, \(state.label)")
    }

    /// What the renderer needs, resolved once per view update rather than per frame.
    private var input: AvatarInput {
        AvatarInput(options: options, state: state, expression: expression, playing: playing, frozenAt: frozenAt,
                    timeline: timeline, time: time, repeating: repeating, cueID: cueID, mood: mood,
                    interactive: interactive, personality: personality,
                    reduceMotion: reduceMotion, running: running, settles: awake, gazing: hovering,
                    ink: AvatarCanvasState.resolve(ink, dark: scheme == .dark),
                    markInk: AvatarCanvasState.resolve(markInk, dark: scheme == .dark),
                    backing: AvatarCanvasState.resolve(backing, dark: scheme == .dark),
                    eye: AvatarCanvasState.resolve(options.shape.category != .shapes
                         ? Color(avatarHex: options.shape.character?.eyeColor ?? "#231A20")
                         : mood == .blocked ? options.color.eyeColor(scheme) : backing,
                         dark: scheme == .dark),
                    dark: scheme == .dark, tracksPointer: awake && (options.followCursor || interactive))
    }

}

@MainActor final class AvatarMotion {
    typealias Factory = @Sendable (Double, StateID, [Double]?, BotExpression?) -> any AvatarEngine

    nonisolated let makeEngine: Factory
    private var engine: (any AvatarEngine)?
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
    private var wasRunning = false
    private var lastMood: MoodID?
    /// Still easing after playing stopped: into its rest, out of a gaze, or through a poke.
    private(set) var settling = false
    /// Stopped short of its rest, say by its window going inactive, and owed the rest of the move.
    private var unsettled = false
    var hover: CGPoint?
    var size = CGSize.zero
    var interactive = false
    var personality = ""
    var petted = false
    var twirling = false

    private var queued: [Poke] = []

    nonisolated init(makeEngine: @escaping Factory = { scale, initial, shape, expression in
        BotEngine(scale: scale, initial: initial, shape: shape, expression: expression)
    }) {
        self.makeEngine = makeEngine
    }

    func suspend() { lastDate = nil }

    func queue(_ moves: [Poke]) { queued += moves }

    func frame(date: Date, options next: AvatarOptions, state: StateID, expression: ExpressionID?,
               running: Bool, settles: Bool, gazing: Bool, still: Bool, frozenAt: Double?, time: Double?,
               timeline: [AvatarBlock]?, reduceMotion: Bool, repeating: Bool, cueID: String?,
               mood: MoodID?) -> BotFrame {
        // A resting avatar keeps drawing until whatever was moving settles.
        let live = running || (settles && (unsettled || wasRunning))
        let stopped = wasRunning
        wasRunning = running
        if live, let lastDate { clock += min(0.064, max(0, date.timeIntervalSince(lastDate))) }
        lastDate = live ? date : nil
        let staticPose = reduceMotion || (!next.animates && time == nil && frozenAt == nil)
            || ((mood == .spawning || mood == .blocked) && !running && time == nil && frozenAt == nil)
        let controlled = frozenAt != nil || time != nil || staticPose
        let now = staticPose ? 1 : max(0, frozenAt ?? time ?? clock)
        let effective = expression ?? next.expression
        let changed = options?.shape != next.shape || appliedExpression != effective
        if engine == nil || timeline != lastBlocks || staticPose != wasStatic
            || (controlled && (changed || mood != lastMood || now < lastTime)) {
            rebuild(next, expression: effective, initial: timeline?.first?.state ?? state)
            playback = StatePlayback()
            blockIndex = 0
            appliedExpression = effective
            lastTime = -1
        }
        wasStatic = staticPose
        lastMood = mood
        let engine = engine!
        engine.interactive = interactive && live && !controlled && state == .idle && (mood == nil || mood == .idle)
        engine.live = live && !controlled
        engine.setGaze(gazing && !controlled ? gaze : nil)
        engine.personality = personality.utf8.reduce(UInt32(2166136261)) { ($0 ^ UInt32($1)) &* 16777619 }
        engine.pet(engine.interactive && petted, now)
        if twirling && engine.interactive { engine.twirl(now) }
        twirling = false
        engine.displaySize = min(size.width, size.height)
        engine.reducedMotion = reduceMotion || !next.animates || (mood == .blocked && staticPose)
        if running && !staticPose { for move in queued { engine.poke(move, now) } }
        queued = []
        let pointer = self.pointer
        let interaction = pointer != lastPointer && pointer != nil
        lastPointer = pointer
        let activeMood = staticPose && mood == .spawning ? .idle : mood
        // Still avatars rest facing up and to the right; paused ones hold their last frame.
        engine.resting = still || staticPose
        engine.setMood(timeline == nil ? activeMood ?? MoodID(expression: effective) : nil, interaction: interaction)
        if controlled, lastTime < 0 { _ = engine.sample(0) }
        if changed, !controlled {
            // An avatar that can't draw again can't carry a morph, so it takes the new look at once.
            let start = settles ? clock : clock - BotEngine.SHAPE_MORPH
            engine.setShape(next.shape.radii, start)
            engine.solid = next.shape.solid
            engine.character = next.shape.character
            engine.setExpression(effective.expression, start)
            appliedExpression = effective
            if options != nil { unsettled = true }
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
                            self.engine!.setState(prior.state, start, restart: false)
                            start += prior.duration
                        }
                    }
                    blockIndex = index
                    lastTime = now
                    lastBlocks = blocks
                    self.options = next
                    aim(self.engine!, state: block.state, now: now, follows: next.followCursor && !reduceMotion)
                    settling = false
                    return self.engine!.sample(now)
                }
                offset += block.duration
            }
        }
        playback.update(engine, state: state, now: now, controlled: controlled,
                        repeating: repeating, cueID: cueID)
        // Following the pointer is for playing avatars; resting ones face it through the gaze.
        aim(engine, state: state, now: now, follows: next.followCursor && !reduceMotion && frozenAt == nil && !still)
        self.options = next
        lastTime = now
        lastBlocks = timeline
        let frame = engine.sample(now)
        unsettled = (live || stopped || unsettled) && !controlled && !engine.settled(now)
        settling = settles && unsettled
        return frame
    }

    private func rebuild(_ options: AvatarOptions, expression: ExpressionID, initial: StateID) {
        engine = makeEngine(100, initial, options.shape.radii, expression.expression)
        engine?.solid = options.shape.solid
        engine?.character = options.shape.character
        aimed = false
        cycleNumber = nil
    }

    private func aim(_ engine: any AvatarEngine, state: StateID, now: Double, follows: Bool) {
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

    /// Where the pointer is, x right and y down, with full reach at twice the avatar's width.
    private var pointer: CGPoint? { offset(reach: 2) }

    /// Grok Bot's hover gaze, x right and y up, with full reach at the avatar's edge.
    private var gaze: [Double]? { offset(reach: 0.5).map { [$0.x, -$0.y] } }

    /// The pointer's offset from the avatar's centre in `reach` avatar widths, clamped on each axis.
    private func offset(reach: Double) -> CGPoint? {
        guard let hover else { return nil }
        let width = max(1, size.width) * reach
        let x = (hover.x - size.width / 2) / width, y = (hover.y - size.height / 2) / width
        return CGPoint(x: max(-1, min(1, x)), y: max(-1, min(1, y)))
    }
}
