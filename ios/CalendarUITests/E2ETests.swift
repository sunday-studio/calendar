import XCTest

/// End to end against the REAL hub and the fake Google (scripts/ios-e2e.sh, `make ios-e2e-real`).
/// Skipped unless the script passes E2E_HUB / E2E_TOKEN / E2E_FAKE (TEST_RUNNER_ prefix on the host side).
/// One sequential flow: each step builds on the state the previous one left at the hub.
@MainActor
final class E2ETests: XCTestCase {
    private var hub = "", token = "", fake = "", shotsDir = "", ctlDir = ""
    private var shotIndex = 0
    private var app: XCUIApplication!

    override func setUpWithError() throws {
        continueAfterFailure = false
        let env = ProcessInfo.processInfo.environment
        hub = env["E2E_HUB"] ?? ""; token = env["E2E_TOKEN"] ?? ""; fake = env["E2E_FAKE"] ?? ""; shotsDir = env["E2E_SHOTS"] ?? ""; ctlDir = env["E2E_CTL"] ?? ""
        try XCTSkipIf(hub.isEmpty || token.isEmpty || fake.isEmpty, "needs the real hub: run make ios-e2e-real")
    }

    override func tearDown() async throws {
        if let run = testRun, run.failureCount > 0, app != nil { shot("FAILED") }
    }

    // MARK: flow

    func testFullFlow() throws {
        step("pair through the UI") { pair() }
        step("accounts list shows both seeded accounts") { accounts() }
        step("home shows seeded events and the next-event block") { home() }
        step("month: pick a day, then Day / 3 days / Week") { month() }
        step("event details: Meet, repeating with exception, read-only") { details() }
        step("create an event through the editor") { create() }
        step("edit it after it changed elsewhere (conflict)") { conflict() }
        step("RSVP to the pending invitation") { rsvp() }
        step("delete with Undo, then for real") { delete() }
        step("hide a calendar in Settings") { hideCalendar() }
        step("quick add") { quickAdd() }
        step("search finds it") { search() }
        step("pull to refresh shows an outside change") { pullToRefresh() }
        step("Google refuses a write, then recovers") { googleRefuses() }
        step("hub unreachable: offline banner, edits off, recovers") { hubOffline() }
    }

    // MARK: steps

    private func pair() {
        // The system offers to save the token as a password: decline.
        addUIInterruptionMonitor(withDescription: "save password") { alert in
            for label in ["Not Now", "Never for This Website"] where alert.buttons[label].exists { alert.buttons[label].tap(); return true }
            return false
        }
        app = XCUIApplication()
        app.launchArguments = ["-e2eReset"]
        app.launch()
        tap(app.buttons["welcome.connect"], "welcome")
        let host = app.textFields["pair.host"]
        XCTAssertTrue(host.waitForExistence(timeout: 10), "pair form")
        typeInto(host, hub)
        let tok = app.secureTextFields["pair.token"]
        typeInto(tok, token)
        shot("pair-form")
        tap(app.buttons["pair.submit"], "submit")
        XCTAssertTrue(app.buttons["paired.add"].waitForExistence(timeout: 30), "paired screen\n\(app.debugDescription)")
        dismissSavePassword()
        shot("paired")
        tap(app.buttons["Later"], "later")
    }

    private func openHomeMenu(_ item: String) {
        let today = app.buttons["home.today"]
        XCTAssertTrue(today.waitForExistence(timeout: 20), "home did not appear\n\(app.debugDescription)")
        today.press(forDuration: 1.0)
        tap(app.buttons[item], "menu row \(item)")
    }

    private func accounts() {
        openHomeMenu("Accounts")
        let a = app.staticTexts["a@example.com"], b = app.staticTexts["b@example.com"]
        XCTAssertTrue(a.waitForExistence(timeout: 20), "account a\n\(app.debugDescription)")
        XCTAssertTrue(b.exists, "account b")
        shot("accounts")
        back()
    }

