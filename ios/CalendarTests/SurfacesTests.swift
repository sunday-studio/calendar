import CalendarCore
import Foundation
import SurfacesCore
import SurfacesUI
import Testing

private let ams = TimeZone(identifier: "Europe/Amsterdam")!
private let gb = Locale(identifier: "en_GB")

/// 2026-10-16 (Friday) at the given local time in Amsterdam (CEST).
private func at(_ h: Int, _ m: Int = 0, day: Int = 16) -> Date {
    var cal = Calendar(identifier: .gregorian); cal.timeZone = ams
    var c = DateComponents(year: 2026, month: 10, day: day, hour: h, minute: m); c.timeZone = ams
    return cal.date(from: c)!
}

private func item(_ id: String, _ title: String = "Event", start: Date, mins: Int = 30, color: String = "cobalt",
                  join: Join? = nil, location: String? = nil, response: String = "accepted", status: String = "confirmed",
                  account: String = "a", eventId: String? = nil, reply: Bool = false) -> AgendaItem {
    AgendaItem(id: id, eventId: eventId ?? id, calendarId: "c", accountId: account, color: color, title: title, allDay: false,
               start: start, end: start.addingTimeInterval(Double(mins) * 60), tzid: ams.identifier, location: location, join: join,
               myResponse: response, status: status, canRSVP: reply)
}

private func allDay(_ id: String, _ title: String, date: String, endDate: String) -> AgendaItem {
    AgendaItem(id: id, eventId: id, calendarId: "c", accountId: "a", color: "rose", title: title, allDay: true,
               date: date, endDate: endDate)
}

private let account = Account(id: "a", email: "alex@work.example", displayName: "Work", color: "cobalt", status: "active")
private func fmt() -> SurfaceFormat { SurfaceFormat(tz: ams, locale: gb) }

// MARK: - TimelineBuilder

@Suite struct TimelineBuilderTests {
    func input(_ items: [AgendaItem], accounts: [Account] = [account], paired: Bool = true, synced: Date? = nil) -> WidgetInput {
        WidgetInput(items: items, accounts: accounts, paired: paired, lastSyncAt: synced ?? at(10, 0), tz: ams)
    }

    @Test func nextEventShowsCountdownWords() {
        let e = TimelineBuilder.entry(input([item("e1", "Roadmap review", start: at(10, 30))]), at: at(10, 22), format: fmt())
        guard case .event(let s) = e.content else { Issue.record("expected event"); return }
        #expect(s.title == "Roadmap review")
        #expect(s.time == "10:30")
        #expect(s.relative == "in 8 min")
        #expect(!s.live)
        #expect(s.rows.first?.status == "8 min")
        #expect(s.accessibility == "Roadmap review, 10:30, in 8 min")
        #expect(e.link == .event("e1"))
    }

    @Test func inProgressSaysNowAndMinutesLeft() {
        let e = TimelineBuilder.entry(input([item("e1", start: at(10, 0), mins: 45)]), at: at(10, 23), format: fmt())
        guard case .event(let s) = e.content else { Issue.record("expected event"); return }
        #expect(s.live)
        #expect(s.relative == "Now \u{00B7} 22 min left")
        #expect(s.rows.first?.status == "Now")
    }

    @Test func remainingCountAndRowsSkipPastCancelledAndDeclined() {
        let items = [item("past", start: at(8)), item("now", start: at(10), mins: 60), item("x", start: at(11), status: "cancelled"),
                     item("d", start: at(11, 30), response: "declined"), item("l", start: at(12), response: "needsAction", reply: true),
                     item("m", start: at(14)), item("n", start: at(15)), item("o", start: at(16))]
        let e = TimelineBuilder.entry(input(items), at: at(10, 30), format: fmt())
        guard case .event(let s) = e.content else { Issue.record("expected event"); return }
        #expect(s.remaining == 5)
        #expect(s.rows.map(\.id) == ["now", "l", "m", "n"])   // at most 4
        #expect(s.rows[1].status == "Reply needed")
    }

    @Test func doneForTodayNamesTomorrowsEvent() {
        let items = [item("a", start: at(9)), item("t", "Standup", start: at(9, 0, day: 17))]
        let e = TimelineBuilder.entry(input(items), at: at(18), format: fmt())
        #expect(e.content == .done(next: .init(title: "Standup", when: "tomorrow 09:00", eventId: "t")))
        #expect(e.link == .event("t"))
    }

    @Test func nothingPlannedAndNoAccountsAndSignIn() {
        #expect(TimelineBuilder.entry(input([]), at: at(10), format: fmt()).content == .nothingPlanned)
        #expect(TimelineBuilder.entry(input([], accounts: []), at: at(10), format: fmt()).content == .noAccounts)
        #expect(TimelineBuilder.entry(input([item("a", start: at(11))], paired: false), at: at(10), format: fmt()).content == .noAccounts)
        var bad = account; bad.status = "needs_reauth"
        let e = TimelineBuilder.entry(input([item("a", start: at(11))], accounts: [bad]), at: at(10), format: fmt())
        #expect(e.content == .needsSignIn(account: "Work"))
        #expect(e.link == .accounts)
    }

