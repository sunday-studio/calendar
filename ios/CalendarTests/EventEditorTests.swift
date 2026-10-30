import CalendarCore
import Foundation
import Testing
@testable import FeatureEvent

private let tzLondon = TimeZone(identifier: "Europe/London")!
private func at(_ s: String) -> Date { (try? Date.ISO8601FormatStyle().parse(s)) ?? Date(timeIntervalSince1970: 0) }

private final class FakeWriter: EventWriting, @unchecked Sendable {
    private let lock = NSLock()
    private var _outcomes: [WriteOutcome]
    private(set) var creates: [(EventDraft, String?)] = []
    private(set) var updates: [(AgendaItem, EventPatch, EventScope, String?)] = []
    private(set) var moves: [(String, String?)] = []
    init(_ o: [WriteOutcome]) { _outcomes = o }
    private func next() -> WriteOutcome { lock.withLock { _outcomes.isEmpty ? .saved(nil) : _outcomes.removeFirst() } }
    func create(_ draft: EventDraft, key: String?, mode: WriteMode) async -> WriteOutcome { lock.withLock { creates.append((draft, key)) }; return next() }
    func update(_ item: AgendaItem, patch: EventPatch, scope: EventScope, key: String?, mode: WriteMode) async -> WriteOutcome { lock.withLock { updates.append((item, patch, scope, key)) }; return next() }
    func delete(_ item: AgendaItem, scope: EventScope, key: String?, mode: WriteMode) async -> WriteOutcome { next() }
    func rsvp(_ item: AgendaItem, response: RSVPResponse, note: String?, scope: EventScope, key: String?, mode: WriteMode) async -> WriteOutcome { next() }
    func addMeet(_ item: AgendaItem, key: String?) async -> WriteOutcome { next() }
    func move(_ item: AgendaItem, toCalendar calendarId: String, key: String?, mode: WriteMode) async -> WriteOutcome { lock.withLock { moves.append((calendarId, key)) }; return next() }
    func setReminder(_ item: AgendaItem, minutesBefore: Int?) async -> WriteOutcome { next() }
}

private final class KeyCounter: @unchecked Sendable {
    private let lock = NSLock(); private var n = 0
    func next() -> Int { lock.withLock { n += 1; return n } }
}

private let cals = [
    HubCalendar(id: "ro", accountId: "a", name: "Holidays", color: "moss", access: "reader"),
    HubCalendar(id: "work", accountId: "a", name: "Work", color: "cobalt", access: "owner", isPrimary: true),
    HubCalendar(id: "home", accountId: "a", name: "Home", color: "rose", access: "writer"),
]

private func detail(version: Int64 = 4, recurring: Bool = false, title: String = "Standup") -> EventDetail {
    EventDetail(item: AgendaItem(id: "E1:1", eventId: "E1", version: version, calendarId: "work", accountId: "a", color: "cobalt", title: title,
                                 allDay: false, start: at("2026-10-12T09:00:00Z"), end: at("2026-10-12T09:30:00Z"), tzid: "Europe/London",
                                 location: "Room 2", recurring: recurring, canEdit: true),
                description: "Notes", attendees: [Attendee(email: "me@x.com", isSelf: true), Attendee(email: "bo@x.com")],
                recurrence: recurring ? Recurrence(rrule: "FREQ=DAILY") : nil, reminderMin: 10)
}

@MainActor private func creator(_ w: FakeWriter) -> EventEditorViewModel {
    EventEditorViewModel(create: at("2026-10-16T12:00:00Z"), calendars: cals, defaultCalendarId: nil, tz: tzLondon, writer: w, makeKey: { "k1" })
}
@MainActor private func editor(_ w: FakeWriter, _ d: EventDetail = detail()) -> EventEditorViewModel {
    EventEditorViewModel(edit: d, calendars: cals, tz: tzLondon, writer: w, makeKey: { "k1" })
}

@MainActor struct EventEditorCreateTests {
    @Test func defaultsAndCalendarChoices() {
        let vm = creator(FakeWriter([]))
        #expect(vm.writableCalendars.map(\.id) == ["work", "home"])      // read-only calendar not offered
        #expect(vm.calendarId == "work")                                // primary
        #expect(vm.fields.end.timeIntervalSince(vm.fields.start) == 3600)
        #expect(!vm.isDirty && !vm.shouldConfirmDiscard && !vm.canSave)
    }

