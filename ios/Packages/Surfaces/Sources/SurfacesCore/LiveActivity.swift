import CalendarCore
import Foundation
#if canImport(ActivityKit)
import ActivityKit
#endif

/// Pure rules for the "meeting in progress" Live Activity (spec w-live).
public enum LiveActivityPlanner {
    /// Starts this long before the event (spec: 30 minutes).
    public static let lead: TimeInterval = 30 * 60
    /// "Ended 10:30" stays this long after the event.
    public static let endedLinger: TimeInterval = 5 * 60

    public struct Desired: Equatable, Sendable {
        public var eventId: String
        public var occurrenceId: String
        public var title: String
        public var start: Date
        public var end: Date
        public var color: String
        public var joinURL: String?
        public var joinKind: String?
        public var location: String?
        public var running: Bool
        /// When the lock-screen copy must flip ("in 8 min" -> "Now"), then end.
        public var staleDate: Date { running ? end : start }
    }

    /// Events qualify with a join link or a location. Prefers the one in progress, else the nearest inside the lead.
    public static func desired(items: [AgendaItem], now: Date) -> Desired? {
        let candidates = items.compactMap { i -> (AgendaItem, Date, Date)? in
            guard !i.allDay, i.status != "cancelled", i.myResponse != "declined", let s = i.start, let e = i.end, e > s else { return nil }
            guard i.join != nil || !(i.location ?? "").isEmpty else { return nil }
            guard now >= s.addingTimeInterval(-lead), now < e else { return nil }
            return (i, s, e)
        }
        let running = candidates.filter { $0.1 <= now }.sorted { $0.1 > $1.1 }.first   // latest started
        guard let pick = running ?? candidates.sorted(by: { $0.1 < $1.1 }).first else { return nil }
        let (i, s, e) = pick
        return Desired(eventId: i.eventId, occurrenceId: i.id, title: i.title, start: s, end: e, color: i.color,
                       joinURL: i.join?.url, joinKind: i.join?.kind, location: i.location, running: s <= now)
    }

    public enum Action: Equatable, Sendable {
        case start(Desired)
        case update(Desired)
    }

    public struct Plan: Equatable, Sendable {
        public var action: Action?
        /// Occurrence ids of running activities to end.
        public var end: [String]
    }

    /// `existing`: occurrence ids of activities already running.
    public static func plan(items: [AgendaItem], existing: [String], now: Date) -> Plan {
        guard let d = desired(items: items, now: now) else { return Plan(action: nil, end: existing) }
        let stale = existing.filter { $0 != d.occurrenceId }
        return Plan(action: existing.contains(d.occurrenceId) ? .update(d) : .start(d), end: stale)
    }

    /// Fraction for the 3pt line: elapsed part of the 30 minutes before the start, then of the event.
    public static func progress(start: Date, end: Date, now: Date) -> Double {
        if now < start { return min(max(1 - start.timeIntervalSince(now) / lead, 0), 1) }
        let total = end.timeIntervalSince(start)
        return total > 0 ? min(max(now.timeIntervalSince(start) / total, 0), 1) : 1
    }
}

#if canImport(ActivityKit)
public struct MeetingActivityAttributes: ActivityAttributes {
    public struct ContentState: Codable, Hashable, Sendable {
        public var title: String
        public var start: Date
        public var end: Date
        public var location: String?
        public var running: Bool
        public init(title: String, start: Date, end: Date, location: String?, running: Bool) {
            self.title = title; self.start = start; self.end = end; self.location = location; self.running = running
        }
    }
    public var occurrenceId: String
    public var color: String
    public var joinURL: String?
    public var joinKind: String?
    public init(occurrenceId: String, color: String, joinURL: String?, joinKind: String?) {
        self.occurrenceId = occurrenceId; self.color = color; self.joinURL = joinURL; self.joinKind = joinKind
    }
}

public extension LiveActivityPlanner.Desired {
    var state: MeetingActivityAttributes.ContentState {
        .init(title: title, start: start, end: end, location: location, running: running)
    }
    var attributes: MeetingActivityAttributes {
        .init(occurrenceId: occurrenceId, color: color, joinURL: joinURL, joinKind: joinKind)
    }
}

/// Starts, updates and ends the activity after each sync and when the app comes to the foreground.
/// ActivityKit only lets a foreground app *start* one; updates and ends also work from background fetch.
public final class LiveActivityController: AfterSyncHook, @unchecked Sendable {
    private let db: AppDatabase
    private let clock: AppClock
    public init(db: AppDatabase, clock: AppClock = .system) { self.db = db; self.clock = clock }

    public func afterSync(_ outcome: SyncOutcome) async { await refresh() }

    public func refresh() async {
        guard ActivityAuthorizationInfo().areActivitiesEnabled else { return }
        let now = clock.now()
        guard let tz = try? db.userTimeZone() else { return }
        let today = DayKey(now, in: tz)
        let items = (try? await GRDBAgendaRepository(db: db).items(from: today.adding(days: -1), to: today.adding(days: 1))) ?? []
        let running = Activity<MeetingActivityAttributes>.activities
        let plan = LiveActivityPlanner.plan(items: items, existing: running.map { $0.attributes.occurrenceId }, now: now)
        let endedLinger = LiveActivityPlanner.endedLinger
        for a in running where plan.end.contains(a.attributes.occurrenceId) {
            // An event that just ended stays as a plain "Ended 10:30" row for 5 minutes, then goes.
            let state = a.content.state
            if state.end <= now, now < state.end.addingTimeInterval(endedLinger) {
                await a.end(ActivityContent(state: state, staleDate: nil), dismissalPolicy: .after(state.end.addingTimeInterval(endedLinger)))
            } else {
                await a.end(nil, dismissalPolicy: .immediate)
            }
        }
        switch plan.action {
        case .start(let d):
            _ = try? Activity.request(attributes: d.attributes, content: ActivityContent(state: d.state, staleDate: d.staleDate), pushType: nil)
        case .update(let d):
            for a in running where a.attributes.occurrenceId == d.occurrenceId {
                await a.update(ActivityContent(state: d.state, staleDate: d.staleDate))
            }
        case nil: break
        }
    }
}
#endif
