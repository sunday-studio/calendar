import CalendarCore
import CalendarUI
import Foundation
import Testing
@testable import FeatureHome

// MARK: Fixtures

private let utc = TimeZone(identifier: "UTC")!
private let gb = Locale(identifier: "en_GB")
/// Tuesday 13 Oct 2026, 09:52 UTC.
private let t0 = Date(timeIntervalSince1970: 1_791_885_120)

private func at(_ h: Int, _ m: Int = 0, dayOffset: Int = 0) -> Date {
    let startOfDay = DayKey(t0, in: utc).startOfDay(in: utc)
    return startOfDay.addingTimeInterval(TimeInterval(dayOffset * 86_400 + h * 3600 + m * 60))
}

private func ev(_ id: String, _ title: String = "Event", _ s: Date, _ e: Date, color: String = "cobalt", join: Join? = nil,
                loc: String? = nil, resp: String? = "accepted", status: String = "confirmed", busy: Bool = true,
                canRSVP: Bool = false, version: Int64 = 1) -> AgendaItem {
    AgendaItem(id: id, eventId: id, version: version, calendarId: "c", accountId: "a", color: color, title: title,
               allDay: false, start: s, end: e, tzid: "UTC", location: loc, join: join, myResponse: resp, status: status,
               busy: busy, canRSVP: canRSVP)
}

private func allDay(_ id: String, _ title: String, _ date: String, _ end: String) -> AgendaItem {
    AgendaItem(id: id, eventId: id, calendarId: "c", accountId: "a", color: "rose", title: title, allDay: true,
               date: date, endDate: end)
}

private let fmt = HomeFormat(tz: utc, locale: gb)

// MARK: Pure logic

@Suite("Home hero state")
struct HeroStateTests {
    private func hero(_ next: AgendaItem?, items: [AgendaItem] = [], accounts: Int = 1, now: Date = t0,
                      never: Bool = false, unreachable: Bool = false) -> HomeHero {
        HomeLogic.hero(accountCount: accounts, neverSynced: never, syncUnreachable: unreachable, next: next, items: items, now: now, tz: utc)
    }

    @Test func noAccounts() {
        #expect(hero(nil, accounts: 0) == .noAccounts)
        #expect(hero(nil, accounts: 0, never: true, unreachable: true) == .unreachable)
    }

    @Test func upcomingToday() {
        let e = ev("1", "Standup", at(10), at(10, 30))
        #expect(hero(e, items: [e]) == .upcoming(e))
    }

    @Test func liveWhileInProgress() {
        let e = ev("1", "Standup", at(9, 30), at(10, 30))
        #expect(hero(e, items: [e]) == .live(e))
    }

    @Test func doneWhenNextIsTomorrow() {
        let past = ev("0", "Earlier", at(8), at(9))
        let tomorrow = ev("1", "Standup", at(10, dayOffset: 1), at(11, dayOffset: 1))
        #expect(hero(tomorrow, items: [past, tomorrow]) == .doneForToday(next: tomorrow, hadEvents: true))
        #expect(hero(tomorrow, items: [tomorrow]) == .doneForToday(next: tomorrow, hadEvents: false))
    }

    @Test func nothingPlannedWhenNoEvents() {
        #expect(hero(nil) == .nothingPlanned)
    }

    @Test func doneWhenOnlyPastEventsRemain() {
        let past = ev("0", "Earlier", at(8), at(9))
        #expect(hero(nil, items: [past]) == .doneForToday(next: nil, hadEvents: true))
    }

    @Test func cancelledEventsDoNotCount() {
        let c = ev("0", "Gone", at(8), at(9), status: "cancelled")
        #expect(hero(nil, items: [c]) == .nothingPlanned)
    }
}

@Suite("Hero block")
struct HeroBlockTests {
    private let meet = Join(url: "https://meet.example/abc", kind: "meet")

    @Test func joinOnlyInsideTenMinutes() {
        let far = HomeLogic.block(for: ev("1", "R", at(11), at(12), join: meet), live: false, now: t0, format: fmt)
        #expect(far.action == .none)
        #expect(far.footer == "in 1 h 8 min" && far.time == "11:00")
        let near = HomeLogic.block(for: ev("1", "R", at(10), at(10, 30), join: meet), live: false, now: t0, format: fmt)
        #expect(near.action == .join("Join"))
        #expect(near.target == .join(URL(string: "https://meet.example/abc")!))
        #expect(near.footer == "in 8 min" && near.time == "10:00")
    }

