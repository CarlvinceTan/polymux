import Foundation

/// A value a plugin setting can hold.
public enum PluginSettingValue: Codable, Hashable, Sendable {
    case bool(Bool)
    case text(String)

    public init(from decoder: Decoder) throws {
        let container = try decoder.singleValueContainer()
        if let value = try? container.decode(Bool.self) { self = .bool(value) }
        else { self = .text((try? container.decode(String.self)) ?? "") }
    }

    public func encode(to encoder: Encoder) throws {
        var container = encoder.singleValueContainer()
        switch self {
        case .bool(let value): try container.encode(value)
        case .text(let value): try container.encode(value)
        }
    }
}

/// One option a plugin declares, with its current value. Mirrors the server's `GET /plugins/{name}/settings`.
public struct PluginSetting: Codable, Hashable, Sendable, Identifiable {
    public var key: String
    public var title: String
    public var type: String
    public var value: PluginSettingValue?

    public var id: String { key }

    public init(key: String, title: String, type: String, value: PluginSettingValue?) {
        self.key = key
        self.title = title
        self.type = type
        self.value = value
    }
}

/// Where a transport plugin says this server can be reached. Mirrors the server's `GET /transports`.
public struct PluginTransport: Codable, Hashable, Sendable {
    public var plugin: String
    public var title: String
    public var state: String
    public var message: String?
    public var api: String?
    public var device: String?
}
