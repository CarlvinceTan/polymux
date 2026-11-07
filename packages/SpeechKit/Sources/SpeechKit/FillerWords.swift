import Foundation

/// Hesitations ("um", "uh", …) leave a dictation with no model involved.
/// A filler matches as a whole word, lowercase or with only its first letter
/// capitalised, so "umbrella", "Uhura" and "ER" stay. The comma or ellipsis
/// that comes with a filler goes with it, and a capitalised filler that
/// started a sentence hands its capital to the next word. Line breaks are kept.
public enum FillerWords {
    private static let fillers = ["um", "umm", "uh", "uhh", "erm", "er", "ah"]

    public static func apply(_ transcript: String) -> String {
        let chars = Array(transcript)
        var out = ""
        out.reserveCapacity(chars.count)
        // A filler was just removed: the next text decides the spacing.
        var removed = false
        var capitalize = false
        var index = 0
        while index < chars.count {
            if let end = fillerAt(chars, index) {
                trimEnd(&out, also: [])
                if !removed { capitalize = chars[index].isUppercase && sentenceStart(out) }
                index = afterFiller(chars, end)
                removed = true
                continue
            }
            let c = chars[index]
            index += 1
            if removed {
                if isSpace(c) { continue }
                if c == "\n" {
                    removed = false
                    capitalize = false
                    out.append(c)
                    continue
                }
                if (c == "." || c == "!" || c == "?" || c == ",") && sentenceStart(out) { continue }
                removed = false
                let closing = ".;:!?)]}”’…".contains(c)
                    || (c == "\"" && (index >= chars.count || !(chars[index].isLetter || chars[index].isNumber)))
                if closing {
                    trimEnd(&out, also: [","])
                    capitalize = false
                } else {
                    if needsSpace(out) { out.append(" ") }
                    if capitalize {
                        capitalize = false
                        out.append(Character(c.uppercased()))
                        continue
                    }
                }
            }
            out.append(c)
        }
        if removed { trimEnd(&out, also: [","]) }
        return out
    }

    private static func joinsWord(_ c: Character) -> Bool {
        c.isLetter || c.isNumber || c == "-" || c == "'" || c == "’"
    }

    /// Where the filler starting at `start` ends, if one does.
    private static func fillerAt(_ chars: [Character], _ start: Int) -> Int? {
        if start > 0 && joinsWord(chars[start - 1]) { return nil }
        for word in fillers {
            let letters = Array(word)
            let end = start + letters.count
            guard end <= chars.count else { continue }
            var matches = true
            for (n, expected) in letters.enumerated() {
                let c = chars[start + n]
                if c != expected && !(n == 0 && c == Character(expected.uppercased())) { matches = false; break }
            }
            let joined = end < chars.count && joinsWord(chars[end])
            if matches && !joined { return end }
        }
        return nil
    }

    /// Where the text continues after a filler ending at `end`, past its own comma or ellipsis.
    private static func afterFiller(_ chars: [Character], _ end: Int) -> Int {
        var index = skipSpaces(chars, end)
        if index < chars.count {
            let c = chars[index]
            if c == "," || c == "…" { index += 1 }
            else if c == "." && index + 2 < chars.count && chars[index + 1] == "." && chars[index + 2] == "." { index += 3 }
        }
        return skipSpaces(chars, index)
    }

    private static func isSpace(_ c: Character) -> Bool { c.isWhitespace && c != "\n" }

    private static func skipSpaces(_ chars: [Character], _ index: Int) -> Int {
        var next = index
        while next < chars.count && isSpace(chars[next]) { next += 1 }
        return next
    }

    private static func trimEnd(_ out: inout String, also: [Character]) {
        while let last = out.last, isSpace(last) || also.contains(last) { out.removeLast() }
    }

    private static func sentenceStart(_ out: String) -> Bool {
        guard let last = out.last else { return true }
        return last == "." || last == "!" || last == "?" || last == "\n"
    }

    private static func needsSpace(_ out: String) -> Bool {
        guard let last = out.last else { return false }
        switch last {
        case "\n", "(", "[", "{", "“", "‘": return false
        case "\"":
            guard let before = out.dropLast().last else { return false }
            return !before.isWhitespace
        default: return true
        }
    }
}