    @Test func liveShowsJoinAndTimeLeft() {
        let b = HomeLogic.block(for: ev("1", "S", at(9, 40), at(10, 10), join: meet), live: true, now: t0, format: fmt)
        #expect(b.action == .join("Join"))
        #expect(b.footer == "Now \u{00B7} 18 min left")
    }

    @Test func liveWithoutLinkHasNoButton() {
        let b = HomeLogic.block(for: ev("1", "S", at(9, 40), at(10, 10)), live: true, now: t0, format: fmt)
        #expect(b.action == .none && b.footer == "Now \u{00B7} 18 min left")
    }

    @Test func inPersonGetsMapAndPlaceInFooter() {
        let b = HomeLogic.block(for: ev("1", "Dentist", at(11), at(12), loc: "14 Harley St"), live: false, now: t0, format: fmt)
        #expect(b.action == .map("Map"))
        #expect(b.target == .map("14 Harley St"))
        #expect(b.footer == "14 Harley St \u{00B7} in 1 h 8 min")
    }

    @Test func takesAccountColour() {
        let b = HomeLogic.block(for: ev("1", "S", at(11), at(12), color: "mauve"), live: false, now: t0, format: fmt)
        #expect(b.color == .mauve)
    }

    @Test func minutesFormatting() {
        #expect(HomeLogic.minutesText(8) == "8 min")
        #expect(HomeLogic.minutesText(60) == "1 h")
        #expect(HomeLogic.minutesText(128) == "2 h 8 min")
    }
}

@Suite("Overlap detection")
struct OverlapTests {
    @Test func detectsPartialOverlap() {
        let a = ev("a", "Design crit", at(11), at(12))
        let b = ev("b", "Pairing", at(11), at(11, 45))
        let c = ev("c", "Lunch", at(13), at(14))
        let o = HomeLogic.overlaps([c, b, a])
        #expect(o["a"] == "Pairing" && o["b"] == "Design crit" && o["c"] == nil)
    }

    @Test func backToBackIsNotAnOverlap() {
        #expect(HomeLogic.overlaps([ev("a", "A", at(10), at(11)), ev("b", "B", at(11), at(12))]).isEmpty)
    }

    @Test func ignoresAllDayDeclinedCancelledFreeAndZeroLength() {
        let base = ev("a", "A", at(10), at(12))
        let others = [
            allDay("d", "Birthday", "2026-10-13", "2026-10-14"),
            ev("x", "Declined", at(10), at(11), resp: "declined"),
            ev("y", "Cancelled", at(10), at(11), status: "cancelled"),
            ev("z", "Free", at(10), at(11), busy: false),
            ev("w", "Instant", at(10, 30), at(10, 30)),
        ]
        #expect(HomeLogic.overlaps([base] + others).isEmpty)
    }

    @Test func longEventOverlapsLaterOnes() {
        let long = ev("l", "Offsite", at(9), at(17))
        let o = HomeLogic.overlaps([long, ev("a", "A", at(10), at(11)), ev("b", "B", at(15), at(16))])
        #expect(o["l"] == "A" && o["a"] == "Offsite" && o["b"] == "Offsite")
    }
}

@Suite("Status words")
struct StatusWordTests {
    @Test func plainAcceptedHasNone() {
        #expect(HomeLogic.statusWord(ev("a", "A", at(10), at(11))) == nil)
        #expect(HomeLogic.statusWord(ev("a", "A", at(10), at(11), resp: nil)) == nil)
    }

    @Test func words() {
        #expect(HomeLogic.statusWord(ev("a", "A", at(10), at(11), resp: "tentative")) == .maybe)
        #expect(HomeLogic.statusWord(ev("a", "A", at(10), at(11), status: "tentative")) == .maybe)
        #expect(HomeLogic.statusWord(ev("a", "A", at(10), at(11), resp: "declined")) == .declined)
        #expect(HomeLogic.statusWord(ev("a", "A", at(10), at(11), status: "cancelled")) == .cancelled)
        #expect(HomeLogic.statusWord(ev("a", "A", at(10), at(11)), moved: true) == .moved)
        #expect(HomeLogic.statusWord(ev("a", "A", at(10), at(11), resp: "needsAction", canRSVP: true)) == .needsReply)
        #expect(HomeLogic.statusWord(ev("a", "A", at(10), at(11), resp: "needsAction", canRSVP: false)) == nil)
    }

    @Test func precedence() {
        let declinedMoved = ev("a", "A", at(10), at(11), resp: "declined")
        #expect(HomeLogic.statusWord(declinedMoved, moved: true) == .declined)
    }

