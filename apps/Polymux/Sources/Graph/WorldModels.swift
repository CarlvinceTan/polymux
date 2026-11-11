import SwiftUI
import PolymuxKit
#if os(macOS)
import AppKit
#endif

/// Draws solid things on the map's ground. Distances run along the ground's two axes and up, in points.
struct Iso {
    var context: GraphicsContext
    /// Where the ground's origin is on screen.
    var origin: CGPoint

    func point(_ i: Double, _ j: Double, _ z: Double = 0) -> CGPoint {
        CGPoint(x: origin.x + (i - j), y: origin.y + (i + j) / 2 - z)
    }

    private func quad(_ a: CGPoint, _ b: CGPoint, _ c: CGPoint, _ d: CGPoint) -> Path {
        var path = Path()
        path.move(to: a)
        path.addLine(to: b)
        path.addLine(to: c)
        path.addLine(to: d)
        path.closeSubpath()
        return path
    }

    /// What sits under something, so it stands on the ground rather than floating over it.
    mutating func shadow(i: Double, j: Double, radius: Double) {
        let centre = point(i + 3, j + 3)
        let box = CGRect(x: centre.x - radius * 1.5, y: centre.y - radius * 0.75, width: radius * 3, height: radius * 1.5)
        context.fill(Path(ellipseIn: box), with: .color(.black.opacity(0.12)))
    }

    /// A box centred on (i, j), `a` deep and `b` wide, lit from the left.
    mutating func box(i: Double, j: Double, a: Double, b: Double, z: Double = 0, h: Double, color: Color) {
        let near = i + a / 2
        let far = i - a / 2
        let right = j - b / 2
        let left = j + b / 2
        let top = z + h
        context.fill(quad(point(far, left, top), point(near, left, top), point(near, left, z), point(far, left, z)),
                     with: .color(color.mix(with: .white, by: 0.12)))
        context.fill(quad(point(near, left, top), point(near, right, top), point(near, right, z), point(near, left, z)),
                     with: .color(color.mix(with: .black, by: 0.16)))
        context.fill(quad(point(far, right, top), point(near, right, top), point(near, left, top), point(far, left, top)),
                     with: .color(color.mix(with: .white, by: 0.42)))
    }

    /// Draws on the face of a box that looks down and right. The space handed over is `b` wide and `h` tall.
    mutating func right(i: Double, j: Double, b: Double, z: Double, h: Double, _ draw: (inout GraphicsContext, CGSize) -> Void) {
        let corner = point(i, j + b / 2, z + h)
        var layer = context
        layer.concatenate(CGAffineTransform(a: 1, b: -0.5, c: 0, d: 1, tx: corner.x, ty: corner.y))
        draw(&layer, CGSize(width: b, height: h))
    }

    /// Draws on the face of a box that looks down and left. The space handed over is `a` wide and `h` tall.
    mutating func left(i: Double, j: Double, a: Double, z: Double, h: Double, _ draw: (inout GraphicsContext, CGSize) -> Void) {
        let corner = point(i - a / 2, j, z + h)
        var layer = context
        layer.concatenate(CGAffineTransform(a: 1, b: 0.5, c: 0, d: 1, tx: corner.x, ty: corner.y))
        draw(&layer, CGSize(width: a, height: h))
    }

    /// Gives anything drawn on the plane facing down and right real thickness: it's drawn again and again back along
    /// the ground, then once more in front. The closure hears whether it's drawing the face or the body behind it.
    mutating func extrude(i: Double, j: Double, b: Double, z: Double = 0, h: Double, depth: Double,
                          _ draw: (inout GraphicsContext, CGSize, Bool) -> Void) {
        for step in stride(from: depth, to: 0, by: -0.75) {
            right(i: i - step, j: j, b: b, z: z, h: h) { layer, size in draw(&layer, size, false) }
        }
        right(i: i, j: j, b: b, z: z, h: h) { layer, size in draw(&layer, size, true) }
    }

