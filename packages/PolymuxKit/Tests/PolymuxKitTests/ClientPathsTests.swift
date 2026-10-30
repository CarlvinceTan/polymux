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
                "/Users/example/.polymux/server-c38ac81a739036b41a5657bf8cdad6ea/cache.sqlite")
    }

    @Test func collidingAddressesStayApart() throws {
        // These two collide under the old folder hash: 97×33+114 = 99×33+48.
        let root = FileManager.default.temporaryDirectory.appending(path: "paths-\(UUID())")
        defer { try? FileManager.default.removeItem(at: root) }
        #expect(ClientPaths.token("http://ar.test:7337", root: root) != ClientPaths.token("http://c0.test:7337", root: root))
    }

    @Test func legacyFolderMoves() throws {
        let root = FileManager.default.temporaryDirectory.appending(path: "paths-\(UUID())")
        defer { try? FileManager.default.removeItem(at: root) }
        let legacy = root.appending(path: "app-349688521712662452")
        try FileManager.default.createDirectory(at: legacy, withIntermediateDirectories: true)
        try Data("saved".utf8).write(to: legacy.appending(path: "token"))
        let token = ClientPaths.token("http://localhost:7337", root: root)
        #expect(try String(contentsOf: token, encoding: .utf8) == "saved")
        #expect(!FileManager.default.fileExists(atPath: legacy.path))
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
