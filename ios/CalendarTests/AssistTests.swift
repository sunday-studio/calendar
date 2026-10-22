import CalendarCore
import Foundation
import Testing
@testable import FeatureAssist

private let london = TimeZone(identifier: "Europe/London")!
private func at(_ s: String) -> Date { (try? Date.ISO8601FormatStyle().parse(s)) ?? Date(timeIntervalSince1970: 0) }

// MARK: Fakes

private final class FakeAssist: HubAssistAPI, @unchecked Sendable {
    private let lock = NSLock()
    var drafts: [String: QuickAddDraft] = [:]
    var error: Error?
    var delays: [String: Duration] = [:]
    var freeResults: [FreeTimeResult] = []
    var freeError: Error?
    private(set) var texts: [String] = []
    private(set) var freeRequests: [FreeTimeRequest] = []

    func quickAdd(text: String, now: Date?, tzid: String?) async throws -> QuickAddDraft {
        lock.withLock { texts.append(text) }
        if let d = delays[text] { try await Task.sleep(for: d) }
        if let error { throw error }
        return lock.withLock { drafts[text] } ?? draft(title: text)
    }
    func freeTime(_ r: FreeTimeRequest) async throws -> FreeTimeResult {
        lock.withLock { freeRequests.append(r) }
        if let freeError { throw freeError }
        return lock.withLock { freeResults.isEmpty ? FreeTimeResult(durationMin: r.durationMin, from: r.from, to: r.to, slots: []) : freeResults.removeFirst() }
    }
    func people(q: String) async throws -> [PersonHit] { [] }
}

private func draft(title: String, ambiguities: [DraftAmbiguity] = [], degraded: Bool? = nil, spans: [DraftSpan] = []) -> QuickAddDraft {
    QuickAddDraft(degraded: degraded,
                  event: EventDraft(calendarId: "work", title: title,
                                    when: EventWhen(allDay: false, start: "2026-10-16T13:00:00", tzid: "Europe/London", durationMin: 60),
                                    location: "Kōya", attendees: ["sam@x.com"]),
                  calendarName: "Work", people: [DraftPerson(name: "Sam", email: "sam@x.com")], ambiguities: ambiguities, spans: spans)
}

private let whichFriday = DraftAmbiguity(field: "when.date", question: "Which Friday?",
                                          options: [DraftOption(value: "2026-10-16", label: "This Friday, 16 Oct"), DraftOption(value: "2026-10-23", label: "Next Friday, 23 Oct")],
                                          default: 0)
private let whichSam = DraftAmbiguity(field: "attendees", question: "Which Sam?",
                                       options: [DraftOption(value: "sam.a@x.com", label: "Sam Adu"), DraftOption(value: "sam.b@x.com", label: "Sam Boateng")], default: -1)
private let whatTime = DraftAmbiguity(field: "when.time", question: "What time?", options: [], default: -1, optional: true)

private final class FakeWriter: EventWriting, @unchecked Sendable {
    private let lock = NSLock()
    var outcomes: [WriteOutcome] = []
    private(set) var creates: [(EventDraft, String?, WriteMode)] = []
    private(set) var deletes: [String] = []
    private func next() -> WriteOutcome { lock.withLock { outcomes.isEmpty ? .saved(created()) : outcomes.removeFirst() } }
    private func created() -> EventDetail {
        EventDetail(item: AgendaItem(id: "new1", eventId: "new1", calendarId: "work", accountId: "a", color: "cobalt", title: "x", allDay: false,
                                     start: at("2026-10-16T12:00:00Z"), end: at("2026-10-16T13:00:00Z"), tzid: "Europe/London", canEdit: true))
    }
    func create(_ draft: EventDraft, key: String?, mode: WriteMode) async -> WriteOutcome { lock.withLock { creates.append((draft, key, mode)) }; return next() }
    func update(_ item: AgendaItem, patch: EventPatch, scope: EventScope, key: String?, mode: WriteMode) async -> WriteOutcome { .saved(nil) }
    func delete(_ item: AgendaItem, scope: EventScope, key: String?, mode: WriteMode) async -> WriteOutcome { lock.withLock { deletes.append(item.id) }; return .saved(nil) }
    func rsvp(_ item: AgendaItem, response: RSVPResponse, note: String?, scope: EventScope, key: String?, mode: WriteMode) async -> WriteOutcome { .saved(nil) }
    func addMeet(_ item: AgendaItem, key: String?) async -> WriteOutcome { .saved(nil) }
    func move(_ item: AgendaItem, toCalendar calendarId: String, key: String?, mode: WriteMode) async -> WriteOutcome { .saved(nil) }
    func setReminder(_ item: AgendaItem, minutesBefore: Int?) async -> WriteOutcome { .saved(nil) }
}

