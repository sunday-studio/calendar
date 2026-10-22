import Foundation

// TimeKit: pure time-grid maths (no SwiftUI). ENGINEERING 8.5.
// - OverlapLayout: cluster sweep + greedy column assignment (interval-graph colouring).
// - TimeSnap: round to a step (15 min default).
// - DayGrid: y <-> time for one local day, including 23 and 25 hour DST days.

public enum OverlapLayout {
    public struct Slot: Equatable, Sendable {
        /// Index into the input array.
        public var index: Int
        /// 0-based column inside the cluster.
        public var column: Int
        /// Number of columns of the whole cluster this item belongs to (>= 1).
        public var columnCount: Int
        /// Cluster number (0-based, by start time).
        public var cluster: Int
    }

    /// `intervals` are `[start, end)` in any monotonic unit (seconds). Items shorter than `minDuration`
    /// are treated as `minDuration` long, so tiny events still claim their own column when they touch others.
    /// Output has one slot per input item, in input order. Deterministic: ties break by longer first, then index.
    public static func layout(_ intervals: [(start: TimeInterval, end: TimeInterval)],
                              minDuration: TimeInterval = 0) -> [Slot] {
        let n = intervals.count
        guard n > 0 else { return [] }
        let eff = intervals.map { (s: $0.start, e: max($0.end, $0.start + minDuration)) }
        let order = (0..<n).sorted { a, b in
            if eff[a].s != eff[b].s { return eff[a].s < eff[b].s }
            if eff[a].e != eff[b].e { return eff[a].e > eff[b].e }
            return a < b
        }
        var slots = [Slot](repeating: Slot(index: 0, column: 0, columnCount: 1, cluster: 0), count: n)
        var clusterMembers: [Int] = []
        var columnEnds: [TimeInterval] = [] // end of the last item per column
        var clusterEnd = -TimeInterval.infinity
        var clusterNo = -1

        func close() {
            let count = max(columnEnds.count, 1)
            for i in clusterMembers { slots[i].columnCount = count }
            clusterMembers.removeAll(); columnEnds.removeAll()
        }

        for i in order {
            let (s, e) = (eff[i].s, eff[i].e)
            if !clusterMembers.isEmpty && s >= clusterEnd { close() }
            if clusterMembers.isEmpty { clusterNo += 1; clusterEnd = -.infinity }
            // Greedy: first column whose last item has ended.
            var col = columnEnds.firstIndex { $0 <= s } ?? -1
            if col < 0 { columnEnds.append(e); col = columnEnds.count - 1 } else { columnEnds[col] = e }
            clusterEnd = max(clusterEnd, e)
            slots[i] = Slot(index: i, column: col, columnCount: 1, cluster: clusterNo)
            clusterMembers.append(i)
        }
        close()
        return slots
    }
}

public enum TimeSnap {
    public enum Mode: Sendable { case nearest, down, up }

    public static let defaultStep: TimeInterval = 15 * 60

    public static func snap(_ t: TimeInterval, step: TimeInterval = defaultStep, mode: Mode = .nearest) -> TimeInterval {
        guard step > 0 else { return t }
        let q = t / step
        switch mode {
        case .nearest: return q.rounded(.toNearestOrAwayFromZero) * step
        case .down: return q.rounded(.down) * step
        case .up: return q.rounded(.up) * step
        }
    }

    public static func snap(_ d: Date, step: TimeInterval = defaultStep, mode: Mode = .nearest) -> Date {
        Date(timeIntervalSince1970: snap(d.timeIntervalSince1970, step: step, mode: mode))
    }
}

/// One local day laid out on a vertical axis. Time is real elapsed time, so a DST day is 23 or 25 hours tall.
public struct DayGrid: Equatable, Sendable {
    public struct HourMark: Equatable, Sendable, Identifiable {
        public var date: Date
        /// Wall-clock hour (0...23) at `date`; repeats on a 25 hour day.
        public var hour: Int
        public var y: CGFloat
        public var id: Int { Int(date.timeIntervalSince1970) }
    }

    public let day: DayKey
    public let tz: TimeZone
    public let start: Date
    public let end: Date
    public let hourHeight: CGFloat

    public init(day: DayKey, tz: TimeZone, hourHeight: CGFloat) {
        self.day = day; self.tz = tz; self.hourHeight = hourHeight
        start = day.startOfDay(in: tz)
        end = day.adding(days: 1).startOfDay(in: tz)
    }

    public var duration: TimeInterval { end.timeIntervalSince(start) }
    /// 23, 24 or 25.
    public var hours: Double { duration / 3600 }
    public var height: CGFloat { CGFloat(hours) * hourHeight }

    public func y(for date: Date) -> CGFloat {
        CGFloat(date.timeIntervalSince(start) / 3600) * hourHeight
    }

    /// `y` is clamped to the day.
    public func date(atY y: CGFloat) -> Date {
        let c = min(max(y, 0), height)
        return start.addingTimeInterval(TimeInterval(c / hourHeight) * 3600)
    }

    public func snapped(atY y: CGFloat, step: TimeInterval = TimeSnap.defaultStep) -> Date {
        let d = date(atY: y)
        let local = TimeInterval(tz.secondsFromGMT(for: d))
        // Snap on local wall-clock so :15 means :15 on the clock even in half-hour-offset zones.
        let s = TimeSnap.snap(d.timeIntervalSince1970 + local, step: step) - local
        return min(max(Date(timeIntervalSince1970: s), start), end)
    }

    /// Top and height for a timed item, clipped to the day. nil if it does not touch the day.
    public func frame(start s: Date, end e: Date, minHeight: CGFloat = 0) -> (y: CGFloat, height: CGFloat)? {
        let e = max(e, s)
        if e <= start && !(s == e && s == start) { return nil }
        if s >= end { return nil }
        let top = y(for: max(s, start))
        let bottom = y(for: min(e, end))
        return (top, min(max(bottom - top, minHeight), max(height - top, minHeight)))
    }

    /// One mark per real hour boundary in the day (23, 24 or 25 marks), top-down.
    public func hourMarks() -> [HourMark] {
        var cal = Calendar(identifier: .gregorian); cal.timeZone = tz
        var out: [HourMark] = []
        // Walk real hours from the first whole local hour at or after `start`; offsets with :30 zones land on :30.
        var t = start
        while t < end {
            out.append(HourMark(date: t, hour: cal.component(.hour, from: t), y: y(for: t)))
            t = t.addingTimeInterval(3600)
        }
        return out
    }

    /// Minutes since local midnight, as shown on the clock (not elapsed).
    public func clockMinutes(_ d: Date) -> Int {
        var cal = Calendar(identifier: .gregorian); cal.timeZone = tz
        return cal.component(.hour, from: d) * 60 + cal.component(.minute, from: d)
    }
}
