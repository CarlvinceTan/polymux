import Foundation
import Testing
@testable import PolymuxKit

/// Uploads interrupted by a crash, quit or lost connection pick up from a kept copy and send only server ids.
@MainActor
struct UploadRecoveryTests {
    @Test func crashedUploadResumesWithServerIDs() async throws {
        let rig = try await Recovery()
        let files = try rig.files()
        let first = rig.chat(StuckUploader())
        await first.ready()
        await first.send(text: "Two files", files: files)
        let id = try #require(first.messages.first?.id)
        #expect(await rig.kept(id, count: 2))
        for file in files { try FileManager.default.removeItem(at: file.url) }
        await rig.actor.unclaim(id)

        // A relaunch: a fresh cache connection and process state, with uploads that now finish.
        let relaunched = try await rig.reopen()
        let uploader = RecordingUploader()
        let resumed = relaunched.chat(uploader)
        await resumed.ready()
        #expect(await until { rig.sent.count == 1 })
        let request = try #require(rig.sent.first)
        #expect(request.id == id && request.attachmentIDs == files.map { "server-\($0.id)" })
        #expect(uploader.uploaded.sorted() == files.map(\.id).sorted())
        #expect(await until { !FileManager.default.fileExists(atPath: relaunched.actor.uploads.appending(path: id).path) })
    }

    @Test func finishedFileIsNotUploadedAgain() async throws {
        let rig = try await Recovery()
        let files = try rig.files()
        let first = rig.chat(StuckUploader(finishing: [files[0].id]))
        await first.ready()
        await first.send(text: "Two files", files: files)
        let id = try #require(first.messages.first?.id)
        #expect(await until { await rig.row(id)?.attachments.first?.id == "server-\(files[0].id)" })
        await rig.actor.unclaim(id)

        let relaunched = try await rig.reopen()
        let uploader = RecordingUploader()
        let resumed = relaunched.chat(uploader)
        await resumed.ready()
        #expect(await until { rig.sent.count == 1 })
        #expect(uploader.uploaded == [files[1].id])
        #expect(rig.sent.first?.attachmentIDs == files.map { "server-\($0.id)" })
    }

    @Test func missingFileIsNotSentAndSaysWhy() async throws {
        let rig = try await Recovery()
        let gone = LocalFile(url: rig.folder.appending(path: "Gone.png"), name: "Gone.png", kind: "image", mimeType: "image/png", size: 4)
        let message = Message(id: "lost", threadID: "arlo", role: MessageRole.user,
                              parts: [Part(kind: PartKind.attachment, attachment: gone.placeholder)],
                              createdAt: WireDate.string(), status: MessageStatus.pending, seq: 0)
        try await rig.actor.call { try $0.merge([message]) }
        let uploader = RecordingUploader()
        let chat = rig.chat(uploader)
        await chat.ready()
        #expect(await until { chat.messages.first?.status == MessageStatus.error })
        #expect(chat.error == "A file in this message is no longer on this device.")
        #expect(uploader.uploaded.isEmpty && rig.sent.isEmpty)
    }

    @Test(arguments: [false, true])
    func reopeningAChatDuringAnUploadStartsNoSecondOne(separateConnection: Bool) async throws {
        let rig = try await Recovery()
        let files = try rig.files()
        let uploader = RecordingUploader(holding: true)
        let first = rig.chat(uploader)
        await first.ready()
        await first.send(text: "Two files", files: files)
        // The first file is up and the second is held mid-upload.
        #expect(await until { uploader.uploaded.count == 2 })
        // Switching away and back makes a new store over the same cache while the first keeps uploading.
        let next = separateConnection ? try await rig.reopen() : rig
        let again = next.chat(uploader)
        await again.ready()
        uploader.release()
        #expect(await until { rig.sent.count == 1 })
        try await Task.sleep(for: .milliseconds(200))
        #expect(uploader.uploaded.sorted() == files.map(\.id).sorted())
        #expect(rig.sent.count == 1)
    }

    @Test func refusedUploadIsNotRetried() async throws {
        let rig = try await Recovery()
        let files = try rig.files()
        let refusing = RecordingUploader(refusal: 413)
        let chat = rig.chat(refusing)
        await chat.ready()
        await chat.send(text: "Too big", files: [files[0]])
        #expect(await until { chat.messages.first?.status == MessageStatus.error })
        let reopened = rig.chat(refusing)
        await reopened.ready()
        try await Task.sleep(for: .milliseconds(200))
        #expect(refusing.uploaded == [files[0].id] && rig.sent.isEmpty)
    }

