import CalendarCore
import Foundation

/// Pure text for the event detail screen (no SwiftUI). All inputs are explicit so tests pin time zone, locale and clock.
public final class EventTimeFormat: @unchecked Sendable {
    public let tz: TimeZone
    public let locale: Locale
    private let timeF: DateFormatter

    public init(tz: TimeZone, locale: Locale = .current) {
        self.tz = tz; self.locale = locale
        func make(_ template: String, _ zone: TimeZone) -> DateFormatter {
            let f = DateFormatter()
            f.locale = locale; f.timeZone = zone
            f.setLocalizedDateFormatFromTemplate(template)
            return f
        }
        timeF = make("jmm", tz)
    }

    func time(_ d: Date, in zone: TimeZone? = nil) -> String {
        guard let zone, zone.identifier != tz.identifier else { return timeF.string(from: d) }
        let f = DateFormatter(); f.locale = locale; f.timeZone = zone; f.setLocalizedDateFormatFromTemplate("jmm")
        return f.string(from: d)
    }

    // MARK: Durations

    /// "30 min", "1 h 30 min", "2 h", "3 days".
    public static func durationText(_ seconds: TimeInterval) -> String {
        let mins = Int((seconds / 60).rounded())
        if mins < 60 { return "\(max(mins, 1)) min" }
        if mins >= 24 * 60 * 2 && mins % (24 * 60) == 0 { return "\(mins / (24 * 60)) days" }
        let h = mins / 60, m = mins % 60
        return m == 0 ? "\(h) h" : "\(h) h \(m) min"
    }

    // MARK: Relative

    /// "in 8 min", "in 1 h 8 min", "20 min left"; nil for all-day, far-away or finished events.
    public static func relative(_ item: AgendaItem, now: Date) -> String? {
        guard !item.allDay, let s = item.start else { return nil }
        let e = item.end ?? s
        if s > now {
            let secs = s.timeIntervalSince(now)
            if secs >= 24 * 3600 { return nil }
            return "in " + durationText(max(secs, 60))
        }
        if e > now { return durationText(e.timeIntervalSince(now)) + " left" }
        return nil
    }
}

// MARK: - Join

public enum JoinLabel {
    /// "Join Meet", "Join Teams", "Join Zoom", "Join Webex", else "Join call".
    public static func label(for join: Join) -> String {
        switch join.kind {
        case "meet": return "Join Meet"
        case "zoom": return "Join Zoom"
        case "teams": return "Join Teams"
        default:
            let host = URL(string: join.url)?.host?.lowercased() ?? ""
            if host.contains("webex.com") { return "Join Webex" }
            if host.contains("zoom.us") { return "Join Zoom" }
            if host.contains("teams.microsoft.com") || host.contains("teams.live.com") { return "Join Teams" }
            if host.contains("meet.google.com") { return "Join Meet" }
            return "Join call"
        }
    }

    /// Provider name without the verb, for the row subtitle.
    public static func provider(for join: Join) -> String {
        label(for: join).replacingOccurrences(of: "Join ", with: "").replacingOccurrences(of: "call", with: "Video call")
    }
}

// MARK: - Responses

public enum ResponseStyle: Equatable, Sendable {
    case going, declined, maybe, noReply

    public init(_ wire: String?) {
        switch wire {
        case "accepted": self = .going
        case "declined": self = .declined
        case "tentative": self = .maybe
        default: self = .noReply
        }
    }
}

public struct GuestRow: Equatable, Sendable, Identifiable {
    public var id: String
    public var name: String
    public var email: String
    public var response: ResponseStyle
    public var isOrganizer: Bool
    public var isSelf: Bool
    public var isOptional: Bool
}

public enum PeopleLogic {
    /// Organiser first, then you, then the rest by name. The organiser is added even if absent from `attendees`.
    public static func rows(_ d: EventDetail) -> [GuestRow] {
        var rows = d.attendees.map { a -> GuestRow in
            let org = a.isOrganizer == true || a.email.lowercased() == d.organizer?.email.lowercased()
            return GuestRow(id: a.email.lowercased(), name: displayName(a.name, a.email), email: a.email,
                             response: ResponseStyle(a.response), isOrganizer: org, isSelf: a.isSelf == true,
                             isOptional: a.optional == true)
        }
        if let o = d.organizer, !rows.contains(where: { $0.id == o.email.lowercased() }), !d.attendees.isEmpty {
            rows.append(GuestRow(id: o.email.lowercased(), name: displayName(o.name, o.email), email: o.email,
                                  response: .going, isOrganizer: true, isSelf: false, isOptional: false))
        }
        return rows.sorted { a, b in
            if a.isOrganizer != b.isOrganizer { return a.isOrganizer }
            if a.isSelf != b.isSelf { return a.isSelf }
            return (a.name.lowercased(), a.id) < (b.name.lowercased(), b.id)
        }
    }

    static func displayName(_ name: String?, _ email: String) -> String {
        if let n = name?.trimmingCharacters(in: .whitespaces), !n.isEmpty { return n }
        return email
    }
}

// MARK: - Recurrence

