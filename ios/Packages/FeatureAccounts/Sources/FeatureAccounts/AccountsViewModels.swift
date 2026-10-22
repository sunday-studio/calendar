import CalendarCore
import Foundation
import Observation

public enum LoadPhase: Equatable, Sendable {
    case loading, loaded
    case failed(String)
}

@MainActor
@Observable
public final class AccountsListViewModel {
    public private(set) var accounts: [Account] = []
    /// Calendars per account id, for the "Google · 4 calendars" line.
    public private(set) var calendarCounts: [String: Int] = [:]
    public private(set) var phase: LoadPhase = .loading
    public var toast: String?

    @ObservationIgnored let api: any HubAPI

    public init(api: any HubAPI) { self.api = api }

    public var needingSignIn: [Account] { accounts.filter { $0.statusKind == .needsReauth } }

    public func load() async {
        if accounts.isEmpty { phase = .loading }
        do {
            accounts = try await api.accounts()
            phase = .loaded
            if let cals = try? await api.calendars() {
                calendarCounts = Dictionary(grouping: cals, by: \.accountId).mapValues(\.count)
            }
        } catch {
            phase = .failed(Self.message(error))
        }
    }

    public func replace(_ account: Account) {
        if let i = accounts.firstIndex(where: { $0.id == account.id }) { accounts[i] = account }
    }

    public func remove(id: String) { accounts.removeAll { $0.id == id } }

    static func message(_ e: Error) -> String {
        switch e {
        case APIClientError.transport: "Can’t reach the server. Check Tailscale, then pull to refresh."
        case APIClientError.unauthorized: "The server no longer accepts this phone’s token."
        default: "The server returned something unexpected."
        }
    }
}

@MainActor
@Observable
public final class AccountDetailViewModel {
    public private(set) var account: Account
    public private(set) var calendars: [HubCalendar] = []
    public private(set) var removed = false
    public var errorMessage: String?
    public var toast: String?
    public private(set) var busy = false

    @ObservationIgnored private let api: any HubAPI

    public init(account: Account, api: any HubAPI) {
        self.account = account
        self.api = api
    }

    public func load() async {
        if let a = try? await api.account(id: account.id) { account = a }
        if let cals = try? await api.calendars() { calendars = cals.filter { $0.accountId == account.id } }
    }

    public func setColor(_ key: String) async {
        guard key != account.color else { return }
        let previous = account
        account.color = key // optimistic
        do { account = try await api.patchAccount(id: account.id, color: key, displayName: nil) }
        catch {
            account = previous
            errorMessage = "Couldn’t change the colour. Try again."
        }
    }

    public func reconnected() async {
        await load()
        toast = "Reconnected"
    }

    public func remove() async {
        busy = true
        defer { busy = false }
        do {
            try await api.deleteAccount(id: account.id)
            removed = true
        } catch APIClientError.notFound {
            removed = true // already gone
        } catch {
            errorMessage = "Couldn’t remove the account. Nothing was changed."
        }
    }
}
