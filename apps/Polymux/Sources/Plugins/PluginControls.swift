import SwiftUI
import PolymuxKit

extension Plugin {
    /// Filled while selected, unfilled otherwise.
    func icon(selected: Bool = false) -> String {
        let name = selected ? selectedSymbol : symbol
        return name.isEmpty ? "puzzlepiece.extension" + (selected ? ".fill" : "") : name
    }
}

/// A Scoped plugin's button. Only the icon comes from the plugin.
private struct PluginButton: View {
    let plugin: Plugin
    let selected: Bool
    let action: () -> Void

    var body: some View {
        Button(action: action) {
            Image(systemName: plugin.icon(selected: selected))
                .font(.system(size: 15))
                .foregroundStyle(selected ? Color.accentColor : Color.primary)
                .frame(width: 36, height: 36)
                .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .help(plugin.title)
        .accessibilityLabel(plugin.title)
        .accessibilityAddTraits(selected ? .isSelected : [])
    }
}

/// Scoped buttons in their shared layout. A group of two or more merges into one piece of glass.
struct PluginButtons: View {
    let groups: [[Plugin]]
    var selected: String?
    let open: (Plugin) -> Void
    @Namespace private var glass

    var body: some View {
        GlassEffectContainer(spacing: 8) {
            HStack(spacing: 8) {
                ForEach(groups, id: \.first?.name) { group in
                    HStack(spacing: 0) {
                        ForEach(group, id: \.name) { plugin in
                            PluginButton(plugin: plugin, selected: plugin.name == selected) { open(plugin) }
                                .glassEffect(.regular.interactive(), in: Capsule())
                                .glassEffectUnion(id: group.first?.name, namespace: glass)
                        }
                    }
                }
            }
        }
    }
}

/// On iPhone every Scoped button collapses into one menu.
struct PluginMenu: View {
    let groups: [[Plugin]]
    let open: (Plugin) -> Void

    var body: some View {
        Menu("Plugins", systemImage: "ellipsis") {
            ForEach(groups.flatMap { $0 }, id: \.name) { plugin in
                Button(plugin.title, systemImage: plugin.icon()) { open(plugin) }
            }
        }
    }
}

/// The side panel for a Scoped plugin. The close button belongs to the panel; a web plugin fills
/// everything else, and may hide the button.
struct PluginPanel: View {
    let plugin: Plugin
    let thread: PolymuxKit.Thread
    let session: AppSession
    let close: () -> Void

    var body: some View {
        PluginSurface.panel(plugin, thread: thread, session: session, close: close)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
            #if os(macOS)
            .padding(.top, plugin.surface == PluginSurface.web ? 0 : 52)
            .ignoresSafeArea(.container, edges: plugin.surface == PluginSurface.web ? .top : [])
            .panelBar(value: plugin.name, enabled: plugin.hideClose != true) {
                HStack {
                    Spacer(minLength: 0)
                    PanelNavButton(symbol: "xmark", label: "Close side panel", action: close)
                }
            }
            #else
            .ignoresSafeArea(.container, edges: plugin.surface == PluginSurface.web ? .bottom : [])
            .navigationTitle(plugin.title)
            .navigationBarTitleDisplayMode(.inline)
            #endif
    }
}

/// Icons only: the sidebar's foot on macOS and iPad.
struct PluginSwitcher: View {
    let tabs: [PluginTab]
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
                    let tab = tabs[min(max(Int(drag.location.x / cell), 0), tabs.count - 1)]
                    if tab.server == nil { selection = tab.id } else { dragTarget = tab.id }
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

    /// Two servers' Chats share an icon, so each shows its server's name instead.
    @ViewBuilder private func entry(_ tab: PluginTab) -> some View {
        let count = unread[tab.id] ?? 0
        if tab.plugin == nil, let name = tab.serverName {
            HStack(spacing: 4) {
                Text(name)
                    .font(.subheadline.weight(.medium))
                    .foregroundStyle(Color.primary)
                    .lineLimit(1)
                    .truncationMode(.tail)
                // A dot leaves the name its room; the count stays in the entry's accessibility value.
                if count > 0 { Circle().fill(Color("Unread")).frame(width: 7, height: 7) }
            }
            .padding(.horizontal, 8)
        } else {
            Image(systemName: Self.symbol(tab, selected: tab.id == selection))
                .font(.system(size: 17))
                .foregroundStyle(Color.primary)
                .frame(width: 24, height: 20)
                // Overlaps only the icon's corner, clear of the lens's top edge.
                .overlay(alignment: .topLeading) {
                    if count > 0 { UnreadPill(count: count, small: true).offset(x: 19, y: -3) }
                }
        }
    }

    static func title(_ tab: PluginTab) -> String {
        let title = tab.plugin?.title ?? "Chats"
        return tab.serverName.map { "\(title) on \($0)" } ?? title
    }

    static func symbol(_ tab: PluginTab, selected: Bool = false) -> String {
        if let plugin = tab.plugin { return plugin.icon(selected: selected) }
        let base = tab.serverMark.map { "\($0).circle" } ?? "bubble.left.and.bubble.right"
        return base + (selected ? ".fill" : "")
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
