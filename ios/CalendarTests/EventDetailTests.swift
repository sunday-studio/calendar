import CalendarCore
import Foundation
import Testing
@testable import FeatureEvent
@testable import FeatureHome

private let utcZone = TimeZone(identifier: "UTC")!
private func at(_ s: String) -> Date { (try? Date.ISO8601FormatStyle().parse(s)) ?? Date(timeIntervalSince1970: 0) }

private struct StubRepo: EventDetailRepository {
    var d: EventDetail
    func cached(id: String) async throws -> EventDetail? { d }
    func detail(id: String) async throws -> EventDetail { d }
    func refresh(id: String) async throws -> EventDetail { d }
}

private func detail(attendees: [Attendee] = [], reminder: Int? = nil, desc: String? = nil, recurring: Bool = false) -> EventDetail {
    EventDetail(item: AgendaItem(id: "E:1", eventId: "E", calendarId: "c", accountId: "a", color: "cobalt", title: "Roadmap review",
                                 allDay: false, start: at("2026-10-11T10:30:00Z"), end: at("2026-10-11T11:30:00Z"), tzid: "UTC",
                                 recurring: recurring, canEdit: true),
                description: desc, attendees: attendees, reminderMin: reminder, calendarName: "Work", accountName: "me@work.example")
}

@MainActor private func vm(_ d: EventDetail) -> EventDetailViewModel {
    let m = EventDetailViewModel(id: "E:1", repo: StubRepo(d: d), clock: .fixed(at("2026-10-11T10:00:00Z")), tz: utcZone, locale: Locale(identifier: "en_GB"))
    return m
}

@MainActor struct EventDetailLogicTests {
    @Test func whenLines() async {
        let m = vm(detail())
        await m.load()
        #expect(m.whenLine == "Sun 11 Oct \u{00B7} 10:30 to 11:30")
        #expect(m.eyebrow == "Work \u{00B7} me@work.example")
        #expect(m.relative == "in 30 min")
        let f = EventTimeFormat(tz: utcZone, locale: Locale(identifier: "en_GB"))
        let allDay = AgendaItem(id: "a", eventId: "a", calendarId: "c", accountId: "a", color: "rose", title: "Trip", allDay: true, date: "2026-10-15", endDate: "2026-10-17")
        #expect(f.whenLine(for: allDay) == "Thu 15 Oct to Fri 16 Oct")
        var one = allDay; one.endDate = "2026-10-16"
        #expect(f.whenLine(for: one) == "Thu 15 Oct \u{00B7} All day")
    }

    @Test func peopleSummaryAndReminder() async {
        let people = [Attendee(email: "me@x.com", name: "Me", response: "accepted", isSelf: true),
                      Attendee(email: "maya@x.com", name: "Maya Lin", response: "accepted"),
                      Attendee(email: "sam@x.com", name: "Sam Cho", response: "accepted"),
                      Attendee(email: "ana@x.com", name: "Ana Ruiz", response: "needsAction"),
                      Attendee(email: "bo@x.com", name: "Bo Day", response: "declined"),
                      Attendee(email: "cy@x.com", name: "Cy Fox", response: "accepted")]
        let m = vm(detail(attendees: people, reminder: 10, desc: "<p>Agenda</p>"))
        await m.load()
        #expect(m.peopleNames == "Ana, Bo and 3 more")
        #expect(m.peopleCounts == "4 yes \u{00B7} 1 pending \u{00B7} 1 no")
        #expect(m.reminderText == "10 minutes before")
        #expect(m.notesText == "Agenda")
    }

    @Test func reminderWordsAndEmptyNotes() async {
        for (min, text) in [(0, "At the start"), (60, "1 hour before"), (120, "2 hours before"), (1440, "1 day before"), (45, "45 minutes before")] {
            let m = vm(detail(reminder: min)); await m.load()
            #expect(m.reminderText == text)
        }
        let none = vm(detail()); await none.load()
        #expect(none.reminderText == nil && none.notesText == "None")
    }
}

struct HomeModeTests {
    @Test func tabsAreInCanvasOrder() {
        #expect(HomeMode.titles == ["Agenda", "Day", "3 days"])
        #expect(HomeMode.allCases.map(\.rawValue) == [0, 1, 2])
    }
}
