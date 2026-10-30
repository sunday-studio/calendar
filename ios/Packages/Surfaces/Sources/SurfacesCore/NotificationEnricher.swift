import CalendarCore
import Foundation

/// What the extension reads from the hub.
public protocol NotificationHubAPI: Sendable {
    func event(id: String) async throws -> EventDetail
    /// Names an event even when it was cancelled in the last two days (the hub 404s `event(id:)` for those).
    func brief(id: String) async throws -> EventBrief
    func accounts() async throws -> [Account]
    func summary(date: String?) async throws -> DaySummary
}

extension APIClient: NotificationHubAPI {}

/// Gathers the facts for a push (hub first, saved agenda second) and hands them to `NotificationRewriter`.
public struct NotificationEnricher: Sendable {
    public let api: (any NotificationHubAPI)?
    public let db: AppDatabase?
    public let clock: AppClock
    /// Per-request budget; the extension has about 25 s in total.
    public let requestTimeout: TimeInterval

    public init(api: (any NotificationHubAPI)?, db: AppDatabase?, clock: AppClock = .system, requestTimeout: TimeInterval = 8) {
        self.api = api; self.db = db; self.clock = clock; self.requestTimeout = requestTimeout
    }

    public func notification(for p: PushPayload) async -> RewrittenNotification {
        let tz = (try? db?.userTimeZone()) ?? .current
        var ctx = RewriteContext(tz: tz, now: clock.now())
        switch p.kind {
        case .moved, .cancelled, .invited:
            guard let id = p.eventId else { return NotificationRewriter.generic(p) }
            let local = await localItem(id)
            ctx.previousStart = local?.start
            if p.kind == .moved, let d = await fetch({ try await $0.event(id: id) }) {
                ctx.event = EventFacts(d)
            } else if p.kind != .moved, let b = await fetch({ try await $0.brief(id: id) }) {
                // The brief still answers for an event cancelled in the last two days, and for an invitation the phone never saw.
                ctx.event = EventFacts(b)
                if p.kind == .cancelled { ctx.event?.cancelled = true }
            } else if let l = local {
                ctx.event = EventFacts(item: l.item, place: l.place)
                if p.kind == .cancelled { ctx.event?.cancelled = true }
            }
        case .reauth:
            if let id = p.accountId {
                if let db, let a = try? await GRDBAccountRepository(db: db).accounts().first(where: { $0.id == id }) {
                    ctx.accountEmail = a.email
                } else if let list = await fetch({ try await $0.accounts() }) {
                    ctx.accountEmail = list.first { $0.id == id }?.email
                }
            }
        case .summary:
            ctx.summary = await fetch { try await $0.summary(date: p.date) }
        case .changed, .sync:
            break
        }
        return NotificationRewriter.rewrite(p, ctx)
    }

    private func localItem(_ id: String) async -> (item: AgendaItem, start: Date?, place: String?)? {
        guard let db, let item = try? await GRDBAgendaRepository(db: db).item(id: id) else { return nil }
        let cal = try? await GRDBCalendarRepository(db: db, api: OfflineAPI()).calendars().first { $0.id == item.calendarId }
        return (item, item.start, cal?.name ?? item.location)
    }

    private func fetch<T: Sendable>(_ call: @escaping @Sendable (any NotificationHubAPI) async throws -> T) async -> T? {
        guard let api else { return nil }
        return try? await Self.withTimeout(requestTimeout) { try await call(api) }
    }

    static func withTimeout<T: Sendable>(_ seconds: TimeInterval, _ op: @escaping @Sendable () async throws -> T) async throws -> T {
        try await withThrowingTaskGroup(of: T.self) { group in
            group.addTask { try await op() }
            group.addTask { try await Task.sleep(for: .seconds(seconds)); throw CancellationError() }
            defer { group.cancelAll() }
            return try await group.next()!
        }
    }
}
