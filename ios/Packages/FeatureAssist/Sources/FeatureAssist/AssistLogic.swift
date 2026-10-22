import CalendarCore
import Foundation

/// Pure helpers behind the App Intents (Siri, Shortcuts, Action button). No UI, no network.
public enum NextMeeting {
    /// "Join next meeting": the event on now, or the next one starting within `lead`, that has a call link.
    public static func pick(_ items: [AgendaItem], now: Date, lead: TimeInterval = 30 * 60) -> AgendaItem? {
        items.filter { i in
            guard !i.allDay, i.join != nil, i.status != "cancelled", i.myResponse != "declined", let s = i.start else { return false }
            let e = i.end ?? s
            return e > now && s <= now.addingTimeInterval(lead)
        }
        .sorted { ($0.start ?? .distantFuture) < ($1.start ?? .distantFuture) }
        .first
    }

    /// One sentence for Siri to speak or show: "Next up is Design review at 14:00, in 25 minutes, at Room 3."
    public static func sentence(next: AgendaItem?, now: Date, tz: TimeZone, locale: Locale = .current) -> String {
        guard let i = next, let s = i.start else { return "Nothing else on your calendar today." }
        let time = DraftFormat.formatter("jmm", tz, locale)
        let place = (i.location ?? "").isEmpty ? "" : ", at \(i.location!)"
        if s <= now {
            let e = i.end.map { " until \(time.string(from: $0))" } ?? ""
            return "\(i.title) is on now\(e)\(place)."
        }
        return "Next up is \(i.title) at \(time.string(from: s)), \(untilWords(s.timeIntervalSince(now)))\(place)."
    }

    public static func untilWords(_ secs: TimeInterval) -> String {
        let m = max(1, Int((secs / 60).rounded()))
        if m < 60 { return m == 1 ? "in 1 minute" : "in \(m) minutes" }
        let h = m / 60, r = m % 60
        let hw = h == 1 ? "1 hour" : "\(h) hours"
        if h >= 24 { return "in \(h / 24) day\(h / 24 == 1 ? "" : "s")" }
        return r == 0 ? "in \(hw)" : "in \(hw) \(r) minutes"
    }
}

/// "Add event" by voice: what must still be asked, and how the answers become an event.
public enum IntentAdd {
    /// Questions that cannot be skipped (the hub marks the rest optional). Free-form ones cannot be answered by voice.
    public static func required(_ d: QuickAddDraft) -> [DraftAmbiguity] { d.ambiguities.filter { !$0.optional } }

    /// Option labels for Siri to read out, or nil when the question has no fixed choices.
    public static func choices(_ q: DraftAmbiguity) -> [String]? { q.options.isEmpty ? nil : q.options.map(\.label) }

    public static func apply(label: String, to q: DraftAmbiguity, answers: inout [String: String]) {
        if let o = q.options.first(where: { $0.label == label }) { answers[q.field] = o.value }
    }

    /// "Lunch with Sam, Fri 16 Oct \u{00B7} 13:00 to 14:00."
    public static func summary(_ e: EventDraft, tz: TimeZone) -> String {
        let when = DraftFormat.when(e.when, tz: tz).map { ", \($0)" } ?? ""
        return "\(e.title)\(when)"
    }
}
