#if os(macOS)
import AppKit
import UserNotifications
import PolymuxKit

/// A macOS permission the Grant window asks for.
enum Permission: String, CaseIterable, Identifiable {
    case fullDisk, screen, notifications

    var id: String { rawValue }

    var title: String {
        switch self {
        case .fullDisk: "Full Disk Access"
        case .screen: "Screen & System Audio Recording"
        case .notifications: "Notifications"
        }
    }

    var detail: String {
        switch self {
        case .fullDisk: "Allows bots running in Polymux to open files in protected folders and sandboxed containers."
        case .screen: "Allows bots running in Polymux to capture the screen, windows, and system audio."
        case .notifications: "Allow Polymux to notify you when bots reply or need your attention."
        }
    }

    var symbol: String {
        switch self {
        case .fullDisk: "internaldrive"
        case .screen: "rectangle.inset.filled.badge.record"
        case .notifications: "bell.badge"
        }
    }

    /// Granted to Polymux Server, which bots run under, by dragging it into System Settings' list.
    var dragged: Bool { self != .notifications }

    var pane: URL {
        switch self {
        case .fullDisk: URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles")!
        case .screen: URL(string: "x-apple.systempreferences:com.apple.preference.security?Privacy_ScreenCapture")!
        case .notifications:
            URL(string: "x-apple.systempreferences:com.apple.Notifications-Settings.extension?id=\(Bundle.main.bundleIdentifier ?? "")")!
        }
    }
}

/// What this Mac has granted Polymux, and the pane System Settings was opened on to grant more.
@MainActor @Observable
final class Permissions {
    static let shared = Permissions()

    private(set) var granted: Set<Permission> = []
    /// The pane being granted by dragging, while its hint shows beside System Settings.
    private(set) var opened: Permission?
    /// This Mac's own server, which reports what it has been granted.
    var client: APIClient?
    /// The theme picked in Settings, which the Grant window follows.
    var appearance = "system"

    private init() {}

    /// Server permissions only apply when this Mac runs the app's own server.
    var all: [Permission] {
        #if DEBUG
        if CommandLine.arguments.contains("--verify-permissions") { return Permission.allCases }
        #endif
        return ServerHost.shared.running && ServerHost.shared.managed ? Permission.allCases : [.notifications]
    }

    var summary: String {
        let count = all.filter(granted.contains).count
        switch count {
        case 0: return "None granted yet"
        case all.count: return "All granted"
        default: return "\(count) of \(all.count) granted"
        }
    }

    func refresh() async {
        var next = granted
        let settings = await UNUserNotificationCenter.current().notificationSettings()
        if settings.authorizationStatus == .authorized && Notifications.shared.enabled {
            next.insert(.notifications)
        } else {
            next.remove(.notifications)
        }
        // A failed check keeps the last answer.
        if all.contains(.fullDisk), let status = try? await client?.privacy() {
            for (permission, on) in [(Permission.fullDisk, status.fullDiskAccess), (.screen, status.screenRecording)] {
                if on { next.insert(permission) } else { next.remove(permission) }
            }
        }
        granted = next
        if let opened, granted.contains(opened) { self.opened = nil }
    }

    func grant(_ permission: Permission) {
        guard permission.dragged else {
            Task {
                let status = await UNUserNotificationCenter.current().notificationSettings().authorizationStatus
                if status == .denied {
                    NSWorkspace.shared.open(permission.pane)
                } else {
                    await Notifications.shared.allow(true)
                    await refresh()
                }
            }
            return
        }
        opened = permission
        NSApp.yieldActivation(toApplicationWithBundleIdentifier: SystemSettings.id)
        NSWorkspace.shared.open(permission.pane)
        GrantWindow.shared.follow()
    }

    /// Ends the hint, as when System Settings closes.
    func settle() { opened = nil }
}
#endif
