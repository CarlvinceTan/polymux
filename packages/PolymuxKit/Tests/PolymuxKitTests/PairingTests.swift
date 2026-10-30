import Foundation
import Testing
@testable import PolymuxKit

private final class PairingProtocol: URLProtocol, @unchecked Sendable {
    typealias Handler = @Sendable (URLRequest) throws -> (Int, Data)
    private static let lock = NSLock()
    nonisolated(unsafe) private static var handlers: [String: Handler] = [:]

    static func register(_ handler: @escaping Handler) -> URL {
        let host = "pairing-\(UUID().uuidString.lowercased()).test"
        lock.withLock { handlers[host] = handler }
        return URL(string: "https://\(host)")!
    }

    static func remove(_ url: URL) { _ = lock.withLock { handlers.removeValue(forKey: url.host!) } }
    override class func canInit(with request: URLRequest) -> Bool { request.url?.host?.hasPrefix("pairing-") == true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        do {
            guard let url = request.url, let handler = Self.lock.withLock({ Self.handlers[url.host!] }) else { throw URLError(.badURL) }
            let (status, body) = try handler(request)
            client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: status, httpVersion: "HTTP/1.1", headerFields: nil)!, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: body)
            client?.urlProtocolDidFinishLoading(self)
        } catch { client?.urlProtocol(self, didFailWithError: error) }
    }
    override func stopLoading() {}
}

private final class PairingPeer: @unchecked Sendable {
    private let lock = NSLock()
    private var accepted: String?
    private var attempts = 0
    var posts: Int { lock.withLock { attempts } }

    func handle(_ request: URLRequest) throws -> (Int, Data) {
        try lock.withLock {
            if request.url?.path == "/health" {
                return (accepted != nil && request.value(forHTTPHeaderField: "Authorization") == "Bearer \(accepted!)" ? 200 : 401, Data())
            }
            #expect(request.httpMethod == "POST")
            #expect(request.value(forHTTPHeaderField: "X-Polymux-Client") == "polymux-kit")
            var body = request.httpBody ?? Data()
            if let stream = request.httpBodyStream {
                stream.open()
                defer { stream.close() }
                var buffer = [UInt8](repeating: 0, count: 1024)
                while stream.hasBytesAvailable {
                    let count = stream.read(&buffer, maxLength: buffer.count)
                    if count <= 0 { break }
                    body.append(contentsOf: buffer.prefix(count))
                }
            }
            let input = try JSONDecoder().decode([String: String].self, from: body)
            #expect(input["code"] == "test-invitation")
            let token = try #require(input["token"])
            #expect(token.count == 43 && token != input["code"])
            attempts += 1
            if accepted == nil {
                accepted = token
                throw URLError(.networkConnectionLost)
            }
            if accepted == token { return (204, Data()) }
            return (409, try JSONEncoder().encode(WireError(code: "pair_used", message: "Create a new link.")))
        }
    }
}

struct PairingTests {
    private func session() -> URLSession {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [PairingProtocol.self]
        return URLSession(configuration: config)
    }

    @Test func lostResponseRetainsClaimButAnotherDeviceCannotReuseIt() async throws {
        let peer = PairingPeer()
        let url = PairingProtocol.register { try peer.handle($0) }
        defer { PairingProtocol.remove(url) }
        let root = FileManager.default.temporaryDirectory.appending(path: "pairing-\(UUID())")
        defer { try? FileManager.default.removeItem(at: root) }
        let invite = Invite(url: url.absoluteString, token: "test-invitation")
        let first = root.appending(path: "first/claim")
        do {
            _ = try await Pairing.redeem(invite, pending: first, session: session())
            Issue.record("The simulated lost response did not fail")
        } catch {}
        let claimed = try String(contentsOf: first, encoding: .utf8)
        let recovered = try await Pairing.redeem(invite, pending: first, session: session())
        #expect(recovered == claimed && peer.posts == 2)
        let attributes = try FileManager.default.attributesOfItem(atPath: first.path)
        #expect((attributes[.posixPermissions] as? NSNumber)?.intValue == 0o600)
        await #expect(throws: ClientError.self) {
            _ = try await Pairing.redeem(invite, pending: root.appending(path: "second/claim"), session: session())
        }
        #expect(peer.posts == 3)
    }

    @Test func publicHealthResponseCannotCompletePairing() async throws {
        let url = PairingProtocol.register { request in
            #expect(request.url?.path == "/network/pair")
            return (request.url?.path == "/health" ? 200 : 404, Data("{}".utf8))
        }
        defer { PairingProtocol.remove(url) }
        let pending = FileManager.default.temporaryDirectory.appending(path: "pairing-\(UUID())/claim")
        defer { try? FileManager.default.removeItem(at: pending.deletingLastPathComponent()) }
        await #expect(throws: ClientError.self) {
            _ = try await Pairing.redeem(Invite(url: url.absoluteString, token: "test-invitation"), pending: pending, session: session())
        }
    }

    @Test func plainLANReceivesNeitherInvitationNorCredential() async throws {
        let url = PairingProtocol.register { _ in
            Issue.record("Pairing secrets were sent over ordinary HTTP")
            return (204, Data())
        }
        defer { PairingProtocol.remove(url) }
        let pending = FileManager.default.temporaryDirectory.appending(path: "pairing-\(UUID())/claim")
        defer { try? FileManager.default.removeItem(at: pending.deletingLastPathComponent()) }
        let invite = Invite(url: url.absoluteString.replacingOccurrences(of: "https:", with: "http:"), token: "test-invitation")
        await #expect(throws: ClientError.self) { _ = try await Pairing.redeem(invite, pending: pending, session: session()) }
    }

    @Test func modelPatchOmitsUnrelatedFields() throws {
        let data = try JSONEncoder().encode(UpdateAgentRequest(provider: "replacement", model: "chosen"))
        let value = try JSONDecoder().decode([String: String].self, from: data)
        #expect(value == ["provider": "replacement", "model": "chosen"])
    }
}
