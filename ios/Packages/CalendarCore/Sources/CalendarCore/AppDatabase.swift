import Foundation
import GRDB

public let appGroupIdentifier = "group.dev.casprine.calendar"

/// On-device cache (ENGINEERING 8.3). Schema, records and the one-transaction snapshot writer.
public struct AppDatabase: Sendable {
    public let writer: any DatabaseWriter

    public init(writer: any DatabaseWriter) throws {
        self.writer = writer
        try Self.migrator.migrate(writer)
    }

    /// In-memory DB for tests and previews.
    public static func inMemory() throws -> AppDatabase {
        try AppDatabase(writer: DatabaseQueue())
    }

    /// On-disk DB in the App Group container; falls back to Documents when the
    /// group is unavailable (e.g. unsigned simulator builds).
    public static func openShared(fileName: String = "calendar.sqlite") throws -> AppDatabase {
        let fm = FileManager.default
        let dir = fm.containerURL(forSecurityApplicationGroupIdentifier: appGroupIdentifier)
            ?? fm.urls(for: .documentDirectory, in: .userDomainMask)[0]
        try fm.createDirectory(at: dir, withIntermediateDirectories: true)
        return try AppDatabase(writer: DatabasePool(path: dir.appending(path: fileName).path))
    }

    static var migrator: DatabaseMigrator {
        var m = DatabaseMigrator()
        m.registerMigration("v1") { db in
            try db.create(table: "occurrence_item") { t in
                t.primaryKey("id", .text)
                t.column("eventId", .text).notNull().indexed()
                t.column("version", .integer).notNull()
                t.column("calendarId", .text).notNull().indexed()
                t.column("accountId", .text).notNull()
                t.column("color", .text).notNull()
                t.column("title", .text).notNull()
                t.column("allDay", .boolean).notNull()
                t.column("start", .datetime).indexed()
                t.column("end", .datetime)
                t.column("tzid", .text)
                t.column("date", .text)
                t.column("endDate", .text)
                t.column("location", .text)
                t.column("joinUrl", .text)
                t.column("joinKind", .text)
                t.column("myResponse", .text)
                t.column("status", .text).notNull()
                t.column("busy", .boolean).notNull()
                t.column("recurring", .boolean).notNull()
                t.column("canEdit", .boolean).notNull()
                t.column("canRSVP", .boolean).notNull()
                t.column("canAddConference", .boolean).notNull()
            }
            try db.create(table: "event_detail") { t in
                t.primaryKey("eventId", .text)
                t.column("json", .blob).notNull()
                t.column("fetchedAt", .datetime).notNull()
            }
            try db.create(table: "calendar") { t in
                t.primaryKey("id", .text)
                t.column("accountId", .text).notNull().indexed()
                t.column("name", .text).notNull()
                t.column("color", .text).notNull()
                t.column("colorOverride", .text)
                t.column("tzid", .text).notNull()
                t.column("access", .text).notNull()
                t.column("isPrimary", .boolean).notNull()
                t.column("visible", .boolean).notNull()
            }
            try db.create(table: "account") { t in
                t.primaryKey("id", .text)
                t.column("provider", .text).notNull()
                t.column("email", .text).notNull()
                t.column("displayName", .text).notNull()
                t.column("color", .text).notNull()
                t.column("status", .text).notNull()
                t.column("statusDetail", .text)
                t.column("lastSyncAt", .datetime)
                t.column("lastOkAt", .datetime)
                t.column("createdAt", .datetime).notNull()
            }
            try db.create(table: "kv") { t in
                t.primaryKey("key", .text)
                t.column("value", .text)
            }
        }
        // v2 (T10): day keys in the user's tz for per-day queries, version on cached details.
        // Tables are recreated (cache only, repopulated by the next sync).
        m.registerMigration("v2") { db in
            try db.drop(table: "occurrence_item")
            try db.drop(table: "event_detail")
            try db.create(table: "occurrence_item") { t in
                t.primaryKey("id", .text)
                t.column("eventId", .text).notNull().indexed()
                t.column("version", .integer).notNull()
                t.column("calendarId", .text).notNull().indexed()
                t.column("accountId", .text).notNull()
                t.column("color", .text).notNull()
                t.column("title", .text).notNull()
                t.column("allDay", .boolean).notNull()
                t.column("start", .datetime)
                t.column("end", .datetime)
                t.column("tzid", .text)
                t.column("date", .text)
                t.column("endDate", .text)
                t.column("location", .text)
                t.column("joinUrl", .text)
                t.column("joinKind", .text)
                t.column("myResponse", .text)
                t.column("status", .text).notNull()
                t.column("busy", .boolean).notNull()
                t.column("recurring", .boolean).notNull()
                t.column("canEdit", .boolean).notNull()
                t.column("canRSVP", .boolean).notNull()
                t.column("canAddConference", .boolean).notNull()
                // First / last local day (user tz, "yyyy-MM-dd", inclusive).
                t.column("dayStart", .text).notNull()
                t.column("dayEnd", .text).notNull()
            }
            try db.create(index: "occurrence_item_days", on: "occurrence_item", columns: ["dayStart", "dayEnd"])
            try db.create(index: "occurrence_item_start", on: "occurrence_item", columns: ["start", "end"])
            try db.create(table: "event_detail") { t in
                t.primaryKey("id", .text) // the id requested: eventId or eventId:rkey
                t.column("eventId", .text).notNull().indexed()
                t.column("version", .integer).notNull()
                t.column("json", .blob).notNull()
                t.column("fetchedAt", .datetime).notNull()
            }
        }
        // Organiser on agenda rows (cache only, repopulated by the next sync).
        m.registerMigration("v3") { db in
            try db.alter(table: "occurrence_item") { t in
                t.add(column: "organizerEmail", .text)
                t.add(column: "organizerName", .text)
                t.add(column: "organizerSelf", .boolean)
            }
        }
        return m
    }
}

