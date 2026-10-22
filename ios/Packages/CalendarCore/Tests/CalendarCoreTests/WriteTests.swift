import Foundation
import GRDB
import Testing
@testable import CalendarCore

// MARK: Helpers

private func eventJSON(_ id: String, version: Int, title: String) -> String {
    #"{"id":"\#(id)","eventId":"\#(id.split(separator: ":")[0])","version":\#(version),"calendarId":"cal1","accountId":"acc1","color":"cobalt","title":"\#(title)","allDay":false,"start":"2026-10-12T09:00:00Z","end":"2026-10-12T10:00:00Z","tzid":"Europe/London","status":"confirmed","busy":true,"recurring":false,"canEdit":true,"canRSVP":false,"canAddConference":false,"attendees":[]}"#
}

private func detail(_ id: String, version: Int64, title: String, start: String = "2026-10-12T09:00:00Z", end: String = "2026-10-12T10:00:00Z") -> EventDetail {
    EventDetail(item: AgendaItem(id: id, eventId: String(id.split(separator: ":")[0]), version: version, calendarId: "cal1", accountId: "acc1",
                                 color: "cobalt", title: title, allDay: false, start: iso(start), end: iso(end), tzid: "Europe/London", canEdit: true))
}

private func errorBody(_ code: String, _ message: String, field: String? = nil, current: String? = nil) -> Data {
    var s = #"{"error":{"code":"\#(code)","message":"\#(message)""#
    if let field { s += #","field":"\#(field)""# }
    if let current { s += #","current":\#(current)"# }
    return Data((s + "}}").utf8)
}

final class WriteStub: URLProtocol, @unchecked Sendable {
    nonisolated(unsafe) static var handler: (@Sendable (URLRequest) -> (Int, [String: String], Data))?
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let (code, headers, body) = Self.handler!(request)
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: request.url!, statusCode: code, httpVersion: nil, headerFields: headers)!, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: body)
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

private func json(_ e: some Encodable) -> [String: Any] {
    (try? JSONSerialization.jsonObject(with: JSONEncoder().encode(e)) as? [String: Any]) ?? [:]
}

// MARK: Models + validation

@Suite struct WriteModelTests {
    @Test func timedWhenUsesLocalWallTime() {
        let w = EventWhen.timed(start: iso("2026-10-16T12:00:00Z"), end: iso("2026-10-16T13:30:00Z"), tz: london)!
        #expect(w.start == "2026-10-16T13:00:00" && w.tzid == "Europe/London" && w.durationMin == 90)
        #expect(w.validate().isEmpty)
        #expect(EventWhen.timed(start: iso("2026-10-16T12:00:00Z"), end: iso("2026-10-16T12:00:00Z"), tz: london) == nil)
        #expect(EventWhen.timed(start: iso("2026-10-16T12:00:00Z"), end: iso("2026-10-16T11:00:00Z"), tz: london) == nil)
    }

    @Test func nonexistentWallTimeInSpringGapIsRejected() {
        let gap = EventWhen(allDay: false, start: "2026-03-29T01:30:00", tzid: "Europe/London", durationMin: 30)
        #expect(gap.validate().first?.field == "when.start")
        let ok = EventWhen(allDay: false, start: "2026-03-29T02:30:00", tzid: "Europe/London", durationMin: 30)
        #expect(ok.validate().isEmpty)
        // 25-hour day: 01:30 exists (twice); the first occurrence is used.
        let fall = EventWhen(allDay: false, start: "2026-10-25T01:30:00", tzid: "Europe/London", durationMin: 30)
        #expect(fall.validate().isEmpty)
    }

    @Test func whenRules() {
        #expect(EventWhen(allDay: false, start: "2026-10-16T13:00:00", tzid: "Mars/Base", durationMin: 30).validate().first?.field == "when.tzid")
        #expect(EventWhen(allDay: false, start: "nonsense", tzid: "Europe/London", durationMin: 30).validate().first?.field == "when.start")
        #expect(EventWhen(allDay: false, start: "2026-10-16T13:00:00", tzid: "Europe/London", durationMin: 0).validate().first?.field == "when.durationMin")
        #expect(EventWhen.allDay(first: "2026-10-16", last: "2026-10-18").endDate == "2026-10-19")   // exclusive
        #expect(EventWhen.allDay(first: "2026-10-16", last: "2026-10-14").endDate == "2026-10-17")   // clamped
        #expect(EventWhen(allDay: true, date: "2026-10-16", endDate: "2026-10-16").validate().first?.field == "when.endDate")
        #expect(EventWhen(allDay: true, start: "2026-10-16T13:00:00", date: "2026-10-16", endDate: "2026-10-17").validate().first?.field == "when")
        #expect(EventWhen(allDay: true, date: "2026-02-30", endDate: "2026-03-01").validate().first?.field == "when.date")
        #expect(EventWhen(allDay: false, start: "2026-10-16T13:00:00", tzid: "Europe/London", durationMin: 30, date: "2026-10-16").validate().first?.field == "when")
    }

    private func draft(_ title: String = "Lunch", _ mutate: (inout EventDraft) -> Void = { _ in }) -> EventDraft {
        var d = EventDraft(calendarId: "cal1", title: title, when: EventWhen(allDay: false, start: "2026-10-16T13:00:00", tzid: "Europe/London", durationMin: 60))
        mutate(&d); return d
    }

    @Test func draftValidation() {
        #expect(draft().validate().isEmpty)
        #expect(draft("  \n").validate().first?.field == "title")
        #expect(draft { $0.calendarId = "" }.validate().first?.field == "calendarId")
        #expect(draft { $0.attendees = ["a@b.co", "nope"] }.validate().first?.field == "attendees")
        #expect(draft { $0.attendees = ["a@b.co", "A@b.co"] }.validate().first?.message == "Someone is on the list twice.")
        #expect(draft { $0.recurrence = Recurrence(rrule: "RRULE:FREQ=DAILY") }.validate().first?.field == "recurrence")
        #expect(draft { $0.recurrence = Recurrence(rrule: "FREQ=WEEKLY;BYDAY=TU,TH") }.validate().isEmpty)
        #expect(draft { $0.reminderMin = -1 }.validate().first?.field == "reminderMin")
        #expect(draft { $0.reminderMin = 40_321 }.validate().first?.field == "reminderMin")
    }

    @Test func draftEncodingOmitsEmptiesAndTrims() {
        let j = json(draft("  Lunch  ") { $0.location = ""; $0.description = nil; $0.recurrence = Recurrence(rrule: "FREQ=DAILY", summary: "Daily") })
        #expect(j["title"] as? String == "Lunch")
        #expect(j["location"] == nil && j["description"] == nil && j["attendees"] == nil && j["reminderMin"] == nil)
        #expect((j["recurrence"] as? [String: Any])?.keys.sorted() == ["rrule"])     // summary is response-only
        #expect((j["when"] as? [String: Any])?["durationMin"] as? Int == 60)
    }

    @Test func draftSurvivesAJsonRoundTripEvenWhenFieldsAreEmpty() throws {
        let d = draft("Lunch")
        let back = try JSONDecoder().decode(EventDraft.self, from: JSONEncoder().encode(d))
        #expect(back.title == "Lunch" && back.attendees.isEmpty && back.location == nil && back.when == d.when)
    }

    @Test func patchEncodesOmittedNullAndValues() {
        var p = EventPatch()
        #expect(json(p).isEmpty && p.isEmpty)
        p.title = .set(" New ")
        p.location = .clear
        p.reminderMin = .clear
        p.attendees = .set(["a@b.co"])
        let j = json(p)
        #expect(j["title"] as? String == "New")
        #expect(j["location"] is NSNull && j["reminderMin"] is NSNull)
        #expect(j["attendees"] as? [String] == ["a@b.co"])
        #expect(j["description"] == nil && j["when"] == nil)
    }

    @Test func patchValidation() {
        #expect(EventPatch().validate().first?.field == "patch")
        var p = EventPatch(); p.title = .clear
        #expect(p.validate().first?.field == "title")
        p = EventPatch(); p.when = .clear
        #expect(p.validate().first?.field == "when")
        p = EventPatch(); p.when = .set(EventWhen(allDay: true, date: "2026-10-16", endDate: "2026-10-16"))
        #expect(p.validate().first?.field == "when.endDate")
        p = EventPatch(); p.location = .clear
        #expect(p.validate().isEmpty)
    }

    @Test func classifyErrors() {
        func api(_ status: Int, _ code: String, field: String? = nil, current: EventDetail? = nil) -> APIClientError {
            .server(status: status, APIError(error: .init(code: code, message: "m", field: field, current: current)))
        }
        let cur = detail("E1:1", version: 9, title: "Theirs")
        #expect(WriteFailure.classify(api(412, "precondition_failed", current: cur)) == .conflict(current: cur))
        #expect(WriteFailure.classify(api(409, "conflict")) == .conflict(current: nil))
        #expect(WriteFailure.classify(api(400, "validation", field: "title")) == .validation(field: "title", message: "m"))
        #expect(WriteFailure.classify(api(502, "provider_error")) == .provider("m"))
        #expect(WriteFailure.classify(api(401, "needs_reauth")) == .needsReauth)
        #expect(WriteFailure.classify(api(429, "rate_limited")) == .rateLimited)
        #expect(WriteFailure.classify(api(424, "needs_reauth")) == .needsReauth)
        #expect(WriteFailure.classify(api(422, "validation", field: "Idempotency-Key")) == .validation(field: "Idempotency-Key", message: "m"))
        #expect(WriteFailure.classify(APIClientError.transport("x")) == .offline)
        #expect(WriteFailure.classify(APIClientError.unauthorized(APIError(error: .init(code: "forbidden", message: "Read only")))) == .forbidden("Read only"))
        #expect(WriteFailure.classify(APIClientError.unauthorized(nil)) == .unauthorized)
        #expect(WriteFailure.classify(APIClientError.notFound(nil)) == .notFound)
        #expect(WriteFailure.offline.isRetryable && !WriteFailure.provider("x").isRetryable)
    }
}

// MARK: HTTP

@Suite(.serialized) struct WriteHTTPTests {
    func client() -> APIClient {
        let cfg = URLSessionConfiguration.ephemeral
        cfg.protocolClasses = [WriteStub.self]
        return APIClient(baseURL: URL(string: "https://hub.test")!, token: "cal_x", session: URLSession(configuration: cfg))
    }

    @Test func patchSendsKeyIfMatchScopeAndBody() async throws {
        WriteStub.handler = { req in
            #expect(req.httpMethod == "PATCH" && req.url?.path == "/v1/events/E1:100")
            #expect(req.value(forHTTPHeaderField: "Idempotency-Key") == "k1")
            #expect(req.value(forHTTPHeaderField: "If-Match") == "\"7\"")
            #expect(URLComponents(url: req.url!, resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == "scope" }?.value == "following")
            let b = try? JSONSerialization.jsonObject(with: APIClientTests.bodyData(req)) as? [String: Any]
            #expect(b?["title"] as? String == "New" && b?["location"] is NSNull)
            return (200, [:], Data(eventJSON("E1:100", version: 8, title: "New").utf8))
        }
        var p = EventPatch(); p.title = .set("New"); p.location = .clear
        let d = try await client().patchEvent(id: "E1:100", patch: p, version: 7, scope: .following, key: "k1")
        #expect(d.item.version == 8)
    }

    @Test func scopeThisIsOmittedAndDeleteAccepts204() async throws {
        WriteStub.handler = { req in
            #expect(req.httpMethod == "DELETE")
            #expect(req.url?.query == nil)
            #expect(req.value(forHTTPHeaderField: "Idempotency-Key") == "k2")
            return (204, [:], Data())
        }
        try await client().deleteEvent(id: "E1:100", version: 3, scope: .this, key: "k2")
    }

    @Test func createRsvpMeetMoveReminder() async throws {
        WriteStub.handler = { req in
            let path = req.url?.path ?? ""
            let b = try? JSONSerialization.jsonObject(with: APIClientTests.bodyData(req)) as? [String: Any]
            switch (req.httpMethod ?? "", path) {
            case ("POST", "/v1/events"):
                #expect(req.value(forHTTPHeaderField: "Idempotency-Key") == "kc" && req.value(forHTTPHeaderField: "If-Match") == nil)
                #expect(b?["calendarId"] as? String == "cal1")
                return (201, [:], Data(eventJSON("N1:1", version: 1, title: "Lunch").utf8))
            case ("POST", "/v1/events/E1:1/rsvp"):
                #expect(b?["response"] as? String == "tentative" && b?["scope"] as? String == "all")
                #expect(b?["note"] == nil)
            case ("POST", "/v1/events/E1:1/meet"): #expect(req.value(forHTTPHeaderField: "Idempotency-Key") == "km")
            case ("POST", "/v1/events/E1:1/move"): #expect(b?["calendarId"] as? String == "cal2")
            case ("PUT", "/v1/events/E1:1/reminder"): #expect(b?["minutesBefore"] is NSNull)
            default: Issue.record("unexpected \(req.httpMethod ?? "") \(path)")
            }
            return (200, [:], Data(eventJSON("E1:1", version: 2, title: "x").utf8))
        }
        let c = client()
        let draft = EventDraft(calendarId: "cal1", title: "Lunch", when: EventWhen(allDay: false, start: "2026-10-16T13:00:00", tzid: "Europe/London", durationMin: 60))
        #expect(try await c.createEvent(draft, key: "kc").item.id == "N1:1")
        _ = try await c.rsvp(id: "E1:1", response: .tentative, note: nil, scope: .all, key: "kr")
        _ = try await c.addMeet(id: "E1:1", key: "km")
        _ = try await c.moveEvent(id: "E1:1", calendarId: "cal2", key: "kv")
        _ = try await c.setReminder(id: "E1:1", minutesBefore: nil)
    }

    @Test func conflictBodyCarriesCurrentEvent() async throws {
        WriteStub.handler = { _ in
            (412, [:], errorBody("precondition_failed", "Changed elsewhere", current: eventJSON("E1:100", version: 9, title: "Theirs")))
        }
        var p = EventPatch(); p.title = .set("Mine")
        do {
            _ = try await client().patchEvent(id: "E1:100", patch: p, version: 7, scope: .this, key: "k")
            Issue.record("expected throw")
        } catch {
            guard case .conflict(let cur?) = WriteFailure.classify(error) else { Issue.record("not a conflict: \(error)"); return }
            #expect(cur.item.title == "Theirs" && cur.item.version == 9)
        }
    }

    @Test func malformedCurrentDoesNotHideTheError() async throws {
        WriteStub.handler = { _ in (409, [:], errorBody("conflict", "Clash", current: #"{"nonsense":true}"#)) }
        do { try await client().deleteEvent(id: "E1:1", version: 1, scope: .this, key: "k") }
        catch { #expect(WriteFailure.classify(error) == .conflict(current: nil)) }
    }
}

// MARK: Coordinator

private final class FakeWrite: HubWriteAPI, @unchecked Sendable {
    enum Call: Equatable { case create(String), patch(String, Int64?, EventScope, String), delete(String, EventScope, String), rsvp(String, String), meet(String), move(String, String), reminder(String, Int?) }
    private let lock = NSLock()
    private var _calls: [Call] = []
    private var _script: [@Sendable () async throws -> EventDetail?]
    var calls: [Call] { lock.withLock { _calls } }
    init(_ script: [@Sendable () async throws -> EventDetail?]) { _script = script }

    private func next(_ c: Call) async throws -> EventDetail? {
        let step: (@Sendable () async throws -> EventDetail?)? = lock.withLock { _calls.append(c); return _script.isEmpty ? nil : _script.removeFirst() }
        return try await step?()
    }
    private func need(_ d: EventDetail?) throws -> EventDetail { guard let d else { throw APIClientError.invalidResponse }; return d }

    func createEvent(_ draft: EventDraft, key: String) async throws -> EventDetail { try need(await next(.create(key))) }
    func patchEvent(id: String, patch: EventPatch, version: Int64?, scope: EventScope, key: String) async throws -> EventDetail { try need(await next(.patch(id, version, scope, key))) }
    func deleteEvent(id: String, version: Int64?, scope: EventScope, key: String) async throws { _ = try await next(.delete(id, scope, key)) }
    func rsvp(id: String, response: RSVPResponse, note: String?, scope: EventScope, key: String) async throws -> EventDetail { try need(await next(.rsvp(id, response.rawValue))) }
    func addMeet(id: String, key: String) async throws -> EventDetail { try need(await next(.meet(id))) }
    func moveEvent(id: String, calendarId: String, key: String) async throws -> EventDetail { try need(await next(.move(id, calendarId))) }
    func setReminder(id: String, minutesBefore: Int?) async throws -> EventDetail { try need(await next(.reminder(id, minutesBefore))) }
}

private actor WriteCounter { var n = 0; func bump() { n += 1 }; var value: Int { n } }

@Suite struct WriteCoordinatorTests {
    let series = [
        timed("E1:1", "Standup", "2026-10-12T09:00:00Z", "2026-10-12T09:15:00Z", version: 4),
        timed("E1:2", "Standup", "2026-10-13T09:00:00Z", "2026-10-13T09:15:00Z", version: 4),
        timed("E1:3", "Standup", "2026-10-14T09:00:00Z", "2026-10-14T09:15:00Z", version: 4),
    ]

    fileprivate func make(_ script: [@Sendable () async throws -> EventDetail?], items: [AgendaItem]? = nil) async throws -> (WriteCoordinator, AppDatabase, FakeWrite, WriteCounter, WriteCounter) {
        let db = try AppDatabase.inMemory()
        _ = try await db.apply(SyncSnapshot(
            agenda: .init(items: items ?? series, rev: 1, etag: "e", from: iso("2026-09-27T00:00:00Z"), to: iso("2027-01-11T00:00:00Z")),
            accounts: [testAccount()], calendars: [testCalendar(), HubCalendar(id: "cal2", accountId: "acc1", name: "Home", color: "rose", tzid: "Europe/London")],
            settings: londonSettings, syncedAt: iso("2026-10-11T10:00:00Z")))
        let fake = FakeWrite(script)
        let refreshes = WriteCounter(), sleeps = WriteCounter()
        let co = WriteCoordinator(api: fake, db: db, refresh: { await refreshes.bump() }, sleep: { _ in await sleeps.bump() },
                                  makeKey: { "key-fixed" }, retryDelays: [0.1, 0.2])
        return (co, db, fake, refreshes, sleeps)
    }

    func row(_ db: AppDatabase, _ id: String) async throws -> AgendaItem? {
        try await db.writer.read { try OccurrenceRecord.fetchOne($0, key: id)?.item }
    }

    func titlePatch(_ t: String) -> EventPatch { var p = EventPatch(); p.title = .set(t); return p }

    @Test func updateShowsOptimisticallyThenReconcilesWithServer() async throws {
        let db0 = try AppDatabase.inMemory(); _ = db0
        let holder = Holder()
        let (co, db, fake, refreshes, _) = try await make([{ [holder] in
            try await holder.peekTitle()          // read while the call is in flight
            return detail("E1:1", version: 5, title: "Standup (server)")
        }])
        await holder.set(db)
        let out = await co.update(series[0], patch: titlePatch("Daily"))
        guard case .saved(let d?) = out else { Issue.record("not saved: \(out)"); return }
        #expect(await holder.seenTitle == "Daily")
        #expect(d.item.version == 5)
        let r = try await row(db, "E1:1")
        #expect(r?.title == "Standup (server)" && r?.version == 5)
        #expect(try await row(db, "E1:2")?.title == "Standup")       // scope this: others untouched
        #expect(fake.calls == [.patch("E1:1", 4, .this, "key-fixed")])
        #expect(await refreshes.value == 1)
    }

    @Test func scopeAllAndFollowingTouchTheRightRows() async throws {
        let all = series
        #expect(OptimisticEdit.targets(scope: .this, item: series[1], in: all).map(\.id) == ["E1:2"])
        #expect(OptimisticEdit.targets(scope: .following, item: series[1], in: all).map(\.id) == ["E1:2", "E1:3"])
        #expect(OptimisticEdit.targets(scope: .all, item: series[1], in: all).map(\.id) == ["E1:1", "E1:2", "E1:3"])
    }

    @Test func timeEditShiftsWholeSeriesByTheSameDelta() {
        let w = EventWhen.timed(start: iso("2026-10-13T10:00:00Z"), end: iso("2026-10-13T10:30:00Z"), tz: london)!   // 11:00 local, +1h, 30 min
        var p = EventPatch(); p.when = .set(w)
        let moved = OptimisticEdit.apply(p, to: series[2], anchor: series[1], scope: .all)
        #expect(moved.start == iso("2026-10-14T10:00:00Z") && moved.end == iso("2026-10-14T10:30:00Z"))
        let single = OptimisticEdit.apply(p, to: series[1], anchor: series[1], scope: .this)
        #expect(single.start == iso("2026-10-13T10:00:00Z"))
    }

    @Test func allDayEditShifts() {
        let a = allDayItem("A:1", "Trip", "2026-10-20", "2026-10-22")
        var p = EventPatch(); p.when = .set(EventWhen.allDay(first: "2026-10-21", last: "2026-10-23"))
        let r = OptimisticEdit.apply(p, to: a, anchor: a, scope: .this)
        #expect(r.date == "2026-10-21" && r.endDate == "2026-10-24")
    }

    @Test func lostResponseRetriesWithSameKeyAndActsOnce() async throws {
        let (co, db, fake, _, sleeps) = try await make([
            { throw APIClientError.transport("timed out") },
            { detail("E1:1", version: 5, title: "Daily") },
        ])
        let out = await co.update(series[0], patch: titlePatch("Daily"), key: "my-key")
        #expect(out == .saved(detail("E1:1", version: 5, title: "Daily")))
        #expect(fake.calls == [.patch("E1:1", 4, .this, "my-key"), .patch("E1:1", 4, .this, "my-key")])   // same key twice
        let sleepCount = await sleeps.value
        #expect(sleepCount == 1)
        #expect(try await row(db, "E1:1")?.version == 5)
    }

    @Test func offlineAfterRetriesRollsBack() async throws {
        let boom: @Sendable () async throws -> EventDetail? = { throw APIClientError.transport("offline") }
        let (co, db, fake, _, sleeps) = try await make([boom, boom, boom, boom])
        let out = await co.update(series[0], patch: titlePatch("Daily"))
        #expect(out == .failed(.offline))
        #expect(fake.calls.count == 3)              // first try + 2 retries
        let sleepCount = await sleeps.value
        #expect(sleepCount == 2)
        #expect(try await row(db, "E1:1")?.title == "Standup")
    }

    @Test func providerErrorRollsBackWithoutRetry() async throws {
        let (co, db, fake, _, sleeps) = try await make([{ throw APIClientError.server(status: 502, APIError(error: .init(code: "provider_error", message: "Google is down"))) }])
        let out = await co.update(series[0], patch: titlePatch("Daily"))
        #expect(out == .failed(.provider("Google is down")))
        let sleepCount = await sleeps.value
        #expect(fake.calls.count == 1 && sleepCount == 0)
        #expect(try await row(db, "E1:1")?.title == "Standup")
    }

    @Test func conflictRollsBackAndAdoptsServerEvent() async throws {
        let theirs = detail("E1:1", version: 9, title: "Theirs")
        let (co, db, _, refreshes, _) = try await make([{ throw APIClientError.server(status: 412, APIError(error: .init(code: "precondition_failed", message: "Changed", current: theirs))) }])
        let out = await co.update(series[0], patch: titlePatch("Mine"))
        #expect(out == .failed(.conflict(current: theirs)))
        let r = try await row(db, "E1:1")
        #expect(r?.title == "Theirs" && r?.version == 9)
        let refreshCount = await refreshes.value
        #expect(refreshCount == 1)
    }

    @Test func rollbackLeavesRowsAlreadyReplacedByARefresh() async throws {
        let holder = Holder()
        let (co, db, _, _, _) = try await make([{ [holder] in
            try await holder.replace(timed("E1:1", "From refresh", "2026-10-12T09:00:00Z", "2026-10-12T09:15:00Z", version: 6))
            throw APIClientError.server(status: 502, APIError(error: .init(code: "provider_error", message: "no")))
        }])
        await holder.set(db)
        _ = await co.update(series[0], patch: titlePatch("Mine"))
        #expect(try await row(db, "E1:1")?.title == "From refresh")
    }

    @Test func deleteRemovesRowsThenRestoresOnFailure() async throws {
        let holder = Holder()
        let (co, db, fake, _, _) = try await make([{ [holder] in
            try await holder.peekIds()
            throw APIClientError.server(status: 502, APIError(error: .init(code: "provider_error", message: "no")))
        }])
        await holder.set(db)
        let out = await co.delete(series[1], scope: .following)
        let gone = await holder.seenIds
        #expect(gone == ["E1:1"])                    // E1:2 and E1:3 were hidden while the call ran
        #expect(out == .failed(.provider("no")))
        let r2 = try await row(db, "E1:2"), r3 = try await row(db, "E1:3")
        #expect(r2?.title == "Standup" && r3 != nil)
        #expect(fake.calls == [.delete("E1:2", .following, "key-fixed")])
    }

    @Test func deleteSuccessKeepsRowsGone() async throws {
        let (co, db, _, _, _) = try await make([{ nil }])
        #expect(await co.delete(series[0], scope: .this) == .saved(nil))
        let r1 = try await row(db, "E1:1"), r2 = try await row(db, "E1:2")
        #expect(r1 == nil && r2 != nil)
    }

    @Test func createShowsPlaceholderThenRealRow() async throws {
        let holder = Holder()
        let (co, db, _, _, _) = try await make([{ [holder] in
            try await holder.peekIds()
            return detail("N1:1", version: 1, title: "Lunch", start: "2026-10-16T12:00:00Z", end: "2026-10-16T13:00:00Z")
        }])
        await holder.set(db)
        let draft = EventDraft(calendarId: "cal2", title: "Lunch", when: EventWhen.timed(start: iso("2026-10-16T12:00:00Z"), end: iso("2026-10-16T13:00:00Z"), tz: london)!)
        let out = await co.create(draft)
        guard case .saved = out else { Issue.record("not saved"); return }
        let during = await holder.seenIds
        #expect(during.contains("pending:key-fixed"))
        #expect(try await row(db, "pending:key-fixed") == nil)
        #expect(try await row(db, "N1:1")?.title == "Lunch")
    }

    @Test func createFailureRemovesPlaceholderAndInvalidDraftNeverCallsHub() async throws {
        let (co, db, fake, _, _) = try await make([{ throw APIClientError.server(status: 502, APIError(error: .init(code: "provider_error", message: "no"))) }])
        let draft = EventDraft(calendarId: "cal1", title: "Lunch", when: EventWhen.timed(start: iso("2026-10-16T12:00:00Z"), end: iso("2026-10-16T13:00:00Z"), tz: london)!)
        #expect(await co.create(draft) == .failed(.provider("no")))
        #expect(try await row(db, "pending:key-fixed") == nil)
        var bad = draft; bad.title = " "
        if case .failed(.validation(let f, _)) = await co.create(bad) { #expect(f == "title") } else { Issue.record("expected validation") }
        #expect(fake.calls.count == 1)
    }

    @Test func rsvpAndMoveAreOptimistic() async throws {
        let (co, db, _, _, _) = try await make([
            { detail("E1:1", version: 5, title: "Standup") },
            { throw APIClientError.server(status: 502, APIError(error: .init(code: "provider_error", message: "no"))) },
        ])
        _ = await co.rsvp(series[0], response: .declined)
        let r1 = try await row(db, "E1:1")
        #expect(r1?.version == 5)
        let out = await co.move(series[1], toCalendar: "cal2")
        #expect(out == .failed(.provider("no")))
        #expect(try await row(db, "E1:2")?.calendarId == "cal1")      // rolled back
    }

    @Test func rsvpToFollowingIsRejectedLocally() async throws {
        let (co, _, fake, _, _) = try await make([])
        if case .failed(.validation(let f, _)) = await co.rsvp(series[0], response: .accepted, scope: .following) { #expect(f == "scope") } else { Issue.record("expected validation") }
        #expect(fake.calls.isEmpty)
    }

    @Test func reminderValidatesRange() async throws {
        let (co, _, fake, _, _) = try await make([{ detail("E1:1", version: 5, title: "Standup") }])
        if case .failed(.validation) = await co.setReminder(series[0], minutesBefore: 99_999) {} else { Issue.record("expected validation") }
        #expect(fake.calls.isEmpty)
        #expect(await co.setReminder(series[0], minutesBefore: nil) == .saved(detail("E1:1", version: 5, title: "Standup")))
        #expect(fake.calls == [.reminder("E1:1", nil)])
    }
}

/// Lets a scripted fake hub peek at / change the DB while a write is in flight.
private actor Holder {
    var db: AppDatabase?
    func set(_ d: AppDatabase) { db = d }
    var seenTitle: String?
    var seenIds: [String] = []
    func peekTitle() throws { seenTitle = try db?.writer.read { try OccurrenceRecord.fetchOne($0, key: "E1:1")?.title } }
    func peekIds() throws { seenIds = try db?.writer.read { try OccurrenceRecord.order(Column("id")).fetchAll($0).map(\.id) } ?? [] }
    func replace(_ item: AgendaItem) throws { try db?.writer.write { try OccurrenceRecord(item, calendar: DayKey.calendar(london)).save($0) } }
}

@Suite struct WriteModeTests {
    @Test func confirmedModeTouchesNothingLocallyUntilTheHubAnswers() async throws {
        let holder = Holder()
        let series = [timed("E1:1", "Standup", "2026-10-12T09:00:00Z", "2026-10-12T09:15:00Z", version: 4)]
        let db = try AppDatabase.inMemory()
        _ = try await db.apply(SyncSnapshot(
            agenda: .init(items: series, rev: 1, etag: "e", from: iso("2026-09-27T00:00:00Z"), to: iso("2027-01-11T00:00:00Z")),
            accounts: [testAccount()], calendars: [testCalendar()], settings: londonSettings, syncedAt: iso("2026-10-11T10:00:00Z")))
        await holder.set(db)
        let fake = FakeWrite([{ [holder] in
            try await holder.peekTitle()
            return detail("E1:1", version: 5, title: "Daily")
        }])
        let co = WriteCoordinator(api: fake, db: db, refresh: {}, sleep: { _ in }, makeKey: { "k" })
        var p = EventPatch(); p.title = .set("Daily")
        let out = await co.update(series[0], patch: p, mode: .confirmed)
        guard case .saved = out else { Issue.record("not saved"); return }
        #expect(await holder.seenTitle == "Standup")        // unchanged while waiting
        let stored = try await db.writer.read { try OccurrenceRecord.fetchOne($0, key: "E1:1")?.title }
        #expect(stored == "Daily")                           // server truth after confirmation
    }
}
