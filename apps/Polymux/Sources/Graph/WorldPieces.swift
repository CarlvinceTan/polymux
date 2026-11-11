import SwiftUI
import ImageIO
import PolymuxKit

/// The map's measurements. A cell is one ground tile, and a plot is the square a station stands on.
enum WorldMetrics {
    /// Half a tile's width on screen. A tile is twice as wide as it is tall.
    static let half = 28.0
    /// Cells across a plot, which leaves a one-cell lane between neighbours.
    static let plot = 4.0
    /// Cells across the paving at home.
    static let home = 6.6
    static let avatar = 64.0
    /// People never draw smaller than this share of full size, however far out the map is: they're what it's about.
    static let actorFloor = 0.72

    static func screen(_ i: Double, _ j: Double) -> CGPoint {
        CGPoint(x: (i - j) * half, y: (i + j) * half / 2)
    }

    static func cell(_ station: WorldStation) -> (i: Double, j: Double) { (Double(station.i), Double(station.j)) }

    /// Cells across the pad a station stands on.
    static func pad(_ station: WorldStation) -> Double { station.small ? 2.4 : plot }

    /// Where the nth visitor stands, in points from the middle of the plot: across its front, clear of the model.
    private static let docks: [(Double, Double)] = [(46, 24), (-46, 24), (0, 46)]
    /// Where the nth actor rests at home: two rows in front of the house, then either side of it.
    private static let seats: [(Double, Double)] = [(-32, 12), (32, 12), (-96, 8), (96, 8), (0, 52), (-64, 48), (64, 48), (-124, -22), (124, -22)]

    /// A point on screen as a place on the ground.
    private static func ground(_ x: Double, _ y: Double) -> (i: Double, j: Double) {
        ((x / half + y / (half / 2)) / 2, (y / (half / 2) - x / half) / 2)
    }

    /// `spread` moves people apart when they're drawn larger than the map around them, so they never stand on each other.
    static func cell(_ actor: WorldActor, in world: WorkWorld, spread: Double = 1) -> (i: Double, j: Double) {
        guard let station = world.station(actor.station) else {
            let seat = seats[actor.order % seats.count]
            // A crowd past what home seats closes up behind the first rows.
            let lap = Double(actor.order / seats.count) * 14
            return ground((seat.0 + lap) * spread, (seat.1 - lap) * spread)
        }
        let centre = cell(station)
        let dock = docks[actor.order % docks.count]
        let reach = (station.small ? 0.6 : 1) * spread
        let row = Double(actor.order / docks.count)
        let spot = ground(dock.0 * reach + row * 18, dock.1 * reach + row * 30)
        return (centre.i + spot.i, centre.j + spot.j)
    }

    static func diamond(at centre: CGPoint, cells: Double) -> Path {
        let w = cells * half
        var path = Path()
        path.move(to: CGPoint(x: centre.x, y: centre.y - w / 2))
        path.addLine(to: CGPoint(x: centre.x + w, y: centre.y))
        path.addLine(to: CGPoint(x: centre.x, y: centre.y + w / 2))
        path.addLine(to: CGPoint(x: centre.x - w, y: centre.y))
        path.closeSubpath()
        return path
    }

    /// The arc something takes through the air between two points: the further it goes, the higher it rises.
    static func arc(_ from: CGPoint, _ to: CGPoint) -> (path: Path, middle: CGPoint) {
        let rise = max(64, hypot(to.x - from.x, to.y - from.y) * 0.3)
        let control = CGPoint(x: (from.x + to.x) / 2, y: min(from.y, to.y) - rise)
        var path = Path()
        path.move(to: from)
        path.addQuadCurve(to: to, control: control)
        return (path, CGPoint(x: from.x / 4 + control.x / 2 + to.x / 4, y: from.y / 4 + control.y / 2 + to.y / 4))
    }
}

/// How one kind of ground looks. The colours lean on the system's, so they follow light and dark.
struct WorldTerrain {
    let top: Color
    let cliff: Color
    let pad: Color
    /// The beach each islet wears where the water reaches.
    let sand: Color

    static func of(_ biome: WorldBiome) -> WorldTerrain {
        let top: Color = switch biome {
        case .plaza: Color.graphCard.mix(with: .yellow, by: 0.14)
        case .meadow: Color.green.mix(with: .graphCard, by: 0.48)
        case .blossom: Color.pink.mix(with: .graphCard, by: 0.6)
        case .desert: Color.yellow.mix(with: .orange, by: 0.3).mix(with: .graphCard, by: 0.42)
        case .crystal: Color.purple.mix(with: .graphCard, by: 0.56)
        case .ice: Color.cyan.mix(with: .graphCard, by: 0.7)
        case .slate: Color.gray.mix(with: .graphCard, by: 0.42)
        case .savanna: Color.orange.mix(with: .graphCard, by: 0.52)
        }
        return WorldTerrain(top: top, cliff: top.mix(with: .brown, by: 0.5).mix(with: .black, by: 0.14), pad: top.mix(with: .graphCard, by: 0.62),
                            sand: Color(red: 0.90, green: 0.82, blue: 0.62).mix(with: .graphCard, by: 0.18))
    }
}

extension Color {
    /// Open water between islets, and the paler water over the shelf around each one.
    static var worldSea: Color { Color.cyan.mix(with: .blue, by: 0.25).mix(with: .graphCard, by: 0.5) }
    static var worldShallows: Color { Color.cyan.mix(with: .graphCard, by: 0.62) }
    static var worldWood: Color { Color.brown.mix(with: .graphCard, by: 0.4) }
}

extension GraphTint {
    /// What's being passed has a colour of its own, so a question never looks like a result.
    static func cargo(_ kind: String?) -> Color {
        switch kind {
        case WorkKind.question: .orange
        case WorkKind.result: .green
        case WorkKind.task: .blue
        case WorkKind.message, WorkKind.status, nil, "": .gray
        default: color(WorkKind.look(kind).family)
        }
    }
}

/// Small pictures of things on the map, fetched once each and kept while the map is open.
@MainActor @Observable
final class WorldPreviews {
    private(set) var images: [String: Image] = [:]
    @ObservationIgnored private var asked: Set<String> = []
    @ObservationIgnored var load: (String) async -> Data? = { _ in nil }

    func image(_ id: String?) -> Image? {
        guard let id else { return nil }
        if let known = images[id] { return known }
        guard asked.insert(id).inserted else { return nil }
        Task {
            guard let data = await load(id), let source = CGImageSourceCreateWithData(data as CFData, nil),
                  let picture = CGImageSourceCreateThumbnailAtIndex(source, 0, [kCGImageSourceCreateThumbnailFromImageAlways: true,
                                                                              kCGImageSourceThumbnailMaxPixelSize: 640] as CFDictionary) else { return }
            images[id] = Image(decorative: picture, scale: 2)
        }
        return nil
    }
}

/// A picture of what a station made, standing over it like a card held up.
struct WorldThumb: View {
    let image: Image
    var width: CGFloat = 66

