import XCTest

/// Walks the app against a throwaway hub with the rich demo data and saves one PNG per screen and appearance.
/// Driven by scripts/screenshots.sh; skipped unless it passes SHOTS_HUB / SHOTS_TOKEN / SHOTS_DIR
/// (TEST_RUNNER_ prefix on the host side). It writes nothing to the hub: every sheet is cancelled, nothing is saved.
///
/// One run captures one appearance (SHOTS_MODE = light | dark; the script sets the simulator appearance with
/// `xcrun simctl ui` before each run). Names: `<screen>-light.png`, `<screen>-dark.png`.
/// A step that cannot find its element logs `SHOTS skip` and the tour carries on; the script lists the files it got.
@MainActor
final class ScreenshotTourUITests: XCTestCase {
    private var hub = "", token = "", dir = ""
    private var app: XCUIApplication!
    private var mode = "light"

    override func setUpWithError() throws {
        continueAfterFailure = true
        let env = ProcessInfo.processInfo.environment
        hub = env["SHOTS_HUB"] ?? ""; token = env["SHOTS_TOKEN"] ?? ""; dir = env["SHOTS_DIR"] ?? ""; mode = env["SHOTS_MODE"] == "dark" ? "dark" : "light"
        try XCTSkipIf(hub.isEmpty || token.isEmpty || dir.isEmpty, "needs the throwaway hub: run scripts/screenshots.sh")
        try? FileManager.default.createDirectory(atPath: dir, withIntermediateDirectories: true)
    }

    func testTour() {
        app = XCUIApplication()
        // The system offers to save the token as a password: decline.
        addUIInterruptionMonitor(withDescription: "save password") { alert in
            for label in ["Not Now", "Never for This Website"] where alert.buttons[label].exists { alert.buttons[label].tap(); return true }
            return false
        }
        app.launchArguments = ["-e2eReset"]
        app.launch()

        step("forget an earlier pairing") { forgetIfPaired() }
        step("welcome and pairing") { pairing() }
        step("home") { home() }
        step("month, day, 3 days") { views() }
        step("event detail with guests") { detail() }
        step("reply sheet") { reply() }
        step("move to calendar") { move() }
        step("search results") { search() }
        step("settings and its pages") { settings() }
        step("accounts") { accounts() }
        step("new event editor") { editor() }
        step("quick add preview") { quickAdd() }
    }

    // MARK: steps

    /// A pairing from an earlier run survives in the Keychain: forget it so the welcome screen can be captured.
    private func forgetIfPaired() {
        guard app.buttons["home.today"].waitForExistence(timeout: 6) else { return }
        guard tap(app.buttons["home.today"], "date button") else { return }
        let forget = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@ OR label BEGINSWITH %@", "Forget", "Log out"))
        guard tap(forget.firstMatch, "forget row") else { return }
        pause(1)
        _ = tap(forget.firstMatch, "forget confirm")
        pause(1.5)
    }

    private func pairing() {
        guard app.buttons["welcome.connect"].waitForExistence(timeout: 30) else { log("skip: no welcome screen"); if ProcessInfo.processInfo.environment["SHOTS_DEBUG"] != nil { log(String(app.debugDescription.prefix(3000))) }; return }
        pause(1)
        both("welcome")
        _ = tap(app.buttons["welcome.connect"], "connect")
        let host = app.textFields["pair.host"]
        guard host.waitForExistence(timeout: 10) else { return }
        typeInto(host, hub)
        typeInto(app.secureTextFields["pair.token"], token)
        hideKeyboard()
        pause(1)
        both("pair")
        _ = tap(app.buttons["pair.submit"], "submit")
        if app.buttons["paired.add"].waitForExistence(timeout: 40) {
            pause(2)   // the save-password sheet animates in after the form goes away
            app.coordinate(withNormalizedOffset: CGVector(dx: 0.316, dy: 0.626)).tap()
            pause(1)
            _ = tap(app.buttons["Later"], "later")
        }
    }

    private func home() {
        let hero = app.buttons["home.hero"]
        _ = hero.waitForExistence(timeout: 30)
        // let the first sync finish so no skeleton or "syncing" chip is on screen
        let syncing = app.descendants(matching: .any)["home.syncing"]
        _ = syncing.waitForNonExistence(timeout: 30)
        pause(5)
        both("home")
    }

