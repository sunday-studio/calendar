import CalendarCore
import CalendarUI
import Foundation
import Testing
@testable import FeatureSettings

// MARK: Fakes

final class SettingsFakeCalendarRepo: CalendarRepository, @unchecked Sendable {
    var list: [HubCalendar]
    var failure: Error?
    private(set) var updates: [(id: String, visible: Bool?, colour: ColorOverrideChange)] = []
    init(_ l: [HubCalendar]) { list = l }
    func calendars() async throws -> [HubCalendar] { list }
    func observeCalendars() -> AsyncStream<[HubCalendar]> { AsyncStream { $0.yield(self.list) } }
    func update(id: String, visible: Bool?, colorOverride: ColorOverrideChange) async throws -> HubCalendar {
        updates.append((id, visible, colorOverride))
        if let failure { throw failure }
        let i = list.firstIndex { $0.id == id }!
        if let visible { list[i].visible = visible }
        switch colorOverride { case .set(let k): list[i].colorOverride = k; case .clear: list[i].colorOverride = nil; case .unchanged: break }
        return list[i]
    }
}

struct SettingsFakeAccountRepo: AccountRepository {
    var list: [Account]
    func accounts() async throws -> [Account] { list }
    func observeAccounts() -> AsyncStream<[Account]> { AsyncStream { $0.yield(list) } }
}

final class SettingsFakeSettingsRepo: SettingsRepository, @unchecked Sendable {
    var current: Settings
    var failure: Error?
    private(set) var puts: [Settings] = []
    init(_ s: Settings) { current = s }
    func settings() async throws -> Settings? { current }
    func observeSettings() -> AsyncStream<Settings?> { AsyncStream { $0.yield(self.current) } }
    func userTimeZone() async throws -> TimeZone { .current }
    func update(_ s: Settings) async throws -> Settings {
        puts.append(s)
        if let failure { throw failure }
        current = s; return s
    }
}

final class FakeStatusSource: StatusSource, @unchecked Sendable {
    var status: Result<StatusDTO, Error>
    var runs: [SyncRun] = []
    private(set) var posted = 0
    init(_ s: Result<StatusDTO, Error>) { status = s }
    func status() async throws -> StatusDTO { try status.get() }
    func syncRuns(limit: Int?, accountId: String?) async throws -> [SyncRun] { runs }
    func postSync(accountId: String?) async throws -> Int { posted += 1; return 1 }
}

final class SettingsFakeSync: SyncControlling, @unchecked Sendable {
    var states: [SyncState] = [.idle]
    private(set) var refreshes = 0
    var state: AsyncStream<SyncState> { AsyncStream { c in for s in self.states { c.yield(s) } } }
    func appForeground() async {}
    func appBackground() async {}
    func manualRefresh() async { refreshes += 1 }
    func silentPush() async -> Bool { false }
}

struct SettingsFakeMeta: SyncMetaRepository {
    var at: Date?
    func lastSyncAt() async throws -> Date? { at }
    func observeLastSyncAt() -> AsyncStream<Date?> { AsyncStream { $0.yield(at) } }
    func lastRev() async throws -> Int64 { 0 }
}

private let t0 = Date(timeIntervalSince1970: 1_800_000_000)
private let acct = Account(id: "a1", email: "me@x.com", displayName: "Me", color: "cobalt", status: "active")
private func cal(_ id: String, _ name: String, primary: Bool = false, access: String = "owner", visible: Bool = true) -> HubCalendar {
    HubCalendar(id: id, accountId: "a1", name: name, color: "cobalt", access: access, isPrimary: primary, visible: visible)
}
private func settle() async { try? await Task.sleep(for: .milliseconds(100)) }
private struct Boom: Error {}

// MARK: Logic

struct SettingsLogicTests {
    @Test func agoWords() {
        #expect(SettingsLogic.ago(t0, now: t0.addingTimeInterval(30)) == "just now")
        #expect(SettingsLogic.ago(t0, now: t0.addingTimeInterval(12 * 60)) == "12 min ago")
        #expect(SettingsLogic.ago(t0, now: t0.addingTimeInterval(3 * 3600)) == "3 h ago")
        #expect(SettingsLogic.ago(t0, now: t0.addingTimeInterval(30 * 3600)) == "yesterday")
        #expect(SettingsLogic.ago(t0, now: t0.addingTimeInterval(5 * 86400)) == "5 days ago")
        #expect(SettingsLogic.ago(t0, now: t0.addingTimeInterval(-100)) == "just now")
    }

    @Test func timeParsing() {
        #expect(SettingsLogic.minutes("09:30") == 570)
        #expect(SettingsLogic.minutes("9:30") == nil)
        #expect(SettingsLogic.minutes("24:00") == nil)
        #expect(SettingsLogic.minutes("12:60") == nil)
        #expect(SettingsLogic.hhmm(570) == "09:30")
    }