    var body: some View {
        image.resizable().aspectRatio(contentMode: .fill)
            .frame(width: width, height: width * 0.66)
            .clipShape(.rect(cornerRadius: 5))
            .padding(3)
            .background(Color.white, in: .rect(cornerRadius: 7))
            .overlay { RoundedRectangle(cornerRadius: 7).strokeBorder(Color.black.opacity(0.18), lineWidth: 1) }
            .accessibilityHidden(true)
    }
}

/// One station: its model and what's piled up beside it. Its name waits until someone wants it.
struct WorldStationView: View {
    static let size = CGSize(width: 232, height: 232)
    /// The middle of the plot within the view.
    static let anchor = CGPoint(x: 116, y: 168)

    let station: WorldStation
    let present: Int
    /// Drawn at this size rather than scaled afterwards, which keeps every edge sharp at any zoom.
    let scale: Double
    let selected: Bool
    let named: Bool
    let select: () -> Void
    @Environment(WorldPreviews.self) private var previews
    @State private var hovered = false

    private var look: WorkLook { GraphLook.look(station.node) }
    private var tint: Color { GraphTint.color(station.node) }
    private var working: Bool { station.state == WorkNodeState.working }
    /// Where the top of the model is, down from the top of the view.
    private var crown: Double { (Self.anchor.y + (station.small ? -4 : WorldModel.back) - WorldModel.height(station)) * scale }

    private var label: String {
        var parts = [station.node.title]
        if look.title != station.node.title { parts.append(look.title) }
        if let status = GraphTint.status(station.state), station.state != WorkNodeState.idle { parts.append(status) }
        if present > 0 { parts.append(present == 1 ? "1 here" : "\(present) here") }
        if !station.items.isEmpty { parts.append(station.items.count == 1 ? "1 item" : "\(station.items.count) items") }
        return parts.filter { !$0.isEmpty }.joined(separator: ", ")
    }

    var body: some View {
        let picture = previews.image(station.preview)
        Canvas { context, _ in
            context.scaleBy(x: scale, y: scale)
            draw(&context)
        }
        .frame(width: Self.size.width * scale, height: Self.size.height * scale)
        .overlay(alignment: .top) {
            VStack(spacing: 3) {
                if named || hovered || selected {
                    WorldName(text: station.node.title, selected: selected).transition(.opacity)
                }
                if let picture { WorldThumb(image: picture, width: max(54, 70 * scale)) }
            }
            .frame(width: 180)
            // Its foot rests on the model, however tall the stack over it grows.
            .alignmentGuide(.top) { $0[.bottom] - crown + 6 }
        }
        // Lifts a little under the pointer, the way a piece does when it's about to be picked up.
        .offset(y: hovered ? -3 : 0)
        .contentShape(hit)
        .onTapGesture(perform: select)
        .onHover { hovered = $0 }
        .animation(.easeOut(duration: 0.12), value: hovered)
        .help(station.node.title)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(label)
        .accessibilityAddTraits(selected ? [.isButton, .isSelected] : .isButton)
        .accessibilityAction(.default, select)
    }

    /// The plot and the model on it, so a click on the empty corners of the view falls through.
    private var hit: Path {
        var path = WorldMetrics.diamond(at: Self.anchor, cells: WorldMetrics.pad(station))
        let height = WorldModel.height(station)
        path.addRect(CGRect(x: Self.anchor.x - 34, y: Self.anchor.y + WorldModel.back - height, width: 68, height: height))
        return path.applying(CGAffineTransform(scaleX: scale, y: scale))
    }

    private func draw(_ context: inout GraphicsContext) {
        // The pad itself belongs to the ground. Only its edge is drawn here, when there's something to say.
        if selected || working {
            let pad = WorldMetrics.diamond(at: Self.anchor, cells: WorldMetrics.pad(station) - 0.3)
            context.stroke(pad, with: .color(selected ? Color.primary : tint), lineWidth: selected ? 2.5 : 2)
        }
        WorldModel.draw(station, in: &context, at: Self.anchor, tint: tint, symbol: look.symbol)
        crates(&context)
    }

    /// Up to four of what the station holds, stacked at its left, each in its own kind's colour.
    private func crates(_ context: inout GraphicsContext) {
        guard !station.items.isEmpty else { return }
        var iso = Iso(context: context, origin: Self.anchor)
        let spots: [(Double, Double, Double)] = [(-8, 40, 0), (6, 44, 0), (-10, 26, 0), (-8, 40, 9)]
        let reach = station.small ? 0.6 : 1
        for (item, spot) in zip(station.items.prefix(4), spots) {
            iso.box(i: spot.0 * reach, j: spot.1 * reach, a: 9, b: 9, z: spot.2, h: 9, color: GraphTint.color(item.node))
        }
    }
}

/// A name floating over something on the map.
struct WorldName: View {
    let text: String
    var selected = false

    var body: some View {
        Text(text)
            .font(.caption.weight(.semibold)).lineLimit(1).truncationMode(.tail)
            .padding(.horizontal, 8).frame(height: 20)
            .background(Color.graphCard, in: Capsule())
            .overlay { Capsule().strokeBorder(selected ? Color.primary : Color.primary.opacity(0.14), lineWidth: selected ? 2 : 1) }
    }
}

/// What someone's doing, or what's being passed, in a few words beside the icon for it.
struct WorldTag: View {
    let symbol: String
    let text: String
    let color: Color
    var selected = false

    var body: some View {
        HStack(spacing: 5) {
            Image(systemName: symbol)
                .font(.system(size: 9, weight: .bold)).foregroundStyle(.white)
                .frame(width: 18, height: 18)
                .background(color, in: Circle())
            Text(text).font(.caption.weight(.medium)).lineLimit(1).truncationMode(.middle)
        }
        .padding(.leading, 3).padding(.trailing, 9).frame(height: 24)
        .background(Color.graphCard, in: Capsule())
        .overlay { Capsule().strokeBorder(selected ? Color.primary : color.opacity(0.6), lineWidth: selected ? 2 : 1) }
    }
}

/// A district's banner: its mark and its name, at the corner nothing hides. Clicking it goes there.
struct WorldBanner: View {
    let group: WorldGroup
    let selected: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            HStack(spacing: 6) {
                Image(systemName: group.symbol)
                    .font(.system(size: 11, weight: .bold)).foregroundStyle(.white)
                    .frame(width: 24, height: 24)
                    .background(GraphTint.color(group.family), in: .rect(cornerRadius: 7))
                Text(group.title).font(.subheadline.weight(.semibold)).lineLimit(1).truncationMode(.tail)
            }
            .padding(.leading, 4).padding(.trailing, 10).frame(height: 32)
            .background(Color.graphCard, in: .rect(cornerRadius: 10))
            .overlay { RoundedRectangle(cornerRadius: 10).strokeBorder(selected ? Color.primary : Color.primary.opacity(0.16), lineWidth: selected ? 2 : 1) }
            .frame(maxWidth: 190)
            #if os(iOS)
            .frame(minHeight: 44)
            #endif
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityLabel("\(group.title), district")
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// Someone on the map: who they are, standing on their own base, with what they're doing over their head.
struct WorldActorView: View {
    let actor: WorldActor
    /// How far its tag rises to clear a neighbour's.
    var tagLift = 0.0
    let activity: ActivityStore
    let scale: Double
    let selected: Bool
    let select: () -> Void
    @State private var hovered = false

