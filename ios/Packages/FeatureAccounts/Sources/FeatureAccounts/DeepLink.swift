import Foundation

/// URLs the app handles: `calendar://pair?host=&token=` and the hub's `calendar://connected?...` return.
public enum DeepLink: Equatable, Sendable {
    case pair(host: String?, token: String?)
    case connected(accountId: String, status: String?)
    case connectError(code: String)

    public static func parse(_ url: URL) -> DeepLink? {
        guard url.scheme?.lowercased() == "calendar", let host = url.host?.lowercased() else { return nil }
        let items = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems ?? []
        func q(_ n: String) -> String? {
            let v = items.first { $0.name == n }?.value
            return (v?.isEmpty ?? true) ? nil : v
        }
        switch host {
        case "pair":
            return .pair(host: q("host"), token: q("token"))
        case "connected":
            if let e = q("error") { return .connectError(code: e) }
            if let a = q("account") { return .connected(accountId: a, status: q("status")) }
            return .connectError(code: "internal")
        default:
            return nil
        }
    }
}
