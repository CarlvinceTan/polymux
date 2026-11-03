import SwiftUI
import GraphKit
import PolymuxKit

/// One node's card. Everything it shows comes from the graph's data.
struct GraphNodeView: View {
    let shown: ShownNode
    let node: WorkNode
    let selected: Bool
    let activity: ActivityStore
    let select: () -> Void
    let toggle: () -> Void

    private var label: String {
        ([node.title, node.subtitle, GraphTint.status(node.state)] as [String?]).compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: ", ")
    }

    var body: some View {
        Group {
            if shown.isFrame {
                frame
            } else if node.shape == WorkNodeShape.circle {
                circle
            } else {
                card
            }
        }
        .contentShape(Rectangle())
        .onTapGesture(perform: select)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(label)
        .accessibilityAddTraits(selected ? [.isButton, .isSelected] : .isButton)
        .accessibilityAction(.default, select)
        .overlay(alignment: shown.isFrame ? .topTrailing : .bottom) {
            if shown.canExpand { GraphDepthButton(shown: shown, title: node.title, action: toggle) }
        }
    }

    @ViewBuilder private var icon: some View {
        if let agent = node.agentID, !agent.isEmpty {
            AgentAvatar(agentID: agent, activity: activity, showsMarker: false)
                .frame(width: GraphCard.iconSize, height: GraphCard.iconSize)
        } else {
            Image(systemName: node.symbol?.isEmpty == false ? node.symbol ?? "circle" : "circle")
                .font(.system(size: 17))
                .foregroundStyle(GraphTint.color(node.tint))
                .frame(width: GraphCard.iconSize, height: GraphCard.iconSize)
        }
    }

    private var border: some View {
        RoundedRectangle(cornerRadius: 12)
            .strokeBorder(selected ? Color.accentColor : GraphTint.border(node.state), lineWidth: selected ? 2 : 1.5)
    }

    private var card: some View {
        VStack(alignment: .leading, spacing: 6) {
            HStack(spacing: 10) {
                icon
                VStack(alignment: .leading, spacing: 1) {
                    Text(node.title).font(.subheadline.weight(.medium)).lineLimit(1).truncationMode(.tail)
                    if let subtitle = node.subtitle, !subtitle.isEmpty {
                        Text(subtitle).font(.caption).foregroundStyle(.secondary).lineLimit(1).truncationMode(.tail)
                    }
                }
                Spacer(minLength: 0)
            }
            GraphBlocks(blocks: (node.blocks ?? []).filter(GraphCard.known))
        }
        .padding(12)
        .frame(width: shown.box.w, height: shown.box.h, alignment: .leading)
        .background(Color.graphCard, in: RoundedRectangle(cornerRadius: 12))
        .overlay { border }
    }

    private var circle: some View {
        VStack(spacing: 4) {
            icon
            Text(node.title).font(.caption).lineLimit(1).truncationMode(.tail)
        }
        .padding(8)
        .frame(width: shown.box.w, height: shown.box.h)
        .background(Color.graphCard, in: Circle())
        .overlay { Circle().strokeBorder(selected ? Color.accentColor : GraphTint.border(node.state), lineWidth: selected ? 2 : 1.5) }
    }

    private var frame: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 8) {
                Image(systemName: node.symbol?.isEmpty == false ? node.symbol ?? "square" : "square")
                    .font(.system(size: 14)).foregroundStyle(.secondary)
                Text(node.title).font(.subheadline.weight(.semibold)).lineLimit(1).truncationMode(.tail)
                Spacer(minLength: 32)
            }
            .padding(.horizontal, 14).frame(height: 40)
            Spacer(minLength: 0)
        }
        .frame(width: shown.box.w, height: shown.box.h, alignment: .topLeading)
        .background(Color.primary.opacity(0.04), in: RoundedRectangle(cornerRadius: 16))
        .overlay {
            RoundedRectangle(cornerRadius: 16)
                .strokeBorder(selected ? Color.accentColor : Color.primary.opacity(0.2), lineWidth: selected ? 2 : 1)
        }
    }
}

extension Color {
    /// A node's surface, which stays distinct from the canvas in both appearances.
    static var graphCard: Color {
        #if os(macOS)
        Color(nsColor: .controlBackgroundColor)
        #else
        Color(uiColor: .secondarySystemGroupedBackground)
        #endif
    }
}

private struct GraphBlocks: View {
    let blocks: [WorkBlock]

    private var badges: [WorkBlock] { blocks.filter { $0.kind == WorkBlockKind.badge } }

    var body: some View {
        ForEach(Array(blocks.filter { $0.kind != WorkBlockKind.badge }.enumerated()), id: \.offset) { _, block in
            switch block.kind {
            case WorkBlockKind.progress:
                ProgressView(value: min(1, max(0, block.value ?? 0)))
                    .tint(GraphTint.color(block.tint ?? "accent"))
                    .accessibilityLabel(block.text ?? "Progress")
            case WorkBlockKind.status:
                HStack(spacing: 6) {
                    Circle().fill(GraphTint.color(block.tint)).frame(width: 7, height: 7)
                    Text(block.text ?? "").font(.caption).foregroundStyle(.secondary).lineLimit(1)
                }
            default:
                HStack(spacing: 4) {
                    if let symbol = block.symbol, !symbol.isEmpty { Image(systemName: symbol).font(.caption) }
                    Text(block.text ?? "").font(.caption).foregroundStyle(.secondary).lineLimit(1).truncationMode(.tail)
                }
            }
        }
        if !badges.isEmpty {
            HStack(spacing: 4) {
                ForEach(Array(badges.enumerated()), id: \.offset) { _, badge in
                    Text(badge.text ?? "")
                        .font(.caption2.weight(.medium))
                        .foregroundStyle(badge.tint == nil ? Color.secondary : GraphTint.color(badge.tint))
                        .lineLimit(1)
                        .padding(.horizontal, 6).padding(.vertical, 2)
                        .background(Color.primary.opacity(0.06), in: Capsule())
                }
            }
        }
    }
}

/// Opens a node one level deeper, or closes it again.
private struct GraphDepthButton: View {
    let shown: ShownNode
    let title: String
    let action: () -> Void

    private var label: String {
        shown.expanded && shown.hidden == 0 ? "Hide what’s in \(title)" : "Show \(shown.hidden) more in \(title)"
    }

    var body: some View {
        Button(action: action) {
            Group {
                if shown.hidden > 0 {
                    Text("+\(shown.hidden)").font(.caption2.weight(.semibold)).monospacedDigit()
                } else {
                    Image(systemName: "chevron.up").font(.system(size: 9, weight: .semibold))
                }
            }
            .foregroundStyle(Color.primary)
            .frame(minWidth: 24, minHeight: 20)
            .padding(.horizontal, 4)
            .background(Color.graphCard, in: Capsule())
            .overlay { Capsule().strokeBorder(Color.primary.opacity(0.28), lineWidth: 1) }
            #if os(iOS)
            .frame(minWidth: 44, minHeight: 44)
            #endif
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .offset(y: shown.isFrame ? 10 : 10)
        .padding(.trailing, shown.isFrame ? 10 : 0)
        .help(label)
        .accessibilityLabel(label)
    }
}
