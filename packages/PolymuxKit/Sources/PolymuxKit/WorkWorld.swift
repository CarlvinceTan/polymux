import Foundation

/// A place on the map: an app, a repository, a device, a step of a pipeline. Every bot's Terminal is the one Terminal, so a place stays put.
public struct WorldStation: Identifiable, Hashable, Sendable {
    public var id: String
    /// The node that names and draws the station.
    public var node: WorkNode
    /// Every graph node this station stands for.
    public var members: [String]
    public var items: [WorldItem]
    public var state: String
    /// The middle of its plot, in ground cells from home.
    public var i: Int
    public var j: Int
    /// The district it stands in.
    public var group: String
    /// The device or pipeline it belongs to.
    public var zone: String?
    /// A thing standing on its own, such as a database, drawn smaller than a place.
    public var small: Bool
    /// The node whose picture stands for the station: the latest thing it made that has one.
    public var preview: String?
    /// Which step of its pipeline it is, counting from one.
    public var step: Int?
    /// Whose work it holds, when it belongs to one bot.
    public var owner: String?
}

/// The ground a district stands on. Each kind of district has its own, so it's known from across the map without reading.
public enum WorldBiome: String, Sendable, CaseIterable {
    case plaza, meadow, blossom, desert, crystal, ice, slate, savanna
}

/// Stations that belong together share an islet: a machine and what's on it, a pipeline and its steps, or one kind of place.
public struct WorldGroup: Identifiable, Hashable, Sendable {
    public var id: String
    public var title: String
    public var symbol: String
    public var family: WorkFamily
    public var biome: WorldBiome
    /// Its steps happen in order, so they're joined by a road.
    public var ordered: Bool
    /// Where its banner stands, in ground cells: the corner furthest from the viewer.
    public var flagI: Double
    public var flagJ: Double
    /// The plots it covers, as the middles of its outermost stations.
    public var minI: Int
    public var minJ: Int
    public var maxI: Int
    public var maxJ: Int
}

/// One square of ground, from (i, j) to (i + 1, j + 1).
public struct WorldCell: Hashable, Sendable {
    public var i: Int
    public var j: Int

    public init(i: Int, j: Int) {
        self.i = i
        self.j = j
    }
}

/// Something a station holds: a file, a tab, a command, a pull request.
public struct WorldItem: Identifiable, Hashable, Sendable {
    public var node: WorkNode
    /// Who it belongs to, when a bot made or touched it.
    public var owner: String?
    public var id: String { node.id }
    public var pictured: Bool { node.preview?.isEmpty == false }
}

/// Someone on the map: a person, a bot, or an outside agent.
public struct WorldActor: Identifiable, Hashable, Sendable {
    public var node: WorkNode
    /// Where it's working right now. Nil rests at home.
    public var station: String?
    /// Its place in line at that station, or at home.
    public var order: Int
    /// What it's handling right now, drawn over its head.
    public var cargo: String?
    /// What it's doing, in a few words.
    public var doing: String?
    public var id: String { node.id }
}

/// A connection the map draws: something passed between two actors, a road between two stations, or a visit.
public struct WorldTrail: Identifiable, Hashable, Sendable {
    public enum Kind: Sendable { case handoff, road, visit }
    public var wire: WorkWire
    public var kind: Kind
    /// Its ends as the map knows them: actor IDs or station IDs.
    public var from: String
    public var to: String
    public var id: String { wire.id }
    public var active: Bool { wire.state == WorkWireState.active }
}

/// The work graph as a map: districts around a home, actors standing where they work, and what passes between them.
public struct WorkWorld: Hashable, Sendable {
    public var stations: [WorldStation] = []
    public var groups: [WorldGroup] = []
    /// The planks from one islet to the next.
    public var bridges: [WorldCell] = []
    public var actors: [WorldActor] = []
    public var trails: [WorldTrail] = []