// MARK: - Records

struct OccurrenceRecord: Codable, FetchableRecord, PersistableRecord, Sendable, Equatable {
    static let databaseTableName = "occurrence_item"
    var id: String
    var eventId: String
    var version: Int64
    var calendarId: String
    var accountId: String
    var color: String
    var title: String
    var allDay: Bool
    var start: Date?
    var end: Date?
    var tzid: String?
    var date: String?
    var endDate: String?
    var location: String?
    var joinUrl: String?
    var joinKind: String?
    var myResponse: String?
    var status: String
    var busy: Bool
    var recurring: Bool
    var canEdit: Bool
    var canRSVP: Bool
    var canAddConference: Bool
    var dayStart: String
    var dayEnd: String
    var organizerEmail: String?
    var organizerName: String?
    var organizerSelf: Bool?

    init(_ i: AgendaItem, calendar: Calendar) {
        let r = i.dayRange(calendar: calendar)
        id = i.id; eventId = i.eventId; version = i.version; calendarId = i.calendarId; accountId = i.accountId
        color = i.color; title = i.title; allDay = i.allDay; start = i.start; end = i.end; tzid = i.tzid
        date = i.date; endDate = i.endDate; location = i.location; joinUrl = i.join?.url; joinKind = i.join?.kind
        myResponse = i.myResponse; status = i.status; busy = i.busy; recurring = i.recurring
        canEdit = i.canEdit; canRSVP = i.canRSVP; canAddConference = i.canAddConference
        dayStart = r.lowerBound.rawValue; dayEnd = r.upperBound.rawValue
        organizerEmail = i.organizer?.email; organizerName = i.organizer?.name; organizerSelf = i.organizer?.isSelf
    }

    var item: AgendaItem {
        AgendaItem(
            id: id, eventId: eventId, version: version, calendarId: calendarId, accountId: accountId, color: color,
            title: title, allDay: allDay, start: start, end: end, tzid: tzid, date: date, endDate: endDate,
            location: location, join: joinUrl.map { Join(url: $0, kind: joinKind ?? "other") },
            myResponse: myResponse, status: status, busy: busy, recurring: recurring, canEdit: canEdit,
            canRSVP: canRSVP, canAddConference: canAddConference,
            organizer: organizerEmail.map { Person(email: $0, name: organizerName, isSelf: organizerSelf) })
    }
}

extension Account: FetchableRecord, PersistableRecord {
    public static var databaseTableName: String { "account" }
}

extension HubCalendar: FetchableRecord, PersistableRecord {
    public static var databaseTableName: String { "calendar" }
}

struct EventDetailRecord: Codable, FetchableRecord, PersistableRecord, Sendable {
    static let databaseTableName = "event_detail"
    var id: String
    var eventId: String
    var version: Int64
    var json: Data
    var fetchedAt: Date
}

/// Keys in the `kv` table.
public enum KVKey {
    public static let lastRev = "lastRev"
    public static let etag = "etag"
    public static let lastSyncAt = "lastSyncAt"
    public static let windowFrom = "windowFrom"
    public static let windowTo = "windowTo"
    public static let settings = "settings"
    public static let userTz = "userTz"
}

extension Database {
    func kvGet(_ key: String) throws -> String? {
        try String.fetchOne(self, sql: "SELECT value FROM kv WHERE key = ?", arguments: [key])
    }
    func kvSet(_ key: String, _ value: String?) throws {
        if let value {
            try execute(sql: "INSERT INTO kv(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
                        arguments: [key, value])
        } else {
            try execute(sql: "DELETE FROM kv WHERE key = ?", arguments: [key])
        }
    }
}

