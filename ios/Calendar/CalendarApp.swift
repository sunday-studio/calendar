import CalendarCore
import CalendarUI
import FeatureAccounts
import FeatureEvent
import FeatureHome
import FeatureSettings
import SurfacesCore
import SurfacesUI
import SwiftUI

@main
struct CalendarApp: App {
    @UIApplicationDelegateAdaptor(AppDelegate.self) private var appDelegate

    var body: some Scene {
        WindowGroup {
            #if DEBUG
            if CommandLine.arguments.contains("-gallery") {
                ComponentGallery()
            } else if CommandLine.arguments.contains("-uiSettings") {
                SettingsUITestHost()
            } else if CommandLine.arguments.contains("-widgetGallery") {
                WidgetGallery()
                    .preferredColorScheme(CommandLine.arguments.contains("-dark") ? .dark : .light)
            } else if CommandLine.arguments.contains("-uiHome") {
                HomeUITestHost()
            } else if CommandLine.arguments.contains("-uiEditor") {
                EditorUITestHost()
            } else {
                RootView()
            }
            #else
            RootView()
            #endif
        }
    }
}

struct RootView: View {
    @State private var config: HubConfig? = RootView.initialConfig()
    @State private var pairing = PairingViewModel()
    @State private var openAddAccount = false
    @State private var auth = SystemWebAuthSession()
    @AppStorage(Prefs.theme) private var theme = "system"
    @Environment(\.scenePhase) private var scenePhase

    /// The stored pairing. DEBUG builds also take `-e2eReset` (forget the pairing) and `-e2eHub <url> -e2eToken <token>`
    /// (store one) so UI tests against the real hub (scripts/ios-e2e.sh) can start from a known state.
    static func initialConfig() -> HubConfig? {
        #if DEBUG
        let args = CommandLine.arguments
        if args.contains("-e2eReset") { try? HubConfigStore().clear() }
        func value(_ flag: String) -> String? { args.firstIndex(of: flag).flatMap { args.indices.contains($0 + 1) ? args[$0 + 1] : nil } }
        if let url = value("-e2eHub").flatMap(URL.init(string:)), let token = value("-e2eToken") {
            try? HubConfigStore().save(HubConfig(baseURL: url, token: token))
        }
        #endif
        return HubConfigStore().load()
    }

    var body: some View {
        Group {
            if let config {
                AuthedRoot(config: config, auth: auth, startWithAddSheet: openAddAccount) {
                    // Forget: unregister push while the token still works, then clear pairing and every surface.
                    Task { @MainActor in
                        await PushCoordinator.shared.forget(config: config)
                        try? HubConfigStore().clear()
                        self.config = nil
                        openAddAccount = false
                        pairing = PairingViewModel()
                    }
                }
            } else {
                PairingFlowView(model: pairing) { cfg, addAccount in
                    openAddAccount = addAccount
                    config = cfg
                }
            }
        }
        .preferredColorScheme(Prefs.scheme(theme))
        .onChange(of: scenePhase) { _, phase in
            switch phase {
            case .background: AppServices.shared.scheduleAppRefresh()
            case .active:
                Task {
                    await PushCoordinator.shared.model?.registerIfNeeded()
                    await AppServices.shared.current()?.live.refresh()
                }
            default: break
            }
        }
        .onOpenURL { url in
            // Widgets, notifications and the Live Activity open calendar://event/<id>, today, accounts, summary.
            if AppRouter.shared.open(url) { return }
            // calendar://pair?host=&token= prefills pairing; calendar://connected is consumed by the sign-in sheet.
            if config == nil, let link = DeepLink.parse(url) { pairing.apply(link) }
        }
    }
}

/// Authenticated shell: builds the app environment for the paired hub and shows Home.
struct AuthedRoot: View {
    let config: HubConfig
    let auth: SystemWebAuthSession
    let startWithAddSheet: Bool
    let unpair: () -> Void
    @State private var env: AppEnvironment
    @State private var writer: WriteCoordinator
    @State private var scheduler: ReminderScheduler
    private let api: APIClient

