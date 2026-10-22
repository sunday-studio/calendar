import CalendarCore
import Foundation
import Observation

/// Remembers what the hub has been told, so a token is sent once per change.
public protocol PushRegistrationStore: Sendable {
    func load() -> PushRegistrationRecord?
    func save(_ r: PushRegistrationRecord?)
}

public struct PushRegistrationRecord: Codable, Equatable, Sendable {
    public var token: String
    public var environment: PushEnvironment
    public var hub: String   // hub base URL the token was registered with
    public init(token: String, environment: PushEnvironment, hub: String) {
        self.token = token; self.environment = environment; self.hub = hub
    }
}

public final class UserDefaultsPushStore: PushRegistrationStore, @unchecked Sendable {
    private let defaults: UserDefaults
    private let key = "push.registration"
    public init(defaults: UserDefaults = .standard) { self.defaults = defaults }
    public func load() -> PushRegistrationRecord? {
        defaults.data(forKey: key).flatMap { try? JSONDecoder().decode(PushRegistrationRecord.self, from: $0) }
    }
    public func save(_ r: PushRegistrationRecord?) {
        if let r, let d = try? JSONEncoder().encode(r) { defaults.set(d, forKey: key) } else { defaults.removeObject(forKey: key) }
    }
}

/// Registers the device's APNs token with the hub after pairing and on token change; unregisters on Forget.
@MainActor @Observable
public final class PushRegistrationModel {
    public enum State: Equatable, Sendable {
        case idle
        case waitingForToken
        case registering
        case registered
        case failed(String)
    }

    public private(set) var state: State = .idle
    private let api: any HubPushAPI
    private let store: any PushRegistrationStore
    private let environment: PushEnvironment
    private let hub: String
    private let bundleId: String?
    private var token: String?

    public init(api: any HubPushAPI, hub: URL, store: any PushRegistrationStore = UserDefaultsPushStore(),
                environment: PushEnvironment = .current, bundleId: String? = Bundle.main.bundleIdentifier) {
        self.api = api; self.hub = hub.absoluteString; self.store = store
        self.environment = environment; self.bundleId = bundleId
    }

    /// Hex form of an APNs device token.
    public nonisolated static func hex(_ data: Data) -> String { data.map { String(format: "%02x", $0) }.joined() }

    /// The system handed us a token (first launch, or it changed).
    public func tokenReceived(_ data: Data) async { await tokenReceived(hex: Self.hex(data)) }

    public func tokenReceived(hex: String) async {
        token = hex
        await registerIfNeeded()
    }

    /// iOS refused (no entitlement, simulator without push, offline). Not fatal: reminders are local.
    public func registrationFailed(_ message: String) { state = .failed(message) }

    /// Call after pairing and on each foreground: retries a registration that failed earlier.
    public func registerIfNeeded() async {
        guard let token else { if state == .idle { state = .waitingForToken }; return }
        let wanted = PushRegistrationRecord(token: token, environment: environment, hub: hub)
        if store.load() == wanted { state = .registered; return }
        state = .registering
        do {
            try await api.registerPush(apnsToken: token, environment: environment, bundleId: bundleId)
            store.save(wanted)
            state = .registered
        } catch {
            state = .failed(Self.message(error))
        }
    }

    /// Forget this server: stop pushes on the hub (best effort) and clear what we remembered.
    public func forget() async {
        if store.load() != nil || token != nil { try? await api.unregisterPush() }
        store.save(nil)
        token = nil
        state = .idle
    }

    static func message(_ e: Error) -> String {
        switch e as? APIClientError {
        case .unauthorized: "The hub rejected this device's sign-in."
        case .transport, .tls: "Can't reach the hub. Push will retry."
        default: "Push registration failed."
        }
    }
}
