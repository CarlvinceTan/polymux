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

@Test func dictionaryEntriesCleanAndDeduplicate() {
    let entries = [
        VocabularyEntry(term: " Qwen ", soundsLike: ["Quen", "quen", " Qwen "]),
        VocabularyEntry(term: "qwen", soundsLike: ["Queue"]),
        VocabularyEntry(term: "  "),
    ]
    let out = Vocabulary.clean(entries)
    #expect(out.map(\.term) == ["Qwen"])
    #expect(out.first?.soundsLike == ["Quen"])
}

@Test func biasIncludesNamesTermsAndSoundsLike() {
    let entries = [VocabularyEntry(term: "Qwen", soundsLike: ["Quen"])]
    #expect(Vocabulary.bias(entries, names: ["Polymux", "Chief"]) == ["Qwen", "Quen", "Polymux", "Chief"])
}

@Test func heardAliasesBecomeTheDictionaryTerm() {
    let entries = [VocabularyEntry(term: "Qwen", soundsLike: ["Quen", "queue in"])]
    #expect(Vocabulary.replace("Ask quen and queue in about QWEN.", entries: entries)
        == "Ask Qwen and Qwen about Qwen.")
    #expect(Vocabulary.replace("The queens left.", entries: entries) == "The queens left.")
}

@Test func aliasesAreCleanedBeforeReplacing() {
    let entries = [VocabularyEntry(term: "cvmac", soundsLike: ["CV  mac"])]
    #expect(Vocabulary.replace("Open CV mac.", entries: entries) == "Open cvmac.")
}