    init(config: HubConfig, auth: SystemWebAuthSession, startWithAddSheet: Bool, unpair: @escaping () -> Void) {
        self.config = config; self.auth = auth; self.startWithAddSheet = startWithAddSheet; self.unpair = unpair
        self.api = APIClient(config: config)
        // One environment per hub, shared with background work; hooks reload widgets, reschedule reminders, update the Live Activity.
        let services = AppServices.shared.services(for: config)
        let env = services.env
        let db = env.db
        _scheduler = State(initialValue: services.scheduler)
        _env = State(initialValue: env)
        let sync = env.sync
        _writer = State(initialValue: WriteCoordinator(api: APIClient(config: config), db: db, refresh: { _ = await sync.silentPush() }, clock: env.clock))
    }

    var body: some View {
        HomeRoot(env: env, startOnAccounts: startWithAddSheet, onForget: unpair) {
            AccountsScreen(api: APIClient(config: config), auth: auth, startWithAddSheet: startWithAddSheet)
        } event: { id in
            EventScreenHost(id: id, env: env, writer: writer)
        } settings: { openAccounts in
            SettingsScreen(env: env, tokens: api, openAccounts: openAccounts, onLogOut: unpair)
        } new: { entry, close in
            NewEntryHost(entry: entry, env: env, writer: writer, api: api, onClose: close)
        }
        .draft { EventEditorViewModel.loadDraft().map(\.title) }
        .search { openEvent, close in
            AnyView(SearchHost(env: env, api: api, writer: writer, openEvent: openEvent, close: close))
        }
        .navigator(AppRouter.shared.navigator)
        .onMoveEvent { item, when, scope in
            var patch = EventPatch(); patch.when = .set(when)
            return await writer.update(item, patch: patch, scope: scope, mode: .optimistic)
        }
        .task { await PushCoordinator.shared.start(config: config) }
        .task {
            // Settings change (alert defaults): reschedule reminders. The first value replays the current settings.
            for await _ in env.settings.observeSettings() { await scheduler.reschedule() }
        }
        .task {
            // The Join-button preference lives in UserDefaults: reschedule when it flips.
            var last = AppServices.showJoinButton
            AlertPrefs.mirror()   // the notification extension reads the App Group copy
            for await _ in NotificationCenter.default.notifications(named: UserDefaults.didChangeNotification).map({ _ in () }) {
                AlertPrefs.mirror()
                let now = AppServices.showJoinButton
                if now != last { last = now; await scheduler.reschedule() }
            }
        }
    }
}

// MARK: - Event screens (detail + editor hosts)

/// Event detail plus the editor it can open.
struct EventScreenHost: View {
    let id: String
    let env: AppEnvironment
    let writer: WriteCoordinator
    @State private var model: EventDetailViewModel
    @State private var editing: EventDetail?
    @State private var duplicating: EventDetail?
    @State private var moving: EventDetail?

    init(id: String, env: AppEnvironment, writer: WriteCoordinator) {
        self.id = id; self.env = env; self.writer = writer
        _model = State(initialValue: EventDetailViewModel(id: id, repo: env.eventDetail, clock: env.clock,
                                                          tz: (try? env.db.userTimeZone()) ?? .current,
                                                          locale: DisplayPrefs.load().locale()))
    }

    var body: some View {
        EventDetailView(model: model, writer: writer, onEdit: { editing = $0 }, onDuplicate: { duplicating = $0 }, onMove: { moving = $0 })
            .task {
                let cals = (try? await env.calendars.calendars()) ?? []
                let accts = (try? await env.accounts.accounts()) ?? []
                model.setContext(calendars: cals, accounts: accts)
            }
            .fullScreenCover(item: $editing) { d in
                EditorHost(kind: .edit(d), env: env, writer: writer) { saved in
                    editing = nil
                    if saved { Task { await model.refresh() } }
                }
            }
            .fullScreenCover(item: $duplicating) { d in
                EditorHost(kind: .duplicate(d), env: env, writer: writer) { _ in duplicating = nil }
            }
            .fullScreenCover(item: $moving) { d in
                MoveHost(detail: d, env: env, writer: writer) { moved in
                    moving = nil
                    if moved { Task { await model.refresh() } }
                }
            }
    }
}

