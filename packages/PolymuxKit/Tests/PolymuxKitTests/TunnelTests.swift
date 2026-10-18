import Foundation
import Testing
@testable import PolymuxKit

/// Crosses n0's relays with a real dumbpipe sidecar when one is available.
@Suite(.enabled(if: ProcessInfo.processInfo.environment["POLYMUX_DUMBPIPE"] != nil))
struct TunnelTests {
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
