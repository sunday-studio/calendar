import CalendarCore
import Foundation

/// Time and countdown words shared by widgets, reminders and the notification service.
/// Same wording as Home ("in 8 min", "Now · 22 min left"); duplicated here because extensions cannot link FeatureHome.
public final class SurfaceFormat: @unchecked Sendable {
    public let tz: TimeZone
    public let locale: Locale
    private let timeF: DateFormatter
    private let weekdayF: DateFormatter
    private let dayMonthF: DateFormatter

    public init(tz: TimeZone, locale: Locale = .current) {
        self.tz = tz; self.locale = locale
        func make(_ template: String) -> DateFormatter {
            let f = DateFormatter()
            f.locale = locale; f.timeZone = tz
            f.setLocalizedDateFormatFromTemplate(template)
            return f
        }
        timeF = make("jmm"); weekdayF = make("EEEE"); dayMonthF = make("EEE d MMM")
        dayMonthF.dateFormat = "EEE d MMM"
    }

    /// "10:30" / "10:30 AM".
    public func time(_ d: Date) -> String { timeF.string(from: d) }

    /// Compact start time for split-flap cells: the day period is dropped ("10:30") so the cells stay narrow.
    public func flipTime(_ d: Date) -> String {
        let s = time(d)
        let kept = s.filter { $0.isNumber || $0 == ":" || $0 == "." }
        return kept.isEmpty ? s : kept
    }

    public func weekday(_ d: Date) -> String { weekdayF.string(from: d) }
    public func shortDate(_ d: Date) -> String { dayMonthF.string(from: d) }

    /// "today", "tomorrow", "Sunday", or "Mon 12 Oct" relative to `now`.
    public func dayWord(_ d: Date, now: Date) -> String {
        switch DayKey(now, in: tz).days(to: DayKey(d, in: tz)) {
        case 0: return "today"
        case 1: return "tomorrow"
        case 2...6: return weekday(d)
        default: return shortDate(d)
        }
    }

    // MARK: Countdown words

    public static func minutesText(_ minutes: Int) -> String {
        let m = max(minutes, 0)
        if m < 60 { return "\(m) min" }
        if m >= 48 * 60 { return "\(m / 1440) days" }
        if m >= 24 * 60 { return m % 1440 == 0 ? "1 day" : "1 day \((m % 1440) / 60) h" }
        return m % 60 == 0 ? "\(m / 60) h" : "\(m / 60) h \(m % 60) min"
    }

    /// "in 8 min", or "now" at/after the start.
    public static func until(start: Date, now: Date) -> String {
        let mins = Int((start.timeIntervalSince(now) / 60).rounded(.up))
        return mins <= 0 ? "now" : "in \(minutesText(mins))"
    }

    /// "22 min left".
    public static func left(end: Date, now: Date) -> String {
        "\(minutesText(Int((end.timeIntervalSince(now) / 60).rounded(.up)))) left"
    }

    public static func joinLabel(_ kind: String?) -> String {
        switch kind {
        case "meet": "Google Meet"
        case "zoom": "Zoom"
        case "teams": "Teams"
        default: "Call"
        }
    }
}
