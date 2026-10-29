import Foundation
import GRDB

// Repository protocols (views/view models depend on these; previews and tests use fakes or the
// in-memory GRDB implementations below). No SwiftUI here.

public protocol AgendaRepository: Sendable {
    /// Items touching any day in `from...to` (user-tz days, inclusive); multi-day items appear once.
    /// Ordered by first day, all-day first, then start, title, id.
    func items(from: DayKey, to: DayKey) async throws -> [AgendaItem]
    func observeItems(from: DayKey, to: DayKey) -> AsyncStream<[AgendaItem]>
    func item(id: String) async throws -> AgendaItem?
    /// The in-progress or next timed event (all-day, cancelled and declined events are ignored).
    func nextEvent(now: Date) async throws -> AgendaItem?
    func observeNextEvent(now: @escaping @Sendable () -> Date) -> AsyncStream<AgendaItem?>
    /// Days in `from...to` with at least one (non-cancelled) event, for month dots.
    func daysWithEvents(from: DayKey, to: DayKey) async throws -> Set<DayKey>
    func observeDaysWithEvents(from: DayKey, to: DayKey) -> AsyncStream<Set<DayKey>>
    /// Per day in `from...to`: colour keys of the accounts that have a (non-cancelled) event, at most 3, in a stable order.
    func observeDayAccounts(from: DayKey, to: DayKey) -> AsyncStream<[DayKey: [String]]>
}

public extension AgendaRepository {
    func observeDayAccounts(from: DayKey, to: DayKey) -> AsyncStream<[DayKey: [String]]> { AsyncStream { $0.finish() } }
}

public protocol CalendarRepository: Sendable {
    func calendars() async throws -> [HubCalendar]
    func observeCalendars() -> AsyncStream<[HubCalendar]>
    /// PATCH on the hub, then mirror the result locally (agenda follows via the next sync).
    func update(id: String, visible: Bool?, colorOverride: ColorOverrideChange) async throws -> HubCalendar
}

public protocol AccountRepository: Sendable {
    func accounts() async throws -> [Account]
    func observeAccounts() -> AsyncStream<[Account]>
}

public protocol SettingsRepository: Sendable {
    func settings() async throws -> Settings?
    func observeSettings() -> AsyncStream<Settings?>
    func userTimeZone() async throws -> TimeZone
    /// PUT on the hub (full replace), then store the result locally.
    func update(_ settings: Settings) async throws -> Settings
}

public protocol SyncMetaRepository: Sendable {
    func lastSyncAt() async throws -> Date?
    func observeLastSyncAt() -> AsyncStream<Date?>
    func lastRev() async throws -> Int64
}

public protocol EventDetailRepository: Sendable {
    /// Cached detail, no network.
    func cached(id: String) async throws -> EventDetail?
    /// Cache-first: cached copy if present, otherwise fetch (and cache).
    func detail(id: String) async throws -> EventDetail
    /// Always fetch and cache.
    func refresh(id: String) async throws -> EventDetail
    /// Pull to refresh on the detail page: ask the hub to poll the event's account, then fetch until the hub's copy changes.
    func syncAndRefresh(id: String) async throws -> EventDetail
}

public extension EventDetailRepository {
    func syncAndRefresh(id: String) async throws -> EventDetail { try await refresh(id: id) }
}

// MARK: - GRDB implementations

func observe<T: Sendable>(
    _ writer: any DatabaseWriter,
    _ fetch: @escaping @Sendable (Database) throws -> T
) -> AsyncStream<T> {
    AsyncStream { cont in
        let task = Task {
            do {
                for try await v in ValueObservation.tracking(fetch).values(in: writer) { cont.yield(v) }
            } catch {}
            cont.finish()
        }
        cont.onTermination = { _ in task.cancel() }
    }
}

public struct GRDBAgendaRepository: AgendaRepository {
    let writer: any DatabaseWriter
    public init(db: AppDatabase) { writer = db.writer }

    static func fetchItems(_ db: Database, from: DayKey, to: DayKey) throws -> [AgendaItem] {
        try OccurrenceRecord.fetchAll(db, sql: """
            SELECT * FROM occurrence_item WHERE dayStart <= ? AND dayEnd >= ?
            ORDER BY dayStart, allDay DESC, start, title, id
            """, arguments: [to.rawValue, from.rawValue]).map(\.item)
    }

