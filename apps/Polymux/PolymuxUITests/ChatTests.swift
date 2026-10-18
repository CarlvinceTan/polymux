import XCTest

#if os(macOS)
final class ChatTests: FixtureTestCase {
    func testOpenChat() {
        launch(["--verify-fixture", "--verify-tools"])
        XCTAssertTrue(row("Chief").waitForExistence(timeout: 30))
        row("Chief").click()
        XCTAssertTrue(app.staticTexts["Second reply from the same turn."].waitForExistence(timeout: 10),
                      "opening a chat shows its messages")
    }
}
#endif
