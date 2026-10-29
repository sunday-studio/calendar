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
    /// How `status` is drawn: plain, dim mono word, or the filled amber "needs you" tag (OVERLAP, REPLY).
    public var statusKind: StatusKind
    /// Spoken form for VoiceOver ("Design review, 14:30, overlaps 1:1 Maya").
    public var spoken: String
    /// Declined events shown on request are drawn at 45 %.
    public var faded: Bool
    public enum StatusKind: Equatable, Sendable { case none, dim, needsYou }
    public init(id: String, time: String, title: String, status: String?, color: String, live: Bool,
                statusKind: StatusKind = .none, spoken: String? = nil, faded: Bool = false) {
        self.id = id; self.time = time; self.title = title; self.status = status; self.color = color; self.live = live
        self.statusKind = status == nil ? .none : statusKind
        self.spoken = spoken ?? "\(title), \(time)"
        self.faded = faded
    }
}

/// An all-day event, drawn as the quiet dot line (never a row).
public struct AllDayChip: Equatable, Sendable {
    public var title: String
    public var color: String
    public init(title: String, color: String) { self.title = title; self.color = color }
}

/// The Day variant of the large widget: events as blocks on a 33 pt/h timeline that starts at the current time
/// (rounded down to 15 min) and runs 8.5 h; overlaps split the column (max 3).
public struct DayTimeline: Sendable {
    public struct Block: Equatable, Sendable, Identifiable {
        public var id: String
        public var title: String
        public var time: String
        public var color: String
        public var start: Date
        public var end: Date
        public var column: Int
        public var columns: Int
        public var isNext: Bool
    }
    public static let hours: Double = 8.5
    /// Whole hours inside the window, labelled in the user's time zone ("10").
    public var marks: [(at: Date, label: String)]
    public var windowStart: Date
    public var windowEnd: Date
    public var blocks: [Block]
}

extension DayTimeline: Equatable {
    public static func == (a: DayTimeline, b: DayTimeline) -> Bool {
        a.windowStart == b.windowStart && a.windowEnd == b.windowEnd && a.blocks == b.blocks
    }
}

/// The day as the v2 widgets draw it (spec widgets-notifications section 1 ordering rules): a hero event, today's
/// remaining rows, tomorrow's rows and the all-day line. Sizes take what fits with `WidgetFill`.
public struct DayLayout: Equatable, Sendable {
    public struct Hero: Equatable, Sendable {
        public var id: String
        public var title: String
        /// Flip-cell start time.
        public var time: String
        public var color: String
        public var live: Bool
        public var start: Date
        public var end: Date
        /// Mono caps line under the title, without the video tag: "IN 8 MIN", "22 MIN LEFT", "REPLY NEEDED \u{00B7} 19:00".
        public var sub: String
        /// "MEET" / "ZOOM" when the event has a video link; `badgeLong` is "GOOGLE MEET".
        public var badge: String?
        public var badgeLong: String?
        public var joinURL: URL?
        public var needsReply: Bool
        public var place: String?
        public var spoken: String

        public init(id: String, title: String, time: String, color: String, live: Bool, start: Date, end: Date, sub: String,
                    badge: String? = nil, badgeLong: String? = nil, joinURL: URL? = nil, needsReply: Bool = false,
                    place: String? = nil, spoken: String) {
            self.id = id; self.title = title; self.time = time; self.color = color; self.live = live; self.start = start; self.end = end
            self.sub = sub; self.badge = badge; self.badgeLong = badgeLong; self.joinURL = joinURL; self.needsReply = needsReply
            self.place = place; self.spoken = spoken
        }
    }

    /// "TUE 13".
    public var header: String
    /// "TOMORROW \u{00B7} WED 14".
    public var tomorrowHeader: String
    public var allDay: [AllDayChip]
    public var hero: Hero?
    /// Timed events still to come today, after the hero, in order.
    public var today: [WidgetRowData]
    public var tomorrow: [WidgetRowData]
    /// Days after tomorrow that have events (up to a week ahead), so tall widgets keep filling.
    public var later: [LaterDay]
    public struct LaterDay: Equatable, Sendable {
        public var header: String
        public var rows: [WidgetRowData]
        public init(header: String, rows: [WidgetRowData]) { self.header = header; self.rows = rows }
    }
    /// Events not over yet today, the hero included.
    public var todayLeft: Int
    public var timeline: DayTimeline?

