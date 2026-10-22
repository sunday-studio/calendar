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
