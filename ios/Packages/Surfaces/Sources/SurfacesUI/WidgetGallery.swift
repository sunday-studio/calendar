import CalendarCore
import CalendarUI
import SurfacesCore
import SwiftUI

/// Container the widget extension draws with `containerBackground`: system radius 24, hairline ring.
struct WidgetFrame<Content: View>: View {
    let width: CGFloat
    let height: CGFloat
    @ViewBuilder var content: Content
    var body: some View {
        content
            .frame(width: width, height: height)
            .background(WidgetTheme.container)
            .containerShape(RoundedRectangle(cornerRadius: 24))
            .clipShape(RoundedRectangle(cornerRadius: 24))
            .overlay(RoundedRectangle(cornerRadius: 24).strokeBorder(Color.primary.opacity(0.08), lineWidth: 1))
    }
}

/// Debug gallery of every widget size and state on a fixed sample day (app launch argument `-widgetGallery`).
public struct WidgetGallery: View {
    let tz = TimeZone(identifier: "Europe/Amsterdam")!
    let now: Date

    public init() {
        var c = DateComponents(year: 2026, month: 10, day: 13, hour: 9, minute: 52)
        c.timeZone = TimeZone(identifier: "Europe/Amsterdam")
        now = Calendar(identifier: .gregorian).date(from: c)!
    }

    private func at(_ d: Int, _ h: Int, _ m: Int) -> Date {
        var cal = Calendar(identifier: .gregorian); cal.timeZone = tz
        var c = DateComponents(year: 2026, month: 10, day: d, hour: h, minute: m); c.timeZone = tz
        return cal.date(from: c)!
    }

    private func item(_ id: String, _ title: String, _ d: Int, _ h: Int, _ m: Int, mins: Int, color: String, reply: Bool = false,
                      meet: Bool = false, place: String? = nil, cancelled: Bool = false) -> AgendaItem {
        let s = at(d, h, m)
        return AgendaItem(id: id, eventId: id, calendarId: "c", accountId: "a", color: color, title: title, allDay: false,
                          start: s, end: s.addingTimeInterval(Double(mins) * 60), tzid: tz.identifier, location: place,
                          join: meet ? Join(url: "https://meet.google.com/abc", kind: "meet") : nil,
                          myResponse: reply ? "needsAction" : "accepted", status: cancelled ? "cancelled" : "confirmed", canRSVP: reply)
    }

    /// The sample day of the v2 boards: Tuesday 13 October 2026.
    var items: [AgendaItem] {
        [AgendaItem(id: "ad", eventId: "ad", calendarId: "c", accountId: "a", color: "cobalt", title: "Ana\u{2019}s birthday", allDay: true,
                    date: "2026-10-13", endDate: "2026-10-14"),
         item("s0", "Standup", 13, 9, 0, mins: 15, color: "rose"),
         item("e1", "Roadmap review", 13, 10, 0, mins: 30, color: "rose", meet: true),
         item("e2", "Dentist", 13, 11, 45, mins: 20, color: "cobalt"),
         item("e3", "Lunch with Sam", 13, 12, 30, mins: 60, color: "cobalt", place: "K\u{014D}ya"),
         item("e4", "Design review", 13, 14, 30, mins: 60, color: "rose"),
         item("e5", "1:1 Maya", 13, 15, 0, mins: 30, color: "rose"),
         item("e6", "Gym", 13, 17, 30, mins: 60, color: "cobalt", cancelled: true),
         item("e7", "Dinner", 13, 19, 0, mins: 90, color: "verdigris", reply: true),
         item("t1", "Standup", 14, 9, 0, mins: 15, color: "rose"),
         item("t2", "Design sync", 14, 11, 0, mins: 30, color: "rose"),
         item("t3", "Lunch", 14, 12, 30, mins: 60, color: "cobalt"),
         item("t4", "1:1 Maya", 14, 15, 0, mins: 30, color: "rose"),
         item("t5", "Dinner", 14, 19, 30, mins: 90, color: "verdigris")]
    }

    func accounts(_ status: String = "active") -> [Account] {
        [Account(id: "a", email: "alex@work.example", displayName: "Work", color: "cobalt", status: status)]
    }

