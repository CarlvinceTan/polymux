import Foundation
import IrohLib
import Network

/// Reaches a server from any network over iroh and serves it on a local port, so `APIClient`
/// keeps plain HTTP. iroh relays first, then upgrades to a direct path when one exists.
public actor Tunnel {
    /// dumbpipe's protocol: this ALPN, then a fixed handshake on each stream.
    static let alpn = Data("DUMBPIPEV0".utf8)
    static let handshake = Data("hello".utf8)

    private let remote: EndpointAddr
    private var endpoint: Endpoint?
    private var listener: NWListener?

    public init(ticket: String) throws {
        remote = try EndpointTicket.fromString(str: ticket).endpointAddr()
    }

    deinit { listener?.cancel() }

    /// Uses the direct address when it answers quickly, otherwise the ticket's tunnel.
    public static func route(_ direct: URL, ticket: String?) async throws -> (URL, Tunnel?) {
        guard let ticket, !(await reachable(direct)) else { return (direct, nil) }
        let tunnel = try Tunnel(ticket: ticket)
        return (try await tunnel.start(), tunnel)
    }

    /// Any HTTP answer, even 401, means the server is reachable at this address.
    static func reachable(_ url: URL) async -> Bool {
        var request = URLRequest(url: url.appending(path: "health"))
        request.timeoutInterval = 1.5
        return (try? await URLSession.shared.data(for: request).1) is HTTPURLResponse
    }

    /// Starts the local side and returns its base URL.
    public func start() async throws -> URL {
        let builder = EndpointBuilder()
        builder.applyN0()
        let endpoint = try await builder.bind()
        self.endpoint = endpoint
        let parameters = NWParameters.tcp
        parameters.requiredLocalEndpoint = .hostPort(host: "127.0.0.1", port: .any)
        let listener = try NWListener(using: parameters)
        self.listener = listener
        let remote = self.remote
        listener.newConnectionHandler = { connection in
            Task { await Self.forward(connection, endpoint: endpoint, remote: remote) }
        }
        let port = try await Self.ready(listener)
        return URL(string: "http://127.0.0.1:\(port)")!
    }

    public func stop() async {
        listener?.cancel()
        listener = nil
        try? await endpoint?.close()
        endpoint = nil
    }

    private static func ready(_ listener: NWListener) async throws -> UInt16 {
        try await withCheckedThrowingContinuation { (continuation: CheckedContinuation<UInt16, Error>) in
            let once = Once()
            listener.stateUpdateHandler = { state in
                switch state {
                case .ready:
                    if let port = listener.port?.rawValue, once.claim() { continuation.resume(returning: port) }
                case .failed(let error):
                    if once.claim() { continuation.resume(throwing: error) }
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
        } catch {}
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
