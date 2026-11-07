import Foundation

/// Vault requests never share the app's disk cache, cookies, or redirect handling.
public struct VaultClient: Sendable {
    private let endpoint: @Sendable () async throws -> URL
    private let authorization: @Sendable () async -> String?
    private let session: URLSession

    init(baseURL: URL, authorization: @escaping @Sendable () async -> String?,
         configuration: URLSessionConfiguration = .ephemeral, endpoint: (@Sendable () async throws -> URL)? = nil) {
        self.endpoint = endpoint ?? { baseURL }
        self.authorization = authorization
        configuration.urlCache = nil
        configuration.httpCookieStorage = nil
        configuration.httpShouldSetCookies = false
        configuration.requestCachePolicy = .reloadIgnoringLocalCacheData
        session = URLSession(configuration: configuration, delegate: VaultTransport(), delegateQueue: nil)
    }

    public func saveCard(_ id: String, item: VaultItem) async throws -> VaultItem {
        try await decode("vault/cards/\(id)/save", method: "POST", body: JSONEncoder().encode(item))
    }

    static func readFile(_ url: URL, limit: Int) async throws -> Data {
        let data = try await Task.detached(priority: .userInitiated) {
            let access = url.startAccessingSecurityScopedResource()
            defer { if access { url.stopAccessingSecurityScopedResource() } }
            let handle = try FileHandle(forReadingFrom: url)
            defer { try? handle.close() }
            var data = Data()
            while data.count <= limit {
                let chunk = try handle.read(upToCount: min(1 << 20, limit + 1 - data.count)) ?? Data()
                if chunk.isEmpty { break }
                data.append(chunk)
            }
            return data
        }.value
        try Task.checkCancellation()
        guard !data.isEmpty, data.count <= limit else {
            throw ClientError.response(400, "Choose a smaller file.")
        }
        return data
    }

    static func isKDBX(_ data: Data) -> Bool {
        data.prefix(8).elementsEqual([0x03, 0xd9, 0xa2, 0x9a, 0x67, 0xfb, 0x4b, 0xb5])
    }

    func importData(_ data: Data, card: String? = nil) async throws -> VaultImport {
        try await decode(card.map { "vault/cards/\($0)/import" } ?? "vault/import", method: "POST", body: data,
                         contentType: "application/octet-stream")
    }

    func importKDBX(file: Data?, key: Data?, password: String, card: String? = nil) async throws -> VaultImport {
        let boundary = "Polymux-\(UUID().uuidString)"
        var body = Data()
        func add(_ text: String) { body.append(Data(text.utf8)) }
        add("--\(boundary)\r\nContent-Disposition: form-data; name=\"password\"\r\n\r\n\(password)\r\n")
        if let key {
            add("--\(boundary)\r\nContent-Disposition: form-data; name=\"keyfile\"; filename=\"vault.key\"\r\nContent-Type: application/octet-stream\r\n\r\n")
            body.append(key)
            add("\r\n")
        }
        if let file {
            add("--\(boundary)\r\nContent-Disposition: form-data; name=\"file\"; filename=\"vault.kdbx\"\r\nContent-Type: application/octet-stream\r\n\r\n")
            body.append(file)
            add("\r\n")
        }
        add("--\(boundary)--\r\n")
        return try await decode(card.map { "vault/cards/\($0)/import" } ?? "vault/import", method: "POST", body: body,
                                contentType: "multipart/form-data; boundary=\(boundary)")
    }

    public func status() async throws -> VaultStatus {
        try await decode("vault")
    }

    public func items() async throws -> [VaultSummary] {
        try await decode("vault/items")
    }

    public func save(_ item: VaultItem) async throws -> VaultItem {
        try await decode("vault/items/\(item.id)", method: "PUT", body: JSONEncoder().encode(item))
    }

    public func delete(_ item: VaultItem) async throws {
        _ = try await send("vault/items/\(item.id)", method: "DELETE",
                           query: [URLQueryItem(name: "revision", value: String(item.revision))])
    }

    public func item(_ id: String) async throws -> VaultItem {
        try await decode("vault/items/\(id)")
    }

    private func decode<Value: Decodable>(_ path: String, method: String = "GET",
                                          body: Data? = nil, contentType: String = "application/json") async throws -> Value {
        let data = try await send(path, method: method, body: body, contentType: contentType)
        return try JSONDecoder().decode(Value.self, from: data)
    }

    private func send(_ path: String, method: String = "GET",
                      body: Data? = nil, contentType: String = "application/json",
                      query: [URLQueryItem] = []) async throws -> Data {
        let baseURL = try await endpoint()
        guard Tunnel.trusted(baseURL) else {
            throw ClientError.response(403, "Use an encrypted connection to open the vault.")
        }
        let url = baseURL.appending(path: path).appending(queryItems: query)
        var request = URLRequest(url: url, cachePolicy: .reloadIgnoringLocalCacheData, timeoutInterval: 30)
        request.httpMethod = method
        request.httpBody = body
        request.setValue(contentType, forHTTPHeaderField: "Content-Type")
        request.setValue("no-store", forHTTPHeaderField: "Cache-Control")
        request.setValue("polymux-kit", forHTTPHeaderField: "X-Polymux-Client")
        if let bearer = await authorization() { request.setValue("Bearer \(bearer)", forHTTPHeaderField: "Authorization") }
        try Task.checkCancellation()
        let (data, response) = try await session.data(for: request)
        try Task.checkCancellation()
        guard let response = response as? HTTPURLResponse else { throw ClientError.invalidResponse }
        guard (200..<300).contains(response.statusCode) else {
            let error = try? JSONDecoder().decode(WireError.self, from: data)
            throw ClientError.response(response.statusCode, error?.message ?? "Couldn’t complete this vault action.")
        }
        return data
    }
}

private final class VaultTransport: NSObject, URLSessionTaskDelegate, Sendable {
    func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
                    newRequest request: URLRequest, completionHandler: @escaping @Sendable (URLRequest?) -> Void) {
        completionHandler(nil)
    }
}
