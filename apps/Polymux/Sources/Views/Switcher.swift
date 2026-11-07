import SwiftUI

/// One switcher entry: a server's chats, once there's more than one server.
struct ServerTab: Identifiable, Hashable {
    /// The server's address.
    let id: String
    let name: String
    /// Marks the server where a tab bar shows icons only.
    let mark: String

    /// Each server's initial, or everyone's position when initials clash or one has no symbol.
    static func marks(_ names: [String]) -> [String] {
        let initials = names.compactMap { name in
            name.lowercased().first { $0.isASCII && ($0.isLetter || $0.isNumber) }.map(String.init)
        }
        if initials.count == names.count, Set(initials).count == names.count { return initials }
        return names.indices.map { String($0 + 1) }
    }
}

/// The sidebar's foot on macOS and iPad: one entry per server.
struct ServerSwitcher: View {
    let tabs: [ServerTab]
    @Binding var selection: String
    var unread: [String: Int] = [:]
    @State private var width: CGFloat = 0
    @State private var dragX: CGFloat?
    /// Where a drag across servers will land; moving there rebuilds this view, so it waits for the release.
    @State private var dragTarget: String?
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    private var cell: CGFloat { tabs.isEmpty ? 0 : width / CGFloat(tabs.count) }
    private var index: Int { tabs.firstIndex { $0.id == selection } ?? 0 }

    /// The lens rests on the selected tab, or follows the finger while dragging.
    private var lensX: CGFloat {
        guard let dragX else { return CGFloat(index) * cell }
        return min(max(dragX - cell / 2, 0), max(width - cell, 0))
    }

    var body: some View {
        GlassEffectContainer {
            ZStack(alignment: .leading) {
                Capsule().fill(.clear)
                    .glassEffect(.regular.interactive(), in: Capsule())
                    .frame(width: cell)
                    .scaleEffect(dragX == nil ? 1 : 1.12)
                    .offset(x: lensX)
                    .opacity(width > 0 ? 1 : 0)
                HStack(spacing: 0) {
                    ForEach(tabs) { tab in
                        Button { selection = tab.id } label: {
                            entry(tab)
                                .frame(maxWidth: .infinity, maxHeight: .infinity)
                                .contentShape(Capsule())
                        }
                        .buttonStyle(.plain)
                        .help(Self.title(tab))
                        .accessibilityLabel(Self.title(tab))
                        .accessibilityValue((unread[tab.id] ?? 0) > 0 ? "\(unread[tab.id] ?? 0) unread" : "")
                        .accessibilityAddTraits(tab.id == selection ? .isSelected : [])
                    }
                }
            }
            .onGeometryChange(for: CGFloat.self) { $0.size.width } action: { width = $0 }
            // Grab the lens and slide it to another tab.
            .simultaneousGesture(DragGesture(minimumDistance: 6)
                .onChanged { drag in
                    guard cell > 0 else { return }
                    dragX = drag.location.x
                    dragTarget = tabs[min(max(Int(drag.location.x / cell), 0), tabs.count - 1)].id
                }
                .onEnded { _ in
                    dragX = nil
                    if let dragTarget { selection = dragTarget }
                    dragTarget = nil
                })
            .padding(2)
            .frame(height: ComposerMetrics.control)
            .glassEffect(.regular, in: Capsule())
            .animation(dragX == nil ? Motion.move.unless(reduceMotion) : Motion.instant, value: lensX)
            .animation(Motion.quick.unless(reduceMotion), value: dragX == nil)
        }
        .padding(.horizontal, ComposerMetrics.edge).padding(.bottom, ComposerMetrics.edge)
    }

