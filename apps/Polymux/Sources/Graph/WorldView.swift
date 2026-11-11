import SwiftUI
import PolymuxKit

/// The work as a map. Stations stay where they are, and whoever's working stands at the one they're using.
struct WorldView: View {
    let graph: WorkGraph
    let activity: ActivityStore
    let loaded: Bool
    let previews: WorldPreviews
    var openChat: ((String) -> Void)?
    /// Switches to the diagram of the same work.
    let showDiagram: () -> Void
    @State private var world = WorkWorld()
    @State private var picked: String?
    @State private var zoom = 1.0
    @State private var shift = CGSize.zero
    @State private var dragged = CGSize.zero
    @State private var pinch = 1.0
    @State private var viewport = CGSize.zero
    /// On a phone, details open as a line at the foot of the map and grow from there.
    @State private var expanded = false
    /// A narrow map has opened on where the work is, and stays wherever it's taken after that.
    @State private var framed = false
    @State private var flight: Task<Void, Never>?
    /// Names of places are a detail: hovering or picking shows one, and this shows them all.
    @AppStorage("graphNames") private var named = false
    @Environment(\.accessibilityReduceMotion) private var reducedMotion
    @Environment(\.colorScheme) private var scheme
    #if os(macOS)
    @State private var events = GraphEvents()
    #endif

    private static let zooms = 0.4...3.0
    private static let detailsWidth: CGFloat = 320

    private var showsDetails: Bool { picked.flatMap { WorldSubject.of($0, in: world) } != nil }

    var body: some View {
        GeometryReader { geometry in
            let narrow = geometry.size.width < 640
            let tall = narrow || geometry.size.height > geometry.size.width * 1.15
            let room = CGSize(width: geometry.size.width - (showsDetails && !narrow ? Self.detailsWidth + 1 : 0), height: geometry.size.height)
            HStack(spacing: 0) {
                map(size: room, narrow: narrow)
                if showsDetails, !narrow {
                    Divider()
                    details.frame(width: Self.detailsWidth).transition(.move(edge: .trailing))
                }
            }
            .overlay(alignment: .bottom) {
                if showsDetails, narrow {
                    if expanded {
                        details
                            .frame(maxHeight: max(280, geometry.size.height * 0.62))
                            .clipShape(.rect(topLeadingRadius: 18, topTrailingRadius: 18))
                            .transition(.move(edge: .bottom))
                    } else {
                        WorldPeek(world: world, activity: activity, picked: $picked) { expanded = true }
                            .padding(10)
                            .transition(.move(edge: .bottom).combined(with: .opacity))
                    }
                }
            }
            .animation(reducedMotion ? nil : .snappy(duration: 0.22), value: showsDetails)
            .animation(reducedMotion ? nil : .snappy(duration: 0.22), value: expanded)
            .onChange(of: room, initial: true) { viewport = room }
            .onChange(of: graph, initial: true) { rebuild(tall: tall, room: room) }
            .onChange(of: tall) {
                framed = false
                rebuild(tall: tall, room: room)
            }
            .onChange(of: picked) { if picked == nil { expanded = false } }
        }
        .background(Color.worldSea)
        .environment(previews)
        #if os(macOS)
        .onExitCommand { picked = nil }
        #endif
    }

    private func rebuild(tall: Bool, room: CGSize) {
        let next = WorkWorld(graph, tall: tall)
        if next != world { world = next }
        guard tall, !framed, room.width > 0, !world.stations.isEmpty else { return }
        framed = true
        // A long trail doesn't fit a phone at a size worth looking at, so it opens on where the work is.
        let busy = world.actors.filter { $0.station != nil }
        var area = CGRect(x: -120, y: -110, width: 240, height: 200)
        if !busy.isEmpty {
            area = .null
            for actor in busy {
                let cell = WorldMetrics.cell(actor, in: world)
                let feet = WorldMetrics.screen(cell.i, cell.j)
                area = area.union(CGRect(x: feet.x - 130, y: feet.y - 170, width: 260, height: 230))
            }
        }
        let target = aim(at: area, closest: 0.85, in: room)
        zoom = target.zoom
        shift = target.shift
    }

