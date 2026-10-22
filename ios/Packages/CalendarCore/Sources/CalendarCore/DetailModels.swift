import Foundation

// Wire models added in M3 (event detail, status, sync runs, SSE). Decoding is tolerant of
// unknown / newly added optional fields (the synthesized Codable ignores unknown keys).

public struct Attendee: Codable, Hashable, Sendable {
    public var email: String
    public var name: String?
    public var response: String // needsAction|accepted|declined|tentative
    public var optional: Bool?
    public var isSelf: Bool?
    public var isOrganizer: Bool?
    public init(email: String, name: String? = nil, response: String = "needsAction", optional: Bool? = nil,
                isSelf: Bool? = nil, isOrganizer: Bool? = nil) {
        self.email = email; self.name = name; self.response = response
        self.optional = optional; self.isSelf = isSelf; self.isOrganizer = isOrganizer
    }
}

public struct Person: Codable, Hashable, Sendable {
    public var email: String
    public var name: String?
    /// You organise it (agenda items only).
    public var isSelf: Bool?
    public init(email: String, name: String? = nil, isSelf: Bool? = nil) { self.email = email; self.name = name; self.isSelf = isSelf }
}

public struct Recurrence: Codable, Hashable, Sendable {
    public var rrule: String
    public var summary: String?
    public init(rrule: String, summary: String? = nil) { self.rrule = rrule; self.summary = summary }
}

/// `GET /v1/events/{id}`: an `AgendaItem` plus detail fields (flattened on the wire).
public struct EventDetail: Codable, Hashable, Sendable, Identifiable {
    public var item: AgendaItem
    public var description: String?
    public var organizer: Person?
    public var attendees: [Attendee]
    public var recurrence: Recurrence?
    public var reminderMin: Int?
    public var updatedAt: Date?
    public var calendarName: String?
    public var accountName: String?

    public var id: String { item.id }

    public init(item: AgendaItem, description: String? = nil, organizer: Person? = nil, attendees: [Attendee] = [],
                recurrence: Recurrence? = nil, reminderMin: Int? = nil, updatedAt: Date? = nil,
                calendarName: String? = nil, accountName: String? = nil) {
        self.item = item; self.description = description; self.organizer = organizer; self.attendees = attendees
        self.recurrence = recurrence; self.reminderMin = reminderMin; self.updatedAt = updatedAt
        self.calendarName = calendarName; self.accountName = accountName
    }

    private enum Keys: String, CodingKey {
        case description, organizer, attendees, recurrence, reminderMin, updatedAt, calendarName, accountName
    }

    public init(from decoder: Decoder) throws {
        item = try AgendaItem(from: decoder)
        let c = try decoder.container(keyedBy: Keys.self)
        description = try c.decodeIfPresent(String.self, forKey: .description)
        organizer = try c.decodeIfPresent(Person.self, forKey: .organizer)
        attendees = try c.decodeIfPresent([Attendee].self, forKey: .attendees) ?? []
        recurrence = try c.decodeIfPresent(Recurrence.self, forKey: .recurrence)
        reminderMin = try c.decodeIfPresent(Int.self, forKey: .reminderMin)
        updatedAt = try c.decodeIfPresent(Date.self, forKey: .updatedAt)
        calendarName = try c.decodeIfPresent(String.self, forKey: .calendarName)
        accountName = try c.decodeIfPresent(String.self, forKey: .accountName)
    }

    public func encode(to encoder: Encoder) throws {
        try item.encode(to: encoder)
        var c = encoder.container(keyedBy: Keys.self)
        try c.encodeIfPresent(description, forKey: .description)
        try c.encodeIfPresent(organizer, forKey: .organizer)
        if !attendees.isEmpty { try c.encode(attendees, forKey: .attendees) }
        try c.encodeIfPresent(recurrence, forKey: .recurrence)
        try c.encodeIfPresent(reminderMin, forKey: .reminderMin)
        try c.encodeIfPresent(updatedAt, forKey: .updatedAt)
        try c.encodeIfPresent(calendarName, forKey: .calendarName)
        try c.encodeIfPresent(accountName, forKey: .accountName)
    }
}

public struct EventPage: Codable, Hashable, Sendable {
    public var items: [EventDetail]
    public var nextCursor: String?
    public init(items: [EventDetail], nextCursor: String? = nil) { self.items = items; self.nextCursor = nextCursor }
}

