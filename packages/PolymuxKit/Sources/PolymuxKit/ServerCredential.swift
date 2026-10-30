import Darwin
import Foundation

/// Token and route are published together, so a refused save leaves the old connection intact.
public struct ServerCredential: Codable, Sendable, Equatable {
    public let token: String
    public let ticket: String?

    public init(token: String, ticket: String?) {
        self.token = token
        self.ticket = ticket
    }

    public static func load(_ address: String) throws -> Self? {
        try load(from: ClientPaths.token(address).deletingLastPathComponent())
    }

    static func load(from directory: URL) throws -> Self? {
        let file = directory.appending(path: "credential.json")
        if FileManager.default.fileExists(atPath: file.path) {
            return try JSONDecoder().decode(Self.self, from: Data(contentsOf: file))
        }
        // Existing installations keep working until their next confirmed pairing.
        guard let token = try? String(contentsOf: directory.appending(path: "token"), encoding: .utf8) else { return nil }
        return Self(token: token, ticket: try? String(contentsOf: directory.appending(path: "ticket"), encoding: .utf8))
    }

    public func save(_ address: String) async throws {
        let task = Task.detached {
            let directory = ClientPaths.token(address).deletingLastPathComponent()
            try write(to: directory.appending(path: "credential.json"))
            for name in ["token", "ticket"] { try? FileManager.default.removeItem(at: directory.appending(path: name)) }
        }
        try await withTaskCancellationHandler { try await task.value } onCancel: { task.cancel() }
    }

    func write(to file: URL, publish: (URL, URL) throws -> Void = Self.publish) throws {
        try Task.checkCancellation()
        let directory = file.deletingLastPathComponent()
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true,
                                                attributes: [.posixPermissions: 0o700])
        let staged = directory.appending(path: ".credential-\(UUID())")
        defer { try? FileManager.default.removeItem(at: staged) }
        guard FileManager.default.createFile(atPath: staged.path, contents: try JSONEncoder().encode(self),
                                              attributes: [.posixPermissions: 0o600]) else {
            throw CocoaError(.fileWriteUnknown)
        }
        try Task.checkCancellation()
        try publish(staged, file)
    }

    private static func publish(_ staged: URL, _ file: URL) throws {
        guard rename(staged.path, file.path) == 0 else { throw POSIXError(POSIXErrorCode(rawValue: errno) ?? .EIO) }
    }
}
