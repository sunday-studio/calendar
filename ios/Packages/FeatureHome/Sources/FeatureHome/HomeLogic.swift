import CalendarCore
import CalendarUI
import Foundation

// Pure Home logic: no SwiftUI state, no I/O. Everything here is unit-tested with fake clocks.

/// Formats dates for Home in the user's zone. Create once per zone; formatters are not cheap.
public final class HomeFormat: @unchecked Sendable {
    public let tz: TimeZone
    public let locale: Locale
    private let timeF: DateFormatter
    private let weekdayF: DateFormatter
    private let dayMonthF: DateFormatter
    private let monthF: DateFormatter
    private let yearF: DateFormatter
    private let shortDateF: DateFormatter

    public init(tz: TimeZone, locale: Locale = .current) {
        self.tz = tz; self.locale = locale
        func make(_ template: String) -> DateFormatter {
            let f = DateFormatter()
            f.locale = locale; f.timeZone = tz
            f.setLocalizedDateFormatFromTemplate(template)
            return f
        }
        timeF = make("jmm"); weekdayF = make("EEEE"); dayMonthF = make("d MMM"); monthF = make("LLLL"); yearF = make("y"); shortDateF = make("EEE d MMM"); shortDateF.dateFormat = "EEE d MMM"   // fixed order: "Tue 13 Oct"
    }

    public func time(_ d: Date) -> String { timeF.string(from: d) }
    private func noon(_ day: DayKey) -> Date { day.startOfDay(in: tz).addingTimeInterval(12 * 3600) }
    public func weekday(_ day: DayKey) -> String { weekdayF.string(from: noon(day)) }
    public func dayMonth(_ day: DayKey) -> String { dayMonthF.string(from: noon(day)) }
    /// "Tue 13 Oct"
    public func shortDate(_ day: DayKey) -> String { shortDateF.string(from: noon(day)) }
    public func month(_ day: DayKey) -> String { monthF.string(from: noon(day)) }
    public func year(_ day: DayKey) -> String { yearF.string(from: noon(day)) }
}

public enum HomeHero: Equatable, Sendable {
    case noAccounts
    /// Never synced and the hub cannot be reached.
    case unreachable
    case nothingPlanned
    /// Nothing more today. `next` is the following event (a later day), if any.
    case doneForToday(next: AgendaItem?, hadEvents: Bool)
    case upcoming(AgendaItem)
    case live(AgendaItem)
}

public enum HomeLogic {
    /// Join appears this long before the start.
    public static let joinLeadTime: TimeInterval = 10 * 60

    // MARK: Hero state

    public static func hero(accountCount: Int, neverSynced: Bool, syncUnreachable: Bool, next: AgendaItem?,
                            items: [AgendaItem], now: Date, tz: TimeZone) -> HomeHero {
        if accountCount == 0 { return syncUnreachable && neverSynced ? .unreachable : .noAccounts }
        let today = DayKey(now, in: tz)
        let live = items.filter { $0.status != "cancelled" }
        let hadEvents = live.contains { $0.dayRange(in: tz).contains(today) }
        guard let next, let start = next.start else {
            return live.isEmpty ? .nothingPlanned : .doneForToday(next: nil, hadEvents: hadEvents)
        }
        let end = next.end ?? start
        if start <= now && end > now { return .live(next) }
        if DayKey(start, in: tz) == today { return .upcoming(next) }
        return .doneForToday(next: next, hadEvents: hadEvents)
    }

    // MARK: Countdown + presentation of the hero block

    public static func minutesText(_ minutes: Int) -> String {
        let m = max(minutes, 0)
        if m < 60 { return "\(m) min" }
        return m % 60 == 0 ? "\(m / 60) h" : "\(m / 60) h \(m % 60) min"
    }

    public static func untilText(start: Date, now: Date) -> String {
        let mins = Int((start.timeIntervalSince(now) / 60).rounded(.up))
        return mins <= 0 ? "now" : "in \(minutesText(mins))"
    }

    public static func leftText(end: Date, now: Date) -> String {
        "\(minutesText(Int((end.timeIntervalSince(now) / 60).rounded(.up)))) left"
    }

    public enum HeroTarget: Equatable, Sendable {
        case join(URL)
        case map(String)
    }

