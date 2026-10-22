import CalendarCore
import CalendarUI
import SwiftUI
import UserNotifications

// Settings boards g-home ... g-week. Changes save on interaction (no Save button), except working hours,
// which validate when the user leaves the screen.

/// Page chrome: bare nav bar, optional sub line, scrolling content.
struct SettingsPage<Content: View>: View {
    let title: String
    var sub: String?
    var trailing: String?
    var onTrailing: (() -> Void)?
    @ViewBuilder var content: Content
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        VStack(spacing: 0) {
            ScreenNav(title, leading: .back, trailing: trailing, onLeading: { dismiss() }, onTrailing: onTrailing)
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    if let sub {
                        Text(sub).font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calInk2)
                            .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s).padding(.bottom, Spacing.s)
                    }
                    content
                    Color.clear.frame(height: Spacing.xxl)
                }
            }
        }
        .background(Color.calBg)
        .toolbar(.hidden, for: .navigationBar)
    }
}

struct GroupLabel: View {
    let text: String
    var first = false
    init(_ text: String, first: Bool = false) { self.text = text; self.first = first }
    var body: some View {
        Text(text).calLabel().padding(.horizontal, Spacing.gutter).padding(.top, first ? 12 : 32).padding(.bottom, 10)
            .accessibilityAddTraits(.isHeader)
    }
}

struct Hint: View {
    let text: String
    init(_ text: String) { self.text = text }
    var body: some View {
        Text(text).font(CalFont.hint).foregroundStyle(Color.calInk2)
            .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.l)
    }
}

/// Shared models for the Settings subscreens.
struct SettingsContext {
    let env: AppEnvironment
    let openAccounts: () -> Void
    let settings: SettingsViewModel
    let calendars: CalendarsViewModel
    let status: StatusViewModel
    let tokens: (any HubTokensAPI)?
}

private struct SettingsContextKey: EnvironmentKey { nonisolated(unsafe) static let defaultValue: SettingsContext? = nil }
extension EnvironmentValues {
    var settingsContext: SettingsContext? {
        get { self[SettingsContextKey.self] }
        set { self[SettingsContextKey.self] = newValue }
    }
}

/// A push to a Settings subscreen. A destination-closure link, NOT `NavigationLink(value:)`: the app's stack path is
/// typed `[HomeRoute]`, so value links of any other type silently do nothing.
struct SettingsLink<Label: View>: View {
    let route: SettingsRoute
    @ViewBuilder var label: Label
    @Environment(\.settingsContext) private var ctx

    init(_ route: SettingsRoute, @ViewBuilder label: () -> Label) { self.route = route; self.label = label() }

    var body: some View {
        NavigationLink { if let ctx { SettingsDestination(route: route, ctx: ctx) } } label: { label }
            .buttonStyle(.plain)
    }
}

struct SettingsDestination: View {
    let route: SettingsRoute
    let ctx: SettingsContext
    var body: some View {
        destination.environment(\.settingsContext, ctx)
    }

    @ViewBuilder private var destination: some View {
        switch route {
        case .calendars: CalendarsScreen(model: ctx.calendars, defaultID: ctx.settings.draft.defaultCalendarId)
        case .colour(let id): ColourScreen(model: ctx.calendars, calendarID: id)
        case .defaultCalendar: DefaultCalendarScreen(model: ctx.settings, calendars: ctx.calendars)
        case .timeZones: TimeZonesScreen(model: ctx.settings)
        case .zonePicker: ZonePickerScreen(model: ctx.settings)
        case .workingHours: WorkingHoursScreen(model: ctx.settings)
        case .weekStart: WeekStartScreen(model: ctx.settings)
        case .alerts: AlertsScreen(model: ctx.settings)
        case .appearance: AppearanceScreen()
        case .server: ServerScreen(model: ctx.status, env: ctx.env, openAccounts: ctx.openAccounts)
        case .syncLog(let id): SyncLogScreen(source: HubStatusSource(api: ctx.env.api), accountID: id)
        case .tokens: if let t = ctx.tokens { TokensScreen(model: TokensViewModel(api: t), api: t) }
        }
    }
}

/// Where the Settings stack can go.
enum SettingsRoute: Hashable {
    case calendars, colour(String), defaultCalendar, timeZones, zonePicker, workingHours, weekStart, alerts, appearance
    case server, syncLog(String?), tokens
}

