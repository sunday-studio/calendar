import CalendarCore
import Foundation

/// One row of the "today" list (medium widget).
public struct WidgetRowData: Equatable, Sendable, Identifiable {
    public var id: String
    public var time: String
    public var title: String
    public var status: String?
    public var color: String
    public var live: Bool
    public init(id: String, time: String, title: String, status: String?, color: String, live: Bool) {
        self.id = id; self.time = time; self.title = title; self.status = status; self.color = color; self.live = live
    }
}

/// What a widget shows at one point in time. Built from the saved agenda only, never the network.
public struct WidgetEntryData: Equatable, Sendable {
    public enum Content: Equatable, Sendable {
        /// Not paired, or paired with no accounts: "Connect an account".
        case noAccounts
        /// An account needs sign in: "Showing saved events" + "[account] needs sign in. Open Calendar."
        case needsSignIn(account: String)
        /// Nothing on the calendars in the window at all.
        case nothingPlanned
        /// No events left today; `next` is the following event (tomorrow or later), if any.
        case done(next: Followup?)
        /// A next or in-progress event.
        case event(EventState)
    }

    public struct Followup: Equatable, Sendable {
        public var title: String
        /// "tomorrow 09:00" / "Mon 12 Oct 09:00".
        public var when: String
        public var eventId: String
        public init(title: String, when: String, eventId: String) { self.title = title; self.when = when; self.eventId = eventId }
    }

    public struct EventState: Equatable, Sendable {
        public var id: String
        public var title: String
        public var start: Date
        public var end: Date
        /// Flip-cell start time, "10:30".
        public var time: String
        public var color: String
        public var live: Bool
        /// "in 8 min" or "Now · 22 min left".
        public var relative: String
        /// Remaining events today, in-progress first; at most 4.
        public var rows: [WidgetRowData]
        /// Count shown as "3 LEFT" (events today not yet ended).
        public var remaining: Int
        public var accessibility: String
        public init(id: String, title: String, start: Date, end: Date, time: String, color: String, live: Bool, relative: String,
                    rows: [WidgetRowData], remaining: Int, accessibility: String) {
            self.id = id; self.title = title; self.start = start; self.end = end; self.time = time; self.color = color
            self.live = live; self.relative = relative; self.rows = rows; self.remaining = remaining; self.accessibility = accessibility
        }
    }

    public var date: Date
    public var content: Content
    /// "Saved 7 h ago" when the last successful sync is old; nil otherwise.
    public var stale: String?

    public init(date: Date, content: Content, stale: String?) { self.date = date; self.content = content; self.stale = stale }

    /// Where a tap on the widget goes.
    public var link: SurfaceLink {
        switch content {
        case .event(let e): .event(e.id)
        case .done(let n?): .event(n.eventId)
        case .needsSignIn: .accounts
        case .noAccounts: .accounts
        default: .today
        }
    }
}

/// Everything the builder reads, already loaded from the shared database.
public struct WidgetInput: Sendable {
    public var items: [AgendaItem]
    public var accounts: [Account]
    public var paired: Bool
    public var lastSyncAt: Date?
    public var tz: TimeZone
    public init(items: [AgendaItem], accounts: [Account], paired: Bool = true, lastSyncAt: Date?, tz: TimeZone) {
        self.items = items; self.accounts = accounts; self.paired = paired; self.lastSyncAt = lastSyncAt; self.tz = tz
    }
}

/// Pure timeline logic for the widgets (spec: entries at every start/end boundary, countdown words per entry).
public enum TimelineBuilder {
    /// Entries cover this far ahead; the timeline is reloaded after the last one (and on every sync).
    public static let horizon: TimeInterval = 8 * 3600
    /// Per-minute entries this long before a boundary so countdown words stay exact.
    public static let minuteWindow = 15
    public static let maxEntries = 100
    public static let staleAfter: TimeInterval = 6 * 3600
    public static let maxRows = 4

    public struct Timeline: Equatable, Sendable {
        public var entries: [WidgetEntryData]
        /// Ask WidgetKit for a new timeline at this time.
        public var reloadAt: Date
    }

    public static func timeline(_ input: WidgetInput, now: Date, locale: Locale = .current) -> Timeline {
        let dates = entryDates(input, now: now)
        let fmt = SurfaceFormat(tz: input.tz, locale: locale)
        let entries = dates.map { entry(input, at: $0, format: fmt) }
        let last = dates.last ?? now
        // Never ask for a reload sooner than 15 min (WidgetKit budget); the sync hook reloads on real changes.
        return Timeline(entries: entries, reloadAt: max(last, now.addingTimeInterval(15 * 60)))
    }

    // MARK: Entry dates

    static func relevant(_ input: WidgetInput) -> [AgendaItem] {
        input.items.filter { isShown($0) }.sorted { ($0.start ?? .distantPast) < ($1.start ?? .distantPast) }
    }

