import Foundation
import Network

/// Keeps the server's identity stable while its direct or encrypted route changes underneath it.
public actor ServerRoute {
    private var direct: URL
    private var ticket: String?
    private var token: String?
    private var also: URL?
    private var base: URL?
    /// The address the current route was opened for.
    private var startedAt: URL?
    private var tunnel: Tunnel?
    private var opening: Task<(URL, Tunnel?), any Error>?
    private var checking = false
    private var checkedAt: ContinuousClock.Instant?
    private var generation = 0
    private var monitor: NWPathMonitor?
    private var pathSeen = false

    public init(url: URL, ticket: String?, token: String?, also: URL? = nil) {
        direct = url
        self.ticket = ticket
        self.token = token
        self.also = also
    }

    /// Learns where a server is moving, ahead of its own address going quiet.
    public func follow(_ url: URL, ticket: String?, token: String) {
        also = url
        if let ticket { self.ticket = ticket }
        self.token = token
    }

    public func resolve() async throws -> URL {
        if let base { return base }
        startedAt = direct
        let epoch = generation
        let task: Task<(URL, Tunnel?), any Error>
        if let opening { task = opening }
        else {
            let (direct, ticket, token) = (direct, ticket, token)
            task = Task { try await Tunnel.route(direct, ticket: ticket, token: token) }
            opening = task
        }
        do {
            let (url, tunnel) = try await task.value
            guard generation == epoch else {
                await tunnel?.stop()
                throw CancellationError()
            }
            base = url
            self.tunnel = tunnel
            opening = nil
            watch()
            return url
        } catch {
            if generation == epoch { opening = nil }
            throw error
        }
    }

    public func recover() async {
        guard ticket != nil || also != nil, !checking else { return }
        if let checkedAt, checkedAt.duration(to: .now) < .seconds(2) { return }
        checking = true
        checkedAt = .now
        let epoch = generation
        let reachable = await Tunnel.reachable(direct, token: token)
        defer { checking = false }
        if !reachable, let also, await Tunnel.reachable(also, token: token) {
            direct = also
            self.also = nil
        }
        guard epoch == generation, reachable == (tunnel != nil) || direct != startedAt else { return }
        generation += 1
        let retired = tunnel
        tunnel = nil
        base = nil
        opening?.cancel()
        opening = nil
        await retired?.stop()
    }

    public func resume() async {
        await tunnel?.resume()
        checkedAt = nil
        await recover()
    }

    public func stop() async {
        generation += 1
        monitor?.cancel()
        monitor = nil
        pathSeen = false
        let pending = opening
        opening = nil
        pending?.cancel()
        let retired = tunnel
        tunnel = nil
        base = nil
        await retired?.stop()
        if let result = try? await pending?.value { await result.1?.stop() }
    }

    private func watch() {
        guard monitor == nil else { return }
        let monitor = NWPathMonitor()
        self.monitor = monitor
        monitor.pathUpdateHandler = { [weak self] _ in
            Task { await self?.changed() }
        }
        monitor.start(queue: .global(qos: .utility))
    }

    private func changed() async {
        guard pathSeen else { pathSeen = true; return }
        await resume()
    }
}
