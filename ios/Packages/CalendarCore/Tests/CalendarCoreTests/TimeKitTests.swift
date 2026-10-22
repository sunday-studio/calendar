import Foundation
import Testing
@testable import CalendarCore

private struct SeededRNG: RandomNumberGenerator {
    var state: UInt64
    mutating func next() -> UInt64 {
        state = state &* 6364136223846793005 &+ 1442695040888963407
        return state
    }
}

@Suite struct OverlapLayoutTests {
    private func iv(_ s: Double, _ e: Double) -> (start: TimeInterval, end: TimeInterval) { (s, e) }

    @Test func empty() { #expect(OverlapLayout.layout([]).isEmpty) }

    @Test func singleAndBackToBackStayFullWidth() {
        let out = OverlapLayout.layout([iv(0, 60), iv(60, 120)])
        #expect(out.map(\.column) == [0, 0])
        #expect(out.map(\.columnCount) == [1, 1])
        #expect(out[0].cluster != out[1].cluster)
    }

    @Test func twoOverlap() {
        let out = OverlapLayout.layout([iv(0, 60), iv(30, 90)])
        #expect(out.map(\.column) == [0, 1])
        #expect(out.map(\.columnCount) == [2, 2])
    }

    @Test func reusesFreedColumnInsideCluster() {
        // A long, B short then C starts after B ends but while A still runs: C reuses B's column.
        let out = OverlapLayout.layout([iv(0, 100), iv(10, 30), iv(40, 60)])
        #expect(out.map(\.column) == [0, 1, 1])
        #expect(out.map(\.columnCount) == [2, 2, 2])
    }

    @Test func threeWay() {
        let out = OverlapLayout.layout([iv(0, 60), iv(0, 60), iv(0, 60)])
        #expect(Set(out.map(\.column)) == [0, 1, 2])
        #expect(out.allSatisfy { $0.columnCount == 3 })
    }

    @Test func minDurationMakesTinyEventsClaimSpace() {
        let out = OverlapLayout.layout([iv(0, 3600), iv(600, 600)], minDuration: 900)
        #expect(out.map(\.columnCount) == [2, 2])
        let none = OverlapLayout.layout([iv(0, 3600), iv(600, 600)], minDuration: 0)
        #expect(none.map(\.columnCount) == [2, 2]) // zero-length inside a running event still overlaps it
    }

    @Test func inputOrderDoesNotChangeColumnsPerItem() {
        let items = [iv(0, 100), iv(10, 30), iv(40, 60), iv(200, 300)]
        let a = OverlapLayout.layout(items)
        let perm = [3, 1, 0, 2]
        let b = OverlapLayout.layout(perm.map { items[$0] })
        for (k, i) in perm.enumerated() { #expect(b[k].column == a[i].column) }
    }

    @Test func propertyNoOverlapWithinColumnAndDeterministic() {
        var rng = SeededRNG(state: 42)
        for round in 0..<200 {
            let n = Int.random(in: 1...60, using: &rng)
            let items: [(start: TimeInterval, end: TimeInterval)] = (0..<n).map { _ in
                let s = Double(Int.random(in: 0...1000, using: &rng))
                return (s, s + Double(Int.random(in: 0...200, using: &rng)))
            }
            let md = Double(Int.random(in: 0...20, using: &rng))
            let out = OverlapLayout.layout(items, minDuration: md)
            #expect(out == OverlapLayout.layout(items, minDuration: md), "deterministic round \(round)")
            #expect(out.count == n)
            for a in 0..<n {
                #expect(out[a].column < out[a].columnCount)
                for b in (a + 1)..<max(n, a + 1) {
                    let ea = max(items[a].end, items[a].start + md), eb = max(items[b].end, items[b].start + md)
                    let overlap = items[a].start < eb && items[b].start < ea
                    if out[a].cluster == out[b].cluster {
                        #expect(out[a].columnCount == out[b].columnCount)
                        if out[a].column == out[b].column { #expect(!overlap, "same column overlap round \(round)") }
                    } else {
                        #expect(!overlap, "overlapping items must share a cluster")
                    }
                }
            }
        }
    }
}

@Suite struct TimeSnapTests {
    @Test func rounding() {
        #expect(TimeSnap.snap(7 * 60) == 0)
        #expect(TimeSnap.snap(8 * 60) == 900)
        #expect(TimeSnap.snap(14 * 60, mode: .down) == 0)
        #expect(TimeSnap.snap(1, mode: .up) == 900)
        #expect(TimeSnap.snap(900, mode: .up) == 900)
        #expect(TimeSnap.snap(100, step: 0) == 100)
    }
}

@Suite struct DayGridTests {
    private let london = TimeZone(identifier: "Europe/London")!
    private let ny = TimeZone(identifier: "America/New_York")!
    private let kolkata = TimeZone(identifier: "Asia/Kolkata")!

    @Test func normalDay() {
        let g = DayGrid(day: "2026-10-13", tz: london, hourHeight: 50)
        #expect(g.hours == 24)
        #expect(g.height == 1200)
        #expect(g.hourMarks().count == 24)
        #expect(g.hourMarks().first?.hour == 0)
        #expect(g.hourMarks().last?.hour == 23)
    }

    @Test func springForwardDayHas23Hours() {
        let g = DayGrid(day: "2026-03-29", tz: london, hourHeight: 50)
        #expect(g.hours == 23)
        let hours = g.hourMarks().map(\.hour)
        #expect(hours.count == 23)
        #expect(!hours.contains(1)) // 01:00 does not exist
        #expect(g.height == 1150)
    }

    @Test func fallBackDayHas25Hours() {
        let g = DayGrid(day: "2026-10-25", tz: london, hourHeight: 40)
        #expect(g.hours == 25)
        let hours = g.hourMarks().map(\.hour)
        #expect(hours.count == 25)
        #expect(hours.filter { $0 == 1 }.count == 2) // 01:00 happens twice
        #expect(g.height == 1000)
    }

    @Test func yTimeRoundTrip() {
        for (day, tz) in [("2026-10-25", london), ("2026-03-08", ny), ("2026-10-13", kolkata)] as [(DayKey, TimeZone)] {
            let g = DayGrid(day: day, tz: tz, hourHeight: 52)
            for y in stride(from: 0 as CGFloat, through: g.height, by: 13) {
                #expect(abs(g.y(for: g.date(atY: y)) - y) < 0.001)
            }
            #expect(g.date(atY: -10) == g.start)
            #expect(g.date(atY: g.height + 100) == g.end)
        }
    }

    @Test func frameClipsToDay() {
        let g = DayGrid(day: "2026-10-13", tz: london, hourHeight: 60)
        let s = g.start
        let f = g.frame(start: s.addingTimeInterval(3600), end: s.addingTimeInterval(5400))!
        #expect(f.y == 60 && f.height == 30)
        // Starts the previous evening, ends 02:00.
        let spill = g.frame(start: s.addingTimeInterval(-3600), end: s.addingTimeInterval(7200))!
        #expect(spill.y == 0 && spill.height == 120)
        // Entirely other day.
        #expect(g.frame(start: s.addingTimeInterval(-7200), end: s.addingTimeInterval(-3600)) == nil)
        #expect(g.frame(start: g.end, end: g.end.addingTimeInterval(3600)) == nil)
        // Minimum height.
        let tiny = g.frame(start: s.addingTimeInterval(3600), end: s.addingTimeInterval(3600), minHeight: 20)!
        #expect(tiny.height == 20)
    }

    @Test func snappedUsesWallClock() {
        let g = DayGrid(day: "2026-10-13", tz: kolkata, hourHeight: 60) // UTC+5:30
        let d = g.snapped(atY: 60 * 9.4) // 09:24 -> 09:30
        #expect(g.clockMinutes(d) == 9 * 60 + 30)
    }
}

@Suite struct TimeKitPropertyTests {
    @Test func columnsEqualMaxDepthPerCluster() {
        var rng = SeededRNG(state: 7)
        for round in 0..<200 {
            let n = Int.random(in: 1...40, using: &rng)
            let items: [(start: TimeInterval, end: TimeInterval)] = (0..<n).map { _ in
                let s = Double(Int.random(in: 0...500, using: &rng))
                return (s, s + Double(Int.random(in: 1...120, using: &rng)))
            }
            let out = OverlapLayout.layout(items)
            // Interval graphs are perfect: greedy column count must equal the max number of simultaneous items.
            for c in Set(out.map(\.cluster)) {
                let members = out.indices.filter { out[$0].cluster == c }
                let depth = members.map { i in members.filter { items[$0].start <= items[i].start && items[$0].end > items[i].start }.count }.max() ?? 1
                #expect(out[members[0]].columnCount == depth, "optimal columns round \(round)")
            }
        }
    }

    @Test func snapIsIdempotentAndInsideDayOnDSTDays() {
        let london = TimeZone(identifier: "Europe/London")!
        for day in ["2026-03-29", "2026-10-25", "2026-10-13"] as [DayKey] {
            let g = DayGrid(day: day, tz: london, hourHeight: 48)
            for y in stride(from: -5 as CGFloat, through: g.height + 5, by: 7) {
                let s = g.snapped(atY: y)
                #expect(s >= g.start && s <= g.end)
                #expect(TimeSnap.snap(s) == s || s == g.start || s == g.end)
            }
        }
    }
}
