import XCTest

#if os(macOS)
final class ApprovalTests: FixtureTestCase {
    func testFailedAnswerKeepsOptions() {
        launch(["--verify-approval"])
        let option = app.buttons["A. Plan my day"]
        XCTAssertTrue(option.waitForExistence(timeout: 10))
        option.click()
        app.buttons["Fail answer"].click()
        XCTAssertTrue(app.staticTexts["Couldn’t record that choice."].waitForExistence(timeout: 10))
        XCTAssertTrue(option.exists, "options stay available to retry")
    }
}
#endif
