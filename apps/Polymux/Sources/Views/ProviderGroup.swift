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

    static func collect(_ providers: [Provider], names: [String: String]) -> [Self] {
        var groups: [Self] = []
        for provider in providers {
            if let index = groups.firstIndex(where: { $0.id == provider.type }) {
                groups[index].connections.append(provider)
            } else {
                let name = names[provider.type] ?? ProviderPreset(rawValue: provider.type)?.label ?? provider.name
                groups.append(Self(id: provider.type, name: name, connections: [provider]))
            }
        }
        return groups
    }
}