    /// The same panel the app uses beside a chat, so details read the way a profile does.
    private var details: some View {
        WorldDetails(world: world, activity: activity, picked: $picked, openChat: openChat)
            .background(PanelSurface.color(scheme))
    }

    private func map(size: CGSize, narrow: Bool) -> some View {
        let camera = Camera(world: world, viewport: size, zoom: zoom, shift: shift)
        let focus = picked.map(world.focus)
        let unit = max(camera.scale, WorldMetrics.actorFloor)
        let heads = heads(camera, unit: unit)
        let spread = unit / camera.scale
        return ZStack(alignment: .topLeading) {
            Color.clear.contentShape(Rectangle())
                .gesture(SpatialTapGesture(count: 2).onEnded { zoom(by: 1.6, around: $0.location, in: size, animated: true) })
                .onTapGesture { picked = nil }
                .accessibilityHidden(true)
            WorldGround(world: world, place: camera.point, scale: camera.scale)
            ForEach(world.groups) { group in
                let foot = camera.point(WorldMetrics.screen(group.flagI, group.flagJ))
                WorldBanner(group: group, selected: picked == "group:" + group.id) { pick("group:" + group.id) }
                    .opacity(focus == nil || picked == "group:" + group.id ? 1 : 0.5)
                    .position(x: foot.x, y: foot.y - 34 * camera.scale - 14)
                    .zIndex(10_000 + (group.flagI + group.flagJ) * 10 - 1)
            }
            ForEach(world.stations) { station in
                let cell = WorldMetrics.cell(station)
                let point = camera.point(WorldMetrics.screen(cell.i, cell.j))
                WorldStationView(station: station, present: world.present(at: station.id).count, scale: camera.scale,
                                 selected: picked == station.id, named: named || focus?.contains(station.id) == true) { pick(station.id) }
                    .opacity(focus.map { $0.contains(station.id) ? 1 : 0.35 } ?? 1)
                    .position(x: point.x, y: point.y + (WorldStationView.size.height / 2 - WorldStationView.anchor.y) * camera.scale)
                    .zIndex(10_000 + (cell.i + cell.j) * 10)
            }
            ForEach(world.actors) { actor in
                let cell = WorldMetrics.cell(actor, in: world, spread: spread)
                let point = camera.point(WorldMetrics.screen(cell.i, cell.j))
                WorldActorView(actor: actor, activity: activity, scale: unit, selected: picked == actor.id) { pick(actor.id) }
                    .opacity(focus.map { $0.contains(actor.id) ? 1 : 0.35 } ?? 1)
                    .position(x: point.x, y: point.y - WorldMetrics.avatar * unit / 2)
                    .zIndex(10_000 + (cell.i + cell.j) * 10 + 5)
                    // Walks to wherever its work takes it, rather than jumping there.
                    .animation(reducedMotion ? nil : .smooth(duration: 0.9), value: actor.station)
                    .animation(reducedMotion ? nil : .smooth(duration: 0.9), value: actor.order)
            }
            // Behind everyone and everything standing, so a line through the air never crosses out what someone's doing.
            WorldTrails(world: world, points: heads, picked: picked, handoffs: activity.handoffs)
                .zIndex(5_000)
            ForEach(flights(camera, unit: unit, heads: heads), id: \.trail.id) { flight in
                let id = "trail:" + flight.trail.id
                Button { pick(id) } label: {
                    WorldTag(symbol: GraphLook.cargo(flight.trail.wire.carries), text: flight.text, color: GraphTint.cargo(flight.trail.wire.carries), selected: picked == id)
                        .frame(width: flight.box.width)
                        #if os(iOS)
                        .frame(minHeight: 44)
                        #endif
                        .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel(Self.caption(flight.trail, in: world))
                .opacity(focus == nil || focus?.contains(flight.trail.from) == true && focus?.contains(flight.trail.to) == true ? 1 : 0.25)
                .position(x: flight.box.midX, y: flight.box.midY)
                .zIndex(100_001)
            }
            if loaded && world.actors.isEmpty && world.stations.isEmpty {
                Text("No activity yet.").foregroundStyle(.muted)
                    .frame(maxWidth: .infinity, maxHeight: .infinity)
            }
            WorldRoster(world: world, activity: activity, picked: picked) { pick($0) }
                .frame(maxWidth: .infinity)
                .padding(.top, 10).padding(.leading, narrow ? 52 : 60).padding(.trailing, 10)
                .zIndex(200_000)
            toolbar(viewport: size)
                .frame(maxHeight: .infinity)
                .padding(.leading, 12)
                .zIndex(200_001)
        }
        .frame(width: size.width, height: size.height, alignment: .topLeading)
        .clipped()
        .coordinateSpace(.named("world"))
        // From anywhere, since most of a map is covered by something that can be picked.
        .simultaneousGesture(DragGesture(minimumDistance: 8)
            .onChanged { value in
                flight?.cancel()
                shift.width += value.translation.width - dragged.width
                shift.height += value.translation.height - dragged.height
                dragged = value.translation
            }
            .onEnded { value in
                dragged = .zero
                // Carries on a little after a flick, the way a map does under a finger.
                let glide = CGSize(width: value.predictedEndTranslation.width - value.translation.width,
                                   height: value.predictedEndTranslation.height - value.translation.height)
                guard hypot(glide.width, glide.height) > 60 else { return }
                fly(zoom: zoom, shift: CGSize(width: shift.width + glide.width * 0.5, height: shift.height + glide.height * 0.5), settling: true)
            })
        .simultaneousGesture(MagnifyGesture()
            .onChanged { value in
                zoom(by: value.magnification / pinch, around: value.startLocation, in: size)
                pinch = value.magnification
            }
            .onEnded { _ in pinch = 1 })
        #if os(macOS)
        .onContinuousHover(coordinateSpace: .named("world")) { phase in
            if case .active(let location) = phase { events.pointer = location } else { events.pointer = nil }
        }
        .onAppear {
            events.pan = { delta in
                flight?.cancel()
                shift.width += delta.width
                shift.height += delta.height
            }
            events.zoom = { factor, anchor in zoom(by: factor, around: anchor, in: viewport) }
            events.start()
        }
        .onDisappear { events.stop() }
        #endif
    }

    // MARK: Picking and moving

    /// Picks something, and brings it into view: a district fills the view, and anything else only moves if it was out of sight.
    private func pick(_ id: String) {
        guard picked != id else {
            picked = nil
            return
        }
        // The details panel is about to take its share of a wide view, so what's picked is centred in what will be left.
        let opening = !showsDetails && viewport.width >= 640
        let room = CGSize(width: viewport.width - (opening ? Self.detailsWidth + 1 : 0), height: viewport.height)
        picked = id
        if let group = world.group(id) {
            let target = aim(at: area(group), closest: 1.25, in: room)
            fly(zoom: target.zoom, shift: target.shift)
        } else if let station = world.station(id) {
            reveal(WorldMetrics.screen(Double(station.i), Double(station.j)))
        } else if let actor = world.actor(id) {
            let cell = WorldMetrics.cell(actor, in: world)
            reveal(WorldMetrics.screen(cell.i, cell.j))
        }
    }

    /// The part of the map a district covers.
    private func area(_ group: WorldGroup) -> CGRect {
        let top = WorldMetrics.screen(Double(group.minI) - 3, Double(group.minJ) - 3)
        let bottom = WorldMetrics.screen(Double(group.maxI) + 3, Double(group.maxJ) + 3)
        let left = WorldMetrics.screen(Double(group.minI) - 3, Double(group.maxJ) + 3)
        let right = WorldMetrics.screen(Double(group.maxI) + 3, Double(group.minJ) - 3)
        return CGRect(x: left.x, y: top.y - 90, width: right.x - left.x, height: bottom.y - top.y + 120)
    }

    /// The zoom and shift that put an area of the map in the middle of the view, filling most of it.
    private func aim(at area: CGRect, closest: Double, in room: CGSize) -> (zoom: Double, shift: CGSize) {
        let camera = Camera(world: world, viewport: room, zoom: 1, shift: .zero)
        let wanted = min(closest, room.width * 0.86 / max(1, area.width), room.height * 0.8 / max(1, area.height))
        let next = min(Self.zooms.upperBound, max(Self.zooms.lowerBound, wanted / camera.scale))
        let scale = camera.scale * next
        return (next, CGSize(width: room.width / 2 - camera.base.x - (area.midX - camera.middle.x) * scale,
                             height: room.height / 2 - camera.base.y - (area.midY - camera.middle.y) * scale))
    }

    /// Moves the map just far enough that a place on it isn't off screen or under something.
    private func reveal(_ place: CGPoint) {
        let at = Camera(world: world, viewport: viewport, zoom: zoom, shift: shift).point(place)
        let safe = CGRect(origin: .zero, size: viewport).insetBy(dx: 80, dy: 130)
        guard !safe.contains(at), viewport.width > 0 else { return }
        fly(zoom: zoom, shift: CGSize(width: shift.width + viewport.width / 2 - at.x, height: shift.height + viewport.height / 2 - at.y))
    }

    /// Glides to a new view. The ground is drawn, not laid out, so it's moved a frame at a time.
    private func fly(zoom target: Double, shift end: CGSize, settling: Bool = false) {
        flight?.cancel()
        guard !reducedMotion else {
            zoom = target
            shift = end
            return
        }
        let from = (zoom: zoom, shift: shift)
        flight = Task { @MainActor in
            let steps = 18
            for step in 1...steps {
                try? await Task.sleep(for: .milliseconds(16))
                if Task.isCancelled { return }
                let t = Double(step) / Double(steps)
                // A glide only slows down; a move to somewhere starts gently too.
                let eased = settling ? 1 - pow(1 - t, 3) : t * t * (3 - 2 * t)
                zoom = from.zoom + (target - from.zoom) * eased
                shift = CGSize(width: from.shift.width + (end.width - from.shift.width) * eased,
                               height: from.shift.height + (end.height - from.shift.height) * eased)
            }
        }
    }

    /// Keeps the point under the pointer still while the map grows or shrinks around it.
    private func zoom(by factor: Double, around anchor: CGPoint, in viewport: CGSize, animated: Bool = false) {
        let next = min(Self.zooms.upperBound, max(Self.zooms.lowerBound, zoom * factor))
        guard next != zoom else { return }
        let ratio = next / zoom
        let base = Camera(world: world, viewport: viewport, zoom: 1, shift: .zero).base
        let fromOrigin = CGSize(width: anchor.x - base.x - shift.width, height: anchor.y - base.y - shift.height)
        let end = CGSize(width: shift.width - fromOrigin.width * (ratio - 1), height: shift.height - fromOrigin.height * (ratio - 1))
        if animated {
            fly(zoom: next, shift: end)
        } else {
            flight?.cancel()
            zoom = next
            shift = end
        }
    }

    // MARK: Where things are

    /// Where trails start and end: over an actor's head, or the top of a station.
    private func heads(_ camera: Camera, unit: Double) -> [String: CGPoint] {
        var out: [String: CGPoint] = [:]
        for actor in world.actors {
            let cell = WorldMetrics.cell(actor, in: world, spread: unit / camera.scale)
            let feet = camera.point(WorldMetrics.screen(cell.i, cell.j))
            // From over their tag when they wear one, so a line never runs through it.
            out[actor.id] = CGPoint(x: feet.x, y: feet.y - WorldMetrics.avatar * unit - (WorldActorView.tag(actor) == nil ? 4 : 32))
        }
        for station in world.stations {
            let cell = WorldMetrics.cell(station)
            let centre = WorldMetrics.screen(cell.i, cell.j)
            out[station.id] = camera.point(CGPoint(x: centre.x, y: centre.y + WorldModel.back - WorldModel.height(station)))
        }
        return out
    }

    /// What a handoff says about itself: what it is and who it's for.
    static func caption(_ trail: WorldTrail, in world: WorkWorld) -> String {
        let back = trail.wire.direction == WorkWireDirection.back
        let what = trail.wire.label?.isEmpty == false ? trail.wire.label ?? "" : GraphLook.cargoTitle(trail.wire.carries) ?? "Handoff"
        guard let target = world.actor(back ? trail.from : trail.to)?.node.title else { return what }
        return target == "You" ? "\(what) for you" : "\(what) for \(target)"
    }

    /// A tag for each handoff in the air, at the top of its arc and clear of every other tag.
    private func flights(_ camera: Camera, unit: Double, heads: [String: CGPoint]) -> [(trail: WorldTrail, box: CGRect, text: String)] {
        var taken: [CGRect] = []
        // Every station's name and picture, so a tag never lands on one.
        for station in world.stations {
            let cell = WorldMetrics.cell(station)
            let feet = camera.point(WorldMetrics.screen(cell.i, cell.j))
            let top = feet.y + WorldModel.back * camera.scale - WorldModel.height(station) * camera.scale
            taken.append(CGRect(x: feet.x - 90, y: top - 74 * camera.scale, width: 180, height: 100 * camera.scale + 24))
        }
        for actor in world.actors {
            let cell = WorldMetrics.cell(actor, in: world, spread: unit / camera.scale)
            let feet = camera.point(WorldMetrics.screen(cell.i, cell.j))
            taken.append(CGRect(x: feet.x - 40, y: feet.y + 9 * unit, width: 80, height: 20))
            guard let tag = WorldActorView.tag(actor) else { continue }
            let width = min(190, Double(tag.text.count) * 6.4 + 40)
            taken.append(CGRect(x: feet.x - width / 2, y: feet.y - WorldMetrics.avatar * unit - 29, width: width, height: 24))
        }
        // Two at a time in the air; the rest are there to pick.
        let shown = world.trails.filter { $0.kind == .handoff && $0.active && WorldTrails.shows($0, in: world, picked: picked) }.prefix(2)
        let spots = shown.compactMap { trail -> (WorldTrail, CGPoint)? in
            guard let from = heads[trail.from], let to = heads[trail.to] else { return nil }
            return (trail, WorldMetrics.arc(from, to).middle)
        }
        var out: [(trail: WorldTrail, box: CGRect, text: String)] = []
        for (trail, middle) in spots.sorted(by: { $0.1.y > $1.1.y }) {
            // Its line already says who it's for, so the tag only says what it is. Picked, it says all of it.
            let whole = picked == "trail:" + trail.id
            let text = whole ? Self.caption(trail, in: world) : trail.wire.label?.isEmpty == false ? trail.wire.label ?? "" : GraphLook.cargoTitle(trail.wire.carries) ?? "Handoff"
            let width = min(190, Double(text.count) * 6.6 + 42)
            var box = CGRect(x: middle.x - width / 2, y: middle.y - 12, width: width, height: 24)
            while taken.contains(where: { $0.insetBy(dx: -4, dy: -3).intersects(box) }) { box.origin.y -= 10 }
            taken.append(box)
            out.append((trail, box, text))
        }
        return out
    }

    private func toolbar(viewport: CGSize) -> some View {
        let centre = CGPoint(x: viewport.width / 2, y: viewport.height / 2)
        return VStack(spacing: 8) {
            VStack(spacing: 0) {
                GraphToolButton(symbol: "point.3.connected.trianglepath.dotted", label: "Show diagram", action: showDiagram)
                GraphToolButton(symbol: named ? "tag.fill" : "tag", label: named ? "Hide names" : "Show names") { named.toggle() }
            }
            .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 14))
            VStack(spacing: 0) {
                GraphToolButton(symbol: "arrow.up.left.and.down.right.magnifyingglass", label: "Fit to view") { fly(zoom: 1, shift: .zero) }
                Divider().frame(width: 16).padding(.vertical, 2)
                GraphToolButton(symbol: "plus", label: "Zoom in", enabled: zoom < Self.zooms.upperBound) { zoom(by: 1.3, around: centre, in: viewport, animated: true) }
                GraphToolButton(symbol: "minus", label: "Zoom out", enabled: zoom > Self.zooms.lowerBound) { zoom(by: 1 / 1.3, around: centre, in: viewport, animated: true) }
            }
            .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 14))
        }
    }
}

