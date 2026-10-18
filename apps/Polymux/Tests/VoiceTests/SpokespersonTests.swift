import Foundation
import Testing
import PolymuxKit
@testable import PolymuxVoice

@MainActor
private final class TextProbe: TextProvider {
    var answers: [TextAnswer]
    var turns: [[TextTurn]] = []
    var options: [LiveOptions] = []

    init(_ answers: [TextAnswer]) { self.answers = answers }
    func generate(turns: [TextTurn], options: LiveOptions, key: String) async throws -> TextAnswer {
        self.turns.append(turns)
        self.options.append(options)
        guard !answers.isEmpty else { throw LiveFailure.invalidMessage }
        return answers.removeFirst()
    }
}

@MainActor
private final class WorkerProbe: VoiceReply {
    var requests: [String] = []
    var cancellations = 0
    func preview(_ text: String) {}
    func stream(_ text: String) -> AsyncThrowingStream<VoiceFragment, Error> {
        requests.append(text)
        return AsyncThrowingStream { continuation in
            continuation.yield(VoiceFragment(text: "Detailed private worker findings", agentID: "worker"))
            continuation.finish()
        }
    }
    func stop() async { cancellations += 1 }
}

@Test @MainActor func pipelineSpokespersonHandlesConversationWithoutWorker() async throws {
    let provider = TextProbe([TextAnswer(text: "Good morning!")])
    let worker = WorkerProbe()
    let spokesperson = Spokesperson(options: .init(model: "fake", voice: "", instructions: "Spokesperson"),
                                   delegate: worker, makeProvider: { provider }, key: { "placeholder" })
    var replies: [VoiceFragment] = []
    for try await fragment in spokesperson.stream("Good morning") { replies.append(fragment) }
    #expect(replies == [VoiceFragment(text: "Good morning!")])
    #expect(worker.requests.isEmpty)
    #expect(provider.options.first?.tools.first?.name == "ask_agent")
    await spokesperson.stop()
    #expect(worker.cancellations == 0)
}

@Test @MainActor func pipelineSpokespersonConsultsThenSpeaksItsOwnSummary() async throws {
    let call = LiveCall(id: "consult", name: "ask_agent", arguments: Data(#"{"text":"Find the user’s notes"}"#.utf8))
    let provider = TextProbe([TextAnswer(text: "I’ll ask the team. ", calls: [call]), TextAnswer(text: "They found your notes.")])
    let worker = WorkerProbe()
    let spokesperson = Spokesperson(options: .init(model: "fake", voice: "", instructions: "Spokesperson"),
                                   delegate: worker, makeProvider: { provider }, key: { "placeholder" })
    var replies: [VoiceFragment] = []
    for try await fragment in spokesperson.stream("Can you find my notes?") { replies.append(fragment) }
    #expect(worker.requests == ["Find the user’s notes"])
    #expect(replies.map(\.text).joined() == "I’ll ask the team. They found your notes.")
    #expect(replies.allSatisfy { $0.agentID == nil })
    #expect(provider.turns.last?.last?.result?.text == "Detailed private worker findings")
    await spokesperson.stop()
}

@Test @MainActor func pipelineSpokespersonKeepsIndependentConversation() async throws {
    let provider = TextProbe([TextAnswer(text: "Hello"), TextAnswer(text: "Still here")])
    let worker = WorkerProbe()
    let spokesperson = Spokesperson(options: .init(model: "fake", voice: "", instructions: "Spokesperson"),
                                   delegate: worker, makeProvider: { provider }, key: { "placeholder" })
    for try await _ in spokesperson.stream("Hi") {}
    for try await _ in spokesperson.stream("Remember our greeting?") {}
    #expect(provider.turns.last?.count == 3)
    #expect(provider.turns.last?.first?.text == "Hi")
    #expect(worker.requests.isEmpty)
    await spokesperson.stop()
}

@Test @MainActor func pipelineSpokespersonRejectsUnapprovedTool() async throws {
    let call = LiveCall(id: "bad", name: "bash", arguments: Data(#"{"text":"Do not execute"}"#.utf8))
    let provider = TextProbe([TextAnswer(text: "", calls: [call]), TextAnswer(text: "I cannot do that directly.")])
    let worker = WorkerProbe()
    let spokesperson = Spokesperson(options: .init(model: "fake", voice: "", instructions: "Spokesperson"),
                                   delegate: worker, makeProvider: { provider }, key: { "placeholder" })
    for try await _ in spokesperson.stream("Hi") {}
    #expect(worker.requests.isEmpty)
    #expect(provider.turns.last?.last?.result?.failed == true)
    await spokesperson.stop()
}
