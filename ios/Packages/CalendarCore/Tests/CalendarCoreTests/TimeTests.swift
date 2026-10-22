import Foundation
import Testing
@testable import CalendarCore

@Suite struct DayKeyTests {
    @Test func sameInstantDifferentDaysAcrossZones() {
        let t = iso("2026-10-11T23:30:00Z")
        #expect(DayKey(t, in: london) == "2026-10-12")      // BST = UTC+1
        #expect(DayKey(t, in: tokyo) == "2026-10-12")
        #expect(DayKey(t, in: losAngeles) == "2026-10-11")  // PDT = UTC-7
        #expect(DayKey(t, in: TimeZone(identifier: "UTC")!) == "2026-10-11")
    }

    @Test func dstBoundaryUsesLocalWallClock() {
        // London leaves BST on 2026-10-25 at 02:00 local; 23:30Z on the 24th is already 00:30 on the 25th... no: UTC+1 -> 00:30 on 25th.
        #expect(DayKey(iso("2026-10-24T23:30:00Z"), in: london) == "2026-10-25")
        // After the change the zone is UTC+0, so 23:30Z on the 25th stays on the 25th.
        #expect(DayKey(iso("2026-10-25T23:30:00Z"), in: london) == "2026-10-25")
    }

    @Test func arithmeticIsPureCivilDate() {
        #expect(DayKey("2026-02-28").adding(days: 1) == "2026-03-01")
        #expect(DayKey("2028-02-28").adding(days: 1) == "2028-02-29")
        #expect(DayKey("2026-01-01").adding(days: -1) == "2025-12-31")
        #expect(DayKey("2026-10-25").days(to: "2026-10-26") == 1) // DST day is still one day
        #expect(DayKey("2026-10-11").weekday == 0)               // Sunday
        #expect(DayKey("2026-10-12").weekday == 1)
        #expect(DayKey("2026-10-30").through("2026-11-02").map(\.rawValue) == ["2026-10-30", "2026-10-31", "2026-11-01", "2026-11-02"])
    }

    @Test func startOfDayInZone() {
        #expect(DayKey("2026-10-12").startOfDay(in: tokyo) == iso("2026-10-11T15:00:00Z"))
        #expect(DayKey("2026-10-25").startOfDay(in: london) == iso("2026-10-24T23:00:00Z"))
    }

    @Test func timedItemRange() {
        let late = timed("a", "late", "2026-10-11T23:30:00Z", "2026-10-12T00:30:00Z")
        #expect(late.dayRange(in: london) == DayKey("2026-10-12")...DayKey("2026-10-12"))
        #expect(late.dayRange(in: losAngeles) == DayKey("2026-10-11")...DayKey("2026-10-11"))
        // Ends exactly at local midnight: does not spill into the next day.
        let toMidnight = timed("b", "b", "2026-10-11T22:00:00Z", "2026-10-11T23:00:00Z")
        #expect(toMidnight.dayRange(in: london) == DayKey("2026-10-11")...DayKey("2026-10-11"))
        // Multi-day.
        let span = timed("c", "c", "2026-10-11T20:00:00Z", "2026-10-13T08:00:00Z")
        #expect(span.dayRange(in: london) == DayKey("2026-10-11")...DayKey("2026-10-13"))
        // Zero length.
        let z = timed("d", "d", "2026-10-11T10:00:00Z", "2026-10-11T10:00:00Z")
        #expect(z.dayRange(in: london) == DayKey("2026-10-11")...DayKey("2026-10-11"))
    }

    @Test func allDayRangeIgnoresZoneAndEndIsExclusive() {
        let one = allDayItem("a", "a", "2026-10-11", "2026-10-12")
        let three = allDayItem("b", "b", "2026-10-11", "2026-10-14")
        for tz in [london, tokyo, losAngeles] {
            #expect(one.dayRange(in: tz) == DayKey("2026-10-11")...DayKey("2026-10-11"))
            #expect(three.dayRange(in: tz) == DayKey("2026-10-11")...DayKey("2026-10-13"))
        }
    }

    @Test func windowIsTodayMinus14ToPlus90() {
        let w = SyncWindow(now: iso("2026-10-11T23:30:00Z"), tz: london) // already the 12th in London
        #expect(w.firstDay == "2026-09-28")
        #expect(w.lastDay == "2027-01-10")
        #expect(w.from == iso("2026-09-27T23:00:00Z"))
        #expect(w.to == iso("2027-01-11T00:00:00Z"))
        // Stable within a local day, so the ETag stays valid.
        #expect(SyncWindow(now: iso("2026-10-12T05:00:00Z"), tz: london) == w)
    }
}

