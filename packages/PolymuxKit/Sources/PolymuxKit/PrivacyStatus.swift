import Foundation

/// The macOS permissions the server, and the bots it runs, have been granted.
public struct PrivacyStatus: Codable, Equatable, Sendable {
    public var fullDiskAccess: Bool
    public var screenRecording: Bool
    public var accessibility: Bool

    public init(fullDiskAccess: Bool = false, screenRecording: Bool = false, accessibility: Bool = false) {
        self.fullDiskAccess = fullDiskAccess
        self.screenRecording = screenRecording
        self.accessibility = accessibility
    }

    /// A server older than Accessibility reporting leaves it false.
    public init(from decoder: Decoder) throws {
        let values = try decoder.container(keyedBy: CodingKeys.self)
        fullDiskAccess = try values.decode(Bool.self, forKey: .fullDiskAccess)
        screenRecording = try values.decode(Bool.self, forKey: .screenRecording)
        accessibility = try values.decodeIfPresent(Bool.self, forKey: .accessibility) ?? false
    }
}
