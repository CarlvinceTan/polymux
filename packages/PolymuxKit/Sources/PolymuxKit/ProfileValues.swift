import Foundation

/// Editable profile fields, preserving local changes when the server updates them.
public struct ProfileValues: Codable, Equatable, Sendable {
    public var name: String
    public var role: String
    public var prompt: String

    public init(name: String, role: String, prompt: String = "") {
        self.name = name
        self.role = role
        self.prompt = prompt
    }

    public func merging(_ update: Self, saved: Self) -> Self {
        Self(name: Self.clean(name) == saved.name && update.name != saved.name ? update.name : name,
             role: Self.clean(role) == saved.role && update.role != saved.role ? update.role : role,
             prompt: Self.clean(prompt) == saved.prompt && update.prompt != saved.prompt ? update.prompt : prompt)
    }

    private static func clean(_ value: String) -> String { value.trimmingCharacters(in: .whitespacesAndNewlines) }
}
