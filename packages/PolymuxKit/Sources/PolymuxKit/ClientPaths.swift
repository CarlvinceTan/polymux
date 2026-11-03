import CryptoKit
import Foundation

public enum ClientPaths {
    public static func cache(_ address: String) -> URL {
        cache(address, root: root)
    }

    public static func token(_ address: String) -> URL {
        token(address, root: root)
    }

    /// Deletes this device's credential and cache for a server.
    public static func forget(_ address: String) throws {
        try FileManager.default.removeItem(at: directory(address, root: root))
    }

    /// The tunnel ticket a pairing link carried for this server, if any.
    public static func ticket(_ address: String) -> URL {
        token(address, root: root).deletingLastPathComponent().appending(path: "ticket")
    }

    static func claim(_ invite: Invite) -> URL {
        let address = invite.ticket.flatMap(Tunnel.identity).map { "http://\($0).tunnel.invalid" } ?? invite.url
        let key = SHA256.hash(data: Data(invite.token.utf8)).map { String(format: "%02x", $0) }.joined()
        return directory(address, root: root).appending(path: ".pairing-\(key)")
    }

    private static var root: URL {
        let support = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask)[0]
        #if os(macOS)
        let home = FileManager.default.homeDirectoryForCurrentUser
        let desktop = true
        #else
        let home = support
        let desktop = false
        #endif
        return root(home: home, support: support, environment: ProcessInfo.processInfo.environment, desktop: desktop)
    }

    static func root(home: URL, support: URL, environment: [String: String], desktop: Bool) -> URL {
        // Beside a server's data, not in it, as an install has them: the data can move away.
        if let path = environment["POLYMUX_CLIENT_DIR"] ?? environment["POLYMUX_DATA_DIR"], !path.isEmpty {
            return URL(fileURLWithPath: path, isDirectory: true).standardizedFileURL
        }
        return desktop ? home.appending(path: ".polymux", directoryHint: .isDirectory)
            : support.appending(path: "Polymux", directoryHint: .isDirectory)
    }

    static func cache(_ address: String, root: URL) -> URL {
        directory(address, root: root).appending(path: "cache.sqlite")
    }

    static func token(_ address: String, root: URL) -> URL {
        directory(address, root: root).appending(path: "token")
    }

    /// Each server's folder is named by a SHA-256 of its address, so two addresses never share a token or cache.
    static func directory(_ address: String, root: URL) -> URL {
        let digest = SHA256.hash(data: Data(address.utf8)).prefix(16).map { String(format: "%02x", $0) }.joined()
        let folder = root.appending(path: "server-\(digest)", directoryHint: .isDirectory)
        // Folders were once named by a weak hash; move one forward the first time its server is used.
        let legacy = root.appending(path: "app-\(legacyKey(address))", directoryHint: .isDirectory)
        if !FileManager.default.fileExists(atPath: folder.path), FileManager.default.fileExists(atPath: legacy.path) {
            try? FileManager.default.moveItem(at: legacy, to: folder)
        }
        return folder
    }

    private static func legacyKey(_ address: String) -> UInt64 {
        address.utf8.reduce(UInt64(5381)) { ($0 &* 33) &+ UInt64($1) }
    }
}