/// Root of Settings. Push it inside the app's NavigationStack.
public struct SettingsScreen: View {
    let env: AppEnvironment
    let openAccounts: () -> Void
    let tokensAPI: (any HubTokensAPI)?
    @State private var settings: SettingsViewModel
    @State private var calendars: CalendarsViewModel
    @State private var status: StatusViewModel
    @State private var accountCount = 0
    @State private var syncState: SyncState = .idle
    @State private var needsSignIn = false

    public init(env: AppEnvironment, tokens: (any HubTokensAPI)? = nil, openAccounts: @escaping () -> Void) {
        self.env = env; self.openAccounts = openAccounts; self.tokensAPI = tokens
        _settings = State(initialValue: SettingsViewModel(deps: .init(env: env)))
        _calendars = State(initialValue: CalendarsViewModel(deps: .init(env: env)))
        _status = State(initialValue: StatusViewModel(deps: .init(env: env)))
    }

    private var tz: TimeZone { settings.draft.userTzid.flatMap(TimeZone.init(identifier:)) ?? .current }
    private var host: String { HubConfigStore().load()?.baseURL.host ?? "your server" }

    public var body: some View {
        SettingsPage(title: "Settings") {
            GroupLabel("Calendars", first: true)
            Button(action: openAccounts) {
                NavRow("Accounts", value: accountCount == 0 ? "None" : "\(accountCount) connected", isFirst: true)
            }.buttonStyle(.plain)
            SettingsLink(SettingsRoute.calendars) { NavRow("Calendars", sub: "Show, hide, colour") }
            SettingsLink(SettingsRoute.defaultCalendar) { NavRow("Default calendar", value: defaultName) }

            GroupLabel("Time")
            SettingsLink(SettingsRoute.timeZones) { NavRow("Time zones", value: zonesValue, isFirst: true) }
            SettingsLink(SettingsRoute.workingHours) { NavRow("Working hours", value: hoursValue) }
            SettingsLink(SettingsRoute.weekStart) { NavRow("Week starts", value: weekName(settings.draft.weekStart)) }

            GroupLabel("Alerts and look")
            SettingsLink(SettingsRoute.alerts) { NavRow("Alerts", value: alertValue, isFirst: true) }
            SettingsLink(SettingsRoute.appearance) { NavRow("Appearance", value: themeName) }

            GroupLabel("Server")
            SettingsLink(.server) {
                SettingsRow("Server and sync", sub: host, isFirst: true) {
                    HStack(spacing: Spacing.s) {
                        StatusLabel(serverStatus.text, kind: serverStatus.kind)
                        CalGlyph(.forward, size: 9).foregroundStyle(Color.calInk2)
                    }
                }
            }.buttonStyle(.plain)
            if tokensAPI != nil {
                SettingsLink(.tokens) { NavRow("API tokens", sub: "Phones and scripts") }
            }
        }
        .environment(\.settingsContext, SettingsContext(env: env, openAccounts: openAccounts, settings: settings, calendars: calendars, status: status, tokens: tokensAPI))
        .task {
            settings.start(); calendars.start(); status.start()
            async let a: Void = watchAccounts()
            async let s: Void = watchSync()
            _ = await (a, s)
        }
        .onDisappear { settings.stop(); calendars.stop(); status.stop() }
    }

    private func watchAccounts() async {
        for await list in env.accounts.observeAccounts() {
            accountCount = list.count
            needsSignIn = list.contains { $0.statusKind == .needsReauth }
        }
    }
    private func watchSync() async { for await s in env.sync.state { syncState = s } }

    private var defaultName: String {
        settings.writableCalendars.first { $0.id == settings.draft.defaultCalendarId }?.name ?? "None"
    }
    private var zonesValue: String {
        let main = SettingsLogic.city(tz)
        guard let s = settings.draft.secondaryTzid.flatMap(TimeZone.init(identifier:)) else { return main }
        return "\(main) \u{00B7} \(SettingsLogic.city(s))"
    }
    private var hoursValue: String { "\(settings.draft.workingHours.start) to \(settings.draft.workingHours.end)" }
    private var alertValue: String {
        let m = settings.draft.alertDefaults?.timed ?? 10
        return m == 0 ? "At start" : "\(m) min"
    }
    @AppStorage(Prefs.theme) private var theme = "system"
    private var themeName: String { theme == "dark" ? "Dark" : theme == "light" ? "Light" : "System" }
    private var serverStatus: (text: String, kind: StatusLabel.Kind) {
        switch syncState {
        case .offline, .stale: return ("Offline", .bad)
        case .error: return ("Problem", .bad)
        default: return needsSignIn ? ("Sign in", .bad) : ("OK", .ok)
        }
    }
}