/// Fits the whole map in view, then applies whatever the viewer moved or zoomed.
private struct Camera {
    let scale: Double
    /// Where the middle of the map lands before the viewer moves anything.
    let base: CGPoint
    let middle: CGPoint
    private let origin: CGPoint

    init(world: WorkWorld, viewport: CGSize, zoom: Double, shift: CGSize) {
        var bounds = CGRect(x: -200, y: -120, width: 400, height: 250)
        for station in world.stations {
            let cell = WorldMetrics.cell(station)
            let centre = WorldMetrics.screen(cell.i, cell.j)
            bounds = bounds.union(CGRect(x: centre.x - 176, y: centre.y - 150, width: 352, height: 270))
        }
        for actor in world.actors {
            let cell = WorldMetrics.cell(actor, in: world)
            let feet = WorldMetrics.screen(cell.i, cell.j)
            bounds = bounds.union(CGRect(x: feet.x - 80, y: feet.y - 130, width: 160, height: 166))
        }
        let narrow = viewport.width < 640
        let leading = narrow ? 44.0 : 60.0
        let margin = narrow ? 8.0 : 24.0
        let roomX = max(1, viewport.width - leading - margin * 2)
        let roomY = max(1, viewport.height - margin * 2 - 30)
        let fit = min(1.2, roomX / bounds.width, roomY / bounds.height)
        // Past a point, shrinking to fit everything leaves nothing readable, so a large map opens larger than the view and pans.
        scale = max(narrow ? 0.5 : 0.42, fit) * zoom
        middle = CGPoint(x: bounds.midX, y: bounds.midY)
        base = CGPoint(x: leading + margin + roomX / 2, y: margin + 30 + roomY / 2)
        origin = CGPoint(x: base.x + shift.width, y: base.y + shift.height)
    }

