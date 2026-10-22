import Foundation

// Write-side models for api/openapi.yaml (EventInput, Patch, When, Scope) plus client-side validation.

/// Recurring-event edit scope (`?scope=`), default `this`.
public enum EventScope: String, Codable, Sendable, CaseIterable {
    case this, following, all
}

public enum RSVPResponse: String, Codable, Sendable { case accepted, declined, tentative }

public enum ConferenceChoice: String, Codable, Sendable { case meet, none }

public struct WriteIssue: Equatable, Hashable, Sendable {
    public var field: String
    public var message: String
    public init(field: String, message: String) { self.field = field; self.message = message }
}

/// `When` in the contract. Timed: local wall time `start` (no zone) in `tzid` + `durationMin`.
/// All-day: `date` and EXCLUSIVE `endDate`.
public struct EventWhen: Codable, Hashable, Sendable {
    public var allDay: Bool
    public var start: String?
    public var tzid: String?
    public var durationMin: Int?
    public var date: String?
    public var endDate: String?

    public init(allDay: Bool, start: String? = nil, tzid: String? = nil, durationMin: Int? = nil,
                date: String? = nil, endDate: String? = nil) {
        self.allDay = allDay; self.start = start; self.tzid = tzid; self.durationMin = durationMin
        self.date = date; self.endDate = endDate
    }

    /// Timed event from real instants. Returns nil when `end <= start` (duration would be < 1 min).
    public static func timed(start: Date, end: Date, tz: TimeZone) -> EventWhen? {
        let minutes = Int((end.timeIntervalSince(start) / 60).rounded())
        guard minutes >= 1 else { return nil }
        return EventWhen(allDay: false, start: Self.localString(start, tz: tz), tzid: tz.identifier, durationMin: minutes)
    }

    /// All-day from inclusive first and last day (UI shows the last day; the wire wants an exclusive end).
    public static func allDay(first: DayKey, last: DayKey) -> EventWhen {
        EventWhen(allDay: true, date: first.rawValue, endDate: max(first, last).adding(days: 1).rawValue)
    }

    static func formatter(_ tz: TimeZone) -> DateFormatter {
        let f = DateFormatter()
        f.locale = Locale(identifier: "en_US_POSIX")
        f.timeZone = tz
        f.dateFormat = "yyyy-MM-dd'T'HH:mm:ss"
        return f
    }

    public static func localString(_ d: Date, tz: TimeZone) -> String { formatter(tz).string(from: d) }

    /// nil if malformed or the wall time does not exist in `tz` (spring-forward gap).
    public static func parseLocal(_ s: String, tz: TimeZone) -> Date? {
        let f = formatter(tz)
        guard let d = f.date(from: s), f.string(from: d) == s else { return nil }
        return d
    }

    public func startDate() -> Date? {
        guard let start, let tzid, let tz = TimeZone(identifier: tzid) else { return nil }
        return Self.parseLocal(start, tz: tz)
    }

    public func endInstant() -> Date? {
        guard let s = startDate(), let durationMin else { return nil }
        return s.addingTimeInterval(TimeInterval(durationMin) * 60)
    }

    public func validate(prefix: String = "when") -> [WriteIssue] {
        var out: [WriteIssue] = []
        if allDay {
            if start != nil || tzid != nil || durationMin != nil {
                out.append(.init(field: prefix, message: "An all-day event has dates, not times."))
            }
            let a = date.flatMap(Self.parseDay), b = endDate.flatMap(Self.parseDay)
            if a == nil { out.append(.init(field: "\(prefix).date", message: "Pick a start date.")) }
            if b == nil { out.append(.init(field: "\(prefix).endDate", message: "Pick an end date.")) }
            if let a, let b, b <= a { out.append(.init(field: "\(prefix).endDate", message: "The end date has to be after the start date.")) }
        } else {
            if date != nil || endDate != nil { out.append(.init(field: prefix, message: "A timed event has times, not dates.")) }
            guard let tzid, let tz = TimeZone(identifier: tzid) else {
                out.append(.init(field: "\(prefix).tzid", message: "Unknown time zone."))
                return out
            }
            if let start {
                if Self.formatter(tz).date(from: start) == nil {
                    out.append(.init(field: "\(prefix).start", message: "Pick a start time."))
                } else if Self.parseLocal(start, tz: tz) == nil {
                    out.append(.init(field: "\(prefix).start", message: "That time doesn't exist on the day the clocks change."))
                }
            } else { out.append(.init(field: "\(prefix).start", message: "Pick a start time.")) }
            if (durationMin ?? 0) < 1 { out.append(.init(field: "\(prefix).durationMin", message: "The end has to be after the start.")) }
        }
        return out
    }