    @Test func movedIsDetectedFromChangedStart() {
        let seen = ["a": at(10), "b": at(12)]
        let now = [ev("a", "A", at(10, 30), at(11)), ev("b", "B", at(12), at(13)), ev("c", "C", at(14), at(15))]
        #expect(HomeLogic.movedIDs(seen: seen, items: now) == ["a"])
    }

    @Test func changeCounting() {
        let after = [ev("a", "A", at(10), at(11), version: 2), ev("b", "B", at(12), at(13)), ev("n", "New", at(14), at(15))]
        #expect(HomeLogic.changeCount(before: ["a": 1, "b": 1, "gone": 1], after: after) == 3)
        #expect(HomeLogic.changeText(0) == "Up to date" && HomeLogic.changeText(1) == "1 change" && HomeLogic.changeText(4) == "4 changes")
    }
}

@Suite("Grouping")
struct GroupingTests {
    private func sections(_ items: [AgendaItem], now: Date = t0, show: Set<DayKey> = []) -> [HomeLogic.DaySection] {
        let today = DayKey(now, in: utc)
        return HomeLogic.sections(items: items, now: now, tz: utc, firstDay: today, lastDay: today.adding(days: 13),
                                  alwaysShow: show.union([today]), moved: [], format: fmt)
    }

    @Test func headersAreTodayTomorrowWeekday() {
        let s = sections([ev("1", "A", at(10), at(11)), ev("2", "B", at(10, dayOffset: 1), at(11, dayOffset: 1)),
                          ev("3", "C", at(10, dayOffset: 2), at(11, dayOffset: 2))])
        #expect(s.map(\.title) == ["Today", "Tomorrow", "Thu 15 Oct"])
        #expect(s[0].isToday)
    }

    @Test func todayAlwaysShownEvenWhenEmpty() {
        let s = sections([])
        #expect(s.count == 1 && s[0].entries == [.empty("2026-10-13")])
    }

    @Test func emptyDaysAreSkippedUnlessPicked() {
        let items = [ev("3", "C", at(10, dayOffset: 3), at(11, dayOffset: 3))]
        #expect(sections(items).map(\.title) == ["Today", "Fri 16 Oct"])
        #expect(sections(items, show: ["2026-10-15"]).map(\.title) == ["Today", "Thu 15 Oct", "Fri 16 Oct"])
    }

    @Test func allDayPinnedFirstThenByStart() {
        let s = sections([ev("2", "Later", at(14), at(15)), ev("1", "Early", at(8), at(9)), allDay("d", "Birthday", "2026-10-13", "2026-10-14")],
                         now: at(7))
        let ids = s[0].entries.compactMap { if case .row(let r) = $0 { r.id } else { nil } }
        #expect(ids == ["d", "1", "2"])
    }

    @Test func todaySplitsIntoComingThenEarlierToday() {
        let s = sections([ev("1", "Past", at(8), at(9)), ev("2", "Next", at(11), at(12))])
        #expect(s.map(\.title) == ["Today", "Earlier today"])
        #expect(s[1].isEarlier && s[1].id == "2026-10-13-earlier")
        guard case .row(let coming) = s[0].entries[0], case .row(let earlier) = s[1].entries[0] else { Issue.record("rows"); return }
        #expect(coming.id == "2" && !coming.model.isPast)
        #expect(earlier.id == "1" && earlier.model.isPast)
    }

    @Test func onlyFinishedEventsLeavesJustEarlierToday() {
        let s = sections([ev("1", "Past", at(8), at(9))])
        #expect(s.map(\.title) == ["Earlier today"])
    }

    @Test func liveEventRelabelsTodayAsUpNext() {
        let s = sections([ev("1", "Now", at(9, 30), at(10, 30)), ev("2", "Later", at(14), at(15))])
        #expect(s.map(\.title) == ["Up next"])
    }

    @Test func staleAccountsMarkRowsStale() {
        let items = [ev("1", "A", at(11), at(12))]
        let s = HomeLogic.sections(items: items, now: t0, tz: utc, firstDay: "2026-10-13", lastDay: "2026-10-26",
                                   alwaysShow: ["2026-10-13"], moved: [], format: fmt, staleAccounts: [items[0].accountId])
        guard case .row(let r) = s[0].entries[0] else { Issue.record("row"); return }
        #expect(r.model.status == .stale && r.model.statusWord == "Stale")
    }

