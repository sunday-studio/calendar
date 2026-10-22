import CalendarCore
import Foundation
import SurfacesCore
import UIKit
import UserNotifications

/// Asks for notification permission, registers for remote notifications and keeps the hub's copy of the APNs token current.
@MainActor
final class PushCoordinator {
    static let shared = PushCoordinator()
    private(set) var model: PushRegistrationModel?
    private var pendingToken: Data?

    /// After pairing / launch with a paired hub.
    func start(config: HubConfig) async {
        if model == nil {
            model = PushRegistrationModel(api: APIClient(config: config), hub: config.baseURL)
        }
        // No system prompt here: Settings > Alerts asks, with an explanation (design audit 7). Already granted: register now.
        if AlertPermission.isGranted(await AlertPermission.status()) { UIApplication.shared.registerForRemoteNotifications() }
        if observer == nil {
            observer = NotificationCenter.default.addObserver(forName: AlertPermission.changed, object: nil, queue: .main) { [weak self] _ in
                Task { @MainActor in await self?.permissionChanged() }
            }
        }
        if let t = pendingToken { pendingToken = nil; await model?.tokenReceived(t) }
        await model?.registerIfNeeded()
        await AppServices.shared.current()?.scheduler.reschedule()
    }

    private var observer: NSObjectProtocol?

    private func permissionChanged() async {
        guard AlertPermission.isGranted(await AlertPermission.status()) else { return }
        UIApplication.shared.registerForRemoteNotifications()
        await AppServices.shared.current()?.scheduler.reschedule()
    }

    func tokenReceived(_ data: Data) async {
        if let model { await model.tokenReceived(data) } else { pendingToken = data }
    }

    func registrationFailed(_ error: Error) { model?.registrationFailed(error.localizedDescription) }

    /// Forget this server: unregister on the hub while the token still works, then drop local surfaces.
    func forget(config: HubConfig?) async {
        let m = model ?? config.map { PushRegistrationModel(api: APIClient(config: $0), hub: $0.baseURL) }
        await m?.forget()
        model = nil; pendingToken = nil
        UIApplication.shared.unregisterForRemoteNotifications()
        await AppServices.shared.teardown()
    }
}
