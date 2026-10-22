import CalendarCore
import Foundation
import Observation

/// A free-time question read on the phone from plain words ("when am I free for 2 hours this week?"). The hub does the
/// real work (`POST /v1/freetime`); this only picks duration, window and part of the day, and never guesses silently:
/// what it understood is written back in the screen title and sub line.
public struct FreeTimeAsk: Equatable, Sendable {
    public var durationMin: Int
    public var from: Date
    public var to: Date
    public var dayPart: DayPart?
    /// "This week", "Tomorrow", "Next 7 days".
    public var windowWords: String

    /// "Free for 2 hours", "Free for 30 minutes", "Free for 1 hour 30 minutes".
    public var title: String { "Free for \(Self.durationWords(durationMin))" }

    public static func durationWords(_ m: Int) -> String {
        if m < 60 { return "\(m) minutes" }
        let h = m / 60, r = m % 60
        let hw = h == 1 ? "1 hour" : "\(h) hours"
        return r == 0 ? hw : "\(hw) \(r) minutes"
    }

    /// `weekStart`: 0 Sunday, 1 Monday (hub settings).
    public static func parse(_ raw: String, now: Date, tz: TimeZone, weekStart: Int = 1) -> FreeTimeAsk? {
        let text = raw.lowercased()
        guard !text.trimmingCharacters(in: .whitespaces).isEmpty else { return nil }
        var cal = DayKey.calendar(tz)
        cal.firstWeekday = (weekStart % 7) + 1

        // Duration
        var minutes: Int?
        if let m = firstMatch(#"(\d+(?:\.\d+)?)\s*(hours?|hrs?|h)\b"#, in: text), let v = Double(m[1]) { minutes = Int((v * 60).rounded()) }
        if let m = firstMatch(#"(\d+)\s*(minutes?|mins?|m)\b"#, in: text), let v = Int(m[1]) { minutes = (minutes ?? 0) + v }
        if minutes == nil {
            if text.contains("half an hour") || text.contains("half hour") { minutes = 30 }
            else if text.contains("an hour") || text.contains("one hour") { minutes = 60 }
            else if text.contains("two hours") { minutes = 120 }
            else if text.contains("three hours") { minutes = 180 }
        }
        let duration = min(max(minutes ?? 60, 5), 1440)

        // Day part
        let part: DayPart? = text.contains("morning") ? .morning : text.contains("afternoon") ? .afternoon : text.contains("evening") ? .evening : nil

        // Window
        let startOfToday = cal.startOfDay(for: now)
        func days(_ n: Int, from d: Date) -> Date { cal.date(byAdding: .day, value: n, to: d) ?? d }
        let names = ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"]   // copy is English, not the device's symbols
        let weekdayNames = names.map { $0.lowercased() }
        var from = now, to = days(7, from: startOfToday), words = "Next 7 days"
        if text.contains("tomorrow") {
            from = days(1, from: startOfToday); to = days(2, from: startOfToday); words = "Tomorrow"
        } else if text.contains("today") || text.contains("tonight") {
            to = days(1, from: startOfToday); words = "Today"
        } else if text.contains("next week") {
            let thisWeekStart = cal.dateInterval(of: .weekOfYear, for: now)?.start ?? startOfToday
            from = days(7, from: thisWeekStart); to = days(14, from: thisWeekStart); words = "Next week"
        } else if text.contains("this week") {
            let end = cal.dateInterval(of: .weekOfYear, for: now)?.end ?? days(7, from: startOfToday)
            to = end; words = "This week"
        } else if let idx = weekdayNames.firstIndex(where: { text.contains($0) }) {
            // "before friday" / "on friday": from now up to the end of that weekday (or just that day for "on").
            let target = idx + 1
            var d = startOfToday
            var steps = 0
            while cal.component(.weekday, from: d) != target && steps < 8 { d = days(1, from: d); steps += 1 }
            let name = names[idx]
            if text.contains("before") { to = d; words = "Before \(name)" }
            else { from = max(now, d); to = days(1, from: d); words = name }
        }
        if to <= from { to = days(1, from: from) }
        return FreeTimeAsk(durationMin: duration, from: from, to: to, dayPart: part, windowWords: words)
    }

    private static func firstMatch(_ pattern: String, in s: String) -> [String]? {
        guard let re = try? NSRegularExpression(pattern: pattern), let m = re.firstMatch(in: s, range: NSRange(s.startIndex..., in: s)) else { return nil }
        return (0..<m.numberOfRanges).map { i in Range(m.range(at: i), in: s).map { String(s[$0]) } ?? "" }
    }
}

/// Find free time (canvas q-ask, q-slots, q-book, q-none).
@MainActor
@Observable
public final class FreeTimeViewModel {
    public enum Phase: Equatable, Sendable {
        case ask
        case searching
        case slots(FreeTimeResult)
        /// Nothing fits; `closest` has up to two alternatives.
        case none(FreeTimeResult)
        case failed(String)
        case booking(FreeSlot)
        case booked
    }

    public static let suggestions = ["Free for an hour tomorrow afternoon", "A 30 minute gap before Friday", "Two hours of focus next week"]

    public var text = ""
    public private(set) var phase: Phase = .ask
    public private(set) var ask: FreeTimeAsk?
    public private(set) var selected: FreeSlot?
    public var bookTitle = "Focus time"
    public var bookCalendarId = ""
    public private(set) var bookError: String?
    public private(set) var booked: EventDetail?
    public private(set) var ignoredWorkingHours = false

    public let tz: TimeZone
    public let weekStart: Int
    public private(set) var calendars: [HubCalendar]
    @ObservationIgnored private let api: any HubAssistAPI
    @ObservationIgnored private let writer: any EventWriting
    @ObservationIgnored private let clock: AppClock
    @ObservationIgnored private var attemptKey: String?

    public init(api: any HubAssistAPI, writer: any EventWriting, calendars: [HubCalendar], defaultCalendarId: String?,
                tz: TimeZone, weekStart: Int = 1, clock: AppClock = .system, text: String = "") {
        self.api = api; self.writer = writer; self.tz = tz; self.weekStart = weekStart; self.clock = clock
        self.calendars = calendars.filter { $0.access == "owner" || $0.access == "writer" }
        self.text = text
        bookCalendarId = self.calendars.first { $0.id == defaultCalendarId }?.id ?? self.calendars.first { $0.isPrimary }?.id ?? self.calendars.first?.id ?? ""
    }

    public var canSearch: Bool { !text.trimmingCharacters(in: .whitespaces).isEmpty && phase != .searching }

    public func use(suggestion: String) { text = suggestion }

    /// Words -> request -> slots. `nil` ask (empty text) does nothing.
    public func search() async {
        guard let a = FreeTimeAsk.parse(text, now: clock.now(), tz: tz, weekStart: weekStart) else { return }
        ask = a; ignoredWorkingHours = false
        await run(a, ignoreWorkingHours: false)
    }

    /// Outline button on the no-gap screen: same question, working hours off.
    public func ignoreWorkingHours() async {
        guard let a = ask else { return }
        ignoredWorkingHours = true
        await run(a, ignoreWorkingHours: true)
    }

    private func run(_ a: FreeTimeAsk, ignoreWorkingHours: Bool) async {
        phase = .searching; selected = nil
        do {
            let r = try await api.freeTime(FreeTimeRequest(durationMin: a.durationMin, from: a.from, to: a.to,
                                                           ignoreWorkingHours: ignoreWorkingHours, dayPart: a.dayPart, limit: 5))
            if r.slots.isEmpty { phase = .none(r) }
            else { phase = .slots(r); selected = r.slots.first(where: \.best) ?? r.slots.first }
        } catch {
            switch error {
            case APIClientError.transport, APIClientError.tls: phase = .failed("Can\u{2019}t reach the server. Check Tailscale, then try again.")
            case APIClientError.server(_, let b?) where b.code == "validation": phase = .failed(b.message)
            default: phase = .failed("The server returned something unexpected.")
            }
        }
    }

    public func backToAsk() { phase = .ask; selected = nil; bookError = nil }

    public func select(_ slot: FreeSlot) { selected = slot }

    // MARK: Words

    /// "This week, inside 09:00 to 18:00. 3 slots found."
    public func subLine(workingHours: WorkingHours?) -> String {
        guard let a = ask else { return "" }
        let inside: String
        if ignoredWorkingHours { inside = "any time of day" }
        else if let w = workingHours { inside = "inside \(w.start) to \(w.end)" }
        else { inside = "inside your working hours" }
        var s = "\(a.windowWords), \(inside)."
        if case .slots(let r) = phase { s += " \(r.slots.count) slot\(r.slots.count == 1 ? "" : "s") found." }
        return s
    }

    // MARK: Booking

    public func startBooking(_ slot: FreeSlot) {
        selected = slot; bookError = nil; attemptKey = nil
        phase = .booking(slot)
    }

    public var bookDraft: EventDraft? {
        guard let s = selected, let when = EventWhen.timed(start: s.start, end: s.end, tz: tz) else { return nil }
        return EventDraft(calendarId: bookCalendarId, title: bookTitle.trimmingCharacters(in: .whitespacesAndNewlines), when: when)
    }

    public var canBook: Bool { if case .booking = phase, let d = bookDraft { d.validate().isEmpty } else { false } }

    public func book() async {
        guard case .booking(let slot) = phase, let d = bookDraft, d.validate().isEmpty else { return }
        let key = attemptKey ?? UUID().uuidString
        attemptKey = key
        bookError = nil
        switch await writer.create(d, key: key, mode: .confirmed) {
        case .saved(let detail): booked = detail; phase = .booked
        case .failed(let f): bookError = QuickAddViewModel.words(f); phase = .booking(slot)
        }
    }

    public func undoBooking() async -> Bool {
        guard let b = booked else { return false }
        if case .saved = await writer.delete(b.item, scope: .this, key: nil, mode: .confirmed) { return true }
        return false
    }

    /// "Booked Focus time, Tue 14:00".
    public var bookedToast: String {
        guard let s = selected, let w = EventWhen.timed(start: s.start, end: s.end, tz: tz) else { return "Booked" }
        return "Booked \(bookTitle.trimmingCharacters(in: .whitespaces)), \(DraftFormat.short(w, tz: tz))"
    }

    /// "Tue 13 \u{00B7} 14:00 to 16:00"-style text for slot rows.
    public func slotTitle(_ s: FreeSlot) -> String {
        let day = DraftFormat.formatter("EEEd", tz, .current), time = DraftFormat.formatter("jmm", tz, .current)
        return "\(day.string(from: s.start)), \(time.string(from: s.start)) to \(time.string(from: s.end))"
    }
}