    @Test func staleAfterSixHours() {
        let old = at(10).addingTimeInterval(-7 * 3600)
        let e = TimelineBuilder.entry(input([item("a", start: at(11))], synced: old), at: at(10), format: fmt())
        #expect(e.stale == "Saved 7 h ago")
        #expect(TimelineBuilder.entry(input([item("a", start: at(11))]), at: at(10), format: fmt()).stale == nil)
    }

    @Test func allDayEventsAreNotWidgetEvents() {
        let e = TimelineBuilder.entry(input([allDay("b", "Birthday", date: "2026-10-16", endDate: "2026-10-17")]), at: at(10), format: fmt())
        #expect(e.content == .done(next: nil))
    }

    @Test func timelineHasEntryAtEveryBoundaryAndPerMinuteCountdown() {
        let now = at(10, 0)
        let items = [item("a", start: at(10, 30), mins: 30), item("b", start: at(12, 0), mins: 60)]
        let dates = TimelineBuilder.entryDates(input(items), now: now)
        #expect(dates.first == now)
        #expect(dates == dates.sorted())
        #expect(Set(dates).count == dates.count)
        for d in [at(10, 30), at(11, 0), at(12, 0), at(13, 0)] { #expect(dates.contains(d)) }
        #expect(dates.contains(at(10, 22)))               // per-minute countdown before a start
        #expect(dates.contains(at(10, 45)))               // 15-minute grid
        #expect(dates.count <= TimelineBuilder.maxEntries)
        let tl = TimelineBuilder.timeline(input(items), now: now, locale: gb)
        #expect(tl.entries.count == dates.count)
        #expect(tl.reloadAt >= now.addingTimeInterval(15 * 60))
    }

    @Test func timelineEntriesRollOverAtBoundaries() {
        let now = at(10, 0)
        let tl = TimelineBuilder.timeline(input([item("a", start: at(10, 30), mins: 30), item("b", start: at(11, 15), mins: 30)]), now: now, locale: gb)
        func content(at t: Date) -> WidgetEntryData.Content? { tl.entries.last { $0.date <= t }?.content }
        guard case .event(let before)? = content(at: at(10, 29)), case .event(let during)? = content(at: at(10, 31)),
              case .event(let gap)? = content(at: at(11, 5)) else { Issue.record("entries"); return }
        #expect(before.id == "a" && !before.live)
        #expect(during.id == "a" && during.live)
        #expect(gap.id == "b" && !gap.live)
        if case .done? = content(at: at(11, 50)) {} else { Issue.record("expected done after the last event") }
    }

    @Test func midnightRolloverMakesTomorrowToday() {
        let now = at(23, 0)
        let items = [item("t", "Early", start: at(1, 0, day: 17), mins: 30)]
        let dates = TimelineBuilder.entryDates(input(items), now: now)
        #expect(dates.contains(at(0, 0, day: 17)))
    }

    @Test func flipTimeDropsDayPeriodForTwelveHourLocales() {
        let us = SurfaceFormat(tz: ams, locale: Locale(identifier: "en_US"))
        #expect(us.flipTime(at(14, 5)) == "2:05")
        #expect(fmt().flipTime(at(14, 5)) == "14:05")
    }
}

// MARK: - ReminderPlanner

@Suite struct ReminderPlannerTests {
    let now = at(9, 0)
    func plan(_ items: [AgendaItem], settings: Settings? = Settings(alertDefaults: AlertDefaults(timed: 10, allDay: 540)),
              overrides: [String: Int] = [:], showJoin: Bool = true) -> [PlannedReminder] {
        ReminderPlanner.plan(ReminderInput(items: items, settings: settings, overrides: overrides, accountNames: ["a": "alex@work.example"],
                                           tz: ams, showJoin: showJoin, locale: gb), now: now)
    }

    @Test func defaultLeadAndWording() {
        let meet = Join(url: "https://meet.google.com/abc", kind: "meet")
        let r = plan([item("e1", "Roadmap review", start: at(10, 30), join: meet), item("e2", "Lunch with Sam", start: at(12, 30), location: "Kōya")])
        // Two reminders plus a "starts now" for each event that has a link or a place.
        #expect(r.count == 4)
        #expect(r[0].fireDate == at(10, 20))
        #expect(r[0].title == "Roadmap review in 10 min")
        #expect(r[0].subtitle == "10:30 \u{00B7} Google Meet")
        #expect(r[0].body == "alex@work.example. Tap Join to open Google Meet.")
        #expect(r[0].category == NotificationCategory.reminderJoin)
        #expect(r[0].joinURL == "https://meet.google.com/abc")
        #expect(!r[0].timeSensitive && r[0].relevance == 0.7)
        #expect(r[1].title == "Roadmap review starts now" && r[1].timeSensitive && r[1].relevance == 1.0)
        #expect(r[1].subtitle == "Google Meet" && r[1].body == "10:30 to 11:00")
        #expect(r[2].title == "Lunch with Sam at 12:20".replacingOccurrences(of: "12:20", with: "12:30"))
        #expect(r[2].subtitle == "Kōya" && r[2].body == "")
        #expect(r[2].category == NotificationCategory.reminder && r[2].joinURL == nil && !r[2].timeSensitive)
    }

