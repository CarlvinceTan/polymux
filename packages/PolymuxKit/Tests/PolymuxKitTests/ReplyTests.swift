import Foundation
import Testing
@testable import PolymuxKit

@Suite
struct ReplyTests {
    @Test func requestUsesReferenceAndMessageKeepsSnapshot() throws {
        let quote = ReplyContext(messageID: "target", role: "assistant", text: "Quoted words")
        let request = SendRequest(id: "user", text: "My response", replyToID: quote.messageID)
        let data = try JSONEncoder().encode(request)
        let fields = try #require(JSONSerialization.jsonObject(with: data) as? [String: Any])
        #expect(fields["replyToID"] as? String == "target")
        #expect(fields["reply"] == nil)
        #expect(try JSONDecoder().decode(SendRequest.self, from: data) == request)

        let plain = try JSONEncoder().encode(SendRequest(id: "plain", text: "No quote"))
        #expect(try (JSONSerialization.jsonObject(with: plain) as? [String: Any])?["replyToID"] == nil)
        let message = Message(id: request.id, role: "user", reply: quote)
        let restored = try JSONDecoder().decode(Message.self, from: JSONEncoder().encode(message))
        #expect(restored.reply == quote)
        #expect(restored.replyToID == nil)
        let assistant = Message(id: "answer", role: "assistant", replyToID: request.id)
        let correlated = try JSONDecoder().decode(Message.self, from: JSONEncoder().encode(assistant))
        #expect(correlated.replyToID == request.id)
        #expect(correlated.reply == nil)
    }

    @Test func selectedQuoteKeepsExactPartIdentity() throws {
        let first = Attachment(id: "first", kind: "image", name: "Same.png")
        let second = Attachment(id: "second", kind: "image", name: "Same.png")
        let message = Message(id: "original", role: "assistant",
                              parts: [Part(kind: "text", text: "Caption"),
                                      Part(kind: "attachment", attachment: first),
                                      Part(kind: "tool", text: "Hidden tool data"),
                                      Part(kind: "attachment", attachment: second),
                                      Part(kind: "text", text: "Other text")], agentID: "research")
        let text = try #require(message.replyContext(part: 0))
        #expect(text.text == "Caption")
        #expect(text.attachment == nil)
        #expect(text.partIndex == 0)
        let image = try #require(message.replyContext(part: 3))
        #expect(image.text.isEmpty)
        #expect(image.attachment == second)
        #expect(image.partIndex == 3)
        #expect(image.agentID == "research")
        #expect(message.replyContext(part: 4)?.text == "Other text")
        #expect(message.replyContext(part: 2) == nil)
        #expect(message.replyContext(part: -1) == nil)
        #expect(message.replyContext(part: 5) == nil)
        let malformed = Message(parts: [Part(kind: "text", text: ""), Part(kind: "text"), Part(kind: "attachment")])
        #expect(malformed.parts.indices.allSatisfy { malformed.replyContext(part: $0) == nil })
        #expect(message.replyContext.partIndex == nil)
        let request = SendRequest(id: "response", text: "This image", replyToID: image.messageID, replyPart: image.partIndex)
        let data = try JSONEncoder().encode(request)
        let fields = try #require(JSONSerialization.jsonObject(with: data) as? [String: Any])
        #expect(fields["replyPart"] as? Int == 3)
        #expect(try JSONDecoder().decode(SendRequest.self, from: data) == request)
        #expect(try JSONDecoder().decode(ReplyContext.self, from: JSONEncoder().encode(image)) == image)
    }

    @Test func attachmentOnlyPendingMessageMatchesServerPartOrder() throws {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString)
        defer { try? FileManager.default.removeItem(at: folder) }
        let cache = try LocalCache(url: folder.appendingPathComponent("cache.db"))
        let attachments = [Attachment(id: "one", kind: "image"), Attachment(id: "two", kind: "image")]
        let request = SendRequest(id: "images", attachmentIDs: attachments.map(\.id))
        let optimistic = try cache.enqueue(threadID: "chat", request: request, attachments: attachments)
        #expect(optimistic.parts.count == 2)
        #expect(optimistic.replyContext(part: 0)?.attachment == attachments[0])
        #expect(optimistic.replyContext(part: 1)?.attachment == attachments[1])
        var accepted = optimistic
        accepted.seq = 1
        accepted.status = "done"
        try cache.apply(Event(seq: 1, conversationID: "chat", type: "message.start", message: accepted))
        let restored = try #require(cache.messages(threadID: "chat").first)
        #expect(restored.replyContext(part: 1) == optimistic.replyContext(part: 1))
    }
}
