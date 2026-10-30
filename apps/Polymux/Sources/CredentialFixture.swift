#if DEBUG
import Foundation
import PolymuxKit

/// A chat holding credential cards, so their states can be checked without a server.
final class CredentialFixture: @unchecked Sendable {
    static let shared = CredentialFixture()
    private let lock = NSLock()
    private var saved = false

    func handle(_ fixture: VerifyFixture) -> Bool {
        guard CommandLine.arguments.contains("--verify-credentials"),
              let url = fixture.request.url, url.path.hasPrefix("/threads/t-chief/") else { return false }
        switch url.lastPathComponent {
        case "messages" where fixture.request.httpMethod != "POST":
            reply(fixture, data: try! JSONEncoder().encode(cards))
        case "credential":
            lock.withLock { saved = true }
            reply(fixture, data: try! JSONEncoder().encode(cards[1]))
        case "events":
            reply(fixture, data: try! JSONEncoder().encode(events(after: url)))
        case "stream":
            let response = HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil,
                                           headerFields: ["Content-Type": "text/event-stream"])!
            fixture.client?.urlProtocol(fixture, didReceive: response, cacheStoragePolicy: .notAllowed)
            let wire = events(after: url)
                .map { "data: " + String(decoding: try! JSONEncoder().encode($0), as: UTF8.self) + "\n\n" }
                .joined()
            fixture.client?.urlProtocol(fixture, didLoad: Data((wire + ": connected\n\n").utf8))
        default:
            return false
        }
        return true
    }

    private func reply(_ fixture: VerifyFixture, data: Data) {
        let response = HTTPURLResponse(url: fixture.request.url!, statusCode: 200, httpVersion: nil,
                                      headerFields: ["Content-Type": "application/json"])!
        fixture.client?.urlProtocol(fixture, didReceive: response, cacheStoragePolicy: .notAllowed)
        fixture.client?.urlProtocol(fixture, didLoad: data)
        fixture.client?.urlProtocolDidFinishLoading(fixture)
    }

    private func events(after url: URL) -> [Event] {
        let after = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems?
            .first { $0.name == "after" }?.value.flatMap(Int64.init) ?? 0
        return cards.map {
            Event(seq: $0.seq, conversationID: "t-chief", type: EventType.messageDone,
                  messageID: $0.id, message: $0, createdAt: $0.createdAt)
        }.filter { $0.seq > after }
    }

    private var cards: [Message] {
        let at = "2026-10-25T12:00:00Z"
        var api = Message(id: "credential-api", threadID: "t-chief", role: MessageRole.assistant,
                          createdAt: at, status: MessageStatus.done, seq: 1, agentID: "t-chief")
        api.parts = [Part(kind: PartKind.credential, credential: CredentialRequest(
            name: "trial-token", label: "Trial key", kind: CredentialRequestKind.api,
            origin: "https://api.example.com", header: "Authorization", scheme: "Bearer",
            ephemeral: true, status: CredentialRequestStatus.saved))]
        var key = Message(id: "credential-ssh", threadID: "t-chief", role: MessageRole.assistant,
                          createdAt: at, status: MessageStatus.done, seq: 2, agentID: "t-chief")
        key.parts = [Part(kind: PartKind.credential, credential: CredentialRequest(
            name: "deploy-key", label: "Deploy key", kind: CredentialRequestKind.ssh, origin: "github.com",
            status: saved ? CredentialRequestStatus.saved : CredentialRequestStatus.pending))]
        return [api, key]
    }
}
#endif