    static func fetchNext(_ db: Database, now: Date) throws -> AgendaItem? {
        try OccurrenceRecord.fetchOne(db, sql: """
            SELECT * FROM occurrence_item
            WHERE allDay = 0 AND start IS NOT NULL AND status != 'cancelled'
              AND (myResponse IS NULL OR myResponse != 'declined')
              AND (end > :now OR (end <= start AND start >= :now))
            ORDER BY start, end, id LIMIT 1
            """, arguments: ["now": now])?.item
    }

    static func fetchDays(_ db: Database, from: DayKey, to: DayKey) throws -> Set<DayKey> {
        let rows = try Row.fetchAll(db, sql: """
            SELECT DISTINCT dayStart, dayEnd FROM occurrence_item
            WHERE dayStart <= ? AND dayEnd >= ? AND status != 'cancelled'
            """, arguments: [to.rawValue, from.rawValue])
        var out = Set<DayKey>()
        for r in rows {
            let s = DayKey(r["dayStart"] as String), e = DayKey(r["dayEnd"] as String)
            for d in max(s, from).through(min(e, to)) { out.insert(d) }
        }
        return out
    }

    static func fetchDayAccounts(_ db: Database, from: DayKey, to: DayKey) throws -> [DayKey: [String]] {
        let rows = try Row.fetchAll(db, sql: """
            SELECT DISTINCT dayStart, dayEnd, accountId, color FROM occurrence_item
            WHERE dayStart <= ? AND dayEnd >= ? AND status != 'cancelled'
            ORDER BY accountId
            """, arguments: [to.rawValue, from.rawValue])
        var out: [DayKey: [String]] = [:]
        for r in rows {
            let s = DayKey(r["dayStart"] as String), e = DayKey(r["dayEnd"] as String), c: String = r["color"]
            for d in max(s, from).through(min(e, to)) where (out[d]?.count ?? 0) < 3 && !(out[d]?.contains(c) ?? false) {
                out[d, default: []].append(c)
            }
        }
        return out
    }

    public func observeDayAccounts(from: DayKey, to: DayKey) -> AsyncStream<[DayKey: [String]]> {
        observe(writer) { try Self.fetchDayAccounts($0, from: from, to: to) }
    }

    public func items(from: DayKey, to: DayKey) async throws -> [AgendaItem] {
        try await writer.read { try Self.fetchItems($0, from: from, to: to) }
    }
    public func observeItems(from: DayKey, to: DayKey) -> AsyncStream<[AgendaItem]> {
        observe(writer) { try Self.fetchItems($0, from: from, to: to) }
    }
    public func item(id: String) async throws -> AgendaItem? {
        try await writer.read { try OccurrenceRecord.fetchOne($0, key: id)?.item }
    }
    public func nextEvent(now: Date) async throws -> AgendaItem? {
        try await writer.read { try Self.fetchNext($0, now: now) }
    }
    public func observeNextEvent(now: @escaping @Sendable () -> Date) -> AsyncStream<AgendaItem?> {
        observe(writer) { try Self.fetchNext($0, now: now()) }
    }
    public func daysWithEvents(from: DayKey, to: DayKey) async throws -> Set<DayKey> {
        try await writer.read { try Self.fetchDays($0, from: from, to: to) }
    }
    public func observeDaysWithEvents(from: DayKey, to: DayKey) -> AsyncStream<Set<DayKey>> {
        observe(writer) { try Self.fetchDays($0, from: from, to: to) }
    }
}

public struct GRDBCalendarRepository: CalendarRepository {
    let writer: any DatabaseWriter
    let api: any HubReadAPI
    public init(db: AppDatabase, api: any HubReadAPI) { writer = db.writer; self.api = api }

    static func fetch(_ db: Database) throws -> [HubCalendar] {
        try HubCalendar.order(Column("accountId"), Column("isPrimary").desc, Column("name")).fetchAll(db)
    }
    public func calendars() async throws -> [HubCalendar] { try await writer.read(Self.fetch) }
    public func observeCalendars() -> AsyncStream<[HubCalendar]> { observe(writer, Self.fetch) }
    public func update(id: String, visible: Bool?, colorOverride: ColorOverrideChange) async throws -> HubCalendar {
        let c = try await api.patchCalendar(id: id, visible: visible, colorOverride: colorOverride)
        try await writer.write { try c.save($0) }
        return c
    }
}

public extension AppDatabase {
    /// The calendars saved on this phone (widget settings list them without a network or an API client).
    func savedCalendars() throws -> [HubCalendar] { try writer.read(GRDBCalendarRepository.fetch) }
}

