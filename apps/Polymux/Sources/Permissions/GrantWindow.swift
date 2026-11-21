#if os(macOS)
import AppKit
import SwiftUI

/// The Grant Permissions window. A permission's guide docks inside System Settings, over its list,
/// with the server's icon ready to drag into it.
@MainActor
final class GrantWindow: NSObject, NSWindowDelegate {
    static let shared = GrantWindow()
    private var window: NSPanel?
    private var guide: NSPanel?
    private var following: Task<Void, Never>?
    private var checking: Task<Void, Never>?

    func show() {
        let window = window ?? make()
        self.window = window
        if !window.isVisible { window.center() }
        window.makeKeyAndOrderFront(nil)
        NSApp.activate()
        startChecking()
    }

    /// Shows a permission's guide over System Settings and follows it as it moves.
    func follow() {
        guard let permission = Permissions.shared.opened else { return }
        hideGuide()
        let panel = makeGuide(permission)
        guide = panel
        startFollowing()
        startChecking()
    }

    private func startChecking() {
        checking?.cancel()
        checking = Task {
            while !Task.isCancelled {
                await Permissions.shared.refresh()
                if Permissions.shared.opened == nil { closeGuide() }
                if window?.isVisible != true && guide == nil { return }
                try? await Task.sleep(for: .seconds(1))
            }
        }
    }

    /// Docks the guide inside System Settings until the permission is granted or Settings closes.
    private func startFollowing() {
        following?.cancel()
        following = Task { [weak self] in
            var seen = false
            let deadline = Date.now.addingTimeInterval(10)
            while !Task.isCancelled {
                guard let self else { return }
                if Permissions.shared.opened == nil { break }
                if let frame = SystemSettings.frame() {
                    // Raise Settings once, in case it opened behind Polymux's windows.
                    if !seen { NSRunningApplication.runningApplications(withBundleIdentifier: SystemSettings.id).first?.activate() }
                    seen = true
                    place(frame)
                } else if seen || Date.now > deadline {
                    break
                }
                try? await Task.sleep(for: .milliseconds(100))
            }
            if !Task.isCancelled { self?.closeGuide() }
        }
    }

    /// Keeps the guide at the bottom-right of the Settings window, where the list is above it.
    private func place(_ settings: CGRect) {
        guard let guide, let screen = NSScreen.screens.first else { return }
        let top = screen.frame.height - settings.minY
        let size = guide.frame.size
        let visible = screen.visibleFrame
        let x = min(max(settings.maxX - size.width - 16, visible.minX + 8), visible.maxX - size.width - 8)
        let y = min(max(top - settings.height + 16, visible.minY + 8), visible.maxY - size.height - 8)
        let origin = NSPoint(x: x, y: y)
        if guide.frame.origin != origin { guide.setFrameOrigin(origin) }
        if !guide.isVisible { guide.orderFrontRegardless() }
    }

    func windowWillClose(_ notification: Notification) {
        closeGuide()
        checking?.cancel()
        checking = nil
    }

    private func closeGuide() {
        hideGuide()
        Permissions.shared.settle()
        if window?.isVisible != true {
            checking?.cancel()
            checking = nil
        }
    }

    private func hideGuide() {
        following?.cancel()
        following = nil
        guide?.orderOut(nil)
        guide = nil
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

    private func makeGuide(_ permission: Permission) -> NSPanel {
        let view = PermissionGuide(permission: permission)
        let size = NSHostingController(rootView: view).sizeThatFits(in: CGSize(width: 480, height: 400))
        let panel = NSPanel(contentRect: NSRect(origin: .zero, size: size),
                            styleMask: [.borderless, .nonactivatingPanel], backing: .buffered, defer: true)
        panel.isOpaque = false
        panel.backgroundColor = .clear
        panel.hasShadow = true
        panel.level = .floating
        panel.collectionBehavior = [.canJoinAllSpaces, .fullScreenAuxiliary, .transient, .ignoresCycle]
        panel.isReleasedWhenClosed = false
        panel.hidesOnDeactivate = false
        panel.contentView = NSHostingView(rootView: view)
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

/// The card inside System Settings: one instruction and the server's tile to drag into the list.
private struct PermissionGuide: View {
    let permission: Permission
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Drag Polymux Server into the \(permission.title) list above")
                .font(.callout.weight(.medium))
                .multilineTextAlignment(.leading)
            PermissionTile()
        }
        .padding(16)
        .frame(width: 480, alignment: .leading)
        .background(PanelSurface.color(scheme), in: RoundedRectangle(cornerRadius: 20, style: .continuous))
        .appAppearance(Permissions.shared.appearance)
    }
}

/// The server as System Settings lists it: its icon and name, ready to drag.
private struct PermissionTile: View {
    var body: some View {
        HStack(spacing: 10) {
            ServerIcon().frame(width: 30, height: 30)
            Text("Polymux Server").font(.callout.weight(.medium))
            Spacer(minLength: 8)
            Image(systemName: "hand.draw")
                .font(.system(size: 14))
                .foregroundStyle(.secondary)
        }
        .padding(.horizontal, 12)
        .frame(height: 44)
        .background(.primary.opacity(0.06), in: RoundedRectangle(cornerRadius: 8, style: .continuous))
        .contentShape(Rectangle())
        .onDrag { NSItemProvider(contentsOf: ServerHost.bundle) ?? NSItemProvider() }
        .onHover { inside in
            if inside { NSCursor.openHand.set() } else { NSCursor.arrow.set() }
        }
        .help("Drag Polymux Server into the list in System Settings.")
        .accessibilityLabel("Polymux Server")
    }
}

#endif
