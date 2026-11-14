import Foundation
import Testing
@testable import SpeechKit

/// Parity against the GLiDE encoder tokenizer: each fixture is an eval transcript
/// with the exact [CLS]-wrapped ids the Python oracle produced for its prompt.
struct TokenizerTests {
    private struct Fixture: Decodable {
        let hyp: String
        let wrap: [Int]
    }

    private static let fixtures: [Fixture] = {
        guard let url = Bundle.module.url(forResource: "tokenizer_fixtures", withExtension: "json"),
              let data = try? Data(contentsOf: url),
              let list = try? JSONDecoder().decode([Fixture].self, from: data) else { return [] }
        return list
    }()

    @Test func tokenizesEvalPrompts() throws {
        let tokenizer = try #require(BPETokenizer.shared)
        #expect(!Self.fixtures.isEmpty)
        for fixture in Self.fixtures {
            let wrap = [50281] + tokenizer.encode(GlidePrompt.text(fixture.hyp)) + [50282]
            #expect(wrap == fixture.wrap, "mismatch for: \(fixture.hyp)")
        }
    }

    @Test func maxPromptFitsModelWindow() throws {
        let tokenizer = try #require(BPETokenizer.shared)
        let longest = Self.fixtures.map { fixture -> Int in
            let wrap = tokenizer.encode(GlidePrompt.text(fixture.hyp))
            return wrap.count + 2
        }.max() ?? 0
        #expect(longest <= DecisionModels.glide04B.maxTokens)
    }
}
