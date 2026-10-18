import Foundation
import Testing
@testable import PolymuxKit

@Suite
struct ClientPathsTests {
    @Test func desktopLayout() {
        let home = URL(fileURLWithPath: "/Users/example", isDirectory: true)
        let support = home.appending(path: "Library/Application Support", directoryHint: .isDirectory)
        let root = ClientPaths.root(home: home, support: support, environment: [:], desktop: true)
        #expect(root.path == "/Users/example/.polymux")
        #expect(ClientPaths.cache("http://localhost:7337", root: root).path ==
                "/Users/example/.polymux/app-349688521712662452/cache.sqlite")
    }

    @Test func dataOverride() {
        let home = URL(fileURLWithPath: "/Users/example", isDirectory: true)
        let support = home.appending(path: "Library/Application Support", directoryHint: .isDirectory)
        let environment = ["POLYMUX_DATA_DIR": "/private/test-data"]
        #expect(ClientPaths.root(home: home, support: support, environment: environment, desktop: true).path ==
                "/private/test-data")
        #expect(ClientPaths.root(home: home, support: support, environment: ["POLYMUX_DATA_DIR": ""], desktop: true).path ==
                "/Users/example/.polymux")
    }

    @Test func mobileLayout() {
        let home = URL(fileURLWithPath: "/var/mobile", isDirectory: true)
        let support = home.appending(path: "Library/Application Support", directoryHint: .isDirectory)
        #expect(ClientPaths.root(home: home, support: support, environment: [:], desktop: false).path ==
                "/var/mobile/Library/Application Support/Polymux")
    }

    @Test func networkDevicesDecode() throws {
        let payload = Data(#"{"server":{"name":"Home","build":"1","platform":"darwin","role":"server"},"devices":[{"id":"d1","name":"Local Mac app","agent":"local","capabilities":[],"revoked":false}]}"#.utf8)
        let info = try JSONDecoder().decode(NetworkInfo.self, from: payload)
        #expect(info.devices.count == 1)
        #expect(info.devices[0].id == "d1")
        #expect(info.devices[0].name == "Local Mac app")
    }
}
