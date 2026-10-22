import CalendarCore
import Foundation
import Observation

/// API tokens (canvas y-tokens, y-newtoken). Admin scope is needed; a 403 is explained plainly, never retried.
@MainActor
@Observable
public final class TokensViewModel {
    public enum Phase: Equatable, Sendable {
        case loading, loaded
        /// This phone's token is not an admin token.
        case needsAdmin
        case failed(String)
    }

    public struct Row: Identifiable, Equatable, Sendable {
        public var id: String
        public var name: String
        public var sub: String
        public var isMine: Bool
        public var revoked: Bool
        public var token: APIToken
    }

    public private(set) var phase: Phase = .loading
    public private(set) var tokens: [APIToken] = []
    public private(set) var currentTokenId: String?
    /// Shown inline on the list / detail after a failed revoke.
    public var message: String?

    @ObservationIgnored private let api: any HubTokensAPI
    @ObservationIgnored private let now: @Sendable () -> Date

    public init(api: any HubTokensAPI, now: @escaping @Sendable () -> Date = { Date() }) {
        self.api = api; self.now = now
    }

    // MARK: Derived

    public var active: [Row] { tokens.filter { $0.revokedAt == nil }.map(row) }
    public var revoked: [Row] { tokens.filter { $0.revokedAt != nil }.map(row) }

    func row(_ t: APIToken) -> Row {
        Row(id: t.id, name: t.name, sub: Self.subLine(t, now: now()), isMine: t.id == currentTokenId, revoked: t.revokedAt != nil, token: t)
    }

    public static func scopeWords(_ s: TokenScope) -> String {
        switch s { case .read: "Read only"; case .write: "Read and write"; case .admin: "Full access" }
    }
    public static func kindWords(_ k: TokenKind) -> String { k == .device ? "Phone or tablet" : "Script" }

    /// "Read and write \u{00B7} used just now", "Read only \u{00B7} not used in 9 days", "Script \u{00B7} Read only \u{00B7} never used".
    public static func subLine(_ t: APIToken, now: Date) -> String {
        if let r = t.revokedAt { return "\(scopeWords(t.scope)) \u{00B7} revoked \(ago(r, now: now))" }
        return "\(scopeWords(t.scope)) \u{00B7} \(usedWords(t, now: now))"
    }

    /// "used just now", "used 5 min ago", "not used in 9 days", "never used".
    public static func usedWords(_ t: APIToken, now: Date) -> String {
        guard let u = t.lastUsedAt else { return "never used" }
        let secs = max(0, now.timeIntervalSince(u))
        let days = Int(secs / 86_400)
        if secs < 120 { return "used just now" }
        if secs < 3600 { return "used \(Int(secs / 60)) min ago" }
        if secs < 86_400 { return "used \(Int(secs / 3600)) h ago" }
        if days < 7 { return days == 1 ? "used yesterday" : "used \(days) days ago" }
        return "not used in \(days) days"
    }

    static func ago(_ d: Date, now: Date) -> String {
        let secs = max(0, now.timeIntervalSince(d))
        if secs < 120 { return "just now" }
        if secs < 3600 { return "\(Int(secs / 60)) min ago" }
        if secs < 86_400 { return "\(Int(secs / 3600)) h ago" }
        let days = Int(secs / 86_400)
        return days == 1 ? "yesterday" : "\(days) days ago"
    }

    // MARK: Actions

    public func load() async {
        if tokens.isEmpty { phase = .loading }
        if currentTokenId == nil { currentTokenId = (try? await api.hello())?.tokenId }
        do {
            tokens = try await api.tokens().sorted { $0.createdAt > $1.createdAt }
            phase = .loaded
        } catch {
            phase = Self.phase(for: error)
        }
    }

    static func phase(for e: Error) -> Phase {
        switch e {
        case APIClientError.unauthorized(let b) where b?.code == "forbidden": .needsAdmin
        case APIClientError.unauthorized: .failed("The server no longer accepts this phone\u{2019}s token.")
        case APIClientError.transport, APIClientError.tls: .failed("Can\u{2019}t reach the server. Check Tailscale, then try again.")
        default: .failed("The server returned something unexpected.")
        }
    }

    /// Returns true when the token is gone (revoked now, or already gone).
    @discardableResult
    public func revoke(_ id: String) async -> Bool {
        if id == currentTokenId {
            message = "This is the token this phone uses. Revoke it from another device, or forget the server."
            return false
        }
        do {
            try await api.revokeToken(id: id)
        } catch APIClientError.notFound {
            // already gone
        } catch {
            switch Self.phase(for: error) {
            case .needsAdmin: message = "This token can\u{2019}t revoke tokens. Use a token with admin access."
            case .failed(let m): message = m
            default: message = "Couldn\u{2019}t revoke the token. Nothing was changed."
            }
            if case APIClientError.server(_, let b?) = error, b.code == "validation" || b.error.code == "validation" { message = b.message }
            return false
        }
        message = nil
        await load()
        return true
    }
}

/// New token (canvas y-newtoken): form, then the secret exactly once.
@MainActor
@Observable
public final class NewTokenViewModel {
    public enum Phase: Equatable, Sendable {
        case editing
        case creating
        /// The secret lives only here, only until `done()`.
        case revealed(CreatedToken)
    }

    public var name = "" { didSet { if name != oldValue { error = nil } } }
    public var kind: TokenKind = .script
    public var canWrite = false
    public private(set) var phase: Phase = .editing
    public private(set) var error: String?
    public private(set) var copied = false

    @ObservationIgnored private let api: any HubTokensAPI
    @ObservationIgnored private let copy: @MainActor (String) -> Void

    public init(api: any HubTokensAPI, copy: @escaping @MainActor (String) -> Void) {
        self.api = api; self.copy = copy
    }

    public var scope: TokenScope { canWrite ? .write : .read }
    public var trimmedName: String { name.trimmingCharacters(in: .whitespacesAndNewlines) }
    public var canCreate: Bool { phase == .editing && !trimmedName.isEmpty && trimmedName.count <= 100 }
    public var secret: String? { if case .revealed(let t) = phase { t.secret } else { nil } }

    public func create() async {
        guard canCreate else { return }
        phase = .creating
        do {
            phase = .revealed(try await api.createToken(name: trimmedName, kind: kind, scope: scope))
        } catch {
            phase = .editing
            switch TokensViewModel.phase(for: error) {
            case .needsAdmin: self.error = "This token can\u{2019}t make tokens. Use a token with admin access."
            case .failed(let m): self.error = m
            default: self.error = "Couldn\u{2019}t make the token."
            }
            if case APIClientError.server(_, let b?) = error { self.error = b.message }
        }
    }

    public func copyToken() {
        guard let s = secret else { return }
        copy(s); copied = true
    }

    /// Forgets the secret. There is no way back to it: the hub never returns it again.
    public func done() { phase = .editing; name = ""; copied = false }
}
