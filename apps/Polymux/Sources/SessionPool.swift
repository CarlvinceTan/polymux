import Foundation
import Observation
import PolymuxKit
#if os(iOS)
import UIKit
#endif

/// One live session per known server, so switching is instant and every server keeps alerting.
@MainActor @Observable
final class SessionPool {
    let servers: ServerStore
    private(set) var sessions: [String: AppSession] = [:]
    private(set) var issues: [String: String] = [:]
    /// The last session opened. A window leaving the screen still draws avatars after its own
    /// session is gone, so this outlives every other reference.
    private(set) var latest: AppSession?
    private var links: [String: Task<Void, Never>] = [:]
    private var prepared: Set<String> = []
    private var active = ""
    private(set) var linking: Set<String> = []
    private var naming: Set<String> = []
    private var following: Set<String> = []
    private(set) var linkIssue: String?

    init(servers: ServerStore) {
        self.servers = servers
    }

    /// Connects every saved server, and readies the one on show.
    func show(_ address: String) {
        active = address
        servers.remember(address)
        Notifications.shared.current = address
        Connections.shared.current = address
        for device in servers.devices where links[device.address] == nil { start(device.address) }
        if let session = sessions[address] { prepare(session) }
    }

    /// A new pairing replaces the credential the old connection holds.
    func restart(_ address: String) {
        links[address]?.cancel()
        start(address)
    }

    func select(_ address: String) {
        UserDefaults.standard.set(address, forKey: "serverAddress")
    }

    /// Saves a pairing and connects to it. The returned address is where it's kept.
    @discardableResult
    func pair(_ invite: Invite, name: String? = nil, serverID: String? = nil) async throws -> String {
        guard let parsed = ServerAddress.parse(invite.url) else {
            throw ClientError.response(400, "Paste a valid Polymux pairing link.")
        }
        let token = try await Pairing.redeem(invite)
        let known = await Task.detached {
            try? ServerCredential.load(parsed.absoluteString)?.ticket
        }.value
        let kept = await Tunnel.address(for: parsed, ticket: invite.ticket, token: token, knownTicket: known)
        try Task.checkCancellation()
        let target = kept.absoluteString
        do {
            try await ServerCredential(token: token, ticket: invite.ticket).save(target)
        } catch is CancellationError {
            throw CancellationError()
        } catch {
            throw ClientError.response(0, "Couldn’t save this connection on this device.")
        }
        // Publishing is the commit point; even cancellation must reconnect once it succeeds.
        // A tunnel-only server's name arrives once connected.
        servers.remember(target, name: name ?? (kept == parsed ? nil : "Server"), serverID: serverID)
        restart(target)
        await Pairing.finish(invite)
        return target
    }

    /// Moving or merging needs the two servers to trust each other, so they link first.
    func link(_ source: String, to target: String) async -> Bool {
        guard let here = sessions[source], let there = sessions[target], let id = servers.devices.first(where: { $0.address == target })?.serverID,
              linking.insert(target).inserted else { return false }
        defer { linking.remove(target) }
        linkIssue = nil
        if here.network.value?.peers?.contains(where: { $0.id == id }) == true { return true }
        do {
            try await here.client.linkPeer(there.client.peerInvite())
            await here.network.refresh()
            await there.network.refresh()
            return true
        } catch {
            linkIssue = "Couldn’t reach \(servers.devices.first { $0.address == target }?.title ?? "that network")."
            return false
        }
    }

    /// Starts or stops this Mac's own server. Its cache stays, in case it comes back.
    func host(_ on: Bool) async {
        let host = ServerHost.shared
        if on {
            await vacate(ServerHost.address)
            await host.start()
            guard host.running else { return }
            servers.remember(ServerHost.address)
            show(active)
        } else {
            await host.stop()
            guard !host.running else { return }
            drop(ServerHost.address)
            if ServerHost.address == active, let next = servers.known.first ?? servers.devices.first { select(next.address) }
        }
    }

