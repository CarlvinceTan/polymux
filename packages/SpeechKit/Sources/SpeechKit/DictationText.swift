import Foundation

/// Applies the user's dictation settings to a finished transcript.
public enum DictationText {
    public static func clean(_ raw: String, options: DictationOptions, vocabulary: [VocabularyEntry] = [],
                             capitalizeStart: Bool = true) -> String {
        var text = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        if options.fillerWords {
            text = FillerWords.apply(text).trimmingCharacters(in: .whitespacesAndNewlines)
        }
        if options.spokenSymbols {
            text = replacing(Self.symbols, in: text)
        }
        if options.spokenFormatting {
            text = replacing(Self.formatting, in: text, guarded: ["period", "colon", "dash", "hyphen"])
        }
        if options.outputFixes {
            text = fixes(text, capitalizeStart: capitalizeStart)
        }
        if !vocabulary.isEmpty {
            text = Vocabulary.replace(text, entries: vocabulary)
        }
        return text.trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// Adds the spaces the recogniser left out at either side of the insertion.
    public static func spaced(_ text: String, before: Character?, after: Character?) -> String {
        guard !text.isEmpty else { return text }
        var out = text
        if let before, !before.isWhitespace, let first = out.first, !first.isWhitespace,
           !",.;:!?)]}”’".contains(first) {
            out = " " + out
        }
        if let after, !after.isWhitespace, let last = out.last, !last.isWhitespace,
           !",.;:!?)]}”’".contains(after) {
            out += " "
        }
        return out
    }

    /// Symbol names, as whole words only.
    private static let symbols: [(String, String)] = [
        ("forward slash", "/"),
        ("backslash", "\\"),
        ("open parenthesis", "("),
        ("left parenthesis", "("),
        ("close parenthesis", ")"),
        ("right parenthesis", ")"),
        ("open bracket", "["),
        ("close bracket", "]"),
        ("open brace", "{"),
        ("close brace", "}"),
        ("open quote", "“"),
        ("close quote", "”"),
        ("at sign", "@"),
        ("underscore", "_"),
        ("plus sign", "+"),
        ("equals sign", "="),
        ("percent sign", "%"),
        ("dollar sign", "$"),
        ("hash sign", "#"),
        ("number sign", "#"),
        ("asterisk", "*"),
        ("ampersand", "&"),
    ]

    /// Punctuation and layout words. Risky everyday words only convert at a phrase boundary.
    private static let formatting: [(String, String)] = [
        ("new paragraph", "\n\n"),
        ("new line", "\n"),
        ("question mark", "?"),
        ("exclamation mark", "!"),
        ("exclamation point", "!"),
        ("full stop", "."),
        ("ellipsis", "…"),
        ("semicolon", ";"),
        ("colon", ":"),
        ("comma", ","),
        ("period", "."),
        ("hyphen", "-"),
        ("dash", "-"),
    ]

    /// Conservative spacing and capitalisation fixes; the model polish layer may add more.
    private static func fixes(_ text: String, capitalizeStart: Bool) -> String {
        var out = text
        out = out.replacingOccurrences(of: "[ \\t]+([,.;:!?])", with: "$1", options: .regularExpression)
        out = out.replacingOccurrences(of: "[ \\t]{2,}", with: " ", options: .regularExpression)
        out = out.replacingOccurrences(of: "[ \\t]+\\n", with: "\n", options: .regularExpression)
        out = out.replacingOccurrences(of: "\\n[ \\t]+", with: "\n", options: .regularExpression)
        out = out.replacingOccurrences(of: "\\n{3,}", with: "\n\n", options: .regularExpression)
        out = capitalizeStarts(out, initial: capitalizeStart)
        if let regex = try? NSRegularExpression(pattern: "(?i)(?<![\\p{L}\\p{N}])i(?![\\p{L}\\p{N}])") {
            let range = NSRange(location: 0, length: (out as NSString).length)
            out = regex.stringByReplacingMatches(in: out, range: range, withTemplate: "I")
        }
        return out
    }

    private static func capitalizeStarts(_ text: String, initial: Bool) -> String {
        let chars = Array(text)
        var out = ""
        out.reserveCapacity(chars.count)
        var capitalize = initial
        for (index, char) in chars.enumerated() {
            if capitalize, char.isLetter {
                out.append(contentsOf: char.uppercased())
                capitalize = false
            } else {
                out.append(char)
                if char == "\n" {
                    capitalize = true
                } else if ".!?".contains(char) {
                    let next = index + 1 < chars.count ? chars[index + 1] : nil
                    if next == nil || next!.isWhitespace { capitalize = true }
                }
            }
        }
        return out
    }

    /// Replaces whole-word phrases, longest first, so "question mark" wins over "mark".
    /// Guarded words only convert at the end of a phrase or before a capital, so
    /// "a period of time" stays words while "hello period Next" becomes punctuation.
    private static func replacing(_ words: [(String, String)], in text: String,
                                  guarded: Set<String> = []) -> String {
        guard !text.isEmpty else { return text }
        let sorted = words.sorted { $0.0.count > $1.0.count }
        var replacements: [String: String] = [:]
        for (word, symbol) in sorted {
            replacements[word.lowercased()] = symbol
        }
        let alternatives = sorted.map { NSRegularExpression.escapedPattern(for: $0.0) }
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
            if guarded.contains(heard) {
                let end = match.range.location + match.range.length
                var next = end
                while next < source.length, source.character(at: next) == 32 || source.character(at: next) == 9 {
                    next += 1
                }
                let follows = next < source.length ? source.character(at: next) : nil
                let converts: Bool
                if let follows {
                    if let scalar = UnicodeScalar(follows) {
                        converts = follows == 10 || Character(scalar).isUppercase
                    } else {
                        converts = false
                    }
                } else {
                    converts = true
                }
                out += converts ? (replacements[heard] ?? source.substring(with: match.range))
                    : source.substring(with: match.range)
            } else {
                out += replacements[heard] ?? source.substring(with: match.range)
            }
            cursor = match.range.location + match.range.length
        }
        out += source.substring(from: cursor)
        return out
    }
}
