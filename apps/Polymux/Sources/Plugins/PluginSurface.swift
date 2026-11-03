import SwiftUI
import PolymuxKit

/// What each plugin draws. Plugin screens ship with the app; a package only names one.
@MainActor enum PluginSurface {
    static let graph = "graph-visualiser"
    /// A package's own page, the one surface that doesn't ship with the app.
    static let web = "web"

    static var available: Set<String> {
        #if DEBUG
        if VerifyFixture.enabled { return ["hub", "permissions", graph, web, SampleSurface.name] }
        #endif
        return ["hub", "permissions", graph, web]
    }

    /// A Scoped plugin's content in the side panel. The panel itself stays the app's.
    @ViewBuilder static func panel(_ plugin: Plugin, thread: PolymuxKit.Thread, session: AppSession,
                                   close: @escaping () -> Void) -> some View {
        if plugin.surface == "permissions" { PermissionsPanel(plugin: plugin, thread: thread, session: session) }
        if plugin.surface == web {
            let chat = thread.isTask ? thread.agentID : thread.id
            PluginWeb(plugin: plugin, chat: chat, client: session.client, close: close).id(plugin.name + chat)
        }
        if plugin.surface == graph {
            let chat = thread.isTask ? thread.agentID : thread.id
            GraphView(plugin: plugin, chat: chat, session: session).id(chat)
        }
        #if DEBUG
        if plugin.surface == SampleSurface.name { SampleSurface(plugin: plugin, thread: thread) }
        #endif
    }

    /// A both plugin's global view, which fills the content beside the chats.
    @ViewBuilder static func view(_ plugin: Plugin, session: AppSession, openChat: @escaping (String) -> Void) -> some View {
        if plugin.surface == graph { GraphView(plugin: plugin, chat: nil, session: session, openChat: openChat).id(plugin.name) }
    }

    /// The switcher's count for a tab.
    static func unread(_ tab: PluginTab, session: AppSession) -> Int {
        guard session.settings.value?.switcherBadges != false else { return 0 }
        if tab.plugin?.surface == "hub" { return session.hub.unread }
        guard tab.plugin == nil else { return 0 }
        return Int(session.sidebar.chats.reduce(0) { $0 + $1.unreadCount })
    }

    /// A Global plugin's whole page on iPhone.
    @ViewBuilder static func page(_ plugin: Plugin, session: AppSession, settings: @escaping () -> Void) -> some View {
        #if os(iOS)
        if plugin.surface == "hub" { HubRoot(store: session.hub, client: session.client, settings: settings) }
        #endif
    }

    /// What the sidebar's + offers while a Global plugin is on show.
    static func actions(_ plugin: Plugin, session: AppSession) -> [PluginAction] {
        plugin.surface == "hub" ? session.hub.actions : []
    }

    /// The sidebar's search button while a Global plugin is on show.
    static func search(_ plugin: Plugin, session: AppSession) {
        if plugin.surface == "hub" { session.hub.panel = .search }
    }

    /// A plugin's own settings, under its page in Settings.
    @ViewBuilder static func settings(_ plugin: Plugin, open: @escaping () -> Void) -> some View {
        if plugin.surface == "hub" { HubPluginSettings(open: open) }
    }

    /// A page that a plugin's settings open, in place of its page in Settings.
    @ViewBuilder static func subpage(_ plugin: Plugin, back: @escaping () -> Void) -> some View {
        if plugin.surface == "hub" { HubAccountsPage(back: back) }
    }

    /// A Global plugin's sidebar.
    @ViewBuilder static func sidebar(_ plugin: Plugin, session: AppSession) -> some View {
        if plugin.surface == "hub" { HubSidebar(store: session.hub, client: session.client) }
    }

    /// A Global plugin's content to the right of its sidebar.
    @ViewBuilder static func detail(_ plugin: Plugin, session: AppSession) -> some View {
        if plugin.surface == "hub" { HubDetail(store: session.hub, client: session.client) }
    }
}

#if DEBUG
/// A Scoped surface for checks of the buttons and side panel.
struct SampleSurface: View {
    nonisolated static let name = "sample"
    let plugin: Plugin
    let thread: PolymuxKit.Thread

    var body: some View {
        Text("\(plugin.title) for \(thread.id)").foregroundStyle(.muted)
            .frame(maxWidth: .infinity, maxHeight: .infinity)
    }
}
#endif
