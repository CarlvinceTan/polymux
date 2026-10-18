import Foundation

public enum ClientPaths {
    public static func cache(_ address: String) -> URL {
        cache(address, root: root)
    }

    public static func token(_ address: String) -> URL {
        token(address, root: root)
    }

    /// The tunnel ticket a pairing link carried for this server, if any.
    public static func ticket(_ address: String) -> URL {
        token(address, root: root).deletingLastPathComponent().appending(path: "ticket")
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
        if let path = environment["POLYMUX_DATA_DIR"], !path.isEmpty {
            return URL(fileURLWithPath: path, isDirectory: true).standardizedFileURL
        }
        return desktop ? home.appending(path: ".polymux", directoryHint: .isDirectory)
            : support.appending(path: "Polymux", directoryHint: .isDirectory)
    }

    static func cache(_ address: String, root: URL) -> URL {
        root.appending(path: "app-\(key(address))/cache.sqlite")
    }

    static func token(_ address: String, root: URL) -> URL {
        root.appending(path: "app-\(key(address))/token")
    }

    private static func key(_ address: String) -> UInt64 {
        address.utf8.reduce(UInt64(5381)) { ($0 &* 33) &+ UInt64($1) }
    }
}
