import SwiftUI
import AvatarKit

/// The website's floating voxel island, small: the app on its tower, bots and what they do around it.
struct SetupIsland: View {
    @Environment(\.accessibilityReduceMotion) private var still

    private static let half: CGFloat = 36
    private static let step: CGFloat = 18
    private static let cube: CGFloat = 30
    private static let radius = 5.4

    private struct Cell: Hashable { let i: Int; let j: Int }

    private struct Pad {
        let cell: Cell
        let hue: Double
        let symbol: String
        let tint: Color
    }

    private struct Bot {
        let cell: Cell
        let shape: ShapeID
        let color: ColorID
        let phase: Double
    }

    private static let hub = [Cell(i: 0, j: 0), Cell(i: 1, j: 0), Cell(i: 0, j: 1), Cell(i: 1, j: 1)]
    private static let pads = [
        Pad(cell: Cell(i: 2, j: -4), hue: 217, symbol: "bubble.left.and.bubble.right.fill", tint: .blue),
        Pad(cell: Cell(i: -4, j: 1), hue: 330, symbol: "waveform", tint: .pink),
        Pad(cell: Cell(i: 2, j: 2), hue: 170, symbol: "iphone", tint: .teal),
    ]
    private static let bots = [
        Bot(cell: Cell(i: -1, j: 3), shape: .penguin, color: .ink, phase: 0),
        Bot(cell: Cell(i: 3, j: -1), shape: .panda, color: .ink, phase: 1.3),
        Bot(cell: Cell(i: -3, j: 0), shape: .fox, color: .orange, phase: 2.1),
        Bot(cell: Cell(i: 4, j: 1), shape: .heart, color: .pink, phase: 0.7),
        Bot(cell: Cell(i: 0, j: 4), shape: .circle, color: .blue, phase: 2.8),
    ]

    /// Every column's height: floor 1, feature pads 2, the app's tower 4.
    private static let heights: [Cell: Int] = {
        var heights: [Cell: Int] = [:]
        for i in -7...7 {
            for j in -7...7 {
                let distance = hypot(Double(i), Double(j) * 1.04)
                if distance <= radius || (distance <= radius + 1 && noise(i, j) > 0.8) {
                    heights[Cell(i: i, j: j)] = 1
                }
            }
        }
        for pad in pads {
            for (di, dj) in [(0, 0), (1, 0), (0, 1), (1, 1)] { heights[Cell(i: pad.cell.i + di, j: pad.cell.j + dj)] = 2 }
        }
        for cell in hub { heights[cell] = 4 }
        return heights
    }()

    private static let occupied: Set<Cell> = Set(bots.map(\.cell))
    private static let trees: [Cell] = heights.compactMap { cell, height in
        height == 1 && !occupied.contains(cell) && noise(cell.i - 5, cell.j + 8) > 0.82 ? cell : nil
    }

    var body: some View {
        GeometryReader { proxy in
            let frame = Self.bounds
            let scale = min(proxy.size.width / frame.width, proxy.size.height / frame.height)
            let origin = CGPoint(x: (proxy.size.width - frame.width * scale) / 2 - frame.minX * scale,
                                 y: (proxy.size.height - frame.height * scale) / 2 - frame.minY * scale)
            let place = { (point: CGPoint) in CGPoint(x: origin.x + point.x * scale, y: origin.y + point.y * scale) }
            TimelineView(.animation(paused: still)) { timeline in
                let time = still ? 0 : timeline.date.timeIntervalSinceReferenceDate
                let drift = CGFloat(sin(time * 0.9)) * 4
                ZStack(alignment: .topLeading) {
                    Canvas { context, _ in
                        context.translateBy(x: origin.x, y: origin.y)
                        context.scaleBy(x: scale, y: scale)
                        Self.draw(&context)
                    }
                    ForEach(Array(Self.pads.enumerated()), id: \.offset) { index, pad in
                        let bob = CGFloat(sin(time * 1.6 + Double(index) * 1.9)) * 3
                        chip(pad.symbol, tint: pad.tint, size: 40 * scale)
                            .position(place(Self.padTop(pad)).offset(y: -34 * scale + bob))
                    }
                    AppIcon()
                        .frame(width: 92 * scale, height: 92 * scale)
                        .shadow(color: .black.opacity(0.25), radius: 6 * scale, y: 4 * scale)
                        .position(place(Self.point(Cell(i: 0, j: 0), height: 4)).offset(y: Self.step * scale - 52 * scale
                            + CGFloat(sin(time * 1.2)) * 2))
                    ForEach(Array(Self.bots.enumerated()), id: \.offset) { _, bot in
                        let hop = abs(CGFloat(sin(time * 2.2 + bot.phase))) * 5 * scale
                        let spot = place(Self.point(bot.cell, height: 1))
                        Ellipse()
                            .fill(.black.opacity(0.22))
                            .frame(width: 40 * scale, height: 13 * scale)
                            .position(spot)
                        AvatarView(options: Self.options(bot), playing: true)
                            .frame(width: 56 * scale, height: 56 * scale)
                            .position(spot.offset(y: -28 * scale - hop))
                    }
                }
                .offset(y: drift)
            }
        }
        .aspectRatio(Self.bounds.width / Self.bounds.height, contentMode: .fit)
        .accessibilityHidden(true)
    }

