import AvatarKit
import QuartzCore

/// One avatar tile as Core Animation layers, so compositing replaces per-frame rasterisation.
@MainActor
final class AvatarLayers {
    let root = CALayer()
    private let space = AvatarLayers.container()
    private let figure = AvatarLayers.container()
    private let body = AvatarLayers.container()
    private let eyes = AvatarLayers.container()
    private let eyeMask = AvatarLayers.container()
    private let props = AvatarLayers.container()
    private let fills = Pool(AvatarLayers.shape)
    private let clips = Pool(AvatarLayers.shape)
    private let masks = Pool(AvatarLayers.shape)
    private let arcs = Pool { ArcLayer() }
    private let gap = CAShapeLayer()
    private var frame: BotFrame?

    init() {
        root.masksToBounds = true
        body.allowsGroupOpacity = true
        eyes.mask = eyeMask
        root.addSublayer(space)
    }

    func apply(_ canvas: AvatarCanvasState, in box: CGRect, scale contents: CGFloat) {
        root.frame = box
        frame = canvas.frame
        guard let input = canvas.input, let frame = canvas.frame, box.width > 0, box.height > 0 else {
            root.isHidden = true
            return
        }
        root.isHidden = false
        gap.contentsScale = contents
        for pool in [fills, clips, masks] { pool.begin(contents) }
        arcs.begin(contents)

        let side = min(box.width, box.height)
        var placement = CGAffineTransform(scaleX: side / BotEngine.FRAME_SIZE, y: side / BotEngine.FRAME_SIZE)
        let work = input.prop ?? (input.showsProps ? WorkProp.activity(input.mood) : nil)
        let working = work != nil && side >= 48
        if working { placement = placement.translatedBy(x: -42, y: -6).scaledBy(x: 0.7, y: 0.7) }
        space.position = CGPoint(x: box.width / 2, y: box.height / 2)
        space.setAffineTransform(placement)
        figure.setAffineTransform(frame.transform)

        var front: [CALayer] = []
        var back: [CALayer] = []
        for arc in frame.arcs { back.append(arcLayer(arc, path: arc.back)) }
        if frame.dotsBehind { back += dots(frame.dots, input: input) }
        compose(frame, input: input)
        if let notch = frame.notch { front.append(fill(Self.circle(notch), input.backing)) }
        if let notice = frame.notif { front.append(fill(Self.circle(notice), input.ink)) }
        if !frame.dotsBehind { front += dots(frame.dots, input: input) }
        for arc in frame.arcs { front.append(arcLayer(arc, path: arc.front)) }
        Self.set(figure, back + [body] + front)

        if working, let work {
            props.setAffineTransform(CGAffineTransform(translationX: 60, y: -28).scaledBy(x: 1.8, y: 1.8))
            let reduced = input.reduceMotion || !input.options.animates || (input.time == nil && !input.running)
            let parts = work.layers(elapsed: input.time ?? canvas.motion.workTime(work), reducedMotion: reduced)
            Self.set(props, parts.map {
                let layer = fill($0.path, AvatarCanvasState.colour($0.color, alpha: $0.alpha))
                layer.fillRule = .evenOdd
                return layer
            })
            Self.set(space, [figure, props])
        } else {
            Self.set(space, [figure])
        }
    }

    /// The silhouette widened by `width`, in tile coordinates: what this avatar cuts from the ones behind it in a stack.
    func cutout(_ width: CGFloat) -> CGPath? {
        guard let frame, width > 0, root.bounds.width > 0 else { return nil }
        let scale = min(root.bounds.width, root.bounds.height) / BotEngine.FRAME_SIZE
        let silhouette = CGMutablePath()
        silhouette.addPath(frame.bodyPath)
        for part in frame.parts where !part.surface { silhouette.addPath(part.outline ?? part.path) }
        let widened = silhouette.copy(strokingWithWidth: width * 2 / scale, lineCap: .round, lineJoin: .round,
                                      miterLimit: 10).union(silhouette)
        var placement = frame.transform.concatenating(CGAffineTransform(scaleX: scale, y: scale))
            .concatenating(CGAffineTransform(translationX: root.bounds.midX, y: root.bounds.midY))
        return widened.copy(using: &placement)
    }

    /// Masks out the avatars in front, so the tile shows only where they leave it uncovered.
    func cut(_ covers: [CGPath]) {
        guard let first = covers.first else {
            root.mask = nil
            return
        }
        gap.frame = root.bounds
        gap.path = CGPath(rect: root.bounds, transform: nil).subtracting(covers.dropFirst().reduce(first) { $0.union($1) })
        gap.fillColor = Self.black
        root.mask = gap
    }

    /// The body group: parts, body and eyes composited together, then faded as one.
    private func compose(_ frame: BotFrame, input: AvatarInput) {
        body.opacity = Float(frame.bodyAlpha)
        let shapes = input.options.shape.category == .shapes
        func part(_ part: CharacterLayer) -> CALayer {
            let colour = shapes ? input.ink.copy(alpha: part.alpha) ?? input.ink
                : AvatarCanvasState.colour(part.color, alpha: part.alpha)
            let layer = fill(part.path, colour)
            if part.surface {
                let clip = clips.take()
                clip.path = frame.bodyPath
                clip.fillColor = Self.black
                layer.mask = clip
            } else {
                layer.mask = nil
            }
            return layer
        }
        var children = frame.parts.filter { !$0.front }.map(part)
        children.append(fill(frame.bodyPath, frame.bare > 0
                             ? AvatarCanvasState.colours(input.ink, input.markInk, frame.bare) : input.ink))
        children += frame.parts.filter(\.front).map(part)

        // Shapes paint eyes only over what is already drawn; characters clip them to the body.
        var cover = [mask(frame.bodyPath, alpha: 1)]
        if input.options.shape.character == nil {
            cover += frame.parts.filter { !$0.surface }.map { mask($0.path, alpha: $0.alpha) }
        }
        Self.set(eyeMask, cover)
        Self.set(eyes, frame.eyes.compactMap { eye in
            var matrix = eye.matrix
            guard let path = eye.d.copy(using: &matrix) else { return nil }
            let layer = fill(path, input.eye)
            layer.opacity = Float(eye.alpha)
            return layer
        })
        children.append(eyes)
        Self.set(body, children)
    }