// MARK: - Snapshot writer

/// Everything one refresh learned. Applied in ONE transaction by `AppDatabase.apply`.
public struct SyncSnapshot: Sendable {
    public struct Agenda: Sendable {
        public var items: [AgendaItem]
        public var rev: Int64
        public var etag: String?
        public var from: Date
        public var to: Date
        public init(items: [AgendaItem], rev: Int64, etag: String?, from: Date, to: Date) {
            self.items = items; self.rev = rev; self.etag = etag; self.from = from; self.to = to
        }
    }
    public var agenda: Agenda?        // nil when the hub answered 304
    public var accounts: [Account]?
    public var calendars: [HubCalendar]?
    public var settings: Settings?
    public var syncedAt: Date
    public init(agenda: Agenda? = nil, accounts: [Account]? = nil, calendars: [HubCalendar]? = nil,
                settings: Settings? = nil, syncedAt: Date) {
        self.agenda = agenda; self.accounts = accounts; self.calendars = calendars
        self.settings = settings; self.syncedAt = syncedAt
    }
}

extension AppDatabase {
    /// The user's time zone: hub `userTzid` setting, else the device zone.
    public static func resolveTimeZone(settings: Settings?, stored: String?) -> TimeZone {
        if let id = settings?.userTzid, let tz = TimeZone(identifier: id) { return tz }
        if let id = stored, let tz = TimeZone(identifier: id) { return tz }
        return .current
    }

    public func userTimeZone() throws -> TimeZone {
        try writer.read { db in
            let settings = try db.kvGet(KVKey.settings).flatMap { try? JSONDecoder.cache.decode(Settings.self, from: Data($0.utf8)) }
            return Self.resolveTimeZone(settings: settings, stored: try db.kvGet(KVKey.userTz))
        }
    }

    /// Replace the window rows and update accounts/calendars/settings/meta atomically. If the user's time
    /// zone changed, day keys of existing rows are recomputed in the same transaction.
    @discardableResult
    public func apply(_ snap: SyncSnapshot) async throws -> TimeZone {
        try await writer.write { db in
            let storedTz = try db.kvGet(KVKey.userTz)
            let storedSettings = try db.kvGet(KVKey.settings).flatMap { try? JSONDecoder.cache.decode(Settings.self, from: Data($0.utf8)) }
            let tz = Self.resolveTimeZone(settings: snap.settings ?? storedSettings, stored: storedTz)
            let cal = DayKey.calendar(tz)

            if let a = snap.agenda {
                try db.execute(sql: "DELETE FROM occurrence_item")
                for i in a.items { try OccurrenceRecord(i, calendar: cal).insert(db, onConflict: .replace) }
                try db.kvSet(KVKey.lastRev, String(a.rev))
                try db.kvSet(KVKey.etag, a.etag)
                try db.kvSet(KVKey.windowFrom, String(Int64(a.from.timeIntervalSince1970)))
                try db.kvSet(KVKey.windowTo, String(Int64(a.to.timeIntervalSince1970)))
                // Cached details whose version moved on are stale; very old ones are dropped.
                try db.execute(sql: """
                    DELETE FROM event_detail WHERE id IN (
                      SELECT d.id FROM event_detail d JOIN occurrence_item o ON o.eventId = d.eventId WHERE o.version != d.version)
                    """)
                try db.execute(sql: "DELETE FROM event_detail WHERE fetchedAt < ?",
                               arguments: [snap.syncedAt.addingTimeInterval(-30 * 86_400)])
            } else if storedTz != tz.identifier {
                for r in try OccurrenceRecord.fetchAll(db) {
                    var n = OccurrenceRecord(r.item, calendar: cal)
                    n.id = r.id
                    try n.update(db)
                }
            }
            if let accounts = snap.accounts {
                let ids = accounts.map(\.id)
                try Account.filter(!ids.contains(Column("id"))).deleteAll(db)
                for a in accounts { try a.save(db) }
            }
            if let calendars = snap.calendars {
                let ids = calendars.map(\.id)
                try HubCalendar.filter(!ids.contains(Column("id"))).deleteAll(db)
                for c in calendars { try c.save(db) }
            }
            if let s = snap.settings {
                try db.kvSet(KVKey.settings, String(decoding: try JSONEncoder.cache.encode(s), as: UTF8.self))
            }
            try db.kvSet(KVKey.userTz, tz.identifier)
            try db.kvSet(KVKey.lastSyncAt, String(snap.syncedAt.timeIntervalSince1970))
            return tz
        }
    }
}

extension JSONDecoder {
    static var cache: JSONDecoder { APIClient.makeDecoder() }
}
extension JSONEncoder {
    static var cache: JSONEncoder {
        let e = JSONEncoder()
        e.dateEncodingStrategy = .iso8601
        return e
    }
}
