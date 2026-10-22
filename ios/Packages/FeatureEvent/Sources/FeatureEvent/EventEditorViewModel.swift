import CalendarCore
import CalendarUI
import Foundation
import Observation

/// Create / edit form logic. No SwiftUI: the view binds to these properties.
@MainActor @Observable
public final class EventEditorViewModel {
    public enum Mode: Sendable {
        case create
        case edit(AgendaItem)
    }

    public enum SaveState: Equatable, Sendable {
        case idle
        case saving
        /// Rolled back. `canRetry` = the same action can be sent again as is.
        case failed(message: String, canRetry: Bool)
        /// The event changed on the hub while editing; `current` is its latest state.
        case conflict(current: EventDetail)
        case saved
    }

    /// Everything the form edits; equality drives dirty tracking.
    public struct Fields: Equatable, Sendable {
        public var title = ""
        public var location = ""
        public var notes = ""
        public var allDay = false
        public var start: Date
        public var end: Date
        public var firstDay: DayKey
        public var lastDay: DayKey
        public var tzid: String
        public var calendarId: String
        public var attendees: [String] = []
        public var rrule: String?
        public var conference: ConferenceChoice?
        public var reminderMin: Int?
    }

    public private(set) var fields: Fields {
        didSet { if fields != oldValue { attemptKey = nil; serverIssues = [] } }
    }
    public private(set) var initial: Fields
    public private(set) var saveState: SaveState = .idle
    public private(set) var serverIssues: [WriteIssue] = []
    public private(set) var lastFailure: WriteFailure?
    public var scope: EventScope = .this
    public let mode: Mode
    public let writableCalendars: [HubCalendar]
    /// The calendar new events default to (Settings); marked "Default" in the picker.
    public private(set) var defaultCalendarId: String?

    private var original: AgendaItem?
    private var attemptKey: String?
    private var rebasedVersion: Int64?
    private let writer: any EventWriting
    private let makeKey: @Sendable () -> String

    // MARK: Init

    /// New event. `start` is where the user tapped (already snapped); length defaults to one hour.
    public init(create start: Date, calendars: [HubCalendar], defaultCalendarId: String?, tz: TimeZone,
                writer: any EventWriting, makeKey: @escaping @Sendable () -> String = { UUID().uuidString }) {
        let writable = calendars.filter(SettingsFilter.writable)
        let cal = writable.first { $0.id == defaultCalendarId } ?? writable.first { $0.isPrimary } ?? writable.first
        let day = DayKey(start, in: tz)
        let f = Fields(start: start, end: start.addingTimeInterval(3600), firstDay: day, lastDay: day,
                       tzid: tz.identifier, calendarId: cal?.id ?? "")
        fields = f; initial = f; mode = .create; writableCalendars = writable; self.defaultCalendarId = cal?.id; self.writer = writer; self.makeKey = makeKey
    }

    /// Edit an existing event (pass the full detail so notes, guests and repeat are filled in).
    public init(edit detail: EventDetail, calendars: [HubCalendar], tz: TimeZone, writer: any EventWriting,
                makeKey: @escaping @Sendable () -> String = { UUID().uuidString }) {
        let f = Self.fields(from: detail, fallbackTz: tz)
        fields = f; initial = f; mode = .edit(detail.item); original = detail.item; defaultCalendarId = nil
        writableCalendars = calendars.filter(SettingsFilter.writable)
        self.writer = writer; self.makeKey = makeKey
    }

    /// "Duplicate": a NEW event that starts as a copy of `detail`. Counts as dirty from the start so Save works.
    public init(duplicating detail: EventDetail, calendars: [HubCalendar], tz: TimeZone, writer: any EventWriting,
                makeKey: @escaping @Sendable () -> String = { UUID().uuidString }) {
        var f = Self.fields(from: detail, fallbackTz: tz)
        f.conference = nil
        var blank = f; blank.title = ""
        fields = f; initial = blank; mode = .create
        writableCalendars = calendars.filter(SettingsFilter.writable)
        defaultCalendarId = nil
        self.writer = writer; self.makeKey = makeKey
    }