    /// A rounded panel standing upright and facing down and right, such as a screen: `b` wide, `h` tall and `depth` thick.
    mutating func panel(i: Double, j: Double, b: Double, z: Double = 0, h: Double, depth: Double = 4, radius: Double = 5,
                        edge: Color, face: Color, _ draw: (inout GraphicsContext, CGSize) -> Void = { _, _ in }) {
        let shape = Path(roundedRect: CGRect(x: 0, y: 0, width: b, height: h), cornerRadius: radius, style: .continuous)
        extrude(i: i, j: j, b: b, z: z, h: h, depth: depth) { layer, size, front in
            layer.fill(shape, with: .color(front ? face : edge))
            guard front else { return }
            layer.clip(to: shape)
            draw(&layer, size)
        }
    }

    /// A pyramid standing on (i, j), `a` across its base.
    mutating func pyramid(i: Double, j: Double, a: Double, z: Double = 0, h: Double, color: Color) {
        let apex = point(i, j, z + h)
        var left = Path()
        left.move(to: apex)
        left.addLine(to: point(i - a / 2, j + a / 2, z))
        left.addLine(to: point(i + a / 2, j + a / 2, z))
        left.closeSubpath()
        var right = Path()
        right.move(to: apex)
        right.addLine(to: point(i + a / 2, j + a / 2, z))
        right.addLine(to: point(i + a / 2, j - a / 2, z))
        right.closeSubpath()
        context.fill(left, with: .color(color.mix(with: .white, by: 0.22)))
        context.fill(right, with: .color(color.mix(with: .black, by: 0.14)))
    }

    /// A drum standing on (i, j).
    mutating func drum(i: Double, j: Double, radius: Double, z: Double = 0, h: Double, color: Color) {
        let rx = radius * 1.414
        let ry = radius * 0.707
        let foot = point(i, j, z)
        let head = point(i, j, z + h)
        var side = Path()
        side.addEllipse(in: CGRect(x: foot.x - rx, y: foot.y - ry, width: rx * 2, height: ry * 2))
        side.addRect(CGRect(x: foot.x - rx, y: head.y, width: rx * 2, height: foot.y - head.y))
        context.fill(side, with: .linearGradient(Gradient(colors: [color.mix(with: .white, by: 0.14), color.mix(with: .black, by: 0.18)]),
                                                 startPoint: CGPoint(x: foot.x - rx, y: 0), endPoint: CGPoint(x: foot.x + rx, y: 0)))
        context.fill(Path(ellipseIn: CGRect(x: head.x - rx, y: head.y - ry, width: rx * 2, height: ry * 2)), with: .color(color.mix(with: .white, by: 0.45)))
    }

    static func symbol(_ name: String, in layer: inout GraphicsContext, box: CGRect, color: Color, weight: Font.Weight = .semibold) {
        // Symbols run wider than their point size, so they're set a little under the room they have.
        let size = min(box.width, box.height) * 0.74
        let text = Text(Image(systemName: name)).font(.system(size: size, weight: weight)).foregroundStyle(color)
        layer.draw(layer.resolve(text), at: CGPoint(x: box.midX, y: box.midY))
    }
}

/// Real icons for apps installed on this Mac, so Safari looks like Safari. Anything else falls back to a symbol.
@MainActor enum WorldIcons {
    private static var cache: [String: Image?] = [:]

    static func app(_ name: String) -> Image? {
        #if os(macOS)
        if let known = cache[name] { return known }
        let folders = ["/Applications", "/System/Applications", "/System/Applications/Utilities", "/System/Cryptexes/App/System/Applications",
                       NSHomeDirectory() + "/Applications"]
        let found = folders.map { "\($0)/\(name).app" }.first { FileManager.default.fileExists(atPath: $0) }
        // Asked for large: an icon's default size is small, which draws soft once it's on the map.
        var box = CGRect(x: 0, y: 0, width: 512, height: 512)
        // Resolved first, or an app that's a link elsewhere draws with a shortcut arrow on it.
        let image = found.map { URL(fileURLWithPath: $0).resolvingSymlinksInPath().path }
            .flatMap { NSWorkspace.shared.icon(forFile: $0).cgImage(forProposedRect: &box, context: nil, hints: nil) }
            .map { Image(decorative: $0, scale: 1).interpolation(.high) }
        cache[name] = image
        return image
        #else
        return nil
        #endif
    }
}

