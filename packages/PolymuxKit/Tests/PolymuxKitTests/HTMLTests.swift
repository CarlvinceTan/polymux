import Foundation
import Testing
@testable import PolymuxKit

@Suite
struct HTMLTests {
    @Test func cardReplaySurvivesRelaunch() throws {
        let folder = FileManager.default.temporaryDirectory.appendingPathComponent("polymux-html-\(UUID())")
        defer { try? FileManager.default.removeItem(at: folder) }
        let url = folder.appendingPathComponent("cache.sqlite")
        let cache = try LocalCache(url: url)
        let date = WireDate.string()
        var message = Message(id: "reply", threadID: "one", role: "assistant", parts: [Part(kind: "text", text: "Here is the chart.")], createdAt: date, status: "streaming", seq: 1)
        try cache.apply(Event(seq: 1, conversationID: "one", type: "message.start", message: message, createdAt: date))
        let card = Attachment(id: "card", kind: "html", name: "Balloon.html", mimeType: "text/html", size: 128, url: "/attachments/card/content", extractedText: "Orange balloon comparison", indexingState: "ready", width: 320.5, height: 180)
        message.parts.append(Part(kind: "attachment", attachment: card))
        let event = Event(seq: 2, conversationID: "one", type: "message.attachment", messageID: message.id, message: message, attachment: card, createdAt: date)
        try cache.apply(event)
        try cache.apply(event)
        message.status = "done"
        try cache.apply(Event(seq: 3, conversationID: "one", type: "message.done", message: message, createdAt: date))
        let reopened = try LocalCache(url: url)
        #expect(try reopened.messages(threadID: "one") == [message])
        #expect(try reopened.messages(threadID: "one").first?.attachments == [card])
        #expect(try reopened.messages(threadID: "one").first?.attachments.first?.width == 320.5)
        #expect(try reopened.messages(threadID: "one").first?.attachments.first?.height == 180)
        #expect(try reopened.cursor(threadID: "one") == 3)
        #expect(try reopened.search(threadID: "one", query: "balloon").map(\.id) == [message.id])
    }

    @Test func dimensionsAreOptionalOnWire() throws {
        let measured = Attachment(id: "measured", kind: "html", name: "Chart.html", mimeType: "text/html", url: "/attachments/measured/content", indexingState: "ready")
        let data = try JSONEncoder().encode(measured)
        let fields = try #require(JSONSerialization.jsonObject(with: data) as? [String: Any])
        #expect(fields["width"] == nil)
        #expect(fields["height"] == nil)
        #expect(try JSONDecoder().decode(Attachment.self, from: data) == measured)
    }
}
