import Foundation
import GRDB
import Testing
@testable import CalendarCore

@Suite struct SyncEngineTests {
    func stored(_ db: AppDatabase) async throws -> [String] {
        try await db.writer.read { try String.fetchAll($0, sql: "SELECT id FROM occurrence_item ORDER BY id") }
    }

    @Test func refreshWritesWindowAccountsCalendarsSettingsAndMeta() async throws {
        let hub = FakeHub()
        hub.items = [timed("a:1", "A", "2026-10-11T09:00:00Z", "2026-10-11T10:00:00Z")]
        hub.rev = 4
        let hook = RecordingHook()
        let (engine, db, _) = try makeEngine(hub, hooks: [hook])
        await engine.refresh()
        #expect(try await stored(db) == ["a:1"])
        let w = SyncWindow(now: iso("2026-10-11T10:00:00Z"), tz: london)
        let call = try #require(hub.agendaCalls.first)
        #expect(call.from == w.from && call.to == w.to && call.etag == nil)
        #expect(try await GRDBAccountRepository(db: db).accounts().count == 1)
        #expect(try await GRDBCalendarRepository(db: db, api: hub).calendars().count == 1)
        #expect(try await GRDBSettingsRepository(db: db, api: hub).settings()?.userTzid == "Europe/London")
        let meta = GRDBSyncMetaRepository(db: db)
        #expect(try await meta.lastRev() == 4)
        #expect(try await meta.lastSyncAt() == iso("2026-10-11T10:00:00Z"))
        #expect(hook.rec.values == [SyncOutcome(changed: true, rev: 4, syncedAt: iso("2026-10-11T10:00:00Z"))])
        #expect(engine.currentState == .idle)
    }

    @Test func etagIsSentAndA304KeepsData() async throws {
        let hub = FakeHub()
        hub.items = [timed("a:1", "A", "2026-10-11T09:00:00Z", "2026-10-11T10:00:00Z")]
        let hook = RecordingHook()
        let (engine, db, clock) = try makeEngine(hub, hooks: [hook])
        await engine.refresh()
        clock.advance(60)
        await engine.refresh()
        let calls = hub.agendaCalls
        #expect(calls.count == 2 && calls[0].etag == nil && calls[1].etag == "W/\"1-a\"")
        #expect(try await stored(db) == ["a:1"])
        #expect(try await GRDBSyncMetaRepository(db: db).lastSyncAt() == iso("2026-10-11T10:01:00Z")) // freshness recorded
        #expect(hook.rec.values.map(\.changed) == [true, false])
        // New rev -> new ETag -> 200 replaces the rows.
        hub.items = [timed("b:1", "B", "2026-10-11T09:00:00Z", "2026-10-11T10:00:00Z")]
        hub.rev = 2
        await engine.refresh()
        #expect(try await stored(db) == ["b:1"])
    }

    @Test func etagIsDroppedWhenTheWindowRollsToANewDay() async throws {
        let hub = FakeHub()
        let (engine, _, clock) = try makeEngine(hub)
        await engine.refresh()
        clock.set(iso("2026-10-12T10:00:00Z"))
        await engine.refresh()
        #expect(hub.agendaCalls.map(\.etag) == [nil, nil])
    }

    @Test func windowReplaceIsAtomic() async throws {
        let hub = FakeHub()
        hub.items = [timed("old:1", "Old", "2026-10-11T09:00:00Z", "2026-10-11T10:00:00Z")]
        let (engine, db, _) = try makeEngine(hub)
        await engine.refresh()
        // Make the second transaction fail after the agenda rows were already replaced inside it.
        try await db.writer.write { try $0.execute(sql: "CREATE TRIGGER boom BEFORE INSERT ON calendar BEGIN SELECT RAISE(ABORT, 'boom'); END") }
        try await db.writer.write { try $0.execute(sql: "DELETE FROM calendar") }
        hub.items = [timed("new:1", "New", "2026-10-11T09:00:00Z", "2026-10-11T10:00:00Z")]
        hub.rev = 2
        await engine.refresh()
        #expect(try await stored(db) == ["old:1"])
        #expect(try await GRDBSyncMetaRepository(db: db).lastRev() == 1)
        guard case .error = engine.currentState else { Issue.record("expected error, got \(engine.currentState)"); return }
        // Recovers once the fault is gone.
        try await db.writer.write { try $0.execute(sql: "DROP TRIGGER boom") }
        await engine.refresh()
        #expect(try await stored(db) == ["new:1"])
        #expect(engine.currentState == .idle)
    }