    /// Cells from one plot to the next within a district.
    public static let pitch = 5
    /// Cells from the middle of home to its edge.
    public static let homeReach = 4
    /// Cells of open water between islets.
    private static let gap = 2

    public init() {}

    /// `tall` lays districts out as a trail down the screen, for a chat's side panel or a phone.
    public init(_ graph: WorkGraph, tall: Bool = false) {
        let byID = Dictionary(graph.nodes.map { ($0.id, $0) }, uniquingKeysWith: { first, _ in first })
        func isFrame(_ node: WorkNode) -> Bool { node.shape == WorkNodeShape.frame || node.kind == WorkKind.group || node.kind == WorkKind.pipeline }
        func isActor(_ node: WorkNode) -> Bool { !isFrame(node) && WorkKind.look(of: node).shape == .actor }
        func parent(_ node: WorkNode) -> WorkNode? { node.parent.flatMap { byID[$0] } }

        // A place is a station. So is a thing that nothing holds, or that only a frame holds.
        func stands(_ node: WorkNode) -> Bool {
            guard !isFrame(node), !isActor(node) else { return false }
            if WorkKind.look(of: node).shape == .place { return true }
            guard let holder = parent(node) else { return true }
            return isFrame(holder)
        }
        func key(_ node: WorkNode) -> String {
            guard let holder = parent(node), isActor(holder) else { return node.id }
            return "shared:\(node.kind ?? ""):\(node.title.lowercased())"
        }

        var order: [String] = []
        var drafts: [String: WorldStation] = [:]
        var stationOf: [String: String] = [:]
        var frames: [String: WorkNode] = [:]
        for node in graph.nodes where stands(node) {
            let id = key(node)
            stationOf[node.id] = id
            if drafts[id] == nil {
                order.append(id)
                let frame = parent(node).flatMap { isFrame($0) ? $0 : nil }
                if let frame { frames[frame.id] = frame }
                // The owner is a bot, known by its node, so a district can carry its name.
                if let sole = parent(node), isActor(sole) { frames["bot:" + sole.id] = sole }
                let sole = parent(node).flatMap { isActor($0) ? $0.id : nil }
                drafts[id] = WorldStation(id: id, node: node, members: [], items: [], state: WorkNodeState.idle, i: 0, j: 0, group: "", zone: frame?.id,
                                          small: WorkKind.look(of: node).shape != .place, preview: node.preview?.isEmpty == false ? node.id : nil, owner: sole)
            }
            drafts[id]?.members.append(node.id)
            // A shared station is as busy as its busiest member; one of its own keeps what it says.
            let state = node.state ?? WorkNodeState.idle
            if id == node.id { drafts[id]?.state = state }
            else if state == WorkNodeState.working { drafts[id]?.state = state }
            else if state == WorkNodeState.failed, drafts[id]?.state != WorkNodeState.working { drafts[id]?.state = state }
        }

        // Everything else that isn't an actor is an item of the nearest station above it.
        for node in graph.nodes where !isFrame(node) && !isActor(node) && stationOf[node.id] == nil {
            var holder = parent(node)
            var seen = Set<String>()
            var owner: String?
            while let next = holder, seen.insert(next.id).inserted, stationOf[next.id] == nil { holder = parent(next) }
            guard let place = holder, let station = stationOf[place.id] else { continue }
            if let above = parent(place), isActor(above) { owner = above.id }
            stationOf[node.id] = station
            drafts[station]?.items.append(WorldItem(node: node, owner: owner))
            if node.preview?.isEmpty == false { drafts[station]?.preview = node.id }
        }

        let actorIDs = Set(graph.nodes.filter(isActor).map(\.id))
        var working: [String: (station: String, cargo: String?, doing: String?)] = [:]
        var seenTrails = Set<String>()
        // Which station follows which, for laying a pipeline out in order.
        var follows: [String: [String]] = [:]
        for wire in graph.wires {
            let fromActor = actorIDs.contains(wire.from)
            let toActor = actorIDs.contains(wire.to)
            if fromActor, toActor {
                trails.append(WorldTrail(wire: wire, kind: .handoff, from: wire.from, to: wire.to))
            } else if fromActor != toActor {
                let actor = fromActor ? wire.from : wire.to
                guard let station = stationOf[fromActor ? wire.to : wire.from] else { continue }
                // Someone on a pipeline's step stands there, whatever tool they're reaching for: the step is what they're doing.
                let step = drafts[station]?.node.kind == WorkKind.step
                if wire.state == WorkWireState.active, working[actor] == nil || step { working[actor] = (station, wire.carries, wire.label) }
                guard seenTrails.insert("\(actor)>\(station)").inserted else { continue }
                trails.append(WorldTrail(wire: wire, kind: .visit, from: actor, to: station))
            } else if let from = stationOf[wire.from], let to = stationOf[wire.to], from != to {
                guard seenTrails.insert(from < to ? "\(from)|\(to)" : "\(to)|\(from)").inserted else { continue }
                let back = wire.direction == WorkWireDirection.back
                follows[back ? to : from, default: []].append(back ? from : to)
                trails.append(WorldTrail(wire: wire, kind: .road, from: back ? to : from, to: back ? from : to))
            }
        }

        var queue: [String: Int] = [:]
        for node in graph.nodes where isActor(node) {
            let at = working[node.id]
            // An outside agent with nowhere of its own to be stays with whoever started it.
            let station = at?.station ?? parent(node).flatMap { isActor($0) ? working[$0.id]?.station : nil }
            let line = station ?? ""
            actors.append(WorldActor(node: node, station: station, order: queue[line, default: 0], cargo: at?.cargo, doing: at?.doing))
            queue[line, default: 0] += 1
        }
        arrange(order.compactMap { drafts[$0] }, frames: frames, follows: follows, tall: tall)
    }

