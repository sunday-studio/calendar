import Foundation

/// Injectable "now" (tests use a fake clock). Not `Swift.Clock`, which is about sleeping.
public struct AppClock: Sendable {
    public var now: @Sendable () -> Date
    public init(now: @escaping @Sendable () -> Date) { self.now = now }
    public static let system = AppClock { Date() }
    public static func fixed(_ d: Date) -> AppClock { AppClock { d } }
}

/// Mutable clock for tests.
public final class TestClock: @unchecked Sendable {
    private let lock = NSLock()
    private var current: Date
    public init(_ d: Date) { current = d }
    public var now: Date { lock.withLock { current } }
    public func set(_ d: Date) { lock.withLock { current = d } }
    public func advance(_ s: TimeInterval) { lock.withLock { current = current.addingTimeInterval(s) } }
    public var clock: AppClock { AppClock { [self] in self.now } }
}

/// A calendar day in the user's time zone, "yyyy-MM-dd" (sortable as text, so SQLite can range-query it).
public struct DayKey: Hashable, Comparable, Codable, Sendable, CustomStringConvertible, ExpressibleByStringLiteral {
    public let rawValue: String
    public init(_ raw: String) { rawValue = raw }
    public init(stringLiteral value: String) { rawValue = value }
    public var description: String { rawValue }

    public static func < (a: DayKey, b: DayKey) -> Bool { a.rawValue < b.rawValue }

    public init(_ date: Date, in tz: TimeZone) {
        self.init(date, calendar: Self.calendar(tz))
    }

    public init(_ date: Date, calendar: Calendar) {
        let c = calendar.dateComponents([.year, .month, .day], from: date)
        rawValue = String(format: "%04d-%02d-%02d", c.year ?? 0, c.month ?? 0, c.day ?? 0)
    }

    public static func calendar(_ tz: TimeZone) -> Calendar {
        var cal = Calendar(identifier: .gregorian)
        cal.timeZone = tz
        return cal
    }

    private static let utc = calendar(TimeZone(identifier: "UTC")!)

    private var utcMidnight: Date? {
        let p = rawValue.split(separator: "-").compactMap { Int($0) }
        guard p.count == 3 else { return nil }
        return Self.utc.date(from: DateComponents(year: p[0], month: p[1], day: p[2]))
    }

    /// Pure civil-date arithmetic (independent of any time zone).
    public func adding(days: Int) -> DayKey {
        guard let d = utcMidnight, let r = Self.utc.date(byAdding: .day, value: days, to: d) else { return self }
        return DayKey(r, calendar: Self.utc)
    }

    /// Number of days from `self` to `other` (positive when `other` is later).
    public func days(to other: DayKey) -> Int {
        guard let a = utcMidnight, let b = other.utcMidnight else { return 0 }
        return Int((b.timeIntervalSince(a) / 86_400).rounded())
    }

    /// 0 = Sunday ... 6 = Saturday (matches the hub's working-hours days).
    public var weekday: Int {
        guard let d = utcMidnight else { return 0 }
        return Self.utc.component(.weekday, from: d) - 1
    }

    /// First instant of this day in `tz`.
    public func startOfDay(in tz: TimeZone) -> Date {
        let p = rawValue.split(separator: "-").compactMap { Int($0) }
        let cal = Self.calendar(tz)
        guard p.count == 3, let d = cal.date(from: DateComponents(year: p[0], month: p[1], day: p[2])) else { return .distantPast }
        return cal.startOfDay(for: d)
    }

    /// Inclusive list of days `self ... last`.
    public func through(_ last: DayKey) -> [DayKey] {
        let n = days(to: last)
        guard n >= 0 else { return [] }
        return (0...n).map { adding(days: $0) }
    }
}

public extension AgendaItem {
    /// First and last (inclusive) local day this item occupies in `calendar`'s time zone.
    /// Timed: day of `start` .. day of the last instant before `end`. All-day: `date` .. `endDate - 1`.
    func dayRange(calendar: Calendar) -> ClosedRange<DayKey> {
        if allDay {
            let first = DayKey(date ?? endDate ?? "0000-00-00")
            if let endDate {
                let last = DayKey(endDate).adding(days: -1)
                return last >= first ? first...last : first...first
            }
            return first...first
        }
        guard let start else { return DayKey("0000-00-00")...DayKey("0000-00-00") }
        let first = DayKey(start, calendar: calendar)
        guard let end, end > start else { return first...first }
        let last = DayKey(end.addingTimeInterval(-0.001), calendar: calendar)
        return last >= first ? first...last : first...first
    }

    func dayRange(in tz: TimeZone) -> ClosedRange<DayKey> { dayRange(calendar: DayKey.calendar(tz)) }
}

/// Window kept on device: [today - 14d, today + 90d] (ENGINEERING 8.3).
public struct SyncWindow: Equatable, Sendable {
    public static let daysBefore = 14
    public static let daysAfter = 90
    public var firstDay: DayKey
    public var lastDay: DayKey
    public var from: Date
    public var to: Date // exclusive: start of the day after lastDay

    public init(now: Date, tz: TimeZone) {
        let today = DayKey(now, in: tz)
        firstDay = today.adding(days: -Self.daysBefore)
        lastDay = today.adding(days: Self.daysAfter)
        from = firstDay.startOfDay(in: tz)
        to = lastDay.adding(days: 1).startOfDay(in: tz)
    }
}