    /// Each server shows its name, since every server's chats would share an icon.
    private func entry(_ tab: ServerTab) -> some View {
        HStack(spacing: 4) {
            Text(tab.name)
                .font(.subheadline.weight(.medium))
                .foregroundStyle(Color.primary)
                .lineLimit(1)
                .truncationMode(.tail)
            // A dot leaves the name its room; the count stays in the entry's accessibility value.
            if (unread[tab.id] ?? 0) > 0 { Circle().fill(Color("Unread")).frame(width: 7, height: 7) }
        }
        .padding(.horizontal, 8)
    }

    static func title(_ tab: ServerTab) -> String { "Chats on \(tab.name)" }

    static func symbol(_ tab: ServerTab, selected: Bool = false) -> String {
        "\(tab.mark).circle" + (selected ? ".fill" : "")
    }
}

/// Which ends of a vertical scroller have more past them, so a row at rest is never faded.
struct ScrollEdges: Equatable {
    var above = false
    var below = false

    init() {}

    init(_ geometry: ScrollGeometry) {
        let furthest = geometry.contentSize.height + geometry.contentInsets.bottom - geometry.containerSize.height
        above = geometry.contentOffset.y + geometry.contentInsets.top > 1
        below = furthest - geometry.contentOffset.y > 1
    }
}

#if os(macOS)
/// Fades the sidebar's list into its surface where more lies past an end, as the chat does under its header and composer.
struct SidebarFade: View {
    var top: CGFloat = 72
    let bottom: CGFloat
    var edges = ScrollEdges()
    @Environment(\.colorScheme) private var scheme
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    static let plain: CGFloat = 40
    static var switcher: CGFloat { ComposerMetrics.control + ComposerMetrics.edge + 40 }

    static func surface(_ scheme: ColorScheme) -> Color {
        Color(white: scheme == .dark ? 38.0 / 255 : 242.0 / 255)
    }

    var body: some View {
        VStack(spacing: 0) {
            Rectangle().fill(Self.surface(scheme)).frame(height: top)
                .mask {
                    LinearGradient(stops: [.init(color: .black, location: 0),
                                           .init(color: .black.opacity(0.9), location: 0.35),
                                           .init(color: .clear, location: 1)],
                                   startPoint: .top, endPoint: .bottom)
                }
                .opacity(edges.above ? 1 : 0)
            Spacer(minLength: 0)
            Rectangle().fill(Self.surface(scheme)).frame(height: bottom)
                .mask {
                    LinearGradient(colors: [.clear, .black.opacity(0.9), .black], startPoint: .top, endPoint: .bottom)
                }
                .opacity(edges.below ? 1 : 0)
        }
        .animation(Motion.quick.unless(reduceMotion), value: edges)
        .ignoresSafeArea(.container, edges: [.top, .bottom])
        .allowsHitTesting(false)
        .accessibilityHidden(true)
    }
}

private struct SidebarFading: ViewModifier {
    let top: CGFloat
    let bottom: CGFloat
    @State private var edges = ScrollEdges()

    func body(content: Content) -> some View {
        content
            .onScrollGeometryChange(for: ScrollEdges.self) { ScrollEdges($0) } action: { _, value in edges = value }
            .overlay { SidebarFade(top: top, bottom: bottom, edges: edges) }
    }
}

extension View {
    func sidebarFade(top: CGFloat = 72, bottom: CGFloat) -> some View {
        modifier(SidebarFading(top: top, bottom: bottom))
    }
}
#endif

/// A count of unread messages, capped so it stays one short pill.
struct UnreadPill: View {
    let count: Int
    var small = false

    var body: some View {
        Text(count > 99 ? "99+" : String(count))
            .font(.system(size: small ? 10 : 11, weight: .semibold)).monospacedDigit()
            .foregroundStyle(Color("OnAccent"))
            .padding(.horizontal, small ? 4 : 5)
            .frame(minWidth: small ? 14 : 18, minHeight: small ? 14 : 18)
            .background(Color("Unread"), in: Capsule())
            .lineLimit(1)
            .layoutPriority(1)
            .accessibilityLabel("\(count) unread")
    }
}