@MainActor private func vm(_ api: FakeAssist, _ writer: FakeWriter = FakeWriter(), debounce: Duration = .zero) -> QuickAddViewModel {
    QuickAddViewModel(api: api, writer: writer, tz: london, clock: .fixed(at("2026-10-12T09:00:00Z")), debounce: debounce, sleep: { _ in })
}

// MARK: Quick add

@MainActor @Suite struct QuickAddTests {
    @Test func typingBurstParsesOnceAfterThePause() async {
        let api = FakeAssist(); let m = vm(api)
        m.text = "l"; m.text = "lu"; m.text = "lunch"
        await m.settle()
        #expect(api.texts == ["lunch"] && m.requestCount == 1)
        #expect(m.draft?.event.title == "lunch")
    }

    @Test func clearingTheFieldCancelsAndEmpties() async {
        let api = FakeAssist(); let m = vm(api)
        m.text = "lunch"; m.text = ""
        await m.settle()
        #expect(api.texts.isEmpty && m.parse == .empty)
    }

    @Test func slowAnswerForOldTextIsDropped() async {
        let api = FakeAssist()
        api.delays["lunch"] = .milliseconds(150)
        let m = vm(api)
        m.text = "lunch"
        while api.texts.isEmpty { await Task.yield() }        // first request is in flight
        m.text = "lunch fri"
        await m.settle()
        try? await Task.sleep(for: .milliseconds(250))          // old answer arrives late
        #expect(m.draft?.event.title == "lunch fri")
        #expect(m.draftText == "lunch fri")
    }

    @Test func underlinesFollowUtf16SpansAndIgnoreStaleOnes() async {
        let api = FakeAssist()
        // "Lunch with Sam fri 1pm": Sam = 11..14, fri 1pm = 15..22 (emoji-free here; offsets are UTF-16)
        api.drafts["Lunch with Sam fri 1pm"] = draft(title: "Lunch", spans: [DraftSpan(kind: "who", from: 11, to: 14), DraftSpan(kind: "when", from: 15, to: 22), DraftSpan(kind: "where", from: 90, to: 99)])
        let m = vm(api)
        m.text = "Lunch with Sam fri 1pm"
        await m.settle()
        let u = m.underlines
        #expect(u.count == 2)
        #expect(String(m.text[u[0].range]) == "Sam" && String(m.text[u[1].range]) == "fri 1pm")
        m.text += "!"                                           // text moved on: no underlines until re-parsed
        #expect(m.underlines.isEmpty)
    }

    @Test func spansCountUtf16UnitsNotCharacters() {
        let t = "🍝 Lunch Sam"                                    // the emoji is 2 UTF-16 units
        let u = DraftFormat.underlines(t, spans: [DraftSpan(kind: "who", from: 9, to: 12)])
        #expect(u.count == 1 && String(t[u[0].range]) == "Sam")
    }

    @Test func offlineHasNoPreviewAndCannotCreate() async {
        let api = FakeAssist(); api.error = APIClientError.transport("down")
        let m = vm(api); m.text = "lunch"
        await m.submit()
        #expect(m.isOffline && !m.canCreate && m.stage == .entry)
    }

    @Test func plainSentenceGoesStraightToPreviewThenCreatesConfirmed() async {
        let api = FakeAssist(); api.drafts["lunch"] = draft(title: "Lunch with Sam")
        let w = FakeWriter()
        let m = vm(api, w); m.text = "lunch"
        await m.submit()
        #expect(m.stage == .preview && m.canCreate)
        let rows = m.previewRows
        #expect(rows?.title == "Lunch with Sam" && rows?.place == "Kōya" && rows?.calendar == "Work" && rows?.people == "Sam")
        #expect(rows?.when?.contains("13:00 to 14:00") == true)
        await m.create()
        #expect(m.stage == .created && w.creates.count == 1 && w.creates[0].2 == .confirmed)
        #expect(m.createdToast.hasPrefix("Added Lunch with Sam, "))
        #expect(await m.undoCreate() && w.deletes == ["new1"])
    }

