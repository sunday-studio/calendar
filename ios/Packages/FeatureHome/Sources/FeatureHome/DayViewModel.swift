import CalendarCore
import Foundation
import Observation

/// Data for the Day view: one observation over the whole device window, laid out per day once per change.
@MainActor @Observable
public final class DayViewModel {
    public private(set) var tz: TimeZone
    public private(set) var secondaryTz: TimeZone?
    public private(set) var weekStart = 1
    public private(set) var contents: [DayKey: DayContent] = [:]
    public private(set) var loaded = false
    /// Updated at most every 5 minutes: fades finished blocks without re-laying-out the grid each minute.
    public private(set) var coarseNow: Date
    /// Minute-accurate clock for the now line.
    public private(set) var now: Date
    public private(set) var today: DayKey
    public private(set) var format: HomeFormat
    public private(set) var prefs = DisplayPrefs()
    private let baseLocale: Locale

    private let agenda: any AgendaRepository
    private let settings: any SettingsRepository
    private let clock: AppClock
    private let sleep: @Sendable (TimeInterval) async throws -> Void
    private var rawItems: [AgendaItem] = []
    private var itemsTask: Task<Void, Never>?

    public init(agenda: any AgendaRepository, settings: any SettingsRepository, clock: AppClock = .system,
                tz: TimeZone = .current, locale: Locale = .current,
                sleep: @escaping @Sendable (TimeInterval) async throws -> Void = { try await Task.sleep(for: .seconds($0)) }) {
        self.agenda = agenda; self.settings = settings; self.clock = clock; self.sleep = sleep
        self.tz = tz
        baseLocale = locale
        format = HomeFormat(tz: tz, locale: locale)
        let n = clock.now()
        coarseNow = n; now = n; today = DayKey(n, in: tz)
    }

    public var windowFirst: DayKey { today.adding(days: -SyncWindow.daysBefore) }
    public var windowLast: DayKey { today.adding(days: SyncWindow.daysAfter) }

    public func content(for day: DayKey) -> DayContent { contents[day] ?? .empty }

    // MARK: Lifecycle

    public func run() async {
        if let z = try? await settings.userTimeZone() { setTimeZone(z) }
        resubscribe()
        let s = settings.observeSettings()
        let settingsTask = Task { for await v in s { self.apply(settings: v) } }
        let tick = Task { await self.clockLoop() }
        await withTaskCancellationHandler {
            await settingsTask.value; await tick.value
        } onCancel: {
            settingsTask.cancel(); tick.cancel()
        }
        itemsTask?.cancel()
    }

    /// Settings > Appearance changed: declined events and the clock.
    public func apply(prefs p: DisplayPrefs) {
        guard p != prefs else { return }
        prefs = p
        format = HomeFormat(tz: tz, locale: p.locale(base: baseLocale))
        if loaded { ingest(rawItems) }
    }

    func apply(settings s: Settings?) {
        weekStart = s?.weekStart ?? 1
        secondaryTz = s?.secondaryTzid.flatMap { $0.isEmpty ? nil : TimeZone(identifier: $0) }
        if let id = s?.userTzid, let z = TimeZone(identifier: id) { setTimeZone(z) }
    }

    private func setTimeZone(_ z: TimeZone) {
        guard z.identifier != tz.identifier else { return }
        tz = z
        format = HomeFormat(tz: z, locale: format.locale)
        today = DayKey(clock.now(), in: z)
        resubscribe()
    }

    private func resubscribe() {
        itemsTask?.cancel()
        let from = windowFirst, to = windowLast
        itemsTask = Task { [weak self] in
            guard let self else { return }
            for await list in agenda.observeItems(from: from, to: to) {
                if Task.isCancelled { return }
                ingest(list)
            }
        }
    }

    func ingest(_ list: [AgendaItem]) {
        rawItems = list
        contents = DayLogic.contents(items: prefs.visible(list), from: windowFirst, to: windowLast, tz: tz)
        loaded = true
    }

    private func clockLoop() async {
        while !Task.isCancelled {
            let t = clock.now().timeIntervalSince1970
            do { try await sleep(max(1, 60 - t.truncatingRemainder(dividingBy: 60))) } catch { return }
            tick()
        }
    }

    public func tick() {
        let n = clock.now()
        now = n
        if n.timeIntervalSince(coarseNow) >= 5 * 60 { coarseNow = n }
        let t = DayKey(n, in: tz)
        if t != today {
            today = t; coarseNow = n
            resubscribe()
        }
    }
}