/// What each kind of station looks like. A station is recognised by its model first; its name is a detail.
@MainActor enum WorldModel {
    /// How far back from the middle of its plot a model stands, leaving the front for whoever's working there.
    static let back = -14.0

    /// How tall a station's model is, for whatever floats over it.
    static func height(_ station: WorldStation) -> Double {
        if station.small { return 34 }
        switch station.node.kind {
        case WorkKind.server, WorkKind.web: return 66
        case WorkKind.phone, WorkKind.desktop, WorkKind.terminal, WorkKind.studio: return 56
        case WorkKind.app, WorkKind.plugin, WorkKind.tab: return 62
        case WorkKind.step: return 54
        case WorkKind.tablet, WorkKind.files, WorkKind.github: return 50
        default: return 42
        }
    }

    static func draw(_ station: WorldStation, in context: inout GraphicsContext, at centre: CGPoint, tint: Color, symbol: String) {
        var iso = Iso(context: context, origin: centre)
        let c = station.small ? -4.0 : back
        if station.small {
            small(station, &iso, c, tint: tint, symbol: symbol)
            return
        }
        switch station.node.kind {
        case WorkKind.phone: phone(&iso, c, width: 22, height: 44)
        case WorkKind.tablet: phone(&iso, c, width: 36, height: 46)
        case WorkKind.laptop, WorkKind.device: laptop(&iso, c)
        case WorkKind.desktop: desktop(&iso, c)
        case WorkKind.server: server(&iso, c)
        case WorkKind.terminal: terminal(&iso, c)
        case WorkKind.files: cabinet(&iso, c, tint: tint)
        case WorkKind.web: globe(&iso, c, tint: tint)
        case WorkKind.studio: easel(&iso, c, tint: tint)
        case WorkKind.repo: crate(&iso, c, tint: tint, symbol: "chevron.left.forwardslash.chevron.right", lift: 0)
        case WorkKind.github: crate(&iso, c, tint: tint, symbol: symbol, lift: 14)
        case WorkKind.step: step(station, &iso, c, tint: tint, symbol: symbol)
        default: tile(&iso, c, tint: tint, symbol: symbol, icon: WorldIcons.app(station.node.title))
        }
    }

    private static let body = Color(white: 0.2)
    private static let metal = Color(white: 0.78)
    private static let glass = Color(red: 0.36, green: 0.62, blue: 0.98)

    /// A lit screen: sky at the top fading down, as a device shows when it's on.
    private static func screen(_ layer: inout GraphicsContext, _ size: CGSize, inset: Double = 2.5, radius: Double = 3) {
        let box = CGRect(origin: .zero, size: size).insetBy(dx: inset, dy: inset)
        layer.fill(Path(roundedRect: box, cornerRadius: radius),
                   with: .linearGradient(Gradient(colors: [glass.mix(with: .white, by: 0.35), glass]), startPoint: box.origin, endPoint: CGPoint(x: box.minX, y: box.maxY)))
    }

    private static func phone(_ iso: inout Iso, _ c: Double, width: Double, height: Double) {
        iso.shadow(i: c, j: c, radius: width * 0.5)
        iso.panel(i: c, j: c, b: width, z: 1, h: height, depth: 4, radius: 5, edge: body.mix(with: .white, by: 0.25), face: body) { layer, size in
            screen(&layer, size, inset: 2, radius: 3.5)
            // The home bar, which is what makes a lit slab read as a phone.
            layer.fill(Path(roundedRect: CGRect(x: size.width / 2 - 4, y: size.height - 6, width: 8, height: 1.6), cornerRadius: 0.8), with: .color(.white.opacity(0.9)))
        }
    }

