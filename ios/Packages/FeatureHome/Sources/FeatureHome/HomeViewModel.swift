import CalendarCore
import CalendarUI
import Foundation
import Observation

/// What Home needs from the app: protocols only, so tests pass fakes.
public struct HomeDependencies: Sendable {
    public var agenda: any AgendaRepository
    public var accounts: any AccountRepository
    public var settings: any SettingsRepository
    public var syncMeta: any SyncMetaRepository
    public var sync: any SyncControlling
    public var clock: AppClock
    public var sleep: @Sendable (TimeInterval) async throws -> Void

    public init(agenda: any AgendaRepository, accounts: any AccountRepository, settings: any SettingsRepository,
                syncMeta: any SyncMetaRepository, sync: any SyncControlling, clock: AppClock = .system,
                sleep: @escaping @Sendable (TimeInterval) async throws -> Void = { try await Task.sleep(for: .seconds($0)) }) {
        self.agenda = agenda; self.accounts = accounts; self.settings = settings; self.syncMeta = syncMeta
        self.sync = sync; self.clock = clock; self.sleep = sleep
    }

    public init(env: AppEnvironment) {
        self.init(agenda: env.agenda, accounts: env.accounts, settings: env.settings, syncMeta: env.syncMeta,
                  sync: env.sync, clock: env.clock)
    }
}

@MainActor @Observable
public final class HomeViewModel {
    public static let chunkDays = 14

    // Inputs mirrored from the repositories.
    public private(set) var now: Date
    public private(set) var tz: TimeZone
    public private(set) var weekStart = 1
    public private(set) var items: [AgendaItem] = []
    public private(set) var nextEvent: AgendaItem?
    public private(set) var accounts: [Account] = []
    public private(set) var syncState: SyncState = .idle
    public private(set) var isOnline = true
    public private(set) var lastSyncAt: Date?
    public private(set) var daysWithEvents: Set<DayKey> = []
    /// Month grid dots: account colours with events that day (max 3).
    public private(set) var dayDots: [DayKey: [AccountColor]] = [:]
    public private(set) var movedIDs: Set<String> = []

    // Load flags (avoid flashing the wrong empty state before the DB has answered).
    public private(set) var itemsLoaded = false
    public private(set) var accountsLoaded = false
    public private(set) var metaLoaded = false

    // UI state.
    public var isMonthOpen = false
    public private(set) var monthAnchor: DayKey
    public private(set) var selectedDay: DayKey
    public private(set) var isRefreshing = false
    public private(set) var refreshNote: String?
    /// Set by `select`/`goToToday`; the view scrolls there and clears it.
    public var scrollTarget: DayKey?

    private let deps: HomeDependencies
    private var pastFirst: DayKey?       // earliest day the user jumped to (before today)
    private var loadedThrough: DayKey
    private var seenStarts: [String: Date] = [:]
    private var itemsTask: Task<Void, Never>?
    private var nextTask: Task<Void, Never>?
    private var monthTask: Task<Void, Never>?
    private var dotsTask: Task<Void, Never>?
    private var noteTask: Task<Void, Never>?
    public private(set) var format: HomeFormat
    /// On-device display preferences (declined events, week numbers, 24-hour clock); see `apply(prefs:)`.
    public private(set) var prefs = DisplayPrefs()
    private let baseLocale: Locale

    public init(deps: HomeDependencies, tz: TimeZone = .current, locale: Locale = .current) {
        self.deps = deps
        baseLocale = locale
        let n = deps.clock.now()
        now = n; self.tz = tz
        format = HomeFormat(tz: tz, locale: locale)
        let t = DayKey(n, in: tz)
        selectedDay = t; monthAnchor = MonthGrid.firstOfMonth(t)
        loadedThrough = t.adding(days: Self.chunkDays - 1)
    }

    // MARK: Derived

    public var today: DayKey { DayKey(now, in: tz) }
    public var firstDay: DayKey { min(today, pastFirst ?? today) }
    public var lastDay: DayKey { max(loadedThrough, today) }
    /// Last day the device keeps (sync window).
    public var windowLast: DayKey { today.adding(days: SyncWindow.daysAfter) }
    public var windowFirst: DayKey { today.adding(days: -SyncWindow.daysBefore) }

