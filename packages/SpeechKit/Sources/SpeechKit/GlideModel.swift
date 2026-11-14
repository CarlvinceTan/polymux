import Foundation
@preconcurrency import CoreML

/// GLiDE-0.4B on-device: tokenises the fixed prompt, runs the converted ModernBERT
/// readout for P(misheard), and returns the combined gate score. Weights are loaded
/// from the model cache (downloaded on demand, never bundled).
public final class GlideModel: DecisionModel, @unchecked Sendable {
    public let descriptor: DecisionModelDescriptor

    /// One instance per process so the compiled model stays loaded across dictations.
    public static let shared = GlideModel()

    private static let cls = 50281
    private static let sep = 50282
    private static let pad = 50283

    private let lock = NSLock()
    private var model: MLModel?

    public init(descriptor: DecisionModelDescriptor = DecisionModels.glide04B) {
        self.descriptor = descriptor
    }

    public var isAvailable: Bool {
        BPETokenizer.shared != nil && ModelCache.compiledModel(descriptor.id) != nil
    }

    public func score(transcript: String, confidences: [Double]) async -> Double? {
        guard isAvailable, let wrap = self.wrap(transcript),
              let probability = await probability(wrap),
              let confidence = DictationGate.score(confidences) else { return nil }
        return descriptor.gate.score(confidence: 1 - confidence, probability: probability)
    }

    /// The [CLS]-wrapped prompt ids fed to the model; nil when the tokenizer is
    /// unavailable or the prompt exceeds the model window.
    public func wrap(_ transcript: String) -> [Int]? {
        guard let tokenizer = BPETokenizer.shared else { return nil }
        let wrap = [Self.cls] + tokenizer.encode(GlidePrompt.text(transcript)) + [Self.sep]
        return wrap.count <= descriptor.maxTokens ? wrap : nil
    }

    /// P(misheard) for wrapped prompt ids; nil when the model is not installed.
    func probability(_ wrap: [Int]) async -> Double? {
        guard let url = ModelCache.compiledModel(descriptor.id), let model = loadModel(at: url) else { return nil }
        return await withCheckedContinuation { continuation in
            DispatchQueue.global(qos: .userInitiated).async {
                continuation.resume(returning: Self.predict(model, wrap: wrap, seq: self.descriptor.maxTokens))
            }
        }
    }

    /// Loads the compiled model once per process; the lock never crosses an await.
    private func loadModel(at url: URL) -> MLModel? {
        lock.lock()
        defer { lock.unlock() }
        if let cached = model { return cached }
        do {
            let config = MLModelConfiguration()
            config.computeUnits = .all
            let loaded = try MLModel(contentsOf: url, configuration: config)
            model = loaded
            return loaded
        } catch {
            return nil
        }
    }

    private static func predict(_ model: MLModel, wrap: [Int], seq: Int) -> Double? {
        let pad = seq - wrap.count
        var ids = [Int32](repeating: Int32(Self.pad), count: seq)
        var mask = [Float](repeating: 0, count: seq)
        for (j, id) in wrap.enumerated() {
            ids[pad + j] = Int32(id)
            mask[pad + j] = 1
        }
        do {
            let idsArray = try MLMultiArray(shape: [1, NSNumber(value: seq)], dataType: .int32)
            let maskArray = try MLMultiArray(shape: [1, NSNumber(value: seq)], dataType: .float32)
            for j in 0..<seq {
                idsArray[j] = NSNumber(value: ids[j])
                maskArray[j] = NSNumber(value: mask[j])
            }
            let input = try MLDictionaryFeatureProvider(dictionary: ["input_ids": idsArray, "mask": maskArray])
            let output = try model.prediction(from: input)
            return output.featureValue(for: "p")?.multiArrayValue?[0].doubleValue
        } catch {
            return nil
        }
    }
}