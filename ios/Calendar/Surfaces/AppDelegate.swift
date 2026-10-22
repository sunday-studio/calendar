import SurfacesCore
import UIKit
@preconcurrency import UserNotifications

final class AppDelegate: NSObject, UIApplicationDelegate, UNUserNotificationCenterDelegate {
    func application(_ application: UIApplication, didFinishLaunchingWithOptions launchOptions: [UIApplication.LaunchOptionsKey: Any]? = nil) -> Bool {
        let center = UNUserNotificationCenter.current()
        center.delegate = self
        center.setNotificationCategories(NotificationCategories.all())
        AppServices.shared.registerAppRefresh()
        return true
    }

    func application(_ application: UIApplication, didRegisterForRemoteNotificationsWithDeviceToken deviceToken: Data) {
        Task { @MainActor in await PushCoordinator.shared.tokenReceived(deviceToken) }
    }

    func application(_ application: UIApplication, didFailToRegisterForRemoteNotificationsWithError error: Error) {
        Task { @MainActor in PushCoordinator.shared.registrationFailed(error) }
    }

    /// Silent `{k:"sync"}` push: refresh the saved agenda; the after-sync hooks reschedule reminders and reload widgets.
    func application(_ application: UIApplication, didReceiveRemoteNotification userInfo: [AnyHashable: Any]) async -> UIBackgroundFetchResult {
        return await AppServices.shared.backgroundSync() ? .newData : .noData
    }

    // Foreground presentation: show reminders as banners too.
    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, willPresent notification: UNNotification,
                                            withCompletionHandler completionHandler: @escaping (UNNotificationPresentationOptions) -> Void) {
        completionHandler([.banner, .list, .sound])
    }

    nonisolated func userNotificationCenter(_ center: UNUserNotificationCenter, didReceive response: UNNotificationResponse,
                                            withCompletionHandler completionHandler: @escaping () -> Void) {
        let content = response.notification.request.content
        let outcome = NotificationRouter.route(action: response.actionIdentifier, userInfo: content.userInfo)
        let done = UncheckedBox(completionHandler)
        Task {
            switch outcome {
            case .open(let link): await MainActor.run { AppRouter.shared.open(link) }
            case .openURL(let url): await MainActor.run { _ = UIApplication.shared.open(url) }
            case .snooze: await AppServices.shared.snooze(content)
            case .muteSummary: await AppServices.shared.muteSummary()
            case .none: break
            }
            done.value()
        }
    }
}

private struct UncheckedBox<T>: @unchecked Sendable { let value: T; init(_ v: T) { value = v } }