    // MARK: Grouping

    private struct District {
        var id: String
        var title: String
        var symbol: String
        var family: WorkFamily
        var biome: WorldBiome
        var ordered = false
    }

    private static func biome(_ family: WorkFamily) -> WorldBiome {
        switch family {
        case .apps, .people, .bots, .agents: .meadow
        case .media: .blossom
        case .files: .desert
        case .code: .crystal
        case .data: .ice
        case .devices: .slate
        case .tasks: .savanna
        }
    }

    /// Districts go by what the work is for, not by what each thing is: a machine and everything done on it, a pipeline
    /// and its steps, or one bot's work. So the map answers "what's being done" before "what kind of thing is this".
    private static func district(_ station: WorldStation, frames: [String: WorkNode]) -> District {
        if let zone = station.zone, let frame = frames[zone] {
            let look = WorkKind.look(of: frame)
            let pipeline = frame.kind == WorkKind.pipeline
            return District(id: "zone:" + zone, title: frame.title, symbol: look.symbol, family: look.family,
                            biome: pipeline ? .savanna : look.family == .devices ? .slate : biome(look.family), ordered: pipeline)
        }
        if let zone = station.zone, let owner = frames["bot:" + zone] {
            return District(id: "zone:" + zone, title: owner.title, symbol: "person.crop.square", family: .bots, biome: .meadow)
        }
        // A place on its own belongs to whoever uses it, so their work sits together.
        if let owner = station.owner, let bot = frames["bot:" + owner] {
            return District(id: "bot:" + owner, title: bot.title, symbol: "person.crop.square", family: .bots, biome: .meadow)
        }
        let family = WorkKind.look(of: station.node).family
        if family == .code { return District(id: "code", title: "Shared code", symbol: WorkFamily.code.symbol, family: .code, biome: .crystal) }
        return District(id: "shared", title: "Shared", symbol: "square.stack.3d.up", family: .apps, biome: .meadow)
    }

