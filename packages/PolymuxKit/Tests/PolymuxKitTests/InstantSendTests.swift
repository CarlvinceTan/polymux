import Foundation
import Testing
@testable import PolymuxKit

extension InstantTiming {
/// SA1: pressing Send with files shows the bubble and clears the composer before uploads finish.
@Suite @MainActor
struct InstantSendTests {
    @Test func testBubbleBeforeUpload() async throws {
        let uploader = BlockingUploader()
        let rig = try await Rig(uploader: uploader)
        let files = try rig.files()
        // A stalled cache keeps `send` inside its first suspension; the bubble must be up anyway.
        await rig.cacheActor.setStall(.milliseconds(60))
        let latch = Latch()
        let sending = Task {
            await rig.chat.send(text: "Two files", files: files)
            latch.fire()
        }
        #expect(await wait { rig.chat.messages.count == 1 })
        #expect(!sending.isCancelled)
        #expect(rig.chat.trace.spans.isEmpty)   // the cache round trip has not returned yet
        #expect(await wait { latch.fired })     // and `send` still returns with the upload blocked
        let message = try #require(rig.chat.messages.first)
        #expect(message.role == "user")
        #expect(message.text == "Two files")
        #expect(message.status == "pending")
        #expect(message.seq == 0)
        #expect(message.attachments.count == 2)
        #expect(message.attachments.compactMap(\.localURL).count == 2)
        #expect(Set(rig.chat.uploads.keys) == Set(files.map(\.id)))
    }

    @Test func testUploadOffMain() async throws {
        let uploader = ParkedUploader()
        let rig = try await Rig(uploader: uploader)
        let files = try rig.files()
        await rig.chat.send(text: "Two files", files: files)
        #expect(await wait { uploader.started })
        #expect(uploader.ranOnMain == false)   // traps outright on the main queue
        #expect(await wait { rig.chat.uploads[files[0].id] == 0.5 })
        // The upload is parked in the uploader: the main actor still drives the rest of the send.
        #expect(rig.chat.messages.first?.status == "pending")
        #expect(rig.chat.isSending)
        uploader.release()
        #expect(await wait { rig.chat.messages.first?.status == "done" })
        #expect(rig.chat.messages.first?.attachments.allSatisfy { $0.localURL == nil } == true)
        #expect(rig.chat.uploads.isEmpty)
    }

    @Test func testAckReplacesNotDuplicates() async throws {
        let rig = try await Rig(uploader: InstantUploader())
        let files = try rig.files()
        await rig.chat.send(text: "Two files", files: files)
        #expect(await wait { rig.chat.messages.first?.status == "done" })
        let id = try #require(rig.chat.messages.first?.id)
        #expect(rig.chat.messages.count == 1)
        #expect(rig.chat.messages.first?.attachments.map(\.id) == ["server-\(files[0].id)", "server-\(files[1].id)"])
        let server = Message(id: id, threadID: rig.chat.threadID, role: "user",
                             parts: [Part(kind: "text", text: "Two files")]
                                 + files.map { Part(kind: "attachment", attachment: uploaded($0)) },
                             createdAt: WireDate.string(), status: "done", seq: 1)
        try await rig.chat.receive(Event(seq: 1, conversationID: rig.chat.threadID, type: "message.start",
                                         message: server, createdAt: server.createdAt))
        #expect(rig.chat.messages.filter { $0.id == id }.count == 1)
        #expect(rig.chat.messages.count == 1)
        #expect(rig.chat.messages.first?.attachments.map(\.id) == ["server-\(files[0].id)", "server-\(files[1].id)"])
        #expect(rig.chat.messages.first?.status == "done")
    }

    @Test func testBubbleBudgetHoldsWhileFilesUpload() async throws {
        let rig = try await Rig(uploader: BlockingUploader())
        let files = try rig.files()
        let tap = ContinuousClock().now
        await rig.chat.send(text: "Two files", files: files, at: tap)
        let span = try #require(rig.chat.trace.spans.first { $0.name == "send.bubble" })
        #expect(span.budget == InstantUI.feedbackBudget)
        #expect(span.withinBudget)
    }

    /// Stall twin: the fake cache blocks 40 ms, so the same check must report a miss.
    @Test func testSlowCacheMissesTheBubbleBudget() async throws {
        let rig = try await Rig(uploader: BlockingUploader())
        let files = try rig.files()
        await rig.cacheActor.setStall(.milliseconds(40))
        let tap = ContinuousClock().now
        await rig.chat.send(text: "Two files", files: files, at: tap)
        let span = try #require(rig.chat.trace.spans.first { $0.name == "send.bubble" })
        #expect(span.withinBudget == false)
        #expect(span.duration > InstantUI.feedbackBudget)
        #expect(rig.chat.trace.meetsBudgets == false)
        // A slow cache delays the record, never the bubble: it is on screen while the upload blocks.
        #expect(rig.chat.messages.first?.status == "pending")
        #expect(rig.chat.messages.first?.attachments.compactMap(\.localURL).count == 2)
    }
}

}