    @Test func bannerPerState() {
        #expect(ConnectionBanner.make(state: .idle, lastSyncAt: t0, now: t0) == nil)
        #expect(ConnectionBanner.make(state: .syncing, lastSyncAt: nil, now: t0) == nil)
        #expect(ConnectionBanner.make(state: .offline, lastSyncAt: nil, now: t0)?.kind == .offline)
        let stale = ConnectionBanner.make(state: .stale(since: t0), lastSyncAt: t0, now: t0.addingTimeInterval(600))
        #expect(stale?.kind == .stale)
        #expect(stale?.detail == "Last synced 10 min ago.")
        #expect(ConnectionBanner.make(state: .error("Bad token"), lastSyncAt: nil, now: t0)?.detail == "Bad token")
    }
}

// MARK: Calendars

@MainActor struct CalendarsViewModelTests {
    private func make(_ repo: SettingsFakeCalendarRepo) -> CalendarsViewModel {
        let vm = CalendarsViewModel(deps: .init(calendars: repo, accounts: SettingsFakeAccountRepo(list: [acct])))
        vm.start()
        return vm
    }

    @Test func groupsByAccountPrimaryFirst() async {
        let vm = make(SettingsFakeCalendarRepo([cal("c2", "Zed"), cal("c1", "Work", primary: true), cal("c3", "Alpha")]))
        await settle()
        #expect(vm.groups.count == 1)
        #expect(vm.groups[0].calendars.map(\.name) == ["Work", "Alpha", "Zed"])
    }

    @Test func toggleVisibleSendsPatchAndSticks() async {
        let repo = SettingsFakeCalendarRepo([cal("c1", "Work")])
        let vm = make(repo); await settle()
        await vm.setVisible("c1", false)
        #expect(repo.updates.count == 1)
        #expect(repo.updates[0].visible == false)
        #expect(vm.groups[0].calendars[0].visible == false)
        #expect(vm.errorMessage == nil)
        #expect(vm.pending.isEmpty)
    }

    @Test func failureRollsBackWithMessage() async {
        let repo = SettingsFakeCalendarRepo([cal("c1", "Work")])
        repo.failure = Boom()
        let vm = make(repo); await settle()
        await vm.setVisible("c1", false)
        #expect(vm.groups[0].calendars[0].visible == true)
        #expect(vm.errorMessage != nil)
    }

    @Test func hubMessageIsShown() async {
        let repo = SettingsFakeCalendarRepo([cal("c1", "Work")])
        repo.failure = APIClientError.server(status: 400, APIError(error: .init(code: "validation", message: "Unknown colour.", field: "colorOverride")))
        let vm = make(repo); await settle()
        await vm.setColor("c1", .rose)
        #expect(vm.errorMessage == "Unknown colour.")
    }

    @Test func colourSetAndClear() async {
        let repo = SettingsFakeCalendarRepo([cal("c1", "Work")])
        let vm = make(repo); await settle()
        await vm.setColor("c1", .moss)
        #expect(repo.updates[0].colour == .set("moss"))
        #expect(CalendarsViewModel.colorKey(vm.groups[0].calendars[0]) == "moss")
        await vm.setColor("c1", nil)
        #expect(repo.updates[1].colour == .clear)
        #expect(CalendarsViewModel.colorKey(vm.groups[0].calendars[0]) == "cobalt")
    }
}

// MARK: Settings

@MainActor struct SettingsViewModelTests {
    private func make(_ repo: SettingsFakeSettingsRepo, cals: [HubCalendar] = [cal("c1", "Work"), cal("c2", "Holidays", access: "reader")]) -> SettingsViewModel {
        let vm = SettingsViewModel(deps: .init(settings: repo, calendars: SettingsFakeCalendarRepo(cals)))
        vm.start()
        return vm
    }

    @Test func loadsAndOnlyWritableCalendarsOffered() async {
        let vm = make(SettingsFakeSettingsRepo(Settings()))
        await settle()
        #expect(vm.loaded && !vm.isDirty)
        #expect(vm.writableCalendars.map(\.id) == ["c1"])
    }

    @Test func editingMakesDirtyAndSavePuts() async {
        let repo = SettingsFakeSettingsRepo(Settings())
        let vm = make(repo); await settle()
        vm.draft.weekStart = 0
        vm.draft.defaultCalendarId = "c1"
        #expect(vm.isDirty)
        #expect(await vm.save())
        #expect(repo.puts.count == 1 && repo.puts[0].weekStart == 0)
        #expect(!vm.isDirty)
    }

    @Test func validationBlocksSave() async {
        let repo = SettingsFakeSettingsRepo(Settings())
        let vm = make(repo); await settle()
        vm.workStartMinutes = 18 * 60
        vm.draft.defaultCalendarId = "c2"          // read-only
        vm.draft.secondaryTzid = "Mars/Olympus"
        #expect(await vm.save() == false)
        #expect(repo.puts.isEmpty)
        #expect(Set(vm.errors.keys) == [.workingHours, .defaultCalendar, .secondaryTimeZone])
    }