    @Test func joinActionOffWhenPreferenceOff() {
        let meet = Join(url: "https://meet.google.com/abc", kind: "meet")
        let r = plan([item("e1", start: at(10, 30), join: meet)], showJoin: false)
        #expect(r[0].category == NotificationCategory.reminder && r[0].joinURL == nil)
    }

    @Test func perEventOverrideWinsAndNilDefaultMeansNone() {
        let items = [item("e1", start: at(11), eventId: "E1"), item("e2", start: at(12))]
        let r = plan(items, settings: Settings(alertDefaults: AlertDefaults(timed: nil, allDay: nil)), overrides: ["E1": 30])
        #expect(r.map(\.occurrenceId) == ["e1"])
        #expect(r[0].fireDate == at(10, 30))
        // an occurrence-level override beats the series one
        let r2 = plan([item("e1:2", start: at(11), eventId: "E1")], overrides: ["E1": 30, "e1:2": 5])
        #expect(r2[0].fireDate == at(10, 55))
        // zero minutes = at the start
        #expect(plan([item("z", start: at(11))], overrides: ["z": 0])[0].fireDate == at(11))
    }

    @Test func defaultsWhenSettingsMissing() {
        #expect(plan([item("e", start: at(11))], settings: nil)[0].fireDate == at(10, 50))
    }

    @Test func allDayFiresAtConfiguredTimeBeforeMidnightOfTheDay() {
        // 540 min before 00:00 on the 17th = 15:00 on the 16th (local time, user zone).
        let r = plan([allDay("b", "Sam's birthday", date: "2026-10-17", endDate: "2026-10-18")])
        #expect(r.count == 1)
        #expect(r[0].fireDate == at(15, 0))
        #expect(r[0].title == "Tomorrow: Sam's birthday")
        #expect(r[0].subtitle == "All day \u{00B7} alex@work.example")
        #expect(r[0].thread == "day.2026-10-17")
        // alert time later on the same day: 0 min before midnight of the day itself = already passed -> skipped
        let none = plan([allDay("b", "x", date: "2026-10-16", endDate: "2026-10-17")], settings: Settings(alertDefaults: AlertDefaults(timed: 10, allDay: 0)))
        #expect(none.isEmpty)
    }

    @Test func pastCancelledDeclinedAndPendingAreSkipped() {
        let items = [item("past", start: at(8, 0)), item("soon", start: at(9, 5)),    // fire time 08:55 already passed
                     item("c", start: at(11), status: "cancelled"), item("d", start: at(12), response: "declined"),
                     item("p", start: at(13), eventId: "pending:k"), item("ok", start: at(14))]
        let r = plan(items.map { var i = $0; if i.id == "p" { i.id = "pending:k" }; return i })
        #expect(r.map(\.occurrenceId) == ["ok"])
    }

    @Test func dedupesRepeatedOccurrencesAndSameEventStart() {
        let a = item("e1", start: at(11), eventId: "E")
        let r = plan([a, a, item("e1b", start: at(11), eventId: "E")])
        #expect(r.count == 1)
    }

    @Test func capsAtSixtySortedByFireTime() {
        let items = (0..<80).map { item("e\($0)", start: at(10).addingTimeInterval(Double(80 - $0) * 3600)) }   // reverse order in
        let r = plan(items)
        #expect(r.count == ReminderPlanner.cap)
        #expect(r.count == 60)
        #expect(r.map(\.fireDate) == r.map(\.fireDate).sorted())
        #expect(r.first?.occurrenceId == "e79")
        #expect(ReminderPlanner.cap < 64)
    }

