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
            // The first tap can land while the list is still settling after launch or a pop; tap once more if nothing moved.
            XCTAssertTrue(r.waitForHittable(timeout: 10), "\(name) row never became tappable")
            r.tap()
            if name != "Alerts", !r.waitForNonExistence(timeout: 2) { r.tap() }
            if name == "Alerts" {
                // Its subscreen has a row that also starts with "Alerts" (permission off), so look for its own heading.
                XCTAssertTrue(app.staticTexts["Before an event"].waitForExistence(timeout: 5), "tapping \(name) did not navigate")
            } else {
                // The home list leaves the hierarchy when a subscreen is pushed.
                XCTAssertTrue(r.waitForNonExistence(timeout: 5), "tapping \(name) did not navigate")
            }
            // Buttons of screens lower in the stack can stay in the hierarchy; only the visible Back pops this screen.
            let deadline = Date().addingTimeInterval(10)
            var back: XCUIElement?
            while back == nil, Date() < deadline {
                back = app.buttons.matching(identifier: "Back").allElementsBoundByIndex.first(where: { $0.isHittable })
                if back == nil { Thread.sleep(forTimeInterval: 0.2) }
            }
            XCTAssertNotNil(back, "\(name) has no Back button")
            back?.tap()
            XCTAssertTrue(row(app, name).waitForHittable(timeout: 10), "did not return from \(name)")
        }
    }
}

private extension XCUIElement {
    func waitForHittable(timeout: TimeInterval) -> Bool {
        let p = NSPredicate(format: "exists == true AND hittable == true")
        return XCTWaiter().wait(for: [XCTNSPredicateExpectation(predicate: p, object: self)], timeout: timeout) == .completed
    }
}
