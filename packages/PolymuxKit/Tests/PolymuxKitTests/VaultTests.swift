import Foundation
import Testing
@testable import PolymuxKit

private actor VaultScript {
    var blocked = ""
    var entered = false
    var calls = 0
    var requests: [String] = []
    var failure = 0
    private var created = true
    private var imported = false
    private var continuation: CheckedContinuation<Void, Never>?

    func block(_ path: String, failure: Int = 0) { blocked = path; self.failure = failure }
    func release() { continuation?.resume(); continuation = nil }

    func response(_ request: URLRequest) async -> (Int, Data) {
        calls += 1
        #expect(request.value(forHTTPHeaderField: "Cache-Control") == "no-store")
        #expect(request.value(forHTTPHeaderField: "Authorization") == "Bearer device-token")
        let path = request.url!.path
        requests.append("\(request.httpMethod ?? "GET") \(path)")
        if "\(request.httpMethod ?? "GET") \(path)" == blocked {
            entered = true
            await withCheckedContinuation { continuation = $0 }
            if failure > 0 { return (failure, Data("{}".utf8)) }
        }
        #expect(request.value(forHTTPHeaderField: "X-Polymux-Vault") == nil)
        let body: String
        switch path {
        case "/vault":
            body = "{\"created\":\(created),\"available\":false,\"protection\":\"keychain\"}"
        case "/vault/items":
            let entry = "{\"id\":\"entry\",\"kind\":\"login\",\"title\":\"Private entry\",\"origin\":\"https://example.com\",\"fields\":[\"password\"],\"revision\":1,\"removed\":[]}"
            body = imported ? "[\(entry),{\"id\":\"imported\",\"kind\":\"login\",\"title\":\"Imported entry\",\"origin\":\"https://example.com\",\"fields\":[\"password\"],\"revision\":1}]" : "[\(entry)]"
        case "/vault/items/entry", "/vault/cards/card/save":
            if request.httpMethod == "DELETE" {
                #expect(request.url?.query == "revision=1")
                return (204, Data())
            }
            body = "{\"id\":\"entry\",\"kind\":\"login\",\"title\":\"Private entry\",\"url\":\"https://example.com\",\"fields\":{\"password\":\"\"},\"codes\":[],\"revision\":1,\"removed\":[]}"
        case "/vault/cards/card/import": body = "{\"added\":1,\"skipped\":0}"
        case "/vault/import":
            imported = true
            body = "{\"added\":1,\"skipped\":0}"
        case "/vault/browser": body = "{\"endpoint\":\"\"}"
        default: body = "{}"
        }
        return (200, Data(body.utf8))
    }
}

private final class VaultRoutes: @unchecked Sendable {
    private let lock = NSLock()
    private var scripts: [String: VaultScript] = [:]
    func set(_ host: String, script: VaultScript?) { lock.withLock { scripts[host] = script } }
    func get(_ host: String) -> VaultScript? { lock.withLock { scripts[host] } }
}

private final class VaultProtocol: URLProtocol, @unchecked Sendable {
    static let routes = VaultRoutes()
    private let lock = NSLock()
    private var worker: Task<Void, Never>?
    override class func canInit(with request: URLRequest) -> Bool { request.url?.host?.hasPrefix("vault-") == true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let request = request
        let task = Task { @Sendable [self, request] in
            guard let url = request.url, let script = Self.routes.get(url.host ?? "") else { return }
            let (status, body) = await script.response(request)
            guard !Task.isCancelled else { return }
            client?.urlProtocol(self, didReceive: HTTPURLResponse(url: url, statusCode: status, httpVersion: nil, headerFields: [:])!, cacheStoragePolicy: .notAllowed)
            client?.urlProtocol(self, didLoad: body)
            client?.urlProtocolDidFinishLoading(self)
        }
        lock.withLock { self.worker = task }
    }
    override func stopLoading() { lock.withLock { worker?.cancel() } }
}

@MainActor @Suite
struct VaultTests {
    private func wait(_ condition: () async -> Bool) async throws {
        for _ in 0..<200 {
            if await condition() { return }
            try await Task.sleep(for: .milliseconds(10))
        }
        throw URLError(.timedOut)
    }

    private func client(_ host: String, secure: Bool = true) -> VaultClient {
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [VaultProtocol.self]
        return VaultClient(baseURL: URL(string: "\(secure ? "https" : "http")://\(host)")!,
                           authorization: { "device-token" }, configuration: configuration)
    }

    private func card(_ host: String, action: String = "edit") -> VaultStore {
        VaultStore(client: client(host), card: VaultCard(action: action, itemID: "entry", title: "Private entry", kind: "login"), messageID: "card")
    }

    @Test func browsingLoadsOnlyMetadata() async throws {
        let host = "vault-\(UUID().uuidString).test", script = VaultScript()
        VaultProtocol.routes.set(host, script: script)
        defer { VaultProtocol.routes.set(host, script: nil) }
        let store = VaultLibrary(client: client(host))
        await store.load()
        #expect(store.items.first?.title == "Private entry" && store.draft == nil)
        #expect(await script.requests == ["GET /vault", "GET /vault/items"])
        store.close()
        #expect(store.items.count == 1)
    }