    private func chip(_ symbol: String, tint: Color, size: CGFloat) -> some View {
        Image(systemName: symbol)
            .font(.system(size: size * 0.48, weight: .semibold))
            .foregroundStyle(tint)
            .frame(width: size, height: size)
            .background(.white, in: RoundedRectangle(cornerRadius: size * 0.28, style: .continuous))
            .shadow(color: .black.opacity(0.2), radius: size * 0.15, y: size * 0.1)
    }

    private static func options(_ bot: Bot) -> AvatarOptions {
        var options = AvatarOptions()
        options.shape = bot.shape
        options.color = bot.color
        options.followCursor = false
        return options
    }

    // MARK: Geometry, as on the website: x = (i − j)·half, y = (i + j)·step − height·cube.

    private static func point(_ cell: Cell, height: Int? = nil) -> CGPoint {
        let h = CGFloat(height ?? heights[cell] ?? 1)
        return CGPoint(x: CGFloat(cell.i - cell.j) * half, y: CGFloat(cell.i + cell.j) * step - h * cube)
    }

    /// The middle of a pad's 2×2 top.
    private static func padTop(_ pad: Pad) -> CGPoint {
        point(pad.cell, height: 2).offset(y: step)
    }

    /// Columns reach further down towards the middle, like an upturned cone.
    private static func underside(_ cell: Cell) -> CGFloat {
        let r = hypot(Double(cell.i), Double(cell.j) * 1.04)
        return 12 + 250 * CGFloat(pow(max(0, 1 - r / (radius + 1.4)), 1.6))
    }

    private static let bounds: CGRect = {
        var rect = CGRect.null
        for (cell, height) in heights {
            let top = point(cell)
            rect = rect.union(CGRect(x: top.x - half, y: top.y - step - 60,
                                     width: half * 2, height: step * 2 + CGFloat(height) * cube + underside(cell) + 60))
        }
        return rect.insetBy(dx: -8, dy: -8)
    }()

    private static func draw(_ context: inout GraphicsContext) {
        let stone = { (left: Bool) in
            Gradient(stops: left
                ? [.init(color: hsl(250, 0.24, 0.72), location: 0), .init(color: hsl(254, 0.22, 0.46), location: 0.5), .init(color: hsl(260, 0.26, 0.24), location: 1)]
                : [.init(color: hsl(252, 0.20, 0.58), location: 0), .init(color: hsl(256, 0.20, 0.36), location: 0.5), .init(color: hsl(262, 0.26, 0.18), location: 1)])
        }
        let ordered = heights.keys.sorted { ($0.i + $0.j, $0.i) < ($1.i + $1.j, $1.i) }
        let treesByDepth = Dictionary(grouping: trees) { $0.i + $0.j }
        var depth = Int.min
        for cell in ordered {
            if cell.i + cell.j != depth {
                if let row = treesByDepth[depth] { row.forEach { tree($0, in: &context) } }
                depth = cell.i + cell.j
            }
            let height = heights[cell] ?? 1
            let top = point(cell)
            let deep = CGFloat(height) * cube + underside(cell)
            let cap = CGFloat(max(0, height - 1)) * cube
            let look = look(cell, height: height)
            for left in [true, false] {
                let shading = GraphicsContext.Shading.linearGradient(stone(left), startPoint: CGPoint(x: 0, y: -50), endPoint: CGPoint(x: 0, y: 320))
                context.fill(band(top, left: left, from: cap, to: deep), with: shading)
                if let walls = look.walls { context.fill(band(top, left: left, from: 0, to: cap), with: .color(left ? walls.0 : walls.1)) }
            }
            let diamond = diamond(top, width: half, height: step)
            context.fill(diamond, with: .color(look.top))
            context.stroke(diamond, with: .color(look.edge), lineWidth: 1)
        }
        if let row = treesByDepth[depth] { row.forEach { tree($0, in: &context) } }
    }

