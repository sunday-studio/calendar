import CalendarCore
import SurfacesCore
import UserNotifications

/// Rewrites the hub's generic alerts (moved, cancelled, changed, reauth, summary) using the hub and the saved
/// agenda. Titles never travel through Apple. Any failure or the ~25 s budget running out delivers generic text.
final class NotificationService: UNNotificationServiceExtension, @unchecked Sendable {
    private let lock = NSLock()
    private var handler: ((UNNotificationContent) -> Void)?
    private var original: UNNotificationContent?
    private var work: Task<Void, Never>?

    override func didReceive(_ request: UNNotificationRequest, withContentHandler contentHandler: @escaping (UNNotificationContent) -> Void) {
        lock.withLock { handler = contentHandler; original = request.content }
        guard let payload = PushPayload(userInfo: request.content.userInfo), payload.kind != .sync else {
            finish(nil); return
        }
        work = Task { [weak self] in
            let config = HubConfigStore().load()
            let api = config.map { APIClient(config: $0) }
            let db = try? AppDatabase.openShared()
            let enricher = NotificationEnricher(api: api, db: db)
            let text = await enricher.notification(for: payload)
            self?.finish(text)
        }
    }

    /// The system is about to kill us: ship the generic wording.
    override func serviceExtensionTimeWillExpire() {
        work?.cancel()
        let payload = lock.withLock { original }.flatMap { PushPayload(userInfo: $0.userInfo) }
        finish(payload.map(NotificationRewriter.generic))
    }

    private func finish(_ text: RewrittenNotification?) {
        let (h, orig) = lock.withLock { () -> (((UNNotificationContent) -> Void)?, UNNotificationContent?) in
            let r = (handler, original); handler = nil; return r
        }
        guard let h, let orig else { return }   // already delivered
        guard let content = orig.mutableCopy() as? UNMutableNotificationContent else { h(orig); return }
        // Alerts switched off in Settings arrive quietly (the extension cannot drop them).
        if let payload = PushPayload(userInfo: orig.userInfo), NotificationQuiet.isQuiet(payload.kind, prefs: AlertPrefs.loadShared()) {
            content.interruptionLevel = .passive
            content.sound = nil
        }
        guard let text else { h(content); return }
        content.title = text.title
        content.body = text.body
        if let s = text.subtitle { content.subtitle = s }
        if let c = text.category { content.categoryIdentifier = c }
        if let t = text.thread { content.threadIdentifier = t }
        h(content)
    }
}
