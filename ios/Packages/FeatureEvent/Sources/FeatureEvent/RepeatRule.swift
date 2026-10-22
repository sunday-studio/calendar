import CalendarCore
import Foundation

/// The "Custom" repeat form as plain data: every N days/weeks/months/years, on these weekdays, ends never / on a date / after N.
public struct RepeatRule: Equatable, Sendable {
    public enum Unit: String, CaseIterable, Sendable {
        case day = "DAILY", week = "WEEKLY", month = "MONTHLY", year = "YEARLY"
        public var title: String { switch self { case .day: "day"; case .week: "week"; case .month: "month"; case .year: "year" } }
    }
    public enum Ends: Equatable, Sendable { case never, on(DayKey), after(Int) }

    public var unit: Unit = .week
    public var interval = 1
    /// 0 = Sunday ... 6 = Saturday. Only used for weekly rules.
    public var weekdays: Set<Int> = []
    public var ends: Ends = .never

    public init(unit: Unit = .week, interval: Int = 1, weekdays: Set<Int> = [], ends: Ends = .never) {
        self.unit = unit; self.interval = max(1, min(interval, 99)); self.weekdays = weekdays; self.ends = ends
    }

    static let codes = ["SU", "MO", "TU", "WE", "TH", "FR", "SA"]

    /// RFC 5545 RRULE without the prefix. A weekly rule with no weekday chosen is left to the start date's weekday.
    public var rrule: String {
        var parts = ["FREQ=\(unit.rawValue)"]
        if interval > 1 { parts.append("INTERVAL=\(interval)") }
        if unit == .week, !weekdays.isEmpty {
            parts.append("BYDAY=" + weekdays.sorted { ($0 + 6) % 7 < ($1 + 6) % 7 }.map { Self.codes[$0] }.joined(separator: ","))
        }
        switch ends {
        case .never: break
        case .after(let n): parts.append("COUNT=\(max(1, n))")
        case .on(let d): parts.append("UNTIL=\(d.rawValue.replacingOccurrences(of: "-", with: ""))T235959Z")
        }
        return parts.joined(separator: ";")
    }

    /// Reads back what `rrule` writes (and the common hand-written forms). nil for rules the form can't express.
    public init?(parsing rrule: String) {
        var kv: [String: String] = [:]
        let body = rrule.hasPrefix("RRULE:") ? String(rrule.dropFirst(6)) : rrule
        for p in body.split(separator: ";") {
            let t = p.split(separator: "=", maxSplits: 1).map(String.init)
            if t.count == 2 { kv[t[0].uppercased()] = t[1] }
        }
        guard let f = kv["FREQ"], let unit = Unit(rawValue: f.uppercased()) else { return nil }
        let allowed: Set<String> = ["FREQ", "INTERVAL", "BYDAY", "COUNT", "UNTIL"]
        guard Set(kv.keys).isSubset(of: allowed) else { return nil }
        var days = Set<Int>()
        if let by = kv["BYDAY"] {
            for c in by.split(separator: ",") {
                guard let i = Self.codes.firstIndex(of: String(c)) else { return nil }   // "2TU" (ordinals) is not expressible
                days.insert(i)
            }
        }
        var ends = Ends.never
        if let c = kv["COUNT"], let n = Int(c) { ends = .after(n) }
        else if let u = kv["UNTIL"], u.count >= 8 {
            let d = u.prefix(8); ends = .on(DayKey("\(d.prefix(4))-\(d.dropFirst(4).prefix(2))-\(d.dropFirst(6).prefix(2))"))
        }
        self.init(unit: unit, interval: Int(kv["INTERVAL"] ?? "") ?? 1, weekdays: days, ends: ends)
    }

    /// "Every 2 weeks on Mon, Wed, until 31 Dec"
    public func summary(format: EventTimeFormat) -> String {
        var s = interval == 1 ? "Every \(unit.title)" : "Every \(interval) \(unit.title)s"
        if unit == .week, !weekdays.isEmpty {
            let names = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]
            s += " on " + weekdays.sorted { ($0 + 6) % 7 < ($1 + 6) % 7 }.map { names[$0] }.joined(separator: ", ")
        }
        switch ends {
        case .never: break
        case .after(let n): s += ", \(n) time\(n == 1 ? "" : "s")"
        case .on(let d): s += ", until \(format.shortDate(d))"
        }
        return s
    }
}
