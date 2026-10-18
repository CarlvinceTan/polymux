import Foundation
import Testing
@testable import PolymuxKit

@Suite(.enabled(if: ProcessInfo.processInfo.environment["POLYMUX_LIVE_URL"] != nil)) @MainActor
struct LiveTests {
    @Test func clientStreamsPersistsAndSearchesOffline() async throws {
        let address = try #require(ProcessInfo.processInfo.environment["POLYMUX_LIVE_URL"])
        let endpoint = try #require(URL(string: address))
        let url = FileManager.default.temporaryDirectory.appendingPathComponent("polymux-live-\(UUID())/cache.sqlite")
        defer { try? FileManager.default.removeItem(at: url.deletingLastPathComponent()) }
        let client = APIClient(baseURL: endpoint)
        let agents = try await client.agents()
        let threads = try await client.threads()
        #expect(agents.contains { $0.id == "research" })
        #expect(threads.contains { $0.id == "research" })
        let cache = try await CacheWork.open(url)
        try await cache.offMain { try $0.save(agents: agents, threads: threads) }
        let chat = ChatStore(client: client, actor: LocalCacheActor(cache), threadID: "research")
        defer { chat.stop() }
        await chat.start()
        for _ in 0..<200 {
            if chat.isConnected { break }
            try await Task.sleep(for: .milliseconds(50))
        }
        #expect(chat.isConnected)
        await chat.send(text: "Reply with exactly this short sentence: Polymux client integration verified.")
        var response = ""
        for _ in 0..<1200 {
            if let reply = chat.messages.last(where: { $0.role == "assistant" && $0.status == "done" }),
               reply.text.lowercased().contains("integration verified") { response = reply.text; break }
            try await Task.sleep(for: .milliseconds(100))
        }
        #expect(response.lowercased().contains("integration verified"))
        #expect(chat.messages.contains { $0.role == "assistant" && $0.status == "done" && $0.text == response })
        chat.stop()
        var seq = try await cache.offMain { try $0.cursor(threadID: "research") }
        #expect(seq > 0)
        while true {
            let tail = try await client.events(threadID: "research", after: seq)
            if tail.isEmpty { break }
            for event in tail { try await cache.offMain { try $0.apply(event) } }
            seq = try await cache.offMain { try $0.cursor(threadID: "research") }
        }
        let relaunched = try await CacheWork.open(url)
        #expect(try await relaunched.offMain { try $0.cursor(threadID: "research") } == seq)
        #expect(try await relaunched.offMain { try $0.messages(threadID: "research") }.contains { $0.text == response })
        let offline = APIClient(baseURL: URL(string: "http://127.0.0.1:1")!)
        let actor = LocalCacheActor(relaunched)
        let search = SearchStore(client: offline, actor: actor, threadID: "research")
        search.query = "integration verified"
        await search.search()
        #expect(search.isOffline)
        #expect(search.results.contains { $0.message.text == response })
        let offlineChat = ChatStore(client: offline, actor: actor, threadID: "research")
        await offlineChat.send(text: "Offline probe stays in this temporary cache.")
        #expect(offlineChat.messages.last?.status == "failed")
        let thirdLaunch = try await CacheWork.open(url)
        #expect(try await thirdLaunch.offMain { try $0.pending(threadID: "research") }.count == 1)
        #expect(try await thirdLaunch.offMain { try $0.messages(threadID: "research") }.last?.status == "failed")
    }
}
