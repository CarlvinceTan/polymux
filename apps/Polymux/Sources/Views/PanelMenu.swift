import SwiftUI

struct PanelMenu<Content: View>: View {
    let title: String
    let symbol: String
    @ViewBuilder let content: () -> Content

    init(_ title: String, symbol: String, @ViewBuilder content: @escaping () -> Content) {
        self.title = title; self.symbol = symbol; self.content = content
    }

    var body: some View {
        #if os(macOS)
        NativeMenu(title: title, symbol: symbol, content: content())
        #else
        Menu(content: content) {
            Label(title, systemImage: symbol).labelStyle(.iconOnly)
                .font(.system(size: 15)).foregroundStyle(.primary)
        }
        .accessibilityLabel(title)
        .menuIndicator(.hidden)
        .buttonStyle(.plain)
        #endif
    }
}

#if os(macOS)
import AppKit

private struct NativeMenu<Content: View>: NSViewRepresentable {
    let title: String
    let symbol: String
    let content: Content

    func makeNSView(context: Context) -> MenuButton {
        let button = MenuButton(frame: .zero)
        button.isBordered = false
        button.imagePosition = .imageOnly
        button.setAccessibilityRole(.menuButton)
        return button
    }

    func updateNSView(_ button: MenuButton, context: Context) {
        let content = AnyView(content.environment(\.self, context.environment))
        let appearance = NSAppearance(named: context.environment.colorScheme == .dark ? .darkAqua : .aqua)
        button.appearance = appearance
        button.image = NSImage(systemSymbolName: symbol, accessibilityDescription: nil)?
            .withSymbolConfiguration(.init(pointSize: 15, weight: .regular))
        button.contentTintColor = .labelColor
        button.isEnabled = context.environment.isEnabled
        button.setAccessibilityLabel(title)
        button.toolTip = title
        button.build = {
            let menu = NSHostingMenu(rootView: content)
            menu.appearance = appearance
            menu.allowsContextMenuPlugIns = false
            menu.update()
            return menu
        }
    }
}

private final class MenuButton: NSButton {
    var build: (() -> NSMenu)?

    override func mouseDown(with event: NSEvent) { openMenu() }
    override func performClick(_ sender: Any?) { openMenu() }
    override func accessibilityPerformPress() -> Bool {
        guard isEnabled else { return false }
        DispatchQueue.main.async { [weak self] in self?.openMenu() }
        return true
    }
    override func accessibilityPerformShowMenu() -> Bool { accessibilityPerformPress() }

    override func keyDown(with event: NSEvent) {
        switch event.keyCode {
        case 36, 49, 76, 125: openMenu()
        default: super.keyDown(with: event)
        }
    }

    private func openMenu() {
        guard isEnabled, let menu = build?() else { return }
        highlight(true)
        defer { highlight(false) }
        let y = isFlipped ? bounds.maxY + PanelStyle.headingInset : bounds.minY - PanelStyle.headingInset
        menu.popUp(positioning: nil, at: NSPoint(x: bounds.maxX - menu.size.width, y: y), in: self)
    }
}
#endif
