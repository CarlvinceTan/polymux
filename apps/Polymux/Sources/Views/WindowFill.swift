import SwiftUI

#if os(macOS)
import AppKit

/// Opens the window at the screen's visible size, leaving the menu bar and Dock visible.
struct WindowFill: NSViewRepresentable {
    func makeNSView(context: Context) -> WindowFillView { WindowFillView() }
    func updateNSView(_ view: WindowFillView, context: Context) {}
}

final class WindowFillView: NSView {
    private weak var filled: NSWindow?

    override func hitTest(_ point: NSPoint) -> NSView? { nil }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        guard let window, window !== filled, !window.styleMask.contains(.fullScreen) else { return }
        filled = window
        // SwiftUI sizes the window after the view attaches, so resize once that pass ends.
        DispatchQueue.main.async { [weak window] in
            guard let window, !window.styleMask.contains(.fullScreen),
                  let screen = window.screen ?? NSScreen.main else { return }
            window.setFrame(screen.visibleFrame, display: false)
        }
    }
}
#endif
