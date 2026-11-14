import Foundation
import Testing
@testable import SpeechKit

/// The GLiDE gate against its oracle: dictation transcripts with the token ids and
/// P(misheard) the original PyTorch model gave them. See docs/speech.md.
struct OracleBench {
    private struct Oracle: Decodable {
        let rows: [Row]
    }

    private struct Row: Decodable {
        /// "set1" fitted the gate constants; "set2" is held out.
        let set: String
        let hyp: String
        let conf: [Double]
        let bad: Bool
        let p: Double
        let wrap: [Int]
    }

    private static let rows: [Row] = {
        guard let url = Bundle.module.url(forResource: "oracle", withExtension: "json"),
              let data = try? Data(contentsOf: url),
              let oracle = try? JSONDecoder().decode(Oracle.self, from: data) else { return [] }
        return oracle.rows
    }()

    private static let gate = DecisionModels.glide04B.gate

    /// The model is only scored when a client folder holding it is named.
    private static var installed: Bool {
        ProcessInfo.processInfo.environment["POLYMUX_CLIENT_DIR"] != nil && GlideModel.shared.isAvailable
    }

    private static func flagged(_ row: Row, _ probability: Double) -> Bool {
        guard let confidence = DictationGate.score(row.conf) else { return false }
        return gate.score(confidence: 1 - confidence, probability: probability) >= gate.threshold
    }

    @Test func tokenizerMatches() {
        #expect(Self.rows.count == 1200)
        for row in Self.rows {
            #expect(GlideModel.shared.wrap(row.hyp) == row.wrap, "ids differ for: \(row.hyp)")
        }
    }

    @Test(arguments: [("set1", 14, 357), ("set2", 17, 320)])
    func gateCatchesMishears(set: String, missed: Int, flagged: Int) {
        let rows = Self.rows.filter { $0.set == set }
        let flags = rows.map { Self.flagged($0, $0.p) }
        #expect(zip(rows, flags).filter { $0.bad && !$1 }.count == missed)
        #expect(flags.filter { $0 }.count == flagged)
    }

    @Test(.enabled(if: installed)) func modelMatches() async throws {
        var worst = 0.0
        var flips = 0
        for row in Self.rows {
            let probability = try #require(await GlideModel.shared.probability(row.wrap))
            worst = max(worst, abs(probability - row.p))
            if Self.flagged(row, probability) != Self.flagged(row, row.p) { flips += 1 }
        }
        // 16-bit weights move the probability a little; a row on the threshold may flip.
        #expect(worst <= 0.01)
        #expect(flips <= 2)
    }
}