private func uploaded(_ file: LocalFile) -> PolymuxKit.Attachment {
    PolymuxKit.Attachment(id: "server-\(file.id)", kind: file.kind, name: file.name, mimeType: file.mimeType,
               size: file.size, url: "/attachments/\(file.id)", indexingState: "ready")
}

/// Bounded polling for the async pipeline; a hang fails the expectation instead of the run.
@MainActor
private func wait(_ condition: () -> Bool, seconds: Double = 3) async -> Bool {
    let deadline = ContinuousClock().now + .seconds(seconds)
    while ContinuousClock().now < deadline {
        if condition() { return true }
        try? await Task.sleep(for: .milliseconds(2))
    }
    return condition()
}

/// Set once a task finished, so a blocked pipeline reports a failure instead of hanging.
@MainActor
private final class Latch {
    private(set) var fired = false
    func fire() { fired = true }
}

/// Never finishes an upload, so whatever is visible cannot be waiting for the server.
private struct BlockingUploader: UploadClient {
    func upload(file: LocalFile, progress: @Sendable (Double) -> Void) async throws -> PolymuxKit.Attachment {
        dispatchPrecondition(condition: .notOnQueue(.main))
        progress(0.5)
        try await Task.sleep(for: .seconds(3600))
        throw CancellationError()
    }
}

private struct InstantUploader: UploadClient {
    func upload(file: LocalFile, progress: @Sendable (Double) -> Void) async throws -> PolymuxKit.Attachment {
        dispatchPrecondition(condition: .notOnQueue(.main))
        progress(0.25)
        progress(1)
        return uploaded(file)
    }
}

/// Records where it ran and parks until released, so the test can inspect the live send.
private final class ParkedUploader: UploadClient, @unchecked Sendable {    private let lock = NSLock()
    private var entered = false
    private var onMain = false
    private var stopped = false
    private var parked: CheckedContinuation<Void, Never>?

    var started: Bool { lock.withLock { entered } }
    var ranOnMain: Bool { lock.withLock { onMain } }

    func upload(file: LocalFile, progress: @Sendable (Double) -> Void) async throws -> PolymuxKit.Attachment {
        dispatchPrecondition(condition: .notOnQueue(.main))
        let main = pthread_main_np() != 0
        let park = lock.withLock { () -> Bool in
            entered = true
            onMain = main
            return !stopped
        }
        progress(0.5)
        if park {
            await withCheckedContinuation { continuation in
                let leave = lock.withLock { () -> Bool in
                    if stopped { return true }
                    parked = continuation
                    return false
                }
                if leave { continuation.resume() }
            }
        }
        return uploaded(file)
    }

    func release() {
        lock.withLock {
            stopped = true
            parked?.resume()
            parked = nil
        }
    }
}

/// Answers the outbox POST so the ack path is deterministic; the uploader is faked separately.
private final class AckProtocol: URLProtocol, @unchecked Sendable {
    override class func canInit(with request: URLRequest) -> Bool { request.url?.host == "ack.test" }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        let response = HTTPURLResponse(url: request.url!, statusCode: 202, httpVersion: "HTTP/1.1",
                                       headerFields: ["Content-Type": "application/json"])!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: Data())
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}

@MainActor
private struct Rig {
    let folder: URL
    let cacheActor: LocalCacheActor
    let chat: ChatStore

    init(uploader: any UploadClient) async throws {
        folder = FileManager.default.temporaryDirectory.appendingPathComponent("polymux-instant-\(UUID().uuidString)")
        let cache = try await CacheWork.open(folder.appendingPathComponent("cache.sqlite"))
        cacheActor = LocalCacheActor(cache)
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [AckProtocol.self]
        let client = APIClient(baseURL: URL(string: "http://ack.test")!, session: URLSession(configuration: config))
        chat = ChatStore(client: client, actor: cacheActor, threadID: "arlo", uploadClient: uploader)
        await chat.ready()
    }

    func files() throws -> [LocalFile] {
        try [("Photo.png", "image", "image/png"), ("Notes.pdf", "", "application/pdf")].map { name, kind, mime in
            let url = folder.appendingPathComponent(name)
            try Data(repeating: 7, count: 64).write(to: url)
            return LocalFile(url: url, name: name, kind: kind, mimeType: mime, size: 64)
        }
    }
}
