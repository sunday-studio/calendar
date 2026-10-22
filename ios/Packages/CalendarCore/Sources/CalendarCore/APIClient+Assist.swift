import Foundation

// M6: quick add, free time, people, API tokens, read-only ICS link (api/openapi.yaml, tags assist / tokens / accounts).

// MARK: Quick add

public struct DraftSpan: Codable, Hashable, Sendable {
    public var kind: String   // who | when | where
    public var from: Int      // UTF-16 offsets into the text
    public var to: Int
    public init(kind: String, from: Int, to: Int) { self.kind = kind; self.from = from; self.to = to }
}

public struct DraftPerson: Codable, Hashable, Sendable {
    public var name: String
    public var email: String?
    public init(name: String, email: String? = nil) { self.name = name; self.email = email }
}

public struct DraftOption: Codable, Hashable, Sendable {
    public var value: String
    public var label: String
    public init(value: String, label: String) { self.value = value; self.label = label }
}

public struct DraftAmbiguity: Codable, Hashable, Sendable, Identifiable {
    public var field: String           // title | when.date | when.time | attendees | calendarId
    public var question: String
    public var options: [DraftOption]  // empty = free-form (picker / text)
    public var `default`: Int          // -1 = no suggestion
    public var optional: Bool
    public var id: String { field }
    public init(field: String, question: String, options: [DraftOption] = [], default: Int = -1, optional: Bool = false) {
        self.field = field; self.question = question; self.options = options; self.default = `default`; self.optional = optional
    }
}

/// `POST /v1/quick-add` answer. Nothing is written by the hub.
public struct QuickAddDraft: Codable, Hashable, Sendable {
    public var confidence: Double
    public var source: String          // rules | alfather
    public var degraded: Bool?
    public var event: EventDraft
    public var calendarName: String?
    public var people: [DraftPerson]
    public var ambiguities: [DraftAmbiguity]
    public var spans: [DraftSpan]
    public init(confidence: Double = 0.5, source: String = "rules", degraded: Bool? = nil, event: EventDraft,
                calendarName: String? = nil, people: [DraftPerson] = [], ambiguities: [DraftAmbiguity] = [],
                spans: [DraftSpan] = []) {
        self.confidence = confidence; self.source = source; self.degraded = degraded; self.event = event
        self.calendarName = calendarName; self.people = people; self.ambiguities = ambiguities; self.spans = spans
    }
}

// MARK: Free time

public struct FreeSlot: Codable, Hashable, Sendable, Identifiable {
    public var start: Date
    public var end: Date
    public var reason: String
    public var best: Bool
    public var id: Date { start }
    public init(start: Date, end: Date, reason: String, best: Bool = false) {
        self.start = start; self.end = end; self.reason = reason; self.best = best
    }
}

public struct ClosestSlot: Codable, Hashable, Sendable, Identifiable {
    public enum Kind: String, Codable, Sendable { case later, shorter }
    public var start: Date
    public var end: Date
    public var durationMin: Int
    public var kind: Kind
    public var reason: String
    public var id: Date { start }
    public init(start: Date, end: Date, durationMin: Int, kind: Kind, reason: String) {
        self.start = start; self.end = end; self.durationMin = durationMin; self.kind = kind; self.reason = reason
    }
}

public struct FreeTimeResult: Codable, Hashable, Sendable {
    public var durationMin: Int
    public var from: Date
    public var to: Date
    /// False when the search ignored working hours (`workingHours: null`).
    public var usedWorkingHours: Bool
    public var slots: [FreeSlot]
    public var closest: [ClosestSlot]

    private enum Keys: String, CodingKey { case durationMin, from, to, workingHours, slots, closest }
    public init(durationMin: Int, from: Date, to: Date, usedWorkingHours: Bool = true, slots: [FreeSlot], closest: [ClosestSlot] = []) {
        self.durationMin = durationMin; self.from = from; self.to = to; self.usedWorkingHours = usedWorkingHours
        self.slots = slots; self.closest = closest
    }
    public init(from decoder: Decoder) throws {
        let c = try decoder.container(keyedBy: Keys.self)
        durationMin = try c.decode(Int.self, forKey: .durationMin)
        from = try c.decode(Date.self, forKey: .from)
        to = try c.decode(Date.self, forKey: .to)
        usedWorkingHours = c.contains(.workingHours) && !((try? c.decodeNil(forKey: .workingHours)) ?? true)
        slots = try c.decodeIfPresent([FreeSlot].self, forKey: .slots) ?? []
        closest = try c.decodeIfPresent([ClosestSlot].self, forKey: .closest) ?? []
    }
    public func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: Keys.self)
        try c.encode(durationMin, forKey: .durationMin); try c.encode(from, forKey: .from); try c.encode(to, forKey: .to)
        try c.encode(slots, forKey: .slots); try c.encode(closest, forKey: .closest)
    }
}

public enum DayPart: String, Codable, Sendable { case morning, afternoon, evening }

public struct FreeTimeRequest: Sendable, Equatable {
    public var durationMin: Int
    public var from: Date
    public var to: Date
    public var ignoreWorkingHours: Bool
    public var dayPart: DayPart?
    public var limit: Int?
    public init(durationMin: Int, from: Date, to: Date, ignoreWorkingHours: Bool = false, dayPart: DayPart? = nil, limit: Int? = nil) {
        self.durationMin = durationMin; self.from = from; self.to = to; self.ignoreWorkingHours = ignoreWorkingHours
        self.dayPart = dayPart; self.limit = limit
    }
}