    private var node: WorkNode { actor.node }
    private var look: WorkLook { GraphLook.look(node) }
    private var size: Double { WorldMetrics.avatar * scale }

    private var label: String {
        ([node.title, Self.tag(actor)?.text ?? node.subtitle, GraphTint.status(node.state)] as [String?]).compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: ", ")
    }

    /// What's over someone's head, or nothing while they rest.
    static func tag(_ actor: WorldActor) -> (symbol: String, text: String, color: Color)? {
        switch actor.node.state {
        case WorkNodeState.working:
            let said = actor.doing?.isEmpty == false ? actor.doing : actor.node.subtitle?.isEmpty == false ? actor.node.subtitle : GraphLook.cargoTitle(actor.cargo)
            let color = GraphTint.cargo(actor.cargo)
            return (actor.cargo.map { GraphLook.cargo($0) } ?? "ellipsis", said ?? "Working", color)
        case WorkNodeState.waiting: return ("questionmark", actor.doing?.isEmpty == false ? actor.doing ?? "" : "Waiting for you", .orange)
        case WorkNodeState.failed: return ("exclamationmark", "Failed", .red)
        default: return nil
        }
    }

    var body: some View {
        figure
            .frame(width: size, height: size)
            .offset(y: hovered ? -3 : 0)
            .background(alignment: .bottom) { base }
            .overlay(alignment: .top) {
                if let tag = Self.tag(actor) {
                    // Stacked by place in line, so two people at one station never write over each other.
                    WorldTag(symbol: tag.symbol, text: tag.text, color: tag.color)
                        .frame(width: 190, height: 24)
                        .offset(y: -29 - tagLift)
                }
            }
            .overlay(alignment: .bottom) {
                WorldName(text: node.title, selected: selected)
                    .frame(width: 150, height: 20)
                    .offset(y: 20 + 9 * scale)
            }
            .contentShape(Rectangle())
            .onTapGesture(perform: select)
            .onHover { hovered = $0 }
            .animation(.easeOut(duration: 0.12), value: hovered)
            .help(node.title)
            .accessibilityElement(children: .ignore)
            .accessibilityLabel(label)
            .accessibilityAddTraits(selected ? [.isButton, .isSelected] : .isButton)
            .accessibilityAction(.default, select)
    }

    /// The disc they stand on. Its colour is how they're doing, readable from across the map.
    private var base: some View {
        Canvas { context, canvas in
            context.scaleBy(x: scale, y: scale)
            var iso = Iso(context: context, origin: CGPoint(x: canvas.width / scale / 2, y: canvas.height / scale - 18))
            iso.shadow(i: 0, j: 0, radius: 14)
            let rim: Color = switch node.state {
            case WorkNodeState.working: .green
            case WorkNodeState.waiting: .orange
            case WorkNodeState.failed: .red
            default: Color(white: 0.72)
            }
            iso.drum(i: 0, j: 0, radius: 17, h: 5, color: rim)
            iso.drum(i: 0, j: 0, radius: 13.5, z: 5, h: 0.01, color: rim.mix(with: .white, by: 0.55))
        }
        .frame(width: 88 * scale, height: 48 * scale)
        .offset(y: 10 * scale)
        .allowsHitTesting(false)
    }

    @ViewBuilder private var figure: some View {
        if let agent = node.agentID, !agent.isEmpty {
            AgentAvatar(agentID: agent, activity: activity, showsMarker: false)
        } else if node.kind == WorkKind.person {
            WorldPawn().padding(2 * scale)
        } else {
            WorldToken(symbol: look.symbol, tint: GraphTint.color(node)).padding(7 * scale)
        }
    }
}

/// You, as the piece that's yours on the board: a pawn with a crown.
struct WorldPawn: View {
    var body: some View {
        Canvas { context, size in
            let w = size.width
            let h = size.height
            let color = Color.blue
            var body = Path()
            body.move(to: CGPoint(x: w * 0.5, y: h * 0.4))
            body.addCurve(to: CGPoint(x: w * 0.8, y: h * 0.98), control1: CGPoint(x: w * 0.74, y: h * 0.46), control2: CGPoint(x: w * 0.82, y: h * 0.8))
            body.addLine(to: CGPoint(x: w * 0.2, y: h * 0.98))
            body.addCurve(to: CGPoint(x: w * 0.5, y: h * 0.4), control1: CGPoint(x: w * 0.18, y: h * 0.8), control2: CGPoint(x: w * 0.26, y: h * 0.46))
            let shade = Gradient(colors: [color.mix(with: .white, by: 0.3), color, color.mix(with: .black, by: 0.2)])
            context.fill(body, with: .linearGradient(shade, startPoint: CGPoint(x: w * 0.2, y: 0), endPoint: CGPoint(x: w * 0.8, y: 0)))
            let head = CGRect(x: w * 0.3, y: h * 0.16, width: w * 0.4, height: w * 0.4)
            context.fill(Path(ellipseIn: head), with: .radialGradient(Gradient(colors: [color.mix(with: .white, by: 0.45), color]),
                                                                    center: CGPoint(x: head.minX + head.width * 0.35, y: head.minY + head.height * 0.3),
                                                                    startRadius: 1, endRadius: head.width * 0.7))
            let crown = Text(Image(systemName: "crown.fill")).font(.system(size: w * 0.3, weight: .bold)).foregroundStyle(Color.yellow)
            context.draw(context.resolve(crown), at: CGPoint(x: w * 0.5, y: h * 0.1))
        }
        .accessibilityHidden(true)
    }
}

/// An outside agent, which has no face of its own: its engine's mark on a block with some weight to it.
struct WorldToken: View {
    let symbol: String
    let tint: Color

    /// An outside agent's own mark, when the app ships one for it.
    /// Claude Code's mascot is a crab, which says what it is faster than a mark.
    private var crab: Bool { symbol == "acp:claude-acp" }

    private var hasLogo: Bool {
        #if canImport(UIKit)
        UIImage(named: "agent-" + symbol.dropFirst(4)) != nil
        #elseif canImport(AppKit)
        NSImage(named: "agent-" + symbol.dropFirst(4)) != nil
        #else
        false
        #endif
    }

    private var logo: Image? {
        guard symbol.hasPrefix("acp:") else { return nil }
        #if canImport(UIKit)
        guard UIImage(named: "agent-" + symbol.dropFirst(4)) != nil else { return nil }
        #elseif canImport(AppKit)
        guard NSImage(named: "agent-" + symbol.dropFirst(4)) != nil else { return nil }
        #endif
        return Image("agent-" + symbol.dropFirst(4))
    }

