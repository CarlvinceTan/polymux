import Foundation

/// The server and every device paired with it.
public struct NetworkInfo: Codable, Sendable {
    public struct Server: Codable, Sendable {
        /// Stays the same when the server's address changes.
        public var id: String?
        public let name: String
        /// What the network is called; nil or empty until a device names it.
        public var network: String?
        /// A preset symbol name or an image data URL; nil or empty until a device picks one.
        public var icon: String?
        public let build: String
        public let platform: String
        public let role: String
    }

    public struct Moving: Codable, Sendable, Equatable {
        public let url: String
        public let ticket: String?
        /// The server is being absorbed, so credentials for it won't work where it's going.
        public let merge: Bool?
    }

    /// What a move would do, before it starts.
    public struct MovePreview: Codable, Sendable {
        public let size: Int64
        public let blockers: [String]
        public let stays: [String]
        /// A merge's extras: what it changes, and who it changes it for.
        public let notes: [String]?
        public let gains: [String]?
        public let loses: [String]?
    }

    /// Another server on this network.
    public struct Peer: Codable, Sendable, Identifiable, Equatable {
        public let id: String
        public let name: String

        public init(id: String, name: String) {
            self.id = id
            self.name = name
        }
    }

    public struct Device: Codable, Sendable, Identifiable {
        public let id: String
        /// What the device reports, such as its hostname.
        public let name: String
        /// The name someone chose for it; empty when unset.
        public var label: String?
        /// False while an invite for it hasn't been used yet.
        public var paired: Bool?
        /// What the device runs, such as "macOS 26.1"; nil until it reports one.
        public var system: String?

        public var title: String {
            if let label, !label.isEmpty { label } else { name }
        }
    }

    public var server: Server
    public var devices: [Device]
    /// The device making the request, when the server identifies it.
    public let current: String?
    /// The device on the server's own machine.
    public let host: String?
    /// Devices online now: the server or a connected app.
    public let online: [String]?
    public var peers: [Peer]?
    /// Devices kept off this server, which can be allowed back.
    public var restricted: [Device]?
    /// True when retiring this server would lose nothing.
    public var empty: Bool?
    /// Where this server is going, while a move is under way.
    public var moving: Moving?
    /// Why the last move failed.
    public var moveIssue: String?
    /// Servers this one has absorbed.
    public var merged: [String]?
}


/// A one-time pairing for another device: the direct address, its invitation and, when the
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

    /// The app link carried by pairing QR codes.
    public var link: URL {
        var parts = URLComponents()
        parts.scheme = "polymux"
        parts.host = "connect"
        parts.queryItems = [URLQueryItem(name: "url", value: url), URLQueryItem(name: "token", value: token)]
            + (ticket.map { [URLQueryItem(name: "ticket", value: $0)] } ?? [])
        return parts.url!
    }

    /// The browser-compatible link. The fragment keeps the invite out of HTTP requests.
    public var shareLink: URL {
        var parts = URLComponents(string: "https://polymux.com/connect")!
        parts.percentEncodedFragment = URLComponents(url: link, resolvingAgainstBaseURL: false)?.percentEncodedQuery
        return parts.url!
    }

    /// Reads a pairing link back; nil for anything that is not one.
    public init?(link: URL) {
        guard let parts = URLComponents(url: link, resolvingAgainstBaseURL: false),
              parts.user == nil, parts.password == nil, parts.port == nil else { return nil }
        let items: [URLQueryItem]
        switch (parts.scheme, parts.host, parts.percentEncodedPath) {
        case ("polymux", "connect", "") where parts.fragment == nil:
            items = parts.queryItems ?? []
        case ("https", "polymux.com", "/connect") where parts.query == nil:
            var payload = URLComponents()
            payload.percentEncodedQuery = parts.percentEncodedFragment
            items = payload.queryItems ?? []
        default:
            return nil
        }
        guard items.filter({ $0.name == "url" }).count == 1,
              items.filter({ $0.name == "token" }).count == 1,
              items.filter({ $0.name == "ticket" }).count <= 1,
              let url = items.first(where: { $0.name == "url" })?.value,
              ServerURL.parse(url) != nil,
              let token = items.first(where: { $0.name == "token" })?.value,
              !token.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty,
              !token.contains(where: { $0.isWhitespace || $0.isNewline || $0 == "\0" }) else { return nil }
        if let ticket = items.first(where: { $0.name == "ticket" }), ticket.value?.isEmpty != false { return nil }
        self.init(url: url, token: token, ticket: items.first(where: { $0.name == "ticket" })?.value)
    }
}
