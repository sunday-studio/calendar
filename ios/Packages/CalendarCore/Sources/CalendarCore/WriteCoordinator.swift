import Foundation
import GRDB

/// `optimistic`: change the local agenda first, roll back on failure (drag, resize, move on the grid).
/// `confirmed`: touch nothing locally until the provider confirms (create, edit form, RSVP, delete).
public enum WriteMode: Sendable { case optimistic, confirmed }

public enum WriteOutcome: Equatable, Sendable {
    /// Stored by the hub. `nil` for deletes.
    case saved(EventDetail?)
    /// Rolled back. For `.conflict` the local cache already holds the hub's current event.
    case failed(WriteFailure)
}

/// Pure edits of agenda rows used for the optimistic step (and unit-tested on their own).
public enum OptimisticEdit {
    /// Rows the scope touches. `following` = this occurrence and every later one of the same event.
    public static func targets(scope: EventScope, item: AgendaItem, in rows: [AgendaItem]) -> [AgendaItem] {
        switch scope {
        case .this: return rows.filter { $0.id == item.id }
        case .all: return rows.filter { $0.eventId == item.eventId }
        case .following:
            return rows.filter { $0.eventId == item.eventId && order($0) >= order(item) }
        }
    }

    private static func order(_ i: AgendaItem) -> String {
        i.allDay ? (i.date ?? "") : String(format: "%020.3f", (i.start ?? .distantPast).timeIntervalSince1970 + 1e10)
    }

    /// Applies what can be shown without the hub: title, location, time and day (shifted by the same delta for
    /// scope all/following), calendar. Recurrence, guests, conference and reminders are left to the reconcile.
    public static func apply(_ patch: EventPatch, to row: AgendaItem, anchor: AgendaItem, scope: EventScope) -> AgendaItem {
        var r = row
        if case .set(let t) = patch.title { r.title = t.trimmingCharacters(in: .whitespacesAndNewlines) }
        switch patch.location { case .set(let l): r.location = l; case .clear: r.location = nil; case .unchanged: break }
        if case .set(let w) = patch.when { applyWhen(w, to: &r, anchor: anchor, wholeScope: scope != .this) }
        return r
    }

    private static func applyWhen(_ w: EventWhen, to r: inout AgendaItem, anchor: AgendaItem, wholeScope: Bool) {
        if w.allDay {
            guard let d = w.date, let e = w.endDate else { return }
            let length = DayKey(d).days(to: DayKey(e))
            if r.allDay && wholeScope, let ad = anchor.date {
                let shift = DayKey(ad).days(to: DayKey(d))
                let nd = DayKey(r.date ?? d).adding(days: shift)
                r.date = nd.rawValue; r.endDate = nd.adding(days: length).rawValue
            } else {
                r.date = d; r.endDate = e
            }
            r.allDay = true; r.start = nil; r.end = nil; r.tzid = nil
        } else {
            guard let s = w.startDate(), let e = w.endInstant() else { return }
            if !r.allDay, wholeScope, let anchorStart = anchor.start, let rs = r.start {
                let ns = rs.addingTimeInterval(s.timeIntervalSince(anchorStart))
                r.start = ns; r.end = ns.addingTimeInterval(e.timeIntervalSince(s))
            } else {
                r.start = s; r.end = e
            }
            r.allDay = false; r.tzid = w.tzid; r.date = nil; r.endDate = nil
        }
    }
}