    private static func laptop(_ iso: inout Iso, _ c: Double) {
        iso.shadow(i: c + 4, j: c, radius: 24)
        iso.box(i: c + 6, j: c, a: 30, b: 44, h: 3, color: metal)
        // The keyboard, as a darker patch on the deck.
        iso.box(i: c + 3, j: c, a: 14, b: 36, z: 3, h: 0.6, color: body.mix(with: .white, by: 0.25))
        iso.panel(i: c - 8, j: c, b: 44, z: 3, h: 28, depth: 2.5, radius: 3, edge: metal.mix(with: .black, by: 0.15), face: body) { layer, size in
            screen(&layer, size, inset: 2, radius: 2)
        }
    }

    private static func desktop(_ iso: inout Iso, _ c: Double) {
        iso.shadow(i: c, j: c, radius: 24)
        iso.box(i: c, j: c, a: 14, b: 20, h: 2.5, color: metal)
        iso.box(i: c - 2, j: c, a: 3, b: 6, z: 2.5, h: 14, color: metal.mix(with: .black, by: 0.1))
        iso.panel(i: c, j: c, b: 52, z: 14, h: 34, depth: 3, radius: 4, edge: metal.mix(with: .black, by: 0.15), face: body) { layer, size in
            screen(&layer, CGSize(width: size.width, height: size.height - 5), inset: 2.5, radius: 2)
            layer.fill(Path(CGRect(x: 0, y: size.height - 6, width: size.width, height: 6)), with: .color(metal))
        }
    }

    /// A rack: dark, tall, with a row of lights per shelf.
    private static func server(_ iso: inout Iso, _ c: Double) {
        iso.shadow(i: c, j: c, radius: 22)
        let color = Color(white: 0.32)
        iso.box(i: c, j: c, a: 28, b: 28, h: 58, color: color)
        iso.right(i: c + 14, j: c, b: 28, z: 0, h: 58) { layer, size in
            for shelf in 0..<5 {
                let y = 6 + Double(shelf) * 10.5
                layer.fill(Path(roundedRect: CGRect(x: 3, y: y, width: size.width - 6, height: 7), cornerRadius: 1.5), with: .color(.black.opacity(0.35)))
                layer.fill(Path(ellipseIn: CGRect(x: 6, y: y + 2.2, width: 2.6, height: 2.6)), with: .color(shelf == 3 ? .orange : .green))
                layer.fill(Path(ellipseIn: CGRect(x: 10.5, y: y + 2.2, width: 2.6, height: 2.6)), with: .color(.green))
            }
        }
        iso.left(i: c, j: c + 14, a: 28, z: 0, h: 58) { layer, size in
            for shelf in 0..<5 {
                layer.fill(Path(CGRect(x: 4, y: 9 + Double(shelf) * 10.5, width: size.width - 8, height: 1)), with: .color(.black.opacity(0.25)))
            }
        }
    }

    /// A chunky dark screen with a prompt on it.
    private static func terminal(_ iso: inout Iso, _ c: Double) {
        iso.shadow(i: c, j: c, radius: 24)
        iso.box(i: c - 2, j: c, a: 16, b: 30, h: 5, color: Color(white: 0.5))
        iso.panel(i: c, j: c, b: 52, z: 5, h: 38, depth: 9, radius: 6, edge: Color(white: 0.42), face: Color(white: 0.12)) { layer, size in
            // A prompt and its cursor, drawn rather than typed so they stay crisp on a slanted screen.
            var prompt = Path()
            prompt.move(to: CGPoint(x: 10, y: 10))
            prompt.addLine(to: CGPoint(x: 18, y: 16))
            prompt.addLine(to: CGPoint(x: 10, y: 22))
            layer.stroke(prompt, with: .color(.green), style: StrokeStyle(lineWidth: 3, lineCap: .round, lineJoin: .round))
            layer.fill(Path(roundedRect: CGRect(x: 22, y: 20, width: 12, height: 3), cornerRadius: 1.5), with: .color(.green))
            _ = size
        }
    }

