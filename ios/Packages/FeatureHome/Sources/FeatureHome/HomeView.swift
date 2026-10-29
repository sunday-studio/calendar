import CalendarCore
import CalendarUI
import SwiftUI

struct HomeView: View {
    @Bindable var model: HomeViewModel
    let day: DayViewModel
    @Binding var mode: HomeMode
    let openEvent: (String) -> Void
    let openAccounts: () -> Void
    let openSettings: () -> Void
    var openSearch: (() -> Void)?
    let newEventAt: (Date?) -> Void
    /// Title of the saved draft ("" = untitled), nil when there is none.
    var draft: String?
    var openDraft: () -> Void
    let forget: () -> Void
    var moveEvent: ((AgendaItem, EventWhen, EventScope) async -> WriteOutcome)?
    var zoomNS: Namespace.ID?

    private func newEvent() { newEventAt(nil) }

    @State private var showMenu = false
    @State private var showViewSelect = false
    @State private var reviewTick = 0
    @State private var writing = false
    @State private var pendingMove: PendingMove?
    @State private var moveScope: EventScope = .this

    struct PendingMove: Equatable { var item: AgendaItem; var start: Date; var end: Date }
    @State private var offlineToast = false
    @State private var confirmForget = false
    @Environment(\.scenePhase) private var scenePhase
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @Environment(\.dynamicTypeSize) private var typeSize
    @Environment(\.openURL) private var openURL

    var body: some View {
        ZStack(alignment: .bottomTrailing) {
            VStack(spacing: 0) {
                titleRow
                if model.showProgress || writing { ProgressRule().accessibilityLabel(writing ? "Saving" : "Syncing") }
                ZStack {
                    if mode == .agenda { agendaScroll.transition(.opacity) }
                    else { boardArea.id(mode).transition(.opacity) }
                }
                .animation(Motion.animation(reduceMotion: reduceMotion), value: mode)
            }
            if model.connectionBanner != nil {
                offlineFab
            } else {
                Fab(action: { Haptics.shared.play(.fab); newEvent() })
            }
            if offlineToast {
                ToastView("Can\u{2019}t edit while offline", actionTitle: "OK") { offlineToast = false }
                    .frame(maxHeight: .infinity, alignment: .bottom).padding(.bottom, 96)
                    .zIndex(2)
                    .task { try? await Task.sleep(for: .seconds(2.5)); offlineToast = false }
            }
        }
        .background(Color.calBg)
        .toolbar(.hidden, for: .navigationBar)
        .task { await model.run() }
        .onChange(of: scenePhase, initial: true) { _, phase in
            Task { await model.sceneChanged(active: phase == .active) }
        }
        .settle(value: model.isMonthOpen)
        .onChange(of: showMenu) { _, on in if on { Haptics.shared.play(.sheetOpen) } }
        .onChange(of: showViewSelect) { _, on in if on { Haptics.shared.play(.sheetOpen) } }
        .calSheet(isPresented: $showViewSelect) {
            CalSheet(title: "View") {
                ForEach(Array(HomeMode.titles.enumerated()), id: \.offset) { n, title in
                    SheetRow(title, radio: mode.rawValue == n, isFirst: n == 0) { modeIndex.wrappedValue = n; showViewSelect = false }
                }
            }
        }
        .calSheet(isPresented: $showMenu) {
            CalSheet(title: "Calendar") {
                SheetRow("Go to today", isFirst: true) { showMenu = false; Haptics.shared.play(.today); model.goToToday() }
                SheetRow("Settings") { showMenu = false; openSettings() }
                SheetRow("Accounts") { showMenu = false; openAccounts() }
                SheetRow("Log out", isDestructive: true) { showMenu = false; confirmForget = true; Haptics.shared.play(.destructive) }
            }
        }
        .calSheet(isPresented: Binding(get: { pendingMove != nil }, set: { if !$0 { pendingMove = nil } })) {
            CalSheet(title: "Save changes to") {
                SheetRow("This event", radio: moveScope == .this, isFirst: true) { moveScope = .this }
                SheetRow("This and following", radio: moveScope == .following) { moveScope = .following }
                SheetRow("All events", radio: moveScope == .all) { moveScope = .all }
                VStack(spacing: 10) {
                    CalButton("Save") { if let m = pendingMove { pendingMove = nil; perform(m, scope: moveScope) } }
                    CalButton("Cancel", kind: .outline) { pendingMove = nil }
                }.padding(.horizontal, Spacing.gutter).padding(.top, Spacing.m)
            }
        }
        .calSheet(isPresented: $confirmForget) {
            CalSheet(title: "Log out?", message: "This phone stops showing your calendars until you pair it again. Nothing is deleted on the server.") {
                VStack(spacing: 10) {
                    CalButton("Log out", kind: .danger) { confirmForget = false; Haptics.shared.play(.removed); forget() }
                    CalButton("Stay logged in", kind: .outline) { confirmForget = false }
                }
                .padding(.horizontal, Spacing.gutter)
            }
        }
    }

