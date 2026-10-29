import Foundation

/// URLs the surfaces (widgets, notifications, Live Activity) open. The app handles them in `onOpenURL`.
public enum SurfaceLink: Equatable, Sendable {
    case event(String)
    case today
    /// `calendar://day/<yyyy-MM-dd>`
    case day(String)
    /// `calendar://new`
    case new
    case accounts
    case summary(date: String?)

    public var url: URL {
        switch self {
        case .event(let id):
            var c = URLComponents(); c.scheme = "calendar"; c.host = "event"
            c.percentEncodedPath = "/" + (id.addingPercentEncoding(withAllowedCharacters: Self.segment) ?? id)
            return c.url!
        case .today: return URL(string: "calendar://today")!
        case .day(let d): return URL(string: "calendar://day/\(d)")!
        case .new: return URL(string: "calendar://new")!
        case .accounts: return URL(string: "calendar://accounts")!
        case .summary(let date):
            return URL(string: "calendar://summary" + (date.map { "?date=\($0)" } ?? ""))!
        }
    }

    private static let segment: CharacterSet = {
        var s = CharacterSet.urlPathAllowed; s.remove("/"); return s
    }()

    public static func parse(_ url: URL) -> SurfaceLink? {
        guard url.scheme?.lowercased() == "calendar", let host = url.host?.lowercased() else { return nil }
        switch host {
        case "event":
            let id = String(url.path.dropFirst())
            return id.isEmpty ? nil : .event(id)
        case "today": return .today
        case "day":
            let d = String(url.path.dropFirst())
            return d.isEmpty ? .today : .day(d)
        case "new": return .new
        case "accounts": return .accounts
        case "summary":
            let d = URLComponents(url: url, resolvingAgainstBaseURL: false)?.queryItems?.first { $0.name == "date" }?.value
            return .summary(date: d)
        default: return nil
        }
    }
}
