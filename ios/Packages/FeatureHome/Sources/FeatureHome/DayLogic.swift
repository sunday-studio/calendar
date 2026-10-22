import CalendarCore
import CalendarUI
import Foundation

/// One timed block on a day column, already clipped to the day and assigned a column.
public struct DayBlock: Identifiable, Equatable, Sendable {
    public var item: AgendaItem
    public var start: Date
    public var end: Date
    public var column: Int
    public var columnCount: Int
    /// Starts before / ends after this day.
    public var continuesBefore: Bool
    public var continuesAfter: Bool
    public var id: String { item.id }
}

public struct DayContent: Equatable, Sendable {
    public var allDay: [AgendaItem] = []
    public var blocks: [DayBlock] = []
    public var isEmpty: Bool { allDay.isEmpty && blocks.isEmpty }
    public static let empty = DayContent()
}

public enum DayLogic {
    /// Shortest block (in time) used for overlap detection and drawing: 15 min.
    public static let minDuration: TimeInterval = 15 * 60

    public static func visible(_ i: AgendaItem) -> Bool { i.status != "cancelled" }

    public static func content(for day: DayKey, items: [AgendaItem], tz: TimeZone) -> DayContent {
        let cal = DayKey.calendar(tz)
        let start = day.startOfDay(in: tz), end = day.adding(days: 1).startOfDay(in: tz)
        var out = DayContent()
        var timed: [(AgendaItem, Date, Date)] = []
        for i in items where visible(i) && i.dayRange(calendar: cal).contains(day) {
            if i.allDay { out.allDay.append(i); continue }
            guard let s = i.start else { continue }
            let e = max(i.end ?? s, s)
            timed.append((i, max(s, start), min(max(e, s), end)))
        }
        out.allDay.sort { ($0.title, $0.id) < ($1.title, $1.id) }
        timed.sort { ($0.1, $0.0.id) < ($1.1, $1.0.id) }
        let slots = OverlapLayout.layout(timed.map { ($0.1.timeIntervalSince1970, $0.2.timeIntervalSince1970) },
                                         minDuration: minDuration)
        out.blocks = zip(timed, slots).map { t, slot in
            DayBlock(item: t.0, start: t.1, end: t.2, column: slot.column, columnCount: slot.columnCount,
                     continuesBefore: (t.0.start ?? t.1) < start, continuesAfter: (t.0.end ?? t.2) > end)
        }
        return out
    }

    /// Content for every day in `from...to`; days without items are omitted.
    public static func contents(items: [AgendaItem], from: DayKey, to: DayKey, tz: TimeZone) -> [DayKey: DayContent] {
        let cal = DayKey.calendar(tz)
        var perDay: [DayKey: [AgendaItem]] = [:]
        for i in items where visible(i) {
            let r = i.dayRange(calendar: cal)
            let lo = max(r.lowerBound, from), hi = min(r.upperBound, to)
            guard lo <= hi else { continue }
            for d in lo.through(hi) { perDay[d, default: []].append(i) }
        }
        return perDay.reduce(into: [:]) { acc, kv in acc[kv.key] = content(for: kv.key, items: kv.value, tz: tz) }
    }

    /// Where the grid should open: a bit above now for today, above the first event otherwise, else 08:00.
    public static func initialScrollY(day: DayKey, isToday: Bool, content: DayContent, now: Date, grid: DayGrid) -> CGFloat {
        let lead = grid.hourHeight * 2.5   // now sits about a quarter of the way down
        let target: CGFloat
        if isToday { target = grid.y(for: now) - lead }
        else if let first = content.blocks.min(by: { $0.start < $1.start }) { target = grid.y(for: first.start) - lead * 0.5 }
        else { target = grid.hourHeight * 8 - lead * 0.5 }
        return min(max(target, 0), max(grid.height - grid.hourHeight * 6, 0))
    }

    /// Hour label for the gutter in `zone` at the real instant `date` ("09:00" / "9 AM" per locale).
    public static func gutterLabel(_ date: Date, zone: TimeZone, locale: Locale) -> String {
        let f = DateFormatter()
        f.locale = locale; f.timeZone = zone
        f.setLocalizedDateFormatFromTemplate(locale.hourCycleIs24 ? "HH:mm" : "j")
        return f.string(from: date)
    }

    /// Hour label for the grid's left column: "09:00" / "09" (week) on a 24-hour clock, "9 AM" / "9" on a 12-hour one.
    public static func hourLabel(_ hour: Int, compact: Bool, time24: Bool) -> String {
        if time24 { return String(format: compact ? "%02d" : "%02d:00", hour) }
        let h = hour % 12 == 0 ? 12 : hour % 12
        return compact ? "\(h)" : "\(h) \(hour < 12 ? "AM" : "PM")"
    }

    public static func accessibilityLabel(_ b: DayBlock, format: HomeFormat) -> String {
        let item = b.item
        var parts = [item.title.isEmpty ? "(No title)" : item.title]
        if let s = item.start { parts.append("\(format.time(s)) to \(format.time(item.end ?? s))") }
        if let l = item.location, !l.isEmpty { parts.append(l) }
        if item.myResponse == "declined" { parts.append("declined") }
        else if item.myResponse == "tentative" { parts.append("maybe") }
        if b.columnCount > 1 { parts.append("overlaps another event") }
        return parts.joined(separator: ", ")
    }

    public static func dayAccessibility(_ day: DayKey, content: DayContent, format: HomeFormat) -> String {
        let n = content.allDay.count + content.blocks.count
        let head = "\(format.weekday(day)) \(format.dayMonth(day))"
        return n == 0 ? "\(head), no events" : "\(head), \(n == 1 ? "1 event" : "\(n) events")"
    }

    /// The seven days of the week containing `day`, starting on `weekStart` (0 = Sunday).
    public static func week(containing day: DayKey, weekStart: Int) -> [DayKey] {
        let back = (day.weekday - weekStart + 7) % 7
        let first = day.adding(days: -back)
        return (0..<7).map { first.adding(days: $0) }
    }

    /// Synthetic busy day for the stress screen: `count` blocks, many overlapping.
    public static func stressItems(day: DayKey, tz: TimeZone, count: Int, seed: UInt64 = 7) -> [AgendaItem] {
        var state = seed
        func next(_ n: Int) -> Int {
            state = state &* 6364136223846793005 &+ 1442695040888963407
            return Int((state >> 33) % UInt64(n))
        }
        let base = day.startOfDay(in: tz)
        let colors = AccountColor.allCases.map(\.rawValue)
        return (0..<count).map { k in
            let s = base.addingTimeInterval(TimeInterval(next(22 * 4) * 900 + 3600))
            let e = s.addingTimeInterval(TimeInterval((1 + next(8)) * 900))
            return AgendaItem(id: "stress\(k)", eventId: "stress\(k)", calendarId: "c", accountId: "a", color: colors[k % colors.count],
                              title: "Stress block \(k)", allDay: false, start: s, end: e, tzid: tz.identifier,
                              location: k % 3 == 0 ? "Room \(k)" : nil, myResponse: "accepted", canEdit: true)
        }
    }
}

extension Locale {
    /// True when the locale's short time format uses a 24 hour clock.
    var hourCycleIs24: Bool {
        let f = DateFormatter.dateFormat(fromTemplate: "j", options: 0, locale: self) ?? ""
        return !f.contains("a")
    }
}