    var body: some View {
        GeometryReader { geometry in
            let side = min(geometry.size.width, geometry.size.height)
            let shape = RoundedRectangle(cornerRadius: side * 0.28, style: .continuous)
            ZStack {
                shape.fill(tint.mix(with: .black, by: 0.3)).offset(x: side * 0.05, y: side * 0.08)
                if crab {
                    // Claude Code's mascot is a blocky pixel crab: drawn from its grid, so it stays crisp at any size.
                    Canvas { context, size in
                        let grid = ["..########..", ".##########.", ".##########.", "############", "############",
                                    "..########..", "..#.##.##.#.", "..#.##.##.#."]
                        let unit = min(size.width, size.height) / 12.5
                        let origin = CGPoint(x: (size.width - unit * 12) / 2, y: (size.height - unit * 8) / 2)
                        let shell = Color(red: 0.78, green: 0.37, blue: 0.23)
                        for (row, line) in grid.enumerated() {
                            for (column, cell) in line.enumerated() where cell == "#" {
                                context.fill(Path(CGRect(x: origin.x + Double(column) * unit, y: origin.y + Double(row) * unit,
                                                         width: unit + 0.5, height: unit + 0.5)), with: .color(shell))
                            }
                        }
                        for column in [3, 7] {
                            context.fill(Path(CGRect(x: origin.x + Double(column) * unit, y: origin.y + unit * 1.5,
                                                     width: unit, height: unit * 1.4)), with: .color(Color(red: 0.16, green: 0.1, blue: 0.09)))
                        }
                    }
                    .padding(side * 0.12)
                    .background(Color(red: 0.98, green: 0.94, blue: 0.88), in: shape)
                } else if let logo {
                    logo.resizable().interpolation(.high).scaledToFit()
                        .padding(side * 0.06)
                        .background(.white, in: shape)
                } else {
                    shape.fill(LinearGradient(colors: [tint.mix(with: .white, by: 0.3), tint], startPoint: .top, endPoint: .bottom))
                    Image(systemName: symbol).font(.system(size: side * 0.48, weight: .bold)).foregroundStyle(.white)
                }
            }
            .frame(width: side, height: side)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
        }
        .accessibilityHidden(true)
    }
}

/// The ground: sea, an islet for home and one per district on its own kind of land, planks and roads between them.
struct WorldGround: View {
    let world: WorkWorld
    let terraces: WorldTerraces
    /// Turns a point on the map into a point on screen.
    let place: (CGPoint) -> CGPoint
    let scale: Double

    /// How far a ground point stands above the sand, on screen.
    private func lift(_ i: Double, _ j: Double) -> Double { terraces.lift(i, j, scale: scale) }

    var body: some View {
        Canvas { context, size in draw(&context, size: size) }
            .allowsHitTesting(false)
            .accessibilityHidden(true)
    }

    /// A stable number from 0 to 1 for a cell, so the same cell always grows the same thing.
    private func chance(_ cell: WorldCell) -> Double {
        let value = sin(Double(cell.i) * 127.1 + Double(cell.j) * 311.7) * 43758.5453
        return value - value.rounded(.down)
    }

    private func tile(_ cell: WorldCell, grown: Double = 1) -> (centre: CGPoint, top: Path) {
        let half = WorldMetrics.half * scale * grown
        let centre = place(WorldMetrics.screen(Double(cell.i) + 0.5, Double(cell.j) + 0.5))
        var top = Path()
        top.move(to: CGPoint(x: centre.x, y: centre.y - half / 2))
        top.addLine(to: CGPoint(x: centre.x + half, y: centre.y))
        top.addLine(to: CGPoint(x: centre.x, y: centre.y + half / 2))
        top.addLine(to: CGPoint(x: centre.x - half, y: centre.y))
        top.closeSubpath()
        return (centre, top)
    }

    /// A tile's top face, stood on its terrace.
    private func topShape(_ cell: WorldCell, level: Double) -> Path {
        let (centre, top) = tile(cell)
        return top.offsetBy(dx: 0, dy: -level)
    }

    /// The far corner of a tile's top face, stood on its terrace.
    private func farCorner(_ cell: WorldCell, level: Double) -> CGPoint {
        let (centre, _) = tile(cell)
        return CGPoint(x: centre.x, y: centre.y - half2 / 2 - level)
    }

    private var half2: Double { WorldMetrics.half * scale }

    /// A band of the wall under one edge of a tile, from `from` to `to` below the surface.
    private func wall(_ a: CGPoint, _ b: CGPoint, from: Double, to: Double) -> Path {
        var path = Path()
        path.move(to: CGPoint(x: a.x, y: a.y + from))
        path.addLine(to: CGPoint(x: b.x, y: b.y + from))
        path.addLine(to: CGPoint(x: b.x, y: b.y + to))
        path.addLine(to: CGPoint(x: a.x, y: a.y + to))
        path.closeSubpath()
        return path
    }