    @Test func concurrentRefreshesCoalesceIntoOneFollowUpRun() async throws {
        let hub = FakeHub()
        let gate = AsyncStream<Void>.makeStream()
        let entered = Counter()
        hub.gate = {
            entered.inc()
            if entered.value == 1 { for await _ in gate.stream { break } } // hold the first run
        }
        let (engine, _, _) = try makeEngine(hub)
        let first = Task { await engine.refresh() }
        #expect(await eventually { hub.agendaCalls.count == 1 })
        let others = (0..<5).map { _ in Task { await engine.refresh() } }
        try await Task.sleep(for: .milliseconds(50))
        #expect(hub.agendaCalls.count == 1) // still just the first, held
        gate.continuation.yield()
        await first.value
        for t in others { await t.value }
        #expect(hub.agendaCalls.count == 2) // exactly one extra run, not five
    }

    @Test func unreachableHubKeepsDataAndReportsStale() async throws {
        let hub = FakeHub()
        hub.items = [timed("a:1", "A", "2026-10-11T09:00:00Z", "2026-10-11T10:00:00Z")]
        let (engine, db, clock) = try makeEngine(hub)
        var states = engine.state.makeAsyncIterator()
        #expect(await states.next() == .idle)
        await engine.refresh()
        clock.advance(300)
        hub.failure = APIClientError.transport("The Internet connection appears to be offline.")
        await engine.refresh()
        #expect(engine.currentState == .stale(since: iso("2026-10-11T10:00:00Z")))
        #expect(try await stored(db) == ["a:1"])
        #expect(try await GRDBAgendaRepository(db: db).nextEvent(now: iso("2026-10-11T08:00:00Z"))?.id == "a:1")
        hub.failure = nil
        await engine.refresh()
        #expect(engine.currentState == .idle)
    }

    @Test func neverSyncedAndUnreachableIsOffline() async throws {
        let hub = FakeHub(); hub.failure = APIClientError.tls("bad cert")
        let (engine, _, _) = try makeEngine(hub)
        await engine.refresh()
        #expect(engine.currentState == .offline)
    }

    @Test func hubErrorsSurfaceAsErrorState() async throws {
        let hub = FakeHub()
        let (engine, _, _) = try makeEngine(hub)
        hub.failure = APIClientError.server(status: 500, APIError(error: .init(code: "internal", message: "db locked", field: nil)))
        await engine.refresh()
        #expect(engine.currentState == .error("db locked"))
        hub.failure = APIClientError.unauthorized(nil)
        await engine.refresh()
        guard case .error = engine.currentState else { Issue.record("expected error"); return }
    }

    @Test func stateStreamPublishesSyncingThenIdle() async throws {
        let hub = FakeHub()
        let (engine, _, _) = try makeEngine(hub)
        let seen = Recorder<SyncState>()
        let sub = engine.state
        let task = Task { for await s in sub { seen.add(s) } }
        #expect(await eventually { seen.values.first == .idle })
        await engine.refresh()
        #expect(await eventually { seen.values.last == .idle && seen.values.contains(.syncing) })
        task.cancel()
    }

    @Test func manualRefreshPostsSyncThenRefreshes() async throws {
        let hub = FakeHub()
        let (engine, _, _) = try makeEngine(hub)
        await engine.manualRefresh()
        #expect(hub.postSyncCalls == 1 && hub.agendaCalls.count == 1)
    }

    @Test func silentPushReportsWhetherDataChanged() async throws {
        let hub = FakeHub()
        let (engine, _, _) = try makeEngine(hub)
        #expect(await engine.silentPush() == true)
        #expect(await engine.silentPush() == false) // 304
        hub.rev = 9
        #expect(await engine.silentPush() == true)
    }

