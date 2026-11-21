import Testing
@testable import SpeechKit

@Test func gateIgnoresEdgeWords() {
    // The first and last words are always less certain and must not trigger a review.
    #expect(!DictationGate.needsReview([0.4, 0.97, 0.96, 0.95, 0.5]))
}

@Test func gateFlagsLowConfidence() {
    #expect(DictationGate.needsReview([0.9, 0.5, 0.6, 0.9, 0.9]))
}

@Test func gateSkipsUnknownConfidence() {
    #expect(!DictationGate.needsReview([]))
}

/// Rows from the held-out eval the constants were fitted against: the confidence
/// feature is 1 − mean(inner confidences), the probability comes from the model.
private func gateScore(_ confidences: [Double], probability: Double) -> Double {
    let gate = DecisionModels.glide04B.gate
    let confidence = DictationGate.score(confidences).map { 1 - $0 } ?? 0
    return gate.score(confidence: confidence, probability: probability)
}

@Test func combinedGateFlagsMisheardDictation() {
    // "Ask perplexity to find the release date and added to the notes": p is high
    // enough to flag despite decent confidences; the oracle marked it misheard.
    let score = gateScore([0.8587381591796875, 0.9217022705078125, 0.9686358032226563, 0.9661220703125,
                           0.9752892456054687, 0.9760010375976562, 0.9728197631835938, 0.9765381469726563,
                           0.9681310424804688, 0.9784058837890625, 0.973167724609375, 0.92825341796875],
                          probability: 0.46541187167167664)
    #expect(abs(score - 0.142083) < 1e-4)
    #expect(score >= DecisionModels.glide04B.gate.threshold)
}

@Test func combinedGatePassesCleanDictation() {
    // High confidence and a low model probability; the oracle marked it clean.
    let score = gateScore([0.7726985473632813, 0.9441749877929687, 0.9795044555664062, 0.982080322265625,
                           0.9798056640625, 0.9737578125, 0.96421826171875, 0.942068115234375,
                           0.9455888061523438, 0.9643453369140625, 0.9745953979492188, 0.9517283325195313],
                          probability: 0.14616990089416504)
    #expect(abs(score - (-2.093945)) < 1e-4)
    #expect(score < DecisionModels.glide04B.gate.threshold)
}

@Test func combinedGateUsesModelProbabilityWhenConfidenceIsWeak() {
    // Confidence alone would not flag this, but the model probability does.
    let score = gateScore([0.667820068359375, 0.824578125, 0.9377599487304688, 0.9624314575195313,
                           0.9580780029296875, 0.952826171875, 0.9323451538085937, 0.8490724487304687,
                           0.9342337646484375, 0.951819091796875, 0.9510078125, 0.9211096801757812,
                           0.8962277221679688],
                          probability: 0.2994101643562317)
    #expect(abs(score - (-0.335437)) < 1e-4)
    #expect(score >= DecisionModels.glide04B.gate.threshold)
}