    private func draw(_ context: inout GraphicsContext, size: CGSize) {
        // Open water: short wave strokes in rows, which is what makes the sea read as water rather than a colour.
        for row in 0..<Int(size.height / 46) + 2 {
            let y = Double(row) * 46 + 12
            for column in -1..<Int(size.width / 74) + 2 {
                let x = Double(column) * 74 + (row % 2 == 0 ? 0 : 37)
                var wave = Path()
                wave.move(to: CGPoint(x: x, y: y))
                wave.addQuadCurve(to: CGPoint(x: x + 20, y: y), control: CGPoint(x: x + 10, y: y - 5))
                context.stroke(wave, with: .color(.white.opacity(0.34)), style: StrokeStyle(lineWidth: 2, lineCap: .round))
            }
        }
        let land = terraces.land
        let paved = terraces.paved
        let all = Set(land.keys).union(terraces.ocean)
        let biomes = Dictionary(world.groups.map { ($0.id, $0.biome) }, uniquingKeysWith: { first, _ in first })
        func group(of cell: WorldCell) -> WorldGroup? { world.groups.first { $0.biome == land[cell] } }
        let half = WorldMetrics.half * scale
        let step = WorldTerraces.terrace * scale
        let depth = 22 * scale
        func rise(_ cell: WorldCell) -> Int { terraces.rise[cell] ?? 0 }
        func shoulder(_ cell: WorldCell, _ di: Int, _ dj: Int) -> Double {
            let at = WorldCell(i: cell.i + di, j: cell.j + dj)
            return land[at] == nil ? -depth : Double(rise(at)) * step
        }
        // Back to front, so a wall never paints over the tile in front of it.
        let order = all.sorted { ($0.i + $0.j, $0.i) < ($1.i + $1.j, $1.i) }

        // The shelf each islet sits on, seen through the water around it.
        // The shelf is the land itself sunk to the sea floor; it must stay under the tallest cliff,
        // so it sinks with the highest terrace rather than with each tile's own ledge.
        var shelf = Path()
        for cell in land.keys { shelf.addPath(tile(cell, grown: 2.3).top) }
        context.fill(shelf.offsetBy(dx: 0, dy: depth + Double(max(0, terraces.maxRise - 1)) * step), with: .color(.worldShallows))

        for cell in order {
            let (centre, plankTop) = tile(cell)
            let left = CGPoint(x: centre.x - half, y: centre.y)
            let right = CGPoint(x: centre.x + half, y: centre.y)
            let bottom = CGPoint(x: centre.x, y: centre.y + half / 2)
            guard let biome = land[cell] else {
                if !all.contains(WorldCell(i: cell.i, j: cell.j + 1)) { context.fill(wall(left, bottom, from: 0, to: 5 * scale), with: .color(Color.worldWood.mix(with: .black, by: 0.12))) }
                if !all.contains(WorldCell(i: cell.i + 1, j: cell.j)) { context.fill(wall(bottom, right, from: 0, to: 5 * scale), with: .color(Color.worldWood.mix(with: .black, by: 0.26))) }
                context.fill(plankTop, with: .color(.worldWood))
                context.stroke(plankTop, with: .color(Color.worldWood.mix(with: .black, by: 0.2)), lineWidth: 1)
                continue
            }
            let terrain = WorldTerrain.of(biome)
            let level = Double(rise(cell)) * step
            let atSand = rise(cell) == 0
            let coastal = [(1, 0), (-1, 0), (0, 1), (0, -1)].contains { land[WorldCell(i: cell.i + $0.0, j: cell.j + $0.1)] == nil }
            // Two walls, lower-left and lower-right, from this tile's ledge down to its neighbour's or to the sea.
            for (a, b, di, dj, light) in [(left, bottom, 0, 1, 0.16), (bottom, right, 1, 0, 0.0)] {
                let foot = shoulder(cell, di, dj)
                let span = level - foot
                guard span > 0.5 else { continue }
                let beach = foot > -depth / 2
                let base = (beach ? terrain.sand.mix(with: .brown, by: 0.28) : terrain.cliff).mix(with: .white, by: light)
                // A cliff in strata, so a terrace reads as piled ground rather than a paper edge.
                let bands = span > step * 1.4 ? 3 : 2
                for band in 0..<bands {
                    let from = level - span * Double(band) / Double(bands)
                    context.fill(wall(a, b, from: from - span / Double(bands), to: from),
                                 with: .color(base.mix(with: .black, by: Double(band) * 0.10)))
                }
                context.fill(wall(a, b, from: level - 3.5 * scale, to: level), with: .color(terrain.top.mix(with: .black, by: light > 0 ? 0.06 : 0.14)))
                if foot < -depth / 2 {
                    var surf = Path()
                    surf.move(to: CGPoint(x: a.x, y: a.y + foot))
                    surf.addLine(to: CGPoint(x: b.x, y: b.y + foot))
                    context.stroke(surf, with: .color(.white.opacity(0.85)), style: StrokeStyle(lineWidth: 2.5, lineCap: .round, dash: [1, 5]))
                }
            }
            // Sand where the water reaches, grass on every terrace above it; paving lies inside the sand, never over the coast.
            let face = atSand && !paved.contains(cell) ? terrain.sand : terrain.top
            context.fill(topShape(cell, level: level), with: .color((cell.i + cell.j) % 2 == 0 ? face : face.mix(with: .black, by: 0.035)))
            // The district's own line along its shore, the way a border is drawn on a tiled map.
            let fence: Color? = biome == .plaza ? Color(red: 0.55, green: 0.45, blue: 0.33).mix(with: .graphCard, by: 0.2) : group(of: cell).map { GraphTint.color($0.family) }
            if coastal, let tint = fence {
                var edge = Path()
                for (a, b, di, dj) in [(left, bottom, 0, 1), (bottom, right, 1, 0), (right, farCorner(cell, level: level), 0, -1), (farCorner(cell, level: level), left, -1, 0)] {
                    if land[WorldCell(i: cell.i + di, j: cell.j + dj)] != nil { continue }
                    edge.move(to: CGPoint(x: a.x, y: a.y - level))
                    edge.addLine(to: CGPoint(x: b.x, y: b.y - level))
                }
                context.stroke(edge, with: .color(tint.mix(with: .black, by: 0.15)), style: StrokeStyle(lineWidth: 2.6 * scale, lineCap: .round))
            }
        }

        roads(&context)
        pad(&context, at: .zero, lift: lift(0, 0), cells: WorldMetrics.home, color: WorldTerrain.of(.plaza).pad)
        stamp(&context, at: .zero, lift: lift(0, 0)) { layer in WorldModel.house(in: &layer, at: .zero) }
        for station in world.stations {
            let cell = WorldMetrics.cell(station)
            pad(&context, at: WorldMetrics.screen(cell.i, cell.j), lift: lift(cell.i, cell.j), cells: WorldMetrics.pad(station), color: WorldTerrain.of(biomes[station.group] ?? .meadow).pad)
        }
        // What grows keeps to the shore, where it marks the land without standing between anything.
        let flags = Set(world.groups.map { WorldCell(i: Int($0.flagI.rounded(.down)), j: Int($0.flagJ.rounded(.down))) })
        for cell in order where !paved.contains(cell) && !flags.contains(cell) && chance(cell) > 0.42 {
            guard let biome = land[cell], biome != .plaza, rise(cell) == 1 else { continue }
            let roll = chance(WorldCell(i: cell.j, j: cell.i))
            // Two or three together, the way Polytopia clusters its trees, so a shore reads as a grove.
            let crowd = chance(WorldCell(i: cell.j * 3, j: cell.i * 7))
            for index in 0...Int(crowd * 2.9) {
                let spread = CGFloat(index) * 7 - 7
                let at = WorldMetrics.screen(Double(cell.i) + 0.5, Double(cell.j) + 0.5)
                stamp(&context, at: CGPoint(x: at.x + spread, y: at.y + spread * 0.4), lift: lift(Double(cell.i), Double(cell.j))) { layer in
                    var iso = Iso(context: layer, origin: .zero)
                    Self.grow(biome, &iso, roll: roll - Double(index) * 0.17)
                }
            }
        }
        // Each banner's pole. The banner itself is a button, drawn over the map.
        for group in world.groups {
            stamp(&context, at: WorldMetrics.screen(group.flagI, group.flagJ), lift: lift(group.flagI, group.flagJ)) { layer in
                var iso = Iso(context: layer, origin: .zero)
                iso.box(i: 0, j: 0, a: 2.5, b: 2.5, h: 34, color: .brown)
            }
        }
    }

    /// Draws something at a place on the map, at the map's size, stood on its terrace.
    private func stamp(_ context: inout GraphicsContext, at centre: CGPoint, lift: Double = 0, _ draw: (inout GraphicsContext) -> Void) {
        var layer = context
        let at = place(centre)
        layer.translateBy(x: at.x, y: at.y - lift)
        layer.scaleBy(x: scale, y: scale)
        draw(&layer)
    }

