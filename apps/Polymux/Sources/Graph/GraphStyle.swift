import SwiftUI
import GraphKit
import PolymuxKit

/// The graph's named tints, so data can pick a colour without leaving the app's palette.
enum GraphTint {
    static func color(_ name: String?) -> Color {
        switch name {
        case "accent": .accentColor
        case "green": .green
        case "orange": .orange
        case "red": .red
        case "purple": .purple
        case "blue": .blue
        default: .secondary
        }
    }

    static func color(_ family: WorkFamily) -> Color {
        switch family {
        case .people, .devices: .gray
        case .bots: .accentColor
        case .agents: .purple
        case .apps: .blue
        case .files: .brown
        case .media: .pink
        case .data: .teal
        case .code: .indigo
        case .tasks: .orange
        }
    }

    /// A node's own tint when it names one, or its family's.
    static func color(_ node: WorkNode) -> Color {
        if let tint = node.tint, !tint.isEmpty, tint != WorkNodeTint.neutral { return color(tint) }
        return color(GraphLook.look(node).family)
    }

    static func border(_ state: String?) -> Color {
        switch state {
        case WorkNodeState.working: .accentColor
        case WorkNodeState.waiting: .orange
        case WorkNodeState.failed: .red
        default: Color.primary.opacity(0.16)
        }
    }

    /// The dot a node wears while something is happening to it.
    static func dot(_ state: String?) -> Color? {
        switch state {
        case WorkNodeState.working: .accentColor
        case WorkNodeState.waiting: .orange
        case WorkNodeState.failed: .red
        default: nil
        }
    }

    static func wire(_ state: WireState, selected: Bool) -> Color {
        if selected { return .primary }
        return switch state {
        case .active: .accentColor
        case .failed: .red
        case .idle: Color.primary.opacity(0.42)
        }
    }

    static func status(_ state: String?) -> String? {
        switch state {
        case WorkNodeState.working: "Working"
        case WorkNodeState.waiting: "Waiting"
        case WorkNodeState.done: "Done"
        case WorkNodeState.failed: "Failed"
        case WorkNodeState.idle: "Idle"
        default: nil
        }
    }
}

/// How one node draws: its silhouette, icon and family, from its kind unless the report says otherwise.
enum GraphLook {
    static func look(_ node: WorkNode) -> WorkLook { WorkKind.look(of: node) }

    /// What a wire carries, drawn on its parcel. Words the app doesn't know still get a parcel.
    static func cargo(_ kind: String?) -> String {
        guard let kind, !kind.isEmpty else { return "circle.fill" }
        return WorkKind.known(kind) ? WorkKind.look(kind).symbol : "shippingbox"
    }

    static func cargoTitle(_ kind: String?) -> String? {
        guard let kind, !kind.isEmpty else { return nil }
        return WorkKind.known(kind) ? WorkKind.look(kind).title : kind.prefix(1).uppercased() + kind.dropFirst()
    }
}

/// A node's size from its silhouette and what it holds.
enum GraphCard {
    static let avatarSize = 56.0
    static let iconSize = 36.0
    static let actor = GraphKit.Size(w: 136, h: 120)
    private static let placeWidth = 216.0
    private static let thingWidth = 200.0
    private static let lineHeight = 18.0

    static func size(_ node: WorkNode) -> GraphKit.Size {
        switch GraphLook.look(node).shape {
        case .actor: return actor
        case .thing: return GraphKit.Size(w: thingWidth, h: 40)
        case .place:
            let blocks = (node.blocks ?? []).filter(known)
            let badges = blocks.contains { $0.kind == WorkBlockKind.badge } ? 1 : 0
            let lines = blocks.filter { $0.kind != WorkBlockKind.badge }.count + badges
            return GraphKit.Size(w: placeWidth, h: 60 + Double(lines) * (lineHeight + 4))
        }
    }

    /// An older app skips kinds it doesn't draw, so they take no room.
    static func known(_ block: WorkBlock) -> Bool {
        [WorkBlockKind.text, WorkBlockKind.badge, WorkBlockKind.status, WorkBlockKind.progress].contains(block.kind)
    }
}
