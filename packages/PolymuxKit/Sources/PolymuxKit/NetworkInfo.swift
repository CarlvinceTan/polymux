import Foundation

/// The server and every device paired with it.
public struct NetworkInfo: Codable, Sendable {
    public struct Server: Codable, Sendable {
        public let name: String
        public let build: String
        public let platform: String
        public let role: String
    }

    public struct Device: Codable, Sendable, Identifiable {
        public let id: String
        public let name: String
    }

    public let server: Server
    public var devices: [Device]
    /// The device making the request, when the server identifies it.
    public let current: String?
}


/// A one-time pairing for another device: the direct address, its credential and, when the
/// server runs a tunnel, a ticket that reaches it from other networks.
public struct Invite: Codable, Sendable, Equatable {
    public let url: String
    public let token: String
    public let ticket: String?

    public init(url: String, token: String, ticket: String? = nil) {
        self.url = url
        self.token = token
        self.ticket = ticket
    }

    /// The `polymux://connect` link a QR code or message carries.
    public var link: URL {
        var parts = URLComponents()
        parts.scheme = "polymux"
        parts.host = "connect"
        parts.queryItems = [URLQueryItem(name: "url", value: url), URLQueryItem(name: "token", value: token)]
            + (ticket.map { [URLQueryItem(name: "ticket", value: $0)] } ?? [])
        return parts.url!
    }

    /// Reads a pairing link back; nil for anything that is not one.
    public init?(link: URL) {
        guard link.scheme == "polymux", link.host == "connect",
              let items = URLComponents(url: link, resolvingAgainstBaseURL: false)?.queryItems,
              let url = items.first(where: { $0.name == "url" })?.value,
              let token = items.first(where: { $0.name == "token" })?.value else { return nil }
        self.init(url: url, token: token, ticket: items.first(where: { $0.name == "ticket" })?.value)
    }
}
