import CalendarCore
import CalendarUI
import Foundation
import Observation

/// Pure text helpers for drafts (no SwiftUI).
public enum DraftFormat {
    /// The locale every Assist surface formats with: the device locale with the clock forced to the
    /// "24 hour time" preference (default on), so output never depends on the device's region setting.
    public static var displayLocale: Locale { DisplayPrefs.load().locale() }

    public static func formatter(_ template: String, _ tz: TimeZone, _ locale: Locale) -> DateFormatter {
        let f = DateFormatter()
        f.locale = locale; f.timeZone = tz
        if template.contains("j") {
            // `j` follows the region, not the locale's hourCycle override, so spell the clock out.
            let hc = locale.hourCycle
            let is24 = hc == nil || hc == .zeroToTwentyThree || hc == .oneToTwentyFour
            f.dateFormat = is24 ? "HH:mm" : "h:mm a"
        } else {
            f.setLocalizedDateFormatFromTemplate(template)
        }
        return f
    }

    /// "Fri 16 Oct \u{00B7} 13:00 to 14:00", "Fri 16 Oct \u{00B7} All day", nil when the draft has no time yet.
    public static func when(_ w: EventWhen, tz: TimeZone, locale: Locale = DraftFormat.displayLocale) -> String? {
        let day = formatter("EEEdMMM", tz, locale), time = formatter("jmm", tz, locale)
        if w.allDay {
            guard let d = w.date, let k = Optional(DayKey(d)) else { return nil }
            return "\(day.string(from: k.startOfDay(in: tz).addingTimeInterval(12 * 3600))) \u{00B7} All day"
        }
        guard let s = w.startDate() else { return nil }
        let e = w.endInstant() ?? s
        let zone = w.tzid.flatMap(TimeZone.init(identifier:)) ?? tz
        let dayF = formatter("EEEdMMM", zone, locale), timeF = formatter("jmm", zone, locale)
        return "\(dayF.string(from: s)) \u{00B7} \(timeF.string(from: s)) to \(timeF.string(from: e))"
    }

    /// "Fri 13:00" for the Created toast.
    public static func short(_ w: EventWhen, tz: TimeZone, locale: Locale = DraftFormat.displayLocale) -> String {
        if w.allDay, let d = w.date {
            return formatter("EEEdMMM", tz, locale).string(from: DayKey(d).startOfDay(in: tz).addingTimeInterval(12 * 3600))
        }
        guard let s = w.startDate() else { return "" }
        let zone = w.tzid.flatMap(TimeZone.init(identifier:)) ?? tz
        return "\(formatter("EEE", zone, locale).string(from: s)) \(formatter("jmm", zone, locale).string(from: s))"
    }

    /// UTF-16 spans from the hub -> string ranges, dropping any that do not fit `text`.
    public static func underlines(_ text: String, spans: [DraftSpan]) -> [(range: Range<String.Index>, kind: UnderlineSpan.Kind)] {
        let u = text.utf16
        var out: [(Range<String.Index>, UnderlineSpan.Kind)] = []
        for s in spans {
            guard s.from >= 0, s.to > s.from, s.to <= u.count,
                  let lo = u.index(u.startIndex, offsetBy: s.from, limitedBy: u.endIndex),
                  let hi = u.index(u.startIndex, offsetBy: s.to, limitedBy: u.endIndex),
                  let a = lo.samePosition(in: text), let b = hi.samePosition(in: text) else { continue }
            let kind: UnderlineSpan.Kind = s.kind == "who" ? .who : s.kind == "where" ? .place : .when
            out.append((a..<b, kind))
        }
        return out
    }
}

/// Quick add (canvas q-type ... q-down): text -> debounced hub draft -> choices -> create.
/// Nothing is created until `create()`; everything the screens show comes from the hub's draft.
@MainActor
@Observable
public final class QuickAddViewModel {
    public enum Stage: Equatable, Sendable {
        case entry                 // the new-event sheet: field + rows
        case preview               // "I understood"
        case ambiguity             // "Two things to check."
        case creating
        case created
        case unreachable           // alfather isn't answering (draft.degraded)
    }

    public enum Parse: Equatable, Sendable {
        case empty
        case parsing
        case ready(QuickAddDraft)
        /// Phone offline / hub unreachable: nothing to preview, Create disabled.
        case offline
        case failed(String)
    }

    public var text = "" { didSet { if text != oldValue { textChanged() } } }
    public private(set) var stage: Stage = .entry
    public private(set) var parse: Parse = .empty
    /// The text the current draft was parsed from (spans only line up with this).
    public private(set) var draftText = ""
    /// field -> chosen value (option value, or "HH:mm" / "yyyy-MM-dd" from a picker).
    public private(set) var answers: [String: String] = [:]
    public private(set) var createError: String?
    public private(set) var created: EventDetail?

