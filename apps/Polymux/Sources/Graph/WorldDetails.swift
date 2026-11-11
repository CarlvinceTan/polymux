import SwiftUI
import PolymuxKit

/// Whatever is picked on the map, as the few things every kind of pick has: a name, what it is, and one line about now.
struct WorldSubject {
    var title: String
    var caption: String
    /// The one thing worth saying about it right now.
    var line: String?
    var node: WorkNode?
    var symbol: String
    var tint: Color

    @MainActor static func of(_ id: String, in world: WorkWorld) -> WorldSubject? {
        if let actor = world.actor(id) {
            let look = GraphLook.look(actor.node)
            let where_ = world.station(actor.station)?.node.title
            let doing = WorldActorView.tag(actor)?.text
            return WorldSubject(title: actor.node.title, caption: caption(actor.node, state: actor.node.state),
                                line: [doing, where_.map { "at \($0)" }].compactMap { $0 }.joined(separator: " ").nilIfEmpty ?? "Resting at home",
                                node: actor.node, symbol: look.symbol, tint: GraphTint.color(actor.node))
        }
        if let station = world.station(id) {
            let here = world.present(at: station.id).map(\.node.title)
            var parts: [String] = []
            if !here.isEmpty { parts.append(here.joined(separator: ", ") + " here") }
            if !station.items.isEmpty { parts.append(station.items.count == 1 ? "1 item" : "\(station.items.count) items") }
            return WorldSubject(title: station.node.title, caption: caption(station.node, state: station.state),
                                line: parts.joined(separator: " · ").nilIfEmpty ?? station.node.subtitle,
                                node: station.node, symbol: GraphLook.look(station.node).symbol, tint: GraphTint.color(station.node))
        }
        if let group = world.group(id) {
            let inside = world.stations.filter { $0.group == group.id }
            let done = inside.filter { $0.state == WorkNodeState.done }.count
            let line = group.ordered ? "\(done) of \(inside.count) steps done" : inside.count == 1 ? "1 place" : "\(inside.count) places"
            return WorldSubject(title: group.title, caption: group.ordered ? "Pipeline" : "District", line: line, node: nil,
                                symbol: group.symbol, tint: GraphTint.color(group.family))
        }
        if let trail = world.trail(id) {
            let back = trail.wire.direction == WorkWireDirection.back
            let from = world.actor(back ? trail.to : trail.from)?.node.title ?? world.station(trail.from)?.node.title ?? ""
            return WorldSubject(title: WorldView.caption(trail, in: world), caption: trail.active ? "On its way" : "Delivered", line: "From \(from)",
                                node: nil, symbol: GraphLook.cargo(trail.wire.carries), tint: GraphTint.cargo(trail.wire.carries))
        }
        if let found = world.item(id) {
            return WorldSubject(title: found.item.node.title, caption: caption(found.item.node, state: found.item.node.state),
                                line: [found.item.node.subtitle, "in \(found.station.node.title)"].compactMap { $0 }.joined(separator: " "),
                                node: found.item.node, symbol: GraphLook.look(found.item.node).symbol, tint: GraphTint.color(found.item.node))
        }
        return nil
    }

    private static func caption(_ node: WorkNode, state: String?) -> String {
        var parts = GraphLook.look(node).title == node.title ? [] : [GraphLook.look(node).title]
        if let status = GraphTint.status(state) { parts.append(status) }
        return parts.filter { !$0.isEmpty }.joined(separator: " · ")
    }
}

private extension String {
    var nilIfEmpty: String? { isEmpty ? nil : self }
}

/// What's picked, as a line at the foot of a narrow map: enough to know what it is, with the rest one tap away.
struct WorldPeek: View {
    let world: WorkWorld
    let activity: ActivityStore
    @Binding var picked: String?
    let expand: () -> Void

    var body: some View {
        if let picked, let subject = WorldSubject.of(picked, in: world) {
            HStack(spacing: 10) {
                WorldFace(node: subject.node, symbol: subject.symbol, tint: subject.tint, activity: activity, size: 40)
                VStack(alignment: .leading, spacing: 1) {
                    HStack(alignment: .firstTextBaseline, spacing: 6) {
                        Text(subject.title).font(.headline).lineLimit(1)
                        Text(subject.caption).font(.caption).foregroundStyle(.muted).lineLimit(1).layoutPriority(-1)
                    }
                    if let line = subject.line { Text(line).font(.subheadline).foregroundStyle(.muted).lineLimit(1).truncationMode(.middle) }
                }
                Spacer(minLength: 4)
                PanelNavButton(symbol: "chevron.up", label: "More", action: expand)
                PanelNavButton(symbol: "xmark", label: "Close details") { self.picked = nil }
            }
            .padding(.leading, 12).padding(.trailing, 8).padding(.vertical, 8)
            .glassEffect(.regular, in: RoundedRectangle(cornerRadius: 20))
            .contentShape(Rectangle())
            .onTapGesture(perform: expand)
        }
    }
}