    @Test func diffReplacesChangedAndRemovesGone() {
        let a = plan([item("e1", start: at(11)), item("e2", start: at(12))])
        let pending = Dictionary(uniqueKeysWithValues: a.map { ($0.id, $0.signature) })
        // e1 moved, e2 untouched, e3 new, nothing removed
        let b = plan([item("e1", start: at(11, 30)), item("e2", start: at(12)), item("e3", start: at(13))])
        let d = ReminderPlanner.diff(planned: b, pending: pending)
        #expect(d.add.map(\.occurrenceId).sorted() == ["e1", "e3"])
        #expect(d.remove.isEmpty)
        let gone = ReminderPlanner.diff(planned: [], pending: pending)
        #expect(gone.remove == ["rem.e1", "rem.e2"])
    }
}

// MARK: - Scheduler against a fake notification centre

private actor FakeCenter: ReminderCenter {
    var authorized = true
    var pending: [String: PlannedReminder] = [:]
    var adds = 0, removes = 0
    func isAuthorized() async -> Bool { authorized }
    func pendingSignatures(prefix: String) async -> [String: String] {
        pending.filter { $0.key.hasPrefix(prefix) }.mapValues(\.signature)
    }
    func add(_ r: PlannedReminder) async { pending[r.id] = r; adds += 1 }
    func remove(ids: [String]) async { for i in ids { pending[i] = nil }; removes += ids.count }
    func setAuthorized(_ v: Bool) { authorized = v }
}

@Suite struct ReminderSchedulerTests {
    @Test func reschedulesFromTheSavedAgendaAndIsIdempotent() async throws {
        let now = Date()
        let tz = TimeZone.current
        let acc = Account(id: "acc1", email: "alex@example.com", displayName: "Alex", color: "cobalt", status: "active", lastSyncAt: now, lastOkAt: now)
        func it(_ id: String, _ mins: Double) -> AgendaItem {
            let s = now.addingTimeInterval(mins * 60)
            return AgendaItem(id: id, eventId: id, calendarId: "c", accountId: "acc1", color: "cobalt", title: id, allDay: false,
                              start: s, end: s.addingTimeInterval(1800), tzid: tz.identifier)
        }
        let env = try PreviewEnvironment.make(
            agenda: AgendaResponse(rev: 1, from: now.addingTimeInterval(-86_400), to: now.addingTimeInterval(86_400 * 7), items: [it("e1", 120), it("e2", 240)]),
            accounts: [acc], calendars: [HubCalendar(id: "c", accountId: "acc1", name: "Work", color: "cobalt")],
            settings: Settings(userTzid: tz.identifier, alertDefaults: AlertDefaults(timed: 10, allDay: nil)), clock: .fixed(now))
        let center = FakeCenter()
        let sched = ReminderScheduler(db: env.db, api: OfflineAPI(), center: center, clock: .fixed(now))
        await sched.reschedule()
        #expect(await center.pending.keys.sorted() == ["rem.e1", "rem.e2"])
        let adds = await center.adds
        await sched.reschedule()
        #expect(await center.adds == adds)           // unchanged agenda: nothing re-added
        await center.setAuthorized(false)
        try await env.db.apply(SyncSnapshot(agenda: .init(items: [it("e1", 120)], rev: 2, etag: nil, from: now.addingTimeInterval(-86_400), to: now.addingTimeInterval(86_400 * 7)), syncedAt: now))
        await sched.reschedule()
        #expect(await center.pending.count == 2)      // no permission: untouched
        await center.setAuthorized(true)
        await sched.reschedule()
        #expect(await center.pending.keys.sorted() == ["rem.e1"])
    }
}

// MARK: - Notification wording (NSE)

@Suite struct NotificationRewriterTests {
    let now = at(9, 0)
    func ctx(_ e: EventFacts? = nil, prev: Date? = nil, email: String? = nil, summary: DaySummary? = nil) -> RewriteContext {
        RewriteContext(event: e, previousStart: prev, accountEmail: email, summary: summary, tz: ams, now: now, locale: gb)
    }

    @Test func movedUsesNewTimeOldTimeAndCalendar() {
        let n = NotificationRewriter.rewrite(.init(kind: .moved, eventId: "E"),
                                             ctx(.init(title: "Dentist", start: at(11, 45), place: "Personal"), prev: at(11, 0)))
        #expect(n.title == "Dentist moved to 11:45")
        #expect(n.subtitle == "Was 11:00 \u{00B7} Personal")
        #expect(n.body == "Today, 45 min later.")
        #expect(n.category == NotificationCategory.moved)
    }

    @Test func movedToAnotherDayNamesTheDay() {
        let n = NotificationRewriter.rewrite(.init(kind: .moved, eventId: "E"),
                                             ctx(.init(title: "Dentist", start: at(11, 45, day: 17), place: nil), prev: at(11, 0)))
        #expect(n.title == "Dentist moved to tomorrow 11:45")
        #expect(n.subtitle == "Was Fri 16 Oct 11:00")
    }

    @Test func cancelledNamesTheDay() {
        let n = NotificationRewriter.rewrite(.init(kind: .cancelled, eventId: "E"),
                                             ctx(.init(title: "Gym", start: at(17, 30, day: 18), place: "Home", cancelled: true)))
        #expect(n.title == "Gym at 17:30 cancelled")
        #expect(n.subtitle == "Home" && n.body == "Removed from Sunday.")
        #expect(n.category == NotificationCategory.event)
        let t = NotificationRewriter.rewrite(.init(kind: .cancelled, eventId: "E"), ctx(.init(title: "Gym", start: at(17, 30), place: nil)))
        #expect(t.title == "Gym at 17:30 cancelled" && t.body == "Removed from today.")
    }

    @Test func reauthAndSummary() {
        let r = NotificationRewriter.rewrite(.init(kind: .reauth, accountId: "a"), ctx(email: "alex@work.example"))
        #expect(r.title == "alex@work.example needs you to sign in")
        #expect(r.body == "Its events are paused until you do.")
        #expect(r.category == NotificationCategory.reauth)
        let s = NotificationRewriter.rewrite(.init(kind: .summary, date: "2026-10-16"),
                                             ctx(summary: DaySummary(date: "2026-10-16", count: 5, title: "Today: 5 events, first at 09:00", body: "Standup, Design sync.")))
        #expect(s.title == "Today: 5 events, first at 09:00" && s.body == "Standup, Design sync.")
        #expect(s.category == NotificationCategory.summary)
    }

    @Test func fallsBackToGenericTextWhenFactsAreMissing() {
        for k in [PushPayload.Kind.moved, .cancelled, .reauth, .summary] {
            let n = NotificationRewriter.rewrite(.init(kind: k), ctx())
            #expect(!n.title.isEmpty && !n.body.isEmpty)
            #expect(n == NotificationRewriter.generic(.init(kind: k)))
        }
        #expect(NotificationRewriter.generic(.init(kind: .changed)).title == "Your calendar changed")
        #expect(NotificationRewriter.rewrite(.init(kind: .changed), ctx()).body == "Open Calendar to see what is new.")
        // a "moved" push for an event that is gone / cancelled falls back too
        let gone = NotificationRewriter.rewrite(.init(kind: .moved), ctx(.init(title: "X", start: at(10), cancelled: true)))
        #expect(gone == NotificationRewriter.generic(.init(kind: .moved)))
    }

