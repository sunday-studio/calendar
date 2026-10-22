import CalendarCore
import CalendarUI
import SwiftUI

/// Plain words for the wire status enum.
enum StatusWords {
    static func word(_ a: Account) -> String {
        switch a.statusKind {
        case .active: "Up to date"
        case .syncing: "Syncing"
        case .paused: "Paused"
        case .needsReauth: "Sign in needed"
        case .error: "Problem"
        }
    }

    static func lastSync(_ a: Account, now: Date = Date()) -> String {
        guard let d = a.lastOkAt ?? a.lastSyncAt else { return "Not synced yet" }
        let f = RelativeDateTimeFormatter()
        f.unitsStyle = .full
        return "Synced " + f.localizedString(for: d, relativeTo: now)
    }
}

extension StatusWords {
    /// Account list status on the right of a row: "1 min ago" (ok), "Syncing", "Sign in" (bad).
    static func trailing(_ a: Account, now: Date = Date()) -> (text: String, kind: StatusLabel.Kind) {
        switch a.statusKind {
        case .needsReauth: return ("Sign in", .bad)
        case .error: return ("Problem", .bad)
        case .paused: return ("Paused", .none)
        case .syncing: return ("Syncing", .working)
        case .active:
            guard let d = a.lastOkAt ?? a.lastSyncAt else { return ("Not synced yet", .none) }
            let s = max(0, now.timeIntervalSince(d))
            if s < 60 { return ("Just now", .ok) }
            if s < 3600 { return ("\(Int(s / 60)) min ago", .ok) }
            if s < 86_400 { return ("\(Int(s / 3600)) h ago", .ok) }
            return ("\(Int(s / 86_400)) d ago", .ok)
        }
    }
}

extension CheckStep {
    /// Canvas wording for the checklist: Done / Working / Waiting (or the failure).
    var statusText: String {
        switch state {
        case .ok: "Done"
        case .running: "Working"
        case .pending: "Waiting"
        case .failed: "Failed"
        }
    }
    var statusKind: StatusLabel.Kind {
        switch state {
        case .ok: .ok
        case .running: .working
        case .pending: .none
        case .failed: .bad
        }
    }
    var detailText: String? {
        switch state {
        case .ok(let d), .failed(let d): d
        default: nil
        }
    }
}
