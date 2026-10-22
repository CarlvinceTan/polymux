import SwiftUI

extension View {
    func appMenu<Menu: View>(@ViewBuilder content: () -> Menu) -> some View {
        #if os(macOS)
        overlay { ContextMenu(content: content()).accessibilityHidden(true) }
            .accessibilityActions { content() }
        #else
        contextMenu(menuItems: content)
        #endif
    }
}

#if os(macOS)
import AppKit

private struct ContextMenu<Content: View>: NSViewRepresentable {
    let content: Content

    func makeNSView(context: Context) -> ContextView {
        ContextView()
    }

    func updateNSView(_ view: ContextView, context: Context) {
        let content = AnyView(content.environment(\.self, context.environment))
        view.build = {
            let menu = NSHostingMenu(rootView: content)
            menu.allowsContextMenuPlugIns = false
            return menu
        }
        view.enabled = context.environment.isEnabled
    }
}

private final class ContextView: NSView {
    var build: (() -> NSMenu)?
    var enabled = true
    override var acceptsFirstResponder: Bool { false }

    override func hitTest(_ point: NSPoint) -> NSView? {
        guard enabled, let event = NSApp.currentEvent,
              event.type == .rightMouseDown || (event.type == .leftMouseDown && event.modifierFlags.contains(.control)) else { return nil }
        if let content = window?.contentView, containsMenu(in: content, at: convert(point, to: nil)) { return nil }
        return super.hitTest(point)
    }

    private func containsMenu(in view: NSView, at point: NSPoint) -> Bool {
        for child in view.subviews where !child.isHidden {
            let active = (child as? ContextView)?.enabled ?? (child as? MenuAnchor)?.enabled ?? false
            if child !== self, active, child.bounds.width * child.bounds.height < bounds.width * bounds.height,
               child.bounds.contains(child.convert(point, from: nil)) { return true }
            if containsMenu(in: child, at: point) { return true }
        }
        return false
    }

    override func rightMouseDown(with event: NSEvent) { showMenu(event) }
    override func mouseDown(with event: NSEvent) { showMenu(event) }

    override func menu(for event: NSEvent) -> NSMenu? { enabled ? build?() : nil }

    private func showMenu(_ event: NSEvent) {
        guard let menu = menu(for: event) else { return }
        NSMenu.popUpContextMenu(menu, with: event, for: self)
    }
}
#endif
