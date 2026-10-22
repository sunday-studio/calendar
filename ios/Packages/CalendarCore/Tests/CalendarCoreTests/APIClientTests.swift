import Foundation
import Testing
@testable import CalendarCore

final class StubProtocol: URLProtocol, @unchecked Sendable {
    nonisolated(unsafe) static var handler: (@Sendable (URLRequest) -> (Int, [String: String], Data))?

    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let (code, headers, body) = Self.handler!(request)
        let resp = HTTPURLResponse(url: request.url!, statusCode: code, httpVersion: nil, headerFields: headers)!
        client?.urlProtocol(self, didReceive: resp, cacheStoragePolicy: .notAllowed)
        client?.urlProtocol(self, didLoad: body)
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

@Suite(.serialized) struct APIClientTests {
    func client() -> APIClient {
        let cfg = URLSessionConfiguration.ephemeral
        cfg.protocolClasses = [StubProtocol.self]
        return APIClient(baseURL: URL(string: "https://hub.test")!, token: "cal_x", session: URLSession(configuration: cfg))
    }

    @Test func etagRoundTrip() async throws {
        let body = try fixture("agenda")
        StubProtocol.handler = { req in
            #expect(req.value(forHTTPHeaderField: "Authorization") == "Bearer cal_x")
            if req.value(forHTTPHeaderField: "If-None-Match") == "\"1-abc\"" { return (304, [:], Data()) }
            return (200, ["ETag": "\"1-abc\""], body)
        }
        let c = client()
        let now = Date()
        guard case .modified(let a, let etag) = try await c.agenda(from: now, to: now.addingTimeInterval(3600)) else {
            Issue.record("expected modified"); return
        }
        #expect(!a.items.isEmpty)
        #expect(etag == "\"1-abc\"")
        guard case .notModified = try await c.agenda(from: now, to: now.addingTimeInterval(3600), etag: etag) else {
            Issue.record("expected 304"); return
        }
    }

    @Test func typedErrors() async throws {
        StubProtocol.handler = { _ in
            (401, [:], Data(#"{"error":{"code":"unauthorized","message":"bad token"}}"#.utf8))
        }
        do { _ = try await client().hello(); Issue.record("should throw") }
        catch let e as APIClientError {
            guard case .unauthorized(let body) = e else { Issue.record("wrong case \(e)"); return }
            #expect(body?.code == "unauthorized")
        }
        StubProtocol.handler = { _ in (500, [:], Data("oops".utf8)) }
        do { _ = try await client().hello(); Issue.record("should throw") }
        catch let e as APIClientError { #expect(e == .server(status: 500, nil)) }
    }
}

extension APIClientTests {
    static func bodyData(_ req: URLRequest) -> Data {
        if let b = req.httpBody { return b }
        guard let s = req.httpBodyStream else { return Data() }
        s.open(); defer { s.close() }
        var out = Data(); var buf = [UInt8](repeating: 0, count: 1024)
        while s.hasBytesAvailable { let n = s.read(&buf, maxLength: 1024); if n <= 0 { break }; out.append(buf, count: n) }
        return out
    }

    @Test func connectPostsProviderAndDecodesSession() async throws {
        StubProtocol.handler = { req in
            #expect(req.httpMethod == "POST")
            #expect(req.url?.path == "/v1/accounts/connect")
            let body = try? JSONSerialization.jsonObject(with: Self.bodyData(req)) as? [String: String]
            #expect(body == ["provider": "google"])
            return (200, [:], Data(#"{"authUrl":"https://accounts.example/auth?x=1","state":"st"}"#.utf8))
        }
        let s = try await client().connectAccount(provider: "google")
        #expect(s.authUrl == "https://accounts.example/auth?x=1")
        #expect(s.state == "st")
    }

    @Test func reconnectPatchDeleteAndGet() async throws {
        let acc = try fixture("accounts")
        let one = try JSONSerialization.data(withJSONObject: (JSONSerialization.jsonObject(with: acc) as! [String: Any])["items"].map { ($0 as! [Any])[0] }!)
        StubProtocol.handler = { req in
            switch (req.httpMethod ?? "", req.url?.path ?? "") {
            case ("POST", "/v1/accounts/A1/reconnect"):
                return (200, [:], Data(#"{"authUrl":"https://x/y","state":"s2"}"#.utf8))
            case ("PATCH", "/v1/accounts/A1"):
                let body = try? JSONSerialization.jsonObject(with: Self.bodyData(req)) as? [String: String]
                #expect(body == ["color": "moss"])
                return (200, [:], one)
            case ("GET", "/v1/accounts/A1"):
                return (200, [:], one)
            case ("DELETE", "/v1/accounts/A1"):
                return (204, [:], Data())
            default:
                Issue.record("unexpected \(req.httpMethod ?? "") \(req.url?.path ?? "")")
                return (404, [:], Data())
            }
        }
        let c = client()
        #expect(try await c.reconnectAccount(id: "A1").state == "s2")
        #expect(try await c.patchAccount(id: "A1", color: "moss").status == "active")
        #expect(try await c.account(id: "A1").email == "alex@northwind.example")
        try await c.deleteAccount(id: "A1")
    }

    @Test func deleteNotFoundThrows() async throws {
        StubProtocol.handler = { _ in (404, [:], Data(#"{"error":{"code":"not_found","message":"no"}}"#.utf8)) }
        do { try await client().deleteAccount(id: "zz"); Issue.record("should throw") }
        catch let e as APIClientError { guard case .notFound = e else { Issue.record("\(e)"); return } }
    }

    @Test func tlsErrorsAreDistinguished() {
        #expect(APIClient.map(URLError(.secureConnectionFailed)) == .tls(URLError(.secureConnectionFailed).localizedDescription))
        #expect(APIClient.map(URLError(.timedOut)) == .transport(URLError(.timedOut).localizedDescription))
    }

    @Test func statusKindMapsWireEnum() {
        for (w, k) in [("active", AccountStatus.active), ("syncing", .syncing), ("paused", .paused), ("needs_reauth", .needsReauth), ("error", .error)] {
            #expect(Account(id: "a", email: "e", displayName: "d", color: "cobalt", status: w).statusKind == k)
        }
    }
}
