import Foundation
import Darwin
import IrohLib
import Network
import Testing
@testable import PolymuxKit

/// Crosses n0's relays with a real dumbpipe sidecar when one is available.
@Suite(.enabled(if: ProcessInfo.processInfo.environment["POLYMUX_DUMBPIPE"] != nil))
struct TunnelTests {
    @Test func failedRebindRetriesUntilTheListenerRecovers() async throws {
        let probe = ListenerProbe()
        let ticket = try EndpointTicket.fromAddr(addr: EndpointAddr(id: SecretKey.generate().public(), relayUrl: nil, addresses: [])).description
        let tunnel = try Tunnel(ticket: ticket, listener: probe.make)
        let base = try await tunnel.start()
        await tunnel.resume()
        var recovered = false
        for _ in 0..<160 {
            if probe.attempts >= 4 { recovered = await Task.detached { portAccepts(base.port!) }.value }
            if recovered { break }
            try await Task.sleep(for: .milliseconds(25))
        }
        await tunnel.stop()
        #expect(recovered)
    }

    @Test func concurrentStartAndStoppedResumeKeepOneListener() async throws {
        let ticket = try EndpointTicket.fromAddr(addr: EndpointAddr(id: SecretKey.generate().public(), relayUrl: nil, addresses: [])).description
        let tunnel = try Tunnel(ticket: ticket)
        async let first = tunnel.start()
        async let second = tunnel.start()
        let (a, b) = try await (first, second)
        #expect(a == b)
        let port = try #require(a.port)
        #expect(await Task.detached { portAccepts(port) }.value)
        async let resume: Void = tunnel.resume()
        await Task.yield()
        await tunnel.stop()
        await resume
        var stopped = false
        for _ in 0..<20 {
            stopped = await Task.detached { !portAccepts(port) }.value
            if stopped { break }
            try await Task.sleep(for: .milliseconds(25))
        }
        #expect(stopped)
        _ = try await tunnel.start()
        await tunnel.stop()
    }

    @Test func requestsReachTheServerThroughTheTicket() async throws {
        let backend = try Process.run(URL(fileURLWithPath: "/usr/bin/python3"),
                                      arguments: ["-m", "http.server", "18090", "--bind", "127.0.0.1"])
        defer { backend.terminate() }
        let errors = Pipe()
        let sidecar = Process()
        sidecar.executableURL = URL(fileURLWithPath: ProcessInfo.processInfo.environment["POLYMUX_DUMBPIPE"]!)
        sidecar.arguments = ["listen-tcp", "--host", "127.0.0.1:18090"]
        sidecar.standardError = errors
        try sidecar.run()
        defer { sidecar.terminate() }
        var ticket = ""
        for try await line in errors.fileHandleForReading.bytes.lines {
            if let field = line.split(separator: " ").first(where: { $0.hasPrefix("endpoint") && $0.count > 40 }) {
                ticket = String(field)
                break
            }
        }
        let tunnel = try Tunnel(ticket: ticket)
        let base = try await tunnel.start()
        var body = ""
        for _ in 0..<20 where body.isEmpty {
            if let (data, _) = try? await URLSession.shared.data(from: base) {
                body = String(decoding: data, as: UTF8.self)
            } else {
                try await Task.sleep(for: .milliseconds(500))
            }
        }
        await tunnel.stop()
        #expect(body.contains("Directory listing"))
    }
}

private final class ListenerProbe: @unchecked Sendable {
    private let lock = NSLock()
    private var count = 0
    var attempts: Int { lock.withLock { count } }

    func make(_ parameters: NWParameters) throws -> NWListener {
        let attempt = lock.withLock { count += 1; return count }
        if [2, 3].contains(attempt) { throw NWError.posix(.EADDRINUSE) }
        return try NWListener(using: parameters)
    }
}

private func portAccepts(_ port: Int) -> Bool {
    let descriptor = socket(AF_INET, SOCK_STREAM, 0)
    guard descriptor >= 0 else { return false }
    defer { close(descriptor) }
    var address = sockaddr_in()
    address.sin_len = UInt8(MemoryLayout<sockaddr_in>.size)
    address.sin_family = sa_family_t(AF_INET)
    address.sin_port = UInt16(port).bigEndian
    address.sin_addr.s_addr = inet_addr("127.0.0.1")
    return withUnsafePointer(to: &address) {
        $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
            Darwin.connect(descriptor, $0, socklen_t(MemoryLayout<sockaddr_in>.size)) == 0
        }
    }
}