/// A bot's own face, or the mark of whatever else it is.
struct WorldFace: View {
    let node: WorkNode?
    let symbol: String
    let tint: Color
    let activity: ActivityStore
    let size: CGFloat

    var body: some View {
        if let agent = node?.agentID, !agent.isEmpty {
            AgentAvatar(agentID: agent, activity: activity, showsMarker: false).frame(width: size, height: size)
        } else if node?.kind == WorkKind.person {
            WorldPawn().frame(width: size, height: size)
        } else {
            GraphBadge(symbol: symbol, tint: tint, size: size)
        }
    }
}

/// What's known about whoever or whatever is picked, in the app's side panel. It reads; it never changes the work.
struct WorldDetails: View {
    let world: WorkWorld
    let activity: ActivityStore
    @Binding var picked: String?
    var openChat: ((String) -> Void)?
    @Environment(WorldPreviews.self) private var previews
    /// A preview opened full size, over everything, until it's closed.
    @State private var zoomed = false

    private var actor: WorldActor? { world.actor(picked) }
    private var station: WorldStation? { world.station(picked) }
    private var subject: WorldSubject? { picked.flatMap { WorldSubject.of($0, in: world) } }

    var body: some View {
        // Something inside a station goes back to the station it's in.
        let back = world.item(picked).map { found in { picked = found.station.id } }
        PanelView(subject?.title ?? "", onClose: { picked = nil }, onBack: back, header: AnyView(hero), closeLabel: "Close details") {
            ScrollView {
                VStack(alignment: .leading, spacing: PanelStyle.sectionGap) {
                    if let actor { sections(actor) }
                    if let station { sections(station) }
                    if let group = world.group(picked) { sections(group) }
                    if let trail = world.trail(picked) { sections(trail) }
                    if let found = world.item(picked) { sections(found.item, in: found.station) }
                }
                .padding(.horizontal, PanelStyle.contentInset)
            }
        } actions: {
            if let agent = actor?.node.agentID, !agent.isEmpty, let openChat {
                Button("Chat") { openChat(agent) }
            }
        }
        // Full size as its own sheet, so it's bigger than the panel and can't be covered by it.
        .sheet(isPresented: $zoomed) {
            VStack(spacing: 12) {
                if let found = world.item(picked), let picture = previews.image(found.item.id) {
                    picture.resizable().aspectRatio(contentMode: .fit)
                        .frame(minWidth: 480, idealWidth: 760, maxWidth: 900, minHeight: 320, idealHeight: 520, maxHeight: 700)
                        .clipShape(.rect(cornerRadius: 12))
                        .accessibilityLabel("Preview of \(found.item.node.title)")
                    Text(found.item.node.title).font(.headline)
                }
                Button("Close") { zoomed = false }.keyboardShortcut(.cancelAction)
            }
            .padding(24)
        }
        .onChange(of: picked) { zoomed = false }
    }

    /// The thing itself, as it stands on the map, over what it is and how it's doing.
    private var hero: some View {
        VStack(spacing: 6) {
            if let found = world.item(picked), let picture = previews.image(found.item.pictured ? found.item.id : nil) {
                Button { zoomed = true } label: {
                    picture.resizable().aspectRatio(contentMode: .fit)
                        .frame(maxHeight: 190)
                        .clipShape(.rect(cornerRadius: 12))
                        .overlay(alignment: .bottomTrailing) {
                            Image(systemName: "arrow.up.left.and.arrow.down.right")
                                .font(.caption.weight(.bold)).foregroundStyle(.white)
                                .padding(7).background(.black.opacity(0.45), in: Circle()).padding(8)
                        }
                }
                .buttonStyle(.plain)
                .accessibilityLabel("Open \(found.item.node.title) full size")
            } else if let station {
                Canvas { context, size in
                    WorldModel.draw(station, in: &context, at: CGPoint(x: size.width / 2, y: size.height - 18),
                                    tint: GraphTint.color(station.node), symbol: GraphLook.look(station.node).symbol)
                }
                .frame(width: 140, height: 96)
                .accessibilityHidden(true)
            } else if let subject {
                WorldFace(node: subject.node, symbol: subject.symbol, tint: subject.tint, activity: activity, size: 72)
            }
            if let caption = subject?.caption, !caption.isEmpty { Text(caption).font(.subheadline).foregroundStyle(.muted).lineLimit(1) }
        }
        .frame(maxWidth: .infinity)
    }