    @Test func streamRevAboveLastRevTriggersRefresh() async throws {
        let hub = FakeHub()
        let tx = Recorder<AsyncThrowingStream<StreamEvent, Error>.Continuation>()
        hub.stream = { _, _ in AsyncThrowingStream { c in tx.add(c) } }
        let (engine, _, _) = try makeEngine(hub)
        await engine.appForeground()
        #expect(hub.agendaCalls.count == 1)
        #expect(await eventually { !tx.values.isEmpty })
        let c = try #require(tx.values.first)
        c.yield(StreamEvent(id: "1", kind: .rev(1)))        // == lastRev: ignored
        c.yield(StreamEvent(id: nil, kind: .ping))
        try await Task.sleep(for: .milliseconds(80))
        #expect(hub.agendaCalls.count == 1)
        hub.rev = 2
        c.yield(StreamEvent(id: "2", kind: .rev(2)))
        #expect(await eventually { hub.agendaCalls.count == 2 })
        await engine.appBackground()
        #expect(await engine.isStreaming == false)
    }

    @Test func streamingStartsOnlyWhileForegrounded() async throws {
        let hub = FakeHub()
        let (engine, _, _) = try makeEngine(hub)
        #expect(await engine.isStreaming == false)
        await engine.appForeground()
        #expect(await engine.isStreaming == true)
        await engine.appBackground()
        #expect(await engine.isStreaming == false)
        // Resumes from the stored rev on the next foreground.
        await engine.appForeground()
        #expect(await eventually { hub.streamLastEventIDs.count >= 2 })
        #expect(hub.streamLastEventIDs.last == "1")
        await engine.appBackground()
    }
}

@Suite struct LiveStreamTests {
    @Test func reconnectsWithLastEventIDAndBackoff() async throws {
        let delays = Recorder<TimeInterval>()
        let ids = Recorder<String?>()
        let n = Counter()
        let ls = LiveStream(
            backoff: Backoff(initial: 1, cap: 4, jitter: 0),
            connect: { last in
                ids.add(last)
                n.inc()
                let attempt = n.value
                return AsyncThrowingStream { c in
                    switch attempt {
                    case 1: c.yield(StreamEvent(id: "5", kind: .rev(5))); c.finish(throwing: URLError(.networkConnectionLost))
                    case 2, 3, 4: c.finish(throwing: URLError(.cannotConnectToHost))
                    default: c.yield(StreamEvent(id: "9", kind: .rev(9))) // stays open
                    }
                }
            },
            sleep: { delays.add($0) })
        var it = ls.events.makeAsyncIterator()
        await ls.start()
        #expect(await it.next() == StreamEvent(id: "5", kind: .rev(5)))
        #expect(await it.next() == StreamEvent(id: "9", kind: .rev(9)))
        // A failure after an event restarts at 1 s; then 2, 4, 4 (cap) while nothing arrives.
        #expect(Array(delays.values.prefix(4)) == [1, 2, 4, 4])
        #expect(ids.values == [nil, "5", "5", "5", "5"])
        await ls.stop()
        #expect(await it.next() == nil)
    }

    @Test func backoffDoublesCapsAndJitters() {
        let b = Backoff(initial: 1, cap: 30, jitter: 0)
        #expect((0..<8).map { b.delay(attempt: $0) } == [1, 2, 4, 8, 16, 30, 30, 30])
        let j = Backoff(initial: 1, cap: 30, jitter: 0.2)
        for a in 0..<6 { let d = j.delay(attempt: a); let base = min(30, pow(2, Double(a))); #expect(d >= base * 0.8 && d <= base * 1.2) }
    }
}

@Suite struct PreviewEnvironmentTests {
    @Test func seedsFromFixtureJSON() async throws {
        let env = try PreviewEnvironment.make(
            agendaJSON: fixture("agenda"), accountsJSON: fixture("accounts"), calendarsJSON: fixture("calendars"),
            settingsJSON: Data(#"{"defaultCalendarId":null,"weekStart":1,"userTzid":"Europe/London","workingHours":{"start":"09:00","end":"17:30","days":[1,2,3,4,5]}}"#.utf8),
            clock: .fixed(iso("2026-10-11T08:00:00Z")))
        #expect(try await env.accounts.accounts().count == 3)
        #expect(try await env.calendars.calendars().isEmpty == false)
        let day = try await env.agenda.items(from: "2026-10-11", to: "2026-10-11")
        #expect(!day.isEmpty)
        #expect(try await env.agenda.nextEvent(now: iso("2026-10-11T08:00:00Z")) != nil)
        #expect(try await env.syncMeta.lastRev() == 1)
        _ = try PreviewEnvironment.sample()
    }
}
