import Foundation
import UserNotifications

/// The notification permission is asked for in context (Settings > Alerts, with a sentence of explanation), never
/// on first launch over Home (design audit 7, item 1).
public enum AlertPermission {
    /// Posted (main thread) after the user answered the system prompt.
    public static let changed = Notification.Name("studio.thesunday.calendar.alertPermissionChanged")

    public static func status() async -> UNAuthorizationStatus {
        await UNUserNotificationCenter.current().notificationSettings().authorizationStatus
    }

    public static func isGranted(_ s: UNAuthorizationStatus) -> Bool {
        switch s { case .authorized, .provisional, .ephemeral: true; default: false }
    }

    @discardableResult
    @MainActor public static func request() async -> Bool {
        let granted = (try? await UNUserNotificationCenter.current().requestAuthorization(options: [.alert, .sound, .badge])) ?? false
        NotificationCenter.default.post(name: changed, object: nil)
        return granted
    }
}
