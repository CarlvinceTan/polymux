import Foundation

/// The macOS permissions the server, and the bots it runs, have been granted.
public struct PrivacyStatus: Codable, Equatable, Sendable {
    public var fullDiskAccess: Bool
    public var screenRecording: Bool

    public init(fullDiskAccess: Bool = false, screenRecording: Bool = false) {
        self.fullDiskAccess = fullDiskAccess
        self.screenRecording = screenRecording
    }
}