    /// The paving a station stands on: a slab a little proud of the ground around it.
    private func pad(_ context: inout GraphicsContext, at centre: CGPoint, lift: Double = 0, cells: Double, color: Color) {
        let at = CGPoint(x: place(centre).x, y: place(centre).y - lift)
        let shape = WorldMetrics.diamond(at: .zero, cells: cells - 0.3).applying(CGAffineTransform(scaleX: scale, y: scale)).offsetBy(dx: at.x, dy: at.y)
        context.fill(shape.offsetBy(dx: 0, dy: 3 * scale), with: .color(color.mix(with: .black, by: 0.22)))
        context.fill(shape, with: .color(color))
    }

    /// A ground point on screen, stood on its terrace.
    private func lifted(_ i: Double, _ j: Double) -> CGPoint {
        let at = place(WorldMetrics.screen(i, j))
        return CGPoint(x: at.x, y: at.y - lift(i, j))
    }

    /// The road through a pipeline, from each step to the next: green where the work has been, pale where it hasn't yet.
    private func roads(_ context: inout GraphicsContext) {
        let ordered = Set(world.groups.filter(\.ordered).map(\.id))
        for trail in world.trails where trail.kind == .road {
            guard let from = world.station(trail.from), let to = world.station(trail.to), from.group == to.group, ordered.contains(from.group) else { continue }
            let a = lifted(Double(from.i), Double(from.j))
            let b = lifted(Double(to.i), Double(to.j))
            var road = Path()
            road.move(to: a)
            road.addLine(to: b)
            let walked = from.state == WorkNodeState.done
            context.stroke(road, with: .color(Color.graphCard), style: StrokeStyle(lineWidth: 24 * scale, lineCap: .round))
            context.stroke(road, with: .color(walked ? Color.green : Color.primary.opacity(0.3)),
                           style: StrokeStyle(lineWidth: 6 * scale, lineCap: .round, dash: walked ? [] : [1, 12 * scale]))
        }
    }

    /// What grows on each kind of land, which is most of what tells one district from another at a distance.
    private static func grow(_ biome: WorldBiome, _ iso: inout Iso, roll: Double) {
        iso.shadow(i: 0, j: 0, radius: 6)
        switch biome {
        case .meadow:
            let leaf = Color.green.mix(with: .black, by: 0.12)
            iso.box(i: 0, j: 0, a: 3, b: 3, h: 6, color: .brown)
            iso.box(i: 0, j: 0, a: 13, b: 13, z: 6, h: roll > 0.5 ? 12 : 9, color: leaf)
            if roll > 0.5 { iso.box(i: 0, j: 0, a: 7, b: 7, z: 18, h: 7, color: leaf.mix(with: .white, by: 0.14)) }
        case .blossom:
            iso.box(i: 0, j: 0, a: 3, b: 3, h: 9, color: Color.brown.mix(with: .black, by: 0.3))
            iso.box(i: 0, j: 0, a: 15, b: 15, z: 9, h: 8, color: .pink)
            iso.box(i: 0, j: 0, a: 9, b: 9, z: 17, h: 6, color: Color.pink.mix(with: .white, by: 0.3))
        case .desert:
            if roll > 0.45 {
                let cactus = Color.green.mix(with: .black, by: 0.2)
                iso.box(i: 0, j: 0, a: 5, b: 5, h: 20, color: cactus)
                iso.box(i: 0, j: 6, a: 4, b: 4, z: 8, h: 8, color: cactus)
                iso.box(i: 0, j: 4, a: 4, b: 6, z: 7, h: 3, color: cactus)
            } else {
                iso.pyramid(i: 0, j: 0, a: 18, h: 9, color: Color.orange.mix(with: .brown, by: 0.5))
            }
        case .crystal:
            iso.pyramid(i: 0, j: 2, a: 9, h: 24, color: .purple)
            iso.pyramid(i: 4, j: -4, a: 7, h: 15, color: Color.purple.mix(with: .pink, by: 0.4))
            if roll > 0.5 { iso.pyramid(i: -5, j: -2, a: 6, h: 11, color: .indigo) }
        case .ice:
            iso.pyramid(i: 0, j: 0, a: 16, h: 16, color: Color.cyan.mix(with: .white, by: 0.55))
            if roll > 0.5 { iso.pyramid(i: 6, j: -5, a: 9, h: 9, color: Color.white) }
        case .slate:
            iso.pyramid(i: 0, j: 0, a: 20, h: roll > 0.5 ? 20 : 13, color: Color(white: 0.5))
            if roll > 0.5 { iso.pyramid(i: 0, j: 0, a: 8, z: 12, h: 8, color: Color(white: 0.95)) }
        case .savanna:
            iso.box(i: 0, j: 0, a: 3, b: 3, h: 13, color: Color.brown.mix(with: .black, by: 0.2))
            iso.box(i: 0, j: 0, a: 20, b: 20, z: 13, h: 4, color: Color.green.mix(with: .yellow, by: 0.4).mix(with: .black, by: 0.12))
        case .plaza:
            break
        }
    }
}

/// What's in the air: what's passing between actors, and the lines from whoever's picked to what they use.
/// Their tags are buttons, drawn over this.
struct WorldTrails: View {
    let world: WorkWorld
    let terraces: WorldTerraces
    /// Where each actor's head and each station's top are on screen.
    let points: [String: CGPoint]
    /// What's picked. Its own trails light up; the rest step back.
    let picked: String?
    let handoffs: [ActivityStore.Handoff]
    /// Turns a point on the map into a point on screen, so a route drawn here lands where it's meant to.
    let place: (CGPoint) -> CGPoint
    /// How large the map is drawn, so terraces lift routes by the same amount the ground does.
    var cameraScale: Double = 1
    @Environment(\.accessibilityReduceMotion) private var reducedMotion
    @Environment(\.scenePhase) private var scenePhase

    private static let trip = 4.2
    private static let flight = 1.2

    private var moving: Bool {
        // Any trail at all keeps the layer drawing: a paused timeline never even paints its first frame.
        !reducedMotion && scenePhase == .active && (!handoffs.isEmpty || world.trails.contains { $0.kind != .road })
    }

    var body: some View {
        TimelineView(.animation(minimumInterval: 1 / 30, paused: !moving)) { tick in
            Canvas { context, _ in draw(&context, now: tick.date) }
        }
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }

    /// Whether a trail shows now: work in flight does by itself, and the rest waits until one of its ends is picked.
    static func shows(_ trail: WorldTrail, in world: WorkWorld, picked: String?) -> Bool {
        let lit = picked.map { trail.from == $0 || trail.to == $0 || "trail:" + trail.id == $0 } ?? false
        if lit { return true }
        guard trail.active else { return false }
        switch trail.kind {
        case .handoff: return world.actor(trail.from)?.node.kind != WorkKind.person
        // Reaching for a tool from somewhere else shows once it's picked, so the water stays calm.
        case .visit: return world.actor(trail.from)?.station != trail.to && lit
        case .road: return false
        }
    }

