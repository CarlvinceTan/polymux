import SwiftUI
import UserNotifications
import PolymuxKit
#if os(macOS)
import AppKit
#else
import UIKit
#endif

@MainActor @Observable
final class Notifications: NSObject, UNUserNotificationCenterDelegate {
    static let shared = Notifications()
    private(set) var destination: String?
    private(set) var routeID = UUID()
    private(set) var issue: String?
    private var client: APIClient?
    private var token: String? = UserDefaults.standard.string(forKey: "notifications.token")
    private var remote = false
    private var registered = false
    private var registrationTime = Date.distantPast
    private var monitor: Task<Void, Never>?
    private var windows: [UUID: String] = [:]
    private let defaults = UserDefaults.standard

    private(set) var enabled = UserDefaults.standard.bool(forKey: "notifications.enabled")
    private var installation: String {
        if let id = defaults.string(forKey: "notifications.installation") { return id }
        let id = UUID().uuidString
        defaults.set(id, forKey: "notifications.installation")
        return id
    }

    override private init() {
        super.init()
        UNUserNotificationCenter.current().delegate = self
    }

    func connect(_ client: APIClient) {
        guard self.client?.baseURL != client.baseURL else { return }
        monitor?.cancel()
        if let previous = self.client, let token, previous.baseURL != client.baseURL {
            let device = PushDevice(id: installation, token: token, environment: pushEnvironment,
                server: previous.baseURL.absoluteString, enabled: false)
            Task { _ = try? await previous.registerPush(device) }
        }
        self.client = client
        registered = false
        remote = false
        monitor = Task { [weak self] in
            while !Task.isCancelled {
                guard let self else { return }
                await self.refresh(client)
                do { try await Task.sleep(for: .seconds(3)) } catch { return }
            }
        }
        if enabled { register() }
    }

    func allow(_ enabled: Bool) async {
        do {
            if enabled {
                let granted = try await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge])
                guard granted else { issue = "Allow notifications in system settings."; return }
            }
            defaults.set(enabled, forKey: "notifications.enabled")
            self.enabled = enabled
            registered = false
            if enabled { register() }
            else { UNUserNotificationCenter.current().removeAllPendingNotificationRequests() }
            issue = nil
            if let client { await refresh(client) }
        } catch { issue = "Couldn’t update notification permission." }
    }

    func received(_ data: Data) {
        token = data.map { String(format: "%02x", $0) }.joined()
        defaults.set(token, forKey: "notifications.token")
        registered = false
    }

    func failed() { issue = "Push registration unavailable; local alerts work while Polymux is running." }

    func visible(_ thread: String?, window: UUID) { windows[window] = thread }

    private func register() {
        #if os(macOS)
        NSApplication.shared.registerForRemoteNotifications()
        #else
        UIApplication.shared.registerForRemoteNotifications()
        #endif
    }

    private func refresh(_ client: APIClient) async {
        guard self.client?.baseURL == client.baseURL else { return }
        do {
            if (!registered || Date().timeIntervalSince(registrationTime) > 60), let token {
                let status = try await client.registerPush(PushDevice(id: installation, token: token, environment: pushEnvironment,
                    server: client.baseURL.absoluteString, enabled: enabled))
                guard self.client?.baseURL == client.baseURL else { return }
                remote = status.available && enabled
                registered = true
                registrationTime = Date()
                if enabled && !status.available { issue = "Closed-app alerts need server push setup." }
            }
            let key = "notifications.cursor.\(client.baseURL.absoluteString)"
            let cursor = defaults.object(forKey: key) as? NSNumber
            let page = try await client.notices(after: cursor?.int64Value)
            guard self.client?.baseURL == client.baseURL else { return }
            for notice in page.notices {
                guard enabled, !remote, !windows.values.contains(notice.conversationID),
                      let date = ISO8601DateFormatter().date(from: notice.createdAt) ?? fractionalDate(notice.createdAt),
                      abs(date.timeIntervalSinceNow) < 600 else { continue }
                let content = UNMutableNotificationContent()
                content.title = notice.title
                content.body = notice.body ?? "New message in Polymux"
                content.sound = .default
                content.threadIdentifier = notice.conversationID
                content.userInfo = ["server": client.baseURL.absoluteString, "conversationID": notice.conversationID,
                                    "messageID": notice.messageID]
                let id = "\(client.baseURL.absoluteString):\(notice.seq)"
                try await UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: id, content: content, trigger: nil))
            }
            defaults.set(page.cursor, forKey: key)
        } catch { }
    }

    private func fractionalDate(_ value: String) -> Date? {
        let format = ISO8601DateFormatter()
        format.formatOptions = [.withInternetDateTime, .withFractionalSeconds]
        return format.date(from: value)
    }

    private var pushEnvironment: String {
        #if DEBUG
        "sandbox"
        #else
        "production"
        #endif
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter,
        didReceive response: UNNotificationResponse) async {
        let server = response.notification.request.content.userInfo["server"] as? String
        let thread = response.notification.request.content.userInfo["conversationID"] as? String
        await MainActor.run {
            guard server == self.defaults.string(forKey: "serverAddress"), let thread else { return }
            self.destination = thread
            self.routeID = UUID()
        }
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter,
        willPresent notification: UNNotification) async -> UNNotificationPresentationOptions {
        let server = notification.request.content.userInfo["server"] as? String
        let thread = notification.request.content.userInfo["conversationID"] as? String
        return await MainActor.run {
            guard self.enabled, server == self.client?.baseURL.absoluteString,
                  let thread, !self.windows.values.contains(thread) else { return [] }
            return [.banner, .sound]
        }
    }
}

@MainActor
final class URLRouter {
    static let shared = URLRouter()
    var handler: ((URL) -> Void)?
}

#if os(macOS)
final class AppDelegate: NSObject, NSApplicationDelegate {
    func application(_ application: NSApplication, open urls: [URL]) {
        for url in urls {
            Task { @MainActor in URLRouter.shared.handler?(url) }
        }
    }
    func application(_ application: NSApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        Notifications.shared.received(deviceToken)
    }
    func application(_ application: NSApplication, didFailToRegisterForRemoteNotificationsWithError error: any Error) {
        Notifications.shared.failed()
    }
}
#else
final class AppDelegate: NSObject, UIApplicationDelegate {
    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        Notifications.shared.received(deviceToken)
    }
    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: any Error) {
        Notifications.shared.failed()
    }
}
#endif