    private static func look(_ cell: Cell, height: Int) -> (top: Color, edge: Color, walls: (Color, Color)?) {
        if let pad = pads.first(where: { (pad: Pad) in (0...1).contains(cell.i - pad.cell.i) && (0...1).contains(cell.j - pad.cell.j) }) {
            return (hsl(pad.hue, 0.62, 0.82), hsl(pad.hue, 0.7, 0.6), (hsl(pad.hue, 0.24, 0.97), hsl(pad.hue, 0.14, 0.80)))
        }
        if hub.contains(cell) {
            return (hsl(262, 0.54, 0.80), hsl(262, 0.6, 0.96), (hsl(256, 0.30, 0.95), hsl(258, 0.20, 0.77)))
        }
        let shade = noise(cell.i + 9, cell.j - 4)
        return (hsl(250, 0.16 + shade * 0.06, 0.81 + shade * 0.04), .white.opacity(0.4), nil)
    }

    /// A soft voxel tree: trunk, crown and cap.
    private static func tree(_ cell: Cell, in context: inout GraphicsContext) {
        let base = point(cell)
        let purple = noise(cell.i + 2, cell.j + 6) > 0.5
        let crown = purple ? [hsl(268, 0.42, 0.84), hsl(268, 0.26, 0.69), hsl(270, 0.24, 0.58)]
                           : [hsl(152, 0.28, 0.74), hsl(154, 0.22, 0.58), hsl(156, 0.20, 0.47)]
        let cap = purple ? [hsl(268, 0.48, 0.91), hsl(268, 0.30, 0.77), hsl(270, 0.26, 0.66)]
                         : [hsl(150, 0.32, 0.84), hsl(152, 0.24, 0.69), hsl(154, 0.22, 0.57)]
        block(base.offset(y: 2), size: 4, height: 12, colors: [hsl(28, 0.22, 0.70), hsl(28, 0.18, 0.56), hsl(28, 0.16, 0.46)], in: &context)
        block(base.offset(y: -8), size: 15, height: 19, colors: crown, in: &context)
        block(CGPoint(x: base.x + 2, y: base.y - 27), size: 8, height: 9, colors: cap, in: &context)
    }

    private static func block(_ p: CGPoint, size s: CGFloat, height h: CGFloat, colors: [Color], in context: inout GraphicsContext) {
        context.fill(polygon([CGPoint(x: p.x - s, y: p.y - h), CGPoint(x: p.x, y: p.y - h + s / 2), CGPoint(x: p.x, y: p.y + s / 2), CGPoint(x: p.x - s, y: p.y)]), with: .color(colors[1]))
        context.fill(polygon([CGPoint(x: p.x, y: p.y - h + s / 2), CGPoint(x: p.x + s, y: p.y - h), CGPoint(x: p.x + s, y: p.y), CGPoint(x: p.x, y: p.y + s / 2)]), with: .color(colors[2]))
        context.fill(diamond(p.offset(y: -h), width: s, height: s / 2), with: .color(colors[0]))
    }

    private static func band(_ p: CGPoint, left: Bool, from a: CGFloat, to b: CGFloat) -> Path {
        left
            ? polygon([CGPoint(x: p.x - half, y: p.y + a), CGPoint(x: p.x, y: p.y + step + a), CGPoint(x: p.x, y: p.y + step + b), CGPoint(x: p.x - half, y: p.y + b)])
            : polygon([CGPoint(x: p.x, y: p.y + step + a), CGPoint(x: p.x + half, y: p.y + a), CGPoint(x: p.x + half, y: p.y + b), CGPoint(x: p.x, y: p.y + step + b)])
    }

    private static func diamond(_ p: CGPoint, width w: CGFloat, height h: CGFloat) -> Path {
        polygon([CGPoint(x: p.x, y: p.y - h), CGPoint(x: p.x + w, y: p.y), CGPoint(x: p.x, y: p.y + h), CGPoint(x: p.x - w, y: p.y)])
    }

    private static func polygon(_ points: [CGPoint]) -> Path {
        var path = Path()
        path.addLines(points)
        path.closeSubpath()
        return path
    }

    private static func noise(_ i: Int, _ j: Int) -> Double {
        let n = sin(Double(i) * 127.1 + Double(j) * 311.7) * 43758.5453
        return n - floor(n)
    }

    private static func hsl(_ hue: Double, _ saturation: Double, _ lightness: Double) -> Color {
        let brightness = lightness + saturation * min(lightness, 1 - lightness)
        return Color(hue: hue / 360, saturation: brightness == 0 ? 0 : 2 * (1 - lightness / brightness), brightness: brightness)
    }
}

private extension CGPoint {
    func offset(y dy: CGFloat) -> CGPoint { CGPoint(x: x, y: y + dy) }
}