    func point(_ map: CGPoint) -> CGPoint {
        CGPoint(x: origin.x + (map.x - middle.x) * scale, y: origin.y + (map.y - middle.y) * scale)
    }
}

/// Who's busy, along the top, gathered by what the work belongs to: a district, a pipeline, a bot's own work.
/// Whoever's waiting on you comes first. Clicking one goes to them, wherever the map is looking.
struct WorldRoster: View {
    let world: WorkWorld
    let activity: ActivityStore
    let picked: String?
    let pick: (String) -> Void

    private struct Section: Identifiable {
        var id: String
        var title: String
        var symbol: String
        var tint: Color
        var actors: [WorldActor]
    }

    private var sections: [Section] {
        let rank = [WorkNodeState.waiting: 0, WorkNodeState.failed: 1, WorkNodeState.working: 2]
        let busy = world.actors.filter { rank[$0.node.state ?? ""] != nil && $0.node.kind != WorkKind.person }
        var order: [String] = []
        var members: [String: [WorldActor]] = [:]
        for actor in busy {
            let group = world.group(of: actor.id)
            let id = group?.id ?? "loose"
            if members[id] == nil { order.append(id) }
            members[id, default: []].append(actor)
        }
        let made = order.map { id -> Section in
            let list = (members[id] ?? []).sorted { (rank[$0.node.state ?? ""] ?? 9, $0.node.title) < (rank[$1.node.state ?? ""] ?? 9, $1.node.title) }
            let group = world.groups.first { $0.id == id }
            return Section(id: id, title: group?.title ?? "Elsewhere", symbol: group?.symbol ?? "person.2", tint: group.map { GraphTint.color($0.family) } ?? .secondary,
                           actors: list)
        }
        // Whatever needs you first, then the rest in the map's own order.
        return made.sorted { a, b in
            let first = a.actors.contains { $0.node.state == WorkNodeState.waiting || $0.node.state == WorkNodeState.failed } ? 0 : 1
            let second = b.actors.contains { $0.node.state == WorkNodeState.waiting || $0.node.state == WorkNodeState.failed } ? 0 : 1
            return first != second ? first < second : a.title < b.title
        }
    }

