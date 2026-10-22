import Foundation

public enum APIClientError: Error, Sendable, Equatable {
    case unauthorized(APIError?)
    case notFound(APIError?)
    case server(status: Int, APIError?)
    case transport(String)
    /// TLS / App Transport Security failure (reachable host, bad or missing secure connection).
    case tls(String)
    case decoding(String)
    case invalidResponse
}

public enum Conditional<Value: Sendable>: Sendable {
    case modified(Value, etag: String?)
    case notModified
}

public actor APIClient {
    public let baseURL: URL
    private let token: String
    private let session: URLSession
    private let decoder: JSONDecoder

    public init(baseURL: URL, token: String, session: URLSession = .shared) {
        self.baseURL = baseURL
        self.token = token
        self.session = session
        self.decoder = Self.makeDecoder()
    }

    public init(config: HubConfig, session: URLSession = .shared) {
        self.init(baseURL: config.baseURL, token: config.token, session: session)
    }

    // MARK: Endpoints

    public func hello() async throws -> Hello {
        try await value("v1/hello")
    }

    public func agenda(from: Date, to: Date, etag: String? = nil) async throws -> Conditional<AgendaResponse> {
        let f = ISO8601DateFormatter()
        return try await get("v1/agenda", query: [
            URLQueryItem(name: "from", value: f.string(from: from)),
            URLQueryItem(name: "to", value: f.string(from: to)),
        ], etag: etag)
    }

    public func accounts() async throws -> [Account] {
        try await value("v1/accounts", as: AccountList.self).items
    }

    // MARK: Accounts

    public func connectAccount(provider: String, reauthAccountId: String? = nil) async throws -> ConnectSession {
        try await send("POST", "v1/accounts/connect", body: ConnectRequest(provider: provider, reauthAccountId: reauthAccountId))
    }

    public func account(id: String) async throws -> Account {
        try await value("v1/accounts/\(id)")
    }

    public func patchAccount(id: String, color: String? = nil, displayName: String? = nil) async throws -> Account {
        try await send("PATCH", "v1/accounts/\(id)", body: AccountPatch(displayName: displayName, color: color))
    }

    public func deleteAccount(id: String) async throws {
        _ = try await perform("DELETE", "v1/accounts/\(id)", query: [], body: nil, etag: nil)
    }

    public func reconnectAccount(id: String) async throws -> ConnectSession {
        try await send("POST", "v1/accounts/\(id)/reconnect")
    }

    public func calendars() async throws -> [HubCalendar] {
        try await value("v1/calendars", as: CalendarList.self).items
    }

    public func settings() async throws -> Settings {
        try await value("v1/settings")
    }

    // MARK: Read API (M3)

    public func event(id: String) async throws -> EventDetail {
        try await value("v1/events/\(id)")
    }

    /// `GET /v1/events`. Pass `q` for full-text search (then `from`/`to` are optional).
    public func events(q: String? = nil, from: Date? = nil, to: Date? = nil, calendar: String? = nil,
                       cursor: String? = nil, limit: Int? = nil) async throws -> EventPage {
        let f = ISO8601DateFormatter()
        var items: [URLQueryItem] = []
        if let q { items.append(.init(name: "q", value: q)) }
        if let from { items.append(.init(name: "from", value: f.string(from: from))) }
        if let to { items.append(.init(name: "to", value: f.string(from: to))) }
        if let calendar { items.append(.init(name: "calendar", value: calendar)) }
        if let cursor { items.append(.init(name: "cursor", value: cursor)) }
        if let limit { items.append(.init(name: "limit", value: String(limit))) }
        switch try await get("v1/events", query: items, etag: nil) as Conditional<EventPage> {
        case .modified(let v, _): return v
        case .notModified: throw APIClientError.invalidResponse
        }
    }

    public func patchCalendar(id: String, visible: Bool? = nil, colorOverride: ColorOverrideChange = .unchanged) async throws -> HubCalendar {
        try await send("PATCH", "v1/calendars/\(id)", body: CalendarPatch(visible: visible, colorOverride: colorOverride))
    }

    public func putSettings(_ settings: Settings) async throws -> Settings {
        try await send("PUT", "v1/settings", body: settings)
    }

    public func status() async throws -> StatusDTO {
        try await value("v1/status")
    }

    public func syncRuns(limit: Int? = nil, accountId: String? = nil) async throws -> [SyncRun] {
        var q: [URLQueryItem] = []
        if let limit { q.append(.init(name: "limit", value: String(limit))) }
        if let accountId { q.append(.init(name: "accountId", value: accountId)) }
        switch try await get("v1/sync/runs", query: q, etag: nil) as Conditional<SyncRunList> {
        case .modified(let v, _): return v.items
        case .notModified: throw APIClientError.invalidResponse
        }
    }

    /// `POST /v1/sync`: asks the hub to poll providers now. Returns the number of queued accounts.
    @discardableResult
    public func postSync(accountId: String? = nil) async throws -> Int {
        let r: QueuedResponse = try await send("POST", "v1/sync", body: SyncRequest(accountId: accountId))
        return r.queued
    }

    // MARK: SSE

    /// One SSE connection (`GET /v1/stream`). Finishes when the server closes; throws on transport / HTTP
    /// errors. Reconnection is `LiveStream`'s job, not this method's.
    public nonisolated func openStream(lastEventID: String? = nil) -> AsyncThrowingStream<StreamEvent, Error> {
        let url = baseURL.appending(path: "v1/stream")
        let token = token, session = session
        return AsyncThrowingStream { cont in
            let task = Task {
                var req = URLRequest(url: url)
                req.timeoutInterval = 75 // hub pings every 20 s
                req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
                req.setValue("text/event-stream", forHTTPHeaderField: "Accept")
                req.setValue("no-cache", forHTTPHeaderField: "Cache-Control")
                if let lastEventID { req.setValue(lastEventID, forHTTPHeaderField: "Last-Event-ID") }
                do {
                    let (bytes, resp) = try await session.bytes(for: req)
                    guard let http = resp as? HTTPURLResponse else { throw APIClientError.invalidResponse }
                    switch http.statusCode {
                    case 200..<300: break
                    case 401, 403: throw APIClientError.unauthorized(nil)
                    case 404: throw APIClientError.notFound(nil)
                    default: throw APIClientError.server(status: http.statusCode, nil)
                    }
                    var parser = SSEParser()
                    for try await b in bytes {
                        if let e = parser.feed(byte: b) { cont.yield(e) }
                    }
                    cont.finish()
                } catch let e as URLError where e.code == .cancelled {
                    cont.finish()
                } catch let e as URLError {
                    cont.finish(throwing: Self.map(e))
                } catch {
                    cont.finish(throwing: error)
                }
            }
            cont.onTermination = { _ in task.cancel() }
        }
    }

    // MARK: Core

    private func value<T: Decodable & Sendable>(_ path: String, as: T.Type = T.self) async throws -> T {
        switch try await get(path, query: [], etag: nil) as Conditional<T> {
        case .modified(let v, _): return v
        case .notModified: throw APIClientError.invalidResponse
        }
    }

    /// GET with optional `If-None-Match`; a 304 yields `.notModified`.
    public func get<T: Decodable & Sendable>(
        _ path: String, query: [URLQueryItem] = [], etag: String? = nil
    ) async throws -> Conditional<T> {
        let (data, http) = try await perform("GET", path, query: query, body: nil, etag: etag)
        if http.statusCode == 304 { return .notModified }
        do {
            return .modified(try decoder.decode(T.self, from: data), etag: http.value(forHTTPHeaderField: "ETag"))
        } catch { throw APIClientError.decoding(String(describing: error)) }
    }

    func send<T: Decodable & Sendable>(_ method: String, _ path: String, query: [URLQueryItem] = [],
                                       body: (any Encodable & Sendable)? = nil, headers: [String: String] = [:]) async throws -> T {
        let (data, _) = try await perform(method, path, query: query, body: body, etag: nil, headers: headers)
        do { return try decoder.decode(T.self, from: data) }
        catch { throw APIClientError.decoding(String(describing: error)) }
    }

    func perform(
        _ method: String, _ path: String, query: [URLQueryItem], body: (any Encodable & Sendable)?, etag: String?,
        headers: [String: String] = [:]
    ) async throws -> (Data, HTTPURLResponse) {
        var comps = URLComponents(url: baseURL.appending(path: path), resolvingAgainstBaseURL: false)
        if !query.isEmpty { comps?.queryItems = query }
        guard let url = comps?.url else { throw APIClientError.invalidResponse }
        var req = URLRequest(url: url)
        req.httpMethod = method
        req.timeoutInterval = 15
        req.setValue("Bearer \(token)", forHTTPHeaderField: "Authorization")
        req.setValue("application/json", forHTTPHeaderField: "Accept")
        if let body {
            req.httpBody = try JSONEncoder().encode(AnyEncodable(body))
            req.setValue("application/json", forHTTPHeaderField: "Content-Type")
        }
        if let etag { req.setValue(etag, forHTTPHeaderField: "If-None-Match") }
        for (k, v) in headers { req.setValue(v, forHTTPHeaderField: k) }

        let data: Data
        let resp: URLResponse
        do { (data, resp) = try await session.data(for: req) }
        catch let e as URLError { throw Self.map(e) }
        catch { throw APIClientError.transport(error.localizedDescription) }
        guard let http = resp as? HTTPURLResponse else { throw APIClientError.invalidResponse }

        switch http.statusCode {
        case 200..<300, 304:
            return (data, http)
        default:
            let body = try? decoder.decode(APIError.self, from: data)
            switch http.statusCode {
            case 401, 403: throw APIClientError.unauthorized(body)
            case 404: throw APIClientError.notFound(body)
            default: throw APIClientError.server(status: http.statusCode, body)
            }
        }
    }

    static func map(_ e: URLError) -> APIClientError {
        switch e.code {
        case .secureConnectionFailed, .serverCertificateHasBadDate, .serverCertificateUntrusted,
             .serverCertificateHasUnknownRoot, .serverCertificateNotYetValid,
             .clientCertificateRejected, .clientCertificateRequired, .appTransportSecurityRequiresSecureConnection:
            return .tls(e.localizedDescription)
        default:
            return .transport(e.localizedDescription)
        }
    }

    /// RFC 3339 with or without fractional seconds.
    static func makeDecoder() -> JSONDecoder {
        let d = JSONDecoder()
        d.dateDecodingStrategy = .custom { dec in
            let s = try dec.singleValueContainer().decode(String.self)
            let plain = Date.ISO8601FormatStyle()
            let frac = Date.ISO8601FormatStyle(includingFractionalSeconds: true)
            if let v = try? plain.parse(s) { return v }
            if let v = try? frac.parse(s) { return v }
            throw DecodingError.dataCorrupted(.init(codingPath: dec.codingPath, debugDescription: "bad date \(s)"))
        }
        return d
    }
}