    public let tz: TimeZone
    @ObservationIgnored private let api: any HubAssistAPI
    @ObservationIgnored private let writer: any EventWriting
    @ObservationIgnored private let clock: AppClock
    @ObservationIgnored private let debounce: Duration
    @ObservationIgnored private let sleep: @Sendable (Duration) async throws -> Void
    @ObservationIgnored private var parseTask: Task<Void, Never>?
    @ObservationIgnored private var attemptKey: String?
    /// Counts API calls (tests assert debounce / cancel).
    @ObservationIgnored public private(set) var requestCount = 0

    public init(api: any HubAssistAPI, writer: any EventWriting, tz: TimeZone, clock: AppClock = .system,
                debounce: Duration = .milliseconds(400),
                sleep: @escaping @Sendable (Duration) async throws -> Void = { try await Task.sleep(for: $0) }) {
        self.api = api; self.writer = writer; self.tz = tz; self.clock = clock; self.debounce = debounce; self.sleep = sleep
    }

    // MARK: Live parse

    private func textChanged() {
        parseTask?.cancel()
        attemptKey = nil
        let t = trimmed
        guard !t.isEmpty else { parse = .empty; answers = [:]; return }
        if case .ready = parse {} else { parse = .parsing }
        parseTask = Task { [weak self, debounce, sleep] in
            do { try await sleep(debounce) } catch { return }
            guard !Task.isCancelled else { return }
            await self?.runParse(t)
        }
    }

    /// Awaits the pending debounced parse (tests, and "Go" from the keyboard).
    public func settle() async { await parseTask?.value }

    private var trimmed: String { text.trimmingCharacters(in: .whitespacesAndNewlines) }

    private func runParse(_ t: String) async {
        requestCount += 1
        do {
            let d = try await api.quickAdd(text: t, now: clock.now(), tzid: tz.identifier)
            // A newer keystroke superseded this answer.
            guard !Task.isCancelled, t == trimmed else { return }
            apply(d, for: t)
        } catch is CancellationError {
            return
        } catch {
            guard !Task.isCancelled, t == trimmed else { return }
            switch error {
            case APIClientError.transport, APIClientError.tls: parse = .offline
            case APIClientError.server(_, let b?) where b.code == "validation": parse = .failed(b.message)
            case APIClientError.unauthorized: parse = .failed("The server no longer accepts this phone\u{2019}s token.")
            default: parse = .failed("The server returned something unexpected.")
            }
        }
    }

    private func apply(_ d: QuickAddDraft, for t: String) {
        parse = .ready(d); draftText = t
        // Keep answers still valid for this draft; seed suggestions only where the hub marks a default.
        var next: [String: String] = [:]
        for a in d.ambiguities {
            if let old = answers[a.field], a.options.isEmpty || a.options.contains(where: { $0.value == old }) { next[a.field] = old }
            else if a.default >= 0, a.default < a.options.count { next[a.field] = a.options[a.default].value }
        }
        answers = next
    }

    public var draft: QuickAddDraft? { if case .ready(let d) = parse { d } else { nil } }
    public var isParsing: Bool { parse == .parsing }
    public var isOffline: Bool { parse == .offline }
    public var canGo: Bool { !trimmed.isEmpty }
    public var underlines: [(range: Range<String.Index>, kind: UnderlineSpan.Kind)] {
        guard let d = draft, draftText == text.trimmingCharacters(in: .whitespacesAndNewlines) else { return [] }
        return DraftFormat.underlines(text, spans: d.spans)
    }

    // MARK: Moving between screens

    /// "Go" / "Done": parse now (skipping the debounce), then show the right screen.
    public func submit() async {
        parseTask?.cancel()
        let t = trimmed
        guard !t.isEmpty else { return }
        if draft == nil || draftText != t {
            parse = .parsing
            await runParse(t)
        }
        route()
    }

    /// After `submit()`: unreachable when the hub says alfather is degraded, asks when it must, else the preview.
    private func route() {
        guard let d = draft else { return }   // offline / failed: stay where we are, the banner explains
        if d.degraded == true { stage = .unreachable }
        else if !requiredQuestions.isEmpty { stage = .ambiguity }
        else { stage = .preview }
    }

    public func backToEntry() { stage = .entry; createError = nil }

    /// "Try again" on the unreachable screen.
    public func retry() async {
        parseTask?.cancel()
        let t = trimmed
        guard !t.isEmpty else { return }
        parse = .parsing
        await runParse(t)
        route()
    }

    // MARK: Ambiguities

    /// Questions that must be answered before Continue (everything not optional).
    public var requiredQuestions: [DraftAmbiguity] { draft?.ambiguities.filter { !$0.optional } ?? [] }
    public var optionalQuestions: [DraftAmbiguity] { draft?.ambiguities.filter(\.optional) ?? [] }
    public var unanswered: [DraftAmbiguity] { requiredQuestions.filter { answers[$0.field] == nil } }
    public var canContinue: Bool { draft != nil && unanswered.isEmpty }

    public func answer(_ field: String, _ value: String?) {
        answers[field] = value
        attemptKey = nil
    }

