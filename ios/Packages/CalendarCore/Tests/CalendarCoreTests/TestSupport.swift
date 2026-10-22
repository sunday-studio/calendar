import Foundation
import GRDB
@testable import CalendarCore

func iso(_ s: String) -> Date {
    (try? Date.ISO8601FormatStyle().parse(s)) ?? Date(timeIntervalSince1970: 0)
}

let london = TimeZone(identifier: "Europe/London")!
let tokyo = TimeZone(identifier: "Asia/Tokyo")!
let losAngeles = TimeZone(identifier: "America/Los_Angeles")!

func timed(_ id: String, _ title: String, _ start: String, _ end: String, version: Int64 = 1,
           response: String? = "accepted", status: String = "confirmed") -> AgendaItem {
    AgendaItem(id: id, eventId: id.split(separator: ":").first.map(String.init) ?? id, version: version,
               calendarId: "cal1", accountId: "acc1", color: "cobalt", title: title, allDay: false,
               start: iso(start), end: iso(end), tzid: "Europe/London", myResponse: response, status: status)
}

func allDayItem(_ id: String, _ title: String, _ date: String, _ endDate: String) -> AgendaItem {
    AgendaItem(id: id, eventId: id, calendarId: "cal1", accountId: "acc1", color: "cobalt", title: title,
               allDay: true, date: date, endDate: endDate)
}

func testAccount(_ id: String = "acc1", status: String = "active") -> Account {
    Account(id: id, email: "\(id)@example.com", displayName: id, color: "cobalt", status: status)
}

func testCalendar(_ id: String = "cal1") -> HubCalendar {
    HubCalendar(id: id, accountId: "acc1", name: "Work", color: "cobalt", tzid: "Europe/London", isPrimary: true)
}

let londonSettings = Settings(userTzid: "Europe/London")

/// Scriptable hub. Thread-safe; records calls.
final class FakeHub: HubReadAPI, @unchecked Sendable {
    private let lock = NSLock()
    private var _agenda = AgendaResponse(rev: 1, from: Date(), to: Date(), items: [])
    private var _etag = "W/\"1-a\""
    private var _accounts = [testAccount()]
    private var _calendars = [testCalendar()]
    private var _settings = londonSettings
    private var _failure: Error?
    private var _agendaCalls: [(from: Date, to: Date, etag: String?)] = []
    private var _postSyncCalls = 0
    private var _streamIDs: [String?] = []
    private var _gate: (@Sendable () async -> Void)?
    private var _stream: (@Sendable (Int, String?) -> AsyncThrowingStream<StreamEvent, Error>)?

    var items: [AgendaItem] {
        get { lock.withLock { _agenda.items } }
        set { lock.withLock { _agenda.items = newValue } }
    }
    var rev: Int64 {
        get { lock.withLock { _agenda.rev } }
        set { lock.withLock { _agenda.rev = newValue; _etag = "W/\"\(newValue)-a\"" } }
    }
    var failure: Error? {
        get { lock.withLock { _failure } }
        set { lock.withLock { _failure = newValue } }
    }
    var settingsValue: Settings {
        get { lock.withLock { _settings } }
        set { lock.withLock { _settings = newValue } }
    }
    var accountList: [Account] {
        get { lock.withLock { _accounts } }
        set { lock.withLock { _accounts = newValue } }
    }
    var gate: (@Sendable () async -> Void)? {
        get { lock.withLock { _gate } }
        set { lock.withLock { _gate = newValue } }
    }
    var stream: (@Sendable (Int, String?) -> AsyncThrowingStream<StreamEvent, Error>)? {
        get { lock.withLock { _stream } }
        set { lock.withLock { _stream = newValue } }
    }
    var agendaCalls: [(from: Date, to: Date, etag: String?)] { lock.withLock { _agendaCalls } }
    var postSyncCalls: Int { lock.withLock { _postSyncCalls } }
    var streamLastEventIDs: [String?] { lock.withLock { _streamIDs } }