    private func draw(_ context: inout GraphicsContext, now: Date) {
        let started = CFAbsoluteTimeGetCurrent()
        defer { AtlasFrameCost.shared.record(CFAbsoluteTimeGetCurrent() - started) }
        let seconds = now.timeIntervalSinceReferenceDate
        #if DEBUG
        if CommandLine.arguments.contains("--verify-graph-trace") {
            AtlasTrace.everyHalfSecond {
                for trail in world.trails where trail.kind == .handoff {
                    print("atlas handoff \(trail.id) active=\(trail.active) shows=\(WorldTrails.shows(trail, in: world, picked: picked)) from=\(world.group(of: trail.from)?.id ?? "-") to=\(world.group(of: trail.to)?.id ?? "-")")
                }
            }
        }
        #endif
        var drawn = Set<String>()
        for (index, trail) in world.trails.enumerated() where Self.shows(trail, in: world, picked: picked) {
            guard let from = points[trail.from], let to = points[trail.to] else { continue }
            // One boat per pair and direction, so two kinds of traffic never sit on top of each other.
            let key = trail.wire.direction == WorkWireDirection.back ? trail.to + ">" + trail.from : trail.from + ">" + trail.to
            guard drawn.insert(key).inserted else { continue }
            let lit = picked.map { trail.from == $0 || trail.to == $0 || "trail:" + trail.id == $0 } ?? false
            var layer = context
            layer.opacity = picked != nil && !lit ? 0.16 : 1
            let flying = trail.active && trail.kind != .road
            let color = flying ? GraphTint.cargo(trail.wire.carries) : Color.primary.opacity(0.4)
            let crossing = crossing(trail, from: from, to: to, lane: index)
            if let crossing {
                // A lane of disturbed water, from one shore to the other.
                layer.stroke(crossing.path, with: .color(.white.opacity(0.18)), style: StrokeStyle(lineWidth: 2, lineCap: .round, dash: [1, 12]))
                guard flying else { continue }
                let phase = reducedMotion ? 0.6 : (seconds / Self.trip + Double(index) * 0.37).truncatingRemainder(dividingBy: 1)
                let at = point(crossing.path, at: trail.wire.direction == WorkWireDirection.back ? 1 - phase : phase)
                if CommandLine.arguments.contains("--verify-graph-trace") {
                    print("atlas boat \(trail.id) t=\(String(format: "%.2f", seconds)) x=\(Int(at.x)) y=\(Int(at.y))")
                }
                let bob = reducedMotion ? 0 : sin(seconds * 2.2 + Double(index)) * 1.6
                boat(&layer, at: CGPoint(x: at.x, y: at.y + bob), heading: crossing.heading, color: color, zoom: crossing.zoom)
                // A short wake behind it, rather than a line all the way across.
                let tail = point(crossing.path, at: max(0, (trail.wire.direction == WorkWireDirection.back ? 1 - phase : phase) - 0.06))
                var wake = Path()
                wake.move(to: tail)
                wake.addLine(to: at)
                layer.stroke(wake, with: .color(.white.opacity(0.8)), style: StrokeStyle(lineWidth: 3, lineCap: .round, dash: [2, 4]))
            } else {
                // Whatever else, nothing runs in a straight line: a curve keeps the map from reading as wire.
                let path = WorldMetrics.arc(from, to).path
                layer.stroke(path, with: .color(.black.opacity(0.12)), style: StrokeStyle(lineWidth: 9, lineCap: .round))
                layer.stroke(path, with: .color(color), style: StrokeStyle(lineWidth: 2, lineCap: .round, dash: [4, 5]))
                guard flying else { continue }
                // On land the same cargo travels by cart, so work on one island still reads as moving.
                // Between the two pads themselves, so a cart crossing the island travels the island, not the gap between heads.
                let forward = trail.wire.direction != WorkWireDirection.back
                let start = pad(trail.from, fallback: from, forward: forward)
                let end = pad(trail.to, fallback: to, forward: forward)
                let lane = CGFloat((index % 3) - 1) * 20
                let control = CGPoint(x: (start.x + end.x) / 2, y: max(start.y, end.y) + max(46, hypot(end.x - start.x, end.y - start.y) * 0.22) + lane)
                let at = WorldMotion.position(WorldRoute(from: start, control: control, to: end), seconds: seconds, index: index, still: reducedMotion)
                if CommandLine.arguments.contains("--verify-graph-trace") {
                    print("atlas cart \(trail.id) t=\(String(format: "%.2f", seconds)) x=\(Int(at.x)) y=\(Int(at.y))")
                }
                cart(&layer, at: at, colour: color)
            }
        }
        for handoff in handoffs {
            guard let from = points[WorkGraphBuilder.agentNode(handoff.source)], let to = points[WorkGraphBuilder.agentNode(handoff.target)] else { continue }
            let progress = now.timeIntervalSince(handoff.at) / Self.flight
            guard (0...1).contains(progress) else { continue }
            let kind = handoff.result ? WorkKind.result : WorkKind.task
            let path = WorldMetrics.arc(from, to).path
            let at = point(path, at: progress)
            // Something just handed over sails across rather than flying.
            let heading = atan2(to.y - from.y, to.x - from.x)
            let bob = reducedMotion ? 0 : sin(now.timeIntervalSinceReferenceDate * 2.2) * 1.6
            boat(&context, at: CGPoint(x: at.x, y: at.y + bob), heading: heading, color: GraphTint.cargo(kind), zoom: 1.4)
        }
    }

    /// A boat's course between two islets, with the heading it holds.
    private struct Crossing {
        var path: Path
        var heading: Double
        var zoom: CGFloat
    }

    /// The water between the two things' islets, or nil when they share one.
    private func crossing(_ trail: WorldTrail, from: CGPoint, to: CGPoint, lane: Int = 0) -> Crossing? {
        // Home is an islet too, so someone resting there still sends a boat rather than a bare line.
        let start = islet(world.group(of: trail.from))
        let end = islet(world.group(of: trail.to))
        guard start.id != end.id else { return nil }
        // Each crossing keeps its own lane, so two boats never sail on top of each other.
        let drift = CGFloat((lane % 3) - 1) * 16
        let a = shore(start).map(place).map { CGPoint(x: $0.x, y: $0.y + drift) }
        let b = shore(end).map(place).map { CGPoint(x: $0.x, y: $0.y + drift) }
        // The two shores closest to each other, so a boat takes the short way across.
        var best: (CGFloat, CGPoint, CGPoint) = (.infinity, a[0], b[0])
        for one in a { for other in b {
            let away = hypot(one.x - other.x, one.y - other.y)
            if away < best.0 { best = (away, one, other) }
        } }
        // A gentle bow between the two shores, never a sweep out to open sea.
        let span = max(1, hypot(best.2.x - best.1.x, best.2.y - best.1.y))
        let bow = min(36, span * 0.18)
        let control = CGPoint(x: (best.1.x + best.2.x) / 2 - (best.2.y - best.1.y) / span * bow,
                              y: (best.1.y + best.2.y) / 2 + (best.2.x - best.1.x) / span * bow)
        var path = Path()
        path.move(to: best.1)
        path.addQuadCurve(to: best.2, control: control)
        return Crossing(path: path, heading: atan2(best.2.y - best.1.y, best.2.x - best.1.x), zoom: 1.3)
    }

