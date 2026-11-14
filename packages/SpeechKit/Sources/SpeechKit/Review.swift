import Foundation
import FoundationModels

/// A misheard word the dictionary could fix: what the recogniser wrote and what was meant.
public struct VocabSuggestion: Codable, Hashable, Identifiable, Sendable {
    public var heard: String
    public var term: String
    public var id: String { heard.lowercased() + "→" + term.lowercased() }

    public init(heard: String, term: String) {
        self.heard = heard
        self.term = term
    }
}

/// A cheap check on recogniser confidence that decides whether a dictation is worth a model review.
public enum DictationGate {
    /// Mean confidence below this sends the dictation to the model.
    public static let threshold = 0.92

    /// Mean word confidence; the first and last words are always less certain, so they are left out.
    public static func score(_ confidences: [Double]) -> Double? {
        let inner = confidences.count > 2 ? Array(confidences.dropFirst().dropLast()) : confidences
        guard !inner.isEmpty else { return nil }
        return inner.reduce(0, +) / Double(inner.count)
    }

    public static func needsReview(_ confidences: [Double], threshold: Double = DictationGate.threshold) -> Bool {
        guard let score = score(confidences) else { return false }
        return score < threshold
    }
}

/// Asks the on-device model which words in a dictation were misheard, as dictionary suggestions.
public enum DictationReview {
    @Generable
    struct Findings {
        @Guide(description: "Misrecognised names, products or technical terms only; empty when the text reads correctly.")
        var fixes: [Fix]
    }

    @Generable
    struct Fix {
        @Guide(description: "The words exactly as written in the text.")
        var heard: String
        @Guide(description: "The word or term the speaker most likely said.")
        var meant: String
    }

    public static func suggest(_ text: String, known: [VocabularyEntry] = []) async -> [VocabSuggestion] {
        let model = SystemLanguageModel.default
        guard case .available = model.availability, text.count > 3 else { return [] }
        let session = LanguageModelSession(model: model, instructions: """
            The text was produced by speech recognition and may contain misheard words. \
            Find names, product names and technical terms that were clearly misheard and give what \
            the speaker most likely said. Ignore grammar, punctuation and ordinary words that make sense.
            """)
        guard let findings = try? await session.respond(to: text, generating: Findings.self,
                                                       options: GenerationOptions(temperature: 0)).content else { return [] }
        let skip = Set(known.flatMap { [$0.term] + $0.soundsLike }.map { $0.lowercased() })
        var seen = Set<String>()
        return findings.fixes.compactMap { fix in
            let heard = fix.heard.trimmingCharacters(in: .whitespacesAndNewlines)
            let term = fix.meant.trimmingCharacters(in: .whitespacesAndNewlines)
            guard !heard.isEmpty, !term.isEmpty, heard.split(separator: " ").count <= 3, term.split(separator: " ").count <= 3, heard.count <= Vocabulary.maxChars, term.count <= Vocabulary.maxChars,
                  heard.lowercased() != term.lowercased(), text.range(of: heard, options: .caseInsensitive) != nil,
                  !skip.contains(term.lowercased()), seen.insert(heard.lowercased()).inserted else { return nil }
            return VocabSuggestion(heard: heard, term: term)
        }
    }
}
