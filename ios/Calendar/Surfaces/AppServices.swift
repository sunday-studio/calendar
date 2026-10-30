import BackgroundTasks
import CalendarCore
import FeatureHome
import FeatureSettings
import Foundation
import SurfacesCore
import UIKit
import UserNotifications
import WidgetKit

/// Reloads every widget timeline after a sync.
struct WidgetReloadHook: AfterSyncHook {
    func afterSync(_ outcome: SyncOutcome) async { WidgetCenter.shared.reloadAllTimelines() }
}

/// One environment per paired hub, shared by the UI and by background work (silent push, BGAppRefresh).
final class AppServices: @unchecked Sendable {
    static let shared = AppServices()
    static let refreshTaskID = "studio.thesunday.calendar.refresh"

    private let lock = NSLock()
    private var cached: (config: HubConfig, env: AppEnvironment, scheduler: ReminderScheduler, live: LiveActivityController)?

    struct Bundle { let env: AppEnvironment; let scheduler: ReminderScheduler; let live: LiveActivityController }

    func services(for config: HubConfig) -> Bundle {
        lock.lock(); defer { lock.unlock() }
        if let c = cached, c.config == config { return Bundle(env: c.env, scheduler: c.scheduler, live: c.live) }
        // Shared App Group database; an in-memory one keeps the app usable if it cannot be opened.
        let db = (try? AppDatabase.openShared()) ?? (try! AppDatabase.inMemory())
        let api = APIClient(config: config)
        let scheduler = ReminderScheduler(db: db, api: api, showJoin: { Self.showJoinButton })
        let live = LiveActivityController(db: db)
        let env = AppEnvironment.live(config: config, db: db, hooks: [WidgetReloadHook(), scheduler, live])
        cached = (config, env, scheduler, live)
        return Bundle(env: env, scheduler: scheduler, live: live)
    }

    /// Environment for the stored hub, or nil when not paired.
    func current() -> Bundle? { HubConfigStore().load().map(services(for:)) }

    static var showJoinButton: Bool { UserDefaults.standard.object(forKey: Prefs.alertJoinButton) as? Bool ?? true }

    // MARK: Background work

    /// Silent push / background refresh: refresh the saved agenda (hooks reschedule reminders, widgets, activity).
    func backgroundSync() async -> Bool {
        guard let b = current() else { return false }
        return await b.env.sync.silentPush()
    }

    func scheduleAppRefresh() {
        let r = BGAppRefreshTaskRequest(identifier: Self.refreshTaskID)
        r.earliestBeginDate = Date(timeIntervalSinceNow: 60 * 60)
        try? BGTaskScheduler.shared.submit(r)
    }

    func registerAppRefresh() {
        BGTaskScheduler.shared.register(forTaskWithIdentifier: Self.refreshTaskID, using: nil) { task in
            let box = TaskBox(task: task)
            Task {
                AppServices.shared.scheduleAppRefresh()
                let work = Task { await AppServices.shared.backgroundSync() }
                box.task.expirationHandler = { work.cancel() }
                box.task.setTaskCompleted(success: await work.value)
            }
        }
    }

    private struct TaskBox: @unchecked Sendable { let task: BGTask }

    // MARK: Forget this server

    /// Pending reminders, the Live Activity and widgets must not outlive the pairing.
    func teardown() async {
        let center = UNUserNotificationCenter.current()
        let ids = await center.pendingNotificationRequests().map(\.identifier).filter { $0.hasPrefix(PlannedReminder.prefix) || $0.hasPrefix("snz.") }
        center.removePendingNotificationRequests(withIdentifiers: ids)
        center.removeAllDeliveredNotifications()
        lock.withLock { cached = nil }
        WidgetCenter.shared.reloadAllTimelines()
    }

    // MARK: Notification actions

    func snooze(_ content: UNNotificationContent) async {
        let c = UNMutableNotificationContent()
        c.title = content.title; c.body = content.body; c.sound = .default
        c.categoryIdentifier = content.categoryIdentifier; c.threadIdentifier = content.threadIdentifier
        c.userInfo = content.userInfo; c.interruptionLevel = content.interruptionLevel
        let trigger = UNTimeIntervalNotificationTrigger(timeInterval: NotificationCategory.snoozeSeconds, repeats: false)
        try? await UNUserNotificationCenter.current().add(UNNotificationRequest(identifier: "snz." + UUID().uuidString, content: c, trigger: trigger))
    }

    /// Accept / Maybe / Decline pressed on an invitation banner.
    func reply(eventId: String, response: RSVPResponse) async {
        guard let cfg = HubConfigStore().load() else { return }
        _ = try? await APIClient(config: cfg).rsvp(id: eventId, response: response, note: nil, scope: .this, key: UUID().uuidString)
    }

    func muteSummary() async {
        guard let cfg = HubConfigStore().load() else { return }
        let api = APIClient(config: cfg)
        guard var s = try? await api.settings() else { return }
        s.morningSummary = MorningSummary(enabled: false, time: s.morningSummary?.time ?? "07:30")
        _ = try? await api.putSettings(s)
    }
}

/// Routes links to Home from anywhere (widgets, notifications, Live Activity).
@MainActor
final class AppRouter {
    static let shared = AppRouter()
    let navigator = HomeNavigator()

    /// Returns true when `url` was one of ours.
    @discardableResult
    func open(_ url: URL) -> Bool {
        guard let link = SurfaceLink.parse(url) else { return false }
        open(link)
        return true
    }

    func open(_ link: SurfaceLink) {
        switch link {
        case .event(let id): navigator.pending = .event(id)
        case .accounts: navigator.pending = .accounts
        case .today, .summary, .day: navigator.pending = .today
        case .new: navigator.pending = .newEvent
        }
    }
}
