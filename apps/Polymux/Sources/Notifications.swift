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
    /// One server's alerts; each is registered and polled on its own.
    private final class Feed {
        let client: APIClient
        var remote = false
        var registered = false
        var registrationTime = Date.distantPast
        var monitor: Task<Void, Never>?

        init(client: APIClient) { self.client = client }
    }

    /// The chat a tapped alert opens, until that server's window takes it.
    private var destination: (server: String, thread: String)?
    private(set) var routeID = UUID()
    private(set) var issue: String?
    private var feeds: [String: Feed] = [:]
    private var token: String? = UserDefaults.standard.string(forKey: "notifications.token")
    private var windows: [UUID: String] = [:]
    private let defaults = UserDefaults.standard
    /// The server on show, which mute changes apply to.
    var current = ""

    private(set) var enabled = UserDefaults.standard.bool(forKey: "notifications.enabled")
    /// Agents whose alerts this device mutes, by server; their unread dots are unaffected.
    private var muted: [String: Set<String>] = [:]
    /// This install's ID, the same on every server.
    var installation: String {
        if let id = defaults.string(forKey: "notifications.installation") { return id }
        let id = UUID().uuidString
        defaults.set(id, forKey: "notifications.installation")
        return id
    }

    override private init() {
        super.init()
        UNUserNotificationCenter.current().delegate = self
    }

    /// `address` is the server's saved address; the client's own URL is a local port when tunnelled.
    func connect(_ client: APIClient, address: String) {
        guard feeds[address]?.client.id != client.id else { return }
        feeds[address]?.monitor?.cancel()
        let feed = Feed(client: client)
        feeds[address] = feed
        muted[address] = Set(defaults.stringArray(forKey: "notifications.muted.\(address)") ?? [])
        feed.monitor = Task { [weak self] in
            while !Task.isCancelled {
                guard let self else { return }
                await self.refresh(feed, address: address)
                do { try await Task.sleep(for: .seconds(3)) } catch { return }
            }
        }
        if enabled { register() }
    }

    /// Stops a removed server's alerts, including those it pushes while the app is closed.
    func disconnect(_ address: String) {
        guard let feed = feeds.removeValue(forKey: address) else { return }
        feed.monitor?.cancel()
        guard let token else { return }
        let device = PushDevice(id: installation, token: token, environment: pushEnvironment,
            server: address, enabled: false, muted: [])
        Task { _ = try? await feed.client.registerPush(device) }
    }

    func allow(_ enabled: Bool) async {
        do {
            if enabled {
                let granted = try await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge])
                guard granted else { issue = "Allow notifications in system settings."; return }
            }
            defaults.set(enabled, forKey: "notifications.enabled")
            self.enabled = enabled
            for feed in feeds.values { feed.registered = false }
            if enabled { register() }
            else { UNUserNotificationCenter.current().removeAllPendingNotificationRequests() }
            issue = nil
            for (address, feed) in feeds { await refresh(feed, address: address) }
        } catch { issue = "Couldn’t update notification permission." }
    }

    func received(_ data: Data) {
        token = data.map { String(format: "%02x", $0) }.joined()
        defaults.set(token, forKey: "notifications.token")
        for feed in feeds.values { feed.registered = false }
    }

    func failed() { issue = "Alerts only arrive while Polymux is open." }

    func isMuted(_ agentID: String) -> Bool { muted[current]?.contains(agentID) == true }

    /// Re-registers with the server so its push queue picks up the new mute list.
    func setMuted(_ muted: Bool, agent agentID: String) {
        var agents = self.muted[current] ?? []
        if muted { agents.insert(agentID) } else { agents.remove(agentID) }
        self.muted[current] = agents
        defaults.set(agents.sorted(), forKey: "notifications.muted.\(current)")
        feeds[current]?.registered = false
    }

    func visible(_ thread: String?, window: UUID) { windows[window] = thread }

    /// The chat a tapped alert opened on this server, handed over once.
    func take(_ server: String) -> String? {
        guard destination?.server == server else { return nil }
        defer { destination = nil }
        return destination?.thread
    }

    private func register() {
        #if os(macOS)
        NSApplication.shared.registerForRemoteNotifications()
        #else
        UIApplication.shared.registerForRemoteNotifications()
        #endif
    }

    private func refresh(_ feed: Feed, address: String) async {
        guard feeds[address] === feed else { return }
        let client = feed.client
        let muted = muted[address] ?? []
        do {
            if (!feed.registered || Date().timeIntervalSince(feed.registrationTime) > 60), let token {
                let status = try await client.registerPush(PushDevice(id: installation, token: token, environment: pushEnvironment,
                    server: address, enabled: enabled, muted: muted.sorted()))
                guard feeds[address] === feed else { return }
                feed.remote = status.available && enabled
                feed.registered = true
                feed.registrationTime = Date()
                if enabled && !status.available { issue = "Closed-app alerts need server push setup." }
            }
            let key = "notifications.cursor.\(address)"
            let cursor = defaults.object(forKey: key) as? NSNumber
            let page = try await client.notices(after: cursor?.int64Value)
            guard feeds[address] === feed else { return }
            for notice in page.notices {
                guard enabled, !feed.remote, !windows.values.contains(notice.conversationID),
                      notice.agentID.map({ !muted.contains($0) }) ?? true,
                      let date = WireDate.parse(notice.createdAt),
                      abs(date.timeIntervalSinceNow) < 600 else { continue }
                let content = UNMutableNotificationContent()
                content.title = notice.title
                content.body = notice.body ?? "New message in Polymux"
                content.sound = .default
                content.threadIdentifier = notice.conversationID
                content.userInfo = ["server": address, "conversationID": notice.conversationID,
                                    "messageID": notice.messageID]
                let id = "\(address):\(notice.seq)"
                try await UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: id, content: content, trigger: nil))
            }
            defaults.set(page.cursor, forKey: key)
        } catch { }
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
            guard let server, self.feeds[server] != nil, let thread else { return }
            self.destination = (server, thread)
            self.routeID = UUID()
            // That server's window takes the chat when it appears.
            self.defaults.set(server, forKey: "serverAddress")
        }
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter,
        willPresent notification: UNNotification) async -> UNNotificationPresentationOptions {
        let server = notification.request.content.userInfo["server"] as? String
        let thread = notification.request.content.userInfo["conversationID"] as? String
        return await MainActor.run {
            guard self.enabled, let server, self.feeds[server] != nil,
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