func weekName(_ i: Int) -> String { ["Sunday", "Monday", "Tuesday", "Wednesday", "Thursday", "Friday", "Saturday"][((i % 7) + 7) % 7] }

// MARK: Calendars (g-cals, g-colour, g-default)

struct CalendarsScreen: View {
    let model: CalendarsViewModel
    let defaultID: String?

    var body: some View {
        SettingsPage(title: "Calendars") {
            ForEach(model.groups) { g in
                GroupLabel(g.account.email, first: g.id == model.groups.first?.id)
                ForEach(Array(g.calendars.enumerated()), id: \.element.id) { n, c in
                    HStack(spacing: Spacing.l) {
                        SettingsLink(.colour(c.id)) {
                            HStack(spacing: Spacing.l) {
                                Rectangle().fill(AccountColor(key: CalendarsViewModel.colorKey(c)).color).frame(width: 14, height: 14)
                                VStack(alignment: .leading, spacing: 2) {
                                    Text(c.name).font(CalFont.font(.sans, size: 18, relativeTo: .body)).foregroundStyle(Color.calInk).lineLimit(1)
                                    if c.id == defaultID { Text("Default").font(CalFont.hint).foregroundStyle(Color.calInk2) }
                                }
                                Spacer(minLength: 0)
                            }
                            .contentShape(Rectangle())
                        }
                        .accessibilityLabel("\(c.name), colour")
                        // The switch is only a drawing: wrap it in a button so it can be tapped and is announced.
                        Button { Task { await model.setVisible(c.id, !c.visible) } } label: {
                            CalSwitch(isOn: .constant(c.visible)).frame(minWidth: Spacing.hit, minHeight: Spacing.hit)
                        }
                        .buttonStyle(.plain)
                        .accessibilityLabel("\(c.name), visible")
                        .accessibilityValue(c.visible ? "On" : "Off")
                        .accessibilityIdentifier("calendar.visible.\(c.name)")
                    }
                    .padding(.horizontal, Spacing.gutter).padding(.vertical, 10)
                    .frame(maxWidth: .infinity, minHeight: 60, alignment: .leading)
                    .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
                    .overlay(alignment: .top) { if n == 0 { Rectangle().fill(Color.calLine).frame(height: 1) } }
                }
            }
            if model.groups.isEmpty && model.loaded {
                Text("Calendars appear here once an account has synced.").font(CalFont.sub).foregroundStyle(Color.calInk2)
                    .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.m)
            }
            Hint("Hiding a calendar only hides it here. Tap a name to change its colour.")
        }
        .overlay(alignment: .bottom) {
            if let m = model.errorMessage {
                ToastView(m, actionTitle: "OK") { model.errorMessage = nil }.padding(.bottom, 96)
            }
        }
    }
}

struct ColourScreen: View {
    let model: CalendarsViewModel
    let calendarID: String

    private var calendar: HubCalendar? { model.groups.flatMap(\.calendars).first { $0.id == calendarID } }
    private var account: Account? { model.groups.first { $0.calendars.contains { $0.id == calendarID } }?.account }

    var body: some View {
        SettingsPage(title: calendar?.name ?? "Calendar", sub: account?.email) {
            GroupLabel("Colour", first: true)
            ColourSwatchRow(selection: Binding(
                get: { calendar.map { AccountColor(key: CalendarsViewModel.colorKey($0)) } },
                set: { c in Task { await model.setColor(calendarID, c) } }))
            Hint("By default a calendar uses its account\u{2019}s colour. Pick another to tell it apart.")
            GroupLabel("Preview")
            EventRow(EventRowModel(title: "Dinner with Ana", start: "18:00",
                                   color: AccountColor(key: calendar.map(CalendarsViewModel.colorKey)), isAllDay: false), isFirst: true)
                .accessibilityHidden(true)
            ToggleRow("Use the account colour", isOn: Binding(
                get: { calendar?.colorOverride == nil },
                set: { on in if on { Task { await model.setColor(calendarID, nil) } } }))
        }
    }
}