    /// New event pre-filled from a quick-add / free-time draft ("Edit details", "Book the slot"). Counts as dirty so Save works.
    public init(prefill d: EventDraft, calendars: [HubCalendar], defaultCalendarId: String?, tz: TimeZone, now: Date = Date(),
                writer: any EventWriting, makeKey: @escaping @Sendable () -> String = { UUID().uuidString }) {
        let writable = calendars.filter(SettingsFilter.writable)
        let cal = writable.first { $0.id == d.calendarId } ?? writable.first { $0.id == defaultCalendarId }
            ?? writable.first { $0.isPrimary } ?? writable.first
        let zone = d.when.tzid.flatMap(TimeZone.init(identifier:)) ?? tz
        let start = d.when.startDate() ?? now
        let end = d.when.endInstant() ?? start.addingTimeInterval(3600)
        let first = d.when.date.map { DayKey($0) } ?? DayKey(start, in: zone)
        let last = d.when.endDate.map { DayKey($0).adding(days: -1) } ?? first
        let f = Fields(title: d.title, location: d.location ?? "", notes: d.description ?? "", allDay: d.when.allDay,
                       start: start, end: max(end, start.addingTimeInterval(60)), firstDay: first, lastDay: max(first, last),
                       tzid: zone.identifier, calendarId: cal?.id ?? "", attendees: d.attendees,
                       rrule: d.recurrence?.rrule, conference: d.conference == .meet ? .meet : nil, reminderMin: d.reminderMin)
        var blank = f; blank.title = ""
        fields = f; initial = blank; mode = .create; writableCalendars = writable
        self.defaultCalendarId = cal?.id; self.writer = writer; self.makeKey = makeKey
    }

    static func fields(from d: EventDetail, fallbackTz: TimeZone) -> Fields {
        let i = d.item
        let tz = i.tzid.flatMap(TimeZone.init(identifier:)) ?? fallbackTz
        let start = i.start ?? Date(), end = i.end ?? start.addingTimeInterval(3600)
        let first = i.date.map { DayKey($0) } ?? DayKey(start, in: tz)
        let last = i.endDate.map { DayKey($0).adding(days: -1) } ?? first
        return Fields(title: i.title, location: i.location ?? "", notes: d.description ?? "", allDay: i.allDay,
                      start: start, end: end, firstDay: first, lastDay: max(first, last), tzid: tz.identifier,
                      calendarId: i.calendarId,
                      attendees: d.attendees.filter { $0.isSelf != true }.map(\.email),
                      rrule: d.recurrence?.rrule, conference: nil, reminderMin: d.reminderMin)
    }

    // MARK: Editing

    public var title: String { get { fields.title } set { fields.title = newValue } }
    public var location: String { get { fields.location } set { fields.location = newValue } }
    public var notes: String { get { fields.notes } set { fields.notes = newValue } }
    public var calendarId: String { get { fields.calendarId } set { fields.calendarId = newValue } }
    public var rrule: String? { get { fields.rrule } set { fields.rrule = newValue } }
    public var reminderMin: Int? { get { fields.reminderMin } set { fields.reminderMin = newValue } }
    public var tz: TimeZone { TimeZone(identifier: fields.tzid) ?? .current }
    public var addsMeet: Bool {
        get { fields.conference == .meet }
        set { fields.conference = newValue ? .meet : nil }
    }
    public var isAllDay: Bool {
        get { fields.allDay }
        set { fields.allDay = newValue }
    }

    /// Moving the start keeps the length, like every calendar does.
    public func setStart(_ d: Date) {
        let length = fields.end.timeIntervalSince(fields.start)
        fields.start = d
        fields.end = d.addingTimeInterval(max(length, 60))
    }
    public func setEnd(_ d: Date) { fields.end = d }
    public func setFirstDay(_ d: DayKey) {
        let span = fields.firstDay.days(to: fields.lastDay)
        fields.firstDay = d
        fields.lastDay = d.adding(days: max(span, 0))
    }
    public func setLastDay(_ d: DayKey) { fields.lastDay = d }