    var body: some View {
        let list = sections
        if !list.isEmpty {
            ScrollView(.horizontal) {
                HStack(alignment: .top, spacing: 14) {
                    ForEach(list) { section in
                        VStack(alignment: .leading, spacing: 4) {
                            HStack(spacing: 4) {
                                Image(systemName: section.symbol).font(.system(size: 9, weight: .semibold)).foregroundStyle(section.tint)
                                Text(section.title).font(.caption2.weight(.semibold)).foregroundStyle(.muted).lineLimit(1)
                            }
                            HStack(spacing: 6) {
                                ForEach(section.actors.prefix(3)) { actor in
                                    Button { pick(actor.id) } label: { chip(actor) }
                                        .buttonStyle(.plain)
                                        .accessibilityLabel("\(actor.node.title), \(WorldActorView.tag(actor)?.text ?? "")")
                                }
                                if section.actors.count > 3 {
                                    Text("+\(section.actors.count - 3)").font(.caption2.weight(.semibold)).foregroundStyle(.muted)
                                        .padding(.horizontal, 6).frame(height: 34)
                                        .background(.white.opacity(0.35), in: Capsule())
                                }
                            }
                        }
                    }
                }
                .padding(.horizontal, 2).padding(.vertical, 2)
            }
            .scrollIndicators(.hidden)
            .scrollBounceBehavior(.basedOnSize, axes: .horizontal)
            .frame(maxWidth: .infinity, alignment: .center)
        }
    }

