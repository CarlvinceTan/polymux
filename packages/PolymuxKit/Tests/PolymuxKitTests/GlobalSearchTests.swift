import Foundation
import Testing
@testable import PolymuxKit

@Suite struct GlobalSearchTests {
    private func cache() throws -> (LocalCache, URL) {
        let directory = FileManager.default.temporaryDirectory.appending(path: "search-\(UUID())")
        let cache = try LocalCache(url: directory.appending(path: "cache.sqlite"))
        try cache.save(agents: [Agent(id: "a", name: "Lyla", role: "University assistant"), Agent(id: "b", name: "Bolt")],
                       threads: [Thread(id: "a", agentID: "a", title: "Lyla"), Thread(id: "b", agentID: "b", title: "Bolt")])
        return (cache, directory)
    }

    @Test func findsDownloadedContentAcrossConversations() throws {
        let (cache, directory) = try cache()
        defer { try? FileManager.default.removeItem(at: directory) }
        let file = Attachment(id: "file", name: "robotics-plan.md", mimeType: "text/markdown")
        let image = Attachment(id: "image", name: "robotics-sketch.png", mimeType: "image/png")
        try cache.merge([
            Message(id: "one", threadID: "a", parts: [Part(kind: "text", text: "robotics meeting"), Part(kind: "attachment", attachment: file)], seq: 1),
            Message(id: "two", threadID: "b", parts: [Part(kind: "text", text: "robotics prototype"), Part(kind: "attachment", attachment: image)], seq: 1)
        ])
        #expect(Set(try cache.globalSearch(query: "robotics", kind: "messages").map(\.threadID)) == ["a", "b"])
        #expect(try cache.globalSearch(query: "robotics", kind: "files").map(\.attachment?.id) == ["file"])
        #expect(try cache.globalSearch(query: "robotics", kind: "media").map(\.attachment?.id) == ["image"])
        #expect(try cache.globalSearch(query: "plan.md", kind: "files").first?.title == "robotics-plan.md")
        #expect(try cache.globalSearch(query: "University", kind: "bots").first?.title == "Lyla")
    }

    @Test func retainsSemanticResultsAndTheirNavigationContent() throws {
        let (cache, directory) = try cache()
        defer { try? FileManager.default.removeItem(at: directory) }
        let message = Message(id: "one", threadID: "a", parts: [Part(kind: "text", text: "orange balloon")], status: "done", seq: 1)
        let hit = SearchResult(id: "message/one", kind: "messages", title: "Lyla", snippet: "orange balloon",
                               threadID: "a", messageID: message.id, message: message)
        try cache.cacheSearch([hit], query: "fruit", kind: "all")
        #expect(try cache.globalSearch(query: "fruit", kind: "all") == [hit])
        #expect(try cache.messages(threadID: "a").first?.id == message.id)
        try cache.merge([Message(id: "two", threadID: "b", parts: [Part(kind: "text", text: "fresh fruit")], seq: 1)])
        #expect(Set(try cache.globalSearch(query: "fruit", kind: "all").map(\.messageID)) == ["one", "two"])
        try cache.save(agents: [Agent(id: "b", name: "Bolt")], threads: [Thread(id: "b", agentID: "b", title: "Bolt")])
        #expect(try cache.globalSearch(query: "fruit", kind: "all").map(\.messageID) == ["two"])
    }

    @Test func matchesPrefixesInDownloadedContent() throws {
        let (cache, directory) = try cache()
        defer { try? FileManager.default.removeItem(at: directory) }
        let file = Attachment(id: "file", name: "itinerary.pdf", mimeType: "application/pdf",
                              extractedText: "Vaccination requirements")
        let image = Attachment(id: "image", name: "sunset.png", mimeType: "image/png")
        try cache.merge([
            Message(id: "one", threadID: "a", parts: [Part(kind: "text", text: "Passport renewal checklist")], seq: 1),
            Message(id: "two", threadID: "b", parts: [Part(kind: "text", text: "Shared document"), Part(kind: "attachment", attachment: file)], seq: 1),
            Message(id: "three", threadID: "b", parts: [Part(kind: "text", text: "Photo attached"), Part(kind: "attachment", attachment: image)], seq: 2)
        ])
        for query in ["pa", "pass", "passport", "PASS", "a pass"] {
            #expect(try cache.globalSearch(query: query, kind: "messages").map(\.id) == ["message/one"])
        }
        #expect(try cache.globalSearch(query: "iti", kind: "files").map(\.attachment?.id) == ["file"])
        #expect(try cache.globalSearch(query: "vacc", kind: "files").map(\.attachment?.id) == ["file"])
        #expect(try cache.globalSearch(query: "sun", kind: "media").map(\.attachment?.id) == ["image"])
        #expect(try cache.search(threadID: "b", query: "pass").isEmpty)
    }
}