    static func parseDay(_ s: String) -> DayKey? {
        let p = s.split(separator: "-")
        guard p.count == 3, p[0].count == 4, p[1].count == 2, p[2].count == 2,
              let y = Int(p[0]), let m = Int(p[1]), let d = Int(p[2]) else { return nil }
        var c = Calendar(identifier: .gregorian); c.timeZone = TimeZone(identifier: "UTC")!
        guard let date = c.date(from: DateComponents(year: y, month: m, day: d)),
              c.dateComponents([.year, .month, .day], from: date) == DateComponents(year: y, month: m, day: d) else { return nil }
        return DayKey(s)
    }
}

public enum WriteRules {
    public static func validEmail(_ s: String) -> Bool {
        let parts = s.split(separator: "@", omittingEmptySubsequences: false)
        guard parts.count == 2, !parts[0].isEmpty, parts[1].contains("."), !s.contains(" ") else { return false }
        return !parts[1].hasPrefix(".") && !parts[1].hasSuffix(".")
    }

    public static func attendeeIssues(_ list: [String]) -> [WriteIssue] {
        var out: [WriteIssue] = []
        for a in list where !validEmail(a) { out.append(.init(field: "attendees", message: "\(a) isn't an email address.")) }
        if Set(list.map { $0.lowercased() }).count != list.count { out.append(.init(field: "attendees", message: "Someone is on the list twice.")) }
        return out
    }

    public static func rruleIssues(_ r: String) -> [WriteIssue] {
        let t = r.trimmingCharacters(in: .whitespaces)
        if t.isEmpty || t.uppercased().hasPrefix("RRULE:") || !t.uppercased().contains("FREQ=") {
            return [.init(field: "recurrence", message: "That repeat rule isn't valid.")]
        }
        return []
    }

    public static func reminderIssues(_ m: Int?) -> [WriteIssue] {
        guard let m else { return [] }
        return (0...40_320).contains(m) ? [] : [.init(field: "reminderMin", message: "Reminders can be up to 4 weeks ahead.")]
    }

    public static func titleIssues(_ t: String) -> [WriteIssue] {
        t.trimmingCharacters(in: .whitespacesAndNewlines).isEmpty ? [.init(field: "title", message: "Give it a title.")] : []
    }
}

/// `POST /v1/events` body (`EventInput`).
public struct EventDraft: Codable, Hashable, Sendable {
    public var calendarId: String
    public var title: String
    public var when: EventWhen
    public var location: String?
    public var description: String?
    public var attendees: [String]
    public var recurrence: Recurrence?
    public var conference: ConferenceChoice?
    public var reminderMin: Int?

    public init(calendarId: String, title: String, when: EventWhen, location: String? = nil, description: String? = nil,
                attendees: [String] = [], recurrence: Recurrence? = nil, conference: ConferenceChoice? = nil,
                reminderMin: Int? = nil) {
        self.calendarId = calendarId; self.title = title; self.when = when; self.location = location
        self.description = description; self.attendees = attendees; self.recurrence = recurrence
        self.conference = conference; self.reminderMin = reminderMin
    }

    public func validate() -> [WriteIssue] {
        var out = WriteRules.titleIssues(title)
        if calendarId.isEmpty { out.append(.init(field: "calendarId", message: "Pick a calendar.")) }
        out += when.validate()
        out += WriteRules.attendeeIssues(attendees)
        if let r = recurrence { out += WriteRules.rruleIssues(r.rrule) }
        out += WriteRules.reminderIssues(reminderMin)
        return out
    }

    // Wire: empty optionals are omitted (create has no "clear").
    private enum Keys: String, CodingKey { case calendarId, title, when, location, description, attendees, recurrence, conference, reminderMin }

    /// Tolerant: empty fields are omitted when encoding (and so when drafts are saved).
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: Keys.self)
        calendarId = try c.decode(String.self, forKey: .calendarId)
        title = try c.decode(String.self, forKey: .title)
        when = try c.decode(EventWhen.self, forKey: .when)
        location = try c.decodeIfPresent(String.self, forKey: .location)
        description = try c.decodeIfPresent(String.self, forKey: .description)
        attendees = try c.decodeIfPresent([String].self, forKey: .attendees) ?? []
        recurrence = try c.decodeIfPresent(Recurrence.self, forKey: .recurrence)
        conference = try c.decodeIfPresent(ConferenceChoice.self, forKey: .conference)
        reminderMin = try c.decodeIfPresent(Int.self, forKey: .reminderMin)
    }

    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: Keys.self)
        try c.encode(calendarId, forKey: .calendarId)
        try c.encode(title.trimmingCharacters(in: .whitespacesAndNewlines), forKey: .title)
        try c.encode(when, forKey: .when)
        if let location, !location.isEmpty { try c.encode(location, forKey: .location) }
        if let description, !description.isEmpty { try c.encode(description, forKey: .description) }
        if !attendees.isEmpty { try c.encode(attendees, forKey: .attendees) }
        if let recurrence { try c.encode(Recurrence(rrule: recurrence.rrule), forKey: .recurrence) }
        if let conference { try c.encode(conference, forKey: .conference) }
        if let reminderMin { try c.encode(reminderMin, forKey: .reminderMin) }
    }
}