    /// A filing cabinet: three drawers, each with a handle and a label.
    private static func cabinet(_ iso: inout Iso, _ c: Double, tint: Color) {
        iso.shadow(i: c, j: c, radius: 22)
        iso.box(i: c, j: c, a: 30, b: 32, h: 46, color: tint)
        iso.right(i: c + 15, j: c, b: 32, z: 0, h: 46) { layer, size in
            for drawer in 0..<3 {
                let box = CGRect(x: 3, y: 3 + Double(drawer) * 14.3, width: size.width - 6, height: 12)
                layer.fill(Path(roundedRect: box, cornerRadius: 1.5), with: .color(.white.opacity(0.22)))
                layer.fill(Path(roundedRect: CGRect(x: box.midX - 5, y: box.minY + 2.2, width: 10, height: 3.4), cornerRadius: 0.8), with: .color(.white.opacity(0.9)))
                layer.fill(Path(roundedRect: CGRect(x: box.midX - 4, y: box.minY + 7.6, width: 8, height: 2), cornerRadius: 1), with: .color(.black.opacity(0.4)))
            }
        }
    }

    /// A globe on a stand.
    private static func globe(_ iso: inout Iso, _ c: Double, tint: Color) {
        iso.shadow(i: c, j: c, radius: 18)
        iso.drum(i: c, j: c, radius: 9, h: 4, color: Color(white: 0.6))
        iso.drum(i: c, j: c, radius: 2.2, z: 4, h: 12, color: Color(white: 0.5))
        let centre = iso.point(c, c, 38)
        let radius = 24.0
        let ball = CGRect(x: centre.x - radius, y: centre.y - radius, width: radius * 2, height: radius * 2)
        iso.context.fill(Path(ellipseIn: ball), with: .radialGradient(Gradient(colors: [tint.mix(with: .white, by: 0.5), tint, tint.mix(with: .black, by: 0.2)]),
                                                                    center: CGPoint(x: centre.x - 8, y: centre.y - 9), startRadius: 1, endRadius: radius * 1.5))
        var lines = iso.context
        lines.clip(to: Path(ellipseIn: ball))
        let stroke = StrokeStyle(lineWidth: 1.3)
        for squeeze in [0.38, 0.78] {
            lines.stroke(Path(ellipseIn: CGRect(x: centre.x - radius * squeeze, y: ball.minY, width: radius * squeeze * 2, height: radius * 2)), with: .color(.white.opacity(0.75)), style: stroke)
        }
        for offset in [-0.5, 0, 0.5] {
            var line = Path()
            line.move(to: CGPoint(x: ball.minX, y: centre.y + radius * offset))
            line.addQuadCurve(to: CGPoint(x: ball.maxX, y: centre.y + radius * offset), control: CGPoint(x: centre.x, y: centre.y + radius * offset + 7))
            lines.stroke(line, with: .color(.white.opacity(0.75)), style: stroke)
        }
    }

    /// A canvas on an easel, with a picture on it.
    private static func easel(_ iso: inout Iso, _ c: Double, tint: Color) {
        iso.shadow(i: c, j: c, radius: 22)
        let wood = Color.brown
        iso.box(i: c - 3, j: c + 14, a: 3, b: 3, h: 16, color: wood)
        iso.box(i: c - 3, j: c - 14, a: 3, b: 3, h: 16, color: wood)
        iso.box(i: c - 1, j: c, a: 5, b: 44, z: 12, h: 2.5, color: wood)
        iso.panel(i: c, j: c, b: 46, z: 14, h: 36, depth: 3, radius: 2, edge: wood.mix(with: .white, by: 0.3), face: .white) { layer, size in
            let sky = CGRect(x: 3, y: 3, width: size.width - 6, height: size.height - 6)
            layer.fill(Path(sky), with: .color(tint.mix(with: .white, by: 0.72)))
            var picture = layer
            picture.clip(to: Path(sky))
            picture.fill(Path(ellipseIn: CGRect(x: sky.maxX - 15, y: sky.minY + 4, width: 9, height: 9)), with: .color(.yellow))
            var hills = Path()
            hills.move(to: CGPoint(x: sky.minX, y: sky.maxY))
            hills.addLine(to: CGPoint(x: sky.minX + 13, y: sky.minY + 11))
            hills.addLine(to: CGPoint(x: sky.minX + 22, y: sky.minY + 20))
            hills.addLine(to: CGPoint(x: sky.minX + 29, y: sky.minY + 14))
            hills.addLine(to: CGPoint(x: sky.maxX, y: sky.maxY))
            hills.closeSubpath()
            picture.fill(hills, with: .color(tint))
        }
    }

