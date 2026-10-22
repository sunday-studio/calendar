import Foundation
import Testing
@testable import CalendarCore

final class HubStub: URLProtocol, @unchecked Sendable {
    nonisolated(unsafe) static var handler: (@Sendable (URLRequest) -> (Int, [String: String], [Data]))?
    override class func canInit(with request: URLRequest) -> Bool { true }
    override class func canonicalRequest(for request: URLRequest) -> URLRequest { request }
    override func startLoading() {
        let (code, headers, chunks) = Self.handler!(request)
        client?.urlProtocol(self, didReceive: HTTPURLResponse(url: request.url!, statusCode: code, httpVersion: nil, headerFields: headers)!,
                            cacheStoragePolicy: .notAllowed)
        for c in chunks { client?.urlProtocol(self, didLoad: c) }
        client?.urlProtocolDidFinishLoading(self)
    }
    override func stopLoading() {}
}

@Suite(.serialized) struct APIClientM3Tests {
    func client() -> APIClient {
        let cfg = URLSessionConfiguration.ephemeral
        cfg.protocolClasses = [HubStub.self]
        return APIClient(baseURL: URL(string: "https://hub.test")!, token: "cal_x", session: URLSession(configuration: cfg))
    }
    func json(_ s: String) -> [Data] { [Data(s.utf8)] }

    @Test func eventByOccurrenceId() async throws {
        HubStub.handler = { req in
            #expect(req.url?.path == "/v1/events/E1:1791640800")
            return (200, [:], self.json(#"{"id":"E1:1791640800","eventId":"E1","version":1,"calendarId":"c","accountId":"a","color":"cobalt","title":"T","allDay":true,"date":"2026-10-11","endDate":"2026-10-12","status":"confirmed","busy":false,"recurring":false,"canEdit":false,"canRSVP":false,"canAddConference":false,"attendees":[]}"#))
        }
        let d = try await client().event(id: "E1:1791640800")
        #expect(d.item.allDay && d.attendees.isEmpty)
    }

    @Test func searchQuery() async throws {
        HubStub.handler = { req in
            let q = URLComponents(url: req.url!, resolvingAgainstBaseURL: false)!.queryItems!
            #expect(q.first { $0.name == "q" }?.value == "design rev")
            #expect(q.first { $0.name == "limit" }?.value == "20")
            return (200, [:], self.json(#"{"items":[],"nextCursor":null}"#))
        }
        let p = try await client().events(q: "design rev", limit: 20)
        #expect(p.items.isEmpty && p.nextCursor == nil)
    }

    @Test func patchCalendarEncodesNullToClearOverride() async throws {
        HubStub.handler = { req in
            let body = try? JSONSerialization.jsonObject(with: APIClientTests.bodyData(req)) as? [String: Any]
            #expect(req.httpMethod == "PATCH")
            if req.url?.path == "/v1/calendars/C1" {
                #expect(body?["visible"] as? Bool == false)
                #expect(body?["colorOverride"] is NSNull)
            }
            return (200, [:], self.json(#"{"id":"C1","accountId":"a","name":"n","color":"cobalt","colorOverride":null,"tzid":"UTC","access":"owner","isPrimary":false,"visible":false}"#))
        }
        let c = try await client().patchCalendar(id: "C1", visible: false, colorOverride: .clear)
        #expect(!c.visible)
    }

    @Test func putSettingsAndPostSyncAndStatus() async throws {
        HubStub.handler = { req in
            switch (req.httpMethod ?? "", req.url?.path ?? "") {
            case ("PUT", "/v1/settings"):
                let b = try? JSONSerialization.jsonObject(with: APIClientTests.bodyData(req)) as? [String: Any]
                #expect(b?["userTzid"] as? String == "Europe/London")
                return (200, [:], [APIClientTests.bodyData(req)])
            case ("POST", "/v1/sync"): return (202, [:], self.json(#"{"queued":2}"#))
            case ("GET", "/v1/status"):
                return (200, [:], self.json(#"{"hubVersion":"0.1","rev":3,"dbSizeBytes":1,"sseClients":0,"accounts":[],"warnings":[]}"#))
            case ("GET", "/v1/sync/runs"): return (200, [:], self.json(#"{"items":[]}"#))
            default: return (404, [:], [])
            }
        }
        let c = client()
        #expect(try await c.putSettings(Settings(userTzid: "Europe/London")).userTzid == "Europe/London")
        #expect(try await c.postSync() == 2)
        #expect(try await c.status().rev == 3)
        #expect(try await c.syncRuns(limit: 5).isEmpty)
    }

    @Test func streamParsesFramesAndSendsLastEventID() async throws {
        HubStub.handler = { req in
            #expect(req.value(forHTTPHeaderField: "Last-Event-ID") == "41")
            #expect(req.value(forHTTPHeaderField: "Authorization") == "Bearer cal_x")
            #expect(req.value(forHTTPHeaderField: "Accept") == "text/event-stream")
            return (200, ["Content-Type": "text/event-stream"], [
                Data("retry: 3000\n\nid: 42\nevent: rev\ndata: {\"rev\"".utf8),
                Data(":42}\n\nevent: ping\ndata: {}\n\n".utf8),
            ])
        }
        var got: [StreamEvent] = []
        for try await e in client().openStream(lastEventID: "41") { got.append(e) }
        #expect(got == [StreamEvent(id: "42", kind: .rev(42)), StreamEvent(id: nil, kind: .ping)])
    }

    @Test func streamHTTPErrorsThrowTyped() async throws {
        HubStub.handler = { _ in (401, [:], []) }
        do { for try await _ in client().openStream() {}; Issue.record("should throw") }
        catch let e as APIClientError { guard case .unauthorized = e else { Issue.record("\(e)"); return } }
    }
}