    /// A Mac joining a network doesn't need the empty server it came with.
    func retireEmptyHost(joined target: String) async {
        let host = ServerHost.shared
        guard target != ServerHost.address, host.managed, let session = sessions[ServerHost.address],
              (try? await session.client.network())?.empty == true else { return }
        await self.host(false)
    }

    /// Whether this Mac's own server holds data, beside the network it just joined.
    func keepsData(joined target: String) async -> Bool {
        guard target != ServerHost.address, ServerHost.shared.managed, let session = sessions[ServerHost.address] else { return false }
        return (try? await session.client.network())?.empty == false
    }

    /// Forgets a server on this device: its connection, credential and cache.
    func remove(_ address: String) {
        drop(address)
        try? ClientPaths.forget(address)
        if address == active, let next = servers.known.first ?? servers.devices.first { select(next.address) }
    }

    private func drop(_ address: String) {
        links.removeValue(forKey: address)?.cancel()
        sessions[address] = nil
        issues[address] = nil
        prepared.remove(address)
        Notifications.shared.disconnect(address)
        Connections.shared.disconnect(address)
        servers.forget(address)
    }

    /// Records who answered at this address; the same server saved twice keeps the address on show.
    func report(_ session: AppSession) {
        let address = session.address
        if address == ServerHost.address { ServerHost.shared.refresh() }
        servers.deny(address, session.network.denied)
        if session.network.denied, address == active, let next = servers.known.first { select(next.address) }
        guard let info = session.network.value, !session.network.denied else { return }
        let named = info.server.network.flatMap { $0.isEmpty ? nil : $0 }
        servers.remember(address, name: info.server.name, serverID: info.server.id, network: named)
        if named == nil { nameNetwork(session) }
        if let moving = info.moving, moving.merge != true { follow(moving, session) } else { following.remove(address) }
        for gone in info.merged ?? [] { forgetMerged(gone, into: address) }
        guard let id = info.server.id else { return }
        for twin in servers.devices where twin.serverID == id && twin.address != address {
            // This Mac's own address beats a route through the network to the same server.
            let leave = twin.address == ServerHost.address || (address != active && address != ServerHost.address) ? address : twin.address
            if leave == active { select(leave == address ? twin.address : address) }
            drop(leave)
            return
        }
    }

    /// The first device to reach an unnamed server gives it its next free number.
    /// It's kept on the server, so every device shows the same name.
    private func nameNetwork(_ session: AppSession) {
        guard naming.insert(session.address).inserted else { return }
        let taken = Set(servers.devices.compactMap(\.network))
        let number = (1...).first { !taken.contains("Network \($0)") } ?? 1
        Task {
            do {
                try await session.client.nameNetwork("Network \(number)", first: true)
                await session.network.refresh()
                report(session)
            } catch {
                if error.isOffline { naming.remove(session.address) }
            }
        }
    }

    /// A server that was absorbed won't be back, and its chats are here now.
    private func forgetMerged(_ id: String, into address: String) {
        for device in servers.devices where device.serverID == id && device.address != address {
            drop(device.address)
            try? ClientPaths.forget(device.address)
            if device.address == active { select(address) }
        }
    }

    /// Saves where a server is going, so this device reaches it there with the same credential.
    private func follow(_ moving: NetworkInfo.Moving, _ session: AppSession) {
        let address = session.address
        guard following.insert(address).inserted, let url = URL(string: moving.url) else { return }
        let local = session.network.value.map { $0.current == $0.host } ?? false
        Task {
            // The app beside the server loses the local credential along with the data.
            var token = local ? try? await session.client.ownDevice() : nil
            if token == nil { token = await session.client.currentToken() }
            guard let token else { return }
            let saved = await Task.detached { try? ServerCredential.load(address) }.value
            let ticket = moving.ticket ?? saved?.ticket
            try? await ServerCredential(token: token, ticket: ticket, also: moving.url).save(address)
            await session.route?.follow(url, ticket: ticket, token: token)
        }
    }

