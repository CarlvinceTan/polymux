import XCTest

#if os(macOS)
/// Launches the app against the DEBUG fixture server with an isolated data directory.
class FixtureTestCase: XCTestCase {
    private let data = FileManager.default.temporaryDirectory.appending(path: "polymux-ui-\(UUID())")
    private(set) var app = XCUIApplication()

    override func setUp() {
        super.setUp()
        continueAfterFailure = false
    }

    override func tearDown() {
        app.terminate()
        try? FileManager.default.removeItem(at: data)
        super.tearDown()
    }

    func launch(_ flags: [String]) {
        app.launchEnvironment["POLYMUX_DATA_DIR"] = data.path
        app.launchArguments = flags + ["-AppleInterfaceStyle", "Light"]
        app.launch()
        app.activate()
    }

    func row(_ name: String) -> XCUIElement {
        app.descendants(matching: .any).matching(NSPredicate(format: "label BEGINSWITH %@", "\(name), ")).firstMatch
    }

}
#endif