public struct GRDBAccountRepository: AccountRepository {
    let writer: any DatabaseWriter
    public init(db: AppDatabase) { writer = db.writer }
    static func fetch(_ db: Database) throws -> [Account] { try Account.order(Column("createdAt"), Column("id")).fetchAll(db) }
    public func accounts() async throws -> [Account] { try await writer.read(Self.fetch) }
    public func observeAccounts() -> AsyncStream<[Account]> { observe(writer, Self.fetch) }
}

public struct GRDBSettingsRepository: SettingsRepository {
    let db: AppDatabase
    let api: any HubReadAPI
    public init(db: AppDatabase, api: any HubReadAPI) { self.db = db; self.api = api }

    static func fetch(_ db: Database) throws -> Settings? {
        guard let s = try db.kvGet(KVKey.settings) else { return nil }
        return try? JSONDecoder.cache.decode(Settings.self, from: Data(s.utf8))
    }
    public func settings() async throws -> Settings? { try await db.writer.read(Self.fetch) }
    public func observeSettings() -> AsyncStream<Settings?> { observe(db.writer, Self.fetch) }
    public func userTimeZone() async throws -> TimeZone { try db.userTimeZone() }
    public func update(_ settings: Settings) async throws -> Settings {
        let s = try await api.putSettings(settings)
        try await db.writer.write { d in
            try d.kvSet(KVKey.settings, String(decoding: try JSONEncoder.cache.encode(s), as: UTF8.self))
            if let tz = s.userTzid { try d.kvSet(KVKey.userTz, tz) }
        }
        return s
    }
}

public struct GRDBSyncMetaRepository: SyncMetaRepository {
    let writer: any DatabaseWriter
    public init(db: AppDatabase) { writer = db.writer }
    static func fetchAt(_ db: Database) throws -> Date? {
        try db.kvGet(KVKey.lastSyncAt).flatMap(Double.init).map(Date.init(timeIntervalSince1970:))
    }
    public func lastSyncAt() async throws -> Date? { try await writer.read(Self.fetchAt) }
    public func observeLastSyncAt() -> AsyncStream<Date?> { observe(writer, Self.fetchAt) }
    public func lastRev() async throws -> Int64 {
        try await writer.read { try $0.kvGet(KVKey.lastRev).flatMap(Int64.init) ?? 0 }
    }
}

public struct GRDBEventDetailRepository: EventDetailRepository {
    let writer: any DatabaseWriter
    let api: any HubReadAPI
    let clock: AppClock
    /// Waits between fetches after asking the hub to sync (the hub answers `POST /v1/sync` at once and polls Google in the background).
    let settle: [Duration]
    public init(db: AppDatabase, api: any HubReadAPI, clock: AppClock = .system,
                settle: [Duration] = [.milliseconds(800), .milliseconds(1500), .milliseconds(2500)]) {
        writer = db.writer; self.api = api; self.clock = clock; self.settle = settle
    }

    public func cached(id: String) async throws -> EventDetail? {
        try await writer.read { db in
            guard let r = try EventDetailRecord.fetchOne(db, key: id) else { return nil }
            return try? JSONDecoder.cache.decode(EventDetail.self, from: r.json)
        }
    }

    public func detail(id: String) async throws -> EventDetail {
        if let c = try await cached(id: id) { return c }
        return try await refresh(id: id)
    }

    public func syncAndRefresh(id: String) async throws -> EventDetail {
        let before = try await writer.read { try EventDetailRecord.fetchOne($0, key: id)?.json }
        let accountId = before.flatMap { try? JSONDecoder.cache.decode(EventDetail.self, from: $0) }?.item.accountId
        guard (try? await api.postSync(accountId: accountId)) != nil else { return try await refresh(id: id) }
        var latest = try await refresh(id: id)
        guard let before else { return latest }
        for wait in settle {
            if try JSONEncoder.cache.encode(latest) != before { break }
            try await Task.sleep(for: wait)
            latest = try await refresh(id: id)
        }
        return latest
    }

    public func refresh(id: String) async throws -> EventDetail {
        let d = try await api.event(id: id)
        let json = try JSONEncoder.cache.encode(d)
        let rec = EventDetailRecord(id: id, eventId: d.item.eventId, version: d.item.version, json: json, fetchedAt: clock.now())
        try await writer.write { try rec.save($0) }
        return d
    }
}