    /// Hands this Mac's address back from a server that moved away, keeping the way to it.
    private func vacate(_ address: String) async {
        guard let device = servers.devices.first(where: { $0.address == address }),
              let saved = await Task.detached(operation: { try? ServerCredential.load(address) }).value,
              let also = saved.also,
              let target = saved.ticket.flatMap(Tunnel.address(ticket:))?.absoluteString ?? Optional(also) else { return }
        try? await ServerCredential(token: saved.token, ticket: saved.ticket).save(target)
        drop(address)
        // What's saved here belongs to the server that left, not the one about to start.
        try? ClientPaths.forget(address)
        servers.remember(target, name: device.name, serverID: device.serverID)
        restart(target)
        if address == active { select(target) }
    }

    /// Every server's switcher entries, once there's a second server to show.
    var tabs: [PluginTab] {
        let shown = servers.known.compactMap { server in sessions[server.address].map { (server, $0) } }
        guard shown.count > 1 else { return [] }
        let marks = PluginTab.marks(shown.map(\.0.title))
        return zip(shown, marks).flatMap { entry, mark in
            let (server, session) = entry
            let tabs = session.plugins.tabs.isEmpty ? [PluginTab(id: PluginTab.chats, plugin: nil)] : session.plugins.tabs
            return tabs.map {
                PluginTab(id: "\(server.address)#\($0.id)", plugin: $0.plugin, server: server.address,
                          serverName: server.title, serverMark: mark)
            }
        }
    }

    func unread(_ tab: PluginTab) -> Int {
        guard let address = tab.server, let session = sessions[address] else { return 0 }
        return PluginSurface.unread(tab, session: session)
    }

    func resume() {
        for session in sessions.values {
            if let route = session.route { Task { await route.resume() } }
        }
    }

    private func start(_ address: String) {
        links[address] = Task { [weak self] in await self?.run(address) }
    }

    private func run(_ address: String) async {
        var current: AppSession?
        defer {
            let retired = current?.route
            Task { await retired?.stop() }
        }
        while !Task.isCancelled {
            do {
                let opened = try await AppSession(address: address)
                current = opened
                try Task.checkCancellation()
                sessions[address] = opened
                latest = opened
                issues[address] = nil
                prepared.remove(address)
                connected(opened)
                var round = 0
                while !Task.isCancelled {
                    await opened.settings.refresh()
                    if !opened.settings.isConnected { await opened.route?.recover() }
                    if address != active { await keepFresh(opened, fully: round % 3 == 0) }
                    round += 1
                    do { try await Task.sleep(for: .seconds(address == active ? 3 : 5)) }
                    catch { break }
                }
            } catch {
                guard !Task.isCancelled else { return }
                sessions[address] = nil
                issues[address] = error.isOffline ? "Polymux keeps trying to reconnect." : "Check the server in Connection settings."
            }
            if Task.isCancelled { break }
            try? await Task.sleep(for: .seconds(address == active ? 2 : 15))
        }
    }

    private func connected(_ session: AppSession) {
        Task { await announce(session) }
        if session.address == active { prepare(session) }
        #if DEBUG
        guard !VerifyFixture.local else { return }
        #endif
        Notifications.shared.connect(session.client, address: session.address)
        Connections.shared.connect(session.client, address: session.address)
    }

    /// Warms what's likely to be opened, for the first time a server is on show.
    private func prepare(_ session: AppSession) {
        guard prepared.insert(session.address).inserted else { return }
        Task { await session.prepare() }
    }

    /// A server off screen still needs its unread count and plugins, and it has to be noticed
    /// quickly if it stops allowing this device.
    private func keepFresh(_ session: AppSession, fully: Bool) async {
        if fully {
            await session.sidebar.refresh()
            await session.plugins.refresh()
        }
        await session.network.refresh()
        report(session)
    }

    /// Names this device on the server so the Devices list can tell it apart.
    private func announce(_ session: AppSession) async {
        #if os(macOS)
        let name = await Task.detached { Host.current().localizedName ?? "Mac" }.value
        #else
        let name = UIDevice.current.name
        #endif
        try? await session.client.renameDevice(name, client: Notifications.shared.installation)
        await session.network.refresh()
        report(session)
    }
}