    public struct HeroBlock: Equatable, Sendable {
        /// Start time for the flip cells ("10:30").
        public var time: String
        public var title: String
        /// "in 8 min", "Now \u{00B7} 22 min left", "Room 4 \u{00B7} in 8 min".
        public var footer: String
        /// Just the timing part: "in 8 min" / "Now \u{00B7} 22 min left".
        public var until: String
        public var color: AccountColor
        public var action: NextEventBlock.Action
        public var target: HeroTarget?
        public var itemID: String
        public var accessibilityLabel: String
    }

    /// Block for `.upcoming` / `.live`. Join shows with a link within 10 minutes of the start or while live;
    /// in-person events (no link) get Map; everything else shows no button.
    public static func block(for item: AgendaItem, live: Bool, now: Date, format: HomeFormat) -> HeroBlock {
        let start = item.start ?? now
        let end = item.end ?? start
        let until = live ? "Now \u{00B7} \(leftText(end: end, now: now))" : untilText(start: start, now: now)
        let place = item.location.flatMap { $0.isEmpty ? nil : $0 }
        var action: NextEventBlock.Action = .none
        var target: HeroTarget?
        var footer = until
        let soon = live || start.timeIntervalSince(now) <= joinLeadTime
        if let j = item.join, let url = URL(string: j.url), soon {
            action = .join("Join"); target = .join(url)
        } else if item.join == nil, let place {
            action = .map("Map"); target = .map(place)
            footer = live ? until : "\(place) \u{00B7} \(until)"
        }
        let time = format.time(start)
        let label = ([live ? "Now" : "Up next", item.title, time, until] + (place.map { [$0] } ?? [])).joined(separator: ", ")
        return HeroBlock(time: time, title: item.title, footer: footer, until: until, color: AccountColor(key: item.color),
                         action: action, target: target, itemID: item.id, accessibilityLabel: label)
    }

    // MARK: Overlaps

    /// id -> title of the first other event it overlaps. Timed, busy, not cancelled/declined events only;
    /// zero-length events and back-to-back events never overlap.
    public static func overlaps(_ items: [AgendaItem]) -> [String: String] {
        let timed = items.filter { i in
            !i.allDay && i.start != nil && i.busy && i.status != "cancelled" && i.myResponse != "declined"
        }.sorted { ($0.start ?? .distantPast, $0.id) < ($1.start ?? .distantPast, $1.id) }
        var out: [String: String] = [:]
        for (n, a) in timed.enumerated() {
            guard let aS = a.start, let aE = a.end, aE > aS else { continue }
            for b in timed[(n + 1)...] {
                guard let bS = b.start, let bE = b.end, bE > bS else { continue }
                if bS >= aE { break } // sorted by start: nothing later can overlap `a`
                if out[a.id] == nil { out[a.id] = b.title }
                if out[b.id] == nil { out[b.id] = a.title }
            }
        }
        return out
    }

    // MARK: Status words

    /// A status word only when something changed or needs you; nothing for plain accepted events.
    public static func statusWord(_ item: AgendaItem, moved: Bool = false, stale: Bool = false) -> EventRowModel.Status? {
        if item.status == "cancelled" { return .cancelled }
        if item.myResponse == "declined" { return .declined }
        if stale { return .stale }
        if moved { return .moved }
        if item.myResponse == "needsAction" && item.canRSVP { return .needsReply }
        if item.myResponse == "tentative" || item.status == "tentative" { return .maybe }
        return nil
    }

    /// Items whose start differs from the last time they were seen. `seen` maps id -> last known start.
    public static func movedIDs(seen: [String: Date], items: [AgendaItem]) -> Set<String> {
        var out = Set<String>()
        for i in items {
            if let s = i.start, let old = seen[i.id], old != s { out.insert(i.id) }
        }
        return out
    }

    public static func changeCount(before: [String: Int64], after: [AgendaItem]) -> Int {
        var n = 0
        var afterIDs = Set<String>()
        for i in after {
            afterIDs.insert(i.id)
            if let v = before[i.id] { if v != i.version { n += 1 } } else { n += 1 }
        }
        return n + before.keys.filter { !afterIDs.contains($0) }.count
    }