    private func views() {
        guard tap(app.buttons["home.month"], "month toggle") else { return }
        pause(1)
        both("month")
        _ = tap(app.buttons["home.month"], "month toggle (close)")
        for (mode, layout, name) in [("Day", "day", "day"), ("3 days", "three", "three-days")] {
            guard tap(app.buttons["home.viewSelect"], "view select"), tap(app.buttons[mode], mode) else { continue }
            _ = app.descendants(matching: .any)["board.\(layout)"].waitForExistence(timeout: 10)
            pause(2)
            both(name)
        }
        if tap(app.buttons["home.viewSelect"], "view select") { _ = tap(app.buttons["Agenda"], "Agenda") }
        pause(1)
    }

    private func detail() {
        if openFromSearch("Design review") {
            pause(1.5)
            both("event-detail")
            closeSearchedDetail()
        }
        guard openFromSearch("Q4 kickoff") else { return }
        pause(1)
        // the people row opens the guest list
        if tap(app.descendants(matching: .any)["event.people"], "people row", wait: 5) {
            pause(1.5)
            both("event-guests")
            _ = tap(app.buttons["Back"].firstMatch, "back from guests")
            pause(0.6)
        }
        closeSearchedDetail()
    }

    private func reply() {
        guard openFromSearch("Quarterly sync") else { return }
        pause(1.5)
        let maybe = app.buttons["Maybe"]
        guard tap(maybe, "Maybe") else { closeSearchedDetail(); return }
        pause(1)
        both("reply-sheet")
        if !tap(app.buttons["Cancel"], "Cancel") { app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.1)).tap() }
        pause(0.5)
        closeSearchedDetail()
    }

    private func move() {
        guard openFromSearch("Whiteboard session") else { return }
        pause(1)
        guard tap(app.buttons["More"], "More") else { closeSearchedDetail(); return }
        guard tap(app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Move to another calendar")).firstMatch, "move row") else { closeSearchedDetail(); return }
        _ = app.descendants(matching: .any)["event.move"].waitForExistence(timeout: 10)
        pause(1.5)
        both("move-calendar")
        _ = tap(app.buttons["Close"], "close move")
        pause(0.8)
        closeSearchedDetail()
    }

    private func search() {
        guard tap(app.buttons["home.search"], "search") else { return }
        let field = app.textFields["search.field"]
        guard field.waitForExistence(timeout: 10) else { return }
        typeInto(field, "review")
        _ = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", "review")).firstMatch.waitForExistence(timeout: 20)
        hideKeyboard()
        pause(1.5)
        both("search")
        _ = tap(app.buttons["search.close"], "close search")
        pause(0.8)
    }

    private func editor() {
        guard tap(app.buttons["fab"], "new event") else { return }
        guard tap(app.buttons["quickadd.form"], "full form") else { return }
        let title = app.textFields["editor.title"]
        guard title.waitForExistence(timeout: 10) else { return }
        pause(1)
        hideKeyboard()
        pause(1)
        both("editor")
        _ = tap(app.buttons["Close"].exists ? app.buttons["Close"] : app.buttons["Cancel"], "close editor")
        pause(1)
        _ = tap(app.buttons["Discard"], "discard", wait: 2)
    }

    private func quickAdd() {
        guard tap(app.buttons["fab"], "new event") else { return }
        let field = app.textFields["quickadd.field"]
        guard field.waitForExistence(timeout: 10) else { return }
        typeInto(field, "lunch with Sam friday 1pm")
        guard app.buttons["quickadd.preview"].waitForExistence(timeout: 10) else { return }
        app.buttons["quickadd.preview"].tap()
        if app.staticTexts["WHICH FRIDAY?"].waitForExistence(timeout: 8) || app.buttons["Continue"].waitForExistence(timeout: 1) {
            _ = tap(app.buttons["Continue"], "Continue")
        }
        _ = app.buttons["quickadd.create"].waitForExistence(timeout: 20)
        hideKeyboard()
        pause(1.5)
        both("quick-add")
        _ = tap(app.buttons["Close"], "close quick add")
        pause(1)
    }

    private func settings() {
        guard openHomeMenu("Settings") else { return }
        pause(1)
        both("settings")
        for (prefix, name) in [("Calendars", "calendars"), ("Server and sync", "server-sync")] {
            let row = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", prefix)).firstMatch
            guard tap(row, prefix) else { continue }
            pause(2)
            both(name)
            _ = tap(app.buttons["Back"].firstMatch, "back")
            pause(0.8)
        }
        _ = tap(app.buttons["Back"].firstMatch, "back to Home")
        pause(0.8)
    }

    private func accounts() {
        guard openHomeMenu("Accounts") else { return }
        pause(2)
        both("accounts")
        _ = tap(app.buttons["Back"].firstMatch, "back")
    }

    // MARK: flows

    private func openFromSearch(_ query: String) -> Bool {
        guard tap(app.buttons["home.search"], "search") else { return false }
        let field = app.textFields["search.field"]
        guard field.waitForExistence(timeout: 10) else { return false }
        typeInto(field, query)
        let hit = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", query)).firstMatch
        guard hit.waitForExistence(timeout: 20) else { log("skip: no search hit for \(query)"); _ = tap(app.buttons["search.close"], "close search"); return false }
        hideKeyboard()
        guard tap(hit, "result \(query)") else { return false }
        let ok = app.staticTexts["event.title"].waitForExistence(timeout: 10)
        if !ok { log("skip: detail of \(query) did not open") }
        return ok
    }

    private func closeSearchedDetail() {
        _ = tap(app.buttons["Back"].firstMatch, "back from detail")
        pause(0.6)
        _ = tap(app.buttons["search.close"], "close search")
        _ = app.buttons["home.today"].waitForExistence(timeout: 10)
        pause(0.6)
    }

    private func openHomeMenu(_ item: String) -> Bool {
        let today = app.buttons["home.today"]
        guard today.waitForExistence(timeout: 20) else { return false }
        today.tap()
        pause(1.2)
        if ProcessInfo.processInfo.environment["SHOTS_DEBUG"] != nil { log("buttons: " + app.buttons.allElementsBoundByIndex.map { $0.label }.joined(separator: " | ")) }
        return tap(app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", item)).firstMatch, "menu row \(item)")
    }

    // MARK: capture

    private func both(_ name: String) { snap("\(name)-\(mode)") }

    private func snap(_ name: String) {
        let s = XCUIScreen.main.screenshot()
        let att = XCTAttachment(screenshot: s); att.name = name; att.lifetime = .deleteOnSuccess; add(att)
        let url = URL(fileURLWithPath: dir).appendingPathComponent(name + ".png")
        try? s.pngRepresentation.write(to: url)
        log("shot \(name)")
    }

    // MARK: helpers

    private func log(_ s: String) { print("SHOTS \(s)") }

    private func pause(_ seconds: TimeInterval) { RunLoop.current.run(until: Date().addingTimeInterval(seconds)) }

    private func step(_ name: String, _ body: () -> Void) {
        let only = (ProcessInfo.processInfo.environment["SHOTS_ONLY"] ?? "").split(separator: ",").map(String.init)
        if !only.isEmpty, !name.hasPrefix("forget"), !name.hasPrefix("welcome"), !name.hasPrefix("home"), !only.contains(where: { name.contains($0) }) { return }
        log("step: \(name)")
        body()
    }

    @discardableResult
    private func tap(_ el: XCUIElement, _ what: String, wait: TimeInterval = 15, hittable: Bool = true) -> Bool {
        guard el.waitForExistence(timeout: wait) else { log("skip: missing \(what)"); if ProcessInfo.processInfo.environment["SHOTS_DEBUG"] != nil { log(String(app.debugDescription.prefix(3000))) }; return false }
        var swipes = 0
        while hittable && !el.isHittable && swipes < 4 { pause(0.5); swipes += 1 }
        if el.isHittable { el.tap() } else { el.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap() }
        return true
    }

    private func typeInto(_ el: XCUIElement, _ text: String) {
        el.tap()
        if app.buttons["Continue"].waitForExistence(timeout: 1) { app.buttons["Continue"].tap() }   // keyboard intro
        el.typeText(text)
    }

    /// Dismisses the software keyboard through its own return/search/done key.
    private func hideKeyboard() {
        guard app.keyboards.count > 0 else { return }
        for k in ["Search", "search", "Done", "Return", "return"] where app.keyboards.buttons[k].exists { app.keyboards.buttons[k].tap(); break }
        pause(0.8)
    }
}
