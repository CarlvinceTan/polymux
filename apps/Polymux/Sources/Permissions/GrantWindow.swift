#if os(macOS)
import AppKit
import SwiftUI

/// The Grant Permissions window. While System Settings is open it docks beside it, and a hint over
/// System Settings' list shows where to drag the icon.
@MainActor
final class GrantWindow: NSObject, NSWindowDelegate {
    static let shared = GrantWindow()
    private var window: NSPanel?
    private var hint: NSPanel?
    private var following: Task<Void, Never>?
    private var checking: Task<Void, Never>?

    func show() {
        let window = window ?? make()
        self.window = window
        if !window.isVisible { window.center() }
        window.makeKeyAndOrderFront(nil)
        NSApp.activate()
        // Picks up a grant made in System Settings while the window is open.
        checking?.cancel()
        checking = Task {
            while !Task.isCancelled {
                await Permissions.shared.refresh()
                try? await Task.sleep(for: .seconds(1))
            }
        }
    }

    /// Docks beside System Settings until it closes.
    func follow() {
        following?.cancel()
        window?.hidesOnDeactivate = false
        following = Task { [weak self] in
            var seen = false
            let deadline = Date.now.addingTimeInterval(10)
            while !Task.isCancelled {
                let running = SystemSettings.running
                if running { seen = true }
                // Ends once System Settings quits, or if it never opened.
                if seen ? !running : Date.now > deadline { break }
                self?.place(SystemSettings.frame())
                try? await Task.sleep(for: .milliseconds(100))
            }
            if !Task.isCancelled { self?.stop() }
        }
    }

    func windowWillClose(_ notification: Notification) {
        checking?.cancel()
        checking = nil
        stop()
    }

    private func stop() {
        following?.cancel()
        following = nil
        hint?.orderOut(nil)
        window?.hidesOnDeactivate = true
        Permissions.shared.settle()
    }

    /// Shows the window beside System Settings and the hint over its list, while either app is in front.
    private func place(_ settings: CGRect?) {
        guard let window, let settings, let screen = NSScreen.screens.first else {
            hint?.orderOut(nil)
            return
        }
        let front = NSApp.isActive || NSWorkspace.shared.frontmostApplication?.bundleIdentifier == SystemSettings.id
        guard front else {
            window.orderOut(nil)
            hint?.orderOut(nil)
            return
        }
        // Window frames use a bottom-left origin; System Settings' comes with a top-left one.
        let top = screen.frame.height - settings.minY
        let size = window.frame.size
        var x = settings.minX - 16 - size.width
        if x < (window.screen ?? screen).visibleFrame.minX { x = settings.maxX + 16 }
        let origin = NSPoint(x: x, y: top - 16 - size.height)
        if window.frame.origin != origin { window.setFrameOrigin(origin) }
        // A window hidden when Polymux went to the background still counts as visible.
        if !window.occlusionState.contains(.visible) { window.orderFrontRegardless() }

        guard Permissions.shared.opened != nil else {
            hint?.orderOut(nil)
            return
        }
        let hint = hint ?? makeHint()
        self.hint = hint
        // Ends over the middle of the list, past System Settings' 232-point sidebar.
        let end = CGPoint(x: settings.minX + (settings.width + 232) / 2, y: settings.minY + 195)
        let corner = NSPoint(x: end.x - DragHint.end.x, y: top - (end.y - settings.minY) - (DragHint.size.height - DragHint.end.y))
        if hint.frame.origin != corner { hint.setFrameOrigin(corner) }
        if !hint.isVisible { hint.orderFrontRegardless() }
    }

    private func make() -> NSPanel {
        let view = GrantView { [weak self] in self?.window?.close() }
        let host = NSHostingView(rootView: view)
        host.sizingOptions = [.preferredContentSize]
        // Laid out at its real size from the start, so it centres and its text wraps.
        let size = NSHostingController(rootView: view).sizeThatFits(in: CGSize(width: 500, height: 2000))
        // Borderless with its own rounded background, so no title bar shifts or pads the layout.
        let panel = GrantPanel(contentRect: NSRect(origin: .zero, size: size),
                               styleMask: [.borderless], backing: .buffered, defer: true)
        panel.title = "Grant Permissions for Polymux"
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = true
        panel.isMovableByWindowBackground = true
        panel.isFloatingPanel = true
        panel.hidesOnDeactivate = true
        panel.isReleasedWhenClosed = false
        panel.delegate = self
        panel.contentView = host
        return panel
    }