struct DefaultCalendarScreen: View {
    let model: SettingsViewModel
    let calendars: CalendarsViewModel

    var body: some View {
        SettingsPage(title: "Default calendar", sub: "New events go here unless you pick another.") {
            ForEach(Array(calendars.groups.enumerated()), id: \.element.id) { gi, g in
                let writable = g.calendars.filter(SettingsLogic.isWritable)
                if !writable.isEmpty {
                    GroupLabel(g.account.email, first: gi == 0)
                    ForEach(Array(writable.enumerated()), id: \.element.id) { n, c in
                        Button {
                            model.draft.defaultCalendarId = c.id
                            Task { await model.save() }
                        } label: {
                            HStack(spacing: Spacing.l) {
                                Rectangle().fill(AccountColor(key: CalendarsViewModel.colorKey(c)).color).frame(width: 14, height: 14)
                                Text(c.name).font(CalFont.heading).foregroundStyle(Color.calInk)
                                Spacer(minLength: 0)
                                RadioMark(isOn: model.draft.defaultCalendarId == c.id)
                            }
                            .padding(.horizontal, Spacing.gutter)
                            .frame(maxWidth: .infinity, minHeight: 56, alignment: .leading)
                            .overlay(alignment: .top) { if n != 0 { Rectangle().fill(Color.calLine).frame(height: 1) } }
                            .contentShape(Rectangle())
                        }
                        .buttonStyle(.plain)
                        .accessibilityAddTraits(model.draft.defaultCalendarId == c.id ? .isSelected : [])
                    }
                }
            }
            Hint("Read only calendars are not listed here.")
        }
    }
}

// MARK: Time (g-tz, g-work, g-week)

struct TimeZonesScreen: View {
    let model: SettingsViewModel
    private var main: TimeZone { model.draft.userTzid.flatMap(TimeZone.init(identifier:)) ?? .current }
    private var second: TimeZone? { model.draft.secondaryTzid.flatMap(TimeZone.init(identifier:)) }

    var body: some View {
        SettingsPage(title: "Time zones") {
            GroupLabel("Main", first: true)
            SettingsRow(SettingsLogic.city(main), sub: "Follows this phone", isFirst: true) {
                Text(main.abbreviation() ?? "").font(CalFont.status).foregroundStyle(Color.calInk2)
            }
            GroupLabel("Second column")
            ToggleRow("Show a second time zone", isOn: Binding(
                get: { second != nil },
                set: { on in
                    model.draft.secondaryTzid = on ? (second?.identifier ?? "America/New_York") : nil
                    Task { await model.save() }
                }), isFirst: true)
            if let s = second {
                SettingsLink(.zonePicker) {
                    NavRow(SettingsLogic.city(s), sub: offsetText(s), value: "Change")
                }
                GroupLabel("Preview")
                HStack(alignment: .top, spacing: Spacing.l) {
                    Text("10:30").font(CalFont.time).foregroundStyle(Color.calInk2).frame(width: Spacing.timeColumn, alignment: .leading)
                    VStack(alignment: .leading, spacing: 4) {
                        Text("Roadmap review").font(CalFont.heading).foregroundStyle(Color.calInk)
                        Text("\(previewTime(s)) in \(SettingsLogic.city(s))").font(CalFont.sub).foregroundStyle(Color.calInk2)
                    }
                }
                .padding(.horizontal, Spacing.gutter).padding(.vertical, Spacing.m)
                .frame(maxWidth: .infinity, alignment: .leading)
                .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
                .accessibilityHidden(true)
            }
            Hint("The second zone appears in Day and 3 day views, and as a small line in event details.")
        }
    }

    private func offsetText(_ z: TimeZone) -> String {
        let d = (z.secondsFromGMT() - main.secondsFromGMT()) / 3600
        if d == 0 { return "Same as \(SettingsLogic.city(main))" }
        return "\(abs(d)) hour\(abs(d) == 1 ? "" : "s") \(d < 0 ? "behind" : "ahead")"
    }
    private func previewTime(_ z: TimeZone) -> String {
        let shift = z.secondsFromGMT() - main.secondsFromGMT()
        return SettingsLogic.hhmm((10 * 60 + 30 + shift / 60 + 1440) % 1440)
    }
}

