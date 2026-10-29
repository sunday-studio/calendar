import Foundation
import GRDB
import Testing
@testable import CalendarCore

@Suite struct RepositoryTests {
    func seeded(_ items: [AgendaItem], tz: TimeZone = london) async throws -> (AppDatabase, GRDBAgendaRepository) {
        let db = try AppDatabase.inMemory()
        _ = try await db.apply(SyncSnapshot(
            agenda: .init(items: items, rev: 1, etag: "e", from: iso("2026-09-27T00:00:00Z"), to: iso("2027-01-11T00:00:00Z")),
            accounts: [testAccount()], calendars: [testCalendar()], settings: Settings(userTzid: tz.identifier),
            syncedAt: iso("2026-10-11T10:00:00Z")))
        return (db, GRDBAgendaRepository(db: db))
    }

    @Test func nextEventInProgressUpcomingNone() async throws {
        let (_, repo) = try await seeded([
            timed("past:1", "Past", "2026-10-11T07:00:00Z", "2026-10-11T08:00:00Z"),
            timed("now:1", "Now", "2026-10-11T09:00:00Z", "2026-10-11T10:00:00Z"),
            timed("next:1", "Next", "2026-10-11T11:00:00Z", "2026-10-11T12:00:00Z"),
            timed("later:1", "Later", "2026-10-12T09:00:00Z", "2026-10-12T10:00:00Z"),
        ])
        #expect(try await repo.nextEvent(now: iso("2026-10-11T09:30:00Z"))?.id == "now:1")   // in progress
        #expect(try await repo.nextEvent(now: iso("2026-10-11T10:00:00Z"))?.id == "next:1")  // end is exclusive
        #expect(try await repo.nextEvent(now: iso("2026-10-11T12:30:00Z"))?.id == "later:1") // upcoming, next day
        #expect(try await repo.nextEvent(now: iso("2026-10-12T10:00:00Z")) == nil)           // none left
    }

    @Test func nextEventIgnoresAllDayCancelledAndDeclined() async throws {
        let (_, repo) = try await seeded([
            allDayItem("ad", "Holiday", "2026-10-11", "2026-10-12"),
            timed("dec:1", "Declined", "2026-10-11T09:00:00Z", "2026-10-11T10:00:00Z", response: "declined"),
            timed("can:1", "Cancelled", "2026-10-11T09:10:00Z", "2026-10-11T10:00:00Z", status: "cancelled"),
            timed("ok:1", "Real", "2026-10-11T15:00:00Z", "2026-10-11T16:00:00Z"),
        ])
        #expect(try await repo.nextEvent(now: iso("2026-10-11T09:30:00Z"))?.id == "ok:1")
        let (_, onlyAllDay) = try await seeded([allDayItem("ad", "Holiday", "2026-10-11", "2026-10-12")])
        #expect(try await onlyAllDay.nextEvent(now: iso("2026-10-11T09:30:00Z")) == nil)
    }

    @Test func itemsPerDayUseUserTimeZone() async throws {
        let late = timed("late:1", "Late", "2026-10-11T23:30:00Z", "2026-10-12T00:30:00Z")
        let (_, inLondon) = try await seeded([late], tz: london)
        #expect(try await inLondon.items(from: "2026-10-12", to: "2026-10-12").map(\.id) == ["late:1"])
        #expect(try await inLondon.items(from: "2026-10-11", to: "2026-10-11").isEmpty)
        let (_, inLA) = try await seeded([late], tz: losAngeles)
        #expect(try await inLA.items(from: "2026-10-11", to: "2026-10-11").map(\.id) == ["late:1"])
        #expect(try await inLA.items(from: "2026-10-12", to: "2026-10-12").isEmpty)
    }

    @Test func orderingAllDayFirstAndMultiDayOnce() async throws {
        let (_, repo) = try await seeded([
            timed("b:1", "B", "2026-10-11T11:00:00Z", "2026-10-11T12:00:00Z"),
            timed("a:1", "A", "2026-10-11T09:00:00Z", "2026-10-11T10:00:00Z"),
            allDayItem("ad", "Trip", "2026-10-10", "2026-10-13"),
        ])
        let day = try await repo.items(from: "2026-10-11", to: "2026-10-11")
        #expect(day.map(\.id) == ["ad", "a:1", "b:1"])
        let range = try await repo.items(from: "2026-10-10", to: "2026-10-14")
        #expect(range.filter { $0.id == "ad" }.count == 1)
        #expect(try await repo.item(id: "a:1")?.title == "A")
    }

    @Test func daysWithEventsForMonthDots() async throws {
        let (_, repo) = try await seeded([
            timed("a:1", "A", "2026-10-11T09:00:00Z", "2026-10-11T10:00:00Z"),
            allDayItem("ad", "Trip", "2026-10-20", "2026-10-23"),
            timed("c:1", "Cancelled", "2026-10-15T09:00:00Z", "2026-10-15T10:00:00Z", status: "cancelled"),
            timed("out:1", "Outside", "2026-11-05T09:00:00Z", "2026-11-05T10:00:00Z"),
        ])
        let days = try await repo.daysWithEvents(from: "2026-10-01", to: "2026-10-31")
        #expect(days == ["2026-10-11", "2026-10-20", "2026-10-21", "2026-10-22"])
        // Range clipping of a multi-day item.
        #expect(try await repo.daysWithEvents(from: "2026-10-22", to: "2026-10-25") == ["2026-10-22"])
    }