    @Test func parsesTheHubPayloadShape() {
        let p = PushPayload(userInfo: ["k": "moved", "eventId": "E1", "rev": NSNumber(value: 42), "aps": ["alert": ["title": "Calendar"]]])
        #expect(p == PushPayload(kind: .moved, eventId: "E1", rev: 42))
        #expect(PushPayload(userInfo: ["k": "reauth", "accountId": "a1"])?.accountId == "a1")
        #expect(PushPayload(userInfo: ["k": "summary", "date": "2026-10-16"])?.date == "2026-10-16")
        #expect(PushPayload(userInfo: ["k": "bogus"]) == nil)
        #expect(PushPayload(userInfo: [:]) == nil)
    }
}

private struct FakeNotificationHub: NotificationHubAPI {
    var detail: EventDetail?
    var accounts: [Account] = []
    var summary: DaySummary?
    var delay: Duration = .zero
    func event(id: String) async throws -> EventDetail {
        try await Task.sleep(for: delay)
        guard let detail else { throw APIClientError.notFound(nil) }
        return detail
    }
    func accounts() async throws -> [Account] { accounts }
    func summary(date: String?) async throws -> DaySummary {
        guard let summary else { throw APIClientError.transport("down") }
        return summary
    }
}

@Suite struct NotificationEnricherTests {
    @Test func movedFetchesTheEventAndReadsTheOldTimeFromTheSavedAgenda() async throws {
        let now = Date()
        let old = now.addingTimeInterval(3600), new = now.addingTimeInterval(7200)
        let saved = AgendaItem(id: "E1", eventId: "E1", calendarId: "c", accountId: "a", color: "cobalt", title: "Dentist", allDay: false, start: old, end: old.addingTimeInterval(1800))
        let env = try PreviewEnvironment.make(agenda: AgendaResponse(rev: 1, from: now.addingTimeInterval(-86_400), to: now.addingTimeInterval(86_400 * 7), items: [saved]),
                                              accounts: [account], calendars: [HubCalendar(id: "c", accountId: "a", name: "Personal", color: "cobalt")],
                                              settings: Settings(userTzid: TimeZone.current.identifier), clock: .fixed(now))
        var moved = saved; moved.start = new; moved.end = new.addingTimeInterval(1800)
        let hub = FakeNotificationHub(detail: EventDetail(item: moved, calendarName: "Personal"))
        let n = await NotificationEnricher(api: hub, db: env.db, clock: .fixed(now)).notification(for: .init(kind: .moved, eventId: "E1"))
        #expect(n.title.hasPrefix("Dentist moved to "))
        #expect(n.subtitle?.hasPrefix("Was ") == true && n.subtitle?.hasSuffix("Personal") == true)
    }

    @Test func cancelledUsesTheSavedRowBecauseTheHubReturns404() async throws {
        let now = Date()
        let s = now.addingTimeInterval(3 * 3600)
        let saved = AgendaItem(id: "E1", eventId: "E1", calendarId: "c", accountId: "a", color: "cobalt", title: "Gym", allDay: false, start: s, end: s.addingTimeInterval(3600))
        let env = try PreviewEnvironment.make(agenda: AgendaResponse(rev: 1, from: now.addingTimeInterval(-86_400), to: now.addingTimeInterval(86_400 * 7), items: [saved]),
                                              accounts: [account], calendars: [HubCalendar(id: "c", accountId: "a", name: "Home", color: "cobalt")],
                                              settings: Settings(userTzid: TimeZone.current.identifier), clock: .fixed(now))
        let n = await NotificationEnricher(api: FakeNotificationHub(), db: env.db, clock: .fixed(now)).notification(for: .init(kind: .cancelled, eventId: "E1"))
        #expect(n.title.hasSuffix("cancelled") && n.title.hasPrefix("Gym"))
        #expect(n.subtitle == "Home" && n.body.hasPrefix("Removed from"))
    }

    @Test func timeoutFallsBackToGenericText() async {
        let hub = FakeNotificationHub(detail: nil, summary: nil, delay: .seconds(5))
        let started = Date()
        let n = await NotificationEnricher(api: hub, db: nil, requestTimeout: 0.2).notification(for: .init(kind: .moved, eventId: "E1"))
        #expect(n == NotificationRewriter.generic(.init(kind: .moved)))
        #expect(Date().timeIntervalSince(started) < 3)
    }

