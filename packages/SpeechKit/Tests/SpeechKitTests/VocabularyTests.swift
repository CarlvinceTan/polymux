import Testing
@testable import SpeechKit

@Test func termsCleanCollapseAndDeduplicate() {
    let out = Vocabulary.terms([" Ada  Lovelace ", "", "   ", "ada lovelace", "Grace"])
    #expect(out == ["Ada Lovelace", "Grace"])
}

@Test func overlongTermsAreDropped() {
    let long = String(repeating: "a", count: 65)
    #expect(Vocabulary.terms([long, "ok"]) == ["ok"])
}

@Test func termListIsCapped() {
    let out = Vocabulary.terms((0..<200).map { "agent-\($0)" })
    #expect(out.count == Vocabulary.maxTerms)
    #expect(out.first == "agent-0")
}