    private func row(_ label: String, _ value: String) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 12) {
            Text(label).foregroundStyle(.muted)
            Spacer(minLength: 0)
            Text(value).lineLimit(4).truncationMode(.tail).multilineTextAlignment(.trailing).textSelection(.enabled)
        }
        .sectionRow()
    }

    /// One line that jumps to someone or something else on the map.
    private func jump(_ node: WorkNode, to id: String, note: String?) -> some View {
        let look = GraphLook.look(node)
        return Button { picked = id } label: {
            HStack(spacing: 10) {
                WorldFace(node: node, symbol: look.symbol, tint: GraphTint.color(node), activity: activity, size: 24)
                Text(node.title).lineLimit(1).truncationMode(.middle)
                Spacer(minLength: 8)
                if let note, !note.isEmpty { Text(note).foregroundStyle(.muted).lineLimit(1) }
                if let color = GraphTint.dot(node.state) { Circle().fill(color == .accentColor ? Color.green : color).frame(width: 7, height: 7) }
            }
            .sectionRow()
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    /// What a station holds: the ones with a picture as a grid of them, and the rest as lines. Each opens on its own.
    @ViewBuilder private func contents(_ items: [WorldItem], title: String) -> some View {
        let pictured = items.filter(\.pictured)
        if !pictured.isEmpty {
            PanelSection(title, count: items.count) {
                LazyVGrid(columns: [GridItem(.flexible(), spacing: 8), GridItem(.flexible(), spacing: 8)], spacing: 8) {
                    ForEach(pictured) { item in
                        Button { picked = item.id } label: {
                            VStack(alignment: .leading, spacing: 4) {
                                Color.primary.opacity(0.06)
                                    .aspectRatio(1.5, contentMode: .fit)
                                    .overlay { previews.image(item.id)?.resizable().aspectRatio(contentMode: .fill) }
                                    .clipShape(.rect(cornerRadius: 8))
                                Text(item.node.title).font(.caption).lineLimit(1).truncationMode(.middle)
                            }
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel(item.node.title)
                    }
                }
                .padding(PanelStyle.rowInset)
                ForEach(items.filter { !$0.pictured }) { item in jump(item.node, to: item.id, note: item.node.subtitle) }
            }
        } else if !items.isEmpty {
            PanelSection(title, count: items.count) {
                ForEach(items) { item in jump(item.node, to: item.id, note: item.node.subtitle) }
            }
        }
    }

    @ViewBuilder private func facts(_ node: WorkNode) -> some View {
        if let details = node.details, !details.isEmpty {
            PanelSection("Details") {
                ForEach(Array(details.enumerated()), id: \.offset) { _, detail in row(detail.label, detail.value) }
            }
        }
    }

    // MARK: Someone

    @ViewBuilder private func sections(_ actor: WorldActor) -> some View {
        if let tag = WorldActorView.tag(actor) {
            PanelSection("Now") {
                Label(tag.text, systemImage: tag.symbol).lineLimit(2).sectionRow()
                if let station = world.station(actor.station) { jump(station.node, to: station.id, note: nil) }
            }
        }
        let passes = world.trails.filter { $0.kind == .handoff && ($0.from == actor.id || $0.to == actor.id) }
        if !passes.isEmpty {
            PanelSection("Handoffs", count: passes.count) {
                ForEach(passes.sorted { $0.active && !$1.active }) { pass in other(pass, for: actor.id) }
            }
        }
        let used = world.trails.filter { $0.kind == .visit && $0.from == actor.id && $0.to != actor.station }.compactMap { world.station($0.to) }
        if !used.isEmpty {
            PanelSection("Also used") {
                ForEach(used) { station in jump(station.node, to: station.id, note: nil) }
            }
        }
        contents(world.stations.flatMap(\.items).filter { $0.owner == actor.id }, title: "Touched")
        facts(actor.node)
    }

    /// The other end of something passed, with which way it went.
    @ViewBuilder private func other(_ pass: WorldTrail, for id: String) -> some View {
        let sent = (pass.wire.direction == WorkWireDirection.back) != (pass.from == id)
        if let other = world.actor(pass.from == id ? pass.to : pass.from) {
            let what = pass.wire.label ?? GraphLook.cargoTitle(pass.wire.carries) ?? "Handoff"
            Button { picked = "trail:" + pass.id } label: {
                HStack(spacing: 10) {
                    WorldFace(node: other.node, symbol: GraphLook.look(other.node).symbol, tint: GraphTint.color(other.node), activity: activity, size: 24)
                    Text(other.node.title).lineLimit(1).truncationMode(.middle)
                    Spacer(minLength: 8)
                    Text(sent ? "\(what) sent" : "\(what) received").foregroundStyle(.muted).lineLimit(1)
                    if pass.active { Circle().fill(GraphTint.cargo(pass.wire.carries)).frame(width: 7, height: 7) }
                }
                .sectionRow()
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
        }
    }

    // MARK: Somewhere

    @ViewBuilder private func sections(_ station: WorldStation) -> some View {
        if let subtitle = station.node.subtitle, !subtitle.isEmpty {
            PanelSection { Text(subtitle).sectionRow() }
        }
        let here = world.present(at: station.id)
        if !here.isEmpty {
            PanelSection("Here now") {
                ForEach(here) { actor in jump(actor.node, to: actor.id, note: actor.doing) }
            }
        }
        // Someone working here by reaching over from elsewhere counts as here too.
        let before = world.visitors(of: station.id).filter { $0.station != station.id }
        if !before.isEmpty {
            PanelSection("Used by") {
                ForEach(before) { actor in jump(actor.node, to: actor.id, note: nil) }
            }
        }
        contents(station.items, title: station.node.kind == WorkKind.step ? "Made here" : "Inside")
        let roads = world.trails.filter { $0.kind == .road && ($0.from == station.id || $0.to == station.id) }
        if !roads.isEmpty {
            PanelSection(station.node.kind == WorkKind.step ? "Order" : "Connected to") {
                ForEach(roads) { road in
                    if let other = world.station(road.from == station.id ? road.to : road.from) {
                        jump(other.node, to: other.id, note: station.node.kind == WorkKind.step ? road.from == station.id ? "Next" : "Before" : nil)
                    }
                }
            }
        }
        if let group = world.groups.first(where: { $0.id == station.group }) {
            PanelSection("In") {
                Button { picked = "group:" + group.id } label: {
                    HStack(spacing: 10) {
                        GraphBadge(symbol: group.symbol, tint: GraphTint.color(group.family), size: 24)
                        Text(group.title).lineLimit(1)
                        Spacer(minLength: 0)
                    }
                    .sectionRow().contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
        }
        facts(station.node)
    }

    // MARK: A district

    @ViewBuilder private func sections(_ group: WorldGroup) -> some View {
        let inside = world.stations.filter { $0.group == group.id }
        if group.ordered {
            let done = inside.filter { $0.state == WorkNodeState.done }.count
            PanelSection("Progress") {
                ProgressView(value: Double(done), total: Double(max(1, inside.count)))
                    .tint(.green).sectionRow()
                    .accessibilityLabel("\(done) of \(inside.count) steps done")
            }
        }
        PanelSection(group.ordered ? "Steps" : "Places", count: inside.count) {
            ForEach(inside) { station in
                let here = world.present(at: station.id).map(\.node.title).joined(separator: ", ")
                jump(station.node, to: station.id, note: here.isEmpty ? GraphTint.status(station.state == WorkNodeState.idle && group.ordered ? nil : station.state) : here)
            }
        }
        let people = world.actors.filter { actor in inside.contains { $0.id == actor.station } }
        if !people.isEmpty {
            PanelSection("Here now") {
                ForEach(people) { actor in jump(actor.node, to: actor.id, note: actor.doing) }
            }
        }
    }

    // MARK: Something passed

    @ViewBuilder private func sections(_ trail: WorldTrail) -> some View {
        let back = trail.wire.direction == WorkWireDirection.back
        PanelSection {
            if let from = world.actor(back ? trail.to : trail.from) { jump(from.node, to: from.id, note: "From") }
            if let to = world.actor(back ? trail.from : trail.to) { jump(to.node, to: to.id, note: "To") }
        }
        PanelSection("Details") {
            if let what = GraphLook.cargoTitle(trail.wire.carries) { row("Carries", what) }
            row("Status", trail.active ? "On its way" : trail.wire.state == WorkWireState.failed ? "Failed" : "Delivered")
            ForEach(Array((trail.wire.details ?? []).enumerated()), id: \.offset) { _, detail in row(detail.label, detail.value) }
        }
    }

    // MARK: Something held

    @ViewBuilder private func sections(_ item: WorldItem, in station: WorldStation) -> some View {
        PanelSection {
            jump(station.node, to: station.id, note: "In")
            if let owner = world.actor(item.owner) { jump(owner.node, to: owner.id, note: "By") }
        }
        if let subtitle = item.node.subtitle, !subtitle.isEmpty {
            PanelSection { Text(subtitle).sectionRow() }
        }
        facts(item.node)
    }
}