struct ZonePickerScreen: View {
    let model: SettingsViewModel
    @State private var query = ""
    @Environment(\.dismiss) private var dismiss

    private var matches: [String] {
        let all = TimeZone.knownTimeZoneIdentifiers.filter { $0.contains("/") }
        let q = query.trimmingCharacters(in: .whitespaces).lowercased().replacingOccurrences(of: " ", with: "_")
        return q.isEmpty ? Array(all.prefix(60)) : all.filter { $0.lowercased().contains(q) }
    }

    var body: some View {
        SettingsPage(title: "Second time zone") {
            LabeledField("Search") {
                TextField("City or region", text: $query).textInputAutocapitalization(.never).autocorrectionDisabled()
            }
            .padding(.horizontal, Spacing.gutter).padding(.bottom, Spacing.m)
            ForEach(Array(matches.enumerated()), id: \.element) { n, id in
                SheetRow(SettingsLogic.city(TimeZone(identifier: id) ?? .current), detail: id,
                         radio: model.draft.secondaryTzid == id, isFirst: n == 0) {
                    model.draft.secondaryTzid = id
                    Task { await model.save(); dismiss() }
                }
            }
        }
    }
}

struct WorkingHoursScreen: View {
    let model: SettingsViewModel
    @State private var editing: Edge?
    private enum Edge { case from, to }
    private let days = [(1, "Mon"), (2, "Tue"), (3, "Wed"), (4, "Thu"), (5, "Fri"), (6, "Sat"), (0, "Sun")]

    var body: some View {
        SettingsPage(title: "Working hours", sub: "Used when you ask alfather to find free time.") {
            GroupLabel("Days", first: true)
            HStack(spacing: 6) {
                ForEach(days, id: \.0) { d in
                    ChipToggle(d.1, isOn: Binding(get: { model.draft.workingHours.days.contains(d.0) }, set: { _ in model.toggleWorkingDay(d.0) }))
                }
            }
            .padding(.horizontal, Spacing.gutter)
            if let e = model.errors[.workingDays] { Text(e).font(CalFont.hint).foregroundStyle(Color.calRed).padding(.horizontal, Spacing.gutter) }
            GroupLabel("Hours")
            Button { editing = .from } label: {
                SettingsRow("From", isFirst: true) { FlipCells(SettingsLogic.hhmm(model.workStartMinutes), size: .small) }
            }.buttonStyle(.plain)
            Button { editing = .to } label: {
                SettingsRow("To") { FlipCells(SettingsLogic.hhmm(model.workEndMinutes), size: .small) }
            }.buttonStyle(.plain)
            if let e = model.errors[.workingHours] { Text(e).font(CalFont.hint).foregroundStyle(Color.calRed).padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s) }
            Hint("Free time suggestions stay inside these hours.")
        }
        .calSheet(isPresented: Binding(get: { editing != nil }, set: { if !$0 { editing = nil } })) {
            CalSheet(title: editing == .to ? "To" : "From") {
                TimeStepper(minutes: editing == .to
                            ? Binding(get: { model.workEndMinutes }, set: { model.workEndMinutes = $0 })
                            : Binding(get: { model.workStartMinutes }, set: { model.workStartMinutes = $0 }))
                CalButton("Done") { editing = nil }.padding(.horizontal, Spacing.gutter).padding(.top, Spacing.l)
            }
        }
        .onDisappear { Task { await model.save() } }   // validated when leaving the screen
    }
}

struct WeekStartScreen: View {
    let model: SettingsViewModel
    var body: some View {
        SettingsPage(title: "Week starts") {
            ForEach(Array([1, 0, 6].enumerated()), id: \.element) { n, d in
                SheetRow(weekName(d), radio: model.draft.weekStart == d, isFirst: n == 0) {
                    model.draft.weekStart = d
                    Task { await model.save() }
                }
            }
            GroupLabel("Preview")
            HStack(spacing: 0) {
                ForEach(Array(MonthGrid.weekdayNamesForPreview(model.draft.weekStart).enumerated()), id: \.offset) { _, n in
                    Text(n).font(CalFont.font(.sans, size: 12, relativeTo: .caption)).foregroundStyle(Color.calInk2).frame(maxWidth: .infinity)
                }
            }
            .padding(.horizontal, Spacing.s).accessibilityHidden(true)
            Hint("Changes the month grid, the week view and working days.")
        }
    }
}

