import Foundation
import Observation

struct ServerDevice: Codable, Identifiable, Equatable {
    let address: String
    var name: String
    /// What its server calls this network; nil until a device has named it.
    var network: String?
    /// Set once the server has answered; an address that never did stays out of the switcher.
    var serverID: String?
    /// The server refuses this device, so it's hidden until it lets the device back.
    var denied: Bool?

    var id: String { address }
    var title: String { network ?? name }
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

    /// Servers that have answered at least once and still allow this device.
    var known: [ServerDevice] { devices.filter { $0.serverID != nil && $0.denied != true } }

    func remember(_ address: String, name: String? = nil, serverID: String? = nil, network: String? = nil) {
        guard let url = ServerAddress.parse(address) else { return }
        if let index = devices.firstIndex(where: { $0.address == address }) {
            let updated = ServerDevice(address: address, name: name ?? devices[index].name, network: network ?? devices[index].network,
                                       serverID: serverID ?? devices[index].serverID, denied: devices[index].denied)
            guard devices[index] != updated else { return }
            devices[index] = updated
        } else {
            devices.append(ServerDevice(address: address, name: name ?? url.host() ?? "Server", network: network, serverID: serverID))
        }
        save()
    }

    func forget(_ address: String) {
        devices.removeAll { $0.address == address }
        save()
    }

    func deny(_ address: String, _ denied: Bool) {
        guard let index = devices.firstIndex(where: { $0.address == address }), (devices[index].denied == true) != denied else { return }
        devices[index].denied = denied ? true : nil
        save()
    }

    private func save() {
        if let data = try? JSONEncoder().encode(devices) {
            defaults.set(data, forKey: "serverDevices")
        }
    }
}