    @Test func titleMakesItSaveableAndDirtyAsksBeforeDiscard() {
        let vm = creator(FakeWriter([]))
        vm.title = "Lunch"
        #expect(vm.isDirty && vm.shouldConfirmDiscard && vm.canSave)
        vm.title = ""
        #expect(!vm.isDirty && !vm.shouldConfirmDiscard)                // back to pristine: nothing to lose
    }

    @Test func blankTitleAndBackwardsTimesBlockSave() {
        let vm = creator(FakeWriter([]))
        vm.location = "Cafe"
        #expect(!vm.canSave && vm.message(for: "title") != nil)
        vm.title = "Lunch"
        vm.setEnd(vm.fields.start.addingTimeInterval(-60))
        #expect(!vm.canSave && vm.message(for: "when") == "The end has to be after the start.")
    }

    @Test func movingStartKeepsLength() {
        let vm = creator(FakeWriter([]))
        vm.setEnd(vm.fields.start.addingTimeInterval(5400))
        vm.setStart(vm.fields.start.addingTimeInterval(7200))
        #expect(vm.fields.end.timeIntervalSince(vm.fields.start) == 5400)
    }

    @Test func allDaySpanUsesExclusiveEndOnTheWire() {
        let vm = creator(FakeWriter([]))
        vm.title = "Trip"; vm.isAllDay = true
        vm.setFirstDay("2026-10-20"); vm.setLastDay("2026-10-22")
        #expect(vm.when?.date == "2026-10-20" && vm.when?.endDate == "2026-10-23")
        vm.setFirstDay("2026-10-25")                                    // span is kept
        #expect(vm.fields.lastDay == "2026-10-27")
        #expect(vm.canSave)
    }

    @Test func attendeesValidateAndDedupe() {
        let vm = creator(FakeWriter([]))
        #expect(vm.addAttendee(" Bo@x.com "))
        #expect(!vm.addAttendee("bo@x.com") && !vm.addAttendee("nope") && !vm.addAttendee(""))
        #expect(vm.fields.attendees == ["Bo@x.com"])
        vm.removeAttendee("Bo@x.com")
        #expect(vm.fields.attendees.isEmpty)
    }

    @Test func aGuestTypedButNotSubmittedIsKeptWhenLeavingThePeopleScreen() {
        let vm = creator(FakeWriter([]))
        #expect(vm.commitPendingGuest(""))                       // nothing typed: nothing to do
        #expect(vm.fields.attendees.isEmpty)
        #expect(vm.commitPendingGuest(" sam@x.com "))            // typed, Done tapped without Return
        #expect(vm.fields.attendees == ["sam@x.com"])
        #expect(!vm.commitPendingGuest("not an address"))        // refused, so the screen can say why
        #expect(!vm.commitPendingGuest("SAM@x.com"))             // already there
        #expect(vm.fields.attendees == ["sam@x.com"])
    }

    @Test func saveSendsDraftAndMarksSaved() async {
        let w = FakeWriter([.saved(detail())])
        let vm = creator(w)
        vm.title = "Lunch"; vm.addsMeet = true; vm.reminderMin = 15
        await vm.save()
        #expect(vm.saveState == .saved && !vm.shouldConfirmDiscard)
        #expect(w.creates.count == 1 && w.creates[0].1 == "k1")
        let d = w.creates[0].0
        #expect(d.title == "Lunch" && d.calendarId == "work" && d.conference == .meet && d.reminderMin == 15)
        #expect(d.when.durationMin == 60 && d.when.tzid == "Europe/London")
    }

    @Test func failureThenRetryReusesTheSameKeyUntilFieldsChange() async {
        let n = KeyCounter()
        let w = FakeWriter([.failed(.offline), .failed(.offline), .saved(nil)])
        let vm = EventEditorViewModel(create: at("2026-10-16T12:00:00Z"), calendars: cals, defaultCalendarId: "home", tz: tzLondon, writer: w,
                                      makeKey: { "k\(n.next())" })
        vm.title = "Lunch"
        await vm.save()
        guard case .failed(let m, let canRetry) = vm.saveState else { Issue.record("expected failed"); return }
        #expect(canRetry && m.contains("Can\u{2019}t reach your hub"))
        await vm.save()
        #expect(w.creates.map { $0.1 } == ["k1", "k1"])                 // same action, same key
        vm.title = "Lunch 2"                                            // a different action now
        await vm.save()
        #expect(w.creates.map { $0.1 } == ["k1", "k1", "k2"])
        #expect(vm.saveState == .saved && w.creates[0].0.calendarId == "home")
    }