    @Test func ambiguitiesMustBeAnsweredAndAreAppliedToTheEvent() async {
        let api = FakeAssist()
        api.drafts["lunch"] = draft(title: "Lunch", ambiguities: [whichFriday, whichSam, whatTime])
        let w = FakeWriter()
        let m = vm(api, w); m.text = "lunch"
        await m.submit()
        #expect(m.stage == .ambiguity)
        // Friday is preselected (hub default 0); Sam has no default, so Continue waits.
        #expect(m.answers["when.date"] == "2026-10-16" && m.answers["attendees"] == nil)
        #expect(!m.canContinue && m.unanswered.map(\.field) == ["attendees"])
        #expect(m.optionalQuestions.map(\.field) == ["when.time"])     // optional never blocks
        m.answer("when.date", "2026-10-23"); m.answer("attendees", "sam.b@x.com")
        #expect(m.canContinue)
        m.continueAfterQuestions()
        #expect(m.stage == .preview)
        m.answer("when.time", "15:30")
        await m.create()
        let sent = w.creates[0].0
        #expect(sent.when.start == "2026-10-23T15:30:00" && sent.when.durationMin == 60)
        #expect(sent.attendees == ["sam@x.com", "sam.b@x.com"])
    }

    @Test func resolveCalendarTitleAndAllDay() {
        var d = draft(title: "Trip")
        d.event.when = EventWhen(allDay: true, date: "2026-10-16", endDate: "2026-10-18")
        let e = QuickAddViewModel.resolve(d, answers: ["calendarId": "home", "title": "Weekend away", "when.date": "2026-10-23"], tz: london)
        #expect(e.calendarId == "home" && e.title == "Weekend away")
        #expect(e.when.date == "2026-10-23" && e.when.endDate == "2026-10-25")          // keeps the two-day length
    }

    @Test func timeOnlyAnswerKeepsTheDateAndMakesItTimed() {
        var d = draft(title: "Call")
        d.event.when = EventWhen(allDay: true, date: "2026-10-16", endDate: "2026-10-17")
        let e = QuickAddViewModel.resolve(d, answers: ["when.time": "09:05"], tz: london)
        #expect(!e.when.allDay && e.when.start == "2026-10-16T09:05:00" && e.when.durationMin == 60 && e.when.tzid == "Europe/London")
    }

    @Test func degradedDraftShowsAlfatherDownButRulesDraftStaysUsable() async {
        let api = FakeAssist(); api.drafts["lunch"] = draft(title: "Lunch", degraded: true)
        let w = FakeWriter()
        let m = vm(api, w); m.text = "lunch"
        await m.submit()
        #expect(m.stage == .unreachable)
        #expect(m.resolved?.title == "Lunch" && m.canCreate)           // "New event form" gets this draft; Create works too
        await m.create()
        #expect(m.stage == .created)
    }

    @Test func degradedTryAgainParsesAgain() async {
        let api = FakeAssist(); api.drafts["lunch"] = draft(title: "Lunch", degraded: true)
        let m = vm(api); m.text = "lunch"
        await m.submit()
        api.drafts["lunch"] = draft(title: "Lunch")                     // alfather is back
        await m.retry()
        #expect(m.stage == .preview)
    }

    @Test func failedCreateKeepsTheDraftAndRetriesWithTheSameKey() async {
        let api = FakeAssist(); api.drafts["lunch"] = draft(title: "Lunch")
        let w = FakeWriter(); w.outcomes = [.failed(.offline)]
        let m = vm(api, w); m.text = "lunch"
        await m.submit()
        await m.create()
        #expect(m.stage == .preview && m.createError == "Can\u{2019}t reach the server. Nothing was created.")
        await m.create()
        #expect(m.stage == .created && w.creates.count == 2 && w.creates[0].1 == w.creates[1].1)
    }

    @Test func emptyTitleIsRefusedLocally() async {
        let api = FakeAssist(); api.drafts["x"] = draft(title: "  ")
        let w = FakeWriter()
        let m = vm(api, w); m.text = "x"
        await m.submit()
        #expect(!m.canCreate)
        await m.create()
        #expect(w.creates.isEmpty)
    }
}

// MARK: Free time

@MainActor @Suite struct FreeTimeTests {
    // Mon 12 Oct 2026, 09:00 UTC = 10:00 London
    private let now = at("2026-10-12T09:00:00Z")