    /// A pipeline's steps in the order its roads run, and anything else by kind and then name.
    private static func sorted(_ list: [WorldStation], ordered: Bool, follows: [String: [String]]) -> [WorldStation] {
        let families = WorkFamily.allCases
        let byName = list.sorted { a, b in
            if a.small != b.small { return !a.small }
            let first = families.firstIndex(of: WorkKind.look(of: a.node).family) ?? 0
            let second = families.firstIndex(of: WorkKind.look(of: b.node).family) ?? 0
            if first != second { return first < second }
            return a.node.title.localizedCaseInsensitiveCompare(b.node.title) == .orderedAscending
        }
        guard ordered else { return byName }
        let inside = Set(list.map(\.id))
        var waiting = Dictionary(uniqueKeysWithValues: list.map { ($0.id, 0) })
        for (from, next) in follows where inside.contains(from) {
            for to in next where inside.contains(to) { waiting[to, default: 0] += 1 }
        }
        // As reported, so steps nobody joined by a road still come out in the order they were given.
        var left = list
        var out: [WorldStation] = []
        while !left.isEmpty {
            let index = left.firstIndex { waiting[$0.id] == 0 } ?? 0
            let next = left.remove(at: index)
            out.append(next)
            for to in follows[next.id] ?? [] where inside.contains(to) { waiting[to, default: 0] -= 1 }
        }
        return out
    }

    // MARK: Layout

    private mutating func arrange(_ drafts: [WorldStation], frames: [String: WorkNode], follows: [String: [String]], tall: Bool) {
        var members: [String: [WorldStation]] = [:]
        var found: [District] = []
        for station in drafts {
            let district = Self.district(station, frames: frames)
            if members[district.id] == nil { found.append(district) }
            members[district.id, default: []].append(station)
        }
        // Pipelines first, since they're what's being made; then the largest, so the map comes out balanced.
        // Names break ties, which keeps the map the same between visits.
        found.sort { a, b in
            if a.ordered != b.ordered { return a.ordered }
            let first = members[a.id]?.count ?? 0
            let second = members[b.id]?.count ?? 0
            return first != second ? first > second : a.title < b.title
        }
        for district in found {
            var list = Self.sorted(members[district.id] ?? [], ordered: district.ordered, follows: follows)
            if district.ordered { for index in list.indices { list[index].step = index + 1 } }
            let placed = tall ? trail(list, district) : arm(list, district)
            guard let first = placed.first else { continue }
            let lows = (placed.map(\.i).min() ?? first.i, placed.map(\.j).min() ?? first.j)
            let highs = (placed.map(\.i).max() ?? first.i, placed.map(\.j).max() ?? first.j)
            // The banner stands at the corner of the first plot that's furthest from the viewer, where nothing hides it.
            groups.append(WorldGroup(id: district.id, title: district.title, symbol: district.symbol, family: district.family, biome: district.biome,
                                     ordered: district.ordered, flagI: Double(first.i) - 2.4, flagJ: Double(first.j) - 2.4,
                                     minI: lows.0, minJ: lows.1, maxI: highs.0, maxJ: highs.1))
            stations += placed
        }
    }

    /// Along each arm of the pinwheel, and the way it grows to the side.
    private static let arms: [(along: (Int, Int), across: (Int, Int))] = [((1, 0), (0, -1)), ((0, 1), (1, 0)), ((-1, 0), (0, 1)), ((0, -1), (-1, 0))]
    private var edges = Array(repeating: WorkWorld.homeReach, count: 4)

    /// Districts stand around home like the arms of a pinwheel: each starts in line with home and grows to one side,
    /// so no two can meet however large they get.
    private mutating func arm(_ list: [WorldStation], _ district: District) -> [WorldStation] {
        let arm = edges.indices.min { edges[$0] < edges[$1] } ?? 0
        let (along, across) = Self.arms[arm]
        let breadth = list.count <= 3 ? list.count : list.count == 4 ? 2 : 3
        let rows = (list.count + breadth - 1) / max(1, breadth)
        let start = edges[arm] + Self.gap + 3
        var out: [WorldStation] = []
        for (index, station) in list.enumerated() {
            var placed = station
            let row = index / breadth
            // A pipeline snakes, so each step stands beside the one before it.
            let column = district.ordered && row % 2 == 1 ? breadth - 1 - index % breadth : index % breadth
            let far = start + row * Self.pitch
            let side = column * Self.pitch
            placed.i = along.0 * far + across.0 * side
            placed.j = along.1 * far + across.1 * side
            placed.group = district.id
            out.append(placed)
        }
        edges[arm] = start + (rows - 1) * Self.pitch + 3
        return out
    }