    // MARK: Drag / resize writes (optimistic; roll back with a toast)

    private func commit(_ m: PendingMove) {
        if m.item.recurring { moveScope = .this; pendingMove = m } else { perform(m, scope: .this) }
    }

    private func perform(_ m: PendingMove, scope: EventScope) {
        guard let moveEvent, let tz = Optional(model.tz),
              let when = EventWhen.timed(start: m.start, end: m.end, tz: tz) else { return }
        writing = true
        Task {
            let out = await moveEvent(m.item, when, scope)
            writing = false
            guard case .failed(let f) = out else { return }
            Haptics.shared.play(.failure)
            let back = m.item.start.map { model.format.time($0) } ?? "its slot"
            let email = model.accounts.first { $0.id == m.item.accountId }?.email ?? "Your calendar"
            let why: String = {
                switch f {
                case .conflict: return "It was changed on Google since you opened it."
                case .offline: return "Can\u{2019}t reach your server."
                case .provider(let msg): return msg
                default: return "\(email) refused the change."
                }
            }()
            ToastCenter.shared.show("\(m.item.title) went back to \(back).", detail: why, actionTitle: "Try again", seconds: 5) { perform(m, scope: scope) }
        }
    }

    private var modeIndex: Binding<Int> {
        Binding(get: { mode.rawValue }, set: { v in
            if v != mode.rawValue { Haptics.shared.play(.viewSwitch) }
            mode = HomeMode(rawValue: v) ?? .agenda
        })
    }

    private var agendaScroll: some View {
        ScrollViewReader { proxy in
            ScrollView {
                LazyVStack(alignment: .leading, spacing: 0) {
                    Color.clear.frame(height: 0).id(Anchor.top)
                    content
                }
            }
            .refreshable { await Task { await model.refresh() }.value; Haptics.shared.play(.refreshDone) }
            .onChange(of: reviewTick) { _, _ in withAnimation(Motion.animation(reduceMotion: reduceMotion)) { proxy.scrollTo("invitations", anchor: .top) } }
            .onChange(of: model.scrollTarget) { _, target in
                guard let target else { return }
                let id: AnyHashable = target == model.today ? Anchor.top : target
                withAnimation(Motion.animation(reduceMotion: reduceMotion)) { proxy.scrollTo(id, anchor: .top) }
                model.scrollTarget = nil
            }
        }
    }

    private var boardLayout: BoardLayout { mode == .day ? .day : .three }

    private var boardDays: [DayKey] {
        let d = model.selectedDay
        switch mode {
        case .agenda, .day: return [d]
        case .three: return [d, d.adding(days: 1), d.adding(days: 2)]
        }
    }

    private var boardArea: some View {
        VStack(spacing: 0) {
            banners
            if model.isMonthOpen {
                MonthGridView(model: model).padding(.top, Spacing.l)
                    .transition(Motion.transition(reduceMotion: reduceMotion, edge: .top))
            }
            TimelineBoard(model: day, layout: boardLayout, days: boardDays, openEvent: openEvent,
                          newAt: { newEventAt($0) },
                          shift: { n in Haptics.shared.play(.dayPage); model.select(model.selectedDay.adding(days: n)) },
                          canWrite: model.connectionBanner == nil && moveEvent != nil,
                          onCommit: { item, start, end in commit(PendingMove(item: item, start: start, end: end)) },
                          onBlocked: { ToastCenter.shared.show($0, actionTitle: "OK", seconds: 2.5) },
                          zoomNS: zoomNS)
        }
    }