    @Test func asksAreReadFromWords() {
        let a = FreeTimeAsk.parse("when am I free for 2 hours this week?", now: now, tz: london)
        #expect(a?.durationMin == 120 && a?.windowWords == "This week" && a?.dayPart == nil)
        #expect(a?.title == "Free for 2 hours")
        #expect(a.map { DayKey($0.to.addingTimeInterval(-1), in: london).rawValue } == "2026-10-18")      // Sunday ends the Mon-first week

        let t = FreeTimeAsk.parse("Free for an hour tomorrow afternoon", now: now, tz: london)
        #expect(t?.durationMin == 60 && t?.dayPart == .afternoon && t?.windowWords == "Tomorrow")
        #expect(t.map { DayKey($0.from, in: london).rawValue } == "2026-10-13")

        let f = FreeTimeAsk.parse("A 30 minute gap before Friday", now: now, tz: london)
        #expect(f?.durationMin == 30 && f?.windowWords == "Before Friday", "\(String(describing: f))")
        #expect(f.map { DayKey($0.to, in: london).rawValue } == "2026-10-16")

        let n = FreeTimeAsk.parse("two hours of focus next week", now: now, tz: london)
        #expect(n?.durationMin == 120 && n?.windowWords == "Next week")
        #expect(n.map { DayKey($0.from, in: london).rawValue } == "2026-10-19")

        let h = FreeTimeAsk.parse("1.5 hours", now: now, tz: london)
        #expect(h?.durationMin == 90 && h?.windowWords == "Next 7 days")
        #expect(FreeTimeAsk.parse("   ", now: now, tz: london) == nil)
    }

    private func make(_ api: FakeAssist, _ w: FakeWriter = FakeWriter(), text: String = "2 hours this week") -> FreeTimeViewModel {
        let cals = [HubCalendar(id: "ro", accountId: "a", name: "Holidays", color: "moss", access: "reader"),
                    HubCalendar(id: "work", accountId: "a", name: "Work", color: "cobalt", access: "owner", isPrimary: true)]
        return FreeTimeViewModel(api: api, writer: w, calendars: cals, defaultCalendarId: nil, tz: london, clock: .fixed(now), text: text)
    }

    private func slots() -> FreeTimeResult {
        FreeTimeResult(durationMin: 120, from: now, to: now.addingTimeInterval(86400 * 7), slots: [
            FreeSlot(start: at("2026-10-13T13:00:00Z"), end: at("2026-10-13T15:00:00Z"), reason: "Between two meetings", best: false),
            FreeSlot(start: at("2026-10-14T08:00:00Z"), end: at("2026-10-14T10:00:00Z"), reason: "Nothing else on", best: true)])
    }

    @Test func slotsPickTheBestFitAndWriteableCalendarsOnly() async {
        let api = FakeAssist(); api.freeResults = [slots()]
        let m = make(api)
        #expect(m.bookCalendarId == "work")
        await m.search()
        #expect(api.freeRequests.count == 1 && api.freeRequests[0].durationMin == 120 && !api.freeRequests[0].ignoreWorkingHours)
        guard case .slots(let r) = m.phase else { Issue.record("not slots: \(m.phase)"); return }
        #expect(r.slots.count == 2 && m.selected?.best == true)
        #expect(m.subLine(workingHours: WorkingHours(start: "09:00", end: "18:00", days: [1])) == "This week, inside 09:00 to 18:00. 2 slots found.")
    }

    @Test func bookingCreatesTheSlotThenUndoDeletes() async {
        let api = FakeAssist(); api.freeResults = [slots()]
        let w = FakeWriter()
        let m = make(api, w)
        await m.search()
        let best = m.selected!
        m.startBooking(best)
        m.bookTitle = "Deep work"
        #expect(m.canBook)
        await m.book()
        #expect(m.phase == .booked && w.creates.count == 1)
        let d = w.creates[0].0
        #expect(d.title == "Deep work" && d.calendarId == "work" && d.when.start == "2026-10-14T09:00:00" && d.when.durationMin == 120 && d.when.tzid == "Europe/London")
        #expect(m.bookedToast == "Booked Deep work, Wed 09:00")
        #expect(await m.undoBooking() && w.deletes == ["new1"])
    }

    @Test func bookingFailureStaysOnTheBookScreenWithAReason() async {
        let api = FakeAssist(); api.freeResults = [slots()]
        let w = FakeWriter(); w.outcomes = [.failed(.provider("Google is busy."))]
        let m = make(api, w)
        await m.search()
        m.startBooking(m.selected!)
        await m.book()
        if case .booking = m.phase {} else { Issue.record("left the book screen: \(m.phase)") }
        #expect(m.bookError == "Google is busy.")
    }

