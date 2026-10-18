import XCTest

#if os(macOS)
final class SettingsTests: FixtureTestCase {
    private func openSettings(_ network: String? = nil) {
        launch(["--verify-fixture", "--verify-settings", "-serverAddress", "http://127.0.0.1:1"]
            + (network.map { ["--verify-network-\($0)"] } ?? []))
        XCTAssertTrue(row("Chief").waitForExistence(timeout: 30))
        app.buttons["Settings"].firstMatch.click()
        XCTAssertTrue(app.staticTexts["Devices"].waitForExistence(timeout: 20))
    }

    func testDevicesLoaded() {
        openSettings()
        XCTAssertTrue(app.staticTexts["Home server"].waitForExistence(timeout: 10))
    }

    func testDevicesFailed() {
        openSettings("fail")
        XCTAssertTrue(app.descendants(matching: .any)["settings.network-error"].waitForExistence(timeout: 10))
    }

    func testDevicesSlow() {
        openSettings("slow")
        XCTAssertTrue(app.descendants(matching: .any)["settings.network-loading"].waitForExistence(timeout: 10))
    }

    func testPairingLink() {
        openSettings()
        app.buttons["Add device"].click()
        XCTAssertTrue(app.buttons["Copy link"].waitForExistence(timeout: 10), "invite loads")
        app.textFields["settings.link"].click()
        app.typeText("not-a-pairing-link\n")
        XCTAssertTrue(app.staticTexts["Paste a valid Polymux pairing link."].waitForExistence(timeout: 5))
    }
}
#endif
