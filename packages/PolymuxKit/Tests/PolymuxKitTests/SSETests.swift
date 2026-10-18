import Foundation
import Testing
@testable import PolymuxKit

struct SSETests {
    @Test func consecutiveFramesKeepBlankBoundaries() throws {
        let first = Event(seq: 1, conversationID: "one", type: "message.delta", text: "Hello 👋", createdAt: "2026-10-09T10:00:00Z")
        let second = Event(seq: 2, conversationID: "one", type: "message.done", createdAt: "2026-10-09T10:00:00Z")
        let one = String(decoding: try JSONEncoder().encode(first), as: UTF8.self)
        let two = String(decoding: try JSONEncoder().encode(second), as: UTF8.self)
        let wire = ": keepalive\r\n\r\nid: 1\r\nevent: message.delta\r\ndata: \(one)\r\n\r\nid: 2\nevent: message.done\ndata: \(two)\n\n"
        var parser = SSEParser()
        var received: [Event] = []
        for byte in wire.utf8 {
            if let event = try parser.feed(byte) { received.append(event) }
        }
        #expect(received == [first, second])
        #expect(try parser.finish() == nil)
    }

    @Test func multilineJSONAndUnterminatedFinalFrame() throws {
        let event = Event(seq: 1, conversationID: "one", type: "agent.presence", agentID: "a", active: false, createdAt: "2026-10-09T10:00:00Z")
        let encoder = JSONEncoder()
        encoder.outputFormatting = .prettyPrinted
        let json = String(decoding: try encoder.encode(event), as: UTF8.self)
        let wire = json.split(separator: "\n").map { "data: \($0)" }.joined(separator: "\n")
        var parser = SSEParser()
        for byte in wire.utf8 { #expect(try parser.feed(byte) == nil) }
        #expect(try parser.finish() == event)
    }
}