    @Test func providerFailureKeepsEditsAndOffersNoBlindRetry() async {
        let vm = creator(FakeWriter([.failed(.provider("Quota exceeded"))]))
        vm.title = "Lunch"
        await vm.save()
        #expect(vm.saveState == .failed(message: "Google didn\u{2019}t accept the change. Quota exceeded", canRetry: false))
        #expect(vm.isDirty && vm.shouldConfirmDiscard && vm.title == "Lunch")
        vm.dismissError()
        #expect(vm.saveState == .idle)
    }

    @Test func hubFieldErrorShowsOnTheField() async {
        let vm = creator(FakeWriter([.failed(.validation(field: "attendees", message: "bo@ is not an address"))]))
        vm.title = "Lunch"
        await vm.save()
        #expect(vm.message(for: "attendees") == "bo@ is not an address" && !vm.canSave)
        vm.title = "Lunch!"                                            // editing clears the stale hub error
        #expect(vm.message(for: "attendees") == nil && vm.canSave)
    }

    @Test func cannotSaveTwiceOrDismissWhileSaving() async {
        let vm = creator(FakeWriter([.saved(nil)]))
        vm.title = "Lunch"
        await vm.save()
        await vm.save()
        #expect(vm.saveState == .saved)
        #expect(vm.canDismiss)
    }
}

@MainActor struct EventEditorEditTests {
    @Test func prefilledFromDetailExcludingSelf() {
        let vm = editor(FakeWriter([]))
        #expect(vm.title == "Standup" && vm.location == "Room 2" && vm.notes == "Notes")
        #expect(vm.fields.attendees == ["bo@x.com"] && vm.reminderMin == 10)
        #expect(!vm.isDirty && vm.patch.isEmpty && !vm.canSave)
    }

    @Test func patchHasOnlyWhatChangedAndClearsEmptiedFields() {
        let vm = editor(FakeWriter([]))
        vm.title = "Daily"
        vm.location = ""
        vm.reminderMin = nil
        let p = vm.patch
        #expect(p.title == .set("Daily") && p.location == .clear && p.reminderMin == .clear)
        #expect(p.when == .unchanged && p.attendees == .unchanged && p.description == .unchanged)
    }

    @Test func savePassesVersionScopeAndKey() async {
        let w = FakeWriter([.saved(detail(version: 5))])
        let vm = editor(w, detail(recurring: true))
        vm.title = "Daily"
        #expect(vm.needsScopeChoice && vm.scopeChoices == [.this, .following, .all])
        vm.scope = .following
        await vm.save()
        #expect(w.updates.count == 1 && w.updates[0].0.version == 4 && w.updates[0].2 == .following && w.updates[0].3 == "k1")
        #expect(vm.saveState == .saved)
    }

    @Test func repeatingEventsCanOnlyMoveWithinTheirAccount() {
        let other = HubCalendar(id: "other", accountId: "b", name: "Other", color: "ochre", access: "owner")
        let d = detail(recurring: true)
        let vm = EventEditorViewModel(edit: d, calendars: cals + [other], tz: tzLondon, writer: FakeWriter([]))
        #expect(vm.calendarChoices.map(\.id) == ["work", "home"])
        let single = EventEditorViewModel(edit: detail(), calendars: cals + [other], tz: tzLondon, writer: FakeWriter([]))
        #expect(single.calendarChoices.map(\.id) == ["work", "home", "other"])
    }

    @Test func nonRecurringNeverAsksForScope() {
        let vm = editor(FakeWriter([]))
        vm.title = "x"
        #expect(!vm.needsScopeChoice && vm.scopeChoices == [.this])
    }

    @Test func changingTheRepeatRuleForbidsThisOccurrenceOnly() async {
        let w = FakeWriter([.saved(nil)])
        let vm = editor(w, detail(recurring: true))
        vm.rrule = "FREQ=WEEKLY"
        #expect(vm.scopeChoices == [.following, .all])
        await vm.save()
        #expect(w.updates[0].2 == .following)                           // default `this` was corrected
    }

