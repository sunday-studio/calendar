import Foundation
import UserNotifications

/// Categories with their actions (spec: reminder Join + Snooze, moved View + OK, reauth Sign in + Later, summary Open today + Mute).
public enum NotificationCategories {
    public static func all() -> Set<UNNotificationCategory> {
        typealias C = NotificationCategory
        func action(_ id: String, _ title: String, foreground: Bool) -> UNNotificationAction {
            UNNotificationAction(identifier: id, title: title, options: foreground ? [.foreground] : [])
        }
        return [
            UNNotificationCategory(identifier: C.reminderJoin, actions: [action(C.actionJoin, "Join", foreground: true),
                                                                          action(C.actionSnooze, "Snooze 5 min", foreground: false)],
                                   intentIdentifiers: [], options: []),
            UNNotificationCategory(identifier: C.moved, actions: [action(C.actionView, "View", foreground: true),
                                                                  action(C.actionOK, "OK", foreground: false)],
                                   intentIdentifiers: [], options: []),
            UNNotificationCategory(identifier: C.reauth, actions: [action(C.actionSignIn, "Sign in", foreground: true),
                                                                   action(C.actionLater, "Later", foreground: false)],
                                   intentIdentifiers: [], options: []),
            UNNotificationCategory(identifier: C.summary, actions: [action(C.actionOpenToday, "Open today", foreground: true),
                                                                    action(C.actionMute, "Mute", foreground: false)],
                                   intentIdentifiers: [], options: []),
        ]
    }
}

/// What to do with a tapped notification or action button (pure; the app delegate performs it).
public enum NotificationOutcome: Equatable, Sendable {
    case open(SurfaceLink)
    case openURL(URL)
    case snooze
    case muteSummary
    case none
}

public enum NotificationRouter {
    public static func route(action: String, userInfo: [AnyHashable: Any]) -> NotificationOutcome {
        typealias C = NotificationCategory
        func str(_ k: String) -> String? { (userInfo[k] as? String).flatMap { $0.isEmpty ? nil : $0 } }
        switch action {
        case C.actionJoin:
            if let u = str("join").flatMap(URL.init(string:)) { return .openURL(u) }
            return link(userInfo)
        case C.actionSnooze: return .snooze
        case C.actionOK, C.actionLater, UNNotificationDismissActionIdentifier: return .none
        case C.actionMute: return .muteSummary
        case C.actionSignIn: return .open(.accounts)
        case C.actionOpenToday: return .open(.today)
        default: return link(userInfo)   // default tap and "View"
        }
    }

    /// Where a plain tap goes: the stored link (local reminders) or the push kind (remote).
    static func link(_ userInfo: [AnyHashable: Any]) -> NotificationOutcome {
        if let s = userInfo["link"] as? String, let u = URL(string: s), let l = SurfaceLink.parse(u) { return .open(l) }
        switch userInfo["k"] as? String {
        case "cancelled": return .open(.today)   // the event is gone
        case "moved", "reminder":
            if let id = userInfo["eventId"] as? String, !id.isEmpty { return .open(.event(id)) }
            return .open(.today)
        case "reauth": return .open(.accounts)
        case "summary": return .open(.summary(date: userInfo["date"] as? String))
        default: return .open(.today)
        }
    }
}