    /// Returns false (with nothing added) for a bad or duplicate address.
    @discardableResult
    public func addAttendee(_ raw: String) -> Bool {
        let e = raw.trimmingCharacters(in: .whitespacesAndNewlines)
        guard WriteRules.validEmail(e), !fields.attendees.contains(where: { $0.caseInsensitiveCompare(e) == .orderedSame }) else { return false }
        fields.attendees.append(e)
        return true
    }
    public func removeAttendee(_ email: String) { fields.attendees.removeAll { $0 == email } }

    // MARK: Derived

    /// Calendars the event can go to. A repeating event can't leave its account (hub rule).
    public var calendarChoices: [HubCalendar] {
        guard isEditing, original?.recurring == true, let acct = original?.accountId else { return writableCalendars }
        return writableCalendars.filter { $0.accountId == acct }
    }

    public var isEditing: Bool { if case .edit = mode { true } else { false } }
    public var isDirty: Bool { fields != initial }
    public var isSaving: Bool { saveState == .saving }
    /// Cancel / swipe-down asks first when there is something to lose.
    public var shouldConfirmDiscard: Bool { isDirty && saveState != .saved }
    /// Dismissal is blocked only while a request is in flight.
    public var canDismiss: Bool { !isSaving }
    /// Events from a read-only link or calendar can't be saved (defence in depth: the detail screen already hides Edit).
    public var isReadOnly: Bool { if case .edit(let i) = mode { !i.canEdit } else { false } }
    public var canSave: Bool { !isReadOnly && isDirty && issues.isEmpty && !isSaving && saveState != .saved }

    /// Scopes the user may pick for this change (only for recurring events).
    public var scopeChoices: [EventScope] {
        guard isEditing, original?.recurring == true else { return [.this] }
        // A new repeat rule can't be applied to a single occurrence.
        return fields.rrule != initial.rrule ? [.following, .all] : EventScope.allCases
    }
    public var needsScopeChoice: Bool { scopeChoices.count > 1 && isDirty }

    public var when: EventWhen? {
        fields.allDay ? EventWhen.allDay(first: fields.firstDay, last: fields.lastDay)
                      : EventWhen.timed(start: fields.start, end: fields.end, tz: tz)
    }

    /// Everything the hub's rules would reject, as plain messages; empty = can save.
    public var issues: [WriteIssue] {
        var out: [WriteIssue] = []
        if fields.calendarId.isEmpty { out.append(.init(field: "calendarId", message: "Pick a calendar.")) }
        out += WriteRules.titleIssues(fields.title)
        if let w = when { out += w.validate() } else { out.append(.init(field: "when.durationMin", message: "The end has to be after the start.")) }
        out += WriteRules.attendeeIssues(fields.attendees)
        if let r = fields.rrule { out += WriteRules.rruleIssues(r) }
        out += WriteRules.reminderIssues(fields.reminderMin)
        return out + serverIssues
    }

    public func message(for field: String) -> String? { issues.first { $0.field == field || $0.field.hasPrefix(field + ".") }?.message }

    public var draft: EventDraft? {
        guard let w = when else { return nil }
        return EventDraft(calendarId: fields.calendarId, title: fields.title, when: w, location: fields.location,
                          description: fields.notes, attendees: fields.attendees,
                          recurrence: fields.rrule.map { Recurrence(rrule: $0) }, conference: fields.conference,
                          reminderMin: fields.reminderMin)
    }

    /// Only what changed, with emptied text fields sent as clear.
    public var patch: EventPatch {
        var p = EventPatch()
        let a = initial, b = fields
        if b.title != a.title { p.title = .set(b.title) }
        if b.location != a.location { p.location = b.location.isEmpty ? .clear : .set(b.location) }
        if b.notes != a.notes { p.description = b.notes.isEmpty ? .clear : .set(b.notes) }
        if (b.allDay, b.start, b.end, b.firstDay, b.lastDay, b.tzid) != (a.allDay, a.start, a.end, a.firstDay, a.lastDay, a.tzid), let w = when {
            p.when = .set(w)
        }
        if b.attendees != a.attendees { p.attendees = b.attendees.isEmpty ? .clear : .set(b.attendees) }
        if b.rrule != a.rrule { p.recurrence = b.rrule.map { .set(Recurrence(rrule: $0)) } ?? .clear }
        if b.conference != a.conference, b.conference == .meet { p.conference = .set(.meet) }
        if b.reminderMin != a.reminderMin { p.reminderMin = b.reminderMin.map { .set($0) } ?? .clear }
        return p
    }

