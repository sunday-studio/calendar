import Foundation

// Wire models mirroring server/internal/api/dto.go (hand-written, DECISIONS D2).
// No SwiftUI in this module.

public struct Hello: Codable, Hashable, Sendable {
    public var server: String
    public var version: String
    public var apiVersion: Int
    public var scope: String
    public var tokenId: String
    public init(server: String, version: String, apiVersion: Int, scope: String, tokenId: String) {
        self.server = server; self.version = version; self.apiVersion = apiVersion; self.scope = scope; self.tokenId = tokenId
    }
}

public struct Join: Codable, Hashable, Sendable {
    public var url: String
    public var kind: String // meet|zoom|teams|other
    public init(url: String, kind: String) { self.url = url; self.kind = kind }
}

/// Timed: start/end (UTC) + tzid. All-day: date/endDate (endDate exclusive).
public struct AgendaItem: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var eventId: String
    public var version: Int64
    public var calendarId: String
    public var accountId: String
    public var color: String
    public var title: String
    public var allDay: Bool
    public var start: Date?
    public var end: Date?
    public var tzid: String?
    public var date: String?
    public var endDate: String?
    public var location: String?
    public var join: Join?
    public var myResponse: String?
    public var status: String
    public var busy: Bool
    public var recurring: Bool
    public var canEdit: Bool
    public var canRSVP: Bool
    public var canAddConference: Bool
    /// Absent when unknown.
    public var organizer: Person?

    public init(
        id: String, eventId: String, version: Int64 = 1, calendarId: String, accountId: String, color: String,
        title: String, allDay: Bool, start: Date? = nil, end: Date? = nil, tzid: String? = nil,
        date: String? = nil, endDate: String? = nil, location: String? = nil, join: Join? = nil,
        myResponse: String? = nil, status: String = "confirmed", busy: Bool = true, recurring: Bool = false,
        canEdit: Bool = false, canRSVP: Bool = false, canAddConference: Bool = false, organizer: Person? = nil
    ) {
        self.id = id; self.eventId = eventId; self.version = version; self.calendarId = calendarId
        self.accountId = accountId; self.color = color; self.title = title; self.allDay = allDay
        self.start = start; self.end = end; self.tzid = tzid; self.date = date; self.endDate = endDate
        self.location = location; self.join = join; self.myResponse = myResponse; self.status = status
        self.busy = busy; self.recurring = recurring; self.canEdit = canEdit; self.canRSVP = canRSVP
        self.canAddConference = canAddConference; self.organizer = organizer
    }
}

public struct AgendaResponse: Codable, Hashable, Sendable {
    public var rev: Int64
    public var from: Date
    public var to: Date
    public var items: [AgendaItem]
    public init(rev: Int64, from: Date, to: Date, items: [AgendaItem]) {
        self.rev = rev; self.from = from; self.to = to; self.items = items
    }
}

public struct Account: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var provider: String
    public var email: String
    public var displayName: String
    public var color: String
    public var status: String
    public var statusDetail: String?
    public var lastSyncAt: Date?
    public var lastOkAt: Date?
    public var createdAt: Date
    public init(id: String, provider: String = "google", email: String, displayName: String, color: String,
                status: String, statusDetail: String? = nil, lastSyncAt: Date? = nil, lastOkAt: Date? = nil,
                createdAt: Date = Date()) {
        self.id = id; self.provider = provider; self.email = email; self.displayName = displayName
        self.color = color; self.status = status; self.statusDetail = statusDetail
        self.lastSyncAt = lastSyncAt; self.lastOkAt = lastOkAt; self.createdAt = createdAt
    }
}

/// Wire values of `Account.status`.
public enum AccountStatus: String, Sendable {
    case active, syncing, paused
    case needsReauth = "needs_reauth"
    case error
}

public extension Account {
    var statusKind: AccountStatus { AccountStatus(rawValue: status) ?? .error }
}

public struct ConnectSession: Codable, Hashable, Sendable {
    public var authUrl: String
    public var state: String
    public init(authUrl: String, state: String) { self.authUrl = authUrl; self.state = state }
}

struct ConnectRequest: Codable, Sendable {
    var provider: String
    var reauthAccountId: String?
}

