import CalendarCore
import Foundation

/// Remote push payload (B2): `k` plus ids and counters only, never titles.
public struct PushPayload: Equatable, Sendable {
    public enum Kind: String, Sendable { case sync, moved, cancelled, changed, reauth, summary }
    public var kind: Kind
    public var eventId: String?
    public var accountId: String?
    public var date: String?
    public var rev: Int64?

    public init(kind: Kind, eventId: String? = nil, accountId: String? = nil, date: String? = nil, rev: Int64? = nil) {
        self.kind = kind; self.eventId = eventId; self.accountId = accountId; self.date = date; self.rev = rev
    }

    public init?(userInfo: [AnyHashable: Any]) {
        guard let k = userInfo["k"] as? String, let kind = Kind(rawValue: k) else { return nil }
        self.kind = kind
        eventId = userInfo["eventId"] as? String
        accountId = userInfo["accountId"] as? String
        date = userInfo["date"] as? String
        rev = (userInfo["rev"] as? NSNumber)?.int64Value
    }
}

/// What the notification service extension shows.
public struct RewrittenNotification: Equatable, Sendable {
    public var title: String
    public var subtitle: String? = nil
    public var body: String
    public var category: String?
    public var thread: String?
}

/// The facts about an event the wording needs, from the hub (`GET /v1/events/{id}`) or the saved agenda.
public struct EventFacts: Equatable, Sendable {
    public var title: String
    public var start: Date?
    public var allDay: Bool
    public var date: String?
    public var place: String?       // calendar name, else location
    public var cancelled: Bool

    public init(title: String, start: Date?, allDay: Bool = false, date: String? = nil, place: String? = nil, cancelled: Bool = false) {
        self.title = title; self.start = start; self.allDay = allDay; self.date = date; self.place = place; self.cancelled = cancelled
    }

    public init(_ d: EventDetail) {
        self.init(item: d.item, place: d.calendarName ?? d.item.location)
    }

    public init(item i: AgendaItem, place: String?) {
        self.init(title: i.title, start: i.start, allDay: i.allDay, date: i.date, place: place, cancelled: i.status == "cancelled")
    }
}

/// Everything fetched for one push; any part may be missing (offline, timeout, 404).
public struct RewriteContext: Sendable {
    public var event: EventFacts?
    /// Start before the change, when the saved agenda still has it.
    public var previousStart: Date?
    public var accountEmail: String?
    public var summary: DaySummary?
    public var tz: TimeZone
    public var now: Date
    public var locale: Locale
    public init(event: EventFacts? = nil, previousStart: Date? = nil, accountEmail: String? = nil, summary: DaySummary? = nil,
                tz: TimeZone = .current, now: Date = Date(), locale: Locale = .current) {
        self.event = event; self.previousStart = previousStart; self.accountEmail = accountEmail; self.summary = summary
        self.tz = tz; self.now = now; self.locale = locale
    }
}

/// Pure wording for the NSE (spec w-moved, w-reauth, w-summary). Never throws: missing data falls back to generic text.
public enum NotificationRewriter {
    public static func rewrite(_ p: PushPayload, _ c: RewriteContext) -> RewrittenNotification {
        let fmt = SurfaceFormat(tz: c.tz, locale: c.locale)
        switch p.kind {
        case .moved:
            guard let e = c.event, let start = e.start, !e.cancelled else { return generic(p) }
            let when = DayKey(start, in: c.tz) == DayKey(c.now, in: c.tz) ? fmt.time(start) : "\(fmt.dayWord(start, now: c.now)) \(fmt.time(start))"
            // New fact first: "Design review moved to 14:30" · "Was 11:00 · Work" · "Today, 3 h 30 min later."
            var was: [String] = []
            var body = ""
            if let old = c.previousStart, old != start {
                let sameDay = DayKey(old, in: c.tz) == DayKey(start, in: c.tz)
                was.append("Was " + (sameDay ? fmt.time(old) : "\(fmt.shortDate(old)) \(fmt.time(old))"))
                let mins = Int((abs(start.timeIntervalSince(old)) / 60).rounded())
                let day = fmt.dayWord(start, now: c.now)
                body = "\(day.prefix(1).uppercased() + day.dropFirst()), \(SurfaceFormat.minutesText(mins)) \(start > old ? "later" : "earlier")."
            }
            if let place = e.place { was.append(place) }
            return .init(title: "\(e.title) moved to \(when)", subtitle: was.isEmpty ? nil : was.joined(separator: " \u{00B7} "),
                         body: body, category: NotificationCategory.moved, thread: thread(p))
        case .cancelled:
            guard let e = c.event, let start = e.start ?? e.date.map({ DayKey($0).startOfDay(in: c.tz) }) else { return generic(p) }
            let day = fmt.dayWord(start, now: c.now)
            let title = e.allDay ? "\(e.title) cancelled" : "\(e.title) at \(fmt.time(start)) cancelled"
            return .init(title: title, subtitle: e.place, body: "Removed from \(day).", category: NotificationCategory.event, thread: thread(p))
        case .changed:
            return generic(p)
        case .reauth:
            guard let email = c.accountEmail else { return generic(p) }
            return .init(title: "\(email) needs you to sign in", body: "Its events are paused until you do.",
                         category: NotificationCategory.reauth, thread: thread(p))
        case .summary:
            guard let s = c.summary else { return generic(p) }
            return .init(title: s.title, body: s.body, category: NotificationCategory.summary, thread: thread(p))
        case .sync:
            return generic(p)
        }
    }

    /// Text when nothing could be fetched in time (also what the server's generic alert already says).
    public static func generic(_ p: PushPayload) -> RewrittenNotification {
        switch p.kind {
        case .reauth:
            return .init(title: "An account needs you to sign in", body: "Open Calendar to sign in.",
                         category: NotificationCategory.reauth, thread: thread(p))
        case .summary:
            return .init(title: "Your day is ready", body: "Open Calendar to see today.", category: NotificationCategory.summary, thread: thread(p))
        case .moved, .cancelled, .changed, .sync:
            // The news goes in the first 30 characters of the title; same thread as the enriched text, which replaces it.
            return .init(title: "Your calendar changed", body: "Open Calendar to see what is new.", category: nil, thread: thread(p))
        }
    }

    static func thread(_ p: PushPayload) -> String {
        switch p.kind {
        case .reauth: "accounts"
        case .summary: "summary"
        default: "events"
        }
    }
}

/// Alert preferences the extension can honour. It cannot drop a notification (that needs the filtering entitlement),
/// so a switched-off kind is delivered quietly: passive, no sound, no screen wake. It still waits in Notification Centre.
public enum NotificationQuiet {
    public static func isQuiet(_ kind: PushPayload.Kind, prefs: AlertPrefs) -> Bool {
        switch kind {
        case .moved, .cancelled: !prefs.movedCancelled
        case .reauth: !prefs.needsSignIn
        case .changed: !prefs.invitations   // "several events changed" carries new invitations
        case .summary, .sync: false
        }
    }
}