/// Optimistic local apply -> hub call -> reconcile or roll back.
///
/// One user action = one `Idempotency-Key`, reused for retries after an unknown outcome (lost response,
/// timeout), so the hub never performs the action twice. There is no offline outbox: when the hub can't be
/// reached after the retries the change is rolled back and `failed(.offline)` is returned.
public actor WriteCoordinator {
    public typealias Sleep = @Sendable (TimeInterval) async throws -> Void

    private let api: any HubWriteAPI
    private let db: AppDatabase
    private let refresh: @Sendable () async -> Void
    private let clock: AppClock
    private let sleep: Sleep
    private let makeKey: @Sendable () -> String
    private let retryDelays: [TimeInterval]

    public init(
        api: any HubWriteAPI, db: AppDatabase, refresh: @escaping @Sendable () async -> Void = {},
        clock: AppClock = .system,
        sleep: @escaping Sleep = { try await Task.sleep(for: .seconds($0)) },
        makeKey: @escaping @Sendable () -> String = { UUID().uuidString },
        retryDelays: [TimeInterval] = [0.5, 2]
    ) {
        self.api = api; self.db = db; self.refresh = refresh; self.clock = clock; self.sleep = sleep
        self.makeKey = makeKey; self.retryDelays = retryDelays
    }

    /// A fresh key for a new user action; callers keep it to re-send the SAME action after a failure.
    public nonisolated func newKey() -> String { makeKey() }

    // MARK: Actions

    public func create(_ draft: EventDraft, key: String? = nil, mode: WriteMode = .optimistic) async -> WriteOutcome {
        let issues = draft.validate()
        if let i = issues.first { return .failed(.validation(field: i.field, message: i.message)) }
        let key = key ?? makeKey()
        var journal = Journal()
        if mode == .optimistic, let tz = try? db.userTimeZone(), let temp = try? await tempItem(for: draft, key: key) {
            journal.insert(OccurrenceRecord(temp, calendar: DayKey.calendar(tz)))
        }
        if mode == .optimistic { await stage(&journal) }
        return await run(journal: journal, call: { try await self.api.createEvent(draft, key: key) })
    }

    public func update(_ item: AgendaItem, patch: EventPatch, scope: EventScope = .this, key: String? = nil,
                       mode: WriteMode = .optimistic) async -> WriteOutcome {
        let issues = patch.validate()
        if let i = issues.first { return .failed(.validation(field: i.field, message: i.message)) }
        let key = key ?? makeKey()
        var journal = Journal()
        if mode == .optimistic { await edit(&journal, item: item, scope: scope) { OptimisticEdit.apply(patch, to: $0, anchor: item, scope: scope) } }
        return await run(journal: journal, call: {
            try await self.api.patchEvent(id: item.id, patch: patch, version: item.version, scope: scope, key: key)
        })
    }

    public func delete(_ item: AgendaItem, scope: EventScope = .this, key: String? = nil, mode: WriteMode = .optimistic) async -> WriteOutcome {
        let key = key ?? makeKey()
        var journal = Journal()
        if mode == .optimistic, let rows = try? await rows(eventId: item.eventId) {
            for r in OptimisticEdit.targets(scope: scope, item: item, in: rows) {
                if let orig = try? await db.writer.read({ try OccurrenceRecord.fetchOne($0, key: r.id) }) { journal.remove(orig) }
            }
        }
        if mode == .optimistic { await stage(&journal) }
        let out = await run(journal: journal, call: {
            try await self.api.deleteEvent(id: item.id, version: item.version, scope: scope, key: key)
            return nil as EventDetail?
        })
        return out
    }

    public func rsvp(_ item: AgendaItem, response: RSVPResponse, note: String? = nil, scope: EventScope = .this,
                     key: String? = nil, mode: WriteMode = .optimistic) async -> WriteOutcome {
        if scope == .following {   // hub: 400 validation on any event
            return .failed(.validation(field: "scope", message: "Replies can be for this event or the whole series."))
        }
        let key = key ?? makeKey()
        var journal = Journal()
        let wire = response.rawValue
        if mode == .optimistic { await edit(&journal, item: item, scope: scope) { var r = $0; r.myResponse = wire; return r } }
        return await run(journal: journal, call: {
            try await self.api.rsvp(id: item.id, response: response, note: note, scope: scope, key: key)
        })
    }

    public func addMeet(_ item: AgendaItem, key: String? = nil) async -> WriteOutcome {
        let key = key ?? makeKey()
        return await run(journal: Journal(), call: { try await self.api.addMeet(id: item.id, key: key) })
    }

    public func move(_ item: AgendaItem, toCalendar calendarId: String, key: String? = nil, mode: WriteMode = .optimistic) async -> WriteOutcome {
        let key = key ?? makeKey()
        var journal = Journal()
        let target = try? await db.writer.read { try HubCalendar.fetchOne($0, key: calendarId) }
        if mode == .optimistic { await edit(&journal, item: item, scope: .all) { r in
            var n = r
            n.calendarId = calendarId
            if let t = target { n.accountId = t.accountId; n.color = t.colorOverride ?? t.color }
            return n
        } }
        return await run(journal: journal, call: { try await self.api.moveEvent(id: item.id, calendarId: calendarId, key: key) })
    }

    /// `nil` clears the per-event override.
    public func setReminder(_ item: AgendaItem, minutesBefore: Int?) async -> WriteOutcome {
        if let m = minutesBefore, !(0...40_320).contains(m) {
            return .failed(.validation(field: "reminderMin", message: "Reminders can be up to 4 weeks ahead."))
        }
        return await run(journal: Journal(), call: { try await self.api.setReminder(id: item.id, minutesBefore: minutesBefore) })
    }

    // MARK: Engine

    private struct Journal {
        var before: [String: OccurrenceRecord?] = [:]   // id -> original (nil = did not exist)
        var after: [String: OccurrenceRecord?] = [:]    // id -> optimistic value (nil = removed)
        var isEmpty: Bool { after.isEmpty }
        mutating func insert(_ r: OccurrenceRecord) { before[r.id] = .some(nil); after[r.id] = .some(r) }
        mutating func remove(_ orig: OccurrenceRecord) { before[orig.id] = .some(orig); after[orig.id] = .some(nil) }
    }

    /// Reads rows of one event, applies `change` to the scope's rows, records the journal and writes it.
    private func edit(_ journal: inout Journal, item: AgendaItem, scope: EventScope, _ change: (AgendaItem) -> AgendaItem) async {
        guard let tz = try? db.userTimeZone(), let all = try? await rows(eventId: item.eventId) else { return }
        let cal = DayKey.calendar(tz)
        for row in OptimisticEdit.targets(scope: scope, item: item, in: all) {
            guard let orig = try? await db.writer.read({ try OccurrenceRecord.fetchOne($0, key: row.id) }) else { continue }
            var rec = OccurrenceRecord(change(row), calendar: cal)
            rec.id = row.id
            rec.version = orig.version
            journal.before[row.id] = .some(orig); journal.after[row.id] = .some(rec)
        }
        await stage(&journal)
    }

    private func rows(eventId: String) async throws -> [AgendaItem] {
        try await db.writer.read { try OccurrenceRecord.filter(Column("eventId") == eventId).fetchAll($0).map(\.item) }
    }

    private func stage(_ journal: inout Journal) async {
        guard !journal.isEmpty else { return }
        let j = journal
        try? await db.writer.write { dbc in
            for (id, rec) in j.after {
                if let rec { try rec.save(dbc) } else { _ = try OccurrenceRecord.deleteOne(dbc, key: id) }
            }
        }
    }

    private func rollback(_ j: Journal) async {
        guard !j.isEmpty else { return }
        try? await db.writer.write { dbc in
            for (id, orig) in j.before {
                let current = try OccurrenceRecord.fetchOne(dbc, key: id)
                // A refresh may have replaced the row since; only undo what is still our own optimistic value.
                guard current == j.after[id] ?? nil else { continue }
                if let o = orig { try o.save(dbc) } else { _ = try OccurrenceRecord.deleteOne(dbc, key: id) }
            }
        }
    }

    private func run(journal: Journal, call: @Sendable () async throws -> EventDetail?) async -> WriteOutcome {
        var attempt = 0
        while true {
            do {
                let detail = try await call()
                await reconcile(journal, server: detail)
                await refresh()
                return .saved(detail)
            } catch {
                let failure = WriteFailure.classify(error)
                if failure.isRetryable, attempt < retryDelays.count {
                    try? await sleep(retryDelays[attempt]); attempt += 1
                    continue
                }
                await rollback(journal)
                if case .conflict(let current?) = failure { await store(current) }
                await refresh()
                return .failed(failure)
            }
        }
    }

    /// Server truth replaces the optimistic rows it covers; the follow-up refresh fixes the rest of a series.
    private func reconcile(_ j: Journal, server: EventDetail?) async {
        let tz = (try? db.userTimeZone()) ?? .current
        let tempIDs = j.before.filter { $0.value == nil }.map(\.key)
        try? await db.writer.write { dbc in
            for id in tempIDs { _ = try OccurrenceRecord.deleteOne(dbc, key: id) }
        }
        if let server {
            if !tempIDs.isEmpty {   // created: show the real row at once instead of waiting for the refresh
                try? await db.writer.write { try OccurrenceRecord(server.item, calendar: DayKey.calendar(tz)).save($0) }
            }
            await store(server, tz: tz)
        }
    }

    /// Cache the event and, if its occurrence is in the window, its row.
    private func store(_ d: EventDetail, tz: TimeZone? = nil) async {
        let tz = tz ?? (try? db.userTimeZone()) ?? .current
        let now = clock.now()
        try? await db.writer.write { dbc in
            let cal = DayKey.calendar(tz)
            if try OccurrenceRecord.fetchOne(dbc, key: d.item.id) != nil { try OccurrenceRecord(d.item, calendar: cal).save(dbc) }
            if let json = try? JSONEncoder.cache.encode(d) {
                try EventDetailRecord(id: d.item.id, eventId: d.item.eventId, version: d.item.version, json: json, fetchedAt: now).save(dbc)
            }
        }
    }

    private func tempItem(for d: EventDraft, key: String) async throws -> AgendaItem {
        let cal = try await db.writer.read { try HubCalendar.fetchOne($0, key: d.calendarId) }
        var item = AgendaItem(id: "pending:\(key)", eventId: "pending:\(key)", version: 0, calendarId: d.calendarId,
                              accountId: cal?.accountId ?? "", color: cal.map { $0.colorOverride ?? $0.color } ?? "cobalt",
                              title: d.title.trimmingCharacters(in: .whitespacesAndNewlines), allDay: d.when.allDay,
                              location: d.location, recurring: d.recurrence != nil, canEdit: false)
        if d.when.allDay { item.date = d.when.date; item.endDate = d.when.endDate }
        else { item.start = d.when.startDate(); item.end = d.when.endInstant(); item.tzid = d.when.tzid }
        return item
    }
}

/// What view models use to write; tests pass fakes.
public protocol EventWriting: Sendable {
    func create(_ draft: EventDraft, key: String?, mode: WriteMode) async -> WriteOutcome
    func update(_ item: AgendaItem, patch: EventPatch, scope: EventScope, key: String?, mode: WriteMode) async -> WriteOutcome
    func delete(_ item: AgendaItem, scope: EventScope, key: String?, mode: WriteMode) async -> WriteOutcome
    func rsvp(_ item: AgendaItem, response: RSVPResponse, note: String?, scope: EventScope, key: String?, mode: WriteMode) async -> WriteOutcome
    func addMeet(_ item: AgendaItem, key: String?) async -> WriteOutcome
    func move(_ item: AgendaItem, toCalendar calendarId: String, key: String?, mode: WriteMode) async -> WriteOutcome
    func setReminder(_ item: AgendaItem, minutesBefore: Int?) async -> WriteOutcome
}

extension WriteCoordinator: EventWriting {}
