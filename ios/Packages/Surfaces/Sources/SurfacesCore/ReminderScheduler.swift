import CalendarCore
import Foundation
import UserNotifications

/// The slice of `UNUserNotificationCenter` the scheduler needs; tests fake it.
public protocol ReminderCenter: Sendable {
    func isAuthorized() async -> Bool
    /// Pending request id -> signature, for ids starting with `prefix`.
    func pendingSignatures(prefix: String) async -> [String: String]
    func add(_ reminder: PlannedReminder) async
    func remove(ids: [String]) async
}

public struct SystemReminderCenter: ReminderCenter {
    public init() {}
    private var center: UNUserNotificationCenter { .current() }

    public func isAuthorized() async -> Bool {
        switch await center.notificationSettings().authorizationStatus {
        case .authorized, .provisional, .ephemeral: true
        default: false
        }
    }

    public func pendingSignatures(prefix: String) async -> [String: String] {
        var out: [String: String] = [:]
        for r in await center.pendingNotificationRequests() where r.identifier.hasPrefix(prefix) {
            out[r.identifier] = r.content.userInfo["sig"] as? String ?? ""
        }
        return out
    }

    public func add(_ r: PlannedReminder) async {
        let c = UNMutableNotificationContent()
        c.title = r.title
        c.body = r.body
        c.sound = .default
        c.threadIdentifier = r.thread
        if let cat = r.category { c.categoryIdentifier = cat }
        c.interruptionLevel = r.timeSensitive ? .timeSensitive : .active
        var info: [String: Any] = ["link": SurfaceLink.event(r.occurrenceId).url.absoluteString, "sig": r.signature,
                                   "eventId": r.eventId, "k": "reminder"]
        if let j = r.joinURL { info["join"] = j }
        c.userInfo = info
        let cal = Calendar.current
        let comps = cal.dateComponents([.year, .month, .day, .hour, .minute, .second], from: r.fireDate)
        let trigger = UNCalendarNotificationTrigger(dateMatching: comps, repeats: false)
        try? await center.add(UNNotificationRequest(identifier: r.id, content: c, trigger: trigger))
    }

    public func remove(ids: [String]) async {
        center.removePendingNotificationRequests(withIdentifiers: ids)
    }
}

/// Keeps the pending local reminders equal to `ReminderPlanner.plan` for the saved agenda.
/// Runs after every sync and when settings or a reminder override change.
public final class ReminderScheduler: AfterSyncHook, @unchecked Sendable {
    private let db: AppDatabase
    private let api: any HubReadAPI
    private let center: any ReminderCenter
    private let clock: AppClock
    private let showJoin: @Sendable () -> Bool
    private let gate = AsyncGate()
    /// How many uncached upcoming events may be fetched per pass to learn their reminder override.
    static let prefetchLimit = 12

    public init(db: AppDatabase, api: any HubReadAPI, center: any ReminderCenter = SystemReminderCenter(),
                clock: AppClock = .system, showJoin: @escaping @Sendable () -> Bool = { true }) {
        self.db = db; self.api = api; self.center = center; self.clock = clock; self.showJoin = showJoin
    }

    public func afterSync(_ outcome: SyncOutcome) async { await reschedule() }

    /// Recompute and apply. Concurrent calls queue behind each other.
    public func reschedule() async {
        await gate.run { await self.apply() }
    }

    func apply() async {
        guard await center.isAuthorized() else { return }
        let now = clock.now()
        guard let input = try? await load(now: now) else { return }
        let planned = ReminderPlanner.plan(input, now: now)
        let pending = await center.pendingSignatures(prefix: PlannedReminder.prefix)
        let d = ReminderPlanner.diff(planned: planned, pending: pending)
        if !d.remove.isEmpty { await center.remove(ids: d.remove) }
        for r in d.add { await center.add(r) }
    }

    func load(now: Date) async throws -> ReminderInput {
        let tz = try db.userTimeZone()
        let today = DayKey(now, in: tz)
        // All-day reminders can fire a day or more before; timed ones need a long window to fill the cap.
        let items = try await GRDBAgendaRepository(db: db).items(from: today.adding(days: -1), to: today.adding(days: 60))
        await prefetchDetails(items: items, now: now)
        let settings = try await GRDBSettingsRepository(db: db, api: api).settings()
        let accounts = try await GRDBAccountRepository(db: db).accounts()
        let overrides = try await db.reminderOverrides()
        return ReminderInput(items: items, settings: settings, overrides: overrides,
                             accountNames: Dictionary(accounts.map { ($0.id, $0.email) }, uniquingKeysWith: { a, _ in a }),
                             tz: tz, showJoin: showJoin())
    }

    /// Learn the per-event override of the next few events we have never opened (best effort, offline is fine).
    private func prefetchDetails(items: [AgendaItem], now: Date) async {
        let soon = items.filter { ($0.start ?? .distantFuture) > now && ($0.start ?? .distantFuture) < now.addingTimeInterval(48 * 3600) }
            .sorted { ($0.start ?? .distantFuture) < ($1.start ?? .distantFuture) }.map(\.id)
        guard let missing = try? await db.uncachedDetailIDs(ids: soon), !missing.isEmpty else { return }
        let repo = GRDBEventDetailRepository(db: db, api: api, clock: clock)
        for id in missing.prefix(Self.prefetchLimit) {
            do { _ = try await repo.refresh(id: id) } catch { break }   // offline or hub down: stop, try next sync
        }
    }
}

/// Serialises async work (one rescheduling pass at a time).
actor AsyncGate {
    private var tail: Task<Void, Never>?
    func run(_ work: @escaping @Sendable () async -> Void) async {
        let prev = tail
        let t = Task { await prev?.value; await work() }
        tail = t
        await t.value
    }
}