struct AnyEncodable: Encodable {
    let base: any Encodable
    init(_ base: any Encodable) { self.base = base }
    func encode(to encoder: Encoder) throws { try base.encode(to: encoder) }
}

/// What the sync engine and repositories need from the hub; `APIClient` conforms, tests fake it.
public protocol HubReadAPI: Sendable {
    func agenda(from: Date, to: Date, etag: String?) async throws -> Conditional<AgendaResponse>
    func event(id: String) async throws -> EventDetail
    func events(q: String?, from: Date?, to: Date?, calendar: String?, cursor: String?, limit: Int?) async throws -> EventPage
    func accounts() async throws -> [Account]
    func calendars() async throws -> [HubCalendar]
    func patchCalendar(id: String, visible: Bool?, colorOverride: ColorOverrideChange) async throws -> HubCalendar
    func settings() async throws -> Settings
    func putSettings(_ settings: Settings) async throws -> Settings
    func status() async throws -> StatusDTO
    func syncRuns(limit: Int?, accountId: String?) async throws -> [SyncRun]
    func postSync(accountId: String?) async throws -> Int
    func openStream(lastEventID: String?) -> AsyncThrowingStream<StreamEvent, Error>
}

extension APIClient: HubReadAPI {}

/// The slice of the hub API the feature screens use; view models depend on this, tests fake it.
public protocol HubAPI: Sendable {
    func hello() async throws -> Hello
    func accounts() async throws -> [Account]
    func account(id: String) async throws -> Account
    func calendars() async throws -> [HubCalendar]
    func connectAccount(provider: String, reauthAccountId: String?) async throws -> ConnectSession
    func reconnectAccount(id: String) async throws -> ConnectSession
    func patchAccount(id: String, color: String?, displayName: String?) async throws -> Account
    func deleteAccount(id: String) async throws
    func addIcsAccount(url: String, name: String?, color: String?) async throws -> Account
}

extension APIClient: HubAPI {}
