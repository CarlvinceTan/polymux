import XCTest

#if os(macOS)
final class SidebarTests: FixtureTestCase {
    func testRowsLoad() {
        launch(["--verify-fixture"])
        XCTAssertTrue(row("Chief").waitForExistence(timeout: 30), "seeded agent rows load")
        for name in ["Aria", "Milo", "Nova"] {
            XCTAssertTrue(row(name).exists, "\(name) row")
        }
    }
}
#endif