    @Test func blankTitleCannotBeBooked() async {
        let api = FakeAssist(); api.freeResults = [slots()]
        let m = make(api)
        await m.search()
        m.startBooking(m.selected!)
        m.bookTitle = "  "
        #expect(!m.canBook)
    }

    @Test func noGapShowsClosestAndIgnoringWorkingHoursReRuns() async {
        let api = FakeAssist()
        let none = FreeTimeResult(durationMin: 240, from: now, to: now.addingTimeInterval(86400 * 7), slots: [], closest: [
            ClosestSlot(start: at("2026-10-19T09:00:00Z"), end: at("2026-10-19T13:00:00Z"), durationMin: 240, kind: .later, reason: "Nothing else on"),
            ClosestSlot(start: at("2026-10-14T09:00:00Z"), end: at("2026-10-14T11:00:00Z"), durationMin: 120, kind: .shorter, reason: "After Standup")])
        api.freeResults = [none, slots()]
        let m = make(api, text: "4 hours this week")
        await m.search()
        guard case .none(let r) = m.phase else { Issue.record("not none: \(m.phase)"); return }
        #expect(r.closest.count == 2)
        await m.ignoreWorkingHours()
        #expect(api.freeRequests.count == 2 && api.freeRequests[1].ignoreWorkingHours && m.ignoredWorkingHours)
        if case .slots = m.phase {} else { Issue.record("expected slots after ignoring working hours") }
    }

    @Test func offlineGivesAPlainFailure() async {
        let api = FakeAssist(); api.freeError = APIClientError.transport("x")
        let m = make(api)
        await m.search()
        #expect(m.phase == .failed("Can\u{2019}t reach the server. Check Tailscale, then try again."))
    }
}

// MARK: Search

private final class FakeSearch: SearchAPI, @unchecked Sendable {
    private let lock = NSLock()
    var events: [EventDetail] = []
    var people: [PersonHit] = []
    var error: Error?
    private(set) var queries: [String] = []
    func events(q: String?, from: Date?, to: Date?, calendar: String?, cursor: String?, limit: Int?) async throws -> EventPage {
        lock.withLock { queries.append(q ?? "") }
        if let error { throw error }
        return EventPage(items: events)
    }
    func people(q: String) async throws -> [PersonHit] { people }
    func quickAdd(text: String, now: Date?, tzid: String?) async throws -> QuickAddDraft { throw APIClientError.invalidResponse }
    func freeTime(_ request: FreeTimeRequest) async throws -> FreeTimeResult { throw APIClientError.invalidResponse }
    // unused read API
    func agenda(from: Date, to: Date, etag: String?) async throws -> Conditional<AgendaResponse> { throw APIClientError.invalidResponse }
    func event(id: String) async throws -> EventDetail { throw APIClientError.invalidResponse }
    func accounts() async throws -> [Account] { [] }
    func calendars() async throws -> [HubCalendar] { [] }
    func patchCalendar(id: String, visible: Bool?, colorOverride: ColorOverrideChange) async throws -> HubCalendar { throw APIClientError.invalidResponse }
    func settings() async throws -> Settings { throw APIClientError.invalidResponse }
    func putSettings(_ settings: Settings) async throws -> Settings { throw APIClientError.invalidResponse }
    func status() async throws -> StatusDTO { throw APIClientError.invalidResponse }
    func syncRuns(limit: Int?, accountId: String?) async throws -> [SyncRun] { [] }
    func postSync(accountId: String?) async throws -> Int { 0 }
    func openStream(lastEventID: String?) -> AsyncThrowingStream<StreamEvent, Error> { AsyncThrowingStream { $0.finish() } }
}

private func ev(_ id: String, _ title: String, start: String, loc: String? = nil) -> EventDetail {
    let s = at(start)
    return EventDetail(item: AgendaItem(id: id, eventId: id, calendarId: "work", accountId: "a", color: "cobalt", title: title, allDay: false,
                                        start: s, end: s.addingTimeInterval(3600), tzid: "Europe/London", location: loc, canEdit: true))
}

