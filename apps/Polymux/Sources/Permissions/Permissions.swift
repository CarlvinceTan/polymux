#if os(macOS)
import AppKit
import SwiftUI
import ApplicationServices
import UserNotifications
import PolymuxKit

/// A macOS permission the Grant window asks for.
enum Permission: String, CaseIterable, Identifiable {
    case fullDisk, screen, accessibility, notifications

    var id: String { rawValue }

    var title: String {
        switch self {
        case .fullDisk: "Full Disk Access"
        case .screen: "Screen & System Audio Recording"
        case .accessibility: "Accessibility"
        case .notifications: "Notifications"
        }
    }

    /// What the permission allows, for whoever holds it: the server's bots, or this app for a remote server.
    var detail: String {
        switch self {
        case .fullDisk: "Allows bots running in Polymux to open files in protected folders and sandboxed containers."
        case .screen: "Allows bots running in Polymux to capture the screen, windows, and system audio."
        case .accessibility: "Allows bots and plugins such as Computer Use to read and control apps on this Mac."
        case .notifications: "Allow Polymux to notify you when bots reply or need your attention."
        }
    }

    var symbol: String {
        switch self {
        case .fullDisk: "internaldrive"
        case .screen: "rectangle.inset.filled.badge.record"
        case .accessibility: "accessibility"
        case .notifications: "bell.badge"
        }
    }

    var tint: Color {
        switch self {
        case .fullDisk: .blue
        case .screen: .purple
        case .accessibility: .green
        case .notifications: .orange
        }
    }

    /// Granted to Polymux Server, which bots run under, by dragging it into System Settings' list.
    var dragged: Bool { self != .notifications }

    var pane: URL {
        switch self {
        case .fullDisk: URL(string: "x-apple.systempreferences:com.apple.settings.PrivacySecurity.extension?Privacy_AllFiles")!
        case .screen: URL(string: "x-apple.systempreferences:com.apple.settings.PrivacySecurity.extension?Privacy_ScreenCapture")!
        case .accessibility: URL(string: "x-apple.systempreferences:com.apple.settings.PrivacySecurity.extension?Privacy_Accessibility")!
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
    @ObservationIgnored private var refreshID = 0
    /// The pane being granted by dragging, while its hint shows beside System Settings.
    private(set) var opened: Permission?
    /// This Mac's own server, which reports what it has been granted.
    var client: APIClient?
    /// The theme picked in Settings, which the Grant window follows.
    var appearance = "system"

    private init() {}

    /// Whether this Mac runs the app's own server, so grants go to Polymux Server and not to the app.
    var hostsServer: Bool {
        #if DEBUG
        if CommandLine.arguments.contains("--verify-permissions") { return true }
        #endif
        return ServerHost.shared.running && ServerHost.shared.managed
    }

    /// Every permission is listed. With the server here they are the server's; otherwise this Mac's own, which a
    /// server on another device needs to control it.
    var all: [Permission] { Permission.allCases }

    func refresh() async {
        refreshID += 1
        let id = refreshID
        var next = granted
        let settings = await UNUserNotificationCenter.current().notificationSettings()
        if settings.authorizationStatus == .authorized && Notifications.shared.enabled {
            next.insert(.notifications)
        } else {
            next.remove(.notifications)
        }
        // A failed check keeps the last answer.
        let status: PrivacyStatus?
        if hostsServer {
            status = try? await client?.privacy()
        } else {
            status = Self.local()
        }
        if let status {
            for (permission, on) in [(Permission.fullDisk, status.fullDiskAccess), (.screen, status.screenRecording),
                                     (.accessibility, status.accessibility)] {
                if on { next.insert(permission) } else { next.remove(permission) }
            }
        }
        guard !Task.isCancelled, id == refreshID else { return }
        granted = next
        if let opened, granted.contains(opened) { self.opened = nil }
    }

    /// What System Settings has granted this app itself.
    private static func local() -> PrivacyStatus {
        let tcc = FileManager.default.homeDirectoryForCurrentUser
            .appending(path: "Library/Application Support/com.apple.TCC/TCC.db")
        let handle = try? FileHandle(forReadingFrom: tcc)
        let disk = handle != nil
        try? handle?.close()
        return PrivacyStatus(fullDiskAccess: disk, screenRecording: CGPreflightScreenCaptureAccess(),
                             accessibility: AXIsProcessTrusted())
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
        // Asking lists this app in System Settings, so there is nothing to drag for these two.
        if !hostsServer {
            switch permission {
            case .screen: _ = CGRequestScreenCaptureAccess()
            case .accessibility: _ = AXIsProcessTrustedWithOptions(["AXTrustedCheckOptionPrompt": true] as CFDictionary)
            default: break
            }
        }
        NSApp.yieldActivation(toApplicationWithBundleIdentifier: SystemSettings.id)
        let config = NSWorkspace.OpenConfiguration()
        config.activates = true
        NSWorkspace.shared.open(permission.pane, configuration: config)
        GrantWindow.shared.follow()
    }

    /// Ends the hint, as when System Settings closes.
    func settle() { opened = nil }
}
#endif