    public static func changeText(_ n: Int) -> String {
        n == 0 ? "Up to date" : n == 1 ? "1 change" : "\(n) changes"
    }

    // MARK: List sections

    public struct RowEntry: Identifiable, Equatable, Sendable {
        public var id: String
        public var model: EventRowModel
        public var overlapWith: String?
        public var accessibilityLabel: String
    }

    public enum Entry: Identifiable, Equatable, Sendable {
        case row(RowEntry)
        case empty(String)
        public var id: String {
            switch self {
            case .row(let r): r.id
            case .empty(let d): "empty-\(d)"
            }
        }
    }

    public struct DaySection: Identifiable, Equatable, Sendable {
        public var day: DayKey
        public var title: String
        public var isToday: Bool
        /// Today's finished events, listed after the upcoming ones.
        public var isEarlier: Bool
        public var count: Int
        public var entries: [Entry]
        public var id: String { isEarlier ? "\(day.rawValue)-earlier" : day.rawValue }
    }

    /// Section label: Today, Tomorrow, Yesterday, else "Tue 13 Oct".
    public static func dayTitle(_ day: DayKey, today: DayKey, format: HomeFormat) -> String {
        switch today.days(to: day) {
        case 0: "Today"
        case 1: "Tomorrow"
        case -1: "Yesterday"
        default: format.shortDate(day)
        }
    }

    /// Sections for `firstDay...lastDay`. Days with events always appear; `alwaysShow` (today, the picked day)
    /// appear even when empty. Items that began before `firstDay` are shown under `firstDay`. Today is split:
    /// what is still to come ("Today", or "Up next" while one is live), then "Earlier today".
    /// `staleAccounts`: accounts that need a sign-in; their rows read "Stale".
    public static func sections(items: [AgendaItem], now: Date, tz: TimeZone, firstDay: DayKey, lastDay: DayKey,
                                alwaysShow: Set<DayKey>, moved: Set<String>, format: HomeFormat,
                                staleAccounts: Set<String> = []) -> [DaySection] {
        let today = DayKey(now, in: tz)
        let overlap = overlaps(items)
        var byDay: [DayKey: [AgendaItem]] = [:]
        for i in items {
            let first = max(i.dayRange(in: tz).lowerBound, firstDay)
            guard first <= lastDay else { continue }
            byDay[first, default: []].append(i)
        }
        var days = Set(byDay.keys)
        for d in alwaysShow where d >= firstDay && d <= lastDay { days.insert(d) }
        var out: [DaySection] = []
        for day in days.sorted() {
            let list = (byDay[day] ?? []).sorted { a, b in
                if a.allDay != b.allDay { return a.allDay }
                return (a.start ?? .distantPast, a.title, a.id) < (b.start ?? .distantPast, b.title, b.id)
            }
            func entry(_ i: AgendaItem) -> Entry {
                .row(row(i, past: isPast(i, now: now, today: today, tz: tz), overlap: overlap[i.id], moved: moved.contains(i.id),
                         stale: staleAccounts.contains(i.accountId), format: format))
            }
            let title = dayTitle(day, today: today, format: format)
            if day != today {
                out.append(DaySection(day: day, title: title, isToday: false, isEarlier: false, count: list.count,
                                      entries: list.isEmpty ? [.empty(day.rawValue)] : list.map(entry)))
                continue
            }
            let past = list.filter { isPast($0, now: now, today: today, tz: tz) }
            let coming = list.filter { !isPast($0, now: now, today: today, tz: tz) }
            let live = coming.contains { i in !i.allDay && (i.start.map { $0 <= now } ?? false) }
            if !coming.isEmpty || past.isEmpty {
                out.append(DaySection(day: day, title: live ? "Up next" : "Today", isToday: true, isEarlier: false,
                                      count: coming.count, entries: coming.isEmpty ? [.empty(day.rawValue)] : coming.map(entry)))
            }
            if !past.isEmpty {
                out.append(DaySection(day: day, title: "Earlier today", isToday: true, isEarlier: true, count: past.count,
                                      entries: past.map(entry)))
            }
        }
        return out
    }

    static func isPast(_ i: AgendaItem, now: Date, today: DayKey, tz: TimeZone) -> Bool {
        if i.allDay { return i.dayRange(in: tz).upperBound < today }
        guard let s = i.start else { return false }
        return (i.end ?? s) <= now
    }