    @Test func workingDaysToggle() async {
        let vm = make(SettingsFakeSettingsRepo(Settings())); await settle()
        vm.toggleWorkingDay(6); vm.toggleWorkingDay(1)
        #expect(vm.draft.workingHours.days == [2, 3, 4, 5, 6])
        vm.toggleWorkingDay(9)
        #expect(vm.draft.workingHours.days == [2, 3, 4, 5, 6])
    }

    @Test func hubFieldErrorLandsOnField() async {
        let repo = SettingsFakeSettingsRepo(Settings())
        repo.failure = APIClientError.server(status: 400, APIError(error: .init(code: "validation", message: "Bad hours.", field: "workingHours.end")))
        let vm = make(repo); await settle()
        vm.draft.weekStart = 0
        #expect(await vm.save() == false)
        #expect(vm.errors[.workingHours] == "Bad hours.")
        #expect(vm.isDirty)          // edits are kept
    }

    @Test func revertDiscardsEdits() async {
        let vm = make(SettingsFakeSettingsRepo(Settings())); await settle()
        vm.draft.weekStart = 0
        vm.revert()
        #expect(!vm.isDirty)
    }
}

// MARK: Status

@MainActor struct StatusViewModelTests {
    private func dto(_ accounts: [AccountSyncStatus], warnings: [HubWarning] = []) -> StatusDTO {
        StatusDTO(hubVersion: "0.1.0", rev: 5, dbSizeBytes: 2_000_000, sseClients: 2, accounts: accounts, warnings: warnings)
    }
    private func acc(_ status: String, err: String? = nil, fails: Int = 0) -> AccountSyncStatus {
        AccountSyncStatus(accountId: "a1", email: "me@x.com", status: status, lastError: err, failCount: fails)
    }
    private func vm(_ src: FakeStatusSource, sync: SettingsFakeSync = SettingsFakeSync()) -> StatusViewModel {
        StatusViewModel(deps: .init(source: src, sync: sync, syncMeta: SettingsFakeMeta(at: t0), clock: .fixed(t0.addingTimeInterval(600))))
    }

    @Test func loadsHubSummaryAndAccounts() async {
        let src = FakeStatusSource(.success(dto([acc("needs_reauth", err: "invalid_grant", fails: 3)], warnings: [HubWarning(kind: "x", message: "Secret expires soon")])))
        src.runs = [
            SyncRun(id: 2, accountId: "a1", calendarId: nil, startedAt: t0, durationMs: 90, trigger: "timer", added: 2, changed: 1, removed: 0, errorKind: nil, error: nil),
            SyncRun(id: 1, accountId: "a1", calendarId: nil, startedAt: t0, durationMs: 90, trigger: "timer", added: 0, changed: 0, removed: 0, errorKind: nil, error: nil),
            SyncRun(id: 0, accountId: "a1", calendarId: nil, startedAt: t0, durationMs: 9, trigger: "manual", added: 0, changed: 0, removed: 0, errorKind: "auth", error: "Token revoked"),
        ]
        let m = vm(src)
        await m.refresh()
        #expect(m.load == .loaded)
        #expect(m.hubVersion == "0.1.0" && m.connectedDevices == 2)
        #expect(m.warnings == ["Secret expires soon"])
        #expect(m.accounts[0].word == "Sign in needed" && m.accounts[0].needsAttention)
        #expect(m.accounts[0].detail == "invalid_grant (3 tries)")
        #expect(m.runs.map(\.summary) == ["2 added, 1 changed", "No changes", "Token revoked"])
        #expect(m.runs.map(\.failed) == [false, false, true])
        #expect(m.runs[0].when == "10 min ago")
    }

    @Test func unreachableKeepsOldDataAndSaysSo() async {
        let src = FakeStatusSource(.success(dto([acc("active")])))
        let m = vm(src)
        await m.refresh()
        src.status = .failure(APIClientError.transport("offline"))
        await m.refresh()
        if case .unreachable = m.load {} else { Issue.record("expected unreachable") }
        #expect(m.accounts.count == 1)
    }

    @Test func bannerFollowsSyncState() async {
        let sync = SettingsFakeSync(); sync.states = [.stale(since: t0)]
        let m = vm(FakeStatusSource(.success(dto([]))), sync: sync)
        m.start(); await settle()
        #expect(m.banner?.kind == .stale)
        #expect(m.banner?.detail == "Last synced 10 min ago.")
    }

    @Test func syncNowPostsRefreshesAndReloads() async {
        let src = FakeStatusSource(.success(dto([acc("active")])))
        let sync = SettingsFakeSync()
        let m = vm(src, sync: sync)
        await m.syncNow()
        #expect(src.posted == 1 && sync.refreshes == 1)
        #expect(m.load == .loaded && !m.isSyncing)
    }
}
