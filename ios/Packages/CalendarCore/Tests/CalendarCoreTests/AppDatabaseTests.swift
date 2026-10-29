import Foundation
import GRDB
import Testing
@testable import CalendarCore

@Suite struct AppDatabaseTests {
    @Test func migrationCreatesTables() throws {
        let db = try AppDatabase.inMemory()
        let tables = try db.writer.read { try String.fetchAll($0, sql: "SELECT name FROM sqlite_master WHERE type='table'") }
        for t in ["occurrence_item", "event_detail", "calendar", "account", "kv"] {
            #expect(tables.contains(t))
        }
    }
}

@Suite struct MovedMemoryTests {
    func item(_ start: Date) -> AgendaItem {
        AgendaItem(id: "o1", eventId: "e1", calendarId: "c", accountId: "a", color: "cobalt", title: "Dentist", allDay: false,
                   start: start, end: start.addingTimeInterval(1800), tzid: "UTC")
    }
    func sync(_ db: AppDatabase, _ start: Date, at t: Date) async throws {
        let from = Date(timeIntervalSince1970: 0), to = Date(timeIntervalSince1970: 4_000_000_000)
        _ = try await db.apply(SyncSnapshot(agenda: .init(items: [item(start)], rev: 1, etag: nil, from: from, to: to), syncedAt: t))
    }
    func stored(_ db: AppDatabase) throws -> OccurrenceRecord {
        try db.writer.read { try OccurrenceRecord.fetchOne($0)! }
    }

    @Test func aMovedStartKeepsTheOldOneFor24Hours() async throws {
        let db = try AppDatabase.inMemory()
        let t0 = Date(timeIntervalSince1970: 1_800_000_000)
        try await sync(db, t0.addingTimeInterval(3600), at: t0)
        #expect(try stored(db).prevStart == nil)
        try await sync(db, t0.addingTimeInterval(7200), at: t0.addingTimeInterval(60))
        let moved = try stored(db)
        #expect(moved.prevStart == t0.addingTimeInterval(3600) && moved.movedAt == t0.addingTimeInterval(60))
        // Unchanged on the next sync: still remembered; gone after 24 h.
        try await sync(db, t0.addingTimeInterval(7200), at: t0.addingTimeInterval(600))
        #expect(try stored(db).prevStart == t0.addingTimeInterval(3600))
        try await sync(db, t0.addingTimeInterval(7200), at: t0.addingTimeInterval(2 * 86_400))
        #expect(try stored(db).prevStart == nil)
    }
}
