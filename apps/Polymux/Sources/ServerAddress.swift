import Foundation
import PolymuxKit

enum ServerAddress {
    static func resolve(_ address: String, defaults: UserDefaults) -> URL? {
        #if DEBUG
        return parse(defaults.string(forKey: "PolymuxServer") ?? address)
        #else
        return parse(address)
        #endif
    }

    static func parse(_ value: String) -> URL? {
        ServerURL.parse(value)
    }
}
