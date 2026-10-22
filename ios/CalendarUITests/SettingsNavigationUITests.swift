import XCTest

/// Regression: Settings rows pushed onto the Home stack (typed `[HomeRoute]` path) must navigate.
/// Value-based NavigationLinks silently did nothing there.
final class SettingsNavigationUITests: XCTestCase {
    override func setUp() { continueAfterFailure = false }

    private func row(_ app: XCUIApplication, _ name: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", name)).firstMatch
    }

    func testEverySettingsRowNavigatesAndComesBack() {
        let app = XCUIApplication()
        app.launchArguments = ["-uiSettings"]
        app.launch()
        XCTAssertTrue(row(app, "Calendars").waitForExistence(timeout: 20), "Settings home did not appear")

        for name in ["Calendars", "Default calendar", "Time zones", "Working hours", "Week starts", "Alerts", "Appearance", "Server and sync"] {
            let r = row(app, name)
            XCTAssertTrue(r.waitForExistence(timeout: 5), "\(name) row missing")
            r.tap()
            if name == "Alerts" {
                // Its subscreen has a row that also starts with "Alerts" (permission off), so look for its own heading.
                XCTAssertTrue(app.staticTexts["Before an event"].waitForExistence(timeout: 5), "tapping \(name) did not navigate")
            } else {
                // The home list leaves the hierarchy when a subscreen is pushed.
                XCTAssertTrue(r.waitForNonExistence(timeout: 5), "tapping \(name) did not navigate")
            }
            let back = app.buttons["Back"].firstMatch
            XCTAssertTrue(back.waitForExistence(timeout: 10), "\(name) has no Back button")
            back.tap()
            XCTAssertTrue(row(app, name).waitForExistence(timeout: 10), "did not return from \(name)")
        }
    }
}