    static func row(_ i: AgendaItem, past: Bool, overlap: String?, moved: Bool, stale: Bool = false, format: HomeFormat) -> RowEntry {
        let status = statusWord(i, moved: moved, stale: stale)
        let start = i.start.map(format.time) ?? ""
        let end = i.end.flatMap { e in (i.start.map { e > $0 } ?? false) ? format.time(e) : nil }
        let model = EventRowModel(id: i.id, title: i.title, start: start, end: end,
                                  location: i.location.flatMap { $0.isEmpty ? nil : $0 }, color: AccountColor(key: i.color),
                                  status: status, isAllDay: i.allDay, isPast: past, overlaps: false,
                                  statusDetail: status == .moved && !start.isEmpty ? start : nil, overlapWith: overlap)
        var label = EventRowFormatter.accessibilityLabel(
            title: model.title, start: start, end: end, location: model.location, status: status?.rawValue,
            isAllDay: i.allDay, overlaps: overlap != nil, isPast: past)
        if let overlap { label += " with \(overlap)" }
        return RowEntry(id: i.id, model: model, overlapWith: overlap, accessibilityLabel: label)
    }

    // MARK: Invitations (board r-list)

    /// Upcoming invitations that still need an answer, soonest first.
    public static func invitations(_ items: [AgendaItem], now: Date) -> [AgendaItem] {
        items.filter {
            $0.myResponse == "needsAction" && $0.canRSVP && $0.status != "cancelled" && ($0.end ?? $0.start ?? now) > now
        }.sorted { ($0.start ?? .distantFuture, $0.id) < ($1.start ?? .distantFuture, $1.id) }
    }

    /// "2 invitations" + "need a reply."
    public static func invitationsBanner(count: Int) -> BannerText? {
        count == 0 ? nil : BannerText(title: count == 1 ? "1 invitation" : "\(count) invitations",
                                      message: count == 1 ? "needs a reply." : "need a reply.")
    }

    /// Mono column ("19:00", "Tue", "All day") and second line ("Today", "Tomorrow", "Tue 13 Oct").
    public static func inviteWhen(_ i: AgendaItem, now: Date, tz: TimeZone, format: HomeFormat) -> (column: String, sub: String) {
        let today = DayKey(now, in: tz)
        let day = i.allDay ? DayKey(i.date ?? today.rawValue) : DayKey(i.start ?? now, in: tz)
        var sub = dayTitle(day, today: today, format: format)
        let from = i.organizer.flatMap { o -> String? in
            let n = (o.name?.split(separator: " ").first.map(String.init)) ?? o.email.split(separator: "@").first.map(String.init)
            return o.isSelf == true ? nil : n
        }
        if i.allDay { return ("All day", from.map { "\(sub) \u{00B7} from \($0)" } ?? sub) }
        if day == today { return (format.time(i.start ?? now), from.map { "\(sub) \u{00B7} from \($0)" } ?? sub) }
        let weekday = String(format.shortDate(day).split(separator: " ").first ?? "")
        sub = "\(sub) \u{00B7} \(format.time(i.start ?? now))"
        return (weekday, from.map { "\(sub) \u{00B7} from \($0)" } ?? sub)
    }

    // MARK: Banners

    public struct BannerText: Equatable, Sendable {
        public var title: String
        public var message: String
    }

    /// "[account] needs you to sign in again. Its events are paused."
    public static func signInBanner(_ accounts: [Account]) -> BannerText? {
        let need = accounts.filter { $0.statusKind == .needsReauth }
        switch need.count {
        case 0: return nil
        case 1: return BannerText(title: need[0].displayName.isEmpty ? need[0].email : need[0].displayName,
                                  message: "needs you to sign in again. Its events are paused.")
        default: return BannerText(title: "\(need.count) accounts", message: "need you to sign in again. Their events are paused.")
        }
    }