struct AccountPatch: Codable, Sendable {
    var displayName: String?
    var color: String?
}

public struct AccountList: Codable, Hashable, Sendable {
    public var items: [Account]
}

/// Named `HubCalendar` to avoid shadowing `Foundation.Calendar` (DECISIONS D).
public struct HubCalendar: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var accountId: String
    public var name: String
    public var color: String
    public var colorOverride: String?
    public var tzid: String
    public var access: String // owner|writer|reader|freebusy
    public var isPrimary: Bool
    public var visible: Bool
    public init(id: String, accountId: String, name: String, color: String, colorOverride: String? = nil,
                tzid: String = "UTC", access: String = "owner", isPrimary: Bool = false, visible: Bool = true) {
        self.id = id; self.accountId = accountId; self.name = name; self.color = color
        self.colorOverride = colorOverride; self.tzid = tzid; self.access = access
        self.isPrimary = isPrimary; self.visible = visible
    }
}

public struct CalendarList: Codable, Hashable, Sendable {
    public var items: [HubCalendar]
}

public struct WorkingHours: Codable, Hashable, Sendable {
    public var start: String
    public var end: String
    public var days: [Int] // 0 = Sunday
    public init(start: String, end: String, days: [Int]) { self.start = start; self.end = end; self.days = days }
}

/// Default reminder lead times in minutes (nil = no default alert).
public struct AlertDefaults: Codable, Hashable, Sendable {
    public var timed: Int?
    public var allDay: Int?
    public init(timed: Int? = nil, allDay: Int? = nil) { self.timed = timed; self.allDay = allDay }
}

/// Daily summary push (off by default; `time` is HH:MM in the user's zone).
public struct MorningSummary: Codable, Hashable, Sendable {
    public var enabled: Bool
    public var time: String
    public init(enabled: Bool = false, time: String = "07:30") { self.enabled = enabled; self.time = time }
}

/// `GET/PUT /v1/settings`. `userTzid` / `alertDefaults` are optional on the wire (omitted on PUT = unchanged).
public struct Settings: Codable, Hashable, Sendable {
    public var defaultCalendarId: String?
    public var weekStart: Int
    public var secondaryTzid: String?
    public var userTzid: String?
    public var workingHours: WorkingHours
    public var alertDefaults: AlertDefaults?
    public var morningSummary: MorningSummary?
    public init(defaultCalendarId: String? = nil, weekStart: Int = 1, secondaryTzid: String? = nil,
                userTzid: String? = nil, workingHours: WorkingHours = WorkingHours(start: "09:00", end: "17:30", days: [1, 2, 3, 4, 5]),
                alertDefaults: AlertDefaults? = nil, morningSummary: MorningSummary? = nil) {
        self.morningSummary = morningSummary
        self.defaultCalendarId = defaultCalendarId; self.weekStart = weekStart; self.secondaryTzid = secondaryTzid
        self.userTzid = userTzid; self.workingHours = workingHours; self.alertDefaults = alertDefaults
    }
}

public typealias SettingsDTO = Settings

/// Error body: `{"error":{"code","message","field?"}}`.
public struct APIError: Codable, Hashable, Sendable, Error {
    public struct Body: Codable, Hashable, Sendable {
        public var code: String
        public var message: String
        public var field: String?
        /// Server state on 409/412 (an Event), when the hub sent one.
        public var current: EventDetail?
        private enum Keys: String, CodingKey { case code, message, field, current }
        public init(from decoder: Decoder) throws {
            let c = try decoder.container(keyedBy: Keys.self)
            code = try c.decode(String.self, forKey: .code)
            message = try c.decode(String.self, forKey: .message)
            field = try c.decodeIfPresent(String.self, forKey: .field)
            current = (try? c.decodeIfPresent(EventDetail.self, forKey: .current)) ?? nil   // never lose the error over a bad `current`
        }
        public init(code: String, message: String, field: String? = nil, current: EventDetail? = nil) {
            self.code = code; self.message = message; self.field = field; self.current = current
        }
    }
    public var error: Body
    public init(error: Body) { self.error = error }
    public var code: String { error.code }
    public var message: String { error.message }
}