    public var neverSynced: Bool { metaLoaded && lastSyncAt == nil }
    public var syncUnreachable: Bool {
        switch syncState { case .offline, .error: true; default: false }
    }

    /// First launch with an empty database: skeleton rows while the first sync runs.
    public var showSkeleton: Bool {
        guard itemsLoaded, accountsLoaded, metaLoaded else { return false }
        guard items.isEmpty, accounts.isEmpty, lastSyncAt == nil else { return false }
        return !syncUnreachable
    }

    /// Set once the first rows have eased in; later loads do not stagger.

    public var isReady: Bool { itemsLoaded && accountsLoaded && metaLoaded }

    public var hero: HomeHero {
        HomeLogic.hero(accountCount: accounts.count, neverSynced: neverSynced, syncUnreachable: syncUnreachable,
                       next: nextEvent, items: items, now: now, tz: tz)
    }

    public var heroBlock: HomeLogic.HeroBlock? {
        switch hero {
        case .upcoming(let i): HomeLogic.block(for: i, live: false, now: now, format: format)
        case .live(let i): HomeLogic.block(for: i, live: true, now: now, format: format)
        default: nil
        }
    }

    /// "Next: Standup, tomorrow 09:00" / "Next: Design review, Thu 14:00".
    public func doneDetail(next: AgendaItem?) -> String? {
        guard let next, let s = next.start else { return nil }
        let d = DayKey(s, in: tz)
        let gap = today.days(to: d)
        let when = gap == 1 ? "tomorrow" : gap <= 6 ? String(format.shortDate(d).split(separator: " ").first ?? "") : format.dayMonth(d)
        return "Next: \(next.title), \(when) \(format.time(s))"
    }

    /// "Clear until Thursday" when nothing is left and the next event is on another day.
    public func clearUntil(next: AgendaItem?) -> String? {
        guard let s = next?.start else { return nil }
        let d = DayKey(s, in: tz)
        return today.days(to: d) <= 1 ? "Clear until tomorrow" : "Clear until \(format.weekday(d))"
    }

    /// With the month open the list is that day only, labelled "Tue 13 Oct" (canvas h-month).
    public var sections: [HomeLogic.DaySection] {
        // h-live: the live event is the Next block; it is not repeated in the list.
        var listed = prefs.visible(items)
        if case .live(let live) = hero { listed.removeAll { $0.id == live.id } }
        let all = HomeLogic.sections(items: listed, now: now, tz: tz, firstDay: firstDay, lastDay: lastDay,
                                     alwaysShow: [today, selectedDay], moved: movedIDs, format: format,
                                     staleAccounts: staleAccountIDs)
        guard isMonthOpen else { return all }
        return all.filter { $0.day == selectedDay }.map { sec in
            var s = sec
            if !s.isEarlier { s.title = format.shortDate(s.day) }
            return s
        }
    }

    public var staleAccountIDs: Set<String> {
        if showingSavedCopy { return Set(accounts.map(\.id)) }
        return Set(accounts.filter { $0.statusKind == .needsReauth }.map(\.id))
    }
    public var invitations: [AgendaItem] { HomeLogic.invitations(items, now: now) }
    public var invitationsBanner: HomeLogic.BannerText? { HomeLogic.invitationsBanner(count: invitations.count) }
    public func inviteWhen(_ i: AgendaItem) -> (column: String, sub: String) { HomeLogic.inviteWhen(i, now: now, tz: tz, format: format) }

    public var signInBanner: HomeLogic.BannerText? { HomeLogic.signInBanner(accounts) }
    public var errorBanner: HomeLogic.BannerText? { HomeLogic.errorBanner(accounts) }
    public var connectionBanner: HomeLogic.ConnectionBanner? {
        HomeLogic.connectionBanner(state: syncState, lastSyncAt: lastSyncAt, isOnline: isOnline, now: now, format: format)
    }
    /// Saved data is on screen but the hub can't be reached: rows read "Stale", the Next block is the saved-copy card.
    public var showingSavedCopy: Bool { connectionBanner != nil && lastSyncAt != nil }
    /// Accounts still syncing, for the "Syncing 3 accounts / 2 of 3" row.
    public var syncingProgress: (total: Int, done: Int)? {
        let syncing = accounts.filter { $0.statusKind == .syncing }.count
        return syncing > 0 ? (accounts.count, accounts.count - syncing) : nil
    }
    /// The hub could not be reached (or answered badly): never claim "Nothing planned" in that case.
    public var loadFailed: Bool {
        switch syncState { case .offline, .error, .stale: true; default: false }
    }
    public var todayNumber: Int { Int(today.rawValue.suffix(2)) ?? 1 }
    public var weekdayNames: [String] { MonthGrid.weekdayNames(weekStart: weekStart) }
    /// Accounts for the month legend, in list order.
    public var legend: [(color: AccountColor, name: String)] {
        accounts.map { (AccountColor(key: $0.color), $0.displayName.isEmpty ? $0.email : $0.displayName) }
    }
    public var showProgress: Bool { isRefreshing || syncState == .syncing }
    public var canLoadMore: Bool { loadedThrough < windowLast }