    public init(header: String, tomorrowHeader: String, allDay: [AllDayChip], hero: Hero?, today: [WidgetRowData],
                tomorrow: [WidgetRowData], later: [LaterDay] = [], todayLeft: Int, timeline: DayTimeline? = nil) {
        self.later = later
        self.timeline = timeline
        self.header = header; self.tomorrowHeader = tomorrowHeader; self.allDay = allDay; self.hero = hero
        self.today = today; self.tomorrow = tomorrow; self.todayLeft = todayLeft
    }
}

/// What fits in a widget of a given height: today first, then a label and tomorrow, then "+N MORE".
public enum WidgetFill {
    public enum Row: Equatable, Sendable {
        case event(WidgetRowData)
        case label(String)
    }

    public struct Result: Equatable, Sendable {
        public var rows: [Row]
        /// Timed events that did not fit and are still to come (tomorrow's remainder once tomorrow is shown).
        public var overflow: Int
        public var tomorrowShown: Bool
    }

    /// `slots`: how many 24 pt rows fit; a group label takes one. After today come tomorrow and then each later day with events.
    public static func rows(_ l: DayLayout, slots: Int) -> Result {
        let groups: [(label: String, rows: [WidgetRowData])] =
            (l.tomorrow.isEmpty ? [] : [(l.tomorrowHeader, l.tomorrow)]) + l.later.filter { !$0.rows.isEmpty }.map { ($0.header, $0.rows) }
        let total = l.today.count + groups.reduce(0) { $0 + $1.rows.count }
        guard slots > 0 else { return Result(rows: [], overflow: total, tomorrowShown: false) }
        let doneToday = l.hero == nil && l.today.isEmpty
        if !doneToday && l.today.count >= slots {
            return Result(rows: l.today.prefix(slots).map { .event($0) }, overflow: l.today.count - slots, tomorrowShown: false)
        }
        var rows: [Row] = doneToday ? [] : l.today.map { .event($0) }
        var free = slots - rows.count
        var shown = false, overflow = 0
        for (n, g) in groups.enumerated() {
            guard free >= 2 else { break }
            let take = min(free - 1, g.rows.count)
            let tomorrowFirst = doneToday && n == 0 && !l.tomorrow.isEmpty
            rows.append(.label(tomorrowFirst ? "NEXT \u{00B7} TOMORROW" : g.label))
            rows += g.rows.prefix(take).map { .event($0) }
            free -= take + 1
            shown = true
            if take < g.rows.count {
                overflow = g.rows.count - take + groups.dropFirst(n + 1).reduce(0) { $0 + $1.rows.count }
                break
            }
        }
        if !shown && doneToday { overflow = total }
        return Result(rows: rows, overflow: overflow, tomorrowShown: shown)
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
    /// The day laid out for the v2 widgets; nil when there is nothing to lay out (no accounts).
    public var layout: DayLayout?

    public init(date: Date, content: Content, stale: String?, layout: DayLayout? = nil) {
        self.date = date; self.content = content; self.stale = stale; self.layout = layout
    }

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
    public var config: WidgetConfig
    public init(items: [AgendaItem], accounts: [Account], paired: Bool = true, lastSyncAt: Date?, tz: TimeZone,
                config: WidgetConfig = .default) {
        self.items = items.filter { config.includes($0.calendarId) }
        self.accounts = accounts; self.paired = paired; self.lastSyncAt = lastSyncAt; self.tz = tz; self.config = config
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
        input.items.filter { isShown($0, showDeclined: input.config.showDeclined) }.sorted { ($0.start ?? .distantPast) < ($1.start ?? .distantPast) }
    }

    /// Timed events that count: not cancelled, not declined.
    static func isShown(_ i: AgendaItem, showDeclined: Bool = false) -> Bool {
        !i.allDay && i.start != nil && i.end != nil && i.status != "cancelled" && (showDeclined || i.myResponse != "declined")
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
        func make(_ c: WidgetEntryData.Content) -> WidgetEntryData {
            .init(date: now, content: c, stale: stale, layout: input.accounts.isEmpty ? nil : layout(input, at: now, format: format, stale: stale != nil))
        }

        if !input.paired || input.accounts.isEmpty { return make(.noAccounts) }
        if let a = input.accounts.first(where: { $0.statusKind == .needsReauth }) {
            return make(.needsSignIn(account: a.displayName.isEmpty ? a.email : a.displayName))
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

    // MARK: Day layout

    /// Today's remaining events, tomorrow's events and the all-day line (spec section 1: hidden cancelled and declined,
    /// in-progress or next event pinned as the hero, status word per row: OVERLAP before REPLY).
    public static func layout(_ input: WidgetInput, at now: Date, format: SurfaceFormat, stale: Bool = false) -> DayLayout {
        let today = DayKey(now, in: input.tz)
        let tomorrow = today.adding(days: 1)
        let timed = relevant(input).sorted {
            ($0.start ?? .distantPast, $0.end ?? .distantPast, $0.title) < ($1.start ?? .distantPast, $1.end ?? .distantPast, $1.title)
        }
        let todays = timed.filter { i in
            guard let e = i.end, let s = i.start else { return false }
            return e > now && DayKey(s, in: input.tz) <= today && DayKey(e.addingTimeInterval(-0.001), in: input.tz) >= today
        }
        let tomorrows = timed.filter { i in i.start.map { DayKey($0, in: input.tz) == tomorrow } ?? false }

        func overlaps(_ i: AgendaItem, in day: [AgendaItem]) -> AgendaItem? {
            guard let s = i.start, let e = i.end else { return nil }
            return day.first { o in
                guard o.id != i.id, let os = o.start, let oe = o.end else { return false }
                return s < oe && os < e
            }
        }
        func row(_ i: AgendaItem, in day: [AgendaItem], plain: Bool) -> WidgetRowData {
            let s = i.start ?? now
            var status: String?, kind = WidgetRowData.StatusKind.none
            var spoken = "\(i.title), \(format.time(s))"
            if !plain {
                if let o = overlaps(i, in: day) { status = "OVERLAP"; kind = .needsYou; spoken += ", overlaps \(o.title)" }
                else if i.myResponse == "needsAction" && i.canRSVP { status = "REPLY"; kind = .needsYou; spoken += ", reply needed" }
                else if let p = i.prevStart, let m = i.movedAt, now.timeIntervalSince(m) < AgendaItem.movedMemory {
                    status = "MOVED"; kind = .dim; spoken += ", moved from \(format.time(p))"
                }
            }
            return WidgetRowData(id: i.id, time: format.flipTime(s), title: i.title, status: status, color: i.color,
                                 live: s <= now, statusKind: kind, spoken: spoken, faded: i.myResponse == "declined")
        }

        let laterDays: [DayLayout.LaterDay] = (2...7).compactMap { k in
            let day = today.adding(days: k)
            let items = timed.filter { i in i.start.map { DayKey($0, in: input.tz) == day } ?? false }
            return items.isEmpty ? nil : DayLayout.LaterDay(header: format.headerDate(day.startOfDay(in: input.tz)),
                                                            rows: items.map { row($0, in: items, plain: stale) })
        }

        var hero: DayLayout.Hero?
        var rest = todays
        if !stale, let first = todays.first(where: { $0.myResponse != "declined" }), let s = first.start, let e = first.end {
            rest.removeAll { $0.id == first.id }
            let live = s <= now
            let needsReply = first.myResponse == "needsAction" && first.canRSVP
            let words: String
            if live { words = SurfaceFormat.left(end: e, now: now).uppercased() }
            else {
                let mins = Int((s.timeIntervalSince(now) / 60).rounded(.up))
                words = mins <= 0 ? "NOW" : "IN \(SurfaceFormat.minutesText(mins).uppercased())"
            }
            let kind = first.join?.kind
            let badgeLong = first.join.map { SurfaceFormat.joinLabel($0.kind).uppercased() }
            hero = .init(id: first.id, title: first.title, time: format.flipTime(s), color: first.color, live: live, start: s, end: e,
                         sub: needsReply ? "REPLY NEEDED \u{00B7} \(format.time(s))" : words,
                         badge: kind.map { SurfaceFormat.joinBadge($0) }, badgeLong: badgeLong,
                         joinURL: first.join.flatMap { URL(string: $0.url) }, needsReply: needsReply,
                         place: first.location.flatMap { $0.isEmpty ? nil : $0 },
                         spoken: "\(first.title), \(format.time(s)), \(live ? words.lowercased() : words.lowercased())")
        }
        let allDay = input.items.filter { i in
            guard i.allDay, i.status != "cancelled", i.myResponse != "declined", let d = i.date else { return false }
            let first = DayKey(d)
            let last = i.endDate.map { DayKey($0).adding(days: -1) } ?? first
            return first <= today && today <= last
        }.map { AllDayChip(title: $0.title, color: $0.color) }

        return DayLayout(header: format.headerDate(now), tomorrowHeader: "TOMORROW \u{00B7} \(format.headerDate(tomorrow.startOfDay(in: input.tz)))",
                         allDay: allDay, hero: hero,
                         today: rest.map { row($0, in: todays, plain: stale) },
                         tomorrow: tomorrows.map { row($0, in: tomorrows, plain: stale) },
                         later: laterDays,
                         todayLeft: todays.count, timeline: timeline(todays, now: now, format: format, heroId: hero?.id))
    }

    static func timeline(_ events: [AgendaItem], now: Date, format: SurfaceFormat, heroId: String?) -> DayTimeline {
        let start = Date(timeIntervalSince1970: (now.timeIntervalSince1970 / 900).rounded(.down) * 900)
        let end = start.addingTimeInterval(DayTimeline.hours * 3600)
        let inWindow = events.filter { ($0.start ?? .distantPast) < end && ($0.end ?? .distantPast) > start }
            .sorted { ($0.start ?? .distantPast, $0.end ?? .distantPast) < ($1.start ?? .distantPast, $1.end ?? .distantPast) }
        // Greedy columns inside each overlap cluster.
        var blocks: [DayTimeline.Block] = []
        var cluster: [(item: AgendaItem, col: Int)] = []
        var clusterEnd = Date.distantPast
        func flush() {
            let cols = min((cluster.map(\.col).max() ?? 0) + 1, 3)
            for c in cluster {
                guard let s = c.item.start, let e = c.item.end else { continue }
                blocks.append(.init(id: c.item.id, title: c.item.title, time: format.time(s), color: c.item.color, start: s, end: e,
                                    column: min(c.col, 2), columns: cols, isNext: c.item.id == heroId))
            }
            cluster = []
        }
        for i in inWindow {
            guard let s = i.start, let e = i.end else { continue }
            if s >= clusterEnd { flush(); clusterEnd = e }
            else { clusterEnd = max(clusterEnd, e) }
            let used = Set(cluster.filter { ($0.item.end ?? .distantPast) > s }.map(\.col))
            cluster.append((i, (0...).first { !used.contains($0) }!))
        }
        flush()
        let hourF = DateFormatter(); hourF.locale = format.locale; hourF.timeZone = format.tz; hourF.dateFormat = "HH"
        let first = (start.timeIntervalSince1970 / 3600).rounded(.up) * 3600
        let marks = stride(from: first, to: end.timeIntervalSince1970, by: 3600).map { t -> (at: Date, label: String) in
            let d = Date(timeIntervalSince1970: t); return (d, hourF.string(from: d))
        }
        return DayTimeline(marks: marks, windowStart: start, windowEnd: end, blocks: blocks)
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
