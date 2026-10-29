import CalendarCore
import CalendarUI
import SwiftUI

/// The Home view select: Agenda, Day, 3 days.
public enum HomeMode: Int, CaseIterable, Sendable {
    case agenda, day, three
    static let titles = ["Agenda", "Day", "3 days"]
}

/// What the New sheet opens with.
public enum NewEntry: Equatable, Sendable {
    /// Quick add.
    case blank
    /// The full form at this start (long-press on an empty hour).
    case at(Date)
    /// The draft kept by "Keep as draft".
    case draft
    public var start: Date? { if case .at(let d) = self { d } else { nil } }
}

/// Typed navigation for the single `NavigationStack` (no tab bar).
public enum HomeRoute: Hashable, Sendable {
    case event(String)
    case accounts
    case settings
    case search
}

/// Where an outside tap (widget, notification, Live Activity) wants Home to go.
public enum HomeLink: Equatable, Sendable {
    case today
    case event(String)
    case accounts
    /// `calendar://new`: the new-event sheet.
    case newEvent
}

/// The app sets `pending`; `HomeRoot` consumes it (also when it was set before Home existed, e.g. cold launch).
@MainActor @Observable
public final class HomeNavigator {
    public var pending: HomeLink?
    public init(pending: HomeLink? = nil) { self.pending = pending }
}

/// Home plus its navigation: the app root embeds this and supplies the Accounts screen.
public struct HomeRoot<AccountsScreen: View, EventScreen: View, SettingsScreen: View, NewScreen: View>: View {
    @State private var model: HomeViewModel
    @State private var path: [HomeRoute]
    @State private var showNew = false
    @Namespace private var zoomNS
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var newEntry: NewEntry = .blank
    @State private var day: DayViewModel
    @AppStorage("home.mode") private var modeRaw = HomeMode.agenda.rawValue
    @AppStorage(DisplayPrefs.Key.flipAnimation) private var prefFlip = true
    @AppStorage(DisplayPrefs.Key.showDeclined) private var prefDeclined = false
    @AppStorage(DisplayPrefs.Key.showWeekNumbers) private var prefWeeks = false
    @AppStorage(DisplayPrefs.Key.time24) private var prefTime24 = true
    private let accountsScreen: () -> AccountsScreen
    private let eventScreen: (String) -> EventScreen
    private let settingsScreen: (@escaping () -> Void) -> SettingsScreen
    private let newScreen: (NewEntry, @escaping () -> Void) -> NewScreen
    private var draftTitle: (() -> String?)?
    private let onForget: () -> Void
    private var moveEvent: ((AgendaItem, EventWhen, EventScope) async -> WriteOutcome)?
    private var navigator: HomeNavigator?
    private var searchScreen: ((_ openEvent: @escaping (String) -> Void, _ close: @escaping () -> Void) -> AnyView)?

    public init(env: AppEnvironment, startOnAccounts: Bool = false, startOnSettings: Bool = false, onForget: @escaping () -> Void,
                @ViewBuilder accounts: @escaping () -> AccountsScreen,
                @ViewBuilder event: @escaping (String) -> EventScreen,
                @ViewBuilder settings: @escaping (@escaping () -> Void) -> SettingsScreen,
                @ViewBuilder new: @escaping (NewEntry, @escaping () -> Void) -> NewScreen) {
        _model = State(initialValue: HomeViewModel(deps: HomeDependencies(env: env)))
        _day = State(initialValue: DayViewModel(agenda: env.agenda, settings: env.settings, clock: env.clock))
        _path = State(initialValue: startOnAccounts ? [.accounts] : startOnSettings ? [.settings] : [])
        accountsScreen = accounts
        eventScreen = event
        settingsScreen = settings
        newScreen = new
        self.onForget = onForget
    }

    /// Grid drag/resize writes (optimistic) go through this.
    public func onMoveEvent(_ handler: @escaping (AgendaItem, EventWhen, EventScope) async -> WriteOutcome) -> Self {
        var copy = self; copy.moveEvent = handler; return copy
    }

    /// Search (f-open): the app supplies the screen; Home pushes it from the header glyph and opens the tapped event.
    public func search(_ screen: @escaping (_ openEvent: @escaping (String) -> Void, _ close: @escaping () -> Void) -> AnyView) -> Self {
        var copy = self; copy.searchScreen = screen; return copy
    }

    /// The saved "Keep as draft" event, if any: Home lists it as a row. `title` is "" for an untitled draft.
    public func draft(_ title: @escaping () -> String?) -> Self {
        var copy = self; copy.draftTitle = title; return copy
    }

    /// Lets the app steer Home from widget / notification links.
    public func navigator(_ nav: HomeNavigator) -> Self {
        var copy = self; copy.navigator = nav; return copy
    }

    private var prefs: DisplayPrefs {
        DisplayPrefs(flipAnimation: prefFlip, showDeclined: prefDeclined, showWeekNumbers: prefWeeks, time24: prefTime24)
    }

    private func pushPrefs() { model.apply(prefs: prefs); day.apply(prefs: prefs) }

    private func consumeLink() {
        guard let link = navigator?.pending else { return }
        navigator?.pending = nil
        showNew = false
        switch link {
        case .today: path = []
        case .event(let id): path = [.event(id)]
        case .accounts: path = [.accounts]
        case .newEvent: path = []; newEntry = .blank; showNew = true
        }
    }

    public var body: some View {
        NavigationStack(path: $path) {
            HomeView(model: model, day: day, mode: Binding(get: { HomeMode(rawValue: modeRaw) ?? .agenda }, set: { modeRaw = $0.rawValue }),
                     openEvent: { path.append(.event($0)) }, openAccounts: { path.append(.accounts) },
                     openSettings: { path.append(.settings) },
                     openSearch: searchScreen == nil ? nil : { path.append(.search) },
                     newEventAt: { newEntry = $0.map(NewEntry.at) ?? .blank; showNew = true },
                     draft: draftTitle?(), openDraft: { newEntry = .draft; showNew = true }, forget: onForget, moveEvent: moveEvent, zoomNS: zoomNS)
                .task { await day.run() }
                .navigationDestination(for: HomeRoute.self) { route in
                    switch route {
                    case .event(let id): eventScreen(id)
                    case .accounts: accountsScreen()
                    case .settings: settingsScreen { path.append(.accounts) }
                    case .search:
                        if let searchScreen { searchScreen({ path.append(.event($0)) }, { if !path.isEmpty { path.removeLast() } }) }
                    }
                }
        }
        .sheet(isPresented: $showNew) {
            newScreen(newEntry) { showNew = false }
                .presentationDetents([.large])
        }
        .overlay(alignment: .bottom) {
            if let t = ToastCenter.shared.current {
                ToastView(t.message, detail: t.detail, actionTitle: t.actionTitle) { t.action?(); ToastCenter.shared.dismiss() }
                    .padding(.bottom, 96).transition(.opacity)
            }
        }
        .tint(Color.calInk)
        .onAppear { pushPrefs(); consumeLink() }
        .onChange(of: prefs) { _, _ in pushPrefs() }
        .onChange(of: navigator?.pending) { _, _ in consumeLink() }
    }
}
