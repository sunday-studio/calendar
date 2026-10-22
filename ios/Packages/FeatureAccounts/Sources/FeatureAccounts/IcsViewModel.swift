import CalendarCore
import Foundation
import Observation

/// "Calendar link" (canvas a-ics): paste an .ics / webcal URL; the hub fetches it first and saves nothing on failure.
@MainActor
@Observable
public final class IcsLinkViewModel {
    public enum Phase: Equatable, Sendable {
        case editing
        case checking
        case added(Account)
    }

    public var url = "" { didSet { if url != oldValue { urlError = nil; banner = nil } } }
    public var name = ""
    public private(set) var phase: Phase = .editing
    /// Plain message under the URL field (hub 422 with field "url").
    public private(set) var urlError: String?
    /// Anything that is not about the URL (offline, 403 admin).
    public private(set) var banner: String?

    @ObservationIgnored private let api: any HubAPI

    public init(api: any HubAPI) { self.api = api }

    public var trimmedURL: String { url.trimmingCharacters(in: .whitespacesAndNewlines) }
    public var canAdd: Bool { !trimmedURL.isEmpty && phase == .editing }
    public var isChecking: Bool { phase == .checking }

    public func add() async {
        guard canAdd else { return }
        let lower = trimmedURL.lowercased()
        guard lower.hasPrefix("https://") || lower.hasPrefix("webcal://") else {
            urlError = "Use an address that starts with https:// or webcal://."
            return
        }
        phase = .checking
        urlError = nil; banner = nil
        let n = name.trimmingCharacters(in: .whitespacesAndNewlines)
        do {
            let account = try await api.addIcsAccount(url: trimmedURL, name: n.isEmpty ? nil : n, color: nil)
            phase = .added(account)
        } catch {
            phase = .editing
            let f = Self.describe(error)
            urlError = f.field ? f.message : nil
            banner = f.field ? nil : f.message
        }
    }

    static func describe(_ e: Error) -> (field: Bool, message: String) {
        switch e {
        case APIClientError.server(_, let b?) where b.error.field == "url":
            return (true, b.message)
        case APIClientError.server(let status, let b?) where status == 422 || b.code == "validation":
            return (b.error.field == nil || b.error.field == "url", b.message)
        case APIClientError.unauthorized(let b):
            return (false, b?.code == "forbidden" ? "This phone\u{2019}s token can\u{2019}t add accounts. Use a token with admin access." : "The server no longer accepts this phone\u{2019}s token.")
        case APIClientError.transport, APIClientError.tls:
            return (false, "Can\u{2019}t reach the server. Nothing was saved.")
        default:
            return (false, "The server returned something unexpected. Nothing was saved.")
        }
    }
}
