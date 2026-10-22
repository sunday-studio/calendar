#if DEBUG
import SwiftUI

/// Debug gallery of every component. Launch the app with `-gallery`.
public struct ComponentGallery: View {
    enum Mode: String, CaseIterable { case system = "System", light = "Light", dark = "Dark" }
    @State private var mode: Mode = .system

    public init() {}

    private var scheme: ColorScheme? {
        switch mode { case .system: nil; case .light: .light; case .dark: .dark }
    }

    public var body: some View {
        ScrollView {
            VStack(spacing: 0) {
                Picker("Appearance", selection: $mode) {
                    ForEach(Mode.allCases, id: \.self) { Text($0.rawValue).tag($0) }
                }
                .pickerStyle(.segmented)
                .padding(.horizontal, Spacing.gutter).padding(.vertical, Spacing.s)
                GalleryContent()
            }
        }
        .background(Color.calBg)
        .preferredColorScheme(scheme)
    }
}

struct ControlsDemo: View {
    @State private var seg = 1
    @State private var on = true
    @State private var off = false
    @State private var colour: AccountColor? = .rose
    var body: some View {
        VStack(alignment: .leading, spacing: Spacing.l) {
            SegmentedControl(["Agenda", "Day", "3 days"], selection: $seg).padding(.horizontal, Spacing.gutter)
            ColourSwatchRow(selection: $colour)
            VStack(spacing: 0) {
                NavRow("Time zones", value: "Accra · New York", isFirst: true)
                ToggleRow("Join button on the alert", sub: "When an event has a call link", isOn: $on)
                ToggleRow("New invitations", isOn: $off)
            }
            HStack { ChipToggle("Mon", isOn: $on); ChipToggle("Sat", isOn: $off) }.padding(.horizontal, Spacing.gutter)
        }
    }
}

struct GalleryContent: View {
    static let sample: [EventRowModel] = [
        .init(title: "Dentist", start: "11:00", color: .cobalt, status: .moved, overlaps: true, statusDetail: "11:45", overlapWith: "Roadmap review"),
        .init(title: "Budget check-in", start: "11:30", color: .rose, status: .maybe),
        .init(title: "Lunch with Sam", start: "12:30", color: .cobalt),
        .init(title: "Planning", start: "13:00", color: .rose, status: .declined),
        .init(title: "Design review", start: "14:00", color: .rose, status: .needsReply),
        .init(title: "Gym", start: "17:30", color: .moss, status: .cancelled),
        .init(title: "Ana's birthday", start: "", color: .cobalt, isAllDay: true),
        .init(title: "Standup", start: "09:00", color: .rose, isPast: true),
    ]

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            section("Next event block")
            NextEventBlock(time: "10:30", title: "Roadmap review", footer: "in 8 min", color: .rose, action: .join("Join"))
            gap
            NextEventBlock(time: "12:30", title: "Lunch with Sam", footer: "Kōya · 14 min walk", color: .cobalt, action: .map("Map"))
            gap
            NextEventBlock(time: "17:30", title: "Dinner", footer: "in 7 h", color: .moss, action: .details("Details"))
            gap
            QuietCard(title: "Done for today", footer: "Next: Standup, tomorrow 09:00")

            section("Flip cells")
            VStack(alignment: .leading, spacing: Spacing.m) {
                FlipCells("CALENDAR", size: .xl)
                FlipCells("10:30")
                FlipCells("10:30", size: .small)
            }.padding(.horizontal, Spacing.gutter)

            section("Agenda")
            DaySectionHeader(title: "Today")
            ForEach(Array(Self.sample.enumerated()), id: \.element.id) { i, m in EventRow(m, isFirst: i == 0) }
            BrassLine(label: "09:52").padding(.vertical, Spacing.m)
            DaySectionHeader(title: "Wednesday")
            SkeletonRow()
            SkeletonRow()

            section("Banner, toast, empty")
            AmberBanner("[work email] needs you to sign in again.", message: "Its events are paused.", actionTitle: "Fix", action: {})
            gap
            AmberBanner("Could not reach the server.", actionTitle: "Retry", action: {}, kind: .error)
            gap
            ToastView("Moved to 11:45", actionTitle: "Undo", action: {})
            gap
            EmptyState(title: "Clear until Thursday", message: "Next: Design review, Thu 14:00", actionTitle: "New event", action: {})