    @Test func movedRowShowsNewTime() {
        let s = HomeLogic.sections(items: [ev("1", "A", at(11), at(12))], now: t0, tz: utc, firstDay: "2026-10-13",
                                   lastDay: "2026-10-26", alwaysShow: [], moved: ["1"], format: fmt)
        guard case .row(let r) = s[0].entries[0] else { Issue.record("row"); return }
        #expect(r.model.statusWord == "Moved 11:00")
    }

    @Test func itemsThatBeganEarlierShowUnderFirstDay() {
        let multi = allDay("m", "Offsite", "2026-10-11", "2026-10-16")
        let s = sections([multi])
        #expect(s[0].title == "Today" && s[0].count == 1)
    }

    @Test func overlapMarkerAndLabel() {
        let s = sections([ev("a", "Design crit", at(11), at(12), resp: "tentative"), ev("b", "Pairing", at(11), at(11, 45))])
        guard case .row(let a) = s[0].entries.first(where: { if case .row(let r) = $0 { r.id == "a" } else { false } })! else { return }
        #expect(a.overlapWith == "Pairing")
        #expect(a.model.status == .maybe)
        #expect(a.accessibilityLabel.contains("Design crit") && a.accessibilityLabel.contains("11:00 to 12:00")
                && a.accessibilityLabel.contains("overlaps another event with Pairing"))
    }
}

@Suite("Month grid")
struct MonthGridTests {
    @Test func mondayStart() {
        let cells = MonthGrid.cells(month: "2026-10-13", weekStart: 1)
        #expect(cells.first == "2026-09-28" && cells.last == "2026-11-01" && cells.count == 35)
    }

    @Test func sundayStart() {
        let cells = MonthGrid.cells(month: "2026-10-13", weekStart: 0)
        #expect(cells.first == "2026-09-27" && cells.count % 7 == 0 && cells.contains("2026-10-31"))
    }

    @Test func sixWeekMonth() {
        // March 2026 starts on a Sunday: Monday-start grid needs six rows.
        #expect(MonthGrid.cells(month: "2026-03-01", weekStart: 1).count == 42)
    }

    @Test func monthArithmetic() {
        #expect(MonthGrid.adding(months: 1, to: "2026-12-01") == "2027-01-01")
        #expect(MonthGrid.adding(months: -1, to: "2026-01-01") == "2025-12-01")
        #expect(MonthGrid.firstOfMonth("2026-10-13") == "2026-10-01")
        #expect(MonthGrid.weekdayLetters(weekStart: 1) == ["M", "T", "W", "T", "F", "S", "S"])
        #expect(MonthGrid.weekdayLetters(weekStart: 0).first == "S")
    }
}

@Suite("Banners")
struct BannerTests {
    @Test func connection() {
        #expect(HomeLogic.connectionBanner(state: .idle, lastSyncAt: t0, now: t0, format: fmt) == nil)
        #expect(HomeLogic.connectionBanner(state: .syncing, lastSyncAt: nil, now: t0, format: fmt) == nil)
        let down = HomeLogic.connectionBanner(state: .stale(since: at(8, 30)), lastSyncAt: nil, now: t0, format: fmt)
        #expect(down?.kind == .unreachable && down?.actionTitle == "Check")
        #expect(down?.text.title == "Can\u{2019}t reach your server.")
        #expect(down?.text.message == "This agenda is from 08:30 and may be out of date.")
        let old = HomeLogic.connectionBanner(state: .stale(since: at(8, 30, dayOffset: -1)), lastSyncAt: nil, now: t0, format: fmt)
        #expect(old?.text.message == "This agenda is from yesterday 08:30 and may be out of date.")
        let off = HomeLogic.connectionBanner(state: .stale(since: at(8, 30)), lastSyncAt: nil, isOnline: false, now: t0, format: fmt)
        #expect(off?.kind == .offline && off?.actionTitle == "Retry")
        #expect(off?.text.title == "Offline." && off?.text.message == "Showing your saved agenda from 08:30.")
        let never = HomeLogic.connectionBanner(state: .offline, lastSyncAt: nil, now: t0, format: fmt)
        #expect(never?.text.message == "Nothing is saved on this phone yet.")
    }

    @Test func ageWording() {
        #expect(HomeLogic.ageText(at(8, 30, dayOffset: -3), now: t0, format: fmt) == "from Sat 10 Oct")
    }

    @Test func signIn() {
        let ok = Account(id: "1", email: "a@x", displayName: "Work", color: "cobalt", status: "active")
        let bad = Account(id: "2", email: "b@x", displayName: "Studio", color: "ochre", status: "needs_reauth")
        #expect(HomeLogic.signInBanner([ok]) == nil)
        #expect(HomeLogic.signInBanner([ok, bad]) == .init(title: "Studio", message: "needs you to sign in again. Its events are paused."))
        #expect(HomeLogic.signInBanner([bad, bad])?.title == "2 accounts")
    }
}