    @Test func completedUploadsWithoutAnOutboxEntryResume() async throws {
        let rig = try await Recovery()
        let files = try rig.files()
        let reply = ReplyContext(messageID: "source", role: "assistant", text: "Original")
        let message = Message(id: "uploaded", threadID: "arlo", role: MessageRole.user,
                              parts: [Part(kind: PartKind.text, text: "@Aria two files")]
                                  + files.map { Part(kind: PartKind.attachment, attachment: serverCopy($0)) },
                              createdAt: WireDate.string(), status: MessageStatus.pending, seq: 0,
                              reply: reply, mentions: ["aria"])
        try await rig.actor.call { try $0.merge([message]) }
        let uploader = RecordingUploader()
        let chat = rig.chat(uploader)
        await chat.ready()
        #expect(await until { rig.sent.count == 1 })
        #expect(rig.sent.first == SendRequest(id: message.id, text: message.text,
                                            attachmentIDs: files.map { "server-\($0.id)" },
                                            replyToID: "source", mentions: ["aria"]))
        #expect(uploader.uploaded.isEmpty)
    }

    @Test func deletingFromAnotherStoreCannotSendAfterUpload() async throws {
        let rig = try await Recovery()
        let files = try rig.files()
        let uploader = RecordingUploader(holding: true)
        let first = rig.chat(uploader)
        await first.ready()
        await first.send(text: "Never send this", files: files)
        let id = try #require(first.messages.first?.id)
        #expect(await until { uploader.uploaded.count == 2 })
        let again = rig.chat(uploader)
        await again.ready()
        await again.discard(messageID: id)
        uploader.release()
        #expect(await until { await !rig.actor.claimed(id) })
        #expect(rig.sent.isEmpty)
        #expect(await rig.row(id) == nil)
    }

    @Test func sendReturnsWithDurableCopies() async throws {
        let rig = try await Recovery()
        let files = try rig.files()
        let chat = rig.chat(StuckUploader())
        await chat.ready()
        await chat.send(text: "Kept", files: files)
        let id = try #require(chat.messages.first?.id)
        let saved = try #require(await rig.row(id))
        #expect(saved.attachments.allSatisfy { $0.url.hasPrefix(rig.actor.uploads.absoluteString) })
        #expect(files.allSatisfy { FileManager.default.fileExists(atPath: $0.url.path) })
    }

    @Test func transientUploadRetriesWhileTheChatStaysConnected() async throws {
        let rig = try await Recovery()
        let files = try rig.files()
        let uploader = RecordingUploader(failOnce: true)
        let chat = rig.chat(uploader)
        defer { chat.stop() }
        await chat.start()
        #expect(await until { chat.isConnected })
        await chat.send(text: "Retry the file", files: [files[0]])
        #expect(await until { chat.messages.first?.status == MessageStatus.failed })
        let id = try #require(chat.messages.first?.id)
        #expect(chat.isConnected)
        #expect(await until { rig.sent.count == 1 })
        #expect(uploader.uploaded == [files[0].id, files[0].id])
        #expect(rig.sent.first?.id == id)
    }
}

/// A chat's cache and a server that records sends, reopened as a relaunch would.
@MainActor
private final class Recovery {
    let folder: URL
    let actor: LocalCacheActor
    private let host = "recovery-\(UUID().uuidString.lowercased()).test"

    init(folder: URL? = nil) async throws {
        self.folder = folder ?? FileManager.default.temporaryDirectory.appending(path: "polymux-recovery-\(UUID().uuidString)")
        actor = LocalCacheActor(try await CacheWork.open(self.folder.appending(path: "cache.sqlite")))
        SendRecorder.open(host)
    }

    var sent: [SendRequest] { SendRecorder.requests(host) }

    func reopen() async throws -> Recovery {
        let next = try await Recovery(folder: folder)
        next.share(host)
        return next
    }

    private var shared: String?
    private func share(_ host: String) { shared = host }

    func chat(_ uploader: any UploadClient) -> ChatStore {
        let config = URLSessionConfiguration.ephemeral
        config.protocolClasses = [SendRecorder.self]
        let client = APIClient(baseURL: URL(string: "http://\(shared ?? host)")!, session: URLSession(configuration: config))
        return ChatStore(client: client, actor: actor, threadID: "arlo", uploadClient: uploader)
    }

    func files() throws -> [LocalFile] {
        try FileManager.default.createDirectory(at: folder, withIntermediateDirectories: true)
        return try [("Photo.png", "image", "image/png"), ("Notes.pdf", "", "application/pdf")].map { name, kind, mime in
            let url = folder.appending(path: name)
            try Data(repeating: 7, count: 64).write(to: url)
            return LocalFile(url: url, name: name, kind: kind, mimeType: mime, size: 64)
        }
    }

    func row(_ id: String) async -> Message? {
        try? await actor.call { try $0.messages(threadID: "arlo") }.first { $0.id == id }
    }