    @Test func summaryAndReauthComeFromTheHub() async {
        let hub = FakeNotificationHub(accounts: [account], summary: DaySummary(date: "d", count: 1, title: "Today: 1 event", body: "Standup."))
        let e = NotificationEnricher(api: hub, db: nil)
        #expect(await e.notification(for: .init(kind: .summary, date: "d")).title == "Today: 1 event")
        #expect(await e.notification(for: .init(kind: .reauth, accountId: "a")).title == "alex@work.example needs you to sign in")
        #expect(await NotificationEnricher(api: nil, db: nil).notification(for: .init(kind: .summary)).title == "Your day is ready")
    }
}

// MARK: - Routing, links, Live Activity

@Suite struct SurfaceRoutingTests {
    @Test func linksRoundTrip() {
        for l in [SurfaceLink.event("abc:20261016T090000Z"), .today, .accounts, .summary(date: "2026-10-16"), .event("plain")] {
            #expect(SurfaceLink.parse(l.url) == l)
        }
        #expect(SurfaceLink.event("E1").url.absoluteString == "calendar://event/E1")
        #expect(SurfaceLink.parse(URL(string: "calendar://pair?host=x")!) == nil)
        #expect(SurfaceLink.parse(URL(string: "https://event/1")!) == nil)
    }

    @Test func actionsRoute() {
        let local: [AnyHashable: Any] = ["link": "calendar://event/e1", "join": "https://meet.google.com/abc", "k": "reminder"]
        #expect(NotificationRouter.route(action: "com.apple.UNNotificationDefaultActionIdentifier", userInfo: local) == .open(.event("e1")))
        #expect(NotificationRouter.route(action: NotificationCategory.actionJoin, userInfo: local) == .openURL(URL(string: "https://meet.google.com/abc")!))
        #expect(NotificationRouter.route(action: NotificationCategory.actionSnooze, userInfo: local) == .snooze)
        #expect(NotificationRouter.route(action: NotificationCategory.actionOK, userInfo: local) == .none)
        #expect(NotificationRouter.route(action: NotificationCategory.actionLater, userInfo: [:]) == .none)
        #expect(NotificationRouter.route(action: NotificationCategory.actionMute, userInfo: ["k": "summary"]) == .muteSummary)
        #expect(NotificationRouter.route(action: NotificationCategory.actionSignIn, userInfo: ["k": "reauth"]) == .open(.accounts))
        #expect(NotificationRouter.route(action: NotificationCategory.actionOpenToday, userInfo: ["k": "summary"]) == .open(.today))
        // remote taps
        let tap = "com.apple.UNNotificationDefaultActionIdentifier"
        #expect(NotificationRouter.route(action: tap, userInfo: ["k": "moved", "eventId": "E9"]) == .open(.event("E9")))
        #expect(NotificationRouter.route(action: tap, userInfo: ["k": "cancelled", "eventId": "E9"]) == .open(.today))
        #expect(NotificationRouter.route(action: tap, userInfo: ["k": "reauth", "accountId": "a"]) == .open(.accounts))
        #expect(NotificationRouter.route(action: NotificationCategory.actionView, userInfo: ["k": "moved", "eventId": "E9"]) == .open(.event("E9")))
    }

    @Test func categoriesHaveTheSpecActions() {
        let cats = Dictionary(uniqueKeysWithValues: NotificationCategories.all().map { ($0.identifier, $0.actions.map(\.title)) })
        #expect(cats[NotificationCategory.reminderJoin] == ["Join", "Snooze 10 min", "Open"])
        #expect(cats[NotificationCategory.reminder] == ["Snooze 10 min", "Open"])
        #expect(cats[NotificationCategory.invite] == ["Accept", "Maybe", "Decline"])
        #expect(cats[NotificationCategory.conflict] == ["Open day", "Dismiss"])
        #expect(cats[NotificationCategory.moved] == ["View", "OK"])
        #expect(cats[NotificationCategory.reauth] == ["Sign in", "Later"])
        #expect(cats[NotificationCategory.summary] == ["Open today", "Mute"])
    }
}

@Suite struct LiveActivityPlannerTests {
    let meet = Join(url: "https://meet.google.com/abc", kind: "meet")

    @Test func startsThirtyMinutesBeforeAnEventWithALinkOrLocation() {
        let items = [item("e1", start: at(10, 30), join: meet)]
        #expect(LiveActivityPlanner.desired(items: items, now: at(9, 59)) == nil)
        #expect(LiveActivityPlanner.desired(items: items, now: at(10, 0))?.occurrenceId == "e1")
        #expect(LiveActivityPlanner.desired(items: items, now: at(10, 0))?.running == false)
        #expect(LiveActivityPlanner.desired(items: [item("p", start: at(10, 30))], now: at(10, 15)) == nil)          // no link or place
        #expect(LiveActivityPlanner.desired(items: [item("p", start: at(10, 30), location: "Room 4")], now: at(10, 15)) != nil)
    }

    @Test func runsUntilTheEventEndsAndSkipsCancelledDeclinedAllDay() {
        let items = [item("e1", start: at(10, 30), mins: 30, join: meet)]
        #expect(LiveActivityPlanner.desired(items: items, now: at(10, 45))?.running == true)
        #expect(LiveActivityPlanner.desired(items: items, now: at(11, 0)) == nil)
        #expect(LiveActivityPlanner.desired(items: [item("c", start: at(10, 30), join: meet, status: "cancelled")], now: at(10, 20)) == nil)
        #expect(LiveActivityPlanner.desired(items: [item("d", start: at(10, 30), join: meet, response: "declined")], now: at(10, 20)) == nil)
    }

