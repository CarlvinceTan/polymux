import Foundation
import Testing
@testable import PolymuxKit

@Suite @MainActor
struct OpenAIRealtimeTests {
    @Test func setupDeclaresAudioAndTools() throws {
        let tool = LiveTool(name: "ask_agent", description: "Consult", parameters: Data(#"{"type":"object"}"#.utf8))
        let data = try OpenAIRealtime.setup(LiveOptions(model: "gpt-realtime", voice: "marin", instructions: "Be brief", tools: [tool]))
        let message = try #require(JSONSerialization.jsonObject(with: data) as? [String: Any])
        let session = try #require(message["session"] as? [String: Any])
        #expect(message["type"] as? String == "session.update")
        #expect(session["instructions"] as? String == "Be brief")
        #expect((session["tools"] as? [[String: Any]])?.first?["name"] as? String == "ask_agent")
    }

    @Test func decodesSpeechTranscriptsAndCalls() throws {
        let audio = Data([1, 0, 2, 0]).base64EncodedString()
        #expect(try OpenAIRealtime.decode(Data(#"{"type":"response.output_audio.delta","delta":"\#(audio)"}"#.utf8))
                == [.audio(.init(data: Data([1, 0, 2, 0]), rate: 24_000))])
        #expect(try OpenAIRealtime.decode(Data(#"{"type":"response.output_audio_transcript.delta","delta":"Hi"}"#.utf8)) == [.response("Hi")])
        #expect(try OpenAIRealtime.decode(Data(#"{"type":"conversation.item.input_audio_transcription.delta","delta":"Hey"}"#.utf8)) == [.transcript("Hey")])
        #expect(try OpenAIRealtime.decode(Data(#"{"type":"input_audio_buffer.speech_started"}"#.utf8)) == [.interrupted])
        let done = #"{"type":"response.done","response":{"output":[{"type":"function_call","call_id":"c1","name":"ask_agent","arguments":"{\"text\":\"x\"}"}],"usage":{"input_tokens":3,"output_tokens":4,"total_tokens":7}}}"#
        #expect(try OpenAIRealtime.decode(Data(done.utf8)) == [
            .calls([LiveCall(id: "c1", name: "ask_agent", arguments: Data(#"{"text":"x"}"#.utf8))]),
            .usage(.init(input: 3, output: 4, total: 7)), .turnComplete(idle: false)])
        #expect(throws: LiveFailure.self) { try OpenAIRealtime.decode(Data(#"{"type":"error","error":{"message":"bad"}}"#.utf8)) }
    }
}