    /// A shipping crate with what it is stamped on the side. Lifted off the ground, it's somewhere else: a host, not this machine.
    private static func crate(_ iso: inout Iso, _ c: Double, tint: Color, symbol: String, lift: Double) {
        iso.shadow(i: c, j: c, radius: lift > 0 ? 16 : 24)
        let side = lift > 0 ? 30.0 : 38.0
        let height = lift > 0 ? 30.0 : 28.0
        iso.box(i: c, j: c, a: side, b: side, z: lift, h: height, color: tint)
        // The lid, a shade lighter and a little proud of the box.
        iso.box(i: c, j: c, a: side + 3, b: side + 3, z: lift + height, h: 4, color: tint.mix(with: .white, by: 0.2))
        iso.right(i: c + side / 2, j: c, b: side, z: lift, h: height) { layer, size in
            Iso.symbol(symbol, in: &layer, box: CGRect(origin: .zero, size: size).insetBy(dx: 5, dy: 5), color: .white, weight: .bold)
        }
    }

    /// An app, standing up as its icon with real thickness: the real icon when this Mac has the app, or its symbol on its colour.
    private static func tile(_ iso: inout Iso, _ c: Double, tint: Color, symbol: String, icon: Image?) {
        iso.shadow(i: c, j: c, radius: 24)
        iso.box(i: c - 4, j: c, a: 16, b: 40, h: 3, color: Color(white: 0.86))
        let side = 54.0
        if let icon {
            iso.extrude(i: c, j: c, b: side, z: 2, h: side, depth: 9) { layer, size, front in
                // The body takes the icon's own outline, darkened, so the thickness follows its shape.
                if !front { layer.addFilter(.colorMultiply(Color(white: 0.55))) }
                // Mac icons carry their own margin, so they're drawn a little past the edge.
                layer.draw(icon, in: CGRect(origin: .zero, size: size).insetBy(dx: -5, dy: -5))
            }
            return
        }
        let shape = Path(roundedRect: CGRect(x: 2, y: 2, width: side - 4, height: side - 4), cornerRadius: 12, style: .continuous)
        iso.extrude(i: c, j: c, b: side, z: 2, h: side, depth: 9) { layer, size, front in
            guard front else {
                layer.fill(shape, with: .color(tint.mix(with: .black, by: 0.3)))
                return
            }
            layer.fill(shape, with: .linearGradient(Gradient(colors: [tint.mix(with: .white, by: 0.3), tint]), startPoint: .zero, endPoint: CGPoint(x: 0, y: size.height)))
            let box = CGRect(origin: .zero, size: size).insetBy(dx: 12, dy: 12)
            // Pressed in a little, so the mark reads as part of the object.
            Iso.symbol(symbol, in: &layer, box: box.offsetBy(dx: 0.8, dy: 1.2), color: .black.opacity(0.25), weight: .bold)
            Iso.symbol(symbol, in: &layer, box: box, color: .white, weight: .bold)
        }
    }