    @Test func prefersTheOneInProgressThenStartsUpdatesAndEnds() {
        let items = [item("now", start: at(10, 0), mins: 60, join: meet), item("next", start: at(10, 40), join: meet)]
        let p = LiveActivityPlanner.plan(items: items, existing: [], now: at(10, 20))
        guard case .start(let d)? = p.action else { Issue.record("start"); return }
        #expect(d.occurrenceId == "now")
        let u = LiveActivityPlanner.plan(items: items, existing: ["now", "old"], now: at(10, 20))
        if case .update? = u.action {} else { Issue.record("update") }
        #expect(u.end == ["old"])
        let e = LiveActivityPlanner.plan(items: [], existing: ["now"], now: at(10, 20))
        #expect(e.action == nil && e.end == ["now"])
    }

    @Test func staleDateFlipsTheCopyAtTheStart() {
        let d = LiveActivityPlanner.desired(items: [item("e", start: at(10, 30), mins: 30, join: meet)], now: at(10, 10))!
        #expect(d.staleDate == at(10, 30))
        let r = LiveActivityPlanner.desired(items: [item("e", start: at(10, 30), mins: 30, join: meet)], now: at(10, 40))!
        #expect(r.staleDate == at(11, 0))
    }

    @Test func progressLineCountsTheLeadThenTheEvent() {
        let s = at(10, 30), e = at(11, 0)
        #expect(LiveActivityPlanner.progress(start: s, end: e, now: at(10, 0)) == 0)
        #expect(abs(LiveActivityPlanner.progress(start: s, end: e, now: at(10, 15)) - 0.5) < 0.001)
        #expect(LiveActivityPlanner.progress(start: s, end: e, now: at(10, 30)) == 0)
        #expect(abs(LiveActivityPlanner.progress(start: s, end: e, now: at(10, 45)) - 0.5) < 0.001)
        #expect(LiveActivityPlanner.progress(start: s, end: e, now: at(11, 30)) == 1)
    }
}

// MARK: - Push registration

private actor FakePushAPI: HubPushAPI {
    var registered: [(String, PushEnvironment)] = []
    var unregisters = 0
    var failNext: APIClientError?
    func fail(_ e: APIClientError?) { failNext = e }
    func registerPush(apnsToken: String, environment: PushEnvironment, bundleId: String?) async throws {
        if let f = failNext { failNext = nil; throw f }
        registered.append((apnsToken, environment))
    }
    func unregisterPush() async throws { unregisters += 1 }
    func summary(date: String?) async throws -> DaySummary { throw APIClientError.invalidResponse }
}

private final class MemoryPushStore: PushRegistrationStore, @unchecked Sendable {
    var record: PushRegistrationRecord?
    func load() -> PushRegistrationRecord? { record }
    func save(_ r: PushRegistrationRecord?) { record = r }
}

@MainActor @Suite struct PushRegistrationTests {
    let hub = URL(string: "https://hub.example.ts.net")!

    fileprivate func model(_ api: FakePushAPI, store: MemoryPushStore = MemoryPushStore(), env: PushEnvironment = .sandbox) -> PushRegistrationModel {
        PushRegistrationModel(api: api, hub: hub, store: store, environment: env, bundleId: "dev.casprine.calendar")
    }

    @Test func hexEncodesTheToken() {
        #expect(PushRegistrationModel.hex(Data([0x0a, 0xff, 0x00, 0x10])) == "0aff0010")
    }

    @Test func registersOnceWithEnvironmentAndSkipsUnchangedTokens() async {
        let api = FakePushAPI(); let m = model(api)
        await m.tokenReceived(hex: "aabb")
        #expect(m.state == .registered)
        await m.tokenReceived(hex: "aabb")
        await m.registerIfNeeded()
        let regs = await api.registered
        #expect(regs.count == 1)
        #expect(regs[0].0 == "aabb" && regs[0].1 == .sandbox)
        await m.tokenReceived(hex: "ccdd")          // token changed
        #expect(await api.registered.map(\.0) == ["aabb", "ccdd"])
    }

    @Test func waitsForATokenAndRetriesAfterAFailure() async {
        let api = FakePushAPI(); let m = model(api)
        await m.registerIfNeeded()
        #expect(m.state == .waitingForToken)
        await api.fail(.transport("offline"))
        await m.tokenReceived(hex: "aabb")
        if case .failed = m.state {} else { Issue.record("expected failed") }
        await m.registerIfNeeded()                   // next foreground
        #expect(m.state == .registered)
        #expect(await api.registered.count == 1)
    }

    @Test func aDifferentHubOrEnvironmentRegistersAgain() async {
        let store = MemoryPushStore()
        store.record = PushRegistrationRecord(token: "aabb", environment: .production, hub: hub.absoluteString)
        let api = FakePushAPI()
        let m = model(api, store: store, env: .sandbox)
        await m.tokenReceived(hex: "aabb")
        #expect(await api.registered.count == 1)
        #expect(store.record?.environment == .sandbox)
    }

    @Test func forgetUnregistersAndClears() async {
        let api = FakePushAPI(); let store = MemoryPushStore(); let m = model(api, store: store)
        await m.tokenReceived(hex: "aabb")
        await m.forget()
        #expect(await api.unregisters == 1)
        #expect(store.record == nil)
        #expect(m.state == .idle)
        await m.forget()                             // nothing left to unregister
        #expect(await api.unregisters == 1)
    }
}

// MARK: - v2 day layout and fill

@Suite struct DayLayoutTests {
    func input(_ items: [AgendaItem]) -> WidgetInput { WidgetInput(items: items, accounts: [account], lastSyncAt: at(10, 0), tz: ams) }
    func layout(_ items: [AgendaItem], now: Date) -> DayLayout {
        TimelineBuilder.layout(input(items), at: now, format: fmt())
    }

