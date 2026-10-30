import Foundation
import Network
import Testing
@testable import PolymuxKit

/// Opt-in checks against a run.sh instance published through an HTTPS tunnel.
@Suite(.enabled(if: ProcessInfo.processInfo.environment["POLYMUX_TEST_REMOTE"] != nil))
struct RemoteTests {
    @Test(arguments: ProcessInfo.processInfo.environment["POLYMUX_TEST_REMOTE", default: ""].split(separator: ",").map(String.init))
    func pairingAndHeartbeatStreamUseHTTPS(_ address: String) async throws {
        let environment = ProcessInfo.processInfo.environment
        let base = try #require(URL(string: address))
        let tokenFile = try #require(environment["POLYMUX_TEST_TOKEN_FILE"])
        let owner = try String(contentsOfFile: tokenFile, encoding: .utf8).trimmingCharacters(in: .whitespacesAndNewlines)
        let session = URLSession(configuration: .ephemeral)
        defer { session.invalidateAndCancel() }
        let client = APIClient(baseURL: base, session: session, token: owner)
        let invite = try await client.invite()
        #expect(invite.ticket == nil)
        let directory = FileManager.default.temporaryDirectory.appending(path: "polymux-remote-\(UUID())")
        defer { try? FileManager.default.removeItem(at: directory) }
        let pairing = Invite(url: base.absoluteString, token: invite.token)
        let token = try await Pairing.redeem(pairing, pending: directory.appending(path: "claim"), session: session)
        let paired = APIClient(baseURL: base, session: session, token: token)
        _ = try await paired.threads()
        try await paired.reportClient(ClientState(platform: ClientStatePlatform.macos, timezone: "Asia/Singapore"))
        // More than the idle deadline: server heartbeats must keep an otherwise quiet stream alive.
        let stream = Task {
            for try await _ in paired.clientRequests() { }
            try Task.checkCancellation()
            throw ClientError.unavailable
        }
        defer { stream.cancel() }
        try await Task.sleep(for: .seconds(50))
        stream.cancel()
        do { try await stream.value }
        catch {
            #expect(error is CancellationError || (error as? URLError)?.code == .cancelled,
                    "Unexpected stream failure: \(String(reflecting: error))")
        }
    }

    @Test func stalledStreamTimesOut() async throws {
        let listener = try NWListener(using: .tcp, on: .any)
        let connections = RemoteSockets()
        listener.newConnectionHandler = { connection in
            connections.keep(connection)
            connection.start(queue: .global())
            connection.receive(minimumIncompleteLength: 1, maximumLength: 8192) { _, _, _, _ in
                let response = "HTTP/1.1 200 OK\r\nContent-Type: text/event-stream\r\nConnection: keep-alive\r\n\r\n: ready\n\n"
                connection.send(content: Data(response.utf8), completion: .contentProcessed { _ in })
            }
        }
        defer { listener.cancel(); connections.close() }
        let port: UInt16 = try await withCheckedThrowingContinuation { continuation in
            listener.stateUpdateHandler = { state in
                switch state {
                case .ready: continuation.resume(returning: listener.port!.rawValue)
                case .failed(let error): continuation.resume(throwing: error)
                default: break
                }
            }
            listener.start(queue: .global())
        }
        let session = URLSession(configuration: .ephemeral)
        defer { session.invalidateAndCancel() }
        let client = APIClient(baseURL: URL(string: "http://127.0.0.1:\(port)")!, session: session, token: "isolated")
        let started = ContinuousClock.now
        do {
            for try await _ in client.clientRequests() { }
            Issue.record("A stalled stream ended without reporting a connection failure")
        } catch {
            #expect((error as? URLError)?.code == .timedOut)
            #expect(started.duration(to: .now) < .seconds(60))
        }
    }
}

private final class RemoteSockets: @unchecked Sendable {
    private let lock = NSLock()
    private var values: [NWConnection] = []
    func keep(_ value: NWConnection) { lock.withLock { values.append(value) } }
    func close() { lock.withLock { values.forEach { $0.cancel() }; values.removeAll() } }
}
