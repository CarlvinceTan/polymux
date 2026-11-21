#if os(macOS)
import AppKit
import SwiftUI

/// A permission's guide docks inside System Settings, over its list, with the icon of whatever holds the
/// grant (Polymux Server, or this app) ready to drag into it.
@MainActor
final class GrantWindow {
    static let shared = GrantWindow()
    private var guide: NSPanel?
    private var following: Task<Void, Never>?
    private var checking: Task<Void, Never>?

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
                if guide == nil { return }
                try? await Task.sleep(for: .seconds(1))
            }
        }
    }

    /// Docks the guide inside System Settings until the permission is granted or Settings closes.
    private func startFollowing() {
        following?.cancel()
        following = Task { [weak self] in
            var seen = false
            var lastRaise = Date.distantPast
            let started = Date.now
            let deadline = started.addingTimeInterval(10)
            while !Task.isCancelled {
                guard let self else { return }
                if Permissions.shared.opened == nil { break }
                if let frame = SystemSettings.frame() {
                    seen = true
                    // Brings Settings forward for the first seconds, in case it opened behind Polymux.
                    if !SystemSettings.isFront, Date.now.timeIntervalSince(lastRaise) > 1, Date.now < started.addingTimeInterval(3) {
                        lastRaise = .now
                        SystemSettings.raise()
                    }
                    place(frame)
                } else if seen || Date.now > deadline {
                    break
                }
                try? await Task.sleep(for: .milliseconds(100))
            }
            if !Task.isCancelled { self?.closeGuide() }
        }
    }

    /// Keeps the guide at the bottom-right of the Settings window, on whichever screen it is on,
    /// and only while Settings is the app in front.
    private func place(_ settings: CGRect) {
        guard let guide, let primary = NSScreen.screens.first else { return }
        guard SystemSettings.isFront else {
            if guide.isVisible { guide.orderOut(nil) }
            return
        }
        // Window frames have their origin at the top left of the primary screen; AppKit's is bottom left.
        let flipped = CGRect(x: settings.minX, y: primary.frame.height - settings.maxY,
                             width: settings.width, height: settings.height)
        let screen = NSScreen.screens.max { overlap($0.frame, flipped) < overlap($1.frame, flipped) } ?? primary
        let size = guide.frame.size
        let visible = screen.visibleFrame
        let x = min(max(flipped.maxX - size.width - 16, visible.minX + 8), visible.maxX - size.width - 8)
        let y = min(max(flipped.minY + 16, visible.minY + 8), visible.maxY - size.height - 8)
        let origin = NSPoint(x: x, y: y)
        if guide.frame.origin != origin { guide.setFrameOrigin(origin) }
        if !guide.isVisible { guide.orderFrontRegardless() }
    }

    private func overlap(_ a: CGRect, _ b: CGRect) -> CGFloat {
        let i = a.intersection(b)
        return i.isNull ? 0 : i.width * i.height
    }

    private func closeGuide() {
        hideGuide()
        Permissions.shared.settle()
        checking?.cancel()
        checking = nil
    }

    private func hideGuide() {
        following?.cancel()
        following = nil
        guide?.orderOut(nil)
        guide = nil
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

/// System Settings, which Polymux can only watch from outside.
enum SystemSettings {
    static let id = "com.apple.systempreferences"

    static var app: NSRunningApplication? { NSRunningApplication.runningApplications(withBundleIdentifier: id).first }
    static var isFront: Bool { NSWorkspace.shared.frontmostApplication?.bundleIdentifier == id }

    /// Brings Settings and its windows forward, even when Polymux is not the active app:
    /// macOS ignores activate() from a background app, but `open` asks LaunchServices.
    static func raise() {
        app?.unhide()
        let open = Process()
        open.executableURL = URL(fileURLWithPath: "/usr/bin/open")
        open.arguments = ["-b", id]
        try? open.run()
    }

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

/// The card inside System Settings: one instruction and the holder's tile to drag into the list.
private struct PermissionGuide: View {
    let permission: Permission
    let holder = Holder.current
    @Environment(\.colorScheme) private var scheme

    var body: some View {
        VStack(alignment: .leading, spacing: 12) {
            Text("Drag \(holder.name) into the \(permission.title) list above")
                .font(.callout.weight(.medium))
                .multilineTextAlignment(.leading)
            PermissionTile(holder: holder)
        }
        .padding(16)
        .frame(width: 480, alignment: .leading)
        .background(PanelSurface.color(scheme), in: RoundedRectangle(cornerRadius: 20, style: .continuous))
        .appAppearance(Permissions.shared.appearance)
    }
}

/// What a grant goes to: Polymux Server when this Mac runs it, otherwise this app.
private struct Holder {
    let name: String
    let bundle: URL
    let server: Bool

    @MainActor static var current: Holder {
        if Permissions.shared.hostsServer { return Holder(name: "Polymux Server", bundle: ServerHost.bundle, server: true) }
        let bundle = Bundle.main.bundleURL
        return Holder(name: FileManager.default.displayName(atPath: bundle.path).replacing(".app", with: ""),
                      bundle: bundle, server: false)
    }
}

/// The holder as System Settings lists it: its icon and name, ready to drag.
private struct PermissionTile: View {
    let holder: Holder

    var body: some View {
        HStack(spacing: 10) {
            Group {
                if holder.server { ServerIcon() } else { AppIcon() }
            }
            .frame(width: 30, height: 30)
            Text(holder.name).font(.callout.weight(.medium))
            Spacer(minLength: 8)
            Image(systemName: "hand.draw")
                .font(.system(size: 14))
                .foregroundStyle(.secondary)
        }
        .padding(.horizontal, 12)
        .frame(height: 44)
        .background(.primary.opacity(0.06), in: RoundedRectangle(cornerRadius: 8, style: .continuous))
        .contentShape(Rectangle())
        .onDrag { NSItemProvider(contentsOf: holder.bundle) ?? NSItemProvider() }
        .onHover { inside in
            if inside { NSCursor.openHand.set() } else { NSCursor.arrow.set() }
        }
        .help("Drag \(holder.name) into the list in System Settings.")
        .accessibilityLabel(holder.name)
    }
}

#endif
