import Foundation

/// The network side of one file upload. Injected so tests can stall, record or fail it.
public protocol UploadClient: Sendable {
    /// Uploads `file` and reports progress in 0...1. Never runs on the main actor.
    func upload(file: LocalFile, progress: @Sendable (Double) -> Void) async throws -> Attachment
}

extension APIClient: UploadClient {
    public func upload(file: LocalFile, progress: @Sendable (Double) -> Void) async throws -> Attachment {
        progress(0)
        let attachment = try await upload(file: file.url)
        progress(1)
        return attachment
    }
}

/// Uploads a send's files off the main actor and hands each finished attachment back.
/// An actor so entering it can never inherit the main actor from the caller.
public actor AttachmentUploader {
    private let client: any UploadClient

    public init(client: any UploadClient) {
        self.client = client
    }

    public func run(
        _ files: [LocalFile],
        progress: @Sendable (String, Double) -> Void,
        finished: @Sendable (String, Attachment) async -> Void
    ) async throws {
        for file in files {
            let attachment = try await client.upload(file: file) { value in
                progress(file.id, value)
            }
            await finished(file.id, attachment)
        }
    }
}
