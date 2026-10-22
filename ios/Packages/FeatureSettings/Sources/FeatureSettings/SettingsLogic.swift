import CalendarCore
import Foundation

/// Pure helpers for the Settings, Calendars and Status screens (no SwiftUI).
public enum SettingsLogic {
    /// "just now", "12 min ago", "3 h ago", "yesterday", "5 days ago".
    public static func ago(_ date: Date, now: Date) -> String {
        let s = max(0, now.timeIntervalSince(date))
        switch s {
        case ..<60: return "just now"
        case ..<3600: return "\(Int(s / 60)) min ago"
        case ..<86_400: return "\(Int(s / 3600)) h ago"
        case ..<(2 * 86_400): return "yesterday"
        default: return "\(Int(s / 86_400)) days ago"
        }
    }

    /// Account status words, same vocabulary as the Accounts screens.
    public static func statusWord(_ wire: String) -> String {
        switch wire {
        case "active": "Up to date"
        case "syncing": "Syncing"
        case "paused": "Paused"
        case "needs_reauth": "Sign in needed"
        default: "Problem"
        }
    }

    public static func isAttention(_ wire: String) -> Bool { wire == "needs_reauth" || wire == "error" }

    public static func byteSize(_ bytes: Int64) -> String {
        let f = ByteCountFormatter(); f.countStyle = .file
        return f.string(fromByteCount: bytes)
    }

    /// "HH:MM" -> minutes since midnight; nil when malformed.
    public static func minutes(_ hhmm: String) -> Int? {
        let p = hhmm.split(separator: ":", omittingEmptySubsequences: false)
        guard p.count == 2, p[0].count == 2, p[1].count == 2, let h = Int(p[0]), let m = Int(p[1]),
              (0..<24).contains(h), (0..<60).contains(m) else { return nil }
        return h * 60 + m
    }

    public static func hhmm(_ minutes: Int) -> String {
        let m = min(max(minutes, 0), 23 * 60 + 59)
        return String(format: "%02d:%02d", m / 60, m % 60)
    }

    /// "New York" from "America/New_York".
    public static func city(_ tz: TimeZone) -> String {
        (tz.identifier.split(separator: "/").last.map(String.init) ?? tz.identifier).replacingOccurrences(of: "_", with: " ")
    }

    public static func isWritable(_ c: HubCalendar) -> Bool { c.access == "owner" || c.access == "writer" }
}

/// What the connection banner (Home, Settings) says. nil = say nothing.
public struct ConnectionBanner: Equatable, Sendable {
    public enum Kind: Equatable, Sendable { case offline, stale, error }
    public var kind: Kind
    public var title: String
    public var detail: String

    public static func make(state: SyncState, lastSyncAt: Date?, now: Date) -> ConnectionBanner? {
        switch state {
        case .idle, .syncing:
            return nil
        case .offline:
            return .init(kind: .offline, title: "Can't reach your hub", detail: "Nothing is saved on this phone yet.")
        case .stale(let since):
            return .init(kind: .stale, title: "Offline, showing saved events",
                         detail: "Last synced \(SettingsLogic.ago(lastSyncAt ?? since, now: now)).")
        case .error(let msg):
            return .init(kind: .error, title: "Your hub had a problem", detail: msg)
        }
    }
}