/// Omitted / set / null for one PATCH field.
public enum Change<T: Sendable & Equatable>: Sendable, Equatable {
    case unchanged, set(T), clear
    public var isChanged: Bool { if case .unchanged = self { false } else { true } }
}

/// `PATCH /v1/events/{id}` body: omitted = untouched, null = clear.
public struct EventPatch: Encodable, Equatable, Sendable {
    public var title: Change<String> = .unchanged
    public var when: Change<EventWhen> = .unchanged
    public var location: Change<String> = .unchanged
    public var description: Change<String> = .unchanged
    public var attendees: Change<[String]> = .unchanged
    public var recurrence: Change<Recurrence> = .unchanged
    public var conference: Change<ConferenceChoice> = .unchanged
    public var reminderMin: Change<Int> = .unchanged

    public init() {}

    public var isEmpty: Bool {
        !(title.isChanged || when.isChanged || location.isChanged || description.isChanged || attendees.isChanged
          || recurrence.isChanged || conference.isChanged || reminderMin.isChanged)
    }

    public func validate() -> [WriteIssue] {
        var out: [WriteIssue] = []
        if isEmpty { return [.init(field: "patch", message: "Nothing to change.")] }
        switch title { case .set(let t): out += WriteRules.titleIssues(t); case .clear: out.append(.init(field: "title", message: "Give it a title.")); case .unchanged: break }
        if case .set(let w) = when { out += w.validate() }
        if case .clear = when { out.append(.init(field: "when", message: "An event needs a time.")) }
        if case .set(let a) = attendees { out += WriteRules.attendeeIssues(a) }
        if case .set(let r) = recurrence { out += WriteRules.rruleIssues(r.rrule) }
        if case .set(let m) = reminderMin { out += WriteRules.reminderIssues(m) }
        return out
    }

    private enum Keys: String, CodingKey { case title, when, location, description, attendees, recurrence, conference, reminderMin }
    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: Keys.self)
        func put<V: Encodable>(_ ch: Change<V>, _ k: Keys, trim: ((V) -> V)? = nil) throws {
            switch ch {
            case .unchanged: break
            case .clear: try c.encodeNil(forKey: k)
            case .set(let v): try c.encode(trim?(v) ?? v, forKey: k)
            }
        }
        try put(title, .title, trim: { $0.trimmingCharacters(in: .whitespacesAndNewlines) })
        try put(when, .when)
        try put(location, .location)
        try put(description, .description)
        try put(attendees, .attendees)
        switch recurrence {
        case .unchanged: break
        case .clear: try c.encodeNil(forKey: .recurrence)
        case .set(let r): try c.encode(Recurrence(rrule: r.rrule), forKey: .recurrence)
        }
        try put(conference, .conference)
        try put(reminderMin, .reminderMin)
    }
}

/// How a failed write should be presented. Built from any thrown error.
public enum WriteFailure: Error, Equatable, Sendable {
    /// 412 (version moved) or 409; `current` is the server's event when it sent one.
    case conflict(current: EventDetail?)
    case validation(field: String?, message: String)
    case forbidden(String)
    case notFound
    /// Google refused or was unreachable (502 provider_error).
    case provider(String)
    case needsReauth
    case rateLimited
    /// Hub or phone offline; the outcome is unknown only if the request may have been received.
    case offline
    case unauthorized
    case other(String)

    public static func classify(_ error: Error) -> WriteFailure {
        if let f = error as? WriteFailure { return f }
        guard let e = error as? APIClientError else { return .other(error.localizedDescription) }
        switch e {
        case .transport, .tls: return .offline
        case .decoding(let m): return .other(m)
        case .invalidResponse: return .other("The hub sent something unexpected.")
        case .notFound: return .notFound
        case .unauthorized(let b):
            return b?.code == "forbidden" ? .forbidden(b?.message ?? "You can't change this event.") : .unauthorized
        case .server(let status, let b):
            let msg = b?.message ?? "The hub had a problem."
            switch (status, b?.code) {
            case (412, _), (409, _), (_, "conflict"?), (_, "precondition_failed"?): return .conflict(current: b?.error.current)
            case (_, "validation"?), (400, _): return .validation(field: b?.error.field, message: msg)
            case (_, "needs_reauth"?): return .needsReauth
            case (_, "rate_limited"?), (429, _): return .rateLimited
            case (_, "provider_error"?), (502, _): return .provider(msg)
            case (_, "forbidden"?): return .forbidden(msg)
            case (_, "not_found"?): return .notFound
            case (503, _), (_, "unavailable"?): return .offline
            default: return .other(msg)
            }
        }
    }

    /// Unknown outcome: the same Idempotency-Key may safely be sent again.
    public var isRetryable: Bool { self == .offline }
}
