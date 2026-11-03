import Foundation
import Observation

/// One switcher entry: the app's own chats, or a Global plugin.
public struct PluginTab: Identifiable, Hashable, Sendable {
    public static let chats = "chats"
    public let id: String
    public let plugin: Plugin?
    /// The server's address and name, when the switcher spans more than one server.
    public let server: String?
    public let serverName: String?
    /// Marks the server's Chats where a tab bar shows icons only.
    public let serverMark: String?

    public init(id: String, plugin: Plugin?, server: String? = nil, serverName: String? = nil, serverMark: String? = nil) {
        self.id = id
        self.plugin = plugin
        self.server = server
        self.serverName = serverName
        self.serverMark = serverMark
    }

    /// The entry within its server: `chats`, or a plugin's name.
    public var name: String { plugin?.name ?? Self.chats }

    /// Each server's initial, or everyone's position when initials clash or one has no symbol.
    public static func marks(_ names: [String]) -> [String] {
        let initials = names.compactMap { name in
            name.lowercased().first { $0.isASCII && ($0.isLetter || $0.isNumber) }.map(String.init)
        }
        if initials.count == names.count, Set(initials).count == names.count { return initials }
        return names.indices.map { String($0 + 1) }
    }
}

@MainActor @Observable
public final class PluginStore {
    public private(set) var list: PluginList?
    public private(set) var market: [MarketPlugin]?
    public private(set) var marketIssue: String?
    public private(set) var issue: String?
    public private(set) var settings: [String: [PluginSetting]] = [:]
    public private(set) var transports: [PluginTransport] = []
    /// The surfaces this build of the app can draw.
    public let surfaces: Set<String>
    @ObservationIgnored private let client: APIClient
    @ObservationIgnored private let cache: LocalCacheActor
    @ObservationIgnored private var revision = 0
    @ObservationIgnored private var pending = 0

    public init(client: APIClient, cache: LocalCacheActor, surfaces: Set<String>) {
        self.client = client
        self.cache = cache
        self.surfaces = surfaces
    }

    public var installed: [Plugin] { list?.installed ?? [] }

    public func restore() async {
        if let saved: PluginList = await cache.snapshot("plugins"), list == nil { list = saved }
        if let saved: [MarketPlugin] = await cache.snapshot("plugin-market"), market == nil { market = saved }
    }

    public func refresh() async {
        guard pending == 0 else { return }
        let revision = revision
        guard let next = try? await client.plugins(), self.revision == revision, pending == 0 else { return }
        accept(next)
    }

    public func loadMarket() async {
        do {
            let next = try await client.pluginMarket()
            marketIssue = nil
            if market != next {
                market = next
                await cache.saveSnapshot("plugin-market", next)
            }
        } catch {
            if !error.isOffline { marketIssue = "Marketplace unavailable." }
        }
    }

    private func accept(_ next: PluginList) {
        guard list != next else { return }
        list = next
        Task { await cache.saveSnapshot("plugins", next) }
    }

    /// Applies an edit at once, and restores the previous list if the server refuses it.
    private func change(failure: String, edit: (inout PluginList) -> Void,
                        commit: @escaping @Sendable (PluginList) async throws -> Void) {
        guard let previous = list else { return }
        var next = previous
        edit(&next)
        list = next
        issue = nil
        revision += 1
        pending += 1
        Task {
            do {
                try await commit(next)
                pending -= 1
                await refresh()
            } catch {
                pending -= 1
                list = previous
                issue = failure
            }
        }
    }

    public func install(_ item: MarketPlugin) {
        let placeholder = Plugin(name: item.name, title: item.title, description: item.description, symbol: item.symbol,
                                 version: item.version, kind: item.kind, surface: item.surface,
                                 state: PluginState.installing, source: item.source)
        let client = client
        change(failure: "\(item.title) couldn’t be installed.") { list in
            list.installed.removeAll { $0.name == item.name }
            list.installed.append(placeholder)
        } commit: { _ in
            _ = try await client.installPlugin(name: item.name, version: item.version)
        }
    }