    // MARK: Save

    public func save() async {
        guard canSave else { return }
        if !scopeChoices.contains(scope) { scope = scopeChoices[0] }
        saveState = .saving
        let key = attemptKey ?? makeKey()
        attemptKey = key
        let outcome: WriteOutcome
        switch mode {
        case .create:
            guard let d = draft else { saveState = .idle; return }
            outcome = await writer.create(d, key: key, mode: .confirmed)
        case .edit:
            outcome = await saveEdit(key: key)
        }
        finish(outcome)
    }

    private func saveEdit(key: String) async -> WriteOutcome {
        guard var item = original else { return .failed(.notFound) }
        if let v = rebasedVersion { item.version = v }
        let p = patch
        var result: WriteOutcome = .saved(nil)
        if !p.isEmpty {
            result = await writer.update(item, patch: p, scope: scope, key: key, mode: .confirmed)
            guard case .saved = result else { return result }
        }
        if fields.calendarId != initial.calendarId {
            let moved = await writer.move(item, toCalendar: fields.calendarId, key: key + ":move", mode: .confirmed)
            if case .failed(let f) = moved {
                // The rest is stored; don't send it twice on retry.
                var base = fields; base.calendarId = initial.calendarId
                initial = base
                return .failed(f)
            }
            if case .saved(let d?) = moved { return .saved(d) }
        }
        return result
    }

    private func finish(_ outcome: WriteOutcome) {
        switch outcome {
        case .saved:
            saveState = .saved
            initial = fields
        case .failed(.conflict(let current?)):
            saveState = .conflict(current: current)
        case .failed(.validation(let field, let message)):
            if let field { serverIssues = [WriteIssue(field: field, message: message)] }
            saveState = .failed(message: message, canRetry: false)
        case .failed(let f):
            lastFailure = f
            saveState = .failed(message: Self.message(for: f), canRetry: f.isRetryable)
        }
    }

    /// Conflict: apply my changes on top of the latest version.
    public func keepMine() async {
        guard case .conflict(let current) = saveState else { return }
        rebasedVersion = current.item.version
        original?.version = current.item.version
        saveState = .idle
        await save()
    }

    /// Conflict: drop my changes and show the hub's version.
    public func useTheirs() {
        guard case .conflict(let current) = saveState else { return }
        let f = Self.fields(from: current, fallbackTz: tz)
        original = current.item; rebasedVersion = nil
        initial = f; fields = f
        saveState = .idle
    }

    func setDefaultReminder(_ m: Int?) {
        var f = fields; f.reminderMin = m
        fields = f; initial = f
    }

    /// Banner headline for a failed save: plain words, never the raw reason (board e-fail).
    public var failureHeadline: String {
        let noun = isEditing ? "change" : "event"
        switch lastFailure {
        case .offline?: return "Can\u{2019}t reach your server."
        case .needsReauth?: return "\(providerName) needs you to sign in again."
        case .rateLimited?: return "\(providerName) is busy right now."
        case .forbidden(let m)?: return m
        case .notFound? where isEditing: return "This event no longer exists."
        default: return "\(providerName) didn\u{2019}t accept the \(noun)."
        }
    }

    public func dismissError() { if case .failed = saveState { saveState = .idle } }

    public static func message(for f: WriteFailure) -> String {
        switch f {
        case .offline: "Can\u{2019}t reach your hub. Check your connection and try again."
        case .provider(let m): "Google didn\u{2019}t accept the change. \(m)"
        case .needsReauth: "Sign in to this Google account again, then try once more."
        case .rateLimited: "Google is busy. Try again in a minute."
        case .forbidden(let m): m
        case .unauthorized: "This phone is no longer signed in to your hub."
        case .notFound: "This event no longer exists."
        case .conflict: "This event changed somewhere else."
        case .validation(_, let m): m
        case .other(let m): m
        }
    }
}

