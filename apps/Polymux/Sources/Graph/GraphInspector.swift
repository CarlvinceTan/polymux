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
            HStack {
                Text(heading).font(.headline).lineLimit(2)
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

    private func row(_ label: String, _ value: String) -> some View {
        LabeledContent(label) {
            Text(value).lineLimit(4).truncationMode(.tail).multilineTextAlignment(.trailing).textSelection(.enabled)
        }
        .font(.subheadline)
    }

    @ViewBuilder private func nodeRows(_ node: WorkNode) -> some View {
        if let subtitle = node.subtitle, !subtitle.isEmpty {
            Text(subtitle).font(.subheadline).foregroundStyle(.secondary).textSelection(.enabled)
        }
        if let status = GraphTint.status(node.state) { row("Status", status) }
        ForEach(Array((node.details ?? []).enumerated()), id: \.offset) { _, detail in row(detail.label, detail.value) }
        let links = connections(node.id)
        if !links.isEmpty {
            Text("Connections").font(.subheadline.weight(.semibold)).padding(.top, 6)
            ForEach(links) { link in
                let other = link.from == node.id ? link.to : link.from
                Button { model.select(other, adding: false) } label: {
                    HStack {
                        Text(model.title(other)).lineLimit(1).truncationMode(.tail)
                        Spacer(minLength: 8)
                        if let label = link.label { Text(label).foregroundStyle(.secondary).lineLimit(1) }
                    }
                    .font(.subheadline).contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
        }
        if let agent = node.agentID, !agent.isEmpty, let openChat {
            Button("Open Chat") { openChat(agent) }.buttonStyle(.glass).padding(.top, 8)
        }
    }

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
            ForEach(Array((item.details ?? []).enumerated()), id: \.offset) { _, detail in row(detail.label, detail.value) }
        }
    }
}
