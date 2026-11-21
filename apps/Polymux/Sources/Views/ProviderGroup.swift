import PolymuxKit

struct ProviderGroup: Identifiable {
    let id: String
    let name: String
    var connections: [Provider]

    var models: [ProviderModel] {
        var result: [ProviderModel] = []
        var indices: [String: Int] = [:]
        for connection in connections {
            for var model in connection.models {
                model.enabled = connection.enabled && model.enabled
                if let index = indices[model.id] {
                    result[index].enabled = result[index].enabled || model.enabled
                } else {
                    indices[model.id] = result.count
                    result.append(model)
                }
            }
        }
        return result
    }

    var summary: String {
        let models = models
        let count = models.count
        let active = models.filter(\.enabled).count
        let status = count > 0 && active == count ? "all active" : "\(active) active"
        let prefix = connections.count > 1 ? "\(connections.count) connections · " : ""
        return "\(prefix)\(count) \(count == 1 ? "model" : "models") (\(status))"
    }

    /// What groups connections together: a server you point at stands alone, others share their type.
    static func id(of provider: Provider) -> String { server(provider.type) ? provider.id : provider.type }

    static func collect(_ providers: [Provider], names: [String: String]) -> [Self] {
        var groups: [Self] = []
        for provider in providers {
            // The user names servers they point at; each one gets its own row.
            let own = server(provider.type)
            let id = Self.id(of: provider)
            if let index = groups.firstIndex(where: { $0.id == id }) {
                groups[index].connections.append(provider)
            } else {
                let preset = ProviderPreset(rawValue: provider.type)?.label
                let name = own ? (provider.name.isEmpty ? preset ?? provider.type : provider.name)
                    : names[provider.type] ?? preset ?? provider.name
                groups.append(Self(id: id, name: name, connections: [provider]))
            }
        }
        return groups
    }

    private static func server(_ type: String) -> Bool {
        switch ProviderPreset(rawValue: type) {
        case .custom, .ollama, .lmstudio: true
        default: false
        }
    }
}
