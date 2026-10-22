import Foundation
import Testing
@testable import CalendarCore

/// Runs the real SyncEngine + APIClient against a live hub (normally `calendard serve --fixtures`).
/// Enabled by `scripts/e2e-fixtures.sh`, which exports TEST_RUNNER_CALENDAR_E2E_URL for xcodebuild.
@Suite(.enabled(if: ProcessInfo.processInfo.environment["CALENDAR_E2E_URL"] != nil)) struct FixtureHubE2ETests {
    @Test func syncsTheFixtureHub() async throws {
        let env = ProcessInfo.processInfo.environment
        let url = try #require(URL(string: env["CALENDAR_E2E_URL"] ?? ""))
        let api = APIClient(baseURL: url, token: env["CALENDAR_E2E_TOKEN"] ?? "cal_e2e")
        let db = try AppDatabase.inMemory()
        let e = AppEnvironment.make(api: api, db: db)
        let engine = try #require(e.sync as? SyncEngine)

        await engine.refresh()
        #expect(engine.currentState == .idle)
        #expect(try await e.accounts.accounts().count == 3)
        #expect(try await e.calendars.calendars().isEmpty == false)
        let tz = try await e.settings.userTimeZone()
        let today = DayKey(Date(), in: tz)
        let items = try await e.agenda.items(from: today.adding(days: -14), to: today.adding(days: 90))
        #expect(!items.isEmpty)
        #expect(try await e.syncMeta.lastRev() >= 1)

        // Second pass must be answered 304 and keep everything.
        let before = items.count
        await engine.refresh()
        #expect(engine.currentState == .idle)
        #expect(try await e.agenda.items(from: today.adding(days: -14), to: today.adding(days: 90)).count == before)

        // Detail is cache-first.
        let first = try #require(items.first)
        let d = try await e.eventDetail.detail(id: first.id)
        #expect(d.item.eventId == first.eventId)
        #expect(try await e.eventDetail.cached(id: first.id) != nil)
    }
}
