import SwiftUI
import GraphKit
import PolymuxKit

/// One node. Who does the work stands on a pad, where it happens is a tile, and what it touches is a chip.
struct GraphNodeView: View {
    let shown: ShownNode
    let node: WorkNode
    let selected: Bool
    let activity: ActivityStore
    let select: () -> Void
    let toggle: () -> Void

    private var look: WorkLook { GraphLook.look(node) }
    private var tint: Color { GraphTint.color(node) }

    private var label: String {
        ([node.title, node.subtitle, GraphTint.status(node.state)] as [String?]).compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: ", ")
    }

    var body: some View {
        Group {
            if shown.isFrame {
                frame
            } else {
                switch look.shape {
                case .actor: actor
                case .place: place
                case .thing: thing
                }
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

    private func outline(_ radius: CGFloat) -> some View {
        RoundedRectangle(cornerRadius: radius)
            .strokeBorder(selected ? Color.accentColor : GraphTint.border(node.state), lineWidth: selected ? 2 : 1)
    }

    /// The dot that says something is happening here, without recolouring the whole node.
    @ViewBuilder private var stateDot: some View {
        if let color = GraphTint.dot(node.state) {
            Circle().fill(color).frame(width: 8, height: 8)
                .overlay { Circle().strokeBorder(Color.graphCard, lineWidth: 1.5).padding(-1.5) }
        }
    }

    // MARK: Actor

    @ViewBuilder private var figure: some View {
        if let agent = node.agentID, !agent.isEmpty {
            AgentAvatar(agentID: agent, activity: activity, showsMarker: false)
        } else {
            Circle().fill(tint.opacity(0.14))
                .overlay { Image(systemName: look.symbol).font(.system(size: 24)).foregroundStyle(tint) }
        }
    }

    /// The outside agent a bot runs on, worn as a small mark on its avatar.
    private var engine: WorkBlock? {
        node.blocks?.first { $0.kind == WorkBlockKind.badge && $0.symbol?.isEmpty == false }
    }

    private var actor: some View {
        VStack(spacing: 2) {
            figure
                .frame(width: GraphCard.avatarSize, height: GraphCard.avatarSize)
                .overlay(alignment: .bottomTrailing) {
                    if let engine {
                        Image(systemName: engine.symbol ?? "sparkle")
                            .font(.system(size: 10, weight: .semibold)).foregroundStyle(.white)
                            .frame(width: 20, height: 20)
                            .background(GraphTint.color(.agents), in: Circle())
                            .overlay { Circle().strokeBorder(Color.graphCard, lineWidth: 2).padding(-1) }
                            .offset(x: 4, y: 2)
                    }
                }
                .padding(.bottom, 4)
            Text(node.title).font(.subheadline.weight(.medium)).lineLimit(1).truncationMode(.tail)
            if let subtitle = node.subtitle, !subtitle.isEmpty {
                Text(subtitle).font(.caption2).foregroundStyle(.secondary).lineLimit(1).truncationMode(.tail)
            }
        }
        .padding(.horizontal, 10)
        .frame(width: shown.box.w, height: shown.box.h)
        .background(Color.graphCard, in: RoundedRectangle(cornerRadius: 28))
        .overlay { outline(28) }
        .overlay(alignment: .topTrailing) { stateDot.padding(12) }
    }

    // MARK: Place

    private var place: some View {
        VStack(alignment: .leading, spacing: 4) {
            HStack(spacing: 10) {
                GraphBadge(symbol: look.symbol, tint: tint, size: GraphCard.iconSize)
                VStack(alignment: .leading, spacing: 1) {
                    Text(node.title).font(.subheadline.weight(.medium)).lineLimit(1).truncationMode(.tail)
                    if let subtitle = node.subtitle, !subtitle.isEmpty {
                        Text(subtitle).font(.caption2).foregroundStyle(.secondary).lineLimit(1).truncationMode(.tail)
                    }
                }
                Spacer(minLength: 0)
                stateDot
            }
            GraphBlocks(blocks: (node.blocks ?? []).filter(GraphCard.known))
        }
        .padding(.horizontal, 12)
        .frame(width: shown.box.w, height: shown.box.h, alignment: .leading)
        .background(Color.graphCard, in: RoundedRectangle(cornerRadius: 16))
        .overlay { outline(16) }
    }

    // MARK: Thing

    private var thing: some View {
        HStack(spacing: 8) {
            Image(systemName: look.symbol)
                .font(.system(size: 13, weight: .medium)).foregroundStyle(tint)
                .frame(width: 20)
            Text(node.title).font(.caption.weight(.medium)).lineLimit(1).truncationMode(.middle)
            Spacer(minLength: 4)
            if let subtitle = node.subtitle, !subtitle.isEmpty {
                Text(subtitle).font(.caption2).foregroundStyle(.secondary).lineLimit(1).layoutPriority(-1)
            }
            stateDot
        }
        .padding(.horizontal, 10)
        .frame(width: shown.box.w, height: shown.box.h, alignment: .leading)
        .background(Color.graphCard, in: RoundedRectangle(cornerRadius: 12))
        .overlay { outline(12) }
    }

    // MARK: Zone

    private var frame: some View {
        VStack(alignment: .leading, spacing: 0) {
            HStack(spacing: 8) {
                Image(systemName: look.symbol).font(.system(size: 13, weight: .medium)).foregroundStyle(tint)
                Text(node.title).font(.subheadline.weight(.semibold)).lineLimit(1).truncationMode(.tail)
                Spacer(minLength: 32)
            }
            .padding(.horizontal, 14).frame(height: 40)
            Spacer(minLength: 0)
        }
        .frame(width: shown.box.w, height: shown.box.h, alignment: .topLeading)
        .background(tint.opacity(0.07), in: RoundedRectangle(cornerRadius: 22))
        .overlay {
            RoundedRectangle(cornerRadius: 22)
                .strokeBorder(selected ? Color.accentColor : tint.opacity(0.22), lineWidth: selected ? 2 : 1)
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
        .offset(y: 10)
        .padding(.trailing, shown.isFrame ? 10 : 0)
        .help(label)
        .accessibilityLabel(label)
    }
}
