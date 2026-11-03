import Testing
@testable import PolymuxKit

struct ModelNameTests {
    @Test(arguments: [
        ("gpt-6.1-sol", "Gpt 6.1 Sol"),
        ("claude-sonnet-4-5", "Claude Sonnet 4.5"),
        ("model-1-2-3", "Model 1.2.3"),
        ("model-12-34-turbo", "Model 12.34 Turbo"),
        ("gpt-4o-mini", "Gpt 4o Mini"),
        ("OpenAI-codex", "OpenAI Codex"),
        ("GPT-5-API", "GPT 5 API"),
        ("  model--name\tpreview  ", "Model Name Preview"),
        ("", "")
    ])
    func formatting(input: String, expected: String) {
        #expect(ModelName.format(input) == expected)
    }

    @Test func modelFirst() {
        let name = ModelName(model: "gpt-6-1-sol", provider: "openAI-provider-2-5")
        #expect(name.model == "Gpt 6.1 Sol")
        #expect(name.provider == "OpenAI Provider 2.5")
        #expect(name.title == "Gpt 6.1 Sol OpenAI Provider 2.5")
    }

    @Test func missingNames() {
        #expect(ModelName(model: "gpt-6-1-sol").title == "Gpt 6.1 Sol")
        #expect(ModelName(model: "", provider: "OpenAI").title == "Not set")
    }
}