public enum RecurrenceText {
    /// Server summary when present, else a best-effort reading of the RRULE.
    public static func summary(_ r: Recurrence?) -> String? {
        guard let r else { return nil }
        if let s = r.summary?.trimmingCharacters(in: .whitespaces), !s.isEmpty { return s }
        var parts: [String: String] = [:]
        let body = r.rrule.hasPrefix("RRULE:") ? String(r.rrule.dropFirst(6)) : r.rrule
        for kv in body.split(separator: ";") {
            let p = kv.split(separator: "=", maxSplits: 1).map(String.init)
            if p.count == 2 { parts[p[0].uppercased()] = p[1] }
        }
        let n = Int(parts["INTERVAL"] ?? "") ?? 1
        let names = ["MO": "Mon", "TU": "Tue", "WE": "Wed", "TH": "Thu", "FR": "Fri", "SA": "Sat", "SU": "Sun"]
        var out: String
        switch parts["FREQ"] {
        case "DAILY": out = n == 1 ? "Every day" : "Every \(n) days"
        case "WEEKLY":
            let days = (parts["BYDAY"] ?? "").split(separator: ",").map(String.init)
            if days.sorted() == ["FR", "MO", "TH", "TU", "WE"] && n == 1 { out = "Every weekday" }
            else {
                out = n == 1 ? "Every week" : "Every \(n) weeks"
                if !days.isEmpty { out += " on " + days.compactMap { names[$0] }.joined(separator: ", ") }
            }
        case "MONTHLY": out = n == 1 ? "Every month" : "Every \(n) months"
        case "YEARLY": out = n == 1 ? "Every year" : "Every \(n) years"
        default: return "Repeats"
        }
        if let c = parts["COUNT"], let k = Int(c) { out += ", \(k) times" }
        else if let u = parts["UNTIL"], u.count >= 8 {
            let y = u.prefix(4), m = u.dropFirst(4).prefix(2), d = u.dropFirst(6).prefix(2)
            out += ", until \(y)-\(m)-\(d)"
        }
        return out
    }
}

// MARK: - Description text

public enum DescriptionText {
    /// Plain text from a description that may contain light HTML (Google/Outlook).
    public static func plain(_ raw: String) -> String {
        var s = raw
        if s.contains("<") {
            for br in ["<br>", "<br/>", "<br />", "</p>", "</div>", "</li>"] { s = s.replacingOccurrences(of: br, with: "\n", options: .caseInsensitive) }
            s = s.replacingOccurrences(of: "<[^>]+>", with: "", options: .regularExpression)
        }
        for (a, b) in [("&amp;", "&"), ("&lt;", "<"), ("&gt;", ">"), ("&quot;", "\""), ("&#39;", "'"), ("&nbsp;", " ")] {
            s = s.replacingOccurrences(of: a, with: b)
        }
        return s.replacingOccurrences(of: "\n{3,}", with: "\n\n", options: .regularExpression)
            .trimmingCharacters(in: .whitespacesAndNewlines)
    }

    /// First join link in free text (for events without a structured join).
    public static func firstMeetingLink(_ raw: String?) -> Join? {
        guard let raw else { return nil }
        let text = plain(raw)
        guard let det = try? NSDataDetector(types: NSTextCheckingResult.CheckingType.link.rawValue) else { return nil }
        for m in det.matches(in: text, range: NSRange(location: 0, length: (text as NSString).length)) {
            guard let u = m.url, let host = u.host?.lowercased() else { continue }
            if host.contains("meet.google.com") { return Join(url: u.absoluteString, kind: "meet") }
            if host.contains("zoom.us") { return Join(url: u.absoluteString, kind: "zoom") }
            if host.contains("teams.microsoft.com") { return Join(url: u.absoluteString, kind: "teams") }
            if host.contains("webex.com") { return Join(url: u.absoluteString, kind: "other") }
        }
        return nil
    }
}

public enum MapsLink {
    public static func url(for place: String) -> URL? {
        var c = URLComponents(string: "https://maps.apple.com/")
        c?.queryItems = [URLQueryItem(name: "q", value: place)]
        return c?.url
    }
}

extension EventTimeFormat {
    /// Fixed-order short day, "Sun 11 Oct".
    func shortDate(_ d: DayKey) -> String {
        let f = DateFormatter(); f.locale = locale; f.timeZone = tz; f.dateFormat = "EEE d MMM"
        return f.string(from: d.startOfDay(in: tz).addingTimeInterval(12 * 3600))
    }

    /// Canvas "when" line.
    public func whenLine(for item: AgendaItem) -> String {
        if item.allDay {
            let first = DayKey(item.date ?? item.endDate ?? "0000-00-00")
            var last = first
            if let end = item.endDate { let l = DayKey(end).adding(days: -1); if l > first { last = l } }
            return last > first ? "\(shortDate(first)) to \(shortDate(last))" : "\(shortDate(first)) \u{00B7} All day"
        }
        guard let s = item.start else { return "" }
        let e = item.end ?? s
        let d1 = DayKey(s, in: tz), d2 = DayKey(e > s ? e.addingTimeInterval(-0.001) : s, in: tz)
        if d2 > d1 { return "\(shortDate(d1)) \(time(s)) to \(shortDate(d2)) \(time(e))" }
        return e > s ? "\(shortDate(d1)) \u{00B7} \(time(s)) to \(time(e))" : "\(shortDate(d1)) \u{00B7} \(time(s))"
    }
}

public enum ReminderText {
    /// "10 minutes before", "At the start", "1 hour before", "1 day before".
    public static func text(_ m: Int) -> String {
        if m == 0 { return "At the start" }
        if m % 1440 == 0 { return m == 1440 ? "1 day before" : "\(m / 1440) days before" }
        if m % 60 == 0 { return m == 60 ? "1 hour before" : "\(m / 60) hours before" }
        return "\(m) minutes before"
    }
    public static let choices: [Int?] = [nil, 0, 5, 10, 30, 60, 1440]
}

extension EventTimeFormat {
    /// "Sun 11" for the day strip.
    func dayShort(_ d: DayKey) -> String {
        let f = DateFormatter(); f.locale = locale; f.timeZone = tz; f.dateFormat = "EEE d"
        return f.string(from: d.startOfDay(in: tz).addingTimeInterval(12 * 3600))
    }
}