    public var titleMonth: String { format.month(selectedDay) }
    public var titleYear: String { format.year(selectedDay) }
    public var gridMonthTitle: String { format.month(monthAnchor) }
    public var gridYear: String { format.year(monthAnchor) }
    public var gridCells: [DayKey] { MonthGrid.cells(month: monthAnchor, weekStart: weekStart) }
    public var isOnToday: Bool { selectedDay == today && pastFirst == nil }

    // MARK: Lifecycle

    /// Runs until cancelled (attach with `.task`). Subscribes to every repository and ticks every minute.
    public func run() async {
        if let z = try? await deps.settings.userTimeZone() { setTimeZone(z) }
        resubscribeItems()
        resubscribeMonth()
        let settingsS = deps.settings.observeSettings()
        let accountsS = deps.accounts.observeAccounts()
        let syncS = deps.sync.state
        let metaS = deps.syncMeta.observeLastSyncAt()
        let tasks: [Task<Void, Never>] = [
            Task { for await s in settingsS { self.apply(settings: s) } },
            Task { for await a in accountsS { self.accounts = a; self.accountsLoaded = true } },
            Task { for await s in syncS { self.syncState = s } },
            Task { for await o in NetworkMonitor.shared.updates { self.isOnline = o } },
            Task { for await d in metaS { self.lastSyncAt = d; self.metaLoaded = true } },
            Task { await self.clockLoop() },
        ]
        await withTaskCancellationHandler {
            for t in tasks { await t.value }
        } onCancel: {
            for t in tasks { t.cancel() }
        }
        itemsTask?.cancel(); nextTask?.cancel(); monthTask?.cancel(); dotsTask?.cancel(); noteTask?.cancel()
    }

    private func clockLoop() async {
        while !Task.isCancelled {
            let t = deps.clock.now().timeIntervalSince1970
            let wait = max(1, 60 - t.truncatingRemainder(dividingBy: 60))
            do { try await deps.sleep(wait) } catch { return }
            tick()
        }
    }

    /// Advances the clock (called every minute; tests call it directly).
    public func tick() {
        let before = today
        now = deps.clock.now()
        if today != before {
            // Midnight: the range slides forward and today's section changes.
            if selectedDay == before { selectedDay = today }
            loadedThrough = max(loadedThrough, today.adding(days: Self.chunkDays - 1))
            resubscribeItems()
        }
        refreshNext()
    }

    private func refreshNext() {
        nextTask?.cancel()
        nextTask = Task { [weak self] in
            guard let self else { return }
            let n = try? await deps.agenda.nextEvent(now: deps.clock.now())
            if !Task.isCancelled { nextEvent = n }
        }
    }

    /// Settings > Appearance changed: re-filter the list and re-render times in the chosen clock.
    public func apply(prefs p: DisplayPrefs) {
        guard p != prefs else { return }
        prefs = p
        format = HomeFormat(tz: tz, locale: p.locale(base: baseLocale))
    }

    /// ISO week number for the month grid's week column, nil when the preference is off.
    public func weekNumber(row: Int) -> Int? {
        guard prefs.showWeekNumbers, let d = gridCells[safe: row * 7] else { return nil }
        // ISO weeks start on Monday: use the Thursday of the row for Sunday-first grids.
        return DisplayPrefs.weekNumber(of: weekStart == 0 ? d.adding(days: 1) : d)
    }

    func apply(settings s: Settings?) {
        weekStart = s?.weekStart ?? 1
        if let id = s?.userTzid, let z = TimeZone(identifier: id) { setTimeZone(z) }
    }

