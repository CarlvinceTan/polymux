import Foundation
import Testing
@testable import PolymuxKit

@Suite struct RoutineTests {
    @Test func schedulesRoundTrip() throws {
        let request = RoutineRequest(prompt: "Review my draft", at: "2026-10-10T09:00:00Z", intervalSeconds: 86400)
        let routine = Routine(at: request.at, nextAt: request.at, intervalSeconds: request.intervalSeconds,
                              status: "active", createdAt: "2026-10-09T09:00:00Z", id: "routine", agentID: "a",
                              trigger: "interval", prompt: request.prompt)
        let data = try JSONEncoder().encode(routine)
        #expect(try JSONDecoder().decode(Routine.self, from: data) == routine)
        let settings = Proactivity(enabled: true, intervalSeconds: 14400, nextAt: request.at)
        #expect(try JSONDecoder().decode(Proactivity.self, from: JSONEncoder().encode(settings)) == settings)
    }

    @Test func noticesReplayWithoutAStreamingBubble() throws {
        let url = FileManager.default.temporaryDirectory.appendingPathComponent(UUID().uuidString).appendingPathComponent("db")
        let cache = try LocalCache(url: url)
        let agent = Agent(id: "a", name: "A")
        let thread = Thread(id: "a", agentID: "a", agentIDs: ["a"])
        try cache.save(agents: [agent], threads: [thread])
        let message = Message(id: "notice", threadID: "a", role: "assistant",
                              parts: [Part(kind: "text", text: "Your outline is ready.")],
                              createdAt: "2026-10-09T09:00:00Z", status: "done", seq: 1, agentID: "a")
        let start = Event(seq: 1, conversationID: "a", type: "message.start", messageID: message.id,
                          agentID: "a", message: message, createdAt: message.createdAt)
        let done = Event(seq: 2, conversationID: "a", type: "message.done", messageID: message.id,
                         agentID: "a", message: message, createdAt: message.createdAt)
        try cache.apply(start)
        try cache.apply(done)
        try cache.apply(done)
        #expect(try cache.messages(threadID: "a").count == 1)
        #expect(try cache.messages(threadID: "a").first?.status == "done")
        #expect(try cache.threads().first?.unreadCount == 1)
    }
}