enum SettingsFilter {
    static func writable(_ c: HubCalendar) -> Bool { c.access == "owner" || c.access == "writer" }
}


// MARK: - Form presentation

public struct RepeatOption: Equatable, Sendable, Identifiable {
    public var id: String { title }
    public var title: String
    public var rrule: String?
}

extension EventEditorViewModel {
    var format: EventTimeFormat { EventTimeFormat(tz: tz, locale: DisplayPrefs.load().locale()) }

    public var startText: String {
        fields.allDay ? format.shortDate(fields.firstDay) : "\(format.shortDate(DayKey(fields.start, in: tz))) \u{00B7} \(format.time(fields.start))"
    }

    public var endText: String {
        if fields.allDay { return format.shortDate(fields.lastDay) }
        let sameDay = DayKey(fields.start, in: tz) == DayKey(fields.end, in: tz)
        return sameDay ? format.time(fields.end) : "\(format.shortDate(DayKey(fields.end, in: tz))) \u{00B7} \(format.time(fields.end))"
    }

    public var calendarName: String { writableCalendars.first { $0.id == fields.calendarId }?.name ?? "Choose" }
    public var calendarColor: AccountColor? {
        writableCalendars.first { $0.id == fields.calendarId }.map { AccountColor(key: $0.colorOverride ?? $0.color) }
    }

    /// Provider word for the saving hint ("Google"); only Google exists today.
    public var providerName: String { "Google" }

    public var repeatOptions: [RepeatOption] {
        let day = DayKey(fields.start, in: tz)
        let code = ["SU", "MO", "TU", "WE", "TH", "FR", "SA"][(day.weekday % 7 + 7) % 7]
        let dom = Int(day.rawValue.suffix(2)) ?? 1
        let ord: String = { let t = dom % 100; if (11...13).contains(t) { return "\(dom)th" }; switch dom % 10 { case 1: return "\(dom)st"; case 2: return "\(dom)nd"; case 3: return "\(dom)rd"; default: return "\(dom)th" } }()
        return [
            RepeatOption(title: "Never", rrule: nil),
            RepeatOption(title: "Every day", rrule: "FREQ=DAILY"),
            RepeatOption(title: "Every weekday", rrule: "FREQ=WEEKLY;BYDAY=MO,TU,WE,TH,FR"),
            RepeatOption(title: "Every week on \(fullWeekday(day))", rrule: "FREQ=WEEKLY;BYDAY=\(code)"),
            RepeatOption(title: "Every month on the \(ord)", rrule: "FREQ=MONTHLY;BYMONTHDAY=\(dom)"),
        ]
    }

    func fullWeekday(_ day: DayKey) -> String {
        let f = DateFormatter(); f.locale = .current; f.timeZone = tz; f.dateFormat = "EEEE"
        return f.string(from: day.startOfDay(in: tz).addingTimeInterval(12 * 3600))
    }

    public var repeatText: String {
        guard let r = fields.rrule else { return "Never" }
        return repeatOptions.first { $0.rrule == r }?.title ?? RepeatRule(parsing: r)?.summary(format: format)
            ?? RecurrenceText.summary(Recurrence(rrule: r)) ?? "Custom"
    }

    public var reminderLabel: String { fields.reminderMin.map(ReminderText.text) ?? "None" }

    public var startChanged: Bool { isEditing && (fields.start != initial.start || fields.firstDay != initial.firstDay || fields.allDay != initial.allDay) }
    public var endChanged: Bool { isEditing && (fields.end != initial.end || fields.lastDay != initial.lastDay) }
    public var repeatChanged: Bool { isEditing && fields.rrule != initial.rrule }
    public var reminderChanged: Bool { isEditing && fields.reminderMin != initial.reminderMin }
    public var calendarChanged: Bool { isEditing && fields.calendarId != initial.calendarId }