    private func home() {
        // The next-event block is the hero while something is on or coming today, else the "Done for today" card (it names the next one).
        let hero = app.buttons["home.hero"], done = app.descendants(matching: .any)["home.done"]
        let end = Date().addingTimeInterval(30)
        while Date() < end && !hero.exists && !done.exists { RunLoop.current.run(until: Date().addingTimeInterval(0.5)) }
        if hero.exists {
            XCTAssertTrue(hero.label.hasPrefix("Up next") || hero.label.hasPrefix("Now"), hero.label)   // "Now" while an event is on
        } else {
            XCTAssertTrue(done.exists, "next-event block\n\(app.debugDescription)")
            XCTAssertTrue(app.staticTexts.matching(NSPredicate(format: "label BEGINSWITH %@", "Next:")).firstMatch.exists, "done card names the next event")
        }
        for t in ["Design review", "Planning", "Quarterly sync", "Standup (moved)", "Team offsite"] { XCTAssertTrue(waitRow(t, 20), "missing \(t)") }
        goToToday()
        shot("home")
    }

    private func month() {
        tap(app.buttons["home.month"], "month toggle")
        // Pick tomorrow (or yesterday on the last day of the month): the grid labels are "Tuesday Oct 13, has events".
        let cal = Calendar.current
        var day = cal.date(byAdding: .day, value: 1, to: Date())!
        if cal.component(.month, from: day) != cal.component(.month, from: Date()) { day = cal.date(byAdding: .day, value: -1, to: Date())! }
        let f = DateFormatter(); f.locale = Locale(identifier: "en_US_POSIX"); f.dateFormat = "EEEE MMM d"
        let cell = app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", f.string(from: day))).firstMatch
        XCTAssertTrue(cell.waitForExistence(timeout: 10), "month cell \(f.string(from: day))\n\(app.debugDescription)")
        shot("month")
        cell.tap()
        tap(app.buttons["home.month"], "month toggle (close)")
        for (mode, layout) in [("Day", "day"), ("3 days", "three")] {
            tap(app.buttons["home.viewSelect"], "view select")
            tap(app.buttons[mode], mode)
            XCTAssertTrue(app.descendants(matching: .any)["board.\(layout)"].waitForExistence(timeout: 10), "board.\(layout)\n\(app.debugDescription)")
            shot("view-\(layout)")
        }
        tap(app.buttons["home.viewSelect"], "view select")
        tap(app.buttons["Agenda"], "Agenda")
        goToToday()
    }

    private func closeDetail() {
        back()
        XCTAssertTrue(app.staticTexts["event.title"].waitForNonExistence(timeout: 10), "detail did not close")
    }

    private func openDetail(_ title: String) {
        let t = app.staticTexts["event.title"]
        // A row tap can be lost while the list re-renders after a sync: retry a couple of times.
        for attempt in 1...3 {
            tap(row(title), title)
            if t.waitForExistence(timeout: attempt == 3 ? 10 : 3) { break }
        }
        XCTAssertTrue(t.waitForExistence(timeout: 10), "detail of \(title)\n\(app.debugDescription)")
        XCTAssertEqual(t.label, title)
    }

    private func details() {
        openDetail("Design review")
        XCTAssertTrue(app.buttons["Join"].exists, "Meet Join button")
        XCTAssertTrue(app.staticTexts["Google Meet"].exists)
        shot("detail-meet")
        closeDetail()

        openDetail("Standup (moved)")
        XCTAssertTrue(app.staticTexts["Daily"].exists && app.staticTexts["Series"].exists, "repeats info")
        shot("detail-repeating-exception")
        tap(app.buttons["More"], "More")
        XCTAssertTrue(app.buttons["Edit"].waitForExistence(timeout: 5), "Edit offered on an owned calendar")
        shot("detail-repeating-more")
        dismissSheet()
        closeDetail()

        openDetail("Team retro")
        shot("detail-readonly")
        XCTAssertTrue(app.staticTexts["Read only"].exists, "read-only tag")
        XCTAssertFalse(app.buttons["More"].exists, "a read-only event offers no actions")
        closeDetail()
    }

