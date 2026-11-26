#if os(macOS)
import AppKit
import Observation
import PolymuxKit

/// The menu bar icon and the notch activity pill, each turned on in Settings.
@MainActor @Observable
final class StatusBar {
    static let shared = StatusBar()

    /// One chat as the menu and the notch show it.
    struct Entry: Identifiable, Equatable {
        let thread: PolymuxKit.Thread
        /// The chat's title, or its bot's name for a bot's own chat.
        let name: String
        let members: [Agent]
        /// The bot whose face the row shows: the first one working, else the chat's own.
        let agentID: String
        let preview: String
        let working: Bool
        let marker: ActivityMarker?
        var id: String { thread.id }
    }

    var menu = UserDefaults.standard.bool(forKey: "statusBar.menu") {
        didSet { UserDefaults.standard.set(menu, forKey: "statusBar.menu"); follow() }
    }
    var notch = UserDefaults.standard.bool(forKey: "statusBar.notch") {
        didSet { UserDefaults.standard.set(notch, forKey: "statusBar.notch"); follow(); NotchPanel.shared.update() }
    }
    /// The server on show, whose chats both surfaces list.
    var session: AppSession? {
        didSet { follow(); NotchPanel.shared.update() }
    }
    /// Settings was asked for from the menu; the next window on show opens it.
    var settingsPending = false
    @ObservationIgnored private var refresher: Task<Void, Never>?

    private init() {}

    func start() {
        follow()
        NotchPanel.shared.update()
    }

    var entries: [Entry] {
        guard let sidebar = session?.sidebar else { return [] }
        let activity = sidebar.activity
        let connected = sidebar.isConnected
        return sidebar.chats.map { thread in
            let members = sidebar.participants(in: thread)
            let unread = thread.unreadCount > 0
            let views = members.map { ($0, activity.view(conversationID: thread.id, agentID: $0.id,
                                                         connected: connected, unread: unread)) }
            let busy = views.filter { $0.1.animating }
            let marker = activity.marker(conversationID: thread.id, agentIDs: members.map(\.id), unread: unread)
            let preview = busy.isEmpty
                ? (thread.lastMessage?.text ?? "").replacingOccurrences(of: "\n", with: " ")
                : ActivityPhrase.line(busy.map { (name: $0.0.name, view: $0.1) })
            let name = thread.isGroup ? thread.title : members.first?.name ?? thread.title
            return Entry(thread: thread, name: name, members: members, agentID: busy.first?.0.id ?? thread.agentID,
                         preview: preview, working: !busy.isEmpty, marker: marker)
        }
    }

    /// Chats with a bot at work.
    var working: [Entry] { entries.filter(\.working) }

    /// The newest chats that aren't already listed as working.
    var recent: [Entry] {
        entries.filter { !$0.working }
            .sorted { ($0.thread.lastMessage?.date ?? .distantPast) > ($1.thread.lastMessage?.date ?? .distantPast) }
            .prefix(6).map { $0 }
    }

    var unread: Int { session?.sidebar.chats.filter { $0.unreadCount > 0 }.count ?? 0 }

    /// Brings Polymux forward on this chat, reopening the window if it was closed.
    func open(_ thread: String) {
        guard let address = session?.address else { return }
        Notifications.shared.route(thread, server: address)
        show()
    }

    func show() {
        NSWorkspace.shared.openApplication(at: Bundle.main.bundleURL, configuration: NSWorkspace.OpenConfiguration())
    }

    /// While either surface is on and no window keeps the chats fresh, refresh them here.
    private func follow() {
        refresher?.cancel()
        guard menu || notch, let session else { return }
        refresher = Task { [weak self] in
            while !Task.isCancelled {
                if self?.windowShown == false { await session.sidebar.refresh() }
                do { try await Task.sleep(for: .seconds(5)) } catch { return }
            }
        }
    }

    private var windowShown: Bool {
        NSApp.windows.contains { $0.isVisible && $0.canBecomeMain && !$0.isMiniaturized }
    }
}
#endif
