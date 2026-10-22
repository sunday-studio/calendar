import Foundation
import Testing
@testable import CalendarCore

final class AssistStub: URLProtocol, @unchecked Sendable {
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

@Suite(.serialized) struct APIClientAssistTests {
    func client() -> APIClient {
        let cfg = URLSessionConfiguration.ephemeral
        cfg.protocolClasses = [AssistStub.self]
        return APIClient(baseURL: URL(string: "https://hub.test")!, token: "cal_x", session: URLSession(configuration: cfg))
    }
    func json(_ s: String) -> [Data] { [Data(s.utf8)] }

    static let draft = #"""
    {"confidence":0.8,"source":"rules","degraded":true,"calendarName":"Work",
     "event":{"calendarId":"work","title":"Lunch with Sam","when":{"allDay":false,"start":"2026-10-16T13:00:00","tzid":"Europe/London","durationMin":60},"location":"Kōya","attendees":["sam@x.com"]},
     "people":[{"name":"Sam","email":"sam@x.com"}],
     "ambiguities":[{"field":"when.date","question":"Which Friday?","options":[{"value":"2026-10-16","label":"This Friday, 16 Oct"},{"value":"2026-10-23","label":"Next Friday, 23 Oct"}],"default":0,"optional":false},
                    {"field":"when.time","question":"What time?","options":[],"default":-1,"optional":true}],
     "spans":[{"kind":"who","from":11,"to":14},{"kind":"when","from":15,"to":28}]}
    """#

    @Test func quickAddSendsTextNowTzAndDecodesDraft() async throws {
        AssistStub.handler = { req in
            #expect(req.url?.path == "/v1/quick-add" && req.httpMethod == "POST")
            let body = try? JSONSerialization.jsonObject(with: APIClientTests.bodyData(req)) as? [String: Any]
            #expect(body?["text"] as? String == "lunch with sam fri 1pm")
            #expect(body?["tzid"] as? String == "Europe/London")
            #expect(body?["now"] is String)
            return (200, [:], self.json(Self.draft))
        }
        let d = try await client().quickAdd(text: "lunch with sam fri 1pm", now: Date(), tzid: "Europe/London")
        #expect(d.degraded == true && d.source == "rules")
        #expect(d.event.title == "Lunch with Sam" && d.event.when.durationMin == 60)
        #expect(d.ambiguities.count == 2 && d.ambiguities[0].options[1].value == "2026-10-23" && d.ambiguities[1].optional)
        #expect(d.spans[0] == DraftSpan(kind: "who", from: 11, to: 14))
    }

    @Test func freeTimeRequestAndResult() async throws {
        AssistStub.handler = { req in
            let body = try? JSONSerialization.jsonObject(with: APIClientTests.bodyData(req)) as? [String: Any]
            #expect(body?["durationMin"] as? Int == 240)
            #expect(body?["ignoreWorkingHours"] as? Bool == true)
            #expect(body?["dayPart"] as? String == "morning")
            #expect(body?["from"] is String && body?["to"] is String)
            return (200, [:], self.json(#"{"durationMin":240,"from":"2026-10-12T00:00:00Z","to":"2026-10-19T00:00:00Z","workingHours":null,"slots":[],"closest":[{"start":"2026-10-19T09:00:00Z","end":"2026-10-19T13:00:00Z","durationMin":240,"kind":"later","reason":"Nothing else on"},{"start":"2026-10-14T09:00:00Z","end":"2026-10-14T11:00:00Z","durationMin":120,"kind":"shorter","reason":"After Standup"}]}"#))
        }
        let from = Date(timeIntervalSince1970: 1_791_000_000)
        let r = try await client().freeTime(FreeTimeRequest(durationMin: 240, from: from, to: from.addingTimeInterval(86400 * 7), ignoreWorkingHours: true, dayPart: .morning))
        #expect(r.slots.isEmpty && r.closest.count == 2 && !r.usedWorkingHours)
        #expect(r.closest[0].kind == .later && r.closest[1].durationMin == 120)
    }

    @Test func tokensListCreateRevoke() async throws {
        AssistStub.handler = { req in
            switch (req.httpMethod, req.url?.path) {
            case ("GET", "/v1/tokens"?):
                return (200, [:], self.json(#"{"items":[{"id":"t1","name":"Phone","kind":"device","scope":"admin","prefix":"cal_ab","createdAt":"2026-10-01T10:00:00Z","lastUsedAt":"2026-10-11T10:00:00Z","revokedAt":null},{"id":"t2","name":"Old","kind":"script","scope":"read","prefix":"cal_cd","createdAt":"2026-09-01T10:00:00Z","lastUsedAt":null,"revokedAt":"2026-09-02T10:00:00Z"}]}"#))
            case ("POST", "/v1/tokens"?):
                let body = try? JSONSerialization.jsonObject(with: APIClientTests.bodyData(req)) as? [String: Any]
                #expect(body?["name"] as? String == "Home script" && body?["kind"] as? String == "script" && body?["scope"] as? String == "read")
                return (201, [:], self.json(#"{"id":"t3","name":"Home script","kind":"script","scope":"read","prefix":"cal_ef","createdAt":"2026-10-12T10:00:00Z","secret":"cal_ef0123456789"}"#))
            case ("DELETE", "/v1/tokens/t2"?):
                return (204, [:], [])
            default:
                Issue.record("unexpected \(req.httpMethod ?? "") \(req.url?.path ?? "")"); return (500, [:], [])
            }
        }
        let c = client()
        let list = try await c.tokens()
        #expect(list.count == 2 && list[0].lastUsedAt != nil && list[1].revokedAt != nil && list[1].lastUsedAt == nil)
        let made = try await c.createToken(name: "Home script", kind: .script, scope: .read)
        #expect(made.secret == "cal_ef0123456789" && made.token.id == "t3")
        try await c.revokeToken(id: "t2")
    }

    @Test func tokenForbiddenIsUnauthorizedWithForbiddenCode() async {
        AssistStub.handler = { _ in (403, [:], self.json(#"{"error":{"code":"forbidden","message":"admin scope required"}}"#)) }
        do { _ = try await client().tokens(); Issue.record("should throw") }
        catch APIClientError.unauthorized(let b) { #expect(b?.code == "forbidden") }
        catch { Issue.record("wrong error \(error)") }
    }

    @Test func icsAccount422CarriesFieldUrl() async {
        AssistStub.handler = { req in
            let body = try? JSONSerialization.jsonObject(with: APIClientTests.bodyData(req)) as? [String: Any]
            #expect(body?["url"] as? String == "https://x.test/a.ics")
            return (422, [:], self.json(#"{"error":{"code":"validation","message":"That link did not return a calendar.","field":"url"}}"#))
        }
        do { _ = try await client().addIcsAccount(url: "https://x.test/a.ics"); Issue.record("should throw") }
        catch APIClientError.server(let s, let b) { #expect(s == 422 && b?.error.field == "url") }
        catch { Issue.record("wrong error \(error)") }
    }

    @Test func peopleDecodes() async throws {
        AssistStub.handler = { req in
            #expect(URLComponents(url: req.url!, resolvingAgainstBaseURL: false)?.queryItems?.first?.value == "sa")
            return (200, [:], self.json(#"{"items":[{"email":"sam@x.com","name":"Sam Boateng","events":7,"lastSeen":"2026-10-10T10:00:00Z"}]}"#))
        }
        let p = try await client().people(q: "sa")
        #expect(p == [PersonHit(email: "sam@x.com", name: "Sam Boateng", events: 7)])
    }
}
