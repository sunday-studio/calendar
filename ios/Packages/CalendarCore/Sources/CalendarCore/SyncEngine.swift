import Foundation
import GRDB

public enum SyncState: Equatable, Sendable {
    case idle
    case syncing
    /// Hub unreachable and nothing has ever been synced.
    case offline
    /// Hub answered with an error (auth, server, bad payload).
    case error(String)
    /// Hub unreachable but cached data exists; `since` = last successful sync.
    case stale(since: Date)
}

public struct SyncOutcome: Equatable, Sendable {
    /// False when the hub answered 304 (nothing new).
    public var changed: Bool
    public var rev: Int64
    public var syncedAt: Date
}

/// Runs after every successful refresh (widgets, notification scheduling, Live Activities later).
public protocol AfterSyncHook: Sendable {
    func afterSync(_ outcome: SyncOutcome) async
}

/// What the UI layer needs from the engine; `PreviewSyncController` fakes it.
public protocol SyncControlling: Sendable {
    var state: AsyncStream<SyncState> { get }
    func appForeground() async
    func appBackground() async
    func manualRefresh() async
    @discardableResult func silentPush() async -> Bool
}

/// Fan-out of values to many `AsyncStream` subscribers; replays the current value on subscribe.
final class Broadcaster<T: Sendable>: @unchecked Sendable {
    private let lock = NSLock()
    private var value: T
    private var subs: [UUID: AsyncStream<T>.Continuation] = [:]
    init(_ initial: T) { value = initial }
    var current: T { lock.withLock { value } }
    func send(_ v: T) {
        let targets = lock.withLock { value = v; return Array(subs.values) }
        for c in targets { c.yield(v) }
    }
    func stream() -> AsyncStream<T> {
        AsyncStream(bufferingPolicy: .bufferingNewest(1)) { cont in
            let id = UUID()
            let cur = lock.withLock { subs[id] = cont; return value }
            cont.yield(cur)
            cont.onTermination = { [weak self] _ in self?.lock.withLock { self?.subs[id] = nil } }
        }
    }
}

