import Foundation

/// `GET /v1/summary` (morning summary for one local day; `title`/`body` are ready to show).
public struct DaySummary: Codable, Hashable, Sendable {
    public var date: String
    public var count: Int
    public var firstStart: String?
    public var titles: [String]
    public var needsReply: Int
    public var title: String
    public var body: String
    public init(date: String, count: Int, firstStart: String? = nil, titles: [String] = [], needsReply: Int = 0,
                title: String, body: String) {
        self.date = date; self.count = count; self.firstStart = firstStart; self.titles = titles
        self.needsReply = needsReply; self.title = title; self.body = body
    }
}

public enum PushEnvironment: String, Codable, Sendable {
    case sandbox, production
    /// Debug builds are signed with the development APNs environment, release builds with production.
    public static var current: PushEnvironment {
        #if DEBUG
        .sandbox
        #else
        .production
        #endif
    }
}

/// Device push registration and the summary read; `APIClient` conforms, tests fake it.
public protocol HubPushAPI: Sendable {
    func registerPush(apnsToken: String, environment: PushEnvironment, bundleId: String?) async throws
    func unregisterPush() async throws
    func summary(date: String?) async throws -> DaySummary
}

private struct PushBody: Encodable {
    var apnsToken: String
    var environment: String
    var bundleId: String?
}

extension APIClient: HubPushAPI {
    /// `PUT /v1/devices/self/push` (204).
    public func registerPush(apnsToken: String, environment: PushEnvironment, bundleId: String?) async throws {
        _ = try await perform("PUT", "v1/devices/self/push", query: [],
                              body: PushBody(apnsToken: apnsToken, environment: environment.rawValue, bundleId: bundleId), etag: nil)
    }

    /// `DELETE /v1/devices/self/push` (204).
    public func unregisterPush() async throws {
        _ = try await perform("DELETE", "v1/devices/self/push", query: [], body: nil, etag: nil)
    }

    public func summary(date: String?) async throws -> DaySummary {
        let q = date.map { [URLQueryItem(name: "date", value: $0)] } ?? []
        switch try await get("v1/summary", query: q, etag: nil) as Conditional<DaySummary> {
        case .modified(let v, _): return v
        case .notModified: throw APIClientError.invalidResponse
        }
    }
}

public extension AppDatabase {
    /// Per-event reminder overrides (minutes before) learned from cached event details, keyed by the id the
    /// detail was fetched with (occurrence id or event id). Events never opened have no entry (default alert).
    func reminderOverrides() async throws -> [String: Int] {
        try await writer.read { db in
            var out: [String: Int] = [:]
            for r in try EventDetailRecord.fetchAll(db) {
                if let d = try? JSONDecoder.cache.decode(EventDetail.self, from: r.json), let m = d.reminderMin { out[r.id] = m }
            }
            return out
        }
    }

    /// Ids of upcoming occurrences whose detail is not cached yet.
    func uncachedDetailIDs(ids: [String]) async throws -> [String] {
        try await writer.read { db in
            let have = Set(try String.fetchAll(db, sql: "SELECT id FROM event_detail"))
            return ids.filter { !have.contains($0) }
        }
    }
}