    /// Waits until the saved message points at kept copies, which a crash would leave behind.
    func kept(_ id: String, count: Int) async -> Bool {
        let uploads = actor.uploads.appending(path: id).path
        return await until {
            let urls = await self.row(id)?.attachments.map(\.url) ?? []
            return urls.count == count && urls.allSatisfy { URL(string: $0)?.path.hasPrefix(uploads) == true }
        }
    }
}

@MainActor
private func until(seconds: Double = 5, _ condition: () async -> Bool) async -> Bool {
    let deadline = ContinuousClock().now + .seconds(seconds)
    while ContinuousClock().now < deadline {
        if await condition() { return true }
        try? await Task.sleep(for: .milliseconds(10))
    }
    return await condition()
}

private func serverCopy(_ file: LocalFile) -> PolymuxKit.Attachment {
    PolymuxKit.Attachment(id: "server-\(file.id)", kind: file.kind, name: file.name, mimeType: file.mimeType,
                          size: file.size, url: "/attachments/server-\(file.id)/content", indexingState: "ready")
}

/// Finishes only the named files and never returns for the rest, like an app killed mid-upload.
private struct StuckUploader: UploadClient {
    var finishing: Set<String> = []

    func upload(file: LocalFile, progress: @escaping @Sendable (Double) -> Void) async throws -> PolymuxKit.Attachment {
        if finishing.contains(file.id) { return serverCopy(file) }
        try await Task.sleep(for: .seconds(3600))
        throw CancellationError()
    }
}

/// Records which files reached it; can hold the second upload or refuse every one.
private final class RecordingUploader: UploadClient, @unchecked Sendable {
    private let lock = NSLock()
    private var files: [String] = []
    private var held: CheckedContinuation<Void, Never>?
    private var holding: Bool
    private let refusal: Int?
    private let failOnce: Bool

    init(holding: Bool = false, refusal: Int? = nil, failOnce: Bool = false) {
        self.holding = holding
        self.refusal = refusal
        self.failOnce = failOnce
    }

    var uploaded: [String] { lock.withLock { files } }

    func upload(file: LocalFile, progress: @escaping @Sendable (Double) -> Void) async throws -> PolymuxKit.Attachment {
        guard FileManager.default.fileExists(atPath: file.url.path) else { throw CocoaError(.fileReadNoSuchFile) }
        let hold = lock.withLock { () -> Bool in
            files.append(file.id)
            return holding && files.count > 1
        }
        if failOnce, uploaded.count == 1 { throw URLError(.networkConnectionLost) }
        if hold {
            await withCheckedContinuation { continuation in
                let go = lock.withLock { () -> Bool in
                    guard holding else { return true }
                    held = continuation
                    return false
                }
                if go { continuation.resume() }
            }
        }
        if let refusal { throw ClientError.response(refusal, "Refused") }
        return serverCopy(file)
    }

    func release() {
        lock.withLock {
            holding = false
            held?.resume()
            held = nil
        }
    }
}

/// Accepts message sends and records them per test host.
private final class SendRecorder: URLProtocol, @unchecked Sendable {
    private static let lock = NSLock()
    nonisolated(unsafe) private static var recorded: [String: [SendRequest]] = [:]

    static func open(_ host: String) { lock.withLock { recorded[host] = recorded[host] ?? [] } }
    static func requests(_ host: String) -> [SendRequest] { lock.withLock { recorded[host] ?? [] } }

    override class func canInit(with request: URLRequest) -> Bool { request.url?.host?.hasPrefix("recovery-") == true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }

    override func startLoading() {
        var body = request.httpBody ?? Data()
        if let stream = request.httpBodyStream {
            stream.open()
            var buffer = [UInt8](repeating: 0, count: 4096)
            while stream.hasBytesAvailable {
                let count = stream.read(&buffer, maxLength: buffer.count)
                if count <= 0 { break }
                body.append(buffer, count: count)
            }
            stream.close()
        }
        if request.httpMethod == "POST", request.url?.lastPathComponent == "messages", let host = request.url?.host,
           let send = try? JSONDecoder().decode(SendRequest.self, from: body) {
            Self.lock.withLock { Self.recorded[host, default: []].append(send) }
        }
        let stream = request.url?.lastPathComponent == "stream"
        let response = HTTPURLResponse(url: request.url!, statusCode: request.httpMethod == "GET" ? 200 : 202,
                                       httpVersion: "HTTP/1.1", headerFields: ["Content-Type": stream ? "text/event-stream" : "application/json"])!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        if stream { return }
        client?.urlProtocol(self, didLoad: request.httpMethod == "GET" ? Data("[]".utf8) : Data())
        client?.urlProtocolDidFinishLoading(self)
    }

    override func stopLoading() {}
}