@Suite struct SSEParserTests {
    @Test func parsesTheHubFrames() {
        var p = SSEParser()
        let wire = "retry: 3000\n\nid: 7\nevent: rev\ndata: {\"rev\":7}\n\nevent: ping\ndata: {}\n\n"
            + "id: 8\nevent: account\ndata: {\"id\":\"A1\",\"status\":\"needs_reauth\"}\n\n"
            + "id: 8\nevent: sync\ndata: {\"accountId\":\"A1\",\"state\":\"finished\"}\n\n"
        let evs = p.feed(Array(wire.utf8))
        #expect(evs == [
            StreamEvent(id: "7", kind: .rev(7)),
            StreamEvent(id: nil, kind: .ping),
            StreamEvent(id: "8", kind: .account(id: "A1", status: "needs_reauth")),
            StreamEvent(id: "8", kind: .sync(accountId: "A1", state: "finished")),
        ])
    }

    @Test func handlesSplitChunksCRLFCommentsAndUnknowns() {
        var p = SSEParser()
        var out: [StreamEvent] = []
        for chunk in ["id: 3\r\nev", "ent: rev\r\ndata: {\"re", "v\":3}\r\n\r\n", ": keepalive\n\n",
                      "event: future\ndata: {}\n\n", "event: rev\ndata: not json\n\n", "event: rev\ndata: {\"rev\":4}\n\n"] {
            out += p.feed(Array(chunk.utf8))
        }
        #expect(out == [StreamEvent(id: "3", kind: .rev(3)), StreamEvent(id: nil, kind: .rev(4))])
    }

    @Test func toleratesExtraFieldsInPayload() {
        var p = SSEParser()
        let evs = p.feed(Array("id: 1\nevent: rev\ndata: {\"rev\":1,\"extra\":true}\n\n".utf8))
        #expect(evs == [StreamEvent(id: "1", kind: .rev(1))])
    }
}

@Suite struct DecodingToleranceTests {
    @Test func eventDetailWithNewOptionalFields() throws {
        let json = """
        {"id":"e:1","eventId":"e","version":3,"calendarId":"c","accountId":"a","color":"cobalt","title":"T","allDay":false,
         "start":"2026-10-11T09:00:00Z","end":"2026-10-11T10:00:00Z","tzid":"Europe/London","status":"confirmed","busy":true,
         "recurring":true,"canEdit":true,"canRSVP":false,"canAddConference":false,
         "description":"hello","organizer":{"email":"o@x.com","name":"Olly"},
         "attendees":[{"email":"a@x.com","response":"accepted","isSelf":true,"futureField":1}],
         "recurrence":{"rrule":"FREQ=WEEKLY","summary":"Weekly on Tue, Thu"},"reminderMin":10,
         "updatedAt":"2026-10-01T10:00:00.123Z","calendarName":"Work","accountName":"Alex","somethingNew":{"x":1}}
        """
        let d = try APIClient.makeDecoder().decode(EventDetail.self, from: Data(json.utf8))
        #expect(d.item.title == "T" && d.attendees.count == 1 && d.attendees[0].isSelf == true)
        #expect(d.recurrence?.summary == "Weekly on Tue, Thu")
        #expect(d.calendarName == "Work" && d.accountName == "Alex" && d.reminderMin == 10)
        // Round-trips through the cache encoding.
        let again = try APIClient.makeDecoder().decode(EventDetail.self, from: JSONEncoder.cache.encode(d))
        #expect(again.item == d.item && again.attendees == d.attendees && again.description == "hello")
    }

    @Test func settingsWithAndWithoutNewFields() throws {
        let old = try APIClient.makeDecoder().decode(Settings.self, from: fixture("settings"))
        #expect(old.userTzid == nil && old.alertDefaults == nil)
        let json = #"{"defaultCalendarId":null,"weekStart":1,"userTzid":"Europe/London","alertDefaults":{"timed":10,"allDay":null},"workingHours":{"start":"09:00","end":"17:30","days":[1]},"x":1}"#
        let s = try APIClient.makeDecoder().decode(SettingsDTO.self, from: Data(json.utf8))
        #expect(s.userTzid == "Europe/London" && s.alertDefaults == AlertDefaults(timed: 10, allDay: nil))
    }

    @Test func statusAndSyncRuns() throws {
        let st = #"{"hubVersion":"1","rev":5,"dbSizeBytes":10,"sseClients":1,"accounts":[{"accountId":"a","email":"e","status":"active","lastSyncAt":"2026-10-11T10:00:00Z","lastOkAt":null,"nextSyncAt":null,"lastError":null,"failCount":0}],"warnings":[]}"#
        let s = try APIClient.makeDecoder().decode(StatusDTO.self, from: Data(st.utf8))
        #expect(s.rev == 5 && s.accounts.first?.failCount == 0)
        let r = #"{"id":9,"accountId":"a","calendarId":null,"startedAt":"2026-10-11T10:00:00Z","durationMs":12,"trigger":"timer","added":1,"changed":2,"removed":3,"errorKind":null,"error":null}"#
        let run = try APIClient.makeDecoder().decode(SyncRun.self, from: Data(r.utf8))
        #expect(run.added == 1 && run.removed == 3)
    }
}
