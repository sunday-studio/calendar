import CalendarCore
import Foundation

/// Pure maths for moving and resizing blocks on the time grid (boards v-drag, v-resize).
public enum DragLogic {
    public static let step: TimeInterval = 15 * 60

    /// nil = can be lifted. Otherwise the plain-words reason for the toast.
    public static func blockReason(_ i: AgendaItem, continues: Bool = false) -> String? {
        if i.allDay || continues { return "All-day and multi-day events are changed in the form" }
        if !i.canEdit { return i.canRSVP ? "Only the organiser can move it" : "Read only calendar" }
        return nil
    }

    /// New times when the block's TOP edge sits at `topY` on `grid`'s day. Snaps to 15 minutes and stays inside the day.
    public static func moved(duration: TimeInterval, topY: CGFloat, grid: DayGrid) -> (start: Date, end: Date) {
        var start = grid.snapped(atY: topY, step: step)
        let latest = grid.end.addingTimeInterval(-max(duration, step))
        if start > latest { start = TimeSnap.snap(latest, step: step, mode: .down) }
        if start < grid.start { start = grid.start }
        return (start, start.addingTimeInterval(duration))
    }

    /// New end when the bottom handle is dragged to `bottomY`: start never moves, 15 min minimum, last slot ends 23:45.
    public static func resizedEnd(start: Date, bottomY: CGFloat, grid: DayGrid) -> Date {
        let raw = grid.snapped(atY: bottomY, step: step)
        let minEnd = start.addingTimeInterval(step)
        let maxEnd = max(grid.end.addingTimeInterval(-step), minEnd)
        return min(max(raw, minEnd), maxEnd)
    }

    /// "Mon 12 \u{00B7} 15:45"
    public static func moveChip(start: Date, tz: TimeZone, locale: Locale) -> String {
        let d = DateFormatter(); d.locale = locale; d.timeZone = tz; d.dateFormat = "EEE d"
        let t = DateFormatter(); t.locale = locale; t.timeZone = tz; t.setLocalizedDateFormatFromTemplate(locale.hourCycleIs24 ? "HH:mm" : "jmm")
        if locale.hourCycleIs24 { t.dateFormat = "HH:mm" }
        return "\(d.string(from: start)) \u{00B7} \(t.string(from: start))"
    }

    /// "1 h 15 min \u{00B7} ends 13:45"
    public static func resizeChip(start: Date, end: Date, tz: TimeZone, locale: Locale) -> String {
        let mins = max(Int(end.timeIntervalSince(start) / 60), 1)
        let len = mins < 60 ? "\(mins) min" : (mins % 60 == 0 ? "\(mins / 60) h" : "\(mins / 60) h \(mins % 60) min")
        let t = DateFormatter(); t.locale = locale; t.timeZone = tz; t.setLocalizedDateFormatFromTemplate(locale.hourCycleIs24 ? "HH:mm" : "jmm")
        if locale.hourCycleIs24 { t.dateFormat = "HH:mm" }
        return "\(len) \u{00B7} ends \(t.string(from: end))"
    }

    // MARK: Accessibility alternative to dragging ("Move..." custom action)

    /// The steps VoiceOver and Switch Control users pick instead of dragging a block.
    public enum Nudge: CaseIterable, Sendable {
        case earlier15, later15, earlier60, later60, previousDay, nextDay

        public var title: String {
            switch self {
            case .earlier15: "15 minutes earlier"
            case .later15: "15 minutes later"
            case .earlier60: "1 hour earlier"
            case .later60: "1 hour later"
            case .previousDay: "Same time, previous day"
            case .nextDay: "Same time, next day"
            }
        }
    }

    /// New times for `nudge`, or nil when it would leave the day (time steps) or the event is not movable.
    public static func nudged(start: Date, end: Date, by nudge: Nudge, tz: TimeZone) -> (start: Date, end: Date)? {
        var cal = Calendar(identifier: .gregorian); cal.timeZone = tz
        func shifted(minutes: Int) -> (Date, Date)? {
            let s = start.addingTimeInterval(Double(minutes) * 60), e = end.addingTimeInterval(Double(minutes) * 60)
            // Stay inside the day the block starts on (same rule as dragging).
            guard cal.isDate(s, inSameDayAs: start), cal.isDate(e.addingTimeInterval(-1), inSameDayAs: start) else { return nil }
            return (s, e)
        }
        func days(_ n: Int) -> (Date, Date)? {
            guard let s = cal.date(byAdding: .day, value: n, to: start), let e = cal.date(byAdding: .day, value: n, to: end) else { return nil }
            return (s, e)
        }
        let r: (Date, Date)?
        switch nudge {
        case .earlier15: r = shifted(minutes: -15)
        case .later15: r = shifted(minutes: 15)
        case .earlier60: r = shifted(minutes: -60)
        case .later60: r = shifted(minutes: 60)
        case .previousDay: r = days(-1)
        case .nextDay: r = days(1)
        }
        return r.map { (start: $0.0, end: $0.1) }
    }
}