public struct AccountSyncStatus: Codable, Hashable, Sendable {
    public var accountId: String
    public var email: String
    public var status: String
    public var lastSyncAt: Date?
    public var lastOkAt: Date?
    public var nextSyncAt: Date?
    public var lastError: String?
    public var failCount: Int
    public init(accountId: String, email: String, status: String, lastSyncAt: Date? = nil, lastOkAt: Date? = nil,
                nextSyncAt: Date? = nil, lastError: String? = nil, failCount: Int = 0) {
        self.accountId = accountId; self.email = email; self.status = status; self.lastSyncAt = lastSyncAt
        self.lastOkAt = lastOkAt; self.nextSyncAt = nextSyncAt; self.lastError = lastError; self.failCount = failCount
    }
}

public struct HubWarning: Codable, Hashable, Sendable {
    public var kind: String
    public var message: String
    public init(kind: String, message: String) { self.kind = kind; self.message = message }
}

/// `GET /v1/status`.
public struct StatusDTO: Codable, Hashable, Sendable {
    public var hubVersion: String
    public var rev: Int64
    public var dbSizeBytes: Int64
    public var sseClients: Int
    public var accounts: [AccountSyncStatus]
    public var warnings: [HubWarning]

    public init(hubVersion: String, rev: Int64 = 0, dbSizeBytes: Int64 = 0, sseClients: Int = 0,
                accounts: [AccountSyncStatus] = [], warnings: [HubWarning] = []) {
        self.hubVersion = hubVersion; self.rev = rev; self.dbSizeBytes = dbSizeBytes; self.sseClients = sseClients
        self.accounts = accounts; self.warnings = warnings
    }

    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: CodingKeys.self)
        hubVersion = try c.decode(String.self, forKey: .hubVersion)
        rev = try c.decode(Int64.self, forKey: .rev)
        dbSizeBytes = try c.decodeIfPresent(Int64.self, forKey: .dbSizeBytes) ?? 0
        sseClients = try c.decodeIfPresent(Int.self, forKey: .sseClients) ?? 0
        accounts = try c.decodeIfPresent([AccountSyncStatus].self, forKey: .accounts) ?? []
        warnings = try c.decodeIfPresent([HubWarning].self, forKey: .warnings) ?? []
    }
}

/// `GET /v1/sync/runs` item.
public struct SyncRun: Codable, Hashable, Sendable, Identifiable {
    public var id: Int64
    public var accountId: String
    public var calendarId: String?
    public var startedAt: Date
    public var durationMs: Int64
    public var trigger: String
    public var added: Int
    public var changed: Int
    public var removed: Int
    public var errorKind: String?
    public var error: String?
    public init(id: Int64, accountId: String, calendarId: String? = nil, startedAt: Date, durationMs: Int64 = 0,
                trigger: String = "timer", added: Int = 0, changed: Int = 0, removed: Int = 0,
                errorKind: String? = nil, error: String? = nil) {
        self.id = id; self.accountId = accountId; self.calendarId = calendarId; self.startedAt = startedAt
        self.durationMs = durationMs; self.trigger = trigger; self.added = added; self.changed = changed
        self.removed = removed; self.errorKind = errorKind; self.error = error
    }
}

struct SyncRunList: Codable, Sendable { var items: [SyncRun] }
struct QueuedResponse: Codable, Sendable { var queued: Int }
struct SyncRequest: Codable, Sendable { var accountId: String? }

/// How a calendar's colour override changes in a PATCH.
public enum ColorOverrideChange: Sendable, Equatable {
    case unchanged
    case set(String)
    case clear
}

struct CalendarPatch: Encodable, Sendable {
    var visible: Bool?
    var colorOverride: ColorOverrideChange
    private enum Keys: String, CodingKey { case visible, colorOverride }
    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: Keys.self)
        try c.encodeIfPresent(visible, forKey: .visible)
        switch colorOverride {
        case .unchanged: break
        case .set(let s): try c.encode(s, forKey: .colorOverride)
        case .clear: try c.encodeNil(forKey: .colorOverride)
        }
    }
}

/// One SSE frame (ENGINEERING 7.3). `id` is the hub rev for rev/account/sync frames, nil for ping.
public struct StreamEvent: Equatable, Sendable {
    public enum Kind: Equatable, Sendable {
        case rev(Int64)
        case account(id: String, status: String)
        case sync(accountId: String, state: String)
        case ping
    }
    public var id: String?
    public var kind: Kind
    public init(id: String? = nil, kind: Kind) { self.id = id; self.kind = kind }
}
