import CalendarCore
import CalendarUI
import Foundation
import Observation

public struct CalendarGroup: Equatable, Identifiable, Sendable {
    public var account: Account
    public var calendars: [HubCalendar]
    public var id: String { account.id }
}

public struct CalendarsDependencies: Sendable {
    public var calendars: any CalendarRepository
    public var accounts: any AccountRepository
    public init(calendars: any CalendarRepository, accounts: any AccountRepository) {
        self.calendars = calendars; self.accounts = accounts
    }
    public init(env: AppEnvironment) { self.init(calendars: env.calendars, accounts: env.accounts) }
}

/// Calendar visibility + colour override. Changes show at once and roll back with a message if the hub refuses.
@MainActor @Observable
public final class CalendarsViewModel {
    public private(set) var groups: [CalendarGroup] = []
    public private(set) var loaded = false
    public private(set) var pending: Set<String> = []
    public var errorMessage: String?

    private let deps: CalendarsDependencies
    private var base: [HubCalendar] = []
    private var accounts: [Account] = []
    private var optimistic: [String: HubCalendar] = [:]
    private var tasks: [Task<Void, Never>] = []

    public init(deps: CalendarsDependencies) { self.deps = deps }

    public func start() {
        guard tasks.isEmpty else { return }
        tasks.append(Task { [weak self, deps] in
            for await cals in deps.calendars.observeCalendars() { self?.base = cals; self?.loaded = true; self?.rebuild() }
        })
        tasks.append(Task { [weak self, deps] in
            for await accts in deps.accounts.observeAccounts() { self?.accounts = accts; self?.rebuild() }
        })
    }

    public func stop() { tasks.forEach { $0.cancel() }; tasks = [] }

    /// Colour key shown for a calendar: its override, else the account palette colour the hub assigned.
    public static func colorKey(_ c: HubCalendar) -> String { c.colorOverride ?? c.color }

    public func setVisible(_ id: String, _ visible: Bool) async {
        await apply(id, change: { $0.visible = visible }, visible: visible, colour: .unchanged)
    }

    /// `nil` clears the override (back to the account colour).
    public func setColor(_ id: String, _ colour: AccountColor?) async {
        let change: ColorOverrideChange = colour.map { .set($0.rawValue) } ?? .clear
        await apply(id, change: { $0.colorOverride = colour?.rawValue }, visible: nil, colour: change)
    }

    private func apply(_ id: String, change: (inout HubCalendar) -> Void, visible: Bool?, colour: ColorOverrideChange) async {
        guard var current = optimistic[id] ?? base.first(where: { $0.id == id }) else { return }
        change(&current)
        optimistic[id] = current
        pending.insert(id)
        rebuild()
        do {
            let saved = try await deps.calendars.update(id: id, visible: visible, colorOverride: colour)
            if let i = base.firstIndex(where: { $0.id == id }) { base[i] = saved }   // no flicker before the DB observer fires
        } catch {
            errorMessage = Self.message(error)
        }
        optimistic[id] = nil   // the repository stored the result (or it failed): the observed value is the truth
        pending.remove(id)
        rebuild()
    }

    private func rebuild() {
        let cals = base.map { optimistic[$0.id] ?? $0 }
        let byAccount = Dictionary(grouping: cals, by: \.accountId)
        groups = accounts.compactMap { a in
            guard let list = byAccount[a.id], !list.isEmpty else { return nil }
            let sorted = list.sorted {
                if $0.isPrimary != $1.isPrimary { return $0.isPrimary }
                return $0.name.localizedCaseInsensitiveCompare($1.name) == .orderedAscending
            }
            return CalendarGroup(account: a, calendars: sorted)
        }
    }

    static func message(_ error: Error) -> String {
        if case APIClientError.server(_, let body?) = error { return body.message }
        if case APIClientError.unauthorized = error { return "This phone is no longer signed in to your hub." }
        return "Couldn't save that change. Check your connection and try again."
    }
}