    private func makeHint() -> NSPanel {
        let panel = NSPanel(contentRect: NSRect(origin: .zero, size: DragHint.size),
                            styleMask: [.borderless, .nonactivatingPanel], backing: .buffered, defer: true)
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = false
        panel.ignoresMouseEvents = true
        panel.level = NSWindow.Level(NSWindow.Level.floating.rawValue + 1)
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .transient, .ignoresCycle]
        panel.isReleasedWhenClosed = false
        panel.contentView = NSHostingView(rootView: DragHint())
        return panel
    }
}

/// A borderless panel that still takes clicks and key focus like a titled one.
private final class GrantPanel: NSPanel {
    override var canBecomeKey: Bool { true }
}

/// System Settings, which Polymux can only watch from outside.
enum SystemSettings {
    static let id = "com.apple.systempreferences"

    static var running: Bool { !NSRunningApplication.runningApplications(withBundleIdentifier: id).isEmpty }

    /// Its front window's frame, with the origin at the top left of the main screen.
    static func frame() -> CGRect? {
        guard let pid = NSRunningApplication.runningApplications(withBundleIdentifier: id).first?.processIdentifier,
              let windows = CGWindowListCopyWindowInfo([.optionOnScreenOnly, .excludeDesktopElements], kCGNullWindowID)
                as? [[String: Any]] else { return nil }
        return windows.lazy
            .filter { $0[kCGWindowOwnerPID as String] as? pid_t == pid && $0[kCGWindowLayer as String] as? Int == 0 }
            .compactMap { ($0[kCGWindowBounds as String] as? NSDictionary).flatMap { CGRect(dictionaryRepresentation: $0) } }
            .first
    }
}

/// A faded copy of the icon being carried into the list, on a loop.
struct DragHint: View {
    static let size = CGSize(width: 256, height: 193)
    static let start = CGPoint(x: 65, y: 97)
    static let end = CGPoint(x: 191, y: 130)
    private static let bend = CGPoint(x: 125, y: 68)
    private static let curve = UnitCurve.bezier(startControlPoint: UnitPoint(x: 0.35, y: 0.1),
                                                endControlPoint: UnitPoint(x: 0.1, y: 0.95))
    @Environment(\.accessibilityReduceMotion) private var still
    @State private var begin = Date.now

    var body: some View {
        TimelineView(.animation) { context in
            let pose = pose(at: context.date.timeIntervalSince(begin).truncatingRemainder(dividingBy: 3.6))
            ZStack {
                AppIcon()
                    .frame(width: 96, height: 96)
                    .opacity(0.55)
                Image(nsImage: NSCursor.closedHand.image)
            }
            .scaleEffect(pose.scale)
            .opacity(pose.fade)
            .position(pose.point)
        }
        .frame(width: Self.size.width, height: Self.size.height)
        .accessibilityHidden(true)
    }

    /// Fades in, carries the icon along a slight arc, fades out, then rests before the next pass.
    private func pose(at time: Double) -> (point: CGPoint, scale: Double, fade: Double) {
        let start = still ? Self.end : Self.start
        switch time {
        case ..<0.24:
            let progress = time / 0.24
            return (start, 0.95 + 0.05 * progress, progress)
        case ..<0.35:
            return (start, 1, 1)
        case ..<2:
            let progress = Self.curve.value(at: (time - 0.35) / 1.65)
            return (still ? Self.end : arc(progress), 1 - 0.025 * progress, 1)
        case ..<2.12:
            return (Self.end, 0.975, 1)
        case ..<2.36:
            let progress = (time - 2.12) / 0.24
            return (Self.end, 0.975 - 0.045 * progress, 1 - progress)
        default:
            return (Self.end, 0.93, 0)
        }
    }

    private func arc(_ t: Double) -> CGPoint {
        let a = (1 - t) * (1 - t), b = 2 * t * (1 - t), c = t * t
        return CGPoint(x: a * Self.start.x + b * Self.bend.x + c * Self.end.x,
                       y: a * Self.start.y + b * Self.bend.y + c * Self.end.y)
    }
}
#endif