/// Builds the editor view model from the local database (calendars, defaults), then shows the form.
struct EditorHost: View {
    enum Kind { case new(Date?), edit(EventDetail), duplicate(EventDetail), prefill(EventDraft) }
    let kind: Kind
    let env: AppEnvironment
    let writer: WriteCoordinator
    let onClose: (Bool) -> Void
    @State private var model: EventEditorViewModel?
    @State private var names: [String: String] = [:]

    var body: some View {
        Group {
            if let model { EventEditorView(model: model, accountNames: names, onClose: onClose) }
            else { Color.calBg.ignoresSafeArea() }
        }
        // On the host, not the form: the sheet is already the right colour and square while it rises.
        .presentationBackground(Color.calBg)
        .presentationCornerRadius(0)
        .presentationDragIndicator(.hidden)
        .task {
            let cals = (try? await env.calendars.calendars()) ?? []
            let settings = try? await env.settings.settings()
            let accts = (try? await env.accounts.accounts()) ?? []
            names = Dictionary(uniqueKeysWithValues: accts.map { ($0.id, $0.email) })
            let tz = (try? env.db.userTimeZone()) ?? .current
            switch kind {
            case .new(let start):
                let m = EventEditorViewModel(create: start ?? Self.nextQuarter(env.clock.now()), calendars: cals,
                                             defaultCalendarId: settings?.defaultCalendarId, tz: tz, writer: writer)
                m.applyDefaults(reminderMin: settings?.alertDefaults?.timed ?? 10)
                model = m
            case .edit(let d):
                model = EventEditorViewModel(edit: d, calendars: cals, tz: tz, writer: writer)
            case .duplicate(let d):
                model = EventEditorViewModel(duplicating: d, calendars: cals, tz: tz, writer: writer)
            case .prefill(let d):
                var m = d
                if m.reminderMin == nil { m.reminderMin = settings?.alertDefaults?.timed ?? 10 }
                model = EventEditorViewModel(prefill: m, calendars: cals, defaultCalendarId: settings?.defaultCalendarId, tz: tz, writer: writer)
            }
        }
    }

    static func nextQuarter(_ now: Date) -> Date {
        let q = 15 * 60.0
        return Date(timeIntervalSince1970: (now.timeIntervalSince1970 / q).rounded(.up) * q)
    }
}


#if DEBUG
/// UI-test harness: Home's real navigation stack with Settings on top, on an in-memory sample database.
struct SettingsUITestHost: View {
    @State private var env = try! PreviewEnvironment.sample()
    var body: some View {
        HomeRoot(env: env, startOnSettings: true, onForget: {}) {
            Text("Accounts")
        } event: { _ in
            EmptyView()
        } settings: { openAccounts in
            SettingsScreen(env: env, openAccounts: openAccounts)
        } new: { _, _ in
            EmptyView()
        }
    }
}
#endif


/// Loads calendars and account names, then shows the Move screen.
struct MoveHost: View {
    let detail: EventDetail
    let env: AppEnvironment
    let writer: WriteCoordinator
    let onClose: (Bool) -> Void
    @State private var cals: [HubCalendar] = []
    @State private var names: [String: String] = [:]

    var body: some View {
        MoveCalendarView(item: detail.item, calendars: cals, accountNames: names, writer: writer, onClose: onClose)
            .task {
                cals = (try? await env.calendars.calendars()) ?? []
                names = Dictionary(uniqueKeysWithValues: ((try? await env.accounts.accounts()) ?? []).map { ($0.id, $0.email) })
            }
    }
}


#if DEBUG
/// UI-test harness for the accessibility audit: Home with its real navigation (event detail, editor, settings) on the
/// in-memory sample agenda (same shape as the fixture hub's), writes go nowhere.
/// Event detail for the UI-test harness: the sample agenda item plus guests, a note and a Meet link, no network.
struct UITestDetailRepository: EventDetailRepository {
    let agenda: any AgendaRepository
    func cached(id: String) async throws -> EventDetail? { try? await detail(id: id) }
    func refresh(id: String) async throws -> EventDetail { try await detail(id: id) }
    func detail(id: String) async throws -> EventDetail {
        guard var item = try await agenda.item(id: id) else { throw APIClientError.transport("not found") }
        item.location = "Room 4"
        item.join = Join(url: "https://meet.google.com/abc-defg-hij", kind: "meet")
        return EventDetail(
            item: item, description: "Agenda: roadmap, hiring, launch dates.",
            organizer: Person(email: "sam@example.com", name: "Sam Rivera"),
            attendees: [Attendee(email: "alex@example.com", name: "Alex", response: "accepted", isSelf: true),
                        Attendee(email: "sam@example.com", name: "Sam Rivera", response: "accepted", isOrganizer: true),
                        Attendee(email: "kim@example.com", name: "Kim Lee", response: "needsAction")],
            reminderMin: 10, calendarName: "Work", accountName: "Alex")
    }
}

