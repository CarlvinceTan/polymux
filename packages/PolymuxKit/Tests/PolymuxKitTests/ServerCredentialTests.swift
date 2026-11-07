import Foundation
import Testing
@testable import PolymuxKit

struct ServerCredentialTests {
    @Test func failedPublicationKeepsBothPreviousValues() throws {
        let directory = FileManager.default.temporaryDirectory.appending(path: "credential-\(UUID())")
        defer { try? FileManager.default.removeItem(at: directory) }
        let file = directory.appending(path: "credential.json")
        let previous = ServerCredential(token: "old-token", ticket: "old-ticket")
        let next = ServerCredential(token: "new-token", ticket: "new-ticket")
        try previous.write(to: file)
        #expect(throws: CocoaError.self) {
            try next.write(to: file) { _, _ in throw CocoaError(.fileWriteNoPermission) }
        }
        #expect(try ServerCredential.load(from: directory) == previous)
        #expect(try FileManager.default.contentsOfDirectory(atPath: directory.path) == ["credential.json"])
        try next.write(to: file)
        #expect(try ServerCredential.load(from: directory) == next)
        let attributes = try FileManager.default.attributesOfItem(atPath: file.path)
        #expect((attributes[.posixPermissions] as? NSNumber)?.intValue == 0o600)
    }

    @Test func cancelledSaveLeavesPreviousCredential() async throws {
        let directory = FileManager.default.temporaryDirectory.appending(path: "credential-\(UUID())")
        defer { try? FileManager.default.removeItem(at: directory) }
        let file = directory.appending(path: "credential.json")
        let previous = ServerCredential(token: "old-token", ticket: "old-ticket")
        try previous.write(to: file)
        let task = Task {
            withUnsafeCurrentTask { $0?.cancel() }
            try ServerCredential(token: "new-token", ticket: nil).write(to: file)
        }
        await #expect(throws: CancellationError.self) { try await task.value }
        #expect(try ServerCredential.load(from: directory) == previous)
    }

    @Test func existingPairingsLoadUntilReplacedAtomically() throws {
        let directory = FileManager.default.temporaryDirectory.appending(path: "credential-\(UUID())")
        defer { try? FileManager.default.removeItem(at: directory) }
        try FileManager.default.createDirectory(at: directory, withIntermediateDirectories: true)
        try "existing-token".write(to: directory.appending(path: "token"), atomically: true, encoding: .utf8)
        try "existing-ticket".write(to: directory.appending(path: "ticket"), atomically: true, encoding: .utf8)
        #expect(try ServerCredential.load(from: directory) == ServerCredential(token: "existing-token", ticket: "existing-ticket"))
        let next = ServerCredential(token: "new-token", ticket: nil)
        try next.write(to: directory.appending(path: "credential.json"))
        #expect(try ServerCredential.load(from: directory) == next)
    }
}