// MARK: Fakes (view model)

private let acct = Account(id: "a", email: "me@x.com", displayName: "Me", color: "cobalt", status: "active")

final class FakeAgenda: AgendaRepository, @unchecked Sendable {
    private let lock = NSLock()
    private var all: [AgendaItem]
    private var observers: [(range: (DayKey, DayKey), c: AsyncStream<[AgendaItem]>.Continuation)] = []
    private var nextObservers: [(now: @Sendable () -> Date, c: AsyncStream<AgendaItem?>.Continuation)] = []
    private var dayObservers: [(range: (DayKey, DayKey), c: AsyncStream<Set<DayKey>>.Continuation)] = []
    private var _ranges: [(DayKey, DayKey)] = []
    var ranges: [(DayKey, DayKey)] { lock.withLock { _ranges } }

    init(_ items: [AgendaItem]) { all = items }

    func set(_ items: [AgendaItem]) {
        let (obs, nxt, days) = lock.withLock { () -> ([(range: (DayKey, DayKey), c: AsyncStream<[AgendaItem]>.Continuation)], [(now: @Sendable () -> Date, c: AsyncStream<AgendaItem?>.Continuation)], [(range: (DayKey, DayKey), c: AsyncStream<Set<DayKey>>.Continuation)]) in
            all = items
            return (observers, nextObservers, dayObservers)
        }
        for o in obs { o.c.yield(filter(o.range.0, o.range.1)) }
        for n in nxt { n.c.yield(next(now: n.now())) }
        for d in days { d.c.yield(daySet(d.range.0, d.range.1)) }
    }

    private func filter(_ from: DayKey, _ to: DayKey) -> [AgendaItem] {
        lock.withLock { all }.filter { i in
            let r = i.dayRange(in: utc)
            return r.lowerBound <= to && r.upperBound >= from
        }.sorted { ($0.start ?? .distantPast, $0.id) < ($1.start ?? .distantPast, $1.id) }
    }

    private func next(now: Date) -> AgendaItem? {
        lock.withLock { all }.filter { i in
            guard !i.allDay, let s = i.start, let e = i.end, i.status != "cancelled", i.myResponse != "declined" else { return false }
            return e > now || (e <= s && s >= now)
        }.min { ($0.start!, $0.id) < ($1.start!, $1.id) }
    }

    private func daySet(_ from: DayKey, _ to: DayKey) -> Set<DayKey> {
        var out = Set<DayKey>()
        for i in filter(from, to) where i.status != "cancelled" {
            let r = i.dayRange(in: utc)
            for d in max(r.lowerBound, from).through(min(r.upperBound, to)) { out.insert(d) }
        }
        return out
    }

    func items(from: DayKey, to: DayKey) async throws -> [AgendaItem] { filter(from, to) }
    func observeItems(from: DayKey, to: DayKey) -> AsyncStream<[AgendaItem]> {
        lock.withLock { _ranges.append((from, to)) }
        return AsyncStream { c in
            lock.withLock { observers.append(((from, to), c)) }
            c.yield(filter(from, to))
        }
    }
    func item(id: String) async throws -> AgendaItem? { lock.withLock { all }.first { $0.id == id } }
    func nextEvent(now: Date) async throws -> AgendaItem? { next(now: now) }
    func observeNextEvent(now: @escaping @Sendable () -> Date) -> AsyncStream<AgendaItem?> {
        AsyncStream { c in
            lock.withLock { nextObservers.append((now, c)) }
            c.yield(next(now: now()))
        }
    }
    func daysWithEvents(from: DayKey, to: DayKey) async throws -> Set<DayKey> { daySet(from, to) }
    func observeDaysWithEvents(from: DayKey, to: DayKey) -> AsyncStream<Set<DayKey>> {
        AsyncStream { c in
            lock.withLock { dayObservers.append(((from, to), c)) }
            c.yield(daySet(from, to))
        }
    }
}

struct FakeAccounts: AccountRepository {
    var list: [Account]
    func accounts() async throws -> [Account] { list }
    func observeAccounts() -> AsyncStream<[Account]> { AsyncStream { $0.yield(list) } }
}