public struct PersonHit: Codable, Hashable, Sendable, Identifiable {
    public var email: String
    public var name: String
    public var events: Int
    public var id: String { email }
    public init(email: String, name: String, events: Int = 0) { self.email = email; self.name = name; self.events = events }
}

// MARK: Tokens

public enum TokenKind: String, Codable, Sendable, CaseIterable { case device, script }
public enum TokenScope: String, Codable, Sendable, CaseIterable { case read, write, admin }

public struct APIToken: Codable, Hashable, Sendable, Identifiable {
    public var id: String
    public var name: String
    public var kind: TokenKind
    public var scope: TokenScope
    public var prefix: String
    public var createdAt: Date
    public var lastUsedAt: Date?
    public var revokedAt: Date?
    public init(id: String, name: String, kind: TokenKind, scope: TokenScope, prefix: String = "cal_xxxx", createdAt: Date,
                lastUsedAt: Date? = nil, revokedAt: Date? = nil) {
        self.id = id; self.name = name; self.kind = kind; self.scope = scope; self.prefix = prefix
        self.createdAt = createdAt; self.lastUsedAt = lastUsedAt; self.revokedAt = revokedAt
    }
}

/// The 201 answer of `POST /v1/tokens`: the token plus its secret, which the hub never returns again.
public struct CreatedToken: Codable, Hashable, Sendable {
    public var token: APIToken
    public var secret: String
    public init(token: APIToken, secret: String) { self.token = token; self.secret = secret }
    public init(from decoder: Decoder) throws {
        token = try APIToken(from: decoder)
        secret = try decoder.container(keyedBy: CodingKeys.self).decode(String.self, forKey: .secret)
    }
    public func encode(to encoder: Encoder) throws {
        try token.encode(to: encoder)
        var c = encoder.container(keyedBy: CodingKeys.self)
        try c.encode(secret, forKey: .secret)
    }
    private enum CodingKeys: String, CodingKey { case secret }
}

// MARK: Protocols

public protocol HubAssistAPI: Sendable {
    func quickAdd(text: String, now: Date?, tzid: String?) async throws -> QuickAddDraft
    func freeTime(_ request: FreeTimeRequest) async throws -> FreeTimeResult
    func people(q: String) async throws -> [PersonHit]
}

public protocol HubTokensAPI: Sendable {
    func hello() async throws -> Hello
    func tokens() async throws -> [APIToken]
    func createToken(name: String, kind: TokenKind, scope: TokenScope) async throws -> CreatedToken
    func revokeToken(id: String) async throws
}

// MARK: APIClient

struct QuickAddBody: Encodable, Sendable { var text: String; var now: String?; var tzid: String? }
struct FreeTimeBody: Encodable, Sendable {
    var durationMin: Int; var from: String; var to: String
    var ignoreWorkingHours: Bool?; var dayPart: DayPart?; var limit: Int?
}
struct TokenBody: Encodable, Sendable { var name: String; var kind: TokenKind; var scope: TokenScope }
struct IcsBody: Encodable, Sendable { var url: String; var name: String?; var color: String? }
private struct TokenList: Decodable, Sendable { var items: [APIToken] }
private struct PeopleList: Decodable, Sendable { var items: [PersonHit] }

extension APIClient: HubAssistAPI, HubTokensAPI {
    public func quickAdd(text: String, now: Date?, tzid: String?) async throws -> QuickAddDraft {
        try await send("POST", "v1/quick-add", body: QuickAddBody(text: text, now: now.map { ISO8601DateFormatter().string(from: $0) }, tzid: tzid))
    }

    public func freeTime(_ r: FreeTimeRequest) async throws -> FreeTimeResult {
        let f = ISO8601DateFormatter()
        return try await send("POST", "v1/freetime", body: FreeTimeBody(
            durationMin: r.durationMin, from: f.string(from: r.from), to: f.string(from: r.to),
            ignoreWorkingHours: r.ignoreWorkingHours ? true : nil, dayPart: r.dayPart, limit: r.limit))
    }

    public func people(q: String) async throws -> [PersonHit] {
        switch try await get("v1/people", query: [URLQueryItem(name: "q", value: q)], etag: nil) as Conditional<PeopleList> {
        case .modified(let v, _): return v.items
        case .notModified: throw APIClientError.invalidResponse
        }
    }

    public func tokens() async throws -> [APIToken] {
        switch try await get("v1/tokens", query: [], etag: nil) as Conditional<TokenList> {
        case .modified(let v, _): return v.items
        case .notModified: throw APIClientError.invalidResponse
        }
    }

    public func createToken(name: String, kind: TokenKind, scope: TokenScope) async throws -> CreatedToken {
        try await send("POST", "v1/tokens", body: TokenBody(name: name, kind: kind, scope: scope))
    }

    public func revokeToken(id: String) async throws {
        _ = try await perform("DELETE", "v1/tokens/\(id)", query: [], body: nil, etag: nil)
    }

    /// `POST /v1/accounts/ics` (admin). 422 with `field: "url"` carries a plain message and saves nothing.
    public func addIcsAccount(url: String, name: String? = nil, color: String? = nil) async throws -> Account {
        try await send("POST", "v1/accounts/ics", body: IcsBody(url: url, name: name, color: color))
    }
}
