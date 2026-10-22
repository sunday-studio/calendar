import CalendarCore
import Foundation
import Observation

/// The slice of the hub API the status screen uses; `APIClient` conforms.
public protocol StatusSource: Sendable {
    func status() async throws -> StatusDTO
    func syncRuns(limit: Int?, accountId: String?) async throws -> [SyncRun]
    func postSync(accountId: String?) async throws -> Int
}

/// Lets the app's `any HubReadAPI` serve as a `StatusSource`.
struct HubStatusSource: StatusSource {
    let api: any HubReadAPI
    func status() async throws -> StatusDTO { try await api.status() }
    func syncRuns(limit: Int?, accountId: String?) async throws -> [SyncRun] { try await api.syncRuns(limit: limit, accountId: accountId) }
    func postSync(accountId: String?) async throws -> Int { try await api.postSync(accountId: accountId) }
}

public struct StatusDependencies: Sendable {
    public var source: any StatusSource
    public var sync: any SyncControlling
    public var syncMeta: any SyncMetaRepository
    public var clock: AppClock
    public init(source: any StatusSource, sync: any SyncControlling, syncMeta: any SyncMetaRepository, clock: AppClock = .system) {
        self.source = source; self.sync = sync; self.syncMeta = syncMeta; self.clock = clock
    }
    public init(env: AppEnvironment) { self.init(source: HubStatusSource(api: env.api), sync: env.sync, syncMeta: env.syncMeta, clock: env.clock) }
}

public struct StatusAccountRow: Equatable, Identifiable, Sendable {
    public var id: String
    public var email: String
    public var word: String
    public var needsAttention: Bool
    public var detail: String
}

public struct SyncRunRow: Equatable, Identifiable, Sendable {
    public var id: Int64
    public var accountId: String
    public var when: String
    public var summary: String
    public var failed: Bool
}

/// Hub status + sync log. Works from the device's own sync state when the hub can't be reached.
@MainActor @Observable
public final class StatusViewModel {
    public enum Load: Equatable, Sendable { case loading, loaded, unreachable(String) }

    public private(set) var load: Load = .loading
    public private(set) var banner: ConnectionBanner?
    public private(set) var hubVersion = ""
    public private(set) var databaseSize = ""
    public private(set) var connectedDevices = 0
    public private(set) var warnings: [String] = []
    public private(set) var accounts: [StatusAccountRow] = []
    public private(set) var runs: [SyncRunRow] = []
    public private(set) var isSyncing = false

    private let deps: StatusDependencies
    private var syncState: SyncState = .idle
    private var lastSyncAt: Date?
    private var tasks: [Task<Void, Never>] = []

    public init(deps: StatusDependencies) { self.deps = deps }

    public func start() {
        guard tasks.isEmpty else { return }
        tasks.append(Task { [weak self, deps] in
            for await s in deps.sync.state { self?.syncState = s; self?.updateBanner() }
        })
        tasks.append(Task { [weak self, deps] in
            for await t in deps.syncMeta.observeLastSyncAt() { self?.lastSyncAt = t; self?.updateBanner() }
        })
    }

    public func stop() { tasks.forEach { $0.cancel() }; tasks = [] }

    public func refresh(runLimit: Int = 20) async {
        let now = deps.clock.now()
        // Reachability comes from the transport: a hub that answers with an HTTP error is reachable, just unable to
        // give us this one thing. Show what we have instead of calling the server down.
        var unreachable: Error?
        var loadedAny = false
        do {
            let status = try await deps.source.status()
            hubVersion = status.hubVersion
            databaseSize = SettingsLogic.byteSize(status.dbSizeBytes)
            connectedDevices = status.sseClients
            warnings = status.warnings.map(\.message)
            accounts = status.accounts.map { Self.row($0, now: now) }
            loadedAny = true
        } catch { if Self.isUnreachable(error) { unreachable = error } }
        if unreachable == nil {
            do { runs = try await deps.source.syncRuns(limit: runLimit, accountId: nil).map { Self.row($0, now: now) }; loadedAny = true }
            catch { if Self.isUnreachable(error) { unreachable = error } }
        }
        if let unreachable { load = .unreachable(CalendarsViewModel.message(unreachable)) } else { load = .loaded }
        _ = loadedAny
        updateBanner()
    }

    static func isUnreachable(_ e: Error) -> Bool {
        if case APIClientError.transport = e { return true }
        if case APIClientError.tls = e { return true }
        return false
    }

    /// Asks the hub to sync every account now, then reloads the log.
    public func syncNow() async {
        guard !isSyncing else { return }
        isSyncing = true
        defer { isSyncing = false }
        _ = try? await deps.source.postSync(accountId: nil)
        await deps.sync.manualRefresh()
        await refresh()
    }

    private func updateBanner() { banner = ConnectionBanner.make(state: syncState, lastSyncAt: lastSyncAt, now: deps.clock.now()) }

    static func row(_ a: AccountSyncStatus, now: Date) -> StatusAccountRow {
        var detail: String
        if SettingsLogic.isAttention(a.status) {
            detail = a.lastError ?? (a.status == "needs_reauth" ? "Sign in again to keep syncing." : "Sync is failing.")
            if a.failCount > 1 { detail += " (\(a.failCount) tries)" }
        } else if let ok = a.lastOkAt {
            detail = "Synced \(SettingsLogic.ago(ok, now: now))"
        } else {
            detail = "Not synced yet"
        }
        return .init(id: a.accountId, email: a.email, word: SettingsLogic.statusWord(a.status),
                     needsAttention: SettingsLogic.isAttention(a.status), detail: detail)
    }

    static func row(_ r: SyncRun, now: Date) -> SyncRunRow {
        let failed = r.errorKind != nil || r.error != nil
        let summary: String
        if failed { summary = r.error ?? "Failed" }
        else if r.added + r.changed + r.removed == 0 { summary = "No changes" }
        else {
            summary = [(r.added, "added"), (r.changed, "changed"), (r.removed, "removed")]
                .filter { $0.0 > 0 }.map { "\($0.0) \($0.1)" }.joined(separator: ", ")
        }
        return .init(id: r.id, accountId: r.accountId, when: SettingsLogic.ago(r.startedAt, now: now), summary: summary, failed: failed)
    }
}