public actor SyncEngine: SyncControlling {
    private let api: any HubReadAPI
    private let db: AppDatabase
    private let clock: AppClock
    private let hooks: [any AfterSyncHook]
    private let backoff: Backoff
    private let sleep: LiveStream.Sleep
    private let states = Broadcaster<SyncState>(.idle)

    private var running: Task<Void, Never>?
    private var rerun = false
    private var lastRev: Int64?
    private var live: LiveStream?
    private var consumer: Task<Void, Never>?
    private var lastOutcome: SyncOutcome?

    public init(
        api: any HubReadAPI, db: AppDatabase, clock: AppClock = .system, hooks: [any AfterSyncHook] = [],
        backoff: Backoff = Backoff(), sleep: @escaping LiveStream.Sleep = { try await Task.sleep(for: .seconds($0)) }
    ) {
        self.api = api; self.db = db; self.clock = clock; self.hooks = hooks
        self.backoff = backoff; self.sleep = sleep
    }

    // MARK: State

    public nonisolated var state: AsyncStream<SyncState> { states.stream() }
    public nonisolated var currentState: SyncState { states.current }

    // MARK: Triggers

    /// Foreground: open the stream, then refresh.
    public func appForeground() async {
        await startStreaming()
        await refresh()
    }

    /// Background: SSE only runs while foregrounded.
    public func appBackground() async { await stopStreaming() }

    /// Pull to refresh: ask the hub to poll providers now, then re-read the window.
    public func manualRefresh() async {
        _ = try? await api.postSync(accountId: nil)
        await refresh()
    }

    /// Silent push / background fetch. Returns whether new data was stored.
    @discardableResult
    public func silentPush() async -> Bool {
        await refresh()
        return lastOutcome?.changed ?? false
    }

    /// Re-read the window. Concurrent calls coalesce: one run in flight plus at most one follow-up run.
    public func refresh() async {
        if let t = running {
            rerun = true
            await t.value
            return
        }
        let t = Task { await self.runLoop() }
        running = t
        await t.value
    }

    private func runLoop() async {
        repeat {
            rerun = false
            await refreshOnce()
        } while rerun
        running = nil
    }

    // MARK: Streaming

    public func startStreaming() async {
        guard live == nil else { return }
        let rev = await loadLastRev()
        let api = self.api
        let ls = LiveStream(
            lastEventID: rev > 0 ? String(rev) : nil, backoff: backoff,
            connect: { api.openStream(lastEventID: $0) }, sleep: sleep)
        live = ls
        let events = ls.events
        consumer = Task { [weak self] in
            for await e in events {
                guard let self else { return }
                await self.handle(e)
            }
        }
        await ls.start()
    }

    public func stopStreaming() async {
        consumer?.cancel(); consumer = nil
        await live?.stop(); live = nil
    }

    public var isStreaming: Bool { live != nil }

    func handle(_ e: StreamEvent) async {
        switch e.kind {
        case .rev(let r):
            let known = await loadLastRev()
            if r > known { await refresh() }
        case .account: await refresh()
        case .sync, .ping: break
        }
    }

    // MARK: One refresh

    private func loadLastRev() async -> Int64 {
        if let lastRev { return lastRev }
        let v = (try? await db.writer.read { try $0.kvGet(KVKey.lastRev).flatMap(Int64.init) }) ?? 0
        lastRev = v
        return v
    }

    private struct Stored: Sendable { var tz: TimeZone; var tzKnown: Bool; var etag: String?; var from: Int64?; var to: Int64? }

    private func refreshOnce() async {
        states.send(.syncing)
        do {
            let stored: Stored = try await db.writer.read { d in
                let settings = try d.kvGet(KVKey.settings).flatMap { try? JSONDecoder.cache.decode(Settings.self, from: Data($0.utf8)) }
                let tzID = try d.kvGet(KVKey.userTz)
                return Stored(tz: AppDatabase.resolveTimeZone(settings: settings, stored: tzID), tzKnown: tzID != nil,
                              etag: try d.kvGet(KVKey.etag),
                              from: try d.kvGet(KVKey.windowFrom).flatMap(Int64.init),
                              to: try d.kvGet(KVKey.windowTo).flatMap(Int64.init))
            }
            let now = clock.now()
            // First run: learn the user's zone before choosing the window, so the window (and ETag) are right.
            var firstSettings: Settings?
            var tz = stored.tz
            if !stored.tzKnown {
                firstSettings = try await api.settings()
                tz = AppDatabase.resolveTimeZone(settings: firstSettings, stored: nil)
            }
            let w = SyncWindow(now: now, tz: tz)
            let sameWindow = stored.from == Int64(w.from.timeIntervalSince1970) && stored.to == Int64(w.to.timeIntervalSince1970)
            let etag = sameWindow ? stored.etag : nil

            let result = try await api.agenda(from: w.from, to: w.to, etag: etag)
            let snap: SyncSnapshot
            var changed = false
            var rev = await loadLastRev()
            switch result {
            case .notModified:
                // Nothing in the window moved; account sync status still changes without a rev bump.
                snap = SyncSnapshot(accounts: try await api.accounts(), syncedAt: now)
            case .modified(let resp, let newEtag):
                async let accounts = api.accounts()
                async let calendars = api.calendars()
                async let settings = firstSettings != nil ? firstSettings! : api.settings()
                let (a, c, s) = try await (accounts, calendars, settings)
                changed = true
                rev = resp.rev
                snap = SyncSnapshot(
                    agenda: .init(items: resp.items, rev: resp.rev, etag: newEtag, from: w.from, to: w.to),
                    accounts: a, calendars: c, settings: s, syncedAt: now)
            }
            try await db.apply(snap)
            lastRev = rev
            let outcome = SyncOutcome(changed: changed, rev: rev, syncedAt: now)
            lastOutcome = outcome
            states.send(.idle)
            for h in hooks { await h.afterSync(outcome) }
        } catch is CancellationError {
            states.send(await resting())
        } catch let e as APIClientError {
            switch e {
            case .transport, .tls: states.send(await resting())
            case .unauthorized: states.send(.error("Sign-in rejected by the hub"))
            case .server(let status, let body): states.send(.error(body?.message ?? "Hub error (\(status))"))
            case .notFound(let body): states.send(.error(body?.message ?? "Not found"))
            case .decoding(let m): states.send(.error("Unexpected response: \(m)"))
            case .invalidResponse: states.send(.error("Invalid response from the hub"))
            }
        } catch {
            states.send(.error(String(describing: error)))
        }
    }

    /// Unreachable: stale when something was synced before (data stays), offline otherwise.
    private func resting() async -> SyncState {
        let at = try? await db.writer.read { try $0.kvGet(KVKey.lastSyncAt).flatMap(Double.init) }
        if let at { return .stale(since: Date(timeIntervalSince1970: at)) }
        return .offline
    }
}
