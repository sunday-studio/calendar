import Foundation

/// Mutating endpoints (B1). Every POST/PATCH/DELETE from the app carries an `Idempotency-Key`: sending the same
/// key again after a lost response returns the first result instead of acting twice.
public protocol HubWriteAPI: Sendable {
    func createEvent(_ draft: EventDraft, key: String) async throws -> EventDetail
    func patchEvent(id: String, patch: EventPatch, version: Int64?, scope: EventScope, key: String) async throws -> EventDetail
    func deleteEvent(id: String, version: Int64?, scope: EventScope, key: String) async throws
    func rsvp(id: String, response: RSVPResponse, note: String?, scope: EventScope, key: String) async throws -> EventDetail
    func addMeet(id: String, key: String) async throws -> EventDetail
    func moveEvent(id: String, calendarId: String, key: String) async throws -> EventDetail
    func setReminder(id: String, minutesBefore: Int?) async throws -> EventDetail
}

struct RSVPBody: Encodable, Sendable {
    var response: RSVPResponse
    var note: String?
    var scope: EventScope
}
struct MoveBody: Encodable, Sendable { var calendarId: String }
struct ReminderBody: Encodable, Sendable {
    var minutesBefore: Int?
    private enum Keys: String, CodingKey { case minutesBefore }
    func encode(to encoder: Encoder) throws {
        var c = encoder.container(keyedBy: Keys.self)
        if let minutesBefore { try c.encode(minutesBefore, forKey: .minutesBefore) } else { try c.encodeNil(forKey: .minutesBefore) }
    }
}

extension APIClient: HubWriteAPI {
    static func writeHeaders(key: String?, version: Int64? = nil) -> [String: String] {
        var h: [String: String] = [:]
        if let key { h["Idempotency-Key"] = key }
        if let version { h["If-Match"] = "\"\(version)\"" }
        return h
    }

    private func scopeQuery(_ scope: EventScope) -> [URLQueryItem] {
        scope == .this ? [] : [URLQueryItem(name: "scope", value: scope.rawValue)]
    }

    public func createEvent(_ draft: EventDraft, key: String) async throws -> EventDetail {
        try await send("POST", "v1/events", body: draft, headers: Self.writeHeaders(key: key))
    }

    public func patchEvent(id: String, patch: EventPatch, version: Int64?, scope: EventScope, key: String) async throws -> EventDetail {
        try await send("PATCH", "v1/events/\(id)", query: scopeQuery(scope), body: patch,
                       headers: Self.writeHeaders(key: key, version: version))
    }

    public func deleteEvent(id: String, version: Int64?, scope: EventScope, key: String) async throws {
        _ = try await perform("DELETE", "v1/events/\(id)", query: scopeQuery(scope), body: nil, etag: nil,
                              headers: Self.writeHeaders(key: key, version: version))
    }

    public func rsvp(id: String, response: RSVPResponse, note: String?, scope: EventScope, key: String) async throws -> EventDetail {
        try await send("POST", "v1/events/\(id)/rsvp", body: RSVPBody(response: response, note: note, scope: scope),
                       headers: Self.writeHeaders(key: key))
    }

    public func addMeet(id: String, key: String) async throws -> EventDetail {
        try await send("POST", "v1/events/\(id)/meet", headers: Self.writeHeaders(key: key))
    }

    public func moveEvent(id: String, calendarId: String, key: String) async throws -> EventDetail {
        try await send("POST", "v1/events/\(id)/move", body: MoveBody(calendarId: calendarId), headers: Self.writeHeaders(key: key))
    }

    /// `PUT /v1/events/{id}/reminder` (naturally idempotent; the contract lists no Idempotency-Key). `nil` clears.
    public func setReminder(id: String, minutesBefore: Int?) async throws -> EventDetail {
        try await send("PUT", "v1/events/\(id)/reminder", body: ReminderBody(minutesBefore: minutesBefore))
    }
}
