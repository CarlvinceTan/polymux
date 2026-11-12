import SwiftUI

#if os(macOS)
import AppKit

/// Opens the window at the screen's visible size, or at setup's smaller fixed size.
struct WindowFill: NSViewRepresentable {
    var setup = false

    func makeNSView(context: Context) -> WindowFillView {
        let view = WindowFillView()
        view.setup = setup
        return view
    }

    func updateNSView(_ view: WindowFillView, context: Context) {
        view.setup = setup
    }
}

final class WindowFillView: NSView {
    private weak var filled: NSWindow?
    private var mode: Bool?
    private var wasMovable: Bool?
    var setup = false {
        didSet {
            guard mode != setup else { return }
            apply()
        }
    }

    override func hitTest(_ point: NSPoint) -> NSView? { nil }

    override func viewDidMoveToWindow() {
        super.viewDidMoveToWindow()
        guard let window, window !== filled, !window.styleMask.contains(.fullScreen) else { return }
        filled = window
        // SwiftUI sizes the window after the view attaches, so resize once that pass ends.
        DispatchQueue.main.async { [weak self] in self?.apply() }
    }

    private func apply() {
        guard let window = filled ?? window, !window.styleMask.contains(.fullScreen),
              let screen = window.screen ?? NSScreen.main else { return }
        if wasMovable == nil { wasMovable = window.isMovableByWindowBackground }
        mode = setup
        if setup {
            window.isMovableByWindowBackground = true
            let size = NSSize(width: min(840, screen.visibleFrame.width - 40),
                              height: min(620, screen.visibleFrame.height - 40))
            window.contentMinSize = NSSize(width: 720, height: 520)
            window.setContentSize(size)
            window.center()
        } else {
            window.isMovableByWindowBackground = wasMovable ?? false
            window.contentMinSize = NSSize(width: PaneWidth.window, height: 720)
            window.setFrame(screen.visibleFrame, display: false)
        }
    }
}
#endif