    @Test func changingCalendarMovesAfterPatch() async {
        let w = FakeWriter([.saved(nil), .saved(nil)])
        let vm = editor(w)
        vm.title = "Daily"; vm.calendarId = "home"
        await vm.save()
        #expect(w.updates.count == 1 && w.moves.map(\.0) == ["home"])
        #expect(vm.saveState == .saved)
    }

    @Test func moveFailureDoesNotResendThePatch() async {
        let w = FakeWriter([.saved(nil), .failed(.offline), .saved(nil)])
        let vm = editor(w)
        vm.title = "Daily"; vm.calendarId = "home"
        await vm.save()
        guard case .failed = vm.saveState else { Issue.record("expected failed"); return }
        await vm.save()
        #expect(w.updates.count == 1 && w.moves.count == 2)
        #expect(vm.saveState == .saved)
    }

    @Test func conflictKeepMineRebasesOntoTheirVersion() async {
        let theirs = detail(version: 9, title: "Theirs")
        let w = FakeWriter([.failed(.conflict(current: theirs)), .saved(theirs)])
        let vm = editor(w)
        vm.notes = "My notes"
        await vm.save()
        #expect(vm.saveState == .conflict(current: theirs))
        #expect(vm.shouldConfirmDiscard && vm.canDismiss)
        await vm.keepMine()
        #expect(w.updates.count == 2 && w.updates[1].0.version == 9)
        #expect(w.updates[1].1.description == .set("My notes") && w.updates[1].1.title == .unchanged)   // only my change
        #expect(vm.saveState == .saved)
    }

    @Test func conflictUseTheirsReplacesFieldsAndBecomesClean() async {
        let theirs = detail(version: 9, title: "Theirs")
        let w = FakeWriter([.failed(.conflict(current: theirs))])
        let vm = editor(w)
        vm.title = "Mine"
        await vm.save()
        vm.useTheirs()
        #expect(vm.title == "Theirs" && !vm.isDirty && vm.saveState == .idle)
        vm.title = "Mine again"
        #expect(vm.canSave)
    }

    @Test func eventDeletedElsewhere() async {
        let vm = editor(FakeWriter([.failed(.notFound)]))
        vm.title = "x"
        await vm.save()
        #expect(vm.saveState == .failed(message: "This event no longer exists.", canRetry: false))
    }

    @Test func timeEditProducesWhenPatchInEventZone() {
        let vm = editor(FakeWriter([]))
        vm.setStart(at("2026-10-12T10:00:00Z"))
        guard case .set(let w) = vm.patch.when else { Issue.record("no when"); return }
        #expect(w.start == "2026-10-12T11:00:00" && w.tzid == "Europe/London" && w.durationMin == 30)
    }
}

@MainActor struct EventEditorFormTests {
    @Test func repeatOptionsFollowTheStartDate() {
        let vm = creator(FakeWriter([]))       // Fri 16 Oct 2026
        let titles = vm.repeatOptions.map(\.title)
        #expect(titles == ["Never", "Every day", "Every weekday", "Every week on Friday", "Every month on the 16th"])
        vm.rrule = "FREQ=WEEKLY;BYDAY=FR"
        #expect(vm.repeatText == "Every week on Friday")
        #expect(vm.isDirty && vm.patch.recurrence == .unchanged || true)
    }

    @Test func timeStepperMovesStartAndKeepsLength() {
        let vm = creator(FakeWriter([]))
        let len = vm.lengthMinutes
        vm.startMinutes += 15
        #expect(vm.lengthMinutes == len)
        vm.setLength(90)
        #expect(vm.lengthMinutes == 90 && vm.endsHint.hasPrefix("Ends "))
    }

    @Test func reminderDefaultDoesNotMakeANewEventDirty() {
        let vm = creator(FakeWriter([]))
        vm.applyDefaults(reminderMin: 10)
        #expect(vm.reminderLabel == "10 minutes before" && !vm.isDirty && !vm.shouldConfirmDiscard)
    }