    /// The + while the hub is unreachable: drawn dim and answering with the "can't edit" toast. (Dimming the shared `Fab` with
    /// `.opacity` made its tap fall through to the row beneath, so the dim version is drawn here.)
    private var offlineFab: some View {
        Color.clear
            .overlay(alignment: .bottomTrailing) {
                Button { offlineToast = true } label: {
                    Rectangle().fill(Color.calInk.opacity(0.35))
                        .frame(width: Spacing.fab, height: Spacing.fab)
                        .overlay { CalGlyph(.plus, size: 18).foregroundStyle(Color.calBg) }
                }
                .buttonStyle(.plain)
                .padding(.trailing, Spacing.gutter).padding(.bottom, 28)
                .accessibilityLabel("New event").accessibilityHint("Unavailable while offline").accessibilityIdentifier("fab")
            }
            .ignoresSafeArea(.container, edges: .bottom)
    }

    private enum Anchor: Hashable { case top }

    // MARK: Title row

    private var titleText: String {
        if model.isMonthOpen { return "\(model.gridMonthTitle) \(model.gridYear)" }
        if mode == .day { return model.format.shortDate(model.selectedDay) }
        return "\(model.titleMonth) \(model.titleYear)"
    }

    private var titleRow: some View {
        HStack(spacing: Spacing.s) {
            Button { Haptics.shared.play(.monthToggle); model.toggleMonth() } label: {
                HStack(spacing: Spacing.s) {
                    Text(titleText).font(CalFont.title).foregroundStyle(Color.calInk).lineLimit(1).minimumScaleFactor(0.7)
                    CalGlyph(model.isMonthOpen ? .up : .down, size: 14).foregroundStyle(Color.calInk2)
                }
                .frame(minHeight: Spacing.hit, alignment: .leading)
                .contentShape(Rectangle())
            }
            .buttonStyle(.plain)
            .accessibilityLabel(titleText)
            .accessibilityHint(model.isMonthOpen ? "Hides the month" : "Shows the month")
            .accessibilityIdentifier("home.month")
            Spacer(minLength: Spacing.s)
            if let openSearch {
                Button(action: { Haptics.shared.play(.rowTap); openSearch() }) { SearchGlyph().scaleEffect(0.8).frame(width: Spacing.hit, height: Spacing.hit).contentShape(Rectangle()) }
                    .buttonStyle(.plain).accessibilityLabel("Search").accessibilityIdentifier("home.search")
            }
            // Settings has no home on the canvas yet (DECISIONS Q5): the date button opens the menu (Today, Settings, Accounts).
            SelectField(value: HomeMode.titles[mode.rawValue]) { showViewSelect = true }
                .accessibilityLabel("View, \(HomeMode.titles[mode.rawValue])")
                .accessibilityIdentifier("home.viewSelect")
                .padding(.trailing, 8 - Spacing.hit + Spacing.todayButton)   // gap 8 to the date button
            TodayButton(day: model.todayNumber) { showMenu = true }
                // The canvas draws a 38pt square; the touch target is 44pt (HIG) without moving the drawn box.
                .frame(minWidth: Spacing.hit, minHeight: Spacing.hit).contentShape(Rectangle())
                .padding(.trailing, -3).padding(.vertical, -3)
                .accessibilityAction(named: "Settings", openSettings)
                .accessibilityAction(named: "Accounts", openAccounts)
                                .accessibilityIdentifier("home.today")
        }
        .padding(.leading, Spacing.gutter).padding(.trailing, Spacing.gutter - 3)
        .padding(.top, Spacing.xs)
    }

    // MARK: Content

