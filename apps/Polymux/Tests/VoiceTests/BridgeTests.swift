import Foundation
import Testing
import PolymuxKit
@testable import PolymuxVoice

private final class BridgeRoutes: @unchecked Sendable {
    private let lock = NSLock()
    private var statuses: [String: Int] = [:]
    private var requests: [String: [URLRequest]] = [:]

    func add(status: Int) -> String {
        let host = "voice-\(UUID().uuidString.lowercased()).test"
        lock.withLock { statuses[host] = status; requests[host] = [] }
        return host
    }
    func record(_ request: URLRequest) -> Int {
        lock.withLock {
            let host = request.url?.host ?? ""
            requests[host, default: []].append(request)
            return statuses[host] ?? 404
        }
    }
    func list(_ host: String) -> [URLRequest] { lock.withLock { requests[host] ?? [] } }
    func remove(_ host: String) { lock.withLock { statuses.removeValue(forKey: host); requests.removeValue(forKey: host) } }
}

private final class BridgeProtocol: URLProtocol, @unchecked Sendable {
    static let routes = BridgeRoutes()
    override class func canInit(with request: URLRequest) -> Bool { request.url?.host?.hasPrefix("voice-") == true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let status = Self.routes.record(request)
        let response = HTTPURLResponse(url: request.url!, statusCode: status, httpVersion: "HTTP/1.1", headerFields: ["Content-Type": "application/json"])!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data(#"{"text":"Private worker result"}"#.utf8))
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

@Test @MainActor func voiceBridgeUsesPrivateEndpointWithoutChatOrCredentials() async throws {
    let host = BridgeProtocol.routes.add(status: 200)
    defer { BridgeProtocol.routes.remove(host) }
    let configuration = URLSessionConfiguration.ephemeral
    configuration.protocolClasses = [BridgeProtocol.self]
    let session = URLSession(configuration: configuration)
    defer { session.invalidateAndCancel() }
    let bridge = VoiceBridge(client: APIClient(baseURL: URL(string: "https://\(host)")!, session: session), threadID: "team")
    var text = ""
    for try await fragment in bridge.stream("A private consultation") { text += fragment.text }
    await bridge.stop()
    #expect(text == "Private worker result")
    let requests = BridgeProtocol.routes.list(host)
    #expect(requests.count == 1)
    #expect(requests.first?.url?.path == "/threads/team/voice")
    #expect(requests.first?.httpMethod == "POST")
    #expect(requests.first?.value(forHTTPHeaderField: "x-goog-api-key") == nil)
    #expect(requests.first?.value(forHTTPHeaderField: "Authorization") == nil)
    let request = try #require(requests.first)
    let body: Data
    if let data = request.httpBody { body = data }
    else if let stream = request.httpBodyStream {
        stream.open()
        defer { stream.close() }
        var data = Data()
        var buffer = [UInt8](repeating: 0, count: 1024)
        while stream.hasBytesAvailable {
            let count = stream.read(&buffer, maxLength: buffer.count)
            if count <= 0 { break }
            data.append(contentsOf: buffer.prefix(count))
        }
        body = data
    } else { body = Data() }
    let task = try JSONDecoder().decode(VoiceRequest.self, from: body)
    #expect(task.text == "A private consultation")
    #expect(!task.id.isEmpty)
}

@Test @MainActor func voiceBridgeNeverReplaysAmbiguousFailures() async {
    let host = BridgeProtocol.routes.add(status: 503)
    defer { BridgeProtocol.routes.remove(host) }
    let configuration = URLSessionConfiguration.ephemeral
    configuration.protocolClasses = [BridgeProtocol.self]
    let session = URLSession(configuration: configuration)
    defer { session.invalidateAndCancel() }
    let bridge = VoiceBridge(client: APIClient(baseURL: URL(string: "https://\(host)")!, session: session), threadID: "team")
    do {
        for try await _ in bridge.stream("Do not replay this task") {}
        Issue.record("Expected server failure")
    } catch { #expect(BridgeProtocol.routes.list(host).count == 1) }
    await bridge.stop()
    #expect(BridgeProtocol.routes.list(host).count == 1)
}
