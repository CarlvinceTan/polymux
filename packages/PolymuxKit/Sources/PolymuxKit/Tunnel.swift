import Foundation
import IrohLib
import Network
import OSLog

/// Reaches a server from any network over iroh and serves it on a local port, so `APIClient`
/// keeps plain HTTP. iroh relays first, then upgrades to a direct path when one exists.
public actor Tunnel {
    /// dumbpipe's protocol: this ALPN, then a fixed handshake on each stream.
    static let alpn = Data("DUMBPIPEV0".utf8)
    static let handshake = Data("hello".utf8)

    private let remote: EndpointAddr
    private var endpoint: Endpoint?
    private var listener: NWListener?
    /// The local port, kept so a rebuilt listener reuses the base URL clients already hold.
    private var port: NWEndpoint.Port?
    private var generation = 0
    private var opening: Task<URL, Error>?
    private var resuming = false
    private var connections: [UUID: NWConnection] = [:]
    private var forwarding: [UUID: Task<Void, Never>] = [:]
    private var recovery: Task<Void, Never>?
    private let makeListener: @Sendable (NWParameters) throws -> NWListener
    private static let log = Logger(subsystem: "com.flarehq.polymux", category: "Tunnel")

    public init(ticket: String) throws {
        remote = try EndpointTicket.fromString(str: ticket).endpointAddr()
        makeListener = { try NWListener(using: $0) }
    }

    init(ticket: String, listener: @escaping @Sendable (NWParameters) throws -> NWListener) throws {
        remote = try EndpointTicket.fromString(str: ticket).endpointAddr()
        makeListener = listener
    }

    deinit {
        listener?.cancel()
        for connection in connections.values { connection.cancel() }
        for task in forwarding.values { task.cancel() }
        recovery?.cancel()
        let retired = endpoint
        Task { try? await retired?.close() }
    }

    /// Uses the direct address when it answers quickly, otherwise the ticket's tunnel.
    public static func route(_ direct: URL, ticket: String?, token: String?) async throws -> (URL, Tunnel?) {
        guard let ticket else {
            // Plain HTTP on an ordinary network would expose the credential.
            guard trusted(direct) else { throw ClientError.response(0, "Pair this device again.") }
            return (direct, nil)
        }
        if await reachable(direct, token: token) { return (direct, nil) }
        let tunnel = try Tunnel(ticket: ticket)
        return (try await tunnel.start(), tunnel)
    }

    /// Only this server accepting the credential counts: a refusal, or another server on the
    /// same address (such as this Mac's own), means the direct route doesn't reach it. The
    /// credential is only ever sent where plain HTTP stays private.
    public static func reachable(_ url: URL, token: String?, session: URLSession = .shared) async -> Bool {
        guard trusted(url) else { return false }
        var request = URLRequest(url: url.appending(path: "health"))
        request.timeoutInterval = 1.5
        if let token { request.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        request.setValue("polymux-kit", forHTTPHeaderField: "X-Polymux-Client")
        return (try? await session.data(for: request).1 as? HTTPURLResponse)?.statusCode == 200
    }

    /// Where a pairing is kept on this device. A loopback address only names this device's own server, so a tunnel
    /// invite from another machine, whose credential that server doesn't accept, is kept under the tunnel's identity.
    public static func address(for url: URL, ticket: String?, token: String?,
                               knownTicket: String? = nil, session: URLSession = .shared) async -> URL {
        guard let ticket, loopback(url), let id = identity(ticket) else { return url }
        // Re-pairing the same endpoint keeps its cache and settings even while it is offline.
        if let knownTicket, identity(knownTicket) == id { return url }
        guard !(await reachable(url, token: token, session: session)) else { return url }
        return URL(string: "http://\(id).tunnel.invalid")!
    }

    /// The address a pairing that only reaches a server through a tunnel is kept under.
    public static func address(ticket: String) -> URL? {
        identity(ticket).flatMap { URL(string: "http://\($0).tunnel.invalid") }
    }

    /// A stable short name for the server a ticket reaches, usable as a host name.
    static func identity(_ ticket: String) -> String? {
        guard let remote = try? EndpointTicket.fromString(str: ticket).endpointAddr() else { return nil }
        return remote.id().toBytes().prefix(8).map { String(format: "%02x", $0) }.joined()
    }

    static func loopback(_ url: URL) -> Bool {
        guard let host = url.host()?.trimmingCharacters(in: CharacterSet(charactersIn: "[]")).lowercased() else { return false }
        if host == "localhost" { return true }
        if let ip = IPv4Address(host) { return ip.rawValue.first == 127 }
        return IPv6Address(host) == .loopback
    }

    /// An address alone cannot prove a VPN route; only loopback permits plain HTTP.
    static func trusted(_ url: URL) -> Bool {
        if url.scheme?.lowercased() == "https" { return true }
        return url.scheme?.lowercased() == "http" && loopback(url)
    }

    /// Starts the local side and returns its base URL.
    public func start() async throws -> URL {
        if let opening { return try await opening.value }
        if let port, endpoint != nil { return URL(string: "http://127.0.0.1:\(port.rawValue)")! }
        generation += 1
        let epoch = generation
        let task = Task { try await self.open(epoch) }
        opening = task
        do {
            let base = try await task.value
            if generation == epoch { opening = nil }
            return base
        } catch {
            if generation == epoch { opening = nil }
            throw error
        }
    }

    private func open(_ epoch: Int) async throws -> URL {
        let fresh = try await Self.bind()
        do {
            guard generation == epoch else { throw CancellationError() }
            try Task.checkCancellation()
            endpoint = fresh
            let bound = try await listen(on: .any, epoch: epoch)
            guard generation == epoch else { throw CancellationError() }
            port = NWEndpoint.Port(rawValue: bound)
            return URL(string: "http://127.0.0.1:\(bound)")!
        } catch {
            if generation == epoch { listener?.cancel(); listener = nil; endpoint = nil }
            try? await fresh.close()
            throw error
        }
    }

    /// Rebuilds what the system reclaims while an iOS app is suspended: the iroh endpoint and
    /// the local port, which keeps its number so the base URL stays valid.
    public func resume() async {
        guard !resuming, opening == nil, let old = endpoint, let port else { return }
        resuming = true
        recovery?.cancel()
        recovery = nil
        generation += 1
        let epoch = generation
        defer { if generation == epoch { resuming = false } }
        let fresh = try? await Self.bind()
        guard generation == epoch else { try? await fresh?.close(); return }
        cancelConnections()
        if let fresh {
            endpoint = fresh
            try? await old.close()
        }
        // A reclaimed socket can still report ready, so always reopen it.
        await relisten(port, epoch: epoch)
    }

    /// Reopens a listener that failed, after a pause so a busy port can't spin.
    private func revive(_ epoch: Int) {
        guard generation == epoch, recovery == nil else { return }
        recovery = Task { [weak self] in
            do { try await Task.sleep(for: .seconds(1)) }
            catch { return }
            await self?.retry(epoch)
        }
    }

    private func retry(_ epoch: Int) async {
        guard generation == epoch else { return }
        recovery = nil
        if let port { await relisten(port, epoch: epoch) }
    }

    private func relisten(_ port: NWEndpoint.Port, epoch: Int) async {
        guard generation == epoch, endpoint != nil else { return }
        listener?.cancel()
        listener = nil
        do { _ = try await listen(on: port, epoch: epoch) }
        catch {
            guard generation == epoch else { return }
            listener?.cancel()
            listener = nil
            Self.log.error("Could not reopen the local encrypted connection listener.")
            revive(epoch)
        }
    }

    private static func bind() async throws -> Endpoint {
        let builder = EndpointBuilder()
        builder.applyN0()
        return try await builder.bind()
    }

    private func listen(on port: NWEndpoint.Port, epoch: Int) async throws -> UInt16 {
        let parameters = NWParameters.tcp
        parameters.requiredLocalEndpoint = .hostPort(host: "127.0.0.1", port: port)
        parameters.allowLocalEndpointReuse = true
        let listener = try makeListener(parameters)
        self.listener = listener
        listener.newConnectionHandler = { [weak self] connection in
            Task { await self?.accept(connection, epoch: epoch) }
        }
        let bound = try await Self.ready(listener) { [weak self] in
            Task { await self?.revive(epoch) }
        }
        return bound
    }

    private func accept(_ connection: NWConnection, epoch: Int) async {
        guard generation == epoch, let endpoint else { return connection.cancel() }
        let remote = remote
        let id = UUID()
        connections[id] = connection
        forwarding[id] = Task.detached { [weak self] in
            await Self.forward(connection, endpoint: endpoint, remote: remote)
            await self?.finished(id)
        }
    }

    private func finished(_ id: UUID) {
        connections[id] = nil
        forwarding[id] = nil
    }

    private func cancelConnections() {
        for connection in connections.values { connection.cancel() }
        for task in forwarding.values { task.cancel() }
        connections.removeAll()
        forwarding.removeAll()
    }

    public func stop() async {
        generation += 1
        opening?.cancel()
        opening = nil
        resuming = false
        recovery?.cancel()
        recovery = nil
        listener?.cancel()
        listener = nil
        port = nil
        cancelConnections()
        let retired = endpoint
        endpoint = nil
        try? await retired?.close()
    }

    private static func ready(_ listener: NWListener, failed: @escaping @Sendable () -> Void) async throws -> UInt16 {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<UInt16, Error>) in
            let once = Once()
            listener.stateUpdateHandler = { state in
                switch state {
                case .ready:
                    if let port = listener.port?.rawValue, once.claim() { continuation.resume(returning: port) }
                case .failed(let error):
                    if once.claim() { continuation.resume(throwing: error) } else { failed() }
                case .cancelled:
                    if once.claim() { continuation.resume(throwing: CancellationError()) }
                default:
                    break
                }
            }
            listener.start(queue: .global(qos: .userInitiated))
        }
    }

    /// One iroh connection per local TCP connection, as dumbpipe expects.
    private static func forward(_ local: NWConnection, endpoint: Endpoint, remote: EndpointAddr) async {
        local.start(queue: .global(qos: .userInitiated))
        do {
            let connection = try await endpoint.connect(addr: remote, alpn: alpn)
            let stream = try await connection.openBi()
            let send = stream.send()
            let recv = stream.recv()
            try await send.writeAll(buf: handshake)
            await withTaskGroup(of: Void.self) { group in
                group.addTask {
                    while let data = try? await local.read(), !data.isEmpty {
                        guard (try? await send.writeAll(buf: data)) != nil else { break }
                    }
                    try? await send.finish()
                }
                group.addTask {
                    while let data = try? await recv.read(sizeLimit: 64 << 10), !data.isEmpty {
                        guard (try? await local.write(data)) != nil else { break }
                    }
                    local.send(content: nil, contentContext: .finalMessage, isComplete: true, completion: .idempotent)
                }
            }
            try? connection.close(errorCode: 0, reason: Data())
        } catch {
            if !Task.isCancelled {
                Self.log.error("Encrypted server connection failed. Check server and relay reachability (code \((error as NSError).code)).")
            }
        }
        local.cancel()
    }
}

private final class Once: @unchecked Sendable {
    private let lock = NSLock()
    private var done = false

    func claim() -> Bool {
        lock.withLock {
            defer { done = true }
            return !done
        }
    }
}

private extension NWConnection {
    /// Next chunk from the peer; empty at end of stream.
    func read() async throws -> Data {
        try await withCheckedThrowingContinuation { continuation in
            receive(minimumIncompleteLength: 1, maximumLength: 64 << 10) { data, _, _, error in
                if let error { continuation.resume(throwing: error) }
                else { continuation.resume(returning: data ?? Data()) }
            }
        }
    }

    func write(_ data: Data) async throws {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<Void, Error>) in
            send(content: data, completion: .contentProcessed { error in
                if let error { continuation.resume(throwing: error) } else { continuation.resume() }
            })
        }
    }
}