    public func remove(_ plugin: Plugin) {
        let client = client
        change(failure: "\(plugin.title) couldn’t be removed.") { list in
            list.installed.removeAll { $0.name == plugin.name }
            list.layout.switcher.removeAll { $0 == plugin.name }
            list.layout.buttons = list.layout.buttons.compactMap { group in
                let kept = group.filter { $0 != plugin.name }
                return kept.isEmpty ? nil : kept
            }
        } commit: { _ in
            try await client.removePlugin(name: plugin.name)
        }
    }

    public func setActive(_ active: Bool, plugin: Plugin, chat: String) {
        let others = (installed.first { $0.name == plugin.name }?.chats ?? plugin.chats).filter { $0 != chat }
        let chats = active ? others + [chat] : others
        let client = client
        change(failure: "Couldn’t update \(plugin.title).") { list in
            guard let index = list.installed.firstIndex(where: { $0.name == plugin.name }) else { return }
            list.installed[index].chats = chats
        } commit: { _ in
            _ = try await client.updatePlugin(name: plugin.name, chats: chats)
        }
    }

    public func save(layout: PluginLayout) {
        let client = client
        change(failure: "Couldn’t save the layout.") { list in
            list.layout = layout
        } commit: { list in
            _ = try await client.savePluginLayout(list.layout)
        }
    }

    private func drawable(_ plugin: Plugin) -> Bool {
        plugin.state == PluginState.ready && surfaces.contains(plugin.surface)
    }

    /// The switcher's entries, or none when no Global plugin can be shown.
    public var tabs: [PluginTab] {
        guard let list else { return [] }
        let tabs: [PluginTab] = list.layout.switcher.compactMap { name in
            if name == PluginTab.chats { return PluginTab(id: name, plugin: nil) }
            return list.installed.first { $0.name == name && drawable($0) }.map { PluginTab(id: name, plugin: $0) }
        }
        return tabs.contains { $0.plugin != nil } ? tabs : []
    }

    /// Plugins with a global view beside the chats, which keep the sidebar.
    public var views: [Plugin] { installed.filter { $0.kind == PluginKind.both && drawable($0) } }

    /// A view's place among the chats, kept apart from every chat ID.
    public static func viewID(_ name: String) -> String { "plugin:" + name }

    public static func isView(_ id: String?) -> Bool { id?.hasPrefix("plugin:") == true }

    /// The shared button layout, keeping only what is active in this chat.
    public func buttons(chat: String) -> [[Plugin]] {
        guard let list else { return [] }
        return list.layout.buttons.compactMap { group in
            let active = group.compactMap { name in
                list.installed.first { $0.name == name && drawable($0) && $0.chats.contains(chat) }
            }
            return active.isEmpty ? nil : active
        }
    }

    public func loadSettings(_ name: String) async {
        guard let next = try? await client.pluginSettings(name: name), settings[name] != next else { return }
        settings[name] = next
    }

    public func loadTransports() async {
        guard let next = try? await client.transports(), transports != next else { return }
        transports = next
    }

    /// Shows a new value at once, and puts the old one back if the server refuses it.
    public func set(_ value: PluginSettingValue, key: String, plugin: String) {
        guard let previous = settings[plugin], let index = previous.firstIndex(where: { $0.key == key }),
              previous[index].value != value else { return }
        var next = previous
        next[index].value = value
        settings[plugin] = next
        issue = nil
        let client = client
        Task {
            do {
                settings[plugin] = try await client.savePluginSettings(name: plugin, values: [key: value])
                await loadTransports()
            } catch {
                settings[plugin] = previous
                issue = "Couldn’t save the setting."
            }
        }
    }

    public func plugin(named name: String?) -> Plugin? {
        installed.first { $0.name == name }
    }
}