@MainActor private func searchVM(_ api: FakeSearch, local: @escaping @Sendable (String) async -> [AgendaItem] = { _ in [] }) -> (SearchViewModel, () -> [String]) {
    nonisolated(unsafe) var store: [String] = []
    let recents = RecentSearches(load: { store }, save: { store = $0 })
    let m = SearchViewModel(api: api, tz: london, clock: .fixed(at("2026-10-12T09:00:00Z")), recents: recents, local: local, debounce: .zero, sleep: { _ in })
    return (m, { store })
}

@MainActor @Suite struct SearchTests {
    @Test func debouncedOneRequestPerPause() async {
        let api = FakeSearch()
        let (m, _) = searchVM(api)
        m.query = "s"; m.query = "sa"; m.query = "sam"
        await m.settle()
        #expect(api.queries == ["sam"] && m.requestCount == 1)
    }

    @Test func groupsComingUpAscendingAndPastDescendingWithPeopleFirst() async {
        let api = FakeSearch()
        api.events = [ev("p1", "Sam retro", start: "2026-10-02T10:00:00Z"), ev("f2", "Sam sync", start: "2026-10-22T10:00:00Z", loc: "Zoom"),
                      ev("f1", "Lunch with Sam", start: "2026-10-16T11:30:00Z", loc: "Kōya"), ev("p2", "Sam coffee", start: "2026-09-28T09:00:00Z")]
        api.people = [PersonHit(email: "sam@x.com", name: "Sam Boateng", events: 7)]
        let (m, _) = searchVM(api)
        m.query = "sam"
        await m.settle()
        #expect(m.phase == .results && m.people.count == 1)
        #expect(m.upcoming.map(\.id) == ["f1", "f2"] && m.past.map(\.id) == ["p1", "p2"])
        #expect(m.upcoming[0].dateWords == "Fri 16" && m.upcoming[0].sub == "12:30 \u{00B7} Kōya")
        #expect(m.past[0].dateWords == "Oct 2")
        #expect(!m.isEmptyResult)
    }

    @Test func emptyResultHasPlainMessage() async {
        let (m, _) = searchVM(FakeSearch())
        m.query = "samuel"
        await m.settle()
        #expect(m.isEmptyResult && m.emptyMessage == "No events or people match \u{201C}samuel\u{201D}.")
    }

    @Test func clearingTheQueryReturnsToRecents() async {
        let (m, _) = searchVM(FakeSearch())
        m.query = "sam"; await m.settle()
        m.query = ""
        #expect(m.phase == .idle && m.upcoming.isEmpty && m.people.isEmpty)
    }

    @Test func recentsAreDedupedCappedAndRemovable() async {
        let (m, store) = searchVM(FakeSearch())
        for t in ["Sam", "Design review", "sam", "Dentist", "a"] { m.query = t; await m.submit() }
        #expect(store() == ["Dentist", "sam", "Design review"])       // "a" is too short, "sam" replaced "Sam"
        m.removeRecent("sam")
        #expect(m.recents == ["Dentist", "Design review"])
        for i in 0..<12 { m.query = "term \(i)"; await m.submit() }
        #expect(store().count == RecentSearches.limit && store()[0] == "term 11")
    }

    @Test func offlineFallsBackToSavedEventsAndSaysSo() async {
        let api = FakeSearch(); api.error = APIClientError.transport("x")
        let saved = ev("s1", "Sam sync", start: "2026-10-22T10:00:00Z").item
        let (m, _) = searchVM(api, local: { _ in [saved] })
        m.query = "sam"
        await m.settle()
        #expect(m.savedOnly && m.upcoming.map(\.id) == ["s1"] && m.people.isEmpty)
    }

    @Test func tokenRejectedIsAPlainFailure() async {
        let api = FakeSearch(); api.error = APIClientError.unauthorized(nil)
        let (m, _) = searchVM(api)
        m.query = "sam"; await m.settle()
        #expect(m.phase == .failed("The server no longer accepts this phone\u{2019}s token."))
    }
}

// MARK: Voice

@MainActor private final class FakeSpeech: SpeechTranscribing {
    var access: SpeechAccess = .ready
    var update: (@MainActor (String, Double) -> Void)?
    private(set) var started = 0, stopped = 0
    func requestAccess() async -> SpeechAccess { access }
    func start(update: @escaping @MainActor (String, Double) -> Void) throws { started += 1; self.update = update }
    func stop() { stopped += 1 }
}

