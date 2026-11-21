import Foundation

/// A word the recogniser should hear correctly, plus the ways it is often misheard.
public struct VocabularyEntry: Codable, Hashable, Identifiable, Sendable {
    public var id: UUID
    public var term: String
    public var soundsLike: [String]

    public init(id: UUID = UUID(), term: String, soundsLike: [String] = []) {
        self.id = id
        self.term = term
        self.soundsLike = soundsLike
    }
}

/// Words the recogniser should hear correctly: app, agent and chat names.
/// Terms are cleaned and deduplicated like Hex's vocabulary lists; short
/// lists bias best, so the recognised-term cap is kept low.
public enum Vocabulary {
    public static let base = ["Polymux"]
    static let maxTerms = 64
    static let maxChars = 64
    public static let maxEntries = 64
    static let maxSoundsLike = 8

    /// Trimmed, whitespace-collapsed, case-insensitively deduplicated terms, capped.
    public static func terms(_ names: [String]) -> [String] {
        var seen = Set<String>()
        var out: [String] = []
        for name in names {
            let term = clean(name)
            guard !term.isEmpty, term.count <= maxChars else { continue }
            guard seen.insert(term.lowercased()).inserted else { continue }
            out.append(term)
            if out.count == maxTerms { break }
        }
        return out
    }

    /// The cleaned dictionary, in the order the user gave it.
    public static func clean(_ entries: [VocabularyEntry]) -> [VocabularyEntry] {
        var seen = Set<String>()
        var out: [VocabularyEntry] = []
        for entry in entries {
            let term = clean(entry.term)
            guard !term.isEmpty, term.count <= maxChars, seen.insert(term.lowercased()).inserted else { continue }
            var aliases = Set<String>()
            let soundsLike = entry.soundsLike.compactMap { alias -> String? in
                let alias = clean(alias)
                guard !alias.isEmpty, alias.count <= maxChars, alias.lowercased() != term.lowercased(),
                      aliases.insert(alias.lowercased()).inserted else { return nil }
                return alias
            }
            out.append(VocabularyEntry(id: entry.id, term: term, soundsLike: Array(soundsLike.prefix(maxSoundsLike))))
            if out.count == maxEntries { break }
        }
        return out
    }

    /// The words that bias recognition: dictionary terms and aliases first, then names.
    public static func bias(_ entries: [VocabularyEntry], names: [String]) -> [String] {
        terms(clean(entries).flatMap { [$0.term] + $0.soundsLike } + names)
    }

    /// Replaces an alias the recogniser heard with its dictionary term.
    public static func replace(_ text: String, entries: [VocabularyEntry]) -> String {
        let entries = clean(entries)
        var canonical: [String: String] = [:]
        for entry in entries {
            for alias in [entry.term] + entry.soundsLike {
                let key = alias.lowercased()
                if canonical[key] == nil { canonical[key] = entry.term }
            }
        }
        guard !canonical.isEmpty, !text.isEmpty else { return text }
        let alternatives = canonical.keys.sorted { $0.count > $1.count }
            .map { NSRegularExpression.escapedPattern(for: $0) }
            .joined(separator: "|")
        guard let regex = try? NSRegularExpression(
            pattern: "(?i)(?<![\\p{L}\\p{N}])(" + alternatives + ")(?![\\p{L}\\p{N}])"
        ) else { return text }
        let source = text as NSString
        let matches = regex.matches(in: text, range: NSRange(location: 0, length: source.length))
        guard !matches.isEmpty else { return text }
        var out = ""
        var cursor = 0
        for match in matches {
            out += source.substring(with: NSRange(location: cursor, length: match.range.location - cursor))
            let heard = source.substring(with: match.range).lowercased()
            out += canonical[heard] ?? source.substring(with: match.range)
            cursor = match.range.location + match.range.length
        }
        out += source.substring(from: cursor)
        return out
    }

    private static func clean(_ name: String) -> String {
        name.trimmingCharacters(in: .whitespacesAndNewlines)
            .replacingOccurrences(of: "\\s+", with: " ", options: .regularExpression)
    }
}
