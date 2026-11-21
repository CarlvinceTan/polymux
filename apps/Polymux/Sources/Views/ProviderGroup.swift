import PolymuxKit

/// A provider as the model lists show it.
struct ProviderGroup: Identifiable {
    let id: String
    let name: String
    let provider: Provider

    /// Its models, off when the provider is.
    var models: [ProviderModel] {
        provider.models.map { model in
            var model = model
            model.enabled = provider.enabled && model.enabled
            return model
        }
    }

    var summary: String {
        let models = models
        let count = models.count
        let active = models.filter(\.enabled).count
        let status = count > 0 && active == count ? "all active" : "\(active) active"
        let connections = provider.connections.count
        let prefix = connections > 1 ? "\(connections) connections · " : ""
        return "\(prefix)\(count) \(count == 1 ? "model" : "models") (\(status))"
    }

    static func collect(_ providers: [Provider]) -> [Self] {
        providers.map { Self(id: $0.id, name: $0.name.isEmpty ? $0.type : $0.name, provider: $0) }
    }
}

extension Provider {
    /// Whether one of its enabled connections is signed in or holds a key.
    var signedIn: Bool { connections.contains { $0.enabled && !($0.status ?? "").isEmpty } }
}
