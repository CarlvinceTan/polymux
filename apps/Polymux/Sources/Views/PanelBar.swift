#if os(macOS)
import AppKit
import SwiftUI

private struct PanelBarEntry {
    let id: UUID
    let x: CGFloat
    let width: CGFloat
    let revision: AnyHashable
    let interactive: Bool
    let scheme: ColorScheme
    let view: AnyView
}

private struct PanelBars: PreferenceKey {
    static var defaultValue: [PanelBarEntry] { [] }

    static func reduce(value: inout [PanelBarEntry], nextValue: () -> [PanelBarEntry]) {
        value.append(contentsOf: nextValue())
    }
}

extension View {
    func panelBar<Revision: Hashable, Actions: View>(value: Revision, enabled: Bool = true,
                                                    @ViewBuilder actions: @escaping () -> Actions) -> some View {
        modifier(PanelBar(revision: AnyHashable(value), enabled: enabled, actions: actions))
    }

    func panelBarHost() -> some View {
        backgroundPreferenceValue(PanelBars.self) { entries in
            PanelBarHost(entries: entries).frame(width: 0, height: 0).accessibilityHidden(true)
        }
    }
}

private struct PanelBar<Actions: View>: ViewModifier {
    let revision: AnyHashable
    let enabled: Bool
    @ViewBuilder let actions: () -> Actions
    @State private var id = UUID()
    @Environment(\.panelClose) private var close
    @Environment(\.isEnabled) private var interactive
    @Environment(\.colorScheme) private var scheme

    func body(content: Content) -> some View {
        content.background {
            GeometryReader { geometry in
                Color.clear.preference(key: PanelBars.self, value: enabled ? [
                    PanelBarEntry(id: id, x: geometry.frame(in: .global).minX, width: geometry.size.width,
                                  revision: revision, interactive: interactive, scheme: scheme,
                                  view: AnyView(actions().environment(\.panelClose, close)
                                    .environment(\.colorScheme, scheme)
                                    .disabled(!interactive).accessibilityHidden(!interactive)))
                ] : [])
            }
            .allowsHitTesting(false)
            .accessibilityHidden(true)
        }
    }
}

private struct PanelBarHost: NSViewRepresentable {
    let entries: [PanelBarEntry]

    func makeNSView(context: Context) -> PanelBarAnchor { PanelBarAnchor() }
    func updateNSView(_ view: PanelBarAnchor, context: Context) { view.update(entries) }
    static func dismantleNSView(_ view: PanelBarAnchor, coordinator: ()) { view.detach() }
}

final class PanelHosting: NSHostingView<AnyView> {
    override func hitTest(_ point: NSPoint) -> NSView? {
        let local = convert(point, from: superview)
        // A collapsed sidebar leaves this host spanning the native titlebar controls.
        for accessory in window?.titlebarAccessoryViewControllers ?? [] {
            let view = accessory.view
            if !view.isHiddenOrHasHiddenAncestor, view.bounds.contains(view.convert(local, from: self)) {
                return nil
            }
        }
        return super.hitTest(point)
    }
}

private final class PanelBarAnchor: NSView {
    private var hosts: [UUID: NSHostingView<AnyView>] = [:]
    private var entries: [PanelBarEntry] = []

    override func hitTest(_ point: NSPoint) -> NSView? { nil }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        if window == nil { detach() } else { attach() }
    }

    func update(_ entries: [PanelBarEntry]) {
        let previous = self.entries
        self.entries = entries
        guard !entries.isEmpty else { detach(); return }
        for id in hosts.keys where !entries.contains(where: { $0.id == id }) {
            hosts.removeValue(forKey: id)?.removeFromSuperview()
        }
        for entry in entries {
            guard let hosting = hosts[entry.id] else { continue }
            let old = previous.first { $0.id == entry.id }
            if old?.revision != entry.revision || old?.interactive != entry.interactive || old?.scheme != entry.scheme {
                hosting.appearance = NSAppearance(named: entry.scheme == .dark ? .darkAqua : .aqua)
                hosting.rootView = content(entry)
            }
            position(hosting, entry: entry)
        }
        attach()
    }

    private func content(_ entry: PanelBarEntry) -> AnyView {
        AnyView(entry.view.padding(.horizontal, 8).frame(maxWidth: .infinity).frame(height: 36))
    }

    private func attach() {
        guard let parent = window?.contentView?.superview else { return }
        for entry in entries where hosts[entry.id] == nil {
            let hosting = PanelHosting(rootView: content(entry))
            hosting.sizingOptions = []
            hosting.safeAreaRegions = []
            hosting.appearance = NSAppearance(named: entry.scheme == .dark ? .darkAqua : .aqua)
            hosting.autoresizingMask = [.minYMargin]
            hosts[entry.id] = hosting
            // Keep pane controls out of the window's titlebar width constraints.
            parent.addSubview(hosting, positioned: .above, relativeTo: nil)
            position(hosting, entry: entry)
        }
    }

    private func position(_ hosting: NSView, entry: PanelBarEntry) {
        guard let parent = hosting.superview else { return }
        hosting.frame = NSRect(x: entry.x, y: parent.bounds.height - 44, width: entry.width, height: 36)
        hosting.layoutSubtreeIfNeeded()
    }

    func detach() {
        hosts.values.forEach { $0.removeFromSuperview() }
        hosts.removeAll()
    }
}
#endif