struct FakeSettings: SettingsRepository {
    func settings() async throws -> Settings? { Settings(weekStart: 1, userTzid: "UTC") }
    func observeSettings() -> AsyncStream<Settings?> { AsyncStream { $0.yield(Settings(weekStart: 1, userTzid: "UTC")) } }
    func userTimeZone() async throws -> TimeZone { utc }
    func update(_ settings: Settings) async throws -> Settings { settings }
}

struct FakeMeta: SyncMetaRepository {
    var at: Date? = t0
    func lastSyncAt() async throws -> Date? { at }
    func observeLastSyncAt() -> AsyncStream<Date?> { AsyncStream { $0.yield(at) } }
    func lastRev() async throws -> Int64 { 1 }
}

final class FakeSync: SyncControlling, @unchecked Sendable {
    private let lock = NSLock()
    private var _calls: [String] = []
    private var _onManual: (() -> Void)?
    var calls: [String] { lock.withLock { _calls } }
    var onManual: (() -> Void)? {
        get { lock.withLock { _onManual } }
        set { lock.withLock { _onManual = newValue } }
    }
    var state: AsyncStream<SyncState> { AsyncStream { $0.yield(.idle) } }
    func appForeground() async { lock.withLock { _calls.append("fg") } }
    func appBackground() async { lock.withLock { _calls.append("bg") } }
    func manualRefresh() async { lock.withLock { _calls.append("manual") }; onManual?() }
    func silentPush() async -> Bool { false }
}

@MainActor
private func makeVM(items: [AgendaItem], accounts: [Account] = [acct], meta: FakeMeta = FakeMeta(),
                    sync: FakeSync = FakeSync(), agenda: FakeAgenda? = nil) -> (HomeViewModel, FakeAgenda, FakeSync, TestClock) {
    let ag = agenda ?? FakeAgenda(items)
    let clock = TestClock(t0)
    let deps = HomeDependencies(agenda: ag, accounts: FakeAccounts(list: accounts), settings: FakeSettings(), syncMeta: meta,
                                sync: sync, clock: AppClock { clock.now }, sleep: { try await Task.sleep(for: .seconds($0 > 600 ? $0 : 3600)) })
    return (HomeViewModel(deps: deps, tz: utc, locale: gb), ag, sync, clock)
}

@MainActor
private func waitFor(_ cond: @MainActor () -> Bool) async {
    for _ in 0..<600 {
        if cond() { return }
        try? await Task.sleep(for: .milliseconds(5))
    }
}

@MainActor
@Suite("HomeViewModel")
struct HomeViewModelTests {
    @Test func loadsAndShowsUpcomingHero() async {
        let (vm, _, _, _) = makeVM(items: [ev("1", "Standup", at(10), at(10, 30)), ev("2", "Lunch", at(13), at(14))])
        let run = Task { await vm.run() }
        await waitFor { vm.isReady && vm.nextEvent != nil }
        #expect(vm.hero == .upcoming(vm.nextEvent!))
        #expect(vm.sections.first?.title == "Today")
        #expect(vm.showSkeleton == false)
        run.cancel()
    }

    @Test func coldStartShowsSkeletonUntilFirstSync() async {
        let (vm, _, _, _) = makeVM(items: [], accounts: [], meta: FakeMeta(at: nil))
        let run = Task { await vm.run() }
        await waitFor { vm.isReady }
        #expect(vm.showSkeleton)
        run.cancel()
    }

    @Test func noAccountsAfterFirstSync() async {
        let (vm, _, _, _) = makeVM(items: [], accounts: [])
        let run = Task { await vm.run() }
        await waitFor { vm.isReady }
        #expect(vm.hero == .noAccounts && !vm.showSkeleton)
        run.cancel()
    }

    @Test func clockTickMovesUpcomingToLive() async {
        let (vm, _, _, clock) = makeVM(items: [ev("1", "Standup", at(10), at(10, 30))])
        let run = Task { await vm.run() }
        await waitFor { vm.isReady && vm.nextEvent != nil }
        guard case .upcoming = vm.hero else { Issue.record("expected upcoming"); return }
        clock.set(at(10, 5))
        vm.tick()
        guard case .live = vm.hero else { Issue.record("expected live, got \(vm.hero)"); return }
        clock.set(at(10, 31))
        vm.tick()
        await waitFor { vm.nextEvent == nil }
        #expect(vm.hero == .doneForToday(next: nil, hadEvents: true))
        run.cancel()
    }

