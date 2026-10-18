import Foundation
import Testing
@testable import PolymuxKit

@Test @MainActor func textSpokespersonPreservesFunctionSignaturesWithoutSpeakingThoughts() throws {
    let data = Data(#"{"candidates":[{"content":{"parts":[{"text":"Private reasoning","thought":true},{"text":"Let me ask."},{"functionCall":{"id":"call-1","name":"ask_agent","args":{"text":"Find notes"}},"thoughtSignature":"opaque-signature"}]}}]}"#.utf8)
    let answer = try GeminiText.answer(data)
    #expect(answer.text == "Let me ask.")
    #expect(answer.calls.first?.id == "call-1")
    let body = try GeminiText.body(turns: [TextTurn(role: .user, text: "Hi"),
        TextTurn(role: .assistant, text: answer.text, state: answer.state),
        TextTurn(role: .tool, result: .init(call: answer.calls[0], text: "Private findings"))],
        options: .init(model: "gemini-3.8-flash", voice: "", instructions: "Spokesperson"))
    let object = try #require(JSONSerialization.jsonObject(with: body) as? [String: Any])
    let contents = try #require(object["contents"] as? [[String: Any]])
    let parts = try #require(contents[1]["parts"] as? [[String: Any]])
    #expect(parts.last?["thoughtSignature"] as? String == "opaque-signature")
    let resultParts = try #require(contents[2]["parts"] as? [[String: Any]])
    #expect((resultParts.first?["functionResponse"] as? [String: Any])?["id"] as? String == "call-1")
}

@Test @MainActor func textSpokespersonRejectsMalformedResponses() {
    #expect(throws: LiveFailure.self) { try GeminiText.answer(Data(#"{"candidates":[]}"#.utf8)) }
    #expect(throws: LiveFailure.self) { try GeminiText.answer(Data("bad json".utf8)) }
}
