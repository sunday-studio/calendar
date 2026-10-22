import CalendarCore
import CalendarUI
import SurfacesCore
import SwiftUI

/// Border + surface the widget container draws (spec: bg, 1pt border); the extension uses `containerBackground` instead.
struct WidgetFrame<Content: View>: View {
    let width: CGFloat
    let height: CGFloat
    @ViewBuilder var content: Content
    var body: some View {
        content
            .frame(width: width, height: height)
            .background(Color.calBg)
            .overlay(RoundedRectangle(cornerRadius: 22).strokeBorder(Color.calBorder, lineWidth: 1))
            .clipShape(RoundedRectangle(cornerRadius: 22))
    }
}

/// Debug gallery of every widget size and state on a fixed sample day (app launch argument `-widgetGallery`).
public struct WidgetGallery: View {
    let tz = TimeZone(identifier: "Europe/Amsterdam")!
    let now: Date

    public init() {
        var c = DateComponents(year: 2026, month: 10, day: 16, hour: 10, minute: 22)
        c.timeZone = TimeZone(identifier: "Europe/Amsterdam")
        now = Calendar(identifier: .gregorian).date(from: c)!
    }

    private func item(_ id: String, _ title: String, _ h: Int, _ m: Int, mins: Int, color: String, reply: Bool = false) -> AgendaItem {
        var cal = Calendar(identifier: .gregorian); cal.timeZone = tz
        var c = DateComponents(year: 2026, month: 10, day: 16, hour: h, minute: m); c.timeZone = tz
        let s = cal.date(from: c)!
        return AgendaItem(id: id, eventId: id, calendarId: "c", accountId: "a", color: color, title: title, allDay: false,
                          start: s, end: s.addingTimeInterval(Double(mins) * 60), tzid: tz.identifier,
                          myResponse: reply ? "needsAction" : "accepted", canRSVP: reply)
    }

    var items: [AgendaItem] {
        [item("e1", "Roadmap review", 10, 30, mins: 45, color: "cobalt"),
         item("e2", "Lunch with Sam", 12, 30, mins: 60, color: "rose", reply: true),
         item("e3", "Design sync", 14, 0, mins: 30, color: "verdigris"),
         item("e4", "Dinner", 19, 0, mins: 90, color: "clay")]
    }

    func accounts(_ status: String = "active") -> [Account] {
        [Account(id: "a", email: "alex@work.example", displayName: "Work", color: "cobalt", status: status)]
    }

    func entry(at t: Date? = nil, items: [AgendaItem]? = nil, accounts: [Account]? = nil, paired: Bool = true, synced: Date? = nil) -> WidgetEntryData {
        let input = WidgetInput(items: items ?? self.items, accounts: accounts ?? self.accounts(), paired: paired,
                                lastSyncAt: synced ?? now, tz: tz)
        return TimelineBuilder.entry(input, at: t ?? now, format: SurfaceFormat(tz: tz, locale: Locale(identifier: "en_GB")))
    }

    var tomorrow: AgendaItem {
        var i = item("t1", "Standup", 9, 0, mins: 15, color: "cobalt")
        i.start = i.start!.addingTimeInterval(86_400); i.end = i.end!.addingTimeInterval(86_400)
        return i
    }

    public var body: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 14) {
                label("Small \u{00B7} next, in progress")
                HStack(spacing: 12) {
                    WidgetFrame(width: 166, height: 166) { NextSmallView(entry: entry()) }
                    WidgetFrame(width: 166, height: 166) { NextSmallView(entry: entry(at: now.addingTimeInterval(9 * 60 + 5))) }
                }
                label("Small date \u{00B7} Medium today")
                HStack(spacing: 12) {
                    WidgetFrame(width: 166, height: 166) { DateSmallView(date: now, tz: tz, locale: Locale(identifier: "en_GB")) }
                }
                WidgetFrame(width: 358, height: 166) { TodayMediumView(entry: entry()) }
                label("States")
                WidgetFrame(width: 358, height: 120) {
                    TodayMediumView(entry: entry(at: now.addingTimeInterval(11 * 3600), items: items + [tomorrow]))
                }
                WidgetFrame(width: 358, height: 120) { TodayMediumView(entry: entry(accounts: accounts("needs_reauth"))) }
                WidgetFrame(width: 358, height: 120) { TodayMediumView(entry: entry(accounts: [], paired: true)) }
                WidgetFrame(width: 358, height: 120) { TodayMediumView(entry: entry(items: [])) }
                WidgetFrame(width: 358, height: 166) { TodayMediumView(entry: entry(synced: now.addingTimeInterval(-7 * 3600))) }
                label("Lock screen")
                HStack(spacing: 12) {
                    WidgetFrame(width: 170, height: 72) { AccessoryRectangularView(entry: entry()) }
                    WidgetFrame(width: 72, height: 72) { AccessoryCircularView(entry: entry()) }
                    WidgetFrame(width: 140, height: 30) { AccessoryInlineView(entry: entry()) }
                }
                #if canImport(ActivityKit)
                label("Live Activity")
                let attrs = MeetingActivityAttributes(occurrenceId: "e1", color: "cobalt", joinURL: "https://meet.google.com/abc", joinKind: "meet")
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