@MainActor @Suite struct VoiceTests {
    @Test func listensShowsWordsAndLevelsThenReturnsTheTranscript() async {
        let e = FakeSpeech(); let v = VoiceViewModel(engine: e)
        await v.begin()
        #expect(v.state == .listening && e.started == 1)
        e.update?("lunch with", 0.6); e.update?("lunch with sam friday", 0.3)
        #expect(v.transcript == "lunch with sam friday" && v.levels.count == 8 && v.levels.suffix(2) == [0.6, 0.3])
        #expect(v.finish() == "lunch with sam friday" && v.state == .idle && e.stopped == 1)
    }

    @Test func deniedAndUnavailableNeverStartTheMic() async {
        let e = FakeSpeech(); e.access = .denied
        let v = VoiceViewModel(engine: e)
        await v.begin()
        #expect(v.state == .denied && e.started == 0)
        e.access = .unavailable
        await v.begin()
        #expect(v.state == .unavailable && e.started == 0)
    }

    @Test func cancelKeepsNothing() async {
        let e = FakeSpeech(); let v = VoiceViewModel(engine: e)
        await v.begin(); e.update?("hello", 0.2)
        v.cancel()
        #expect(v.transcript.isEmpty && v.state == .idle)
    }
}

// MARK: Intents logic

@Suite struct IntentLogicTests {
    private func item(_ title: String, _ s: String, mins: Double = 30, join: Bool = true, loc: String? = nil, status: String = "confirmed") -> AgendaItem {
        let st = at(s)
        return AgendaItem(id: title, eventId: title, calendarId: "c", accountId: "a", color: "cobalt", title: title, allDay: false, start: st,
                          end: st.addingTimeInterval(mins * 60), tzid: "Europe/London", location: loc,
                          join: join ? Join(url: "https://meet.example/\(title)", kind: "meet") : nil, status: status, canEdit: true)
    }
    private let now = at("2026-10-12T09:00:00Z")

    @Test func joinPicksTheLiveOrImminentMeetingWithALink() {
        let items = [item("Past", "2026-10-12T07:00:00Z"), item("NoLink", "2026-10-12T09:05:00Z", join: false),
                     item("Later", "2026-10-12T11:00:00Z"), item("Soon", "2026-10-12T09:20:00Z"), item("Live", "2026-10-12T08:45:00Z")]
        #expect(NextMeeting.pick(items, now: now)?.title == "Live")
        #expect(NextMeeting.pick(Array(items.dropLast()), now: now)?.title == "Soon")
        #expect(NextMeeting.pick([item("Later", "2026-10-12T11:00:00Z")], now: now) == nil)
        #expect(NextMeeting.pick([item("Cancelled", "2026-10-12T09:10:00Z", status: "cancelled")], now: now) == nil)
    }

    @Test func spokenSentences() {
        let en = Locale(identifier: "en_GB")
        #expect(NextMeeting.sentence(next: nil, now: now, tz: london, locale: en) == "Nothing else on your calendar today.")
        #expect(NextMeeting.sentence(next: item("Design review", "2026-10-12T09:25:00Z", loc: "Room 3"), now: now, tz: london, locale: en)
                == "Next up is Design review at 10:25, in 25 minutes, at Room 3.")
        #expect(NextMeeting.sentence(next: item("Standup", "2026-10-12T08:45:00Z", mins: 30), now: now, tz: london, locale: en) == "Standup is on now until 10:15.")
        #expect(NextMeeting.untilWords(3600 * 2 + 600) == "in 2 hours 10 minutes" && NextMeeting.untilWords(30) == "in 1 minute")
    }

    @Test func addEventAsksOnlyFixedChoiceRequiredQuestions() {
        let d = draft(title: "Lunch", ambiguities: [whichFriday, whatTime, DraftAmbiguity(field: "when.time", question: "Morning or afternoon?", options: [], optional: false)])
        let req = IntentAdd.required(d)
        #expect(req.map(\.question) == ["Which Friday?", "Morning or afternoon?"])
        #expect(IntentAdd.choices(req[0]) == ["This Friday, 16 Oct", "Next Friday, 23 Oct"] && IntentAdd.choices(req[1]) == nil)
        var answers: [String: String] = [:]
        IntentAdd.apply(label: "Next Friday, 23 Oct", to: req[0], answers: &answers)
        #expect(answers == ["when.date": "2026-10-23"])
        #expect(IntentAdd.summary(QuickAddViewModel.resolve(d, answers: answers, tz: london), tz: london).hasPrefix("Lunch, "))
    }
}
