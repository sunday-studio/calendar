import CalendarCore
import Foundation

/// A local notification the phone should have pending.
public struct PlannedReminder: Equatable, Sendable, Identifiable {
    public var id: String           // "rem.<occurrence id>"
    public var occurrenceId: String
    public var eventId: String
    public var fireDate: Date
    public var title: String
    public var body: String
    public var thread: String
    public var joinURL: String?
    public var category: String?    // `NotificationCategory.reminderJoin` when a Join action applies
    public var timeSensitive: Bool

    public static let prefix = "rem."

    /// Changes whenever anything the user would see or that changes delivery changes.
    public var signature: String {
        [id, String(Int(fireDate.timeIntervalSince1970)), title, body, joinURL ?? "", category ?? "", timeSensitive ? "1" : "0"]
            .joined(separator: "\u{1F}")
    }
}

/// Notification categories and action identifiers shared by the app, the planner and the extension.
public enum NotificationCategory {
    public static let reminderJoin = "calendar.reminder.join"
    public static let moved = "calendar.event.moved"
    public static let reauth = "calendar.account.reauth"
    public static let summary = "calendar.summary"

    public static let actionJoin = "join"
    public static let actionSnooze = "snooze"
    public static let actionView = "view"
    public static let actionOK = "ok"
    public static let actionSignIn = "signin"
    public static let actionLater = "later"
    public static let actionOpenToday = "open-today"
    public static let actionMute = "mute"

    public static let snoozeSeconds: TimeInterval = 5 * 60
}

public struct ReminderInput: Sendable {
    public var items: [AgendaItem]
    public var settings: Settings?
    /// Per-event overrides in minutes before (key = occurrence id or event id).
    public var overrides: [String: Int]
    /// account id -> email, for the body line.
    public var accountNames: [String: String]
    public var tz: TimeZone
    public var showJoin: Bool
    public var locale: Locale
    public init(items: [AgendaItem], settings: Settings?, overrides: [String: Int] = [:], accountNames: [String: String] = [:],
                tz: TimeZone, showJoin: Bool = true, locale: Locale = .current) {
        self.items = items; self.settings = settings; self.overrides = overrides; self.accountNames = accountNames
        self.tz = tz; self.showJoin = showJoin; self.locale = locale
    }
}

/// Pure planner: which reminders to schedule, from the saved agenda (spec w-remind).
public enum ReminderPlanner {
    /// iOS keeps at most 64 pending local notifications; leave room for snoozes.
    public static let cap = 60
    /// Reminders closer than this to now are skipped (the system would drop or fire them at once).
    public static let minLead: TimeInterval = 2
    public static let defaultTimedMinutes = 10

    public static func plan(_ input: ReminderInput, now: Date) -> [PlannedReminder] {
        let fmt = SurfaceFormat(tz: input.tz, locale: input.locale)
        let timedDefault: Int?
        if let a = input.settings?.alertDefaults { timedDefault = a.timed } else { timedDefault = defaultTimedMinutes }
        let allDayDefault = input.settings?.alertDefaults?.allDay
        var seenIDs = Set<String>()
        var seenEvents = Set<String>()
        var out: [PlannedReminder] = []

        for i in input.items {
            guard i.status != "cancelled", i.myResponse != "declined", !i.id.hasPrefix("pending:") else { continue }
            guard let start = startInstant(i, tz: input.tz) else { continue }
            let override = input.overrides[i.id] ?? input.overrides[i.eventId]
            let lead = override ?? (i.allDay ? allDayDefault : timedDefault)
            guard let lead else { continue }
            let fire = start.addingTimeInterval(-Double(lead) * 60)
            guard fire > now.addingTimeInterval(minLead) else { continue }
            // A reminder that would fire after the event has ended makes no sense (zero-length all-day edge).
            let id = PlannedReminder.prefix + i.id
            let key = "\(i.eventId)|\(Int(start.timeIntervalSince1970))|\(Int(fire.timeIntervalSince1970))"
            guard seenIDs.insert(id).inserted, seenEvents.insert(key).inserted else { continue }
            out.append(make(i, id: id, fire: fire, start: start, lead: lead, fmt: fmt, input: input, now: now))
        }
        out.sort { ($0.fireDate, $0.id) < ($1.fireDate, $1.id) }
        return Array(out.prefix(cap))
    }

    /// The instant an event starts; all-day events start at local midnight of their first day.
    static func startInstant(_ i: AgendaItem, tz: TimeZone) -> Date? {
        if i.allDay { return (i.date ?? i.endDate).map { DayKey($0).startOfDay(in: tz) } }
        return i.start
    }

    static func make(_ i: AgendaItem, id: String, fire: Date, start: Date, lead: Int, fmt: SurfaceFormat,
                     input: ReminderInput, now: Date) -> PlannedReminder {
        let account = input.accountNames[i.accountId]
        let link = i.join.flatMap { URL(string: $0.url) != nil ? $0 : nil }
        let title: String
        let body: String
        if i.allDay {
            // Fired on the day before / morning of: say which day the event is.
            title = "\(i.title) \(fmt.dayWord(start, now: fire))"
            body = ["All day", i.location, account].compactMap { $0 }.joined(separator: " \u{00B7} ")
        } else if let link {
            title = lead <= 0 ? "\(i.title) now" : "\(i.title) in \(SurfaceFormat.minutesText(lead))"
            body = [fmt.time(start), SurfaceFormat.joinLabel(link.kind), account].compactMap { $0 }.joined(separator: " \u{00B7} ")
        } else {
            title = "\(i.title) at \(fmt.time(start))"
            body = i.location ?? account ?? ""
        }
        let joinable = link != nil && !i.allDay && input.showJoin
        return PlannedReminder(
            id: id, occurrenceId: i.id, eventId: i.eventId, fireDate: fire, title: title, body: body,
            thread: "event." + i.eventId, joinURL: joinable ? link?.url : nil,
            category: joinable ? NotificationCategory.reminderJoin : nil,
            timeSensitive: link != nil && !i.allDay && lead <= 60)
    }

    /// Diff against what is pending: ids to remove and reminders to (re)add. A changed fire time, title or body
    /// replaces the pending request (so a moved event is replaced, spec w-remind).
    /// `pending` maps pending request id -> the signature it was scheduled with.
    public static func diff(planned: [PlannedReminder], pending: [String: String]) -> (remove: [String], add: [PlannedReminder]) {
        let ids = Set(planned.map(\.id))
        let remove = pending.keys.filter { !ids.contains($0) }.sorted()
        let add = planned.filter { pending[$0.id] != $0.signature }
        return (remove, add)
    }
}