    private struct Islet {
        var id: String
        var minI: Int
        var minJ: Int
        var maxI: Int
        var maxJ: Int
        /// How far outside its plots the water reaches: none at home, where the paving is the shore.
        var margin: Int = 4
    }

    /// The islet a group covers, or home for someone with none.
    private func islet(_ group: WorldGroup?) -> Islet {
        guard let group else { return Islet(id: "home", minI: -2, minJ: -2, maxI: 2, maxJ: 2, margin: 2) }
        return Islet(id: group.id, minI: group.minI, minJ: group.minJ, maxI: group.maxI, maxJ: group.maxJ)
    }

    /// Points around an islet's edge, in map coordinates, where a boat can wait.
    private func shore(_ group: Islet) -> [CGPoint] {
        let reach = Double(group.margin)
        let near = WorldMetrics.screen(Double(group.minI) - reach, Double(group.minJ) - reach)
        let far = WorldMetrics.screen(Double(group.maxI) + reach, Double(group.maxJ) + reach)
        let across = WorldMetrics.screen(Double(group.minI) - reach, Double(group.maxJ) + reach)
        let other = WorldMetrics.screen(Double(group.maxI) + reach, Double(group.minJ) - reach)
        let left = min(near.x, far.x, across.x, other.x)
        let right = max(near.x, far.x, across.x, other.x)
        let top = min(near.y, far.y, across.y, other.y)
        let bottom = max(near.y, far.y, across.y, other.y)
        let middle = CGPoint(x: (left + right) / 2, y: (top + bottom) / 2)
        return [CGPoint(x: middle.x, y: top), CGPoint(x: right, y: middle.y), CGPoint(x: middle.x, y: bottom), CGPoint(x: left, y: middle.y)]
    }

    /// Where someone stands on their own pad, or where their head is when they have none.
    private func pad(_ id: String, fallback: CGPoint, forward: Bool) -> CGPoint {
        guard let actor = world.actor(id), let station = world.station(actor.station) else { return fallback }
        let cell = WorldMetrics.cell(station)
        let foot = place(WorldMetrics.screen(cell.i, cell.j))
        // Heads already stand on the terrace; a cart's start is lifted the same way, in camera space.
        let height = Double(terraces.rise[WorldCell(i: Int(cell.i.rounded(.down)), j: Int(cell.j.rounded(.down)))] ?? 0) * WorldTerraces.terrace
        return CGPoint(x: foot.x, y: foot.y - 14 - height * cameraScale)
    }

    /// What's being carried, as a small isometric crate: the same object on water and on land.
    private func parcel(_ context: inout GraphicsContext, at point: CGPoint, colour: Color) {
        var iso = Iso(context: context, origin: CGPoint(x: point.x, y: point.y + 5))
        iso.shadow(i: 0, j: 0, radius: 5)
        iso.box(i: 0, j: 0, a: 11, b: 11, z: 2, h: 9, color: colour)
    }

    /// A two-wheeled cart on the ground, drawn in the same isometric light as the buildings.
    private func cart(_ context: inout GraphicsContext, at point: CGPoint, colour: Color) {
        var iso = Iso(context: context, origin: point)
        // Its shadow, then the body with a lid, then the wheels: the same three cues the buildings use.
        iso.shadow(i: 0, j: 0, radius: 9)
        let wood = Color(red: 0.52, green: 0.33, blue: 0.2)
        iso.box(i: 0, j: 0, a: 14, b: 20, z: 4, h: 11, color: colour.mix(with: wood, by: 0.35))
        iso.box(i: 0, j: 0, a: 16, b: 22, z: 15, h: 3, color: colour.mix(with: .white, by: 0.2))
        for side in [-6.0, 6.0] {
            let wheel = iso.point(side, side, 3)
            context.fill(Path(ellipseIn: CGRect(x: wheel.x - 4, y: wheel.y - 1.5, width: 8, height: 7)), with: .color(Color(white: 0.24)))
        }
    }

    /// A small boat: a hull, a sail in the colour of what it carries, and a curl of wake behind it.
    private func boat(_ context: inout GraphicsContext, at point: CGPoint, heading: Double, color: Color, zoom: CGFloat) {
        var layer = context
        layer.translateBy(x: point.x, y: point.y)
        layer.rotate(by: .radians(heading))
        let unit = 15 * zoom
        var hull = Path()
        hull.move(to: CGPoint(x: -unit, y: 0))
        hull.addLine(to: CGPoint(x: unit, y: 0))
        hull.addLine(to: CGPoint(x: unit * 0.58, y: unit * 0.4))
        hull.addLine(to: CGPoint(x: -unit * 0.58, y: unit * 0.4))
        hull.closeSubpath()
        layer.fill(hull, with: .color(color.mix(with: Color(red: 0.4, green: 0.25, blue: 0.15), by: 0.55)))
        // One mast, one sail in the colour of what it carries, and a lighter seam.
        var mast = Path()
        mast.move(to: CGPoint(x: -unit * 0.05, y: 0))
        mast.addLine(to: CGPoint(x: -unit * 0.05, y: -unit * 1.05))
        layer.stroke(mast, with: .color(Color(white: 0.35)), lineWidth: 1.2)
        var sail = Path()
        sail.move(to: CGPoint(x: -unit * 0.05, y: -unit * 1.02))
        sail.addLine(to: CGPoint(x: unit * 0.72, y: -unit * 0.12))
        sail.addLine(to: CGPoint(x: -unit * 0.05, y: -unit * 0.12))
        sail.closeSubpath()
        layer.fill(sail, with: .color(.white))
        layer.stroke(sail, with: .color(color), lineWidth: 2)
    }

    private func point(_ path: Path, at progress: Double) -> CGPoint {
        path.trimmedPath(from: 0, to: min(1, max(0.001, progress))).currentPoint ?? .zero
    }
}






#if DEBUG
@MainActor enum AtlasTrace {
    private static var said: Set<String> = []
    static var last = Date.distantPast
    static func everyHalfSecond(_ body: () -> Void) {
        let now = Date()
        guard now.timeIntervalSince(last) > 0.5 else { return }
        last = now
        body()
    }
}
#endif


/// Frames the moving layer draws, and what they cost, so smoothness can be measured rather than guessed.
final class AtlasFrameCost: @unchecked Sendable {
    static let shared = AtlasFrameCost()
    private var frames = 0
    private var total = 0.0
    private var worst = 0.0
    private let lock = NSLock()

    func record(_ seconds: Double) {
        lock.lock()
        defer { lock.unlock() }
        frames += 1
        total += seconds
        worst = max(worst, seconds)
        guard frames % 120 == 0, CommandLine.arguments.contains("--verify-graph-trace") else { return }
        print(String(format: "atlas frames %d avg=%.2fms worst=%.2fms", frames, total / Double(frames) * 1000, worst * 1000))
    }
}