    /// The bot itself, ringed in how it's doing, over what it's doing. No dot, no clutter.
    private func chip(_ actor: WorldActor) -> some View {
        let tag = WorldActorView.tag(actor)
        let ring: Color = switch actor.node.state {
        case WorkNodeState.waiting: .orange
        case WorkNodeState.failed: .red
        default: .green
        }
        return HStack(spacing: 7) {
            Group {
                if let agent = actor.node.agentID, !agent.isEmpty {
                    AgentAvatar(agentID: agent, activity: activity, showsMarker: false)
                } else {
                    WorldToken(symbol: GraphLook.look(actor.node).symbol, tint: GraphTint.color(actor.node))
                }
            }
            .frame(width: 26, height: 26)
            .padding(2)
            .overlay { Circle().strokeBorder(ring, lineWidth: 1.8) }
            VStack(alignment: .leading, spacing: 0) {
                Text(actor.node.title).font(.caption.weight(.semibold)).lineLimit(1)
                Text(tag?.text ?? "").font(.caption2).foregroundStyle(.muted).lineLimit(1)
                    .frame(maxWidth: 132, alignment: .leading)
            }
        }
        .padding(.leading, 4).padding(.trailing, 10).frame(height: 40)
        #if os(iOS)
        .frame(minHeight: 44)
        #endif
        .contentShape(Rectangle())
        .glassEffect(.regular.interactive(), in: Capsule())
        .overlay { if picked == actor.id { Capsule().strokeBorder(Color.primary, lineWidth: 2) } }
    }
}
