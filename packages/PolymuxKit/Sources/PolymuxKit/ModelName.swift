import Foundation

public struct ModelName: Sendable {
    public let model: String
    public let provider: String

    public init(model: String, provider: String = "") {
        self.model = Self.format(model)
        self.provider = Self.format(provider)
    }

    public var title: String {
        model.isEmpty ? "Not set" : [model, provider].filter { !$0.isEmpty }.joined(separator: " ")
    }

    public static func format(_ value: String) -> String {
        value.replacingOccurrences(of: #"(?<=\d)-(?=\d)"#, with: ".", options: .regularExpression)
            .replacingOccurrences(of: "-", with: " ")
            .split(whereSeparator: \.isWhitespace)
            .map { $0.prefix(1).uppercased() + $0.dropFirst() }
            .joined(separator: " ")
    }
}