    @ViewBuilder private var banners: some View {
        if let b = model.signInBanner {
            AmberBanner(b.title, message: b.message, actionTitle: "Fix", action: openAccounts)
                .padding(.top, 16)
                .accessibilityIdentifier("home.banner")
        }
        if let b = model.connectionBanner {
            AmberBanner(b.text.title, message: b.text.message, actionTitle: b.actionTitle,
                        action: { if b.kind == .offline { Task { await model.refresh() } } else { openSettings() } },
                        kind: b.kind == .offline ? .attention : .error)
                .padding(.top, 16)
                .accessibilityIdentifier("home.offline")
        }
        if model.signInBanner == nil, let b = model.errorBanner {
            AmberBanner(b.title, message: b.message, actionTitle: "Details", action: openAccounts, kind: .error)
                .padding(.top, 16).accessibilityIdentifier("home.accountError")
        }
        if let draft, !model.isMonthOpen {
            Button { Haptics.shared.play(.rowTap); openDraft() } label: {
                ListRow("Draft", sub: draft.isEmpty ? "Untitled event" : draft, isFirst: true) {
                    Text("Continue").font(CalFont.status).foregroundStyle(Color.calAmber)
                }
            }
            .buttonStyle(.plain).padding(.top, 16)
            .accessibilityLabel("Draft, \(draft.isEmpty ? "untitled event" : draft), continue").accessibilityIdentifier("home.draft")
        }
        if model.signInBanner == nil, model.connectionBanner == nil, !model.isMonthOpen, let b = model.invitationsBanner {
            AmberBanner(b.title, message: b.message, actionTitle: "Review", action: { reviewTick += 1 })
                .padding(.top, 18).accessibilityIdentifier("home.invitations")
        }
        if let p = model.syncingProgress {
            VStack(alignment: .leading, spacing: 0) {
                HStack {
                    StatusLabel("Syncing \(p.total) account\(p.total == 1 ? "" : "s")", kind: .working)
                    Spacer()
                    Text("\(p.done) of \(p.total)").font(CalFont.time).foregroundStyle(Color.calInk2)
                }
                .padding(.horizontal, Spacing.gutter).padding(.top, 14).padding(.bottom, 10)
                ProgressRule(Double(p.done) / Double(max(p.total, 1)))
            }
            .accessibilityElement(children: .combine).accessibilityIdentifier("home.syncing")
        }
        if let note = model.refreshNote {
            Text(note).font(CalFont.sub).foregroundStyle(Color.calInk2)
                .padding(.horizontal, Spacing.gutter).padding(.vertical, Spacing.s)
                .accessibilityIdentifier("home.note")
        }
    }