struct HomeUITestHost: View {
    @State private var env: AppEnvironment = {
        var e = try! PreviewEnvironment.sample()
        e.eventDetail = UITestDetailRepository(agenda: e.agenda)
        return e
    }()
    @State private var writer: WriteCoordinator?

    var body: some View {
        let w = writer ?? WriteCoordinator(api: APIClient(config: HubConfig(baseURL: URL(string: "http://127.0.0.1:9")!, token: "ui")),
                                           db: env.db, refresh: {}, clock: env.clock)
        HomeRoot(env: env, onForget: {}) {
            Text("Accounts")
        } event: { id in
            EventScreenHost(id: id, env: env, writer: w)
        } settings: { openAccounts in
            SettingsScreen(env: env, openAccounts: openAccounts)
        } new: { entry, close in
            EditorHost(kind: .new(entry.start), env: env, writer: w) { _ in close() }
        }
        .onAppear { writer = w }
    }
}

/// UI-test harness for the editor: the real form and view model with a scripted writer (success, or a provider refusal).
struct EditorUITestHost: View {
    @State private var result: String?
    private let fail = CommandLine.arguments.contains("-uiEditorFail")

    private struct ScriptedWriter: EventWriting {
        let fail: Bool
        private func out() -> WriteOutcome { fail ? .failed(.provider("Quota exceeded.")) : .saved(nil) }
        func create(_ draft: EventDraft, key: String?, mode: WriteMode) async -> WriteOutcome { out() }
        func update(_ item: AgendaItem, patch: EventPatch, scope: EventScope, key: String?, mode: WriteMode) async -> WriteOutcome { out() }
        func delete(_ item: AgendaItem, scope: EventScope, key: String?, mode: WriteMode) async -> WriteOutcome { .saved(nil) }
        func rsvp(_ item: AgendaItem, response: RSVPResponse, note: String?, scope: EventScope, key: String?, mode: WriteMode) async -> WriteOutcome { .saved(nil) }
        func addMeet(_ item: AgendaItem, key: String?) async -> WriteOutcome { .saved(nil) }
        func move(_ item: AgendaItem, toCalendar calendarId: String, key: String?, mode: WriteMode) async -> WriteOutcome { .saved(nil) }
        func setReminder(_ item: AgendaItem, minutesBefore: Int?) async -> WriteOutcome { .saved(nil) }
    }

    var body: some View {
        if let result {
            Text(result).accessibilityIdentifier("ui.result")
        } else {
            let cal = HubCalendar(id: "cal1", accountId: "acc1", name: "Work", color: "cobalt", isPrimary: true)
            let vm = EventEditorViewModel(create: Date().addingTimeInterval(3600), calendars: [cal], defaultCalendarId: "cal1",
                                          tz: .current, writer: ScriptedWriter(fail: fail))
            EventEditorView(model: vm, accountNames: ["acc1": "alex@example.com"]) { saved in
                result = saved ? "Saved: \(vm.title)" : (EventEditorViewModel.loadDraft() != nil ? "Draft kept" : "Closed")
            }
        }
    }
}
#endif

/// Screens hide the system navigation bar and draw their own, which turns the edge-swipe back off.
/// Keep the gesture alive whenever there is something to pop to.
extension UINavigationController: @retroactive UIGestureRecognizerDelegate {
    override open func viewDidLoad() {
        super.viewDidLoad()
        interactivePopGestureRecognizer?.delegate = self
    }

    public func gestureRecognizerShouldBegin(_ gestureRecognizer: UIGestureRecognizer) -> Bool {
        viewControllers.count > 1
    }
}
