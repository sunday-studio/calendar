import CalendarCore
import SurfacesCore
import SurfacesUI
import SwiftUI
import UIKit
import UserNotifications
import UserNotificationsUI

/// Long look of Calendar notifications (spec section 6): the system draws the frame, header and buttons; this is the
/// middle: the event, a 3-hour mini timeline and a compact Join. Data comes from the saved agenda in the App Group.
final class NotificationViewController: UIViewController, UNNotificationContentExtension {
    private var host: UIHostingController<AnyView>?

    func didReceive(_ notification: UNNotification) {
        let content = notification.request.content
        let info = content.userInfo
        let id = (info["link"] as? String).flatMap(URL.init(string:)).flatMap(SurfaceLink.parse).flatMap { link -> String? in
            if case .event(let id) = link { id } else { nil }
        } ?? (info["eventId"] as? String)
        Task { @MainActor in
            let data = await LongLookLoader.load(eventId: id, now: Date())
            let view = NotificationContentView(model: data ?? .fallback(title: content.title, subtitle: content.subtitle, body: content.body),
                                               showsJoin: content.categoryIdentifier == NotificationCategory.reminderJoin)
            show(AnyView(view))
        }
    }

    private func show(_ view: AnyView) {
        host?.view.removeFromSuperview(); host?.removeFromParent()
        let h = UIHostingController(rootView: view)
        h.view.backgroundColor = .clear
        addChild(h); h.view.frame = self.view.bounds
        h.view.autoresizingMask = [.flexibleWidth, .flexibleHeight]
        self.view.addSubview(h.view); h.didMove(toParent: self)
        host = h
        preferredContentSize = h.sizeThatFits(in: CGSize(width: self.view.bounds.width, height: .greatestFiniteMagnitude))
    }
}

enum LongLookLoader {
    static func load(eventId: String?, now: Date) async -> NotificationContentModel? {
        guard let db = try? AppDatabase.openShared(), let tz = try? db.userTimeZone() else { return nil }
        let today = DayKey(now, in: tz)
        let items = (try? await GRDBAgendaRepository(db: db).items(from: today.adding(days: -1), to: today.adding(days: 1))) ?? []
        return NotificationContentModel.make(items: items, eventId: eventId, now: now, tz: tz)
    }
}