    @Test func pagesInFourteenDayChunks() async {
        let (vm, ag, _, _) = makeVM(items: [])
        let run = Task { await vm.run() }
        await waitFor { vm.isReady }
        #expect(vm.lastDay == "2026-10-26")
        vm.loadMore()
        #expect(vm.lastDay == "2026-11-09")
        await waitFor { ag.ranges.last?.1 == "2026-11-09" }
        for _ in 0..<10 { vm.loadMore() }
        #expect(vm.lastDay == vm.windowLast && !vm.canLoadMore)
        run.cancel()
    }

    @Test func selectingFarDayWidensRangeAndSetsScrollTarget() async {
        let (vm, _, _, _) = makeVM(items: [])
        let run = Task { await vm.run() }
        await waitFor { vm.isReady }
        vm.toggleMonth()
        #expect(vm.isMonthOpen && vm.monthAnchor == "2026-10-01")
        vm.select("2026-12-01")
        #expect(vm.lastDay == "2026-12-01" && vm.selectedDay == "2026-12-01" && vm.isMonthOpen)
        #expect(vm.scrollTarget == nil)                       // month open: the list below shows that day
        #expect(vm.sections.map(\.day) == ["2026-12-01"] && vm.sections[0].title == "Tue 1 Dec")
        vm.toggleMonth()
        vm.select("2026-12-01")
        #expect(vm.scrollTarget == "2026-12-01")
        vm.select("2026-10-05")
        #expect(vm.firstDay == "2026-10-05" && !vm.isOnToday)
        vm.goToToday()
        #expect(vm.firstDay == "2026-10-13" && vm.selectedDay == "2026-10-13" && vm.scrollTarget == "2026-10-13" && vm.isOnToday)
        run.cancel()
    }

    @Test func pullToRefreshCountsChanges() async {
        let a = ev("1", "A", at(10), at(11))
        let sync = FakeSync()
        let ag = FakeAgenda([a])
        sync.onManual = { ag.set([ev("1", "A", at(10), at(11), version: 2), ev("2", "B", at(12), at(13))]) }
        let (vm, _, _, _) = makeVM(items: [a], sync: sync, agenda: ag)
        let run = Task { await vm.run() }
        await waitFor { vm.isReady }
        await vm.refresh()
        #expect(sync.calls == ["manual"])
        #expect(vm.refreshNote == "2 changes" && !vm.isRefreshing)
        run.cancel()
    }

    @Test func movedStatusAppearsWhenStartChanges() async {
        let ag = FakeAgenda([ev("1", "A", at(10), at(11))])
        let (vm, _, _, _) = makeVM(items: [], agenda: ag)
        let run = Task { await vm.run() }
        await waitFor { vm.isReady && !vm.items.isEmpty }
        ag.set([ev("1", "A", at(15), at(16), version: 2)])
        await waitFor { vm.movedIDs.contains("1") }
        let status = vm.sections.flatMap(\.entries).compactMap { e -> EventRowModel.Status? in
            if case .row(let r) = e { r.model.status } else { nil }
        }
        #expect(status == [.moved])
        run.cancel()
    }

    @Test func sceneChangesDriveStreaming() async {
        let (vm, _, sync, _) = makeVM(items: [])
        await vm.sceneChanged(active: true)
        await vm.sceneChanged(active: false)
        #expect(sync.calls == ["fg", "bg"])
    }

    @Test func bannerWhenAccountNeedsSignIn() async {
        let bad = Account(id: "b", email: "b@x", displayName: "Studio", color: "ochre", status: "needs_reauth")
        let (vm, _, _, _) = makeVM(items: [], accounts: [acct, bad])
        let run = Task { await vm.run() }
        await waitFor { vm.isReady }
        #expect(vm.signInBanner?.title == "Studio")
        #expect(vm.staleAccountIDs == ["b"])
        run.cancel()
    }
}

@Suite("Drag and resize logic")
struct DragLogicTests {
    private let london = TimeZone(identifier: "Europe/London")!
    private func grid(_ day: DayKey = "2026-10-13") -> DayGrid { DayGrid(day: day, tz: TimeZone(identifier: "Europe/London")!, hourHeight: 64) }

    @Test func moveSnapsToFifteenMinutesAndKeepsDuration() {
        let g = grid()
        let r = DragLogic.moved(duration: 3600, topY: 64 * 9.4, grid: g)      // 09:24 -> 09:30
        #expect(g.clockMinutes(r.start) == 9 * 60 + 30)
        #expect(r.end.timeIntervalSince(r.start) == 3600)
    }

    @Test func moveStaysInsideTheDay() {
        let g = grid()
        let late = DragLogic.moved(duration: 3600, topY: 64 * 30, grid: g)
        #expect(late.end <= g.end && late.start >= g.start)
        let early = DragLogic.moved(duration: 3600, topY: -500, grid: g)
        #expect(early.start == g.start)
    }

