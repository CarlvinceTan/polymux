import SwiftUI
import GraphKit
import PolymuxKit

/// What's known about the selected node or wire. It reads; it never changes the work.
struct GraphInspector: View {
    let model: GraphModel
    var openChat: ((String) -> Void)?

    private var node: WorkNode? { model.selected.first.flatMap { model.nodes[$0] } }
    private var wire: ShownWire? { model.scene.wires.first { $0.id == model.selectedWire } }

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 10) {
                if let node { GraphBadge(symbol: GraphLook.look(node).symbol, tint: GraphTint.color(node)) }
                VStack(alignment: .leading, spacing: 1) {
                    Text(heading).font(.headline).lineLimit(2)
                    if !caption.isEmpty { Text(caption).font(.caption).foregroundStyle(.secondary).lineLimit(1) }
                }
                Spacer(minLength: 8)
                PanelNavButton(symbol: "xmark", label: "Close details") { model.clearSelection() }
                    .keyboardShortcut(.cancelAction)
            }
            .padding(.horizontal, 16).padding(.vertical, 10)
            ScrollView {
                VStack(alignment: .leading, spacing: 8) {
                    if let node { nodeRows(node) }
                    if let wire { wireRows(wire) }
                }
                .frame(maxWidth: .infinity, alignment: .leading)
                .padding(.horizontal, 16).padding(.bottom, 16)
            }
        }
    }

    private var heading: String {
        if let node { return node.title }
        guard let wire else { return "" }
        return wire.wires.count > 1 ? "\(wire.wires.count) links" : wire.label ?? "Link"
    }

    /// What kind of thing this is and how it's doing, under its name.
    private var caption: String {
        guard let node else { return "" }
        let kind = GraphLook.look(node).title
        return ([kind.isEmpty ? nil : kind, GraphTint.status(node.state)] as [String?]).compactMap { $0 }.joined(separator: " · ")
    }

    private func row(_ label: String, _ value: String) -> some View {
        LabeledContent(label) {
            Text(value).lineLimit(4).truncationMode(.tail).multilineTextAlignment(.trailing).textSelection(.enabled)
        }
        .font(.subheadline)
    }

    private func section(_ title: String) -> some View {
        Text(title).font(.subheadline.weight(.semibold)).padding(.top, 8)
    }

    /// One line that jumps to another node: its icon, its name and a note on the right.
    private func jump(_ id: String, symbol: String, tint: Color, note: String?) -> some View {
        Button { model.select(id, adding: false) } label: {
            HStack(spacing: 8) {
                Image(systemName: symbol).font(.system(size: 12, weight: .medium)).foregroundStyle(tint).frame(width: 18)
                Text(model.title(id)).lineLimit(1).truncationMode(.middle)
                Spacer(minLength: 8)
                if let note, !note.isEmpty { Text(note).foregroundStyle(.secondary).lineLimit(1) }
            }
            .font(.subheadline).contentShape(Rectangle())
        }
        .buttonStyle(.plain)
    }

    @ViewBuilder private func nodeRows(_ node: WorkNode) -> some View {
        if let subtitle = node.subtitle, !subtitle.isEmpty {
            Text(subtitle).font(.subheadline).foregroundStyle(.secondary).textSelection(.enabled)
        }
        ForEach(Array((node.details ?? []).enumerated()), id: \.offset) { _, detail in row(detail.label, detail.value) }
        let links = connections(node.id)
        let moving = links.filter { model.cargo[$0.id]?.isEmpty == false }
        if !moving.isEmpty {
            section("Moving now")
            ForEach(moving) { link in
                let other = link.from == node.id ? link.to : link.from
                let kind = model.cargo[link.id]?.first?.kind
                jump(other, symbol: GraphLook.cargo(kind), tint: .accentColor, note: GraphLook.cargoTitle(kind))
            }
        }
        let rest = links.filter { model.cargo[$0.id]?.isEmpty != false }
        if !rest.isEmpty {
            section("Connections")
            ForEach(rest) { link in
                let other = link.from == node.id ? link.to : link.from
                jump(other, symbol: symbol(other), tint: tint(other), note: link.label)
            }
        }
        let inside = model.graph.nodes.filter { $0.parent == node.id }
        if !inside.isEmpty {
            section("Inside")
            ForEach(inside, id: \.id) { child in
                jump(child.id, symbol: GraphLook.look(child).symbol, tint: GraphTint.color(child), note: child.subtitle)
            }
        }
        if let agent = node.agentID, !agent.isEmpty, let openChat {
            Button("Open Chat") { openChat(agent) }.buttonStyle(.glass).padding(.top, 8)
        }
    }

    private func symbol(_ id: String) -> String { model.nodes[id].map { GraphLook.look($0).symbol } ?? "circle" }
    private func tint(_ id: String) -> Color { model.nodes[id].map(GraphTint.color) ?? .secondary }

    private func connections(_ id: String) -> [ShownWire] {
        model.scene.wires.filter { ($0.from == id || $0.to == id) && !$0.isLink }
    }

    @ViewBuilder private func wireRows(_ wire: ShownWire) -> some View {
        row("From", model.title(wire.direction == .back ? wire.to : wire.from))
        row("To", model.title(wire.direction == .back ? wire.from : wire.to))
        if wire.state == .active { row("Status", "Active") }
        if wire.state == .failed { row("Status", "Failed") }
        ForEach(model.wireDetails(wire), id: \.id) { item in
            if wire.wires.count > 1, let label = item.label, !label.isEmpty { Text(label).font(.subheadline.weight(.semibold)).padding(.top, 4) }
            if let cargo = GraphLook.cargoTitle(item.carries) { row("Carries", cargo) }
            ForEach(Array((item.details ?? []).enumerated()), id: \.offset) { _, detail in row(detail.label, detail.value) }
        }
    }
}

/// A kind's icon on its colour, as places wear it on the graph.
struct GraphBadge: View {
    let symbol: String
    let tint: Color
    var size: CGFloat = 32

    var body: some View {
        Image(systemName: symbol)
            .font(.system(size: size * 0.44, weight: .medium)).foregroundStyle(.white)
            .frame(width: size, height: size)
            .background(tint, in: RoundedRectangle(cornerRadius: size * 0.28))
            .accessibilityHidden(true)
    }
}
