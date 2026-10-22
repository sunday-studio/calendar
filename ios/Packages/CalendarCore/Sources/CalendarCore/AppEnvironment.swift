import Foundation

/// Dependency container handed to view models. Everything is a protocol so previews/tests can swap fakes.
public struct AppEnvironment: Sendable {
    public var api: any HubReadAPI
    public var db: AppDatabase
    public var agenda: any AgendaRepository
    public var calendars: any CalendarRepository
    public var accounts: any AccountRepository
    public var settings: any SettingsRepository
    public var syncMeta: any SyncMetaRepository
    public var eventDetail: any EventDetailRepository
    public var sync: any SyncControlling
    public var clock: AppClock

    public init(
        api: any HubReadAPI, db: AppDatabase, agenda: any AgendaRepository, calendars: any CalendarRepository,
        accounts: any AccountRepository, settings: any SettingsRepository, syncMeta: any SyncMetaRepository,
        eventDetail: any EventDetailRepository, sync: any SyncControlling, clock: AppClock = .system
    ) {
        self.api = api; self.db = db; self.agenda = agenda; self.calendars = calendars; self.accounts = accounts
        self.settings = settings; self.syncMeta = syncMeta; self.eventDetail = eventDetail
        self.sync = sync; self.clock = clock
    }

    /// Wires GRDB repositories and a `SyncEngine` over `api`.
    public static func make(
        api: any HubReadAPI, db: AppDatabase, clock: AppClock = .system, hooks: [any AfterSyncHook] = []
    ) -> AppEnvironment {
        AppEnvironment(
            api: api, db: db,
            agenda: GRDBAgendaRepository(db: db),
            calendars: GRDBCalendarRepository(db: db, api: api),
            accounts: GRDBAccountRepository(db: db),
            settings: GRDBSettingsRepository(db: db, api: api),
            syncMeta: GRDBSyncMetaRepository(db: db),
            eventDetail: GRDBEventDetailRepository(db: db, api: api, clock: clock),
            sync: SyncEngine(api: api, db: db, clock: clock, hooks: hooks),
            clock: clock)
    }

    /// Production wiring from the paired hub config.
    public static func live(
        config: HubConfig, db: AppDatabase, session: URLSession = .shared, clock: AppClock = .system,
        hooks: [any AfterSyncHook] = []
    ) -> AppEnvironment {
        make(api: APIClient(config: config, session: session), db: db, clock: clock, hooks: hooks)
    }
}

/// Does nothing; reports a fixed state. For previews.
public struct PreviewSyncController: SyncControlling {
    public var fixed: SyncState
    public init(_ fixed: SyncState = .idle) { self.fixed = fixed }
    public var state: AsyncStream<SyncState> {
        let s = fixed
        return AsyncStream { $0.yield(s) }
    }
    public func appForeground() async {}
    public func appBackground() async {}
    public func manualRefresh() async {}
    public func silentPush() async -> Bool { false }
}

/// API that never reaches a hub; every call throws `.transport`. Previews read only from the DB.
public struct OfflineAPI: HubReadAPI {
    public init() {}
    private var down: APIClientError { .transport("preview: offline") }
    public func agenda(from: Date, to: Date, etag: String?) async throws -> Conditional<AgendaResponse> { throw down }
    public func event(id: String) async throws -> EventDetail { throw down }
    public func events(q: String?, from: Date?, to: Date?, calendar: String?, cursor: String?, limit: Int?) async throws -> EventPage { throw down }
    public func accounts() async throws -> [Account] { throw down }
    public func calendars() async throws -> [HubCalendar] { throw down }
    public func patchCalendar(id: String, visible: Bool?, colorOverride: ColorOverrideChange) async throws -> HubCalendar { throw down }
    public func settings() async throws -> Settings { throw down }
    public func putSettings(_ settings: Settings) async throws -> Settings { throw down }
    public func status() async throws -> StatusDTO { throw down }
    public func syncRuns(limit: Int?, accountId: String?) async throws -> [SyncRun] { throw down }
    public func postSync(accountId: String?) async throws -> Int { throw down }
    public func openStream(lastEventID: String?) -> AsyncThrowingStream<StreamEvent, Error> {
        AsyncThrowingStream { $0.finish(throwing: APIClientError.transport("preview: offline")) }
    }
}

