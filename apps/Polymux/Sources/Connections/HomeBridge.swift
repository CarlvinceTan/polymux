#if os(iOS)
import HomeKit

/// Apple Home accessories and scenes, through HomeKit.
@MainActor
final class HomeBridge: NSObject, HMHomeManagerDelegate {
    static let shared = HomeBridge()

    private var manager: HMHomeManager?
    private var loaded = false
    private var waiting: [CheckedContinuation<Void, Never>] = []

    /// Characteristic each settable value maps to.
    private static let targets: [String: String] = [
        "power": HMCharacteristicTypePowerState,
        "brightness": HMCharacteristicTypeBrightness,
        "lock": HMCharacteristicTypeTargetLockMechanismState,
        "temperature": HMCharacteristicTypeTargetTemperature,
    ]

    struct Accessory: Encodable {
        let id: String
        let name: String
        let room: String?
        let category: String
        let reachable: Bool
        var power: Bool?
        var brightness: Int?
        var locked: Bool?
        var temperature: Double?
        var targetTemperature: Double?
    }

    struct Home: Encodable {
        let id: String
        let name: String
        let primary: Bool
        let accessories: [Accessory]
        let scenes: [Scene]
    }

    struct Scene: Encodable {
        let id: String
        let name: String
    }

    func authorize() async throws {
        let manager = await ready()
        guard manager.authorizationStatus.contains(.authorized) else {
            throw ConnectionError("Allow Polymux to use Home in Settings.")
        }
    }

    func run(_ command: DeviceCommand) async throws -> Data {
        let homes = try await ready(authorized: true).homes
        switch command.action {
        case "list":
            var listed: [Home] = []
            for home in homes {
                var accessories: [Accessory] = []
                for accessory in home.accessories { accessories.append(await describe(accessory)) }
                listed.append(Home(id: home.uniqueIdentifier.uuidString, name: home.name, primary: home.isPrimary,
                                   accessories: accessories,
                                   scenes: home.actionSets.map { Scene(id: $0.uniqueIdentifier.uuidString, name: $0.name) }))
            }
            return try encoded(listed)
        case "set":
            guard let id = command.accessory, let name = command.characteristic, let type = Self.targets[name], let number = command.value,
                  let accessory = homes.flatMap(\.accessories).first(where: { $0.uniqueIdentifier.uuidString == id }) else {
                throw ConnectionError("Choose an accessory ID from list, a characteristic and a value.")
            }
            guard let characteristic = accessory.services.flatMap(\.characteristics).first(where: { $0.characteristicType == type }) else {
                throw ConnectionError("\(accessory.name) has no \(name) control.")
            }
            let value: Any = switch name {
            case "power": number != 0
            case "temperature": number
            default: Int(number.rounded())
            }
            try await limited(15, "\(accessory.name) didn't respond.") { @MainActor in try await characteristic.writeValue(value) }
            return try encoded(await describe(accessory))
        case "scene":
            for home in homes {
                if let scene = home.actionSets.first(where: { $0.uniqueIdentifier.uuidString == command.scene }) {
                    try await limited(15, "\(scene.name) didn't finish.") { @MainActor in try await home.executeActionSet(scene) }
                    return try encoded(Scene(id: scene.uniqueIdentifier.uuidString, name: scene.name))
                }
            }
            throw ConnectionError("Choose a scene ID from list.")
        default:
            throw ConnectionError("Use list, set or scene.")
        }
    }

    private func describe(_ accessory: HMAccessory) async -> Accessory {
        var result = Accessory(id: accessory.uniqueIdentifier.uuidString, name: accessory.name, room: accessory.room?.name,
                               category: accessory.category.localizedDescription, reachable: accessory.isReachable)
        for characteristic in accessory.services.flatMap(\.characteristics) {
            let type = characteristic.characteristicType
            guard [HMCharacteristicTypePowerState, HMCharacteristicTypeBrightness, HMCharacteristicTypeCurrentLockMechanismState,
                   HMCharacteristicTypeCurrentTemperature, HMCharacteristicTypeTargetTemperature].contains(type) else { continue }
            if accessory.isReachable { try? await limited(5, "") { @MainActor in try await characteristic.readValue() } }
            let value = characteristic.value as? NSNumber
            switch type {
            case HMCharacteristicTypePowerState: result.power = value?.boolValue
            case HMCharacteristicTypeBrightness: result.brightness = value?.intValue
            case HMCharacteristicTypeCurrentLockMechanismState: result.locked = value.map { $0.intValue == 1 }
            case HMCharacteristicTypeCurrentTemperature: result.temperature = value?.doubleValue
            default: result.targetTemperature = value?.doubleValue
            }
        }
        return result
    }

    private func ready(authorized: Bool = false) async throws -> HMHomeManager {
        let manager = await ready()
        if authorized && !manager.authorizationStatus.contains(.authorized) {
            throw ConnectionError("Home access is off for Polymux on this device.")
        }
        return manager
    }

    /// The home manager, once it has loaded the user's homes.
    private func ready() async -> HMHomeManager {
        let manager = manager ?? HMHomeManager()
        if self.manager == nil {
            self.manager = manager
            manager.delegate = self
        }
        if !loaded { await withCheckedContinuation { waiting.append($0) } }
        return manager
    }

    nonisolated func homeManagerDidUpdateHomes(_ manager: HMHomeManager) {
        Task { @MainActor in finish() }
    }

    // A denied request never loads homes, so the answer itself ends the wait.
    nonisolated func homeManager(_ manager: HMHomeManager, didUpdate status: HMHomeManagerAuthorizationStatus) {
        guard status.contains(.determined), !status.contains(.authorized) else { return }
        Task { @MainActor in finish() }
    }

    private func finish() {
        loaded = true
        waiting.forEach { $0.resume() }
        waiting.removeAll()
    }
}
#endif