    func agenda(from: Date, to: Date, etag: String?) async throws -> Conditional<AgendaResponse> {
        lock.withLock { _agendaCalls.append((from, to, etag)) }
        if let g = gate { await g() }
        if let f = failure { throw f }
        return lock.withLock {
            if let etag, etag == _etag { return .notModified }
            var a = _agenda; a.from = from; a.to = to
            return .modified(a, etag: _etag)
        }
    }
    func accounts() async throws -> [Account] { if let f = failure { throw f }; return accountList }
    func calendars() async throws -> [HubCalendar] { if let f = failure { throw f }; return lock.withLock { _calendars } }
    func settings() async throws -> Settings { if let f = failure { throw f }; return settingsValue }
    func putSettings(_ s: Settings) async throws -> Settings { settingsValue = s; return s }
    func event(id: String) async throws -> EventDetail {
        if let f = failure { throw f }
        return EventDetail(item: timed(id, "Detail \(id)", "2026-10-11T10:00:00Z", "2026-10-11T11:00:00Z", version: 1), description: "d")
    }
    func events(q: String?, from: Date?, to: Date?, calendar: String?, cursor: String?, limit: Int?) async throws -> EventPage { EventPage(items: []) }
    func patchCalendar(id: String, visible: Bool?, colorOverride: ColorOverrideChange) async throws -> HubCalendar {
        var c = testCalendar(id); if let visible { c.visible = visible }
        if case .set(let s) = colorOverride { c.colorOverride = s }
        return c
    }
    func status() async throws -> StatusDTO { throw APIClientError.invalidResponse }
    func syncRuns(limit: Int?, accountId: String?) async throws -> [SyncRun] { [] }
    func postSync(accountId: String?) async throws -> Int { lock.withLock { _postSyncCalls += 1 }; return 1 }
    func openStream(lastEventID: String?) -> AsyncThrowingStream<StreamEvent, Error> {
        let (n, script) = lock.withLock { () -> (Int, (@Sendable (Int, String?) -> AsyncThrowingStream<StreamEvent, Error>)?) in
            _streamIDs.append(lastEventID); return (_streamIDs.count, _stream)
        }
        if let script { return script(n, lastEventID) }
        return AsyncThrowingStream { $0.finish() }
    }
}

final class Counter: @unchecked Sendable {
    private let lock = NSLock(); private var n = 0
    func inc() { lock.withLock { n += 1 } }
    var value: Int { lock.withLock { n } }
}

final class Recorder<T: Sendable>: @unchecked Sendable {
    private let lock = NSLock(); private var xs: [T] = []
    func add(_ x: T) { lock.withLock { xs.append(x) } }
    var values: [T] { lock.withLock { xs } }
}

struct RecordingHook: AfterSyncHook {
    let rec = Recorder<SyncOutcome>()
    func afterSync(_ outcome: SyncOutcome) async { rec.add(outcome) }
}

/// Polls `cond` until true or ~2 s elapse.
func eventually(_ cond: @Sendable () async -> Bool) async -> Bool {
    for _ in 0..<200 {
        if await cond() { return true }
        try? await Task.sleep(for: .milliseconds(10))
    }
    return await cond()
}

func firstValue<T: Sendable>(_ s: AsyncStream<T>, where pred: @Sendable (T) -> Bool = { _ in true }) async -> T? {
    for await v in s where pred(v) { return v }
    return nil
}

func makeEngine(_ hub: FakeHub, now: String = "2026-10-11T10:00:00Z", hooks: [any AfterSyncHook] = [],
                sleep: @escaping LiveStream.Sleep = { _ in }) throws -> (SyncEngine, AppDatabase, TestClock) {
    let db = try AppDatabase.inMemory()
    let clock = TestClock(iso(now))
    let engine = SyncEngine(api: hub, db: db, clock: clock.clock, hooks: hooks, backoff: Backoff(jitter: 0), sleep: sleep)
    return (engine, db, clock)
}