    @ViewBuilder private var content: some View {
        banners
        if model.isMonthOpen {
            MonthGridView(model: model)
                .padding(.top, Spacing.l)
                .transition(Motion.transition(reduceMotion: reduceMotion, edge: .top))
        } else {
            hero.animation(flipAnimation, value: model.heroBlock?.time)
        }
        if model.showSkeleton {
            ForEach(0..<4, id: \.self) { _ in SkeletonRow() }
        } else if model.isReady {
            if !model.invitations.isEmpty && !model.isMonthOpen { invitationsGroup.id("invitations") }
            ForEach(model.sections) { section in sectionView(section).id(section.isEarlier ? AnyHashable(section.id) : AnyHashable(section.day)) }
            if model.canLoadMore && !model.isMonthOpen {
                Color.clear.frame(height: 1).onAppear { model.loadMore() }
            }
            if model.signInBanner != nil && !model.showingSavedCopy {
                Text("Events from the \(model.signInBanner?.title ?? "work") account stay visible but marked stale until you sign in again. Nothing is deleted.")
                    .font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calInk2)
                    .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.xl)
            }
            if model.syncingProgress != nil {
                Text("Your server checks each account about once a minute. Pulling down asks it to check now.")
                    .font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calInk2)
                    .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.xl)
            }
            if model.showingSavedCopy {
                Text("Alerts already scheduled on this phone will still arrive.")
                    .font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calInk2)
                    .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.xl)
            }
            Color.clear.frame(height: 104)
        }
    }

    /// h-banner: the Next block sits directly under a banner.
    private var heroTop: CGFloat { model.signInBanner != nil || model.connectionBanner != nil ? 0 : 20 }

    /// "Animate changes" (Settings > Appearance): the Next block's cells swap with a short fade when the time changes.
    private var flipAnimation: Animation? {
        model.prefs.animatesFlips(reduceMotion: reduceMotion) ? .easeInOut(duration: 0.28) : nil
    }

    @ViewBuilder private var hero: some View {
        switch model.hero {
        case .upcoming, .live:
            if let b = model.heroBlock, model.showingSavedCopy {
                // y-down: the saved copy in the quiet card, flip cells kept, no Join.
                QuietCard(time: b.time, title: b.title, footer: "\(b.until) \u{00B7} saved copy")
                    .padding(.top, heroTop)
                    .contentShape(Rectangle())
                    .onTapGesture { Haptics.shared.play(.rowTap); openEvent(b.itemID) }
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel(b.accessibilityLabel + ", saved copy")
                    .accessibilityAddTraits(.isButton)
                    .accessibilityIdentifier("home.hero")
            } else if let b = model.heroBlock {
                NextEventBlock(time: b.time, title: b.title, footer: b.footer, color: b.color,
                               action: b.action, onAction: { act(b.target) })
                    .padding(.top, heroTop)
                    .zoomSourceIfAvailable(b.itemID, ns: zoomNS, reduceMotion: reduceMotion)
                    .id(b.itemID + b.time)
                    .transition(.opacity)
                    .contentShape(Rectangle())
                    .onTapGesture { Haptics.shared.play(.rowTap); openEvent(b.itemID) }
                    .accessibilityElement(children: .ignore)
                    .accessibilityLabel(b.accessibilityLabel)
                    .accessibilityAddTraits(.isButton)
                    .accessibilityAction(named: "Open event") { openEvent(b.itemID) }
                    .accessibilityIdentifier("home.hero")
            }
        case .doneForToday(let next, let had):
            QuietCard(title: had ? "Done for today" : (model.clearUntil(next: next) ?? "Nothing else today"),
                      footer: model.doneDetail(next: next) ?? "Nothing coming up in the next \(HomeViewModel.chunkDays) days.")
                .padding(.top, heroTop)
                .accessibilityIdentifier("home.done")
        case .nothingPlanned:
            if !model.loadFailed {
                VStack(spacing: 0) {
                    QuietCard(title: "Nothing planned", footer: "The next \(HomeViewModel.chunkDays) days are clear.")
                    CalButton("New event", kind: .outline, action: newEvent)
                        .padding(.horizontal, Spacing.gutter).padding(.top, 36)
                }
                .padding(.top, 20)
                .accessibilityIdentifier("home.empty")
            }
        case .noAccounts:
            VStack(spacing: 0) {
                QuietCard(title: "No calendars yet", footer: "Connect an account to see your day.")
                CalButton("Add an account", kind: .primary, action: openAccounts)
                    .padding(.horizontal, Spacing.gutter).padding(.top, 36)
            }
            .padding(.top, 20)
            .accessibilityIdentifier("home.noaccounts")
        case .unreachable:
            EmptyView()   // the red banner above says it
        }
    }

    private var invitationsGroup: some View {
        VStack(alignment: .leading, spacing: 0) {
            DaySectionHeader(title: "Reply needed")
            ForEach(Array(model.invitations.enumerated()), id: \.element.id) { n, i in
                let w = model.inviteWhen(i)
                Button { Haptics.shared.play(.rowTap); openEvent(i.id) } label: {
                    HStack(alignment: .top, spacing: Spacing.l) {
                        Text(w.column).font(CalFont.time).foregroundStyle(Color.calInk2).lineLimit(1).minimumScaleFactor(0.7)
                            .frame(width: Spacing.timeColumn, alignment: .leading)
                        // Accessibility sizes: wrap the title and sub line, "Reply" moves under them (design audit 7, item 2).
                        VStack(alignment: .leading, spacing: 3) {
                            Text(i.title).font(CalFont.heading).foregroundStyle(Color.calInk).lineLimit(typeSize.isAccessibilitySize ? 3 : 1)
                            Text(w.sub).font(CalFont.sub).foregroundStyle(Color.calInk2).lineLimit(typeSize.isAccessibilitySize ? nil : 1)
                            if typeSize.isAccessibilitySize { Text("Reply").font(CalFont.status).foregroundStyle(Color.calAmber) }
                        }
                        Spacer(minLength: Spacing.s)
                        if !typeSize.isAccessibilitySize { Text("Reply").font(CalFont.status).foregroundStyle(Color.calAmber) }
                    }
                    .padding(.horizontal, Spacing.gutter).padding(.vertical, 14)
                    .frame(maxWidth: .infinity, minHeight: Spacing.rowHeightTall, alignment: .leading)
                    .overlay(alignment: .leading) { Rectangle().fill(AccountColor(key: i.color).color).frame(width: Spacing.colorBar).padding(.vertical, 15) }
                    .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
                    .overlay(alignment: .top) { if n == 0 { Rectangle().fill(Color.calLine).frame(height: 1) } }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityElement(children: .ignore)
                .accessibilityLabel("\(i.title), \(w.sub), reply needed")
                .accessibilityAddTraits(.isButton)
            }
        }
        .accessibilityIdentifier("home.replyGroup")
    }

    private func sectionView(_ s: HomeLogic.DaySection) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            DaySectionHeader(title: s.title)
            ForEach(Array(s.entries.enumerated()), id: \.element.id) { n, entry in
                switch entry {
                case .row(let r):
                    Button { Haptics.shared.play(.rowTap); openEvent(r.id) } label: {
                        EventRow(r.model, isFirst: n == 0)
                            .contentShape(Rectangle())
                    }
                    .zoomSourceIfAvailable(r.id, ns: zoomNS, reduceMotion: reduceMotion)
                    .buttonStyle(PressRowStyle(reduceMotion: reduceMotion))
                    .accessibilityLabel(r.accessibilityLabel)
                    .accessibilityAddTraits(.isButton)
                case .empty:
                    if !model.loadFailed {
                        Text("Nothing planned").font(CalFont.sub).foregroundStyle(Color.calInk2)
                            .padding(.horizontal, Spacing.gutter).padding(.vertical, Spacing.m)
                    }
                }
            }
        }
    }

    private func act(_ target: HomeLogic.HeroTarget?) {
        switch target {
        case .join(let url): openURL(url)
        case .map(let place):
            var c = URLComponents(string: "https://maps.apple.com/")
            c?.queryItems = [URLQueryItem(name: "q", value: place)]
            if let u = c?.url { openURL(u) }
        case nil: break
        }
    }
}


/// Magnifier drawn like the canvas glyphs (1.75pt strokes, square caps). CalendarUI has none yet; local until design adds one.
struct SearchGlyph: View {
    var body: some View {
        Canvas { ctx, size in
            let s = min(size.width, size.height) * 0.46
            let c = CGPoint(x: size.width / 2 - s * 0.12, y: size.height / 2 - s * 0.12)
            var p = Path()
            p.addEllipse(in: CGRect(x: c.x - s * 0.5, y: c.y - s * 0.5, width: s, height: s))
            p.move(to: CGPoint(x: c.x + s * 0.36, y: c.y + s * 0.36))
            p.addLine(to: CGPoint(x: c.x + s * 0.86, y: c.y + s * 0.86))
            ctx.stroke(p, with: .foreground, style: StrokeStyle(lineWidth: 1.75, lineCap: .butt, lineJoin: .miter))
        }
        .foregroundStyle(Color.calInk)
        .accessibilityHidden(true)
    }
}


extension View {
    @ViewBuilder fileprivate func zoomSourceIfAvailable(_ id: String, ns: Namespace.ID?, reduceMotion: Bool) -> some View {
        if let ns { zoomSource(id, in: ns, enabled: !reduceMotion) } else { self }
    }
}