    public func answerLabel(for q: DraftAmbiguity) -> String? {
        guard let v = answers[q.field] else { return nil }
        return q.options.first { $0.value == v }?.label ?? v
    }

    public func continueAfterQuestions() {
        guard canContinue else { return }
        stage = .preview
    }

    // MARK: Resolved draft

    /// The hub's event with every answer applied (see api `Draft` description). Nil until there is a draft.
    public var resolved: EventDraft? {
        guard let d = draft else { return nil }
        return Self.resolve(d, answers: answers, tz: tz)
    }

    public nonisolated static func resolve(_ d: QuickAddDraft, answers: [String: String], tz: TimeZone) -> EventDraft {
        var e = d.event
        if let v = answers["title"], !v.trimmingCharacters(in: .whitespaces).isEmpty { e.title = v }
        if let v = answers["calendarId"] { e.calendarId = v }
        if let v = answers["attendees"], !e.attendees.contains(where: { $0.caseInsensitiveCompare(v) == .orderedSame }) { e.attendees.append(v) }
        if let day = answers["when.date"] {
            if e.when.allDay {
                let span = e.when.date.flatMap { s in e.when.endDate.map { DayKey(s).days(to: DayKey($0)) } } ?? 1
                e.when.date = day; e.when.endDate = DayKey(day).adding(days: max(span, 1)).rawValue
            } else {
                let time = e.when.start.flatMap { $0.split(separator: "T").dropFirst().first.map(String.init) } ?? "09:00:00"
                e.when.start = "\(day)T\(time)"
            }
        }
        if let t = answers["when.time"] {
            let hhmm = t.count >= 5 ? String(t.prefix(5)) : t
            let date = e.when.start.flatMap { $0.split(separator: "T").first.map(String.init) }
                ?? e.when.date ?? DayKey(Date(), in: tz).rawValue
            e.when.allDay = false
            e.when.start = "\(date)T\(hhmm):00"
            e.when.date = nil; e.when.endDate = nil
            if e.when.durationMin == nil { e.when.durationMin = 60 }
            if e.when.tzid == nil { e.when.tzid = tz.identifier }
        }
        return e
    }

    // MARK: Preview rows (omit what was not found)

    public struct PreviewRows: Equatable, Sendable {
        public var title: String
        public var when: String?
        public var place: String?
        public var calendar: String?
        public var people: String?
    }

    public var previewRows: PreviewRows? {
        guard let r = resolved, let d = draft else { return nil }
        let people = d.people.map(\.name).filter { !$0.isEmpty }
        let attendeeOnly = r.attendees.filter { a in !d.people.contains { $0.email == a } }
        let calName = d.event.calendarId == r.calendarId ? d.calendarName : nil
        return PreviewRows(
            title: r.title.trimmingCharacters(in: .whitespaces),
            when: DraftFormat.when(r.when, tz: tz),
            place: (r.location ?? "").isEmpty ? nil : r.location,
            calendar: calName,
            people: (people + attendeeOnly).isEmpty ? nil : (people + attendeeOnly).joined(separator: ", "))
    }

    // MARK: Create

    public var canCreate: Bool {
        guard let r = resolved, stage == .preview || stage == .unreachable else { return false }
        return !isOffline && canContinue && r.validate().isEmpty
    }

    public func create() async {
        guard let r = resolved, canContinue else { return }
        let issues = r.validate()
        if let i = issues.first { createError = i.message; return }
        stage = .creating; createError = nil
        let key = attemptKey ?? UUID().uuidString
        attemptKey = key
        switch await writer.create(r, key: key, mode: .confirmed) {
        case .saved(let detail):
            created = detail
            stage = .created
        case .failed(let f):
            stage = stageAfterFailure
            createError = Self.words(f)
        }
    }

    private var stageAfterFailure: Stage { draft?.degraded == true ? .unreachable : .preview }

    public nonisolated static func words(_ f: WriteFailure) -> String {
        switch f {
        case .offline: "Can\u{2019}t reach the server. Nothing was created."
        case .validation(_, let m), .provider(let m), .forbidden(let m), .other(let m): m
        case .needsReauth: "That account needs you to sign in again. Nothing was created."
        case .rateLimited: "The calendar provider asked us to slow down. Try again in a minute."
        case .conflict, .notFound: "That didn\u{2019}t work. Nothing was created."
        case .unauthorized: "The server no longer accepts this phone\u{2019}s token."
        }
    }

    /// "Added Lunch with Sam, Fri 13:00".
    public var createdToast: String {
        guard let r = resolved else { return "Added" }
        let when = DraftFormat.short(r.when, tz: tz)
        return "Added \(r.title)" + (when.isEmpty ? "" : ", \(when)")
    }

    /// Undo for the Created toast.
    public func undoCreate() async -> Bool {
        guard let c = created else { return false }
        if case .saved = await writer.delete(c.item, scope: .this, key: nil, mode: .confirmed) { return true }
        return false
    }
}
