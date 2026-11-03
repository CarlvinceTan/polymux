#if DEBUG
import Foundation
import PolymuxKit

enum SiteFixture {
    private struct Example: Decodable {
        let id, name, shape, color, at, prompt, reply: String
        var threadID: String { "t-" + id }

        var agent: Agent {
            Agent(id: threadID, name: name, active: true,
                  avatar: AvatarDesign(shape: shape, color: color, expression: "neutre",
                                       followCursor: false, animates: false))
        }

        var messages: [Message] {
            [(MessageRole.user, prompt), (MessageRole.assistant, reply)].enumerated().map { index, item in
                Message(id: "site-\(id)-\(index)", threadID: threadID, role: item.0,
                        parts: [Part(kind: PartKind.text, text: item.1)], createdAt: at,
                        status: MessageStatus.done, seq: Int64(index + 1),
                        agentID: index == 0 ? nil : threadID)
            }
        }

        var thread: PolymuxKit.Thread {
            PolymuxKit.Thread(id: threadID, agentID: threadID, lastSeq: 2, unreadCount: 0,
                              lastMessage: messages.last, title: "", agentIDs: [threadID])
        }
    }

    private static var enabled: Bool {
        VerifyFixture.enabled && CommandLine.arguments.contains("--verify-site")
    }

    private static let examples: [Example] = {
        guard let index = CommandLine.arguments.firstIndex(of: "--verify-site") else { return [] }
        let next = CommandLine.arguments.indices.contains(index + 1) ? CommandLine.arguments[index + 1] : ""
        let file = next.isEmpty || next.hasPrefix("-")
            ? URL(fileURLWithPath: #filePath).deletingLastPathComponent().deletingLastPathComponent()
                .deletingLastPathComponent().appendingPathComponent("site/src/content/demo.json")
            : URL(fileURLWithPath: next)
        return (try? JSONDecoder().decode([Example].self, from: Data(contentsOf: file))) ?? []
    }()

    static func handle(_ fixture: VerifyFixture) -> Bool {
        guard enabled, let url = fixture.request.url else { return false }
        let example = examples.first { url.pathComponents.contains($0.threadID) }
        if url.lastPathComponent == "stream" {
            let response = HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil,
                                           headerFields: ["Content-Type": "text/event-stream"])!
            fixture.client?.urlProtocol(fixture, didReceive: response, cacheStoragePolicy: .notAllowed)
            fixture.client?.urlProtocol(fixture, didLoad: Data(": connected\n\n".utf8))
            return true
        }
        do {
            let encoder = JSONEncoder()
            let data: Data
            switch url.lastPathComponent {
            case "agents": data = try encoder.encode(examples.map(\.agent))
            case "threads": data = try encoder.encode(examples.map(\.thread))
            case "sidebar":
                let main = Section(id: SidebarLayout.unassignedID, name: "Unassigned", order: 0)
                data = try encoder.encode(SidebarSnapshot(cursor: 0, sections: [main], placements: examples.map {
                    SidebarPlacement(threadID: $0.threadID, pinned: false)
                }))
            case "messages": data = try encoder.encode(example?.messages ?? [])
            case "events":
                let after = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems?
                    .first { $0.name == "after" }?.value.flatMap(Int64.init) ?? 0
                let events = example?.messages.filter { $0.seq > after }.map {
                    Event(seq: $0.seq, conversationID: $0.threadID, type: EventType.messageDone,
                          message: $0, createdAt: $0.createdAt)
                } ?? []
                data = try encoder.encode(events)
            case "settings":
                data = try encoder.encode(Settings(appearance: VerifyFixture.appearance,
                                                   liveProvider: "gemini",
                                                   liveModel: "gemini-3.8-live", liveVoice: "Aoede"))
            case "tool-runs", "routines", "mcp", "providers": data = Data("[]".utf8)
            default:
                if let example, url.lastPathComponent == example.threadID {
                    data = try encoder.encode(AgentProfile(agent: example.agent, prompt: "Website example."))
                } else {
                    data = Data("{}".utf8)
                }
            }
            let response = HTTPURLResponse(url: url, statusCode: 200, httpVersion: nil,
                                           headerFields: ["Content-Type": "application/json"])!
            fixture.client?.urlProtocol(fixture, didReceive: response, cacheStoragePolicy: .notAllowed)
            fixture.client?.urlProtocol(fixture, didLoad: data)
            fixture.client?.urlProtocolDidFinishLoading(fixture)
        } catch {
            fixture.client?.urlProtocol(fixture, didFailWithError: error)
        }
        return true
    }
}
#endif
