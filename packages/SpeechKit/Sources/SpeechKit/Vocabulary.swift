import Foundation

/// Words the recogniser should hear correctly: app, agent and chat names.
/// Terms are cleaned and deduplicated like Hex's vocabulary lists; short
/// lists bias best, so the recognised-term cap is kept low.
public enum Vocabulary {
    public static let base = ["Polymux"]
    static let maxTerms = 64
    static let maxChars = 64

    /// Trimmed, whitespace-collapsed, case-insensitively deduplicated terms, capped.
    public static func terms(_ names: [String]) -> [String] {
        var seen = Set<String>()
        var out: [String] = []
        for name in names {
            let term = name
                .trimmingCharacters(in: .whitespacesAndNewlines)
                .replacingOccurrences(of: "\\s+", with: " ", options: .regularExpression)
            guard !term.isEmpty, term.count <= maxChars else { continue }
            guard seen.insert(term.lowercased()).inserted else { continue }
            out.append(term)
            if out.count == maxTerms { break }
        }
        return out
    }
}