    @Test func heroIsNextAndRowsFollowWithCancelledAndDeclinedHidden() {
        var cancelled = item("c", "Gym", start: at(17, 30)); cancelled.status = "cancelled"
        var declined = item("d", "Skip", start: at(13, 0)); declined.myResponse = "declined"
        let l = layout([item("a", "Roadmap", start: at(10, 30)), item("b", "Lunch", start: at(12, 30)), cancelled, declined], now: at(10, 22))
        #expect(l.hero?.title == "Roadmap")
        #expect(l.hero?.sub == "IN 8 MIN")
        #expect(l.today.map(\.title) == ["Lunch"])
        #expect(l.todayLeft == 2)
    }

    @Test func overlapBeatsReplyAndInProgressSaysLeft() {
        var reply = item("r", "Dinner", start: at(19, 0)); reply.myResponse = "needsAction"; reply.canRSVP = true
        let l = layout([item("a", "Design review", start: at(14, 30), mins: 60), item("b", "1:1", start: at(15, 0)), reply], now: at(14, 40))
        #expect(l.hero?.live == true)
        #expect(l.hero?.sub == "50 MIN LEFT")
        #expect(l.today.first?.status == "OVERLAP")
        #expect(l.today.last?.status == "REPLY")
    }

    @Test func allDayIsALineNotARow() {
        let l = layout([allDay("ad", "Ana's birthday", date: "2026-10-16", endDate: "2026-10-17"), item("a", "Standup", start: at(11, 0))], now: at(10, 0))
        #expect(l.allDay.map(\.title) == ["Ana's birthday"])
        #expect(l.today.isEmpty && l.hero?.title == "Standup")
    }

    @Test func fillShowsTomorrowUnderALabelAndCountsOverflow() {
        func rows(_ n: Int, _ p: String) -> [WidgetRowData] { (0..<n).map { WidgetRowData(id: "\(p)\($0)", time: "09:00", title: "\(p)\($0)", status: nil, color: "cobalt", live: false) } }
        var l = DayLayout(header: "TUE 13", tomorrowHeader: "TOMORROW \u{00B7} WED 14", allDay: [], hero: nil, today: rows(1, "t"), tomorrow: rows(5, "m"), todayLeft: 1)
        let few = WidgetFill.rows(l, slots: 3)
        #expect(few.rows.count == 3 && few.tomorrowShown && few.overflow == 4)
        l.today = rows(6, "t")
        let many = WidgetFill.rows(l, slots: 3)
        #expect(many.rows.count == 3 && !many.tomorrowShown && many.overflow == 3)
        l.today = []; l.hero = nil
        #expect(WidgetFill.rows(l, slots: 3).rows.first == .label("NEXT \u{00B7} TOMORROW"))
    }

    @Test func staleLayoutDropsTheHeroAndStatusWords() {
        let l = TimelineBuilder.layout(input([item("a", "Roadmap", start: at(10, 30)), item("b", "Lunch", start: at(12, 30))]), at: at(10, 22), format: fmt(), stale: true)
        #expect(l.hero == nil && l.today.count == 2)
    }
}

@Suite struct SurfaceLinkV2Tests {
    @Test func dayAndNewRoundTrip() {
        #expect(SurfaceLink.parse(SurfaceLink.day("2026-10-13").url) == .day("2026-10-13"))
        #expect(SurfaceLink.parse(SurfaceLink.new.url) == .new)
        #expect(SurfaceLink.parse(URL(string: "calendar://today")!) == .today)
    }
}

@Suite struct LongLookModelTests {
    @Test func notifiedEventIsTheOnlyBrightBlockAndJoinComesFromTheLink() {
        let meet = Join(url: "https://meet.google.com/abc", kind: "meet")
        let a = item("a", "Roadmap", start: at(10, 30), join: meet)
        let b = item("b", "Lunch", start: at(11, 30))
        let m = NotificationContentModel.make(items: [a, b], eventId: "a", now: at(10, 22), tz: ams)
        #expect(m?.title == "Roadmap" && m?.joinLabel == "Google Meet" && m?.joinURL != nil)
        #expect(m?.blocks.filter(\.notified).map(\.id) == ["a"])
        #expect(m?.blocks.count == 2)
        #expect(NotificationContentModel.make(items: [a], eventId: "zzz", now: at(10, 22), tz: ams) == nil)
    }

    @Test func movedMarksAreRememberedOnTheLayoutRow() {
        var moved = item("m", "Dentist", start: at(11, 45))
        moved.prevStart = at(11, 0); moved.movedAt = at(9, 0)
        let l = TimelineBuilder.layout(WidgetInput(items: [item("h", "Standup", start: at(10, 30)), moved], accounts: [account], lastSyncAt: at(10, 0), tz: ams),
                                       at: at(10, 22), format: fmt())
        #expect(l.today.first?.status == "MOVED" && l.today.first?.spoken.contains("moved from 11:00") == true)
    }
}