enum MonthGrid {
    static func weekdayNamesForPreview(_ weekStart: Int) -> [String] {
        let all = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"]
        return (0..<7).map { all[(weekStart + $0) % 7] }
    }
}

// MARK: Alerts (g-alerts) and Appearance (g-look)

struct AlertsScreen: View {
    let model: SettingsViewModel
    @AppStorage(Prefs.alertJoinButton) private var joinButton = true
    @AppStorage(Prefs.alertMovedCancelled) private var moved = true
    @AppStorage(Prefs.alertNeedsSignIn) private var signIn = true
    @AppStorage(Prefs.alertInvitations) private var invitations = false
    @State private var permission: UNAuthorizationStatus = .authorized
    @Environment(\.openURL) private var openURL
    private let options: [(Int, String)] = [(0, "At the start"), (5, "5 minutes"), (10, "10 minutes"), (30, "30 minutes")]

    var body: some View {
        SettingsPage(title: "Alerts") {
            if permission == .notDetermined {
                Button { Task { await AlertPermission.request(); permission = await AlertPermission.status() } } label: {
                    NavRow("Allow alerts", sub: "Reminders arrive on this phone, even offline. iOS asks once.", isFirst: true)
                }
                .buttonStyle(.plain).accessibilityIdentifier("alerts.allow")
            } else if permission == .denied {
                Button { if let u = URL(string: UIApplication.openSettingsURLString) { openURL(u) } } label: {
                    NavRow("Alerts are off in iOS Settings", sub: "Open Settings to turn them on.", isFirst: true)
                }
                .buttonStyle(.plain)
            }
            GroupLabel("Before an event", first: permission == .authorized || permission == .provisional)
            ForEach(Array(options.enumerated()), id: \.element.0) { n, o in
                SheetRow(o.1, radio: (model.draft.alertDefaults?.timed ?? 10) == o.0, isFirst: n == 0) {
                    var a = model.draft.alertDefaults ?? AlertDefaults()
                    a.timed = o.0
                    model.draft.alertDefaults = a
                    Task { await model.save() }
                }
            }
            GroupLabel("Also tell me")
            ToggleRow("Join button on the alert", sub: "When an event has a call link", isOn: $joinButton, isFirst: true)
            ToggleRow("When an event moves or is cancelled", isOn: $moved)
            ToggleRow("When an account needs sign in", isOn: $signIn)
            ToggleRow("New invitations", isOn: $invitations)
            Hint("Alerts are scheduled on this phone from your saved agenda, so they still arrive offline. Switched-off kinds arrive quietly, without sound.")
        }
        .task { permission = await AlertPermission.status() }
    }
}

struct AppearanceScreen: View {
    @AppStorage(Prefs.theme) private var theme = "system"
    @AppStorage(Prefs.flipAnimation) private var flip = true
    @AppStorage(Prefs.haptics) private var haptics = true
    @AppStorage(Prefs.showDeclined) private var declined = false
    @AppStorage(Prefs.showWeekNumbers) private var weekNumbers = false
    @AppStorage(Prefs.time24) private var time24 = true
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    private let themes = ["system", "dark", "light"]

    var body: some View {
        SettingsPage(title: "Appearance") {
            GroupLabel("Theme", first: true)
            SegmentedControl(["System", "Dark", "Light"], selection: Binding(
                get: { themes.firstIndex(of: theme) ?? 0 }, set: { theme = themes[$0] }))
                .padding(.horizontal, Spacing.gutter)
            GroupLabel("Feel")
            ToggleRow("Haptics", sub: "Taps and ticks on snaps, saves and replies", isOn: $haptics, isFirst: true)
            GroupLabel("Flip cells")
            ToggleRow("Animate changes", sub: "Cells flip when a time changes", isOn: $flip, isFirst: true)
            SettingsRow("Reduce motion", sub: "Follows the phone setting") {
                Text(reduceMotion ? "On" : "Off").font(CalFont.status).foregroundStyle(Color.calInk2)
            }
            GroupLabel("Agenda")
            ToggleRow("Show declined events", isOn: $declined, isFirst: true)
            ToggleRow("Show week numbers", isOn: $weekNumbers)
            ToggleRow("24 hour time", isOn: $time24)
        }
    }
}