    private func dots(_ dots: [DotRender], input: AvatarInput) -> [CALayer] {
        dots.sorted { ($0.depth ?? 1) < ($1.depth ?? 1) }.map { dot in
            let colour: CGColor
            if let hex = dot.color { colour = AvatarCanvasState.colour(hex, alpha: dot.opacity) }
            else if let depth = dot.depth { colour = AvatarCanvasState.colours(input.backing, input.markInk, depth, alpha: dot.opacity) }
            else { colour = AvatarCanvasState.colours(input.markInk, input.markInk, 0, alpha: dot.opacity) }
            var transform = CGAffineTransform(translationX: dot.x, y: dot.y)
                .rotated(by: (dot.rot ?? 0) * .pi / 180).scaledBy(x: 100, y: 100)
            let path = dot.d.flatMap { $0.copy(using: &transform) }
                ?? CGPath(ellipseIn: CGRect(x: dot.x - dot.r, y: dot.y - dot.r, width: dot.r * 2, height: dot.r * 2),
                          transform: nil)
            return fill(path, colour)
        }
    }

    private func arcLayer(_ arc: ArcRender, path: CGPath) -> CALayer {
        let layer = arcs.take()
        let box = path.boundingBoxOfPath.insetBy(dx: -arc.width, dy: -arc.width)
        layer.bounds = box
        layer.position = CGPoint(x: box.midX, y: box.midY)
        layer.opacity = Float(arc.opacity)
        let key = arc.grad.stops.joined(separator: ",")
        if layer.key != key {
            layer.key = key
            layer.colors = arc.grad.stops.map { AvatarCanvasState.colour($0) }
            layer.locations = arc.grad.stops.indices.map { NSNumber(value: Double($0) / Double(max(1, arc.grad.stops.count - 1))) }
        }
        let width = max(box.width, 1), height = max(box.height, 1)
        layer.startPoint = CGPoint(x: (arc.grad.x1 - box.minX) / width, y: (arc.grad.y1 - box.minY) / height)
        layer.endPoint = CGPoint(x: (arc.grad.x2 - box.minX) / width, y: (arc.grad.y2 - box.minY) / height)
        layer.stroke.frame = layer.bounds
        layer.stroke.bounds = box
        layer.stroke.path = path
        layer.stroke.lineWidth = arc.width
        return layer
    }

    private func fill(_ path: CGPath, _ colour: CGColor) -> CAShapeLayer {
        let layer = fills.take()
        layer.path = path
        layer.fillColor = colour
        layer.fillRule = .nonZero
        layer.opacity = 1
        layer.mask = nil
        return layer
    }

    private func mask(_ path: CGPath, alpha: Double) -> CALayer {
        let layer = masks.take()
        layer.path = path
        layer.fillColor = Self.black
        layer.opacity = Float(alpha)
        return layer
    }

    private static let black = CGColor(gray: 0, alpha: 1)

    private static func circle(_ dot: FrameDot) -> CGPath {
        CGPath(ellipseIn: CGRect(x: dot.x - dot.r, y: dot.y - dot.r, width: dot.r * 2, height: dot.r * 2), transform: nil)
    }

    /// A layer whose own coordinates are the avatar's, so transforms apply about its origin.
    private static func container() -> CALayer {
        let layer = CALayer()
        place(layer)
        return layer
    }

    private static func shape() -> CAShapeLayer {
        let layer = CAShapeLayer()
        place(layer)
        return layer
    }

    private static func place(_ layer: CALayer) {
        layer.bounds = CGRect(x: -600, y: -600, width: 1200, height: 1200)
        layer.position = .zero
    }

    private static func set(_ layer: CALayer, _ children: [CALayer]) {
        guard !(layer.sublayers ?? []).elementsEqual(children, by: ===) else { return }
        layer.sublayers = children.isEmpty ? nil : children
    }
}

/// A gradient stroke: the gradient fills its box and a stroked path masks it to the arc.
private final class ArcLayer: CAGradientLayer {
    let stroke = CAShapeLayer()
    var key = ""

    override init() {
        super.init()
        stroke.fillColor = nil
        stroke.strokeColor = CGColor(gray: 0, alpha: 1)
        stroke.lineCap = .round
        mask = stroke
    }

    override init(layer: Any) { super.init(layer: layer) }

    @available(*, unavailable)
    required init?(coder: NSCoder) { fatalError() }
}

/// Reuses layers frame to frame so each keeps its role and only changed properties are set.
@MainActor
private final class Pool<Layer: CALayer> {
    private var layers: [Layer] = []
    private var used = 0
    private var scale: CGFloat = 1
    private let make: @MainActor () -> Layer

    init(_ make: @escaping @MainActor () -> Layer) { self.make = make }

    func begin(_ scale: CGFloat) {
        used = 0
        self.scale = scale
    }

    func take() -> Layer {
        if used == layers.count { layers.append(make()) }
        let layer = layers[used]
        used += 1
        if layer.contentsScale != scale {
            layer.contentsScale = scale
            layer.mask?.contentsScale = scale
        }
        return layer
    }
}