    /// A step of a pipeline: a medal on a plinth, coloured once work reaches it and ticked once it's done.
    private static func step(_ station: WorldStation, _ iso: inout Iso, _ c: Double, tint: Color, symbol: String) {
        let color: Color = switch station.state {
        case WorkNodeState.failed: .red
        case WorkNodeState.waiting: .orange
        case WorkNodeState.idle: Color(white: 0.64)
        default: tint
        }
        iso.shadow(i: c, j: c, radius: 20)
        iso.drum(i: c, j: c, radius: 15, h: 5, color: Color(white: 0.9))
        let side = 46.0
        let shape = Path(ellipseIn: CGRect(x: 2, y: 2, width: side - 4, height: side - 4))
        iso.extrude(i: c, j: c, b: side, z: 5, h: side, depth: 7) { layer, size, front in
            guard front else {
                layer.fill(shape, with: .color(color.mix(with: .black, by: 0.3)))
                return
            }
            layer.fill(shape, with: .linearGradient(Gradient(colors: [color.mix(with: .white, by: 0.3), color]), startPoint: .zero, endPoint: CGPoint(x: 0, y: size.height)))
            layer.stroke(Path(ellipseIn: CGRect(x: 5, y: 5, width: side - 10, height: side - 10)), with: .color(.white.opacity(0.45)), lineWidth: 1.5)
            Iso.symbol(symbol, in: &layer, box: CGRect(origin: .zero, size: size).insetBy(dx: 12, dy: 12), color: .white, weight: .bold)
        }
        guard station.state == WorkNodeState.done else { return }
        let at = iso.point(c, c - side / 2 + 4, 5 + side - 6)
        let badge = CGRect(x: at.x - 9, y: at.y - 9, width: 18, height: 18)
        iso.context.fill(Path(ellipseIn: badge.insetBy(dx: -2, dy: -2)), with: .color(.white))
        iso.context.fill(Path(ellipseIn: badge), with: .color(.green))
        Iso.symbol("checkmark", in: &iso.context, box: badge.insetBy(dx: 3, dy: 3), color: .white, weight: .heavy)
    }

    /// Home: where whoever isn't working waits.
    static func house(in context: inout GraphicsContext, at centre: CGPoint) {
        var iso = Iso(context: context, origin: centre)
        let c = -44.0
        iso.shadow(i: c, j: c, radius: 16)
        iso.box(i: c, j: c, a: 26, b: 26, h: 16, color: Color(red: 0.96, green: 0.93, blue: 0.86))
        iso.right(i: c + 13, j: c, b: 26, z: 0, h: 16) { layer, _ in
            layer.fill(Path(roundedRect: CGRect(x: 10, y: 5, width: 7, height: 11), cornerRadius: 1), with: .color(.brown))
        }
        // A roof in two slopes meeting along the ridge.
        let roof = Color(red: 0.86, green: 0.36, blue: 0.3)
        let ridge = (iso.point(c, c + 15, 30), iso.point(c, c - 15, 30))
        var front = Path()
        front.move(to: ridge.0)
        front.addLine(to: ridge.1)
        front.addLine(to: iso.point(c + 16, c - 15, 15))
        front.addLine(to: iso.point(c + 16, c + 15, 15))
        front.closeSubpath()
        var gable = Path()
        gable.move(to: ridge.0)
        gable.addLine(to: iso.point(c + 16, c + 15, 15))
        gable.addLine(to: iso.point(c - 16, c + 15, 15))
        gable.closeSubpath()
        iso.context.fill(gable, with: .color(roof.mix(with: .white, by: 0.25)))
        iso.context.fill(front, with: .color(roof))
    }

    /// Something that stands on its own without being a place: a database is a stack of drums, anything else a marked block.
    private static func small(_ station: WorldStation, _ iso: inout Iso, _ c: Double, tint: Color, symbol: String) {
        iso.shadow(i: c, j: c, radius: 15)
        if [WorkKind.data, WorkKind.table].contains(station.node.kind) {
            for layer in 0..<3 { iso.drum(i: c, j: c, radius: 13, z: Double(layer) * 10, h: 8, color: tint) }
            return
        }
        iso.box(i: c, j: c, a: 24, b: 24, h: 24, color: tint)
        iso.right(i: c + 12, j: c, b: 24, z: 0, h: 24) { layer, size in
            Iso.symbol(symbol, in: &layer, box: CGRect(origin: .zero, size: size).insetBy(dx: 6, dy: 6), color: .white)
        }
    }
}
