import Foundation

enum ServerAddress {
    static func resolve(_ address: String, defaults: UserDefaults) -> URL? {
        #if DEBUG
        return parse(defaults.string(forKey: "PolymuxServer") ?? address)
        #else
        return parse(address)
        #endif
    }

    static func parse(_ value: String) -> URL? {
        let text = value.trimmingCharacters(in: .whitespacesAndNewlines)
        guard var parts = URLComponents(string: text),
              let scheme = parts.scheme?.lowercased(), ["http", "https"].contains(scheme),
              let host = parts.host, !host.isEmpty,
              !host.contains(where: { $0.isWhitespace || $0.isNewline }),
              parts.user == nil, parts.password == nil,
              parts.query == nil, parts.fragment == nil else { return nil }
        if let port = parts.port, !(1...65535).contains(port) { return nil }
        if parts.percentEncodedHost?.contains("%") == true { return nil }
        let authority = text.dropFirst((parts.scheme?.count ?? 0) + 3).prefix(while: { $0 != "/" })
        guard !authority.hasSuffix(":") else { return nil }
        parts.scheme = scheme
        return parts.url
    }
}
