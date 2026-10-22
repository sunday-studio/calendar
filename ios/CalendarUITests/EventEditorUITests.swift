import XCTest

/// Real typing flow for create + save, with the editor's real view model and a scripted writer.
final class EventEditorUITests: XCTestCase {
    override func setUp() {
        continueAfterFailure = false
        UserDefaults.standard.removeObject(forKey: "editor.draft")
    }

    private func launch(fail: Bool) -> XCUIApplication {
        let app = XCUIApplication()
        app.launchArguments = ["-uiEditor"] + (fail ? ["-uiEditorFail"] : [])
        app.launch()
        return app
    }

    private func typeTitle(_ app: XCUIApplication, _ text: String) {
        let field = app.textFields["editor.title"]
        XCTAssertTrue(field.waitForExistence(timeout: 20), "editor did not open")
        field.tap()
        if app.buttons["Continue"].waitForExistence(timeout: 2) { app.buttons["Continue"].tap() }   // keyboard intro
        field.typeText(text)
    }

    func testTypeTitleAndSaveClosesWithTheEvent() {
        let app = launch(fail: false)
        let save = app.buttons["editor.save"]
        XCTAssertTrue(save.waitForExistence(timeout: 20))
        XCTAssertFalse(save.isEnabled, "Save must be disabled until there is a title")
        typeTitle(app, "Lunch with Ana")
        XCTAssertTrue(save.isEnabled)
        save.tap()
        XCTAssertTrue(app.staticTexts["Saved: Lunch with Ana"].waitForExistence(timeout: 10))
    }

    func testProviderRefusalKeepsTheFormAndOffersDraft() {
        let app = launch(fail: true)
        typeTitle(app, "Dentist")
        app.buttons["editor.save"].tap()
        XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "Nothing was saved")).firstMatch.waitForExistence(timeout: 10))
        XCTAssertTrue(app.buttons["editor.retry"].exists)
        XCTAssertEqual(app.textFields["editor.title"].value as? String, "Dentist", "the form must keep the typed title")
        app.buttons["Keep as draft"].tap()
        XCTAssertTrue(app.staticTexts["Draft kept"].waitForExistence(timeout: 10))
    }
}
