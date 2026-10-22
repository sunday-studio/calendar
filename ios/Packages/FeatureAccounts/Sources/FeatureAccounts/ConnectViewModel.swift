import CalendarCore
import Foundation
import Observation

public enum ConnectKind: Equatable, Sendable {
    case add(provider: String)
    case reconnect(accountId: String)
}

public struct ConnectFailure: Equatable, Sendable {
    public var title: String
    public var message: String
}

public enum ConnectPhase: Equatable, Sendable {
    case idle
    case starting          // asking the hub for the sign-in URL
    case signingIn         // system sheet is up
    case finishing(accountId: String) // polling until the hub is done
    case connected(Account)
    case cancelled
    case failed(ConnectFailure)
}

@MainActor
@Observable
public final class ConnectViewModel {
    public private(set) var phase: ConnectPhase = .idle
    public let kind: ConnectKind

    @ObservationIgnored private let api: any HubAPI
    @ObservationIgnored private let auth: any WebAuthSession
    @ObservationIgnored private let pollInterval: Duration
    @ObservationIgnored private let maxPolls: Int

    public init(kind: ConnectKind, api: any HubAPI, auth: any WebAuthSession,
                pollInterval: Duration = .seconds(1), maxPolls: Int = 30) {
        self.kind = kind
        self.api = api
        self.auth = auth
        self.pollInterval = pollInterval
        self.maxPolls = maxPolls
    }

    public var isReconnect: Bool { if case .reconnect = kind { true } else { false } }

    public func run() async {
        switch phase { case .starting, .signingIn, .finishing: return; default: break }
        phase = .starting
        let session: ConnectSession
        do {
            switch kind {
            case .add(let provider): session = try await api.connectAccount(provider: provider, reauthAccountId: nil)
            case .reconnect(let id): session = try await api.reconnectAccount(id: id)
            }
        } catch {
            phase = .failed(Self.describe(startError: error))
            return
        }
        guard let url = URL(string: session.authUrl) else {
            phase = .failed(ConnectFailure(title: "Couldn’t start sign in", message: "The server sent an address this phone can’t open."))
            return
        }
        phase = .signingIn
        let callback: URL
        do {
            callback = try await auth.authenticate(url: url, callbackScheme: "calendar")
        } catch WebAuthError.cancelled {
            phase = .cancelled
            return
        } catch {
            phase = .failed(ConnectFailure(title: "Sign in didn’t finish", message: "The sign-in page couldn’t be shown. Try again."))
            return
        }
        await handle(callback: callback)
    }

    /// Also used when the hub's redirect arrives as a plain deep link.
    public func handle(callback: URL) async {
        switch DeepLink.parse(callback) {
        case .connected(let id, _):
            phase = .finishing(accountId: id)
            await poll(id: id)
        case .connectError(let code):
            phase = .failed(Self.describe(code: code, reconnect: isReconnect))
        default:
            phase = .failed(ConnectFailure(title: "Sign in didn’t finish", message: "The server’s reply wasn’t understood. Try again."))
        }
    }

    private func poll(id: String) async {
        var last: Account?
        for i in 0..<maxPolls {
            if Task.isCancelled { return }
            do {
                let a = try await api.account(id: id)
                last = a
                if a.statusKind != .syncing { break }
            } catch APIClientError.notFound {
                phase = .failed(ConnectFailure(title: "The account went missing", message: "The server no longer lists it. Try adding it again."))
                return
            } catch {
                // transient; keep polling
            }
            if i < maxPolls - 1 { try? await Task.sleep(for: pollInterval) }
        }
        guard let a = last else {
            phase = .failed(ConnectFailure(title: "Couldn’t check the account", message: "The server isn’t answering. Pull to refresh on the accounts list."))
            return
        }
        switch a.statusKind {
        case .needsReauth:
            phase = .failed(ConnectFailure(title: "Google needs you to sign in again", message: a.statusDetail ?? "Sign in once more to finish."))
        case .error:
            phase = .failed(ConnectFailure(title: "Connected, but sync failed", message: a.statusDetail ?? "The server couldn’t read this account."))
        default:
            phase = .connected(a) // active, paused, or still syncing in the background
        }
    }

    // MARK: Messages

    static func describe(code: String, reconnect: Bool) -> ConnectFailure {
        switch code {
        case "access_denied":
            ConnectFailure(title: "Sign in was cancelled", message: "Google didn’t give this app access. Nothing was changed.")
        case "invalid_state":
            ConnectFailure(title: "That sign in expired", message: "The link is single use and short lived. Start again.")
        case "identity_mismatch":
            ConnectFailure(title: "Different Google account", message: "You signed in with another account than the one you’re fixing. Choose the same one.")
        case "no_refresh_token":
            ConnectFailure(title: "Google gave no lasting access", message: "Remove this app at myaccount.google.com/permissions, then try again.")
        case "exchange_failed":
            ConnectFailure(title: "The server couldn’t finish", message: "It couldn’t trade the code with Google. Check the server’s connection and try again.")
        default:
            ConnectFailure(title: "Something went wrong", message: "The server hit an error (\(code)). Try again.")
        }
    }

    static func describe(startError e: Error) -> ConnectFailure {
        if case APIClientError.server(let status, let body) = e, status == 503 {
            return ConnectFailure(title: "Google isn’t set up on the server", message: body?.message ?? "Add the Google client file to the hub, then try again.")
        }
        if case APIClientError.unauthorized = e {
            return ConnectFailure(title: "This token can’t add accounts", message: "Use a token with admin access.")
        }
        if case APIClientError.transport = e {
            return ConnectFailure(title: "Can’t reach the server", message: "Check Tailscale and try again.")
        }
        return ConnectFailure(title: "Couldn’t start sign in", message: "The server said no. Try again in a moment.")
    }
}