    /// Timed events that count: not cancelled, not declined.
    static func isShown(_ i: AgendaItem) -> Bool {
        !i.allDay && i.start != nil && i.end != nil && i.status != "cancelled" && i.myResponse != "declined"
    }

    public static func entryDates(_ input: WidgetInput, now: Date) -> [Date] {
        let end = now.addingTimeInterval(horizon)
        var set: Set<TimeInterval> = [now.timeIntervalSince1970]
        func add(_ d: Date) { if d > now && d <= end { set.insert(d.timeIntervalSince1970.rounded()) } }
        // 15-minute grid keeps long countdowns ("in 3 h 20 min") fresh.
        var g = (now.timeIntervalSince1970 / 900).rounded(.down) * 900 + 900
        while g <= end.timeIntervalSince1970 { set.insert(g); g += 900 }
        // Day rollover: "today" changes at local midnight.
        add(DayKey(now, in: input.tz).adding(days: 1).startOfDay(in: input.tz))
        for i in relevant(input) {
            guard let s = i.start, let e = i.end else { continue }
            for b in [s, e] where b > now.addingTimeInterval(-Double(minuteWindow) * 60) && b <= end.addingTimeInterval(Double(minuteWindow) * 60) {
                add(b)
                for k in 1...minuteWindow { add(b.addingTimeInterval(-Double(k) * 60)) }
            }
        }
        var out = set.map { Date(timeIntervalSince1970: $0) }.sorted()
        if out.count > maxEntries { out = Array(out.prefix(maxEntries)) }
        return out
    }

    // MARK: One entry

    public static func entry(_ input: WidgetInput, at now: Date, format: SurfaceFormat) -> WidgetEntryData {
        let stale = staleText(input.lastSyncAt, now: now)
        func make(_ c: WidgetEntryData.Content) -> WidgetEntryData { .init(date: now, content: c, stale: stale) }

        if !input.paired || input.accounts.isEmpty { return make(.noAccounts) }
        if let a = input.accounts.first(where: { $0.statusKind == .needsReauth }) {
            return make(.needsSignIn(account: a.email.isEmpty ? a.displayName : a.email))
        }
        let shown = relevant(input)
        if shown.isEmpty && !input.items.contains(where: { $0.status != "cancelled" && $0.myResponse != "declined" }) {
            return make(.nothingPlanned)
        }
        let today = DayKey(now, in: input.tz)
        // Not over yet: in progress or still ahead today.
        let leftToday = shown.filter { i in
            guard let s = i.start, let e = i.end else { return false }
            return e > now && DayKey(s, in: input.tz) <= today && DayKey(e.addingTimeInterval(-0.001), in: input.tz) >= today
        }
        guard let first = leftToday.first, let s = first.start, let e = first.end else {
            let next = shown.first { ($0.start ?? .distantPast) > now }
            return make(.done(next: next.flatMap { followup($0, now: now, format: format) }))
        }
        let live = s <= now
        let relative = live ? "Now \u{00B7} \(SurfaceFormat.left(end: e, now: now))" : SurfaceFormat.until(start: s, now: now)
        let rows = leftToday.prefix(maxRows).enumerated().map { n, i -> WidgetRowData in
            let st = i.start ?? now
            let isLive = st <= now
            var status: String?
            if n == 0 { status = isLive ? "Now" : SurfaceFormat.minutesText(Int((st.timeIntervalSince(now) / 60).rounded(.up))) }
            else if i.myResponse == "needsAction" && i.canRSVP { status = "Reply needed" }
            if n == 0 && !isLive && status != nil && st.timeIntervalSince(now) > 6 * 3600 { status = nil }
            return WidgetRowData(id: i.id, time: format.flipTime(st), title: i.title, status: status, color: i.color, live: isLive)
        }
        let state = WidgetEntryData.EventState(
            id: first.id, title: first.title, start: s, end: e, time: format.flipTime(s), color: first.color, live: live,
            relative: relative, rows: Array(rows), remaining: leftToday.count,
            accessibility: "\(first.title), \(format.time(s)), \(live ? relative.replacingOccurrences(of: " \u{00B7} ", with: ", ") : relative)")
        return make(.event(state))
    }

    static func followup(_ i: AgendaItem, now: Date, format: SurfaceFormat) -> WidgetEntryData.Followup? {
        guard let s = i.start else { return nil }
        return .init(title: i.title, when: "\(format.dayWord(s, now: now)) \(format.flipTime(s))", eventId: i.id)
    }

    public static func staleText(_ lastSync: Date?, now: Date) -> String? {
        guard let lastSync else { return nil }
        let age = now.timeIntervalSince(lastSync)
        guard age >= staleAfter else { return nil }
        let h = Int(age / 3600)
        return h >= 48 ? "Saved \(h / 24) days ago" : "Saved \(h) h ago"
    }
}
