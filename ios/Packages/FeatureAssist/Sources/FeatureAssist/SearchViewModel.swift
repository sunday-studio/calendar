import CalendarCore
import Foundation
import Observation

/// Last searches, kept on this phone only (canvas f-open "Recent").
public struct RecentSearches: Sendable {
    public static let limit = 8
    private let load: @Sendable () -> [String]
    private let save: @Sendable ([String]) -> Void

    public init(load: @escaping @Sendable () -> [String], save: @escaping @Sendable ([String]) -> Void) { self.load = load; self.save = save }

    public static func defaults(_ key: String = "search.recent") -> RecentSearches {
        RecentSearches(load: { UserDefaults.standard.stringArray(forKey: key) ?? [] }, save: { UserDefaults.standard.set($0, forKey: key) })
    }

    public var items: [String] { load() }

    public func add(_ term: String) {
        let t = term.trimmingCharacters(in: .whitespacesAndNewlines)
        guard t.count >= 2 else { return }
        var all = load().filter { $0.caseInsensitiveCompare(t) != .orderedSame }
        all.insert(t, at: 0)
        save(Array(all.prefix(Self.limit)))
    }

    public func remove(_ term: String) { save(load().filter { $0 != term }) }
    public func clear() { save([]) }
}

/// What search needs from the hub; `APIClient` provides both halves.
public typealias SearchAPI = HubReadAPI & HubAssistAPI

/// Search (canvas f-open, f-results, f-empty). Results update 250 ms after the last keystroke; the hub matches word
/// prefixes (FTS5), so "sam" finds "Samuel". Offline, the saved agenda is searched instead.
@MainActor
@Observable
public final class SearchViewModel {
    public enum Phase: Equatable, Sendable {
        case idle
        case searching
        case results
        case failed(String)
    }

    public struct Result: Identifiable, Equatable, Sendable {
        public var id: String
        public var title: String
        public var dateWords: String      // "Today", "Fri 16", "Oct 2"
        public var sub: String            // "12:30 \u{00B7} K\u{014D}ya"
        public var color: String
        public var isPast: Bool
        public var start: Date
    }

    public var query = "" { didSet { if query != oldValue { queryChanged() } } }
    public private(set) var phase: Phase = .idle
    public private(set) var people: [PersonHit] = []
    public private(set) var upcoming: [Result] = []
    public private(set) var past: [Result] = []
    /// True when the answer came from the saved agenda because the hub could not be reached.
    public private(set) var savedOnly = false
    public private(set) var recents: [String]
    public private(set) var searchedQuery = ""

    public let tz: TimeZone
    @ObservationIgnored private let api: any SearchAPI
    @ObservationIgnored private let local: @Sendable (String) async -> [AgendaItem]
    @ObservationIgnored private let store: RecentSearches
    @ObservationIgnored private let clock: AppClock
    @ObservationIgnored private let debounce: Duration
    @ObservationIgnored private let sleep: @Sendable (Duration) async throws -> Void
    @ObservationIgnored private var task: Task<Void, Never>?
    @ObservationIgnored public private(set) var requestCount = 0

    public init(api: any SearchAPI, tz: TimeZone, clock: AppClock = .system, recents: RecentSearches = .defaults(),
                local: @escaping @Sendable (String) async -> [AgendaItem] = { _ in [] },
                debounce: Duration = .milliseconds(250),
                sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }) {
        self.api = api; self.tz = tz; self.clock = clock; self.store = recents; self.local = local
        self.debounce = debounce; self.sleep = sleep; self.recents = recents.items
    }

    public var trimmed: String { query.trimmingCharacters(in: .whitespacesAndNewlines) }
    public var isEmptyResult: Bool { phase == .results && people.isEmpty && upcoming.isEmpty && past.isEmpty }
    public var emptyMessage: String { "No events or people match \u{201C}\(searchedQuery)\u{201D}." }

    private func queryChanged() {
        task?.cancel()
        guard trimmed.count >= 1 else {
            phase = .idle; people = []; upcoming = []; past = []; savedOnly = false
            return
        }
        phase = .searching
        let q = trimmed
        task = Task { [weak self, debounce, sleep] in
            do { try await sleep(debounce) } catch { return }
            guard !Task.isCancelled else { return }
            await self?.run(q)
        }
    }

    public func settle() async { await task?.value }

    /// Keyboard "search": run now, remember the term.
    public func submit() async {
        task?.cancel()
        guard !trimmed.isEmpty else { return }
        await run(trimmed)
        remember()
    }

    public func fill(_ term: String) { query = term }

    public func remember() { store.add(trimmed); recents = store.items }
    public func removeRecent(_ term: String) { store.remove(term); recents = store.items }

    private func run(_ q: String) async {
        requestCount += 1
        do {
            async let events = api.events(q: q, from: nil, to: nil, calendar: nil, cursor: nil, limit: 50)
            async let ppl = (try? await api.people(q: q)) ?? []
            let (page, hits) = try await (events, ppl)
            guard !Task.isCancelled, q == trimmed else { return }
            savedOnly = false
            people = hits
            group(page.items.map(\.item), query: q)
        } catch is CancellationError {
            return
        } catch {
            guard !Task.isCancelled, q == trimmed else { return }
            switch error {
            case APIClientError.transport, APIClientError.tls, APIClientError.server(503, _):
                let rows = await local(q)
                guard !Task.isCancelled, q == trimmed else { return }
                savedOnly = true; people = []
                group(rows, query: q)
            case APIClientError.unauthorized: phase = .failed("The server no longer accepts this phone\u{2019}s token.")
            default: phase = .failed("The server returned something unexpected.")
            }
        }
    }

    private func group(_ items: [AgendaItem], query q: String) {
        let now = clock.now()
        let rows = items.map { result($0, now: now) }
        upcoming = rows.filter { !$0.isPast }.sorted { $0.start < $1.start }
        past = rows.filter(\.isPast).sorted { $0.start > $1.start }
        searchedQuery = q
        phase = .results
    }

    func result(_ i: AgendaItem, now: Date) -> Result {
        let start: Date
        var timeWords: String
        let time = DraftFormat.formatter("jmm", tz, DraftFormat.displayLocale)
        if i.allDay {
            start = i.date.map { DayKey($0).startOfDay(in: tz) } ?? now
            timeWords = "All day"
        } else {
            start = i.start ?? now
            timeWords = time.string(from: start)
        }
        let end = i.allDay ? (i.endDate.map { DayKey($0).startOfDay(in: tz) } ?? start) : (i.end ?? start)
        if let loc = i.location, !loc.isEmpty { timeWords += " \u{00B7} \(loc)" }
        return Result(id: i.id, title: i.title, dateWords: dateWords(start, now: now), sub: timeWords, color: i.color,
                      isPast: end <= now, start: start)
    }

    func dateWords(_ d: Date, now: Date) -> String {
        let key = DayKey(d, in: tz), today = DayKey(now, in: tz)
        if key == today { return "Today" }
        if key == today.adding(days: 1) { return "Tomorrow" }
        let sameYearWindow = abs(today.days(to: key)) < 60
        let f = DraftFormat.formatter(sameYearWindow && key > today ? "EEEd" : "MMMd", tz, .current)
        return f.string(from: d)
    }
}
