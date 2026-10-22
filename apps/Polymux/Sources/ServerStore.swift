import Foundation
import Observation

struct ServerDevice: Codable, Identifiable {
    let address: String
    var name: String

    var id: String { address }
}

@MainActor @Observable
final class ServerStore {
    private(set) var devices: [ServerDevice]
    private let defaults: UserDefaults

    init(defaults: UserDefaults = .standard) {
        self.defaults = defaults
        devices = defaults.data(forKey: "serverDevices")
            .flatMap { try? JSONDecoder().decode([ServerDevice].self, from: $0) } ?? []
    }

    func remember(_ address: String, name: String? = nil) {
        guard let url = ServerAddress.parse(address) else { return }
        if let index = devices.firstIndex(where: { $0.address == address }) {
            guard let name, devices[index].name != name else { return }
            devices[index].name = name
        } else {
            devices.append(ServerDevice(address: address, name: name ?? url.host() ?? "Server"))
        }
        if let data = try? JSONEncoder().encode(devices) {
            defaults.set(data, forKey: "serverDevices")
        }
    }
}