    private var cursorI = 0
    private var cursorJ = 0
    private var reach = WorkWorld.homeReach
    private var turn = 0

    /// A trail down the screen: three plots one way, three the other, with water between one district and the next.
    private mutating func trail(_ list: [WorldStation], _ district: District) -> [WorldStation] {
        var out: [WorldStation] = []
        var left = list[...]
        var fresh = true
        while !left.isEmpty {
            let run = left.prefix(3)
            left = left.dropFirst(run.count)
            let along = turn % 2 == 0 ? (1, 0) : (0, 1)
            let across = turn % 2 == 0 ? (0, 1) : (1, 0)
            let water = fresh ? Self.gap : 0
            _ = fresh
            let start = reach + water + 3
            for (index, station) in run.enumerated() {
                var placed = station
                placed.i = cursorI + along.0 * (start + index * Self.pitch)
                placed.j = cursorJ + along.1 * (start + index * Self.pitch)
                placed.group = district.id
                out.append(placed)
            }
            if let last = out.last {
                cursorI = last.i
                cursorJ = last.j
            }
            reach = 3
            turn += 1
            fresh = false
        }
        return out
    }

    // MARK: Looking things up

    /// The district a station or an actor stands in.
    public func group(of id: String?) -> WorldGroup? {
        if let station = station(id) { return groups.first { $0.id == station.group } }
        if let actor = actor(id), let at = actor.station, let placed = station(at) { return groups.first { $0.id == placed.group } }
        return group(id)
    }

    public func station(_ id: String?) -> WorldStation? { id.flatMap { id in stations.first { $0.id == id } } }
    public func actor(_ id: String?) -> WorldActor? { id.flatMap { id in actors.first { $0.id == id } } }
    public func group(_ id: String?) -> WorldGroup? { id.flatMap { id in groups.first { "group:" + $0.id == id } } }
    public func trail(_ id: String?) -> WorldTrail? { id.flatMap { id in trails.first { "trail:" + $0.id == id } } }

    /// Something a station holds, and the station holding it.
    public func item(_ id: String?) -> (item: WorldItem, station: WorldStation)? {
        guard let id else { return nil }
        for station in stations {
            if let item = station.items.first(where: { $0.id == id }) { return (item, station) }
        }
        return nil
    }

    /// Who's standing at a station now.
    public func present(at station: String) -> [WorldActor] { actors.filter { $0.station == station } }

    /// Everyone who has used a station, here now or not.
    public func visitors(of station: String) -> [WorldActor] {
        let ids = Set(trails.filter { $0.kind == .visit && $0.to == station }.map(\.from)).union(present(at: station).map(\.id))
        return actors.filter { ids.contains($0.id) }
    }

    /// What stays lit when one thing is picked: it and whatever it touches.
    public func focus(_ id: String) -> Set<String> {
        if let group = group(id) {
            let inside = stations.filter { $0.group == group.id }.map(\.id)
            return Set(inside).union(actors.filter { $0.station.map(inside.contains) == true }.map(\.id))
        }
        if let trail = trail(id) { return [trail.from, trail.to] }
        if let found = item(id) { return Set([found.station.id, found.item.owner].compactMap { $0 }) }
        var lit: Set<String> = [id]
        for trail in trails where trail.from == id || trail.to == id { lit.formUnion([trail.from, trail.to]) }
        if let actor = actor(id), let station = actor.station { lit.insert(station) }
        for actor in present(at: id) { lit.insert(actor.id) }
        return lit
    }
}