    @Test func editingNeedsNoUnlockAndNeverLoadsSavedValues() async throws {
        let host = "vault-\(UUID().uuidString).test", script = VaultScript()
        VaultProtocol.routes.set(host, script: script)
        defer { VaultProtocol.routes.set(host, script: nil) }
        let store = VaultLibrary(client: client(host))
        await store.load()
        store.open("entry")
        try await wait { !store.busy }
        #expect(store.draft?.fields == ["password":""])
        store.draft?.title = "Renamed"
        store.save()
        #expect(store.draft == nil && store.items.first?.title == "Renamed")
        try await wait { !store.busy }
        #expect(store.issue == nil)
        #expect(await script.requests.contains("PUT /vault/items/entry"))
        store.add("login")
        #expect(store.draft?.revision == 0)
        store.close()
    }

    @Test(arguments: [401, 409, 500])
    func failedSaveRestoresTheReplacement(status: Int) async throws {
        let host = "vault-\(UUID().uuidString).test", script = VaultScript()
        VaultProtocol.routes.set(host, script: script)
        defer { VaultProtocol.routes.set(host, script: nil); Task { await script.release() } }
        let store = VaultLibrary(client: client(host))
        await store.load()
        store.open("entry")
        try await wait { !store.busy }
        store.draft?.fields["password"] = "new-private-value"
        await script.block("PUT /vault/items/entry", failure: status)
        store.save()
        try await wait { await script.entered }
        await script.release()
        try await wait { !store.busy }
        #expect(store.draft?.fields["password"] == "new-private-value" && store.issue != nil)
        store.close()
        #expect(store.draft == nil)
    }

    @Test(arguments: ["library", "card"])
    func closingDiscardsPendingEditorResponses(kind: String) async throws {
        let host = "vault-\(UUID().uuidString).test", script = VaultScript()
        VaultProtocol.routes.set(host, script: script)
        defer { VaultProtocol.routes.set(host, script: nil); Task { await script.release() } }
        await script.block("GET /vault/items/entry")
        let store = VaultLibrary(client: client(host)), card = card(host)
        if kind == "library" { store.open("entry") } else { card.load() }
        try await wait { await script.entered }
        store.close(); card.close()
        await script.release()
        try await Task.sleep(for: .milliseconds(100))
        #expect(store.draft == nil && card.draft == nil && !store.busy && !card.busy)
    }

    @Test func privateCardEditsWithoutAnUnlockStep() async throws {
        let host = "vault-\(UUID().uuidString).test", script = VaultScript()
        VaultProtocol.routes.set(host, script: script)
        defer { VaultProtocol.routes.set(host, script: nil) }
        let store = card(host)
        store.load()
        try await wait { !store.busy }
        #expect(store.draft?.fields == ["password":""])
        store.draft?.fields["password"] = "replacement-private-value"
        store.save()
        try await wait { !store.busy }
        #expect(store.done && store.draft == nil)
        #expect(await script.requests == ["GET /vault", "GET /vault/items/entry", "POST /vault/cards/card/save"])
    }

    @Test func importRefreshesMetadataWithoutAuthentication() async throws {
        let host = "vault-\(UUID().uuidString).test", script = VaultScript()
        VaultProtocol.routes.set(host, script: script)
        defer { VaultProtocol.routes.set(host, script: nil) }
        let directory = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        defer { try? FileManager.default.removeItem(at: directory) }
        let file = directory.appendingPathComponent("vault.csv")
        try Data("name,password\nImported,synthetic-import-value".utf8).write(to: file)
        let store = VaultLibrary(client: client(host))
        await store.load()
        store.importFile(file)
        try await wait { !store.busy }
        #expect(store.imported?.added == 1 && store.items.count == 2)
        #expect(store.draft == nil && store.importName == nil)
    }

    @Test func encryptedImportStillRequiresItsOwnFilePassword() async throws {
        let host = "vault-\(UUID().uuidString).test", script = VaultScript()
        VaultProtocol.routes.set(host, script: script)
        defer { VaultProtocol.routes.set(host, script: nil) }
        let file = FileManager.default.temporaryDirectory.appendingPathComponent("\(UUID().uuidString).kdbx")
        defer { try? FileManager.default.removeItem(at: file) }
        try Data([0x03,0xd9,0xa2,0x9a,0x67,0xfb,0x4b,0xb5,0]).write(to: file)
        let store = VaultLibrary(client: client(host))
        store.importFile(file)
        try await wait { !store.busy }
        #expect(store.kdbx && store.importName != nil)
        store.importKDBX("")
        #expect(store.issue != nil)
        store.close()
        #expect(!store.kdbx && store.importName == nil && !store.hasKey)
    }

    @Test func insecureConnectionCannotSendReplacementValues() async throws {
        let host = "vault-\(UUID().uuidString).test", script = VaultScript()
        VaultProtocol.routes.set(host, script: script)
        defer { VaultProtocol.routes.set(host, script: nil) }
        do {
            _ = try await client(host, secure: false).save(VaultItem(id:"entry", kind:"login", title:"Entry", fields:["password":"never-send-insecurely"]))
            Issue.record("Insecure replacement request succeeded")
        } catch {}
        #expect(await script.calls == 0)
    }
}