    @Test func observationEmitsOnChange() async throws {
        let (db, repo) = try await seeded([timed("a:1", "A", "2026-10-11T09:00:00Z", "2026-10-11T10:00:00Z")])
        var it = repo.observeItems(from: "2026-10-11", to: "2026-10-11").makeAsyncIterator()
        #expect(await it.next()?.map(\.id) == ["a:1"])
        _ = try await db.apply(SyncSnapshot(
            agenda: .init(items: [timed("z:1", "Z", "2026-10-11T09:00:00Z", "2026-10-11T10:00:00Z")], rev: 2, etag: "e2",
                          from: iso("2026-09-27T00:00:00Z"), to: iso("2027-01-11T00:00:00Z")),
            syncedAt: iso("2026-10-11T10:01:00Z")))
        #expect(await it.next()?.map(\.id) == ["z:1"])
    }

    @Test func reKeysExistingRowsWhenTimeZoneChanges() async throws {
        let late = timed("late:1", "Late", "2026-10-11T23:30:00Z", "2026-10-12T00:30:00Z")
        let (db, repo) = try await seeded([late], tz: london)
        #expect(try await repo.items(from: "2026-10-12", to: "2026-10-12").count == 1)
        // 304-style snapshot (no agenda) that only changes the zone.
        _ = try await db.apply(SyncSnapshot(settings: Settings(userTzid: "America/Los_Angeles"), syncedAt: iso("2026-10-11T10:05:00Z")))
        #expect(try await repo.items(from: "2026-10-12", to: "2026-10-12").isEmpty)
        #expect(try await repo.items(from: "2026-10-11", to: "2026-10-11").count == 1)
    }

    @Test func accountsAndCalendarsMirrorTheHub() async throws {
        let db = try AppDatabase.inMemory()
        let snap = { (accs: [Account], cals: [HubCalendar]) in
            SyncSnapshot(accounts: accs, calendars: cals, syncedAt: iso("2026-10-11T10:00:00Z"))
        }
        _ = try await db.apply(snap([testAccount("a"), testAccount("b")], [testCalendar("c1"), testCalendar("c2")]))
        _ = try await db.apply(snap([testAccount("a", status: "needs_reauth")], [testCalendar("c1")]))
        let accs = try await GRDBAccountRepository(db: db).accounts()
        #expect(accs.map(\.id) == ["a"] && accs[0].status == "needs_reauth")
        let cals = try await GRDBCalendarRepository(db: db, api: FakeHub()).calendars()
        #expect(cals.map(\.id) == ["c1"])
    }

    @Test func syncAndRefreshAsksTheHubToPollFirst() async throws {
        let hub = FakeHub()
        let db = try AppDatabase.inMemory()
        let repo = GRDBEventDetailRepository(db: db, api: hub, clock: .fixed(iso("2026-10-11T10:00:00Z")), settle: [.milliseconds(1), .milliseconds(1)])
        _ = try await repo.detail(id: "e1:5")
        let d = try await repo.syncAndRefresh(id: "e1:5")
        #expect(d.item.id == "e1:5")
        #expect(hub.postSyncCalls == 1)
        // The hub being unreachable still falls back to a plain read error, not a hang.
        hub.failure = APIClientError.transport("down")
        do { _ = try await repo.syncAndRefresh(id: "e1:5"); Issue.record("should throw") } catch {}
    }

    @Test func eventDetailIsCacheFirstAndInvalidatedByVersion() async throws {
        let hub = FakeHub()
        let db = try AppDatabase.inMemory()
        let repo = GRDBEventDetailRepository(db: db, api: hub, clock: .fixed(iso("2026-10-11T10:00:00Z")))
        #expect(try await repo.cached(id: "e1:5") == nil)
        let first = try await repo.detail(id: "e1:5")
        #expect(first.description == "d")
        hub.failure = APIClientError.transport("down")
        #expect(try await repo.detail(id: "e1:5").item.id == "e1:5")   // served from cache while offline
        do { _ = try await repo.refresh(id: "e1:5"); Issue.record("should throw") } catch {}
        // Window refresh carries a newer version of the event: cached detail is dropped.
        _ = try await db.apply(SyncSnapshot(
            agenda: .init(items: [timed("e1:5", "x", "2026-10-11T10:00:00Z", "2026-10-11T11:00:00Z", version: 2)], rev: 2, etag: nil,
                          from: iso("2026-09-27T00:00:00Z"), to: iso("2027-01-11T00:00:00Z")),
            syncedAt: iso("2026-10-11T10:01:00Z")))
        #expect(try await repo.cached(id: "e1:5") == nil)
    }
}