    /// Closes a CalSheet by tapping its scrim (it is hidden from accessibility, so by position).
    private func dismissSheet() {
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.12)).tap()
        XCTAssertTrue(app.buttons["Duplicate"].waitForNonExistence(timeout: 5), "sheet did not close")
    }

    private let quickTitle = "Lunch with Sam"
    private var created = "E2E created"

    private func create() {
        tap(app.buttons["fab"], "new event")
        tap(app.buttons["quickadd.form"], "full form")
        let title = app.textFields["editor.title"]
        XCTAssertTrue(title.waitForExistence(timeout: 10), "editor\n\(app.debugDescription)")
        typeInto(title, created)
        shot("editor-filled")
        let save = app.buttons["editor.save"]
        XCTAssertTrue(save.isEnabled)
        save.tap()
        XCTAssertTrue(title.waitForNonExistence(timeout: 20), "editor did not close\n\(app.debugDescription)")
        XCTAssertTrue(eventually(15) { !self.fakeEvents(self.created).isEmpty }, "event reached fake Google")
        let ok = waitRow(created, 20)
        XCTAssertTrue(ok, "new event on Home")
        shot("created-on-home")
    }

    private func conflict() {
        openDetail(created)
        tap(app.buttons["More"], "More")
        tap(app.buttons["Edit"], "Edit")
        let title = app.textFields["editor.title"]
        XCTAssertTrue(title.waitForExistence(timeout: 10), "editor")
        // Someone else edits it at Google first (the hub has not synced it yet, so the write hits a 409).
        guard let ev = fakeEvents(created).first else { return XCTFail("event missing at fake Google") }
        fakeAdmin("etag-bump", ["account": ev.account, "cal": ev.cal, "id": ev.id, "summary": "Changed elsewhere"])
        title.tap(); title.typeText(" mine")
        app.buttons["editor.save"].tap()
        XCTAssertTrue(app.staticTexts["This event changed while you were editing."].waitForExistence(timeout: 20), "conflict state\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts["Changed elsewhere"].exists, "their version is shown")
        shot("conflict")
        tap(app.buttons["Use latest and re-apply my changes"], "re-apply")
        XCTAssertTrue(eventually(20) { !self.fakeEvents("Changed elsewhere mine").isEmpty || !self.fakeEvents("E2E created mine").isEmpty }, "re-applied edit reached fake Google")
        XCTAssertTrue(app.staticTexts["E2E created mine"].waitForExistence(timeout: 10), "detail shows the re-applied title")
        shot("conflict-resolved")
        created = "E2E created mine"
        closeDetail()
    }

    private func rsvp() {
        openDetail("Quarterly sync")
        XCTAssertTrue(app.staticTexts["GOING?"].exists, "reply prompt")
        shot("invitation")
        func selfStatus() -> String? {
            let attendees = fakeEvents("Quarterly sync").first?.raw["attendees"] as? [[String: Any]]
            return attendees?.first { $0["self"] as? Bool == true }?["responseStatus"] as? String
        }
        XCTAssertEqual(selfStatus(), "needsAction")
        tap(app.buttons["Yes"], "Yes")
        XCTAssertTrue(eventually(20) { selfStatus() == "accepted" }, "fake Google has the reply (status \(selfStatus() ?? "nil"))\n\(app.debugDescription)")
        XCTAssertTrue(app.staticTexts["GOING?"].waitForNonExistence(timeout: 10), "reply prompt goes away once answered")
        shot("invitation-accepted")
        closeDetail()
        XCTAssertTrue(app.buttons["home.invitations"].waitForNonExistence(timeout: 10) || !app.staticTexts["1 invitation needs a reply."].exists, "invitation banner")
        shot("home-after-rsvp")
    }

    private func deleteCreated() {
        openDetail(created)
        tap(app.buttons["More"], "More")
        tap(app.buttons["Delete"], "Delete")
        shot("delete-sheet")
        tap(app.buttons["Delete event"], "Delete event")
        XCTAssertTrue(app.staticTexts["\(created) deleted"].waitForExistence(timeout: 20), "deleted toast\n\(app.debugDescription)")
        XCTAssertTrue(eventually(20) { self.fakeEvents(self.created).isEmpty }, "event is gone at fake Google")
    }

    private func delete() {
        deleteCreated()
        shot("deleted-toast")
        tap(app.buttons["Undo"], "Undo")
        XCTAssertTrue(eventually(20) { !self.fakeEvents(self.created).isEmpty }, "Undo put the event back at fake Google")
        XCTAssertTrue(waitRow(created, 20), "event is back on Home\n\(app.debugDescription)")
        shot("undo-restored")
        // For real: delete again and let the Undo window pass.
        deleteCreated()
        XCTAssertTrue(app.staticTexts["\(created) deleted"].waitForNonExistence(timeout: 15), "toast times out")
        XCTAssertTrue(row(created).waitForNonExistence(timeout: 20), "event left Home")
        XCTAssertTrue(self.fakeEvents(created).isEmpty, "still gone at fake Google")
        shot("deleted-for-real")
    }

    private func hideCalendar() {
        func visible(_ name: String) -> Bool? {
            let (_, d) = httpSync("GET", "\(hub)/v1/calendars", bearer: true)
            let items = (try? JSONSerialization.jsonObject(with: d) as? [String: Any])?["items"] as? [[String: Any]]
            return items?.first { $0["name"] as? String == name }?["visible"] as? Bool
        }
        XCTAssertTrue(waitRow("Team retro", 10), "Team retro is on Home before hiding")
        XCTAssertEqual(visible("Team"), true)
        openHomeMenu("Settings")
        tap(app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Calendars")).firstMatch, "Calendars")
        let toggle = app.buttons["calendar.visible.Team"]
        tap(toggle, "Team switch")
        XCTAssertEqual(toggle.value as? String, "Off")
        shot("settings-calendars-hidden")
        XCTAssertTrue(eventually(15) { visible("Team") == false }, "the hub has Team hidden")
        back()   // Calendars -> Settings
        back()   // Settings -> Home
        XCTAssertTrue(app.buttons["home.today"].waitForExistence(timeout: 10))
        XCTAssertTrue(row("Team retro").waitForNonExistence(timeout: 20), "Team's events left Home\n\(app.debugDescription)")
        XCTAssertTrue(row("Design review").exists, "other calendars stay")
        shot("home-calendar-hidden")
        // Put it back so later steps see everything.
        openHomeMenu("Settings")
        tap(app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", "Calendars")).firstMatch, "Calendars")
        tap(app.buttons["calendar.visible.Team"], "Team switch")
        XCTAssertTrue(eventually(15) { visible("Team") == true })
        back(); back()
        XCTAssertTrue(waitRow("Team retro", 20), "Team's events return")
    }

    private func quickAdd() {
        tap(app.buttons["fab"], "new event")
        let field = app.textFields["quickadd.field"]
        XCTAssertTrue(field.waitForExistence(timeout: 10), "quick add field\n\(app.debugDescription)")
        typeInto(field, "lunch with Sam friday 1pm")
        XCTAssertTrue(app.buttons["quickadd.preview"].waitForExistence(timeout: 10))
        shot("quickadd-typed")
        app.buttons["quickadd.preview"].tap()
        // The hub asks which Friday (it will not guess): keep "This Friday" and carry on.
        if app.staticTexts["WHICH FRIDAY?"].waitForExistence(timeout: 10) || app.buttons["Continue"].waitForExistence(timeout: 2) {
            shot("quickadd-question")
            tap(app.buttons["Continue"], "Continue")
        }
        let create = app.buttons["quickadd.create"]
        XCTAssertTrue(create.waitForExistence(timeout: 20), "preview with a Create button\n\(app.debugDescription)")
        shot("quickadd-preview")
        create.tap()
        XCTAssertTrue(create.waitForNonExistence(timeout: 20), "quick add closed")
        XCTAssertTrue(eventually(20) { !self.fakeEvents(self.quickTitle).isEmpty }, "quick-added event at fake Google")
        shot("quickadd-created")
    }

    private func search() {
        tap(app.buttons["home.search"], "search")
        let field = app.textFields["search.field"]
        XCTAssertTrue(field.waitForExistence(timeout: 10), "search field\n\(app.debugDescription)")
        typeInto(field, "Sam")
        let hit = app.buttons.matching(NSPredicate(format: "label CONTAINS %@", quickTitle)).firstMatch
        XCTAssertTrue(hit.waitForExistence(timeout: 20), "search finds the quick-added event\n\(app.debugDescription)")
        shot("search")
        tap(app.buttons["search.close"], "close search")
        XCTAssertTrue(app.buttons["home.today"].waitForExistence(timeout: 10))
    }

    private func pullToRefresh() {
        let title = "Outside change"
        let day = Calendar.current.date(byAdding: .day, value: 1, to: Date())!
        let iso = ISO8601DateFormatter()
        let start = Calendar.current.date(bySettingHour: 16, minute: 0, second: 0, of: day)!
        let end = start.addingTimeInterval(1800)
        let ev: [String: Any] = ["summary": title,
                                 "start": ["dateTime": iso.string(from: start), "timeZone": "Europe/London"],
                                 "end": ["dateTime": iso.string(from: end), "timeZone": "Europe/London"]]
        fakeAdmin("seed", ["account": "a@example.com", "cal": "a@example.com", "id": "e2e-outside", "event": ev])
        // Scroll to the top, then pull down on the list.
        let top = app.buttons["home.hero"].exists ? app.buttons["home.hero"] : app.buttons["home.today"]
        let from = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.3))
        from.press(forDuration: 0.1, thenDragTo: app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.8)))
        _ = top
        XCTAssertTrue(waitRow(title, 30), "the outside change shows after refresh\n\(app.debugDescription)")
        shot("pull-to-refresh")
    }

    /// Google refuses the next write: the editor keeps the form, says nothing was saved, and Try again works once Google is back.
    private func googleRefuses() {
        tap(app.buttons["fab"], "new event")
        tap(app.buttons["quickadd.form"], "full form")
        let title = app.textFields["editor.title"]
        XCTAssertTrue(title.waitForExistence(timeout: 10), "editor")
        typeInto(title, "Refused once")
        fakeAdmin("fail", ["method": "POST", "path": "/events", "code": 403, "n": 1])
        app.buttons["editor.save"].tap()
        let failed = app.staticTexts.matching(NSPredicate(format: "label CONTAINS %@", "Nothing was saved")).firstMatch
        XCTAssertTrue(failed.waitForExistence(timeout: 30), "failure is shown\n\(app.debugDescription)")
        XCTAssertEqual(title.value as? String, "Refused once", "the form keeps what was typed")
        XCTAssertTrue(fakeEvents("Refused once").isEmpty, "nothing half-saved at Google")
        shot("google-refused")
        tap(app.buttons["editor.retry"], "Try again")
        XCTAssertTrue(title.waitForNonExistence(timeout: 30), "editor closes after the retry")
        XCTAssertTrue(eventually(20) { !self.fakeEvents("Refused once").isEmpty }, "retry reached Google")
        shot("google-recovered")
    }

    /// The hub goes away (the script stops it on request): saved data stays, a banner says so, edits are off; it recovers when the hub is back.
    private func hubOffline() {
        ctl("hub-down")
        let banner = app.descendants(matching: .any)["home.offline"]
        // The app notices on its next refresh: bring it back from the background, which refreshes.
        for _ in 0..<4 where !banner.exists { resumeApp(); _ = banner.waitForExistence(timeout: 10) }
        XCTAssertTrue(banner.exists, "offline banner\n\(app.debugDescription)")
        XCTAssertTrue(app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "reach your server")).firstMatch.exists, "banner says the server can't be reached\n\(app.debugDescription)")
        XCTAssertTrue(app.buttons["home.today"].exists)
        shot("hub-down")
        tap(app.buttons["fab"], "new event while offline")
        XCTAssertTrue(app.descendants(matching: .any).matching(NSPredicate(format: "label CONTAINS %@", "edit while offline")).firstMatch.waitForExistence(timeout: 5), "edits are disabled offline\n\(app.debugDescription)")
        XCTAssertFalse(app.textFields["quickadd.field"].exists, "no editor opens offline")
        shot("hub-down-edits-off")
        ctl("hub-up")
        for _ in 0..<4 where banner.exists { resumeApp(); _ = banner.waitForNonExistence(timeout: 10) }
        XCTAssertTrue(banner.waitForNonExistence(timeout: 30), "banner clears once the hub is back\n\(app.debugDescription)")
        shot("hub-recovered")
        tap(app.buttons["fab"], "new event again")
        XCTAssertTrue(app.textFields["quickadd.field"].waitForExistence(timeout: 10), "edits work again")
        tap(app.buttons["Close"], "close")
    }

    /// Home, then back to the app: the app refreshes when it becomes active.
    private func resumeApp() {
        XCUIDevice.shared.press(.home)
        app.activate()
        _ = app.buttons["home.today"].waitForExistence(timeout: 10)
    }

    /// Asks scripts/ios-e2e.sh to stop/start the hub (`hub-down` / `hub-up`) and waits for its acknowledgement file.
    private func ctl(_ cmd: String) {
        guard !ctlDir.isEmpty else { return XCTFail("E2E_CTL not set") }
        let ack = URL(fileURLWithPath: ctlDir).appendingPathComponent(cmd + ".done")
        try? FileManager.default.removeItem(at: ack)
        FileManager.default.createFile(atPath: URL(fileURLWithPath: ctlDir).appendingPathComponent(cmd).path, contents: nil)
        XCTAssertTrue(eventually(60) { FileManager.default.fileExists(atPath: ack.path) }, "script did not acknowledge \(cmd)")
    }

    /// Waits for a list row. The agenda is lazy, so rows outside the window are not in the tree: scroll to find them.
    private func waitRow(_ title: String, _ timeout: TimeInterval) -> Bool {
        let end = Date().addingTimeInterval(timeout)
        if row(title).waitForExistence(timeout: 2) { return true }
        if app.buttons["home.today"].exists { goToToday() }   // back to the top of the agenda
        if row(title).waitForExistence(timeout: 3) { return true }
        // Drag less than a screen at a time so no row is skipped.
        func drag(up: Bool) {
            let a = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: up ? 0.75 : 0.35))
            let b = app.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: up ? 0.35 : 0.75))
            a.press(forDuration: 0.05, thenDragTo: b, withVelocity: .slow, thenHoldForDuration: 0.2)
        }
        while Date() < end {
            for _ in 0..<10 { if row(title).exists { return true }; drag(up: true) }
            for _ in 0..<10 { if row(title).exists { return true }; drag(up: false) }
        }
        return row(title).exists
    }

    private func row(_ title: String) -> XCUIElement {
        app.buttons.matching(NSPredicate(format: "label BEGINSWITH %@", title + ",")).firstMatch
    }

    private func back() {
        let b = app.buttons["Back"].firstMatch
        tap(b, "back")
    }

    // MARK: helpers

    struct FakeEvent { let account: String; let cal: String; let id: String; let summary: String; let raw: [String: Any] }

    private func httpSync(_ method: String, _ url: String, json: [String: Any]? = nil, bearer: Bool = false) -> (Int, Data) {
        var req = URLRequest(url: URL(string: url)!)
        req.httpMethod = method
        if let json { req.httpBody = try? JSONSerialization.data(withJSONObject: json); req.setValue("application/json", forHTTPHeaderField: "Content-Type") }
        if bearer { req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization") }
        req.timeoutInterval = 10
        let sem = DispatchSemaphore(value: 0)
        nonisolated(unsafe) var out: (Int, Data) = (0, Data())
        URLSession.shared.dataTask(with: req) { d, r, _ in out = ((r as? HTTPURLResponse)?.statusCode ?? 0, d ?? Data()); sem.signal() }.resume()
        sem.wait()
        return out
    }

    private func fakeAdmin(_ name: String, _ body: [String: Any]) { _ = httpSync("POST", "\(fake)/_admin/\(name)", json: body) }

    /// Events (by summary) that fake Google holds, across both accounts. State is {...: {calendars: {calId: [event]}}}.
    private func fakeEvents(_ summary: String) -> [FakeEvent] {
        var found: [FakeEvent] = []
        for account in ["a@example.com", "b@example.com"] {
            let (_, data) = httpSync("GET", "\(fake)/_admin/state?account=\(account)")
            guard let root = try? JSONSerialization.jsonObject(with: data) else { continue }
            func walk(_ n: Any) {
                if let d = n as? [String: Any] {
                    if let cals = d["calendars"] as? [String: Any] {
                        for (cal, evs) in cals { for e in (evs as? [[String: Any]]) ?? [] where e["summary"] as? String == summary && e["status"] as? String != "cancelled" {
                            found.append(FakeEvent(account: account, cal: cal, id: e["id"] as? String ?? "", summary: summary, raw: e))
                        } }
                    }
                    d.values.forEach(walk)
                } else if let a = n as? [Any] { a.forEach(walk) }
            }
            walk(root)
        }
        return found
    }

    /// Polls `cond` (explicit deadline, no fixed sleeps).
    private func eventually(_ seconds: TimeInterval, _ cond: () -> Bool) -> Bool {
        let end = Date().addingTimeInterval(seconds)
        while Date() < end { if cond() { return true }; RunLoop.current.run(until: Date().addingTimeInterval(0.25)) }
        return cond()
    }

    /// The "Save Password?" sheet is a remote view the accessibility tree cannot reach: tap its "Not Now" by position.
    private func dismissSavePassword() {
        sleep(2)   // the sheet animates in after the form goes away
        app.coordinate(withNormalizedOffset: CGVector(dx: 0.316, dy: 0.626)).tap()
    }

    /// E2E_STEPS="offline,search" runs only the steps whose name contains one of the words (pairing always runs): a quick loop for one step.
    private func step(_ name: String, _ body: () throws -> Void) rethrows {
        let only = (ProcessInfo.processInfo.environment["E2E_STEPS"] ?? "").split(separator: ",").map(String.init)
        if !only.isEmpty, !name.hasPrefix("pair"), !only.contains(where: { name.contains($0) }) { return }
        print("E2E step: \(name)")
        try body()
    }

    /// The date button opens the Calendar menu; its first row jumps to today.
    private func goToToday() {
        tap(app.buttons["home.today"], "date button")
        tap(app.buttons["Go to today"], "Go to today")
    }

    private func tap(_ el: XCUIElement, _ what: String, timeout: TimeInterval = 15) {
        XCTAssertTrue(el.waitForExistence(timeout: timeout), "missing: \(what)\n\(app.debugDescription)")
        // Scroll it into view (long agendas), then wait for it to settle; fall back to a tap on its centre.
        var swipes = 0
        while !el.isHittable && swipes < 8, let list = app.scrollViews.allElementsBoundByIndex.last, el.frame.minY > app.frame.height * 0.8 {
            list.swipeUp(velocity: .slow); swipes += 1
        }
        let settled = NSPredicate(format: "isHittable == true")
        let hit = XCTWaiter().wait(for: [XCTNSPredicateExpectation(predicate: settled, object: el)], timeout: 10) == .completed
        if hit { el.tap() } else { el.coordinate(withNormalizedOffset: CGVector(dx: 0.5, dy: 0.5)).tap() }
    }

    private func typeInto(_ el: XCUIElement, _ text: String) {
        el.tap()
        if app.buttons["Continue"].waitForExistence(timeout: 1) { app.buttons["Continue"].tap() }   // keyboard intro
        el.typeText(text)
    }

    private func shot(_ name: String) {
        shotIndex += 1
        let s = app.screenshot()
        let att = XCTAttachment(screenshot: s); att.name = name; att.lifetime = .keepAlways; add(att)
        guard !shotsDir.isEmpty else { return }
        let url = URL(fileURLWithPath: shotsDir).appendingPathComponent(String(format: "%02d-%@.png", shotIndex, name))
        try? s.pngRepresentation.write(to: url)
    }
}