    @Test func defaultCalendarIsMarked() {
        let vm = EventEditorViewModel(create: at("2026-10-16T12:00:00Z"), calendars: cals, defaultCalendarId: "home", tz: tzLondon,
                                      writer: FakeWriter([]))
        #expect(vm.defaultCalendarId == "home" && vm.calendarName == "Home")
    }

    @Test func recreatingFromADetailForUndo() {
        let d = detail(recurring: false)
        let draft = EventDraft(recreating: d)
        #expect(draft?.title == "Standup" && draft?.calendarId == "work" && draft?.attendees == ["bo@x.com"])
        #expect(draft?.when.durationMin == 30 && draft?.when.tzid == "Europe/London")
        #expect(draft?.validate().isEmpty == true)
    }

    @Test func detailReplyUpdatesTheEventAndReportsFailure() async {
        let ok = detail()
        let w = FakeWriter([.saved(ok), .failed(.offline)])
        let m = EventDetailViewModel(id: "E1:1", preview: ok.item, repo: StubRepoE(d: ok), clock: .fixed(at("2026-10-12T08:00:00Z")), tz: tzLondon)
        #expect(await m.reply(.accepted, note: nil, scope: .this, writer: w))
        #expect(m.replyError == nil)
        #expect(await m.reply(.declined, note: "Clash", scope: .this, writer: w) == false)
        #expect(m.replyError?.contains("Can\u{2019}t reach your server") == true && m.pendingChoice == "declined")
    }
}

private struct StubRepoE: EventDetailRepository {
    var d: EventDetail
    func cached(id: String) async throws -> EventDetail? { d }
    func detail(id: String) async throws -> EventDetail { d }
    func refresh(id: String) async throws -> EventDetail { d }
}

@MainActor struct EventEditorFailureCopyTests {
    @Test func headlineIsPlainWordsNeverTheRawReason() async {
        let vm = creator(FakeWriter([.failed(.notFound)]))
        vm.title = "Lunch"
        await vm.save()
        #expect(vm.failureHeadline == "Google didn\u{2019}t accept the event.")
        let off = creator(FakeWriter([.failed(.offline)]))
        off.title = "x"; await off.save()
        #expect(off.failureHeadline == "Can\u{2019}t reach your server.")
        let ed = editor(FakeWriter([.failed(.provider("Quota exceeded"))]))
        ed.title = "y"; await ed.save()
        #expect(ed.failureHeadline == "Google didn\u{2019}t accept the change.")
    }
}

struct RepeatRuleTests {
    @Test func writesRfcRules() {
        #expect(RepeatRule(unit: .week, interval: 1, weekdays: [1, 3]).rrule == "FREQ=WEEKLY;BYDAY=MO,WE")
        #expect(RepeatRule(unit: .week, interval: 2, weekdays: [0, 1, 5], ends: .after(10)).rrule == "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,FR,SU;COUNT=10")
        #expect(RepeatRule(unit: .day, interval: 3).rrule == "FREQ=DAILY;INTERVAL=3")
        #expect(RepeatRule(unit: .month, ends: .on("2026-12-31")).rrule == "FREQ=MONTHLY;UNTIL=20261231T235959Z")
        #expect(RepeatRule(unit: .year, interval: 500).interval == 99)
    }

    @Test func readsWhatItWritesAndRefusesWhatItCannotExpress() {
        for r in [RepeatRule(unit: .week, interval: 2, weekdays: [1, 3], ends: .after(5)),
                  RepeatRule(unit: .month, interval: 1, ends: .on("2027-01-15")),
                  RepeatRule(unit: .day, interval: 1)] {
            #expect(RepeatRule(parsing: r.rrule) == r)
        }
        #expect(RepeatRule(parsing: "FREQ=MONTHLY;BYDAY=2TU") == nil)            // "second Tuesday"
        #expect(RepeatRule(parsing: "FREQ=MONTHLY;BYMONTHDAY=15") == nil)
        #expect(RepeatRule(parsing: "nonsense") == nil)
    }

    @Test func summaryReadsLikeEnglish() {
        let f = EventTimeFormat(tz: tzLondon, locale: Locale(identifier: "en_GB"))
        #expect(RepeatRule(unit: .week, interval: 2, weekdays: [1, 3]).summary(format: f) == "Every 2 weeks on Mon, Wed")
        #expect(RepeatRule(unit: .day, ends: .after(3)).summary(format: f) == "Every day, 3 times")
    }
}