            section("Buttons")
            VStack(spacing: Spacing.s) {
                CalButton("Save", action: {})
                CalButton("Cancel", kind: .outline, action: {})
                CalButton("Reply", kind: .amber, action: {})
                CalButton("Remove account", kind: .danger, action: {})
                CalButton("Disabled", action: {}).disabled(true)
            }.padding(.horizontal, Spacing.gutter)

            section("Nav, today button, status, tags")
            ScreenNav("Accounts", trailing: "Add", onTrailing: {})
            HStack(spacing: Spacing.m) {
                TodayButton(day: 11, action: {})
                StatusLabel("Synced 1 min ago", kind: .ok)
                StatusLabel("Syncing", kind: .working)
                StatusLabel("Sign in", kind: .bad)
            }.padding(.horizontal, Spacing.gutter)
            HStack { CalTag("Read only"); CalTag("Reply needed", amber: true) }.padding(Spacing.gutter)

            section("Fields, rows, checkbox")
            VStack(spacing: Spacing.l) {
                LabeledField("Server address") { Text("https://hub.tail3a9f.ts.net") }
                LabeledField("Focused", isFocused: true) { Text("casprine.001@gmail.com") }
                LabeledField("Error", error: "Apple did not accept that password.") { Text("Wrong password") }
            }.padding(.horizontal, Spacing.gutter)
            ListRow("casprine.001@gmail.com", sub: "Google · 4 calendars", color: .cobalt, isFirst: true) { StatusLabel("1 min ago", kind: .ok) }
            ListRow("[work email]", sub: "Microsoft · 2 calendars", color: .rose) { StatusLabel("Sign in", kind: .bad) }
            ChecklistRow("Reach the server", status: "Done", kind: .ok, isFirst: true)
            ChecklistRow("Read the server version", status: "Working", kind: .working)
            ChecklistRow("Save this phone", status: "Waiting", kind: .none)
            KeyValueRow("Last good sync", "Today 08:14, 2 h ago")
            KeyValueRow("Last error", "Sign in expired", isError: true)
            HStack(spacing: Spacing.l) { Checkbox(isOn: true); Checkbox(isOn: false) }.padding(Spacing.gutter)

            section("Controls")
            ControlsDemo()
            JoinBar("Google Meet", button: "Join", color: .rose, action: {}).padding(.top, Spacing.m)
            PersonRow("Maya Owusu", status: "Organiser", isFirst: true)
            PersonRow("Efua Lartey", status: "Pending", tone: .amber)

            section("Timeline")
            ZStack(alignment: .topLeading) {
                HourGrid(firstHour: 9, hours: 6)
                TimelineColumn([
                    .init(id: "1", title: "Standup", startMinute: 540, endMinute: 570, color: .rose),
                    .init(id: "2", title: "Roadmap review", subtitle: "10:30", startMinute: 630, endMinute: 690, color: .rose),
                    .init(id: "3", title: "Dentist", subtitle: "11:45 · moved", startMinute: 660, endMinute: 720, color: .cobalt, state: .tentative),
                ], firstHour: 9, hours: 6, now: 622, nowLabel: "10:22")
                .padding(.leading, 64).padding(.trailing, 16)
            }
            .frame(height: 6 * 64)

            section("Account colours, hatch")
            HStack(spacing: Spacing.m) {
                ForEach(AccountColor.allCases) { c in
                    VStack(spacing: Spacing.xs) {
                        AccountDot(c, size: 18)
                        Text(c.rawValue).font(CalFont.timeSmall).foregroundStyle(Color.calInk2)
                    }
                }
            }
            .padding(Spacing.gutter)
            HatchedShade().frame(height: 48).padding(.horizontal, Spacing.gutter)
            Spacer(minLength: Spacing.xxl)
        }
        .background(Color.calBg)
    }

    var gap: some View { Color.clear.frame(height: Spacing.s) }

    func section(_ t: String) -> some View {
        Text(t).calLabel().padding(.horizontal, Spacing.gutter).padding(.top, Spacing.xl).padding(.bottom, Spacing.s)
    }
}

#Preview("Gallery light") { ScrollView { GalleryContent() }.environment(\.colorScheme, .light) }
#Preview("Gallery dark") { ScrollView { GalleryContent() }.environment(\.colorScheme, .dark) }
#endif
