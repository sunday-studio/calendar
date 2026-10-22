import Foundation
import Testing
@testable import CalendarCore

func fixture(_ name: String) throws -> Data {
    let url = try #require(Bundle.module.url(forResource: name, withExtension: "json", subdirectory: "Fixtures"))
    return try Data(contentsOf: url)
}

@Suite struct DecodingTests {
    let decoder = APIClient.makeDecoder()

    @Test func hello() throws {
        let h = try decoder.decode(Hello.self, from: fixture("hello"))
        #expect(h.apiVersion == 1)
        #expect(!h.server.isEmpty)
    }

    @Test func agenda() throws {
        let a = try decoder.decode(AgendaResponse.self, from: fixture("agenda"))
        #expect(!a.items.isEmpty)
        #expect(a.to > a.from)
        let timed = a.items.filter { !$0.allDay }
        #expect(!timed.isEmpty)
        #expect(timed.allSatisfy { $0.start != nil && $0.end != nil && $0.tzid != nil })
        let allDay = a.items.filter(\.allDay)
        #expect(allDay.allSatisfy { $0.date != nil && $0.start == nil })
    }

    @Test func accountsCalendarsSettings() throws {
        let acc = try decoder.decode(AccountList.self, from: fixture("accounts"))
        #expect(acc.items.count == 3)
        let cal = try decoder.decode(CalendarList.self, from: fixture("calendars"))
        #expect(!cal.items.isEmpty)
        let s = try decoder.decode(Settings.self, from: fixture("settings"))
        #expect(!s.workingHours.days.isEmpty)
    }

    @Test func apiError() throws {
        let json = #"{"error":{"code":"not_found","message":"nope","field":"id"}}"#
        let e = try decoder.decode(APIError.self, from: Data(json.utf8))
        #expect(e.code == "not_found" && e.error.field == "id")
    }

    @Test func fractionalDates() throws {
        let json = #"{"rev":1,"from":"2026-01-01T00:00:00.500Z","to":"2026-01-02T00:00:00Z","items":[]}"#
        let a = try decoder.decode(AgendaResponse.self, from: Data(json.utf8))
        #expect(a.to.timeIntervalSince(a.from) > 86_000)
    }
}
