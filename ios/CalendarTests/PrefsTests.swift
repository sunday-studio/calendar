import CalendarCore
import Foundation
import SurfacesCore
import Testing
@testable import FeatureHome

/// On-device preferences (Settings > Appearance / Alerts): defaults, persistence, and that each one changes behaviour.
@Suite struct DisplayPrefsTests {
    private func suite() -> UserDefaults {
        let name = "prefs.test.\(UUID().uuidString)"
        let d = UserDefaults(suiteName: name)!
        d.removePersistentDomain(forName: name)
        return d
    }

    private func item(_ id: String, response: String) -> AgendaItem {
        AgendaItem(id: id, eventId: id, calendarId: "c", accountId: "a", color: "cobalt", title: id, allDay: false,
                   start: Date(timeIntervalSince1970: 1_800_000_000), end: Date(timeIntervalSince1970: 1_800_001_800),
                   tzid: "UTC", myResponse: response)
    }

    @Test func defaultsWhenNothingStored() {
        let p = DisplayPrefs.load(suite())
        #expect(p == DisplayPrefs(flipAnimation: true, showDeclined: false, showWeekNumbers: false, time24: true))
    }

    @Test func storedValuesWin() {
        let d = suite()
        d.set(false, forKey: DisplayPrefs.Key.flipAnimation); d.set(true, forKey: DisplayPrefs.Key.showDeclined)
        d.set(true, forKey: DisplayPrefs.Key.showWeekNumbers); d.set(false, forKey: DisplayPrefs.Key.time24)
        let p = DisplayPrefs.load(d)
        #expect(!p.flipAnimation && p.showDeclined && p.showWeekNumbers && !p.time24)
    }

    @Test func declinedEventsAreHiddenUnlessAsked() {
        let items = [item("a", response: "accepted"), item("b", response: "declined"), item("c", response: "needsAction")]
        #expect(DisplayPrefs(showDeclined: false).visible(items).map(\.id) == ["a", "c"])
        #expect(DisplayPrefs(showDeclined: true).visible(items).map(\.id) == ["a", "b", "c"])
    }

    @Test func flipsAnimateOnlyWhenPreferenceAndReduceMotionAllow() {
        #expect(DisplayPrefs(flipAnimation: true).animatesFlips(reduceMotion: false))
        #expect(!DisplayPrefs(flipAnimation: true).animatesFlips(reduceMotion: true))
        #expect(!DisplayPrefs(flipAnimation: false).animatesFlips(reduceMotion: false))
    }

    @Test func clockPreferenceForcesTheHourCycle() {
        let date = Date(timeIntervalSince1970: 15 * 3600)   // 15:00 UTC
        func fmt(_ p: DisplayPrefs) -> String {
            var f = Date.FormatStyle(date: .omitted, time: .shortened, locale: p.locale(base: Locale(identifier: "en_US")), timeZone: TimeZone(identifier: "UTC")!)
            f.calendar = Calendar(identifier: .gregorian)
            return date.formatted(f)
        }
        #expect(fmt(DisplayPrefs(time24: true)).hasPrefix("15:00"))
        #expect(fmt(DisplayPrefs(time24: false)).contains("3:00"))
    }

    @Test func isoWeekNumbers() {
        #expect(DisplayPrefs.weekNumber(of: DayKey("2026-10-12")) == 42)
        #expect(DisplayPrefs.weekNumber(of: DayKey("2026-01-01")) == 1)
        #expect(DisplayPrefs.weekNumber(of: DayKey("2024-12-30")) == 1)    // belongs to week 1 of 2025
        #expect(DisplayPrefs.weekNumber(of: DayKey("2027-01-03")) == 53)   // last Sunday of ISO 2026
    }

    @Test func hourLabelsFollowTheClock() {
        #expect(DayLogic.hourLabel(9, time24: true) == "09:00")
        #expect(DayLogic.hourLabel(15, time24: false).hasPrefix("3"))
        #expect(DayLogic.hourLabel(15, time24: false).uppercased().contains("PM"))
    }
}

@Suite struct AlertPrefsTests {
    private func suite() -> UserDefaults {
        let name = "alerts.test.\(UUID().uuidString)"
        let d = UserDefaults(suiteName: name)!
        d.removePersistentDomain(forName: name)
        return d
    }

    @Test func defaults() {
        #expect(AlertPrefs.load(suite()) == AlertPrefs(joinButton: true, movedCancelled: true, needsSignIn: true, invitations: true))
    }

    @Test func mirrorCopiesStandardDefaultsToTheSharedSuite() {
        let std = suite(), shared = suite()
        std.set(false, forKey: AlertPrefs.Key.movedCancelled); std.set(true, forKey: AlertPrefs.Key.invitations)
        let p = AlertPrefs.mirror(from: std, to: shared)
        #expect(!p.movedCancelled && p.invitations)
        #expect(AlertPrefs.load(shared) == p)
    }

    @Test func extensionDeliversSwitchedOffKindsQuietly() {
        var p = AlertPrefs()
        #expect(!NotificationQuiet.isQuiet(.moved, prefs: p))
        #expect(!NotificationQuiet.isQuiet(.cancelled, prefs: p))
        #expect(!NotificationQuiet.isQuiet(.reauth, prefs: p))
        #expect(!NotificationQuiet.isQuiet(.changed, prefs: p) && !NotificationQuiet.isQuiet(.invited, prefs: p))   // invitations default on
        p.movedCancelled = false; p.needsSignIn = false; p.invitations = false
        #expect(NotificationQuiet.isQuiet(.moved, prefs: p) && NotificationQuiet.isQuiet(.cancelled, prefs: p))
        #expect(NotificationQuiet.isQuiet(.reauth, prefs: p))
        #expect(NotificationQuiet.isQuiet(.changed, prefs: p) && NotificationQuiet.isQuiet(.invited, prefs: p))
        #expect(!NotificationQuiet.isQuiet(.summary, prefs: p) && !NotificationQuiet.isQuiet(.sync, prefs: p))
    }

    @Test func permissionStatusMapping() {
        #expect(AlertPermission.isGranted(.authorized) && AlertPermission.isGranted(.provisional))
        #expect(!AlertPermission.isGranted(.denied) && !AlertPermission.isGranted(.notDetermined))
    }
}
