import Foundation
import Testing
@testable import PolymuxKit

@Suite(.serialized) @MainActor struct ChatWarmTests {
    @Test func warmCatchesUpWithoutOpeningTheChat() async throws {
        let (chat, sidebar, cache, folder) = try await rig(serverSeq: 3)
        defer { try? FileManager.default.removeItem(at: folder) }
        await chat.warm()
        #expect(await until { chat.messages.count == 3 })
        #expect(!chat.isConnected)
        #expect(sidebar.threads.first?.unreadCount == 3)
        #expect(try await cache.call { try $0.cursor(threadID: "chat") } == 3)
    }

    @Test func warmSkipsAChatThatIsCurrent() async throws {
        let (chat, _, _, folder) = try await rig(serverSeq: 0)
        defer { try? FileManager.default.removeItem(at: folder) }
        await chat.warm()
        #expect(WarmServer.requests() == 0)
        #expect(chat.messages.isEmpty)
    }

    @Test func coldChatStartsFromTheSidebarPreview() async throws {
        let (chat, sidebar, _, folder) = try await rig(serverSeq: 3, preview: Self.event(3).message)
        defer { try? FileManager.default.removeItem(at: folder) }
        #expect(sidebar.lastFinished["chat"]?.id == "m3")
        #expect(chat.messages.map(\.id) == ["m3"])
        await chat.warm()
        #expect(await until { chat.messages.count == 3 })
    }

    /// A catch-up page must reach the sidebar once: per-event updates redraw the window per event.
    @Test func catchUpNotesTheSidebarOnce() async throws {
        let (chat, sidebar, _, folder) = try await rig(serverSeq: 3, sidebarSeq: 0)
        defer { try? FileManager.default.removeItem(at: folder) }
        var notes = 0
        chat.onUpdate = { event, _ in
            if event.type != EventType.messageRead { notes += 1 }
            sidebar.note(event)
        }
        await chat.start()
        #expect(await until { chat.isConnected || chat.messages.count == 3 })
        chat.stop()
        #expect(notes == 1)
        #expect(sidebar.threads.first?.lastMessage?.id == "m3")
    }

    private func rig(serverSeq: Int64, sidebarSeq: Int64? = nil, preview: Message? = nil) async throws -> (ChatStore, SidebarStore, LocalCacheActor, URL) {
        WarmServer.reset(events: serverSeq == 0 ? [] : (1...serverSeq).map(Self.event))
        let folder = FileManager.default.temporaryDirectory.appending(path: "polymux-warm-\(UUID())")
        let cache = try await CacheWork.open(folder.appending(path: "cache.sqlite"))
        let configuration = URLSessionConfiguration.ephemeral
        configuration.protocolClasses = [WarmServer.self]
        let client = APIClient(baseURL: URL(string: "http://warm.test")!, session: URLSession(configuration: configuration))
        try await cache.offMain {
            try $0.save(agents: [Agent(id: "chat", name: "Chat")],
                        threads: [PolymuxKit.Thread(id: "chat", agentID: "chat", lastSeq: sidebarSeq ?? serverSeq,
                                                   unreadCount: serverSeq, lastMessage: preview)])
        }
        let sidebar = SidebarStore(client: client, cache: cache)
        await sidebar.load()
        let chat = ChatStore(client: client, actor: sidebar.cacheActor, threadID: "chat", sidebar: sidebar, resumes: false)
        await chat.ready()
        return (chat, sidebar, sidebar.cacheActor, folder)
    }

    private static func event(_ seq: Int64) -> Event {
        let message = Message(id: "m\(seq)", threadID: "chat", role: MessageRole.assistant,
                              parts: [Part(kind: PartKind.text, text: "Reply \(seq)")],
                              createdAt: "2026-10-31T00:00:0\(seq)Z", status: MessageStatus.done, seq: seq)
        return Event(seq: seq, conversationID: "chat", type: EventType.messageDone, message: message, createdAt: message.createdAt)
    }

    private func until(_ condition: () -> Bool) async -> Bool {
        for _ in 0..<200 {
            if condition() { return true }
            try? await Task.sleep(for: .milliseconds(10))
        }
        return condition()
    }
}

private final class WarmServer: URLProtocol, @unchecked Sendable {
    nonisolated(unsafe) private static var events: [Event] = []
    nonisolated(unsafe) private static var count = 0
    private static let lock = NSLock()

    static func reset(events: [Event]) { lock.withLock { self.events = events; count = 0 } }
    static func requests() -> Int { lock.withLock { count } }

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func stopLoading() {}

    override func startLoading() {
        let url = request.url!
        if url.lastPathComponent == "stream" { return }
        var data = Data("{}".utf8)
        if url.lastPathComponent == "events" {
            let after = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems?
                .first { $0.name == "after" }?.value.flatMap(Int64.init) ?? 0
            let page = Self.lock.withLock {
                Self.count += 1
                return Self.events.filter { $0.seq > after }
            }
            data = (try? JSONEncoder().encode(page)) ?? data
        } else if ["messages", "tool-runs"].contains(url.lastPathComponent) {
            data = Data("[]".utf8)
        }
        let response = HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil, headerFields: nil)!
        client?.urlProtocol(self, didReceive: response, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: data)
        client?.urlProtocolDidFinishLoading(self)
    }
}