    /// "Can't reach Google. Its events may be out of date." for accounts the hub could not sync (status error).
    public static func errorBanner(_ accounts: [Account]) -> BannerText? {
        let bad = accounts.filter { $0.statusKind == .error }
        func provider(_ a: Account) -> String { a.provider == "google" ? "Google" : a.provider == "ics" ? "a calendar link" : a.provider.capitalized }
        switch bad.count {
        case 0: return nil
        case 1: return BannerText(title: "Can\u{2019}t reach \(provider(bad[0])).",
                                  message: "\(bad[0].displayName.isEmpty ? bad[0].email : bad[0].displayName) may be out of date.")
        default: return BannerText(title: "Can\u{2019}t reach \(bad.count) accounts.", message: "Their events may be out of date.")
        }
    }

    /// "from 10:19" same day, "from yesterday 21:40", else "from Sat 10 Oct".
    public static func ageText(_ date: Date, now: Date, format: HomeFormat) -> String {
        let d = DayKey(date, in: format.tz), today = DayKey(now, in: format.tz)
        switch today.days(to: d) {
        case 0: return "from \(format.time(date))"
        case -1: return "from yesterday \(format.time(date))"
        default: return "from \(format.shortDate(d))"
        }
    }

    public enum ConnectionKind: Equatable, Sendable { case offline, unreachable }

    public struct ConnectionBanner: Equatable, Sendable {
        public var kind: ConnectionKind
        public var text: BannerText
        public var actionTitle: String { kind == .offline ? "Retry" : "Check" }
    }

    /// Phone offline = amber "Offline."; hub unreachable = red "Can't reach your server." (canvas y-offline / y-down).
    /// Only one banner at a time; nil when all is well. Saved rows stay on screen either way.
    public static func connectionBanner(state: SyncState, lastSyncAt: Date?, isOnline: Bool = true, now: Date,
                                        format: HomeFormat) -> ConnectionBanner? {
        let age: Date?
        switch state {
        case .idle, .syncing: return nil
        case .stale(let since): age = lastSyncAt ?? since
        case .offline, .error: age = lastSyncAt
        }
        let from = age.map { ageText($0, now: now, format: format) }
        if !isOnline {
            return .init(kind: .offline, text: BannerText(title: "Offline.", message: from.map { "Showing your saved agenda \($0)." } ?? "Nothing is saved on this phone yet."))
        }
        return .init(kind: .unreachable, text: BannerText(
            title: "Can\u{2019}t reach your server.",
            message: from.map { "This agenda is \($0) and may be out of date." } ?? "Nothing is saved on this phone yet."))
    }
}

// MARK: - Month grid

public enum MonthGrid {
    public static func firstOfMonth(_ day: DayKey) -> DayKey { DayKey(String(day.rawValue.prefix(8)) + "01") }

    public static func adding(months: Int, to day: DayKey) -> DayKey {
        let p = day.rawValue.split(separator: "-").compactMap { Int($0) }
        guard p.count == 3 else { return day }
        let idx = p[0] * 12 + (p[1] - 1) + months
        return DayKey(String(format: "%04d-%02d-01", idx / 12, idx % 12 + 1))
    }

    /// Whole weeks covering the month, starting on `weekStart` (0 = Sunday ... 6 = Saturday).
    public static func cells(month: DayKey, weekStart: Int) -> [DayKey] {
        let first = firstOfMonth(month)
        let ws = ((weekStart % 7) + 7) % 7
        let offset = (first.weekday - ws + 7) % 7
        let length = first.days(to: adding(months: 1, to: first))
        let total = ((offset + length + 6) / 7) * 7
        let start = first.adding(days: -offset)
        return (0..<total).map { start.adding(days: $0) }
    }

    /// Single-letter weekday headers starting on `weekStart`.
    public static func weekdayLetters(weekStart: Int) -> [String] {
        let all = ["S", "M", "T", "W", "T", "F", "S"]
        let ws = ((weekStart % 7) + 7) % 7
        return (0..<7).map { all[(ws + $0) % 7] }
    }

    /// "Mon Tue Wed ..." starting on `weekStart`.
    public static func weekdayNames(weekStart: Int) -> [String] {
        let all = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]
        let ws = ((weekStart % 7) + 7) % 7
        return (0..<7).map { all[(ws + $0) % 7] }
    }

    public static func inMonth(_ day: DayKey, month: DayKey) -> Bool {
        day.rawValue.prefix(7) == month.rawValue.prefix(7)
    }
}
