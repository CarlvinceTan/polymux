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

    static func border(_ state: String?) -> Color {
        switch state {
        case WorkNodeState.working: .accentColor
        case WorkNodeState.waiting: .orange
        case WorkNodeState.failed: .red
        default: Color.primary.opacity(0.45)
        }
    }

    static func wire(_ state: WireState, selected: Bool) -> Color {
        if selected { return .primary }
        return switch state {
        case .active: .accentColor
        case .failed: .red
        case .idle: Color.primary.opacity(0.5)
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

/// A node's size from what it holds, snapped so ports stay on the grid.
enum GraphCard {
    static let iconSize = 32.0
    private static let padding = 12.0
    private static let spacing = 10.0
    private static let lineHeight = 18.0
    private static let characterWidth = 7.2
    private static let snap = 48.0

    static func size(_ node: WorkNode) -> GraphKit.Size {
        if node.shape == WorkNodeShape.pill { return GraphKit.Size(w: width(node, chrome: padding * 2), h: 48) }
        if node.shape == WorkNodeShape.circle { return GraphKit.Size(w: 96, h: 96) }
        let blocks = (node.blocks ?? []).filter(known)
        let badges = blocks.contains { $0.kind == WorkBlockKind.badge } ? 1 : 0
        let lines = blocks.filter { $0.kind != WorkBlockKind.badge }.count + badges
        let subtitle = node.subtitle?.isEmpty == false ? lineHeight : 0
        let body = max(iconSize, lineHeight + subtitle)
        let height = padding * 2 + body + Double(lines) * (lineHeight + 6)
        return GraphKit.Size(w: Metrics.nodeW, h: max(Metrics.nodeH, (height / 24).rounded(.up) * 24))
    }

    /// An older app skips kinds it doesn't draw, so they take no room.
    static func known(_ block: WorkBlock) -> Bool {
        [WorkBlockKind.text, WorkBlockKind.badge, WorkBlockKind.status, WorkBlockKind.progress].contains(block.kind)
    }

    private static func width(_ node: WorkNode, chrome: Double) -> Double {
        let longest = max(node.title.utf16.count, min(node.subtitle?.utf16.count ?? 0, 24))
        let wanted = Double(longest) * characterWidth + chrome
        return min(Metrics.nodeMaxW - 192, max(192, (wanted / snap).rounded(.up) * snap))
    }
}