    private func setTimeZone(_ z: TimeZone) {
        guard z.identifier != tz.identifier else { return }
        let wasToday = selectedDay == today
        tz = z
        format = HomeFormat(tz: z, locale: format.locale)
        if wasToday { selectedDay = today }
        loadedThrough = max(loadedThrough, today.adding(days: Self.chunkDays - 1))
        resubscribeItems()
    }

    private func resubscribeItems() {
        itemsTask?.cancel(); nextTask?.cancel()
        let from = firstDay, to = lastDay
        itemsTask = Task { [weak self] in
            guard let self else { return }
            for await list in deps.agenda.observeItems(from: from, to: to) {
                if Task.isCancelled { return }
                ingest(list)
            }
        }
        let clock = deps.clock
        nextTask = Task { [weak self] in
            guard let self else { return }
            for await n in deps.agenda.observeNextEvent(now: { clock.now() }) {
                if Task.isCancelled { return }
                nextEvent = n
            }
        }
    }

    private func ingest(_ list: [AgendaItem]) {
        movedIDs.formUnion(HomeLogic.movedIDs(seen: seenStarts, items: list))
        for i in list { if let s = i.start { seenStarts[i.id] = s } }
        items = list
        itemsLoaded = true
    }

    private func resubscribeMonth() {
        monthTask?.cancel()
        let cells = gridCells
        guard let first = cells.first, let last = cells.last else { return }
        monthTask = Task { [weak self] in
            guard let self else { return }
            for await d in deps.agenda.observeDaysWithEvents(from: first, to: last) {
                if Task.isCancelled { return }
                daysWithEvents = d
            }
        }
        dotsTask?.cancel()
        dotsTask = Task { [weak self] in
            guard let self else { return }
            for await d in deps.agenda.observeDayAccounts(from: first, to: last) {
                if Task.isCancelled { return }
                dayDots = d.mapValues { $0.map { AccountColor(key: $0) } }
            }
        }
    }

    // MARK: Intents

    public func sceneChanged(active: Bool) async {
        if active { await deps.sync.appForeground() } else { await deps.sync.appBackground() }
    }

    /// Pull to refresh: hub poll + re-read, then report how many events changed.
    public func refresh() async {
        guard !isRefreshing else { return }
        let before = Dictionary(items.map { ($0.id, $0.version) }, uniquingKeysWith: { a, _ in a })
        isRefreshing = true
        noteTask?.cancel(); refreshNote = nil
        await deps.sync.manualRefresh()
        let after = (try? await deps.agenda.items(from: firstDay, to: lastDay)) ?? items
        isRefreshing = false
        refreshNote = HomeLogic.changeText(HomeLogic.changeCount(before: before, after: after))
        noteTask = Task { [weak self] in
            guard let self else { return }
            try? await deps.sleep(3)
            if !Task.isCancelled { refreshNote = nil }
        }
    }

    public func loadMore() {
        guard canLoadMore else { return }
        loadedThrough = min(loadedThrough.adding(days: Self.chunkDays), windowLast)
        resubscribeItems()
    }

    public func toggleMonth() {
        if !isMonthOpen { monthAnchor = MonthGrid.firstOfMonth(selectedDay); resubscribeMonth() }
        isMonthOpen.toggle()
    }

    public func shiftMonth(_ delta: Int) {
        monthAnchor = MonthGrid.adding(months: delta, to: monthAnchor)
        resubscribeMonth()
    }

    /// Month grid pick: widen the loaded range if needed, scroll the list to that day.
    public func select(_ day: DayKey) {
        let day = min(max(day, windowFirst), windowLast)
        selectedDay = day
        var changed = false
        if day > lastDay { loadedThrough = day; changed = true }
        if day < today, pastFirst.map({ day < $0 }) ?? true { pastFirst = day; changed = true }
        if changed { resubscribeItems() }
        if !isMonthOpen { scrollTarget = day }   // month open: the list below the grid shows this day
    }

    public func goToToday() {
        let hadPast = pastFirst != nil
        pastFirst = nil
        selectedDay = today
        monthAnchor = MonthGrid.firstOfMonth(today)
        if hadPast { resubscribeItems() }
        resubscribeMonth()
        scrollTarget = today
        isMonthOpen = false
    }
}

private extension Array {
    subscript(safe i: Int) -> Element? { indices.contains(i) ? self[i] : nil }
}