    @Test func resizeKeepsStartHas15MinMinimumAndStopsBeforeMidnight() {
        let g = grid()
        let start = g.start.addingTimeInterval(10 * 3600)
        #expect(DragLogic.resizedEnd(start: start, bottomY: 64 * 5, grid: g) == start.addingTimeInterval(15 * 60))
        #expect(g.clockMinutes(DragLogic.resizedEnd(start: start, bottomY: 64 * 11.3, grid: g)) == 11 * 60 + 15)
        let endOfDay = DragLogic.resizedEnd(start: start, bottomY: 64 * 40, grid: g)
        #expect(endOfDay == g.end.addingTimeInterval(-15 * 60))
    }

    @Test func dstDaysKeepWallClockSnapping() {
        let g = grid("2026-03-29")           // 23 hour day
        let r = DragLogic.moved(duration: 1800, topY: 64 * 3.2, grid: g)
        #expect(g.clockMinutes(r.start) % 15 == 0)
        #expect(r.end.timeIntervalSince(r.start) == 1800)
    }

    @Test func chips() {
        let g = grid()
        let start = g.start.addingTimeInterval(15 * 3600 + 45 * 60)
        #expect(DragLogic.moveChip(start: start, tz: london, locale: Locale(identifier: "en_GB")) == "Tue 13 \u{00B7} 15:45")
        #expect(DragLogic.resizeChip(start: start, end: start.addingTimeInterval(75 * 60), tz: london, locale: Locale(identifier: "en_GB")) == "1 h 15 min \u{00B7} ends 17:00")
        #expect(DragLogic.resizeChip(start: start, end: start.addingTimeInterval(30 * 60), tz: london, locale: Locale(identifier: "en_GB")) == "30 min \u{00B7} ends 16:15")
    }

    @Test func whoCanBeLifted() {
        let ok = AgendaItem(id: "a", eventId: "a", calendarId: "c", accountId: "a", color: "cobalt", title: "T", allDay: false, canEdit: true)
        #expect(DragLogic.blockReason(ok) == nil)
        var ro = ok; ro.canEdit = false
        #expect(DragLogic.blockReason(ro) == "Read only calendar")
        ro.canRSVP = true
        #expect(DragLogic.blockReason(ro) == "Only the organiser can move it")
        var ad = ok; ad.allDay = true
        #expect(DragLogic.blockReason(ad) != nil)
        #expect(DragLogic.blockReason(ok, continues: true) != nil)
    }
}

@Suite("Invitations")
struct InvitationTests {
    private func inv(_ id: String, start: Date, from: Person? = nil, resp: String = "needsAction", canRSVP: Bool = true) -> AgendaItem {
        var i = ev(id, "Roadmap", start, start.addingTimeInterval(3600), resp: resp, canRSVP: canRSVP)
        i.organizer = from
        return i
    }

    @Test func onlyUpcomingUnansweredInvitationsSoonestFirst() {
        let list = [inv("b", start: at(15)), inv("a", start: at(11)), inv("done", start: at(11), resp: "accepted"),
                    inv("past", start: at(7)), inv("ro", start: at(12), canRSVP: false)]
        #expect(HomeLogic.invitations(list, now: t0).map(\.id) == ["a", "b"])
        #expect(HomeLogic.invitationsBanner(count: 0) == nil)
        #expect(HomeLogic.invitationsBanner(count: 1)?.message == "needs a reply.")
        #expect(HomeLogic.invitationsBanner(count: 2) == .init(title: "2 invitations", message: "need a reply."))
    }

    @Test func rowTextNamesTheOrganiser() {
        let ana = Person(email: "ana@x.com", name: "Ana Ruiz")
        let today = HomeLogic.inviteWhen(inv("a", start: at(19), from: ana), now: t0, tz: utc, format: fmt)
        #expect(today.column == "19:00" && today.sub == "Today \u{00B7} from Ana")
        let later = HomeLogic.inviteWhen(inv("b", start: at(13, dayOffset: 1), from: Person(email: "maya@x.com")), now: t0, tz: utc, format: fmt)
        #expect(later.column == "Wed" && later.sub == "Tomorrow \u{00B7} 13:00 \u{00B7} from maya")
        let mine = HomeLogic.inviteWhen(inv("c", start: at(19), from: Person(email: "me@x.com", isSelf: true)), now: t0, tz: utc, format: fmt)
        #expect(mine.sub == "Today")
    }
}