    func entry(at t: Date? = nil, items: [AgendaItem]? = nil, accounts: [Account]? = nil, paired: Bool = true, synced: Date? = nil) -> WidgetEntryData {
        let input = WidgetInput(items: items ?? self.items, accounts: accounts ?? self.accounts(), paired: paired,
                                lastSyncAt: synced ?? (t ?? now), tz: tz)
        return TimelineBuilder.entry(input, at: t ?? now, format: SurfaceFormat(tz: tz, locale: Locale(identifier: "en_GB")))
    }

    public var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                label("Small \u{00B7} next, in person, in progress, date")
                HStack(spacing: 12) {
                    WidgetFrame(width: 158, height: 158) { NextSmallView(entry: entry()) }
                    WidgetFrame(width: 158, height: 158) { NextSmallView(entry: entry(at: at(13, 12, 10))) }
                }
                HStack(spacing: 12) {
                    WidgetFrame(width: 158, height: 158) { NextSmallView(entry: entry(at: at(13, 10, 8))) }
                    WidgetFrame(width: 158, height: 158) {
                        DateSmallView(date: now, tz: tz, locale: Locale(identifier: "en_GB"), left: entry().layout?.todayLeft)
                    }
                }
                label("Medium \u{00B7} A: block + list, in progress, evening")
                WidgetFrame(width: 338, height: 158) { TodayMediumView(entry: entry()) }
                WidgetFrame(width: 338, height: 158) { TodayMediumView(entry: entry(at: at(13, 10, 8))) }
                WidgetFrame(width: 338, height: 158) { TodayMediumView(entry: entry(at: at(13, 17, 40))) }
                label("Medium B \u{00B7} next panel + list")
                WidgetFrame(width: 338, height: 158) { TodayMediumView(entry: entry(), layout: .nextList) }
                label("Large \u{00B7} Agenda, Day")
                WidgetFrame(width: 338, height: 354) { TodayLargeView(entry: entry()) }
                WidgetFrame(width: 338, height: 354) { TodayLargeView(entry: entry(), layout: .day) }
                label("States \u{00B7} medium")
                WidgetFrame(width: 338, height: 158) { TodayMediumView(entry: entry(at: at(13, 20, 50))) }
                WidgetFrame(width: 338, height: 158) { TodayMediumView(entry: entry(accounts: accounts("needs_reauth"))) }
                WidgetFrame(width: 338, height: 158) { TodayMediumView(entry: entry(synced: now.addingTimeInterval(-7 * 3600))) }
                WidgetFrame(width: 338, height: 158) { TodayMediumView(entry: entry(items: [])) }
                WidgetFrame(width: 338, height: 158) { TodayMediumView(entry: entry(accounts: [], paired: true)) }
                label("Lock screen")
                HStack(spacing: 12) {
                    WidgetFrame(width: 170, height: 72) { AccessoryRectangularView(entry: entry()) }
                    WidgetFrame(width: 72, height: 72) { AccessoryCircularView(entry: entry()) }
                    WidgetFrame(width: 140, height: 30) { AccessoryInlineView(entry: entry()) }
                }
                #if canImport(ActivityKit)
                label("Live Activity")
                let attrs = MeetingActivityAttributes(occurrenceId: "e1", color: "rose", joinURL: "https://meet.google.com/abc", joinKind: "meet")
                MeetingLockView(attributes: attrs, state: .init(title: "Roadmap review", start: now.addingTimeInterval(8 * 60),
                                                                 end: now.addingTimeInterval(53 * 60), location: nil, running: false),
                                now: now, tz: tz)
                    .frame(width: 358)
                MeetingLockView(attributes: attrs, state: .init(title: "Roadmap review", start: now.addingTimeInterval(-23 * 60),
                                                                 end: now.addingTimeInterval(22 * 60), location: nil, running: true),
                                now: now, tz: tz)
                    .frame(width: 358)
                #endif
            }
            .padding(16)
        }
        .background(Color(red: 0.55, green: 0.55, blue: 0.55).opacity(0.25))
    }

    func label(_ t: String) -> some View { Text(t).calLabel() }
}
