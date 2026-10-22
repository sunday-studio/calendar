import Foundation
import Network

/// Is there a network path on this phone at all? (Distinguishes "Offline" from "your server is unreachable".)
public final class NetworkMonitor: @unchecked Sendable {
    public static let shared = NetworkMonitor()
    private let monitor = NWPathMonitor()
    private let lock = NSLock()
    private var _online = true
    private var continuations: [UUID: AsyncStream<Bool>.Continuation] = [:]

    public init() {
        monitor.pathUpdateHandler = { [weak self] path in
            guard let self else { return }
            let online = path.status == .satisfied
            let conts = lock.withLock { () -> [AsyncStream<Bool>.Continuation] in
                _online = online
                return Array(continuations.values)
            }
            for c in conts { c.yield(online) }
        }
        monitor.start(queue: DispatchQueue(label: "calendar.network"))
    }

    public var isOnline: Bool { lock.withLock { _online } }

    /// Current value first, then every change.
    public var updates: AsyncStream<Bool> {
        AsyncStream { c in
            let id = UUID()
            lock.withLock { continuations[id] = c }
            c.yield(isOnline)
            c.onTermination = { [weak self] _ in _ = self?.lock.withLock { self?.continuations.removeValue(forKey: id) } }
        }
    }
}