public enum PreviewEnvironment {
    /// In-memory DB seeded from decoded hub payloads (tests pass the fixtures JSON), fake clock, no network.
    public static func make(
        agenda: AgendaResponse, accounts: [Account], calendars: [HubCalendar], settings: Settings,
        clock: AppClock = .system, state: SyncState = .idle
    ) throws -> AppEnvironment {
        let db = try AppDatabase.inMemory()
        let syncedAt = clock.now()
        // Seeding is synchronous for previews: block on the async writer via a detached semaphore-free path.
        try db.writer.write { d in
            let tz = AppDatabase.resolveTimeZone(settings: settings, stored: nil)
            let cal = DayKey.calendar(tz)
            for i in agenda.items { try OccurrenceRecord(i, calendar: cal).insert(d, onConflict: .replace) }
            for a in accounts { try a.save(d) }
            for c in calendars { try c.save(d) }
            try d.kvSet(KVKey.settings, String(decoding: try JSONEncoder.cache.encode(settings), as: UTF8.self))
            try d.kvSet(KVKey.userTz, tz.identifier)
            try d.kvSet(KVKey.lastRev, String(agenda.rev))
            try d.kvSet(KVKey.lastSyncAt, String(syncedAt.timeIntervalSince1970))
        }
        var env = AppEnvironment.make(api: OfflineAPI(), db: db, clock: clock)
        env.sync = PreviewSyncController(state)
        return env
    }

    /// Decodes the raw fixture JSON files (agenda, accounts, calendars, settings).
    public static func make(
        agendaJSON: Data, accountsJSON: Data, calendarsJSON: Data, settingsJSON: Data,
        clock: AppClock = .system, state: SyncState = .idle
    ) throws -> AppEnvironment {
        let d = APIClient.makeDecoder()
        return try make(
            agenda: d.decode(AgendaResponse.self, from: agendaJSON),
            accounts: d.decode(AccountList.self, from: accountsJSON).items,
            calendars: d.decode(CalendarList.self, from: calendarsJSON).items,
            settings: d.decode(Settings.self, from: settingsJSON), clock: clock, state: state)
    }

    /// Small built-in sample relative to `now` for SwiftUI previews that have no fixtures at hand.
    public static func sample(now: Date = Date(), tz: TimeZone = .current) throws -> AppEnvironment {
        let acc = Account(id: "acc1", email: "alex@example.com", displayName: "Alex", color: "cobalt", status: "active", lastSyncAt: now, lastOkAt: now)
        let cal = HubCalendar(id: "cal1", accountId: "acc1", name: "Work", color: "cobalt", tzid: tz.identifier, isPrimary: true)
        func item(_ id: String, _ title: String, startIn: TimeInterval, mins: Double) -> AgendaItem {
            let s = now.addingTimeInterval(startIn)
            return AgendaItem(id: id, eventId: id, calendarId: "cal1", accountId: "acc1", color: "cobalt", title: title, allDay: false,
                              start: s, end: s.addingTimeInterval(mins * 60), tzid: tz.identifier, myResponse: "accepted",
                              canEdit: true)
        }
        let items = [item("e1", "Stand-up", startIn: 600, mins: 15), item("e2", "Design review", startIn: 3600, mins: 45),
                     item("e3", "1:1", startIn: 7200, mins: 30)]
        let agenda = AgendaResponse(rev: 1, from: now.addingTimeInterval(-86_400), to: now.addingTimeInterval(86_400 * 7), items: items)
        return try make(agenda: agenda, accounts: [acc], calendars: [cal], settings: Settings(userTzid: tz.identifier), clock: .fixed(now))
    }
}
