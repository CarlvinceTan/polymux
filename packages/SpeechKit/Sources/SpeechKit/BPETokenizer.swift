import Foundation

/// Byte-level BPE port of the GLiDE encoder tokenizer (bundled tokenizer.json):
/// NFC, added-token longest-match split, GPT-2 regex pre-split, byte→unicode map,
/// then lowest-rank greedy merges. Parsed once; `encode` returns the bare ids
/// (no [CLS]/[SEP] — the caller wraps them).
final class BPETokenizer: @unchecked Sendable {
    static let shared = BPETokenizer()

    private let vocab: [String: Int]
    private let ranks: [String: Int]
    private let added: Trie
    private let byteEncoder: [UInt8: Character]
    private let regex: NSRegularExpression
    private let unk: Int

    init?() {
        guard let url = Bundle.module.url(forResource: "tokenizer", withExtension: "json", subdirectory: "Resources"),
              let data = try? Data(contentsOf: url),
              let obj = try? JSONSerialization.jsonObject(with: data) as? [String: Any],
              let model = obj["model"] as? [String: Any],
              let vocabRaw = model["vocab"] as? [String: Any],
              let mergesRaw = model["merges"] as? [String],
              let addedRaw = obj["added_tokens"] as? [[String: Any]] else { return nil }

        var vocab = [String: Int]()
        for (key, value) in vocabRaw { if let n = value as? Int { vocab[key] = n } }
        self.vocab = vocab
        self.unk = vocab["[UNK]"] ?? 50280

        var ranks = [String: Int]()
        for (i, merge) in mergesRaw.enumerated() { ranks[merge] = i }
        self.ranks = ranks

        let trie = Trie()
        for entry in addedRaw {
            if let content = entry["content"] as? String, let id = entry["id"] as? Int {
                trie.insert(content, id: id)
            }
        }
        self.added = trie
        self.byteEncoder = Self.makeByteEncoder()

        let pattern = "'s|'t|'re|'ve|'m|'ll|'d| ?\\p{L}+| ?\\p{N}+| ?[^\\s\\p{L}\\p{N}]+|\\s+(?!\\S)|\\s+"
        guard let regex = try? NSRegularExpression(pattern: pattern) else { return nil }
        self.regex = regex
    }

    func encode(_ text: String) -> [Int] {
        let chars = Array(text.precomposedStringWithCanonicalMapping)
        var ids = [Int]()
        var plain = [Character]()
        func flush() {
            guard !plain.isEmpty else { return }
            ids.append(contentsOf: bpe(String(plain)))
            plain.removeAll()
        }
        var i = 0
        while i < chars.count {
            if let (id, length) = added.longestMatch(chars, at: i) {
                flush()
                ids.append(id)
                i += length
            } else {
                plain.append(chars[i])
                i += 1
            }
        }
        flush()
        return ids
    }

    private func bpe(_ segment: String) -> [Int] {
        let ns = segment as NSString
        var ids = [Int]()
        regex.enumerateMatches(in: segment, range: NSRange(location: 0, length: ns.length)) { match, _, _ in
            guard let match else { return }
            ids.append(contentsOf: bpeWord(ns.substring(with: match.range)))
        }
        return ids
    }

    private func bpeWord(_ piece: String) -> [Int] {
        var symbols = piece.utf8.compactMap { byteEncoder[$0].map(String.init) }
        while symbols.count > 1 {
            var bestRank = Int.max
            var pair: (String, String)?
            for j in 0..<(symbols.count - 1) {
                if let rank = ranks[symbols[j] + " " + symbols[j + 1]], rank < bestRank {
                    bestRank = rank
                    pair = (symbols[j], symbols[j + 1])
                }
            }
            guard let (a, b) = pair else { break }
            var merged = [String]()
            var k = 0
            while k < symbols.count {
                if k < symbols.count - 1, symbols[k] == a, symbols[k + 1] == b {
                    merged.append(a + b)
                    k += 2
                } else {
                    merged.append(symbols[k])
                    k += 1
                }
            }
            symbols = merged
        }
        return symbols.map { vocab[$0] ?? unk }
    }

    private static func makeByteEncoder() -> [UInt8: Character] {
        var bs = Array(33...126) + Array(161...172) + Array(174...255)
        var cs = bs
        var n = 0
        for b in 0...255 where !bs.contains(b) {
            bs.append(b)
            cs.append(256 + n)
            n += 1
        }
        var map = [UInt8: Character]()
        for (b, c) in zip(bs, cs) { map[UInt8(b)] = Character(UnicodeScalar(c)!) }
        return map
    }

    private final class Trie {
        final class Node {
            var children = [Character: Node]()
            var id: Int?
        }
        let root = Node()

        func insert(_ word: String, id: Int) {
            var node = root
            for ch in word {
                if let next = node.children[ch] {
                    node = next
                } else {
                    let next = Node()
                    node.children[ch] = next
                    node = next
                }
            }
            node.id = id
        }

        func longestMatch(_ chars: [Character], at start: Int) -> (id: Int, length: Int)? {
            var node = root
            var best: (Int, Int)?
            var i = start
            while i < chars.count, let next = node.children[chars[i]] {
                node = next
                i += 1
                if let id = node.id { best = (id, i - start) }
            }
            return best
        }
    }
}