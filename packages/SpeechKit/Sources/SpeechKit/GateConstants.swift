import Foundation

/// Fitted combined-gate constants for one decision model: a logistic over the
/// recogniser-confidence feature and the model's P(misheard), standardised per
/// feature. A dictation is flagged when the score is at least `threshold`.
public struct GateConstants: Sendable, Codable, Hashable {
    /// Feature means, ordered [confidence, probability].
    public let mu: [Double]
    /// Feature population standard deviations, same order.
    public let sd: [Double]
    /// Logistic weights, same order.
    public let w: [Double]
    public let bias: Double
    public let threshold: Double

    public init(mu: [Double], sd: [Double], w: [Double], bias: Double, threshold: Double) {
        self.mu = mu
        self.sd = sd
        self.w = w
        self.bias = bias
        self.threshold = threshold
    }

    /// Standardised logistic score; higher means more likely misheard.
    public func score(confidence: Double, probability: Double) -> Double {
        let c = (confidence - mu[0]) / sd[0]
        let p = (probability - mu[1]) / sd[1]
        return bias + w[0] * c + w[1] * p
    }
}