    /// Minutes since local midnight of the start / end, for the time stepper.
    public var startMinutes: Int {
        get { clockMinutes(fields.start) }
        set { setStart(dateWithMinutes(newValue, like: fields.start)) }
    }
    public var endMinutes: Int {
        get { clockMinutes(fields.end) }
        set { setEnd(dateWithMinutes(newValue, like: fields.end)) }
    }

    func clockMinutes(_ d: Date) -> Int { DayGrid(day: DayKey(d, in: tz), tz: tz, hourHeight: 1).clockMinutes(d) }

    private func dateWithMinutes(_ m: Int, like d: Date) -> Date {
        let day = DayKey(d, in: tz)
        let mm = ((m % 1440) + 1440) % 1440
        let local = EventWhen.localString(day.startOfDay(in: tz), tz: tz)   // "yyyy-MM-ddT00:00:00"
        let stamp = String(local.prefix(11)) + String(format: "%02d:%02d:00", mm / 60, mm % 60)
        return EventWhen.parseLocal(stamp, tz: tz) ?? d
    }

    public var lengthMinutes: Int { Int(fields.end.timeIntervalSince(fields.start) / 60) }
    public func setLength(_ minutes: Int) { setEnd(fields.start.addingTimeInterval(TimeInterval(minutes * 60))) }
    public static let lengthChoices: [(title: String, minutes: Int)] =
        [("15 min", 15), ("30 min", 30), ("45 min", 45), ("1 h", 60), ("1 h 30", 90)]

    public func setFirstDayFromStart(shiftDays n: Int) {
        let nd = DayKey(fields.start, in: tz).adding(days: n)
        let delta = TimeInterval(86_400 * n)
        _ = nd
        setStart(fields.start.addingTimeInterval(delta))
    }

    /// "Ends 10:00." under the length chips.
    public var endsHint: String { "Ends \(format.time(fields.end))." }

    // Drafts: kept on this phone when the provider did not accept the event.
    private static let draftKey = "editor.draft"
    public func saveDraft() {
        guard let d = draft, let data = try? JSONEncoder().encode(d) else { return }
        UserDefaults.standard.set(data, forKey: Self.draftKey)
    }
    public static func loadDraft() -> EventDraft? {
        UserDefaults.standard.data(forKey: draftKey).flatMap { try? JSONDecoder().decode(EventDraft.self, from: $0) }
    }
    public static func clearDraft() { UserDefaults.standard.removeObject(forKey: draftKey) }

    /// Conflict screen content: each field the hub changed that I also changed.
    public func conflictRows(current: EventDetail) -> [(label: String, mine: String, theirs: String)] {
        var rows: [(String, String, String)] = []
        let theirs = Self.fields(from: current, fallbackTz: tz)
        if fields.title != initial.title || theirs.title != initial.title, fields.title != theirs.title {
            rows.append(("Title", fields.title, theirs.title))
        }
        if fields.start != theirs.start || fields.end != theirs.end, startChanged || endChanged || theirs.start != initial.start {
            let f = format
            rows.append(("Start time", "\(f.time(fields.start))", "\(f.time(theirs.start))"))
        }
        return rows
    }
}


public extension EventDraft {
    /// For "Undo delete": the same event, created again.
    init?(recreating d: EventDetail) {
        let i = d.item
        let when: EventWhen
        if i.allDay { guard let a = i.date, let b = i.endDate else { return nil }; when = EventWhen(allDay: true, date: a, endDate: b) }
        else {
            guard let s = i.start, let e = i.end, let id = i.tzid, let tz = TimeZone(identifier: id), let w = EventWhen.timed(start: s, end: e, tz: tz) else { return nil }
            when = w
        }
        self.init(calendarId: i.calendarId, title: i.title, when: when, location: i.location, description: d.description,
                  attendees: d.attendees.filter { $0.isSelf != true }.map(\.email),
                  recurrence: d.recurrence.map { Recurrence(rrule: $0.rrule) }, reminderMin: d.reminderMin)
    }
}


public extension EventEditorViewModel {
    /// Apply the user's default reminder to a NEW event without making it "dirty".
    func applyDefaults(reminderMin: Int?) {
        guard !isEditing else { return }
        reminderMin == nil ? () : setDefaultReminder(reminderMin)
    }
}
