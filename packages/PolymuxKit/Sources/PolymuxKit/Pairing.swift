import CryptoKit
import Foundation

public enum Pairing {
    private static let claims = PairingClaims()
    private static let session = URLSession(configuration: .ephemeral, delegate: PairingRedirects(), delegateQueue: nil)

    /// Confirms an invitation before replacing the saved connection. The locally kept claim
    /// makes a retry safe even when the server accepted it but its response was lost.
    public static func redeem(_ invite: Invite, pending: URL? = nil, session: URLSession? = nil) async throws -> String {
        guard let direct = ServerURL.parse(invite.url) else { throw ClientError.invalidURL }
        let token = try await claims.token(at: pending ?? ClientPaths.claim(invite))
        let session = session ?? Self.session
        var directError: Error?
        if Tunnel.trusted(direct) {
            do {
                try await exchange(invite, token: token, base: direct, session: session)
                return token
            } catch is CancellationError { throw CancellationError() }
            catch { directError = error }
        }
        try Task.checkCancellation()
        guard let ticket = invite.ticket else {
            throw directError ?? ClientError.response(0, "This link has no secure route. Create a new link on the server.")
        }
        let tunnel: Tunnel
        do { tunnel = try Tunnel(ticket: ticket) }
        catch { throw ClientError.response(400, "This pairing ticket is invalid. Create a new link in Connections.") }
        do {
            let base = try await tunnel.start()
            try await exchange(invite, token: token, base: base, session: session)
            await tunnel.stop()
            return token
        } catch {
            await tunnel.stop()
            try Task.checkCancellation()
            throw error
        }
    }

    public static func finish(_ invite: Invite) async { await claims.remove(ClientPaths.claim(invite)) }

    private static func exchange(_ invite: Invite, token: String, base: URL, session: URLSession) async throws {
        try Task.checkCancellation()
        var request = URLRequest(url: base.appending(path: "network/pair"))
        request.httpMethod = "POST"
        request.timeoutInterval = 12
        request.setValue("application/json", forHTTPHeaderField: "Content-Type")
        request.setValue("polymux-kit", forHTTPHeaderField: "X-Polymux-Client")
        request.httpBody = try JSONEncoder().encode(["code": invite.token, "token": token])
        let (data, response) = try await session.data(for: request)
        guard let response = response as? HTTPURLResponse else { throw ClientError.invalidResponse }
        guard response.statusCode == 204 else {
            let code = (try? JSONDecoder().decode(WireError.self, from: data))?.code
            if ["pair_used", "pair_expired"].contains(code) {
                throw ClientError.response(response.statusCode, "This pairing link expired or was already used. Create a new link in Connections.")
            }
            throw ClientError.response(response.statusCode, "Couldn’t pair with this server. Create a new link in Connections.")
        }
    }
}

private actor PairingClaims {
    func token(at file: URL) throws -> String {
        if let token = try? String(contentsOf: file, encoding: .utf8), token.count == 43 { return token }
        let token = SymmetricKey(size: .bits256).withUnsafeBytes { Data($0) }.base64EncodedString()
            .replacingOccurrences(of: "+", with: "-").replacingOccurrences(of: "/", with: "_").replacingOccurrences(of: "=", with: "")
        try FileManager.default.createDirectory(at: file.deletingLastPathComponent(), withIntermediateDirectories: true,
                                                attributes: [.posixPermissions: 0o700])
        try token.write(to: file, atomically: true, encoding: .utf8)
        try FileManager.default.setAttributes([.posixPermissions: 0o600], ofItemAtPath: file.path)
        return token
    }

    func remove(_ file: URL) { try? FileManager.default.removeItem(at: file) }
}

private final class PairingRedirects: NSObject, URLSessionTaskDelegate, Sendable {
    func urlSession(_ session: URLSession, task: URLSessionTask, willPerformHTTPRedirection response: HTTPURLResponse,
                    newRequest request: URLRequest, completionHandler: @escaping @Sendable (URLRequest?) -> Void) {
        completionHandler(nil)
    }
}
