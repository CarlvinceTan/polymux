import Foundation

/// Everything the app needs to offer, download and run one decision model.
/// The gate depends only on this description, never on a specific model.
public struct DecisionModelDescriptor: Sendable, Hashable {
    public let id: String
    public let displayName: String
    /// Where the converted model archive is fetched from; nil until a host is configured.
    public let downloadURL: URL?
    public let byteSize: Int64
    /// SHA-256 of the downloaded archive, verified before use; nil when unpublished.
    public let sha256: String?
    /// Attribution the license requires near the feature.
    public let licenseNotice: String
    public let gate: GateConstants
    /// Prompts longer than this fall back to the confidence-only gate.
    public let maxTokens: Int

    public init(id: String, displayName: String, downloadURL: URL?, byteSize: Int64, sha256: String?,
                licenseNotice: String, gate: GateConstants, maxTokens: Int) {
        self.id = id
        self.displayName = displayName
        self.downloadURL = downloadURL
        self.byteSize = byteSize
        self.sha256 = sha256
        self.licenseNotice = licenseNotice
        self.gate = gate
        self.maxTokens = maxTokens
    }
}

/// A swappable on-device scorer that decides whether a dictation is worth a model review.
public protocol DecisionModel: AnyObject, Sendable {
    var descriptor: DecisionModelDescriptor { get }
    /// True when the model is downloaded and ready to score.
    var isAvailable: Bool { get }
    /// Combined gate score (higher = more likely misheard), or nil when the model
    /// cannot score this transcript (not ready, or too long).
    func score(transcript: String, confidences: [Double]) async -> Double?
}

/// The known decision models. GLiDE-0.4B is the first; others register here without
/// touching the gate.
public enum DecisionModels {
    /// Fitted on the held-out eval (variant "a"): held-out FN 17/241, flag 53%, acc 81%.
    public static let glide04B = DecisionModelDescriptor(
        id: "glide-0.4b",
        displayName: "GLiDE 0.4B",
        downloadURL: URL(string: "https://github.com/CarlvinceTan/polymux/releases/download/models/glide-0.4b.tar"),
        byteSize: 790_334_464,
        sha256: "565248bf6acd39eba0ad1712c9d9b151dffd1e719e303e956ebdc910933da5b1",
        licenseNotice: "Built with Fastino GLiDE",
        gate: GateConstants(mu: [0.114230225010415, 0.2858353562653065],
                            sd: [0.10470903221297216, 0.2029116127454439],
                            w: [1.7384098615132817, 1.4493768572378511],
                            bias: 0.21930574753156917,
                            threshold: -1.3090432768358349),
        maxTokens: 256)

    public static let all: [DecisionModelDescriptor] = [glide04B]
    public static let `default` = glide04B

    public static func descriptor(id: String) -> DecisionModelDescriptor? {
        all.first { $0.id == id }
    }
}