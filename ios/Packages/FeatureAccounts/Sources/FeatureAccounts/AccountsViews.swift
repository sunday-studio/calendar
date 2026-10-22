import CalendarCore
import CalendarUI
import SwiftUI

/// Accounts list. Push it inside the app's NavigationStack.
public struct AccountsScreen: View {
    @State private var model: AccountsListViewModel
    @State private var showAdd: Bool
    @State private var fix: Account?
    private let api: any HubAPI
    private let auth: any WebAuthSession

    public init(api: any HubAPI, auth: any WebAuthSession, startWithAddSheet: Bool = false) {
        self.api = api
        self.auth = auth
        _model = State(initialValue: AccountsListViewModel(api: api))
        _showAdd = State(initialValue: startWithAddSheet)
    }

    @Environment(\.dismiss) private var dismiss

    public var body: some View {
        VStack(spacing: 0) {
            ScreenNav("Accounts", leading: .back, trailing: "Add", onLeading: { dismiss() }, onTrailing: { showAdd = true })
            ScrollView {
                VStack(alignment: .leading, spacing: 0) { content }
            }
            .refreshable { await model.load() }
        }
        .background(Color.calBg)
        .toolbar(.hidden, for: .navigationBar)
        .task { await model.load() }
        .sheet(item: $fix) { account in
            ReconnectSheet(account: account, api: api, auth: auth) { done in
                fix = nil
                if done { model.toast = "Reconnected"; Task { await model.load() } }
            }
        }
        .sheet(isPresented: $showAdd) {
            AddAccountSheet(api: api, auth: auth) { account in
                showAdd = false
                if account != nil { model.toast = "Account connected" }
                Task { await model.load() }
            }
        }
        .toastOverlay($model.toast)
    }

    @ViewBuilder private var content: some View {
        switch model.phase {
        case .loading where model.accounts.isEmpty:
            VStack(spacing: 0) { SkeletonRow(); SkeletonRow(); SkeletonRow() }
        case .failed(let msg) where model.accounts.isEmpty:
            AmberBanner("Can\u{2019}t load accounts.", message: msg, actionTitle: "Retry", action: { Task { await model.load() } }, kind: .error)
        default:
            if model.accounts.isEmpty {
                VStack(spacing: 0) {
                    QuietCard(title: "No accounts yet", footer: "Connect a Google account and its calendars will appear in your agenda.")
                    CalButton("Add an account", action: { showAdd = true }).padding(.horizontal, Spacing.gutter).padding(.top, 36)
                }
            } else {
                if let n = model.needingSignIn.first {
                    let many = model.needingSignIn.count > 1
                    AmberBanner(many ? "\(model.needingSignIn.count) accounts" : n.email,
                                message: many ? "need you to sign in again." : "needs you to sign in again.",
                                actionTitle: "Fix", action: { fix = n })
                        .padding(.bottom, Spacing.s)
                }
                ForEach(Array(model.accounts.enumerated()), id: \.element.id) { n, a in
                    NavigationLink {
                        AccountDetailView(account: a, api: api, auth: auth) { updated in
                            if let updated { model.replace(updated) }
                        } onRemoved: { id in
                            model.remove(id: id)
                            model.toast = "Account removed"
                        }
                    } label: {
                        let t = StatusWords.trailing(a)
                        ListRow(a.email, sub: sub(a), color: AccountColor(key: a.color), isFirst: n == 0) {
                            StatusLabel(t.text, kind: t.kind)
                        }
                    }
                    .buttonStyle(.plain)
                }
                Text("Your server checks each account about once a minute.")
                    .font(CalFont.hint).foregroundStyle(Color.calInk2)
                    .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.l)
            }
        }
    }

    private func sub(_ a: Account) -> String {
        let provider = a.provider == "google" ? "Google" : a.provider == "ics" ? "Link, read only" : a.provider.capitalized
        guard let n = model.calendarCounts[a.id] else { return provider }
        return "\(provider) \u{00B7} \(n) calendar\(n == 1 ? "" : "s")"
    }
}

extension View {
    /// Bottom toast that clears itself after a few seconds.
    func toastOverlay(_ text: Binding<String?>) -> some View {
        overlay(alignment: .bottom) {
            if let t = text.wrappedValue {
                ToastView(t)
                    .transition(.move(edge: .bottom).combined(with: .opacity))
                    .task(id: t) {
                        try? await Task.sleep(for: .seconds(3))
                        text.wrappedValue = nil
                    }
            }
        }
        .animation(Motion.settle, value: text.wrappedValue)
    }
}

// MARK: Add account

struct AddAccountSheet: View {
    let api: any HubAPI
    let auth: any WebAuthSession
    let onClose: (Account?) -> Void
    @State private var connect: ConnectViewModel?
    @State private var ics: IcsLinkViewModel?

    var body: some View {
        VStack(spacing: 0) {
            ScreenNav(ics != nil ? "Calendar link" : connect == nil ? "Add an account" : "Google", leading: ics != nil ? .back : .close,
                      onLeading: { if ics != nil { ics = nil } else { onClose(nil) } })
            if let ics {
                IcsLinkView(model: ics) { onClose($0) }
            } else if let connect {
                ConnectingView(model: connect) { onClose($0) }
            } else {
                providers
            }
        }
        .background(Color.calBg)
        .presentationBackground(Color.calBg)
        .presentationCornerRadius(0)
        .presentationDragIndicator(.hidden)
    }

    private var providers: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                Text("Pick where your calendar lives. Your server signs in for you.")
                    .font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calInk2)
                    .padding(.horizontal, Spacing.gutter).padding(.bottom, Spacing.l)
                Button { connect = ConnectViewModel(kind: .add(provider: "google"), api: api, auth: auth) } label: {
                    ProviderRow(name: "Google Calendar", detail: "Gmail and Google Workspace", color: .cobalt, enabled: true, isFirst: true)
                }
                .buttonStyle(.plain).accessibilityIdentifier("add.google")
                ProviderRow(name: "Microsoft", detail: "Outlook.com, Hotmail or Microsoft 365", color: .rose, enabled: false)
                ProviderRow(name: "iCloud", detail: "Needs an app-specific password", color: .moss, enabled: false)
                Button { ics = IcsLinkViewModel(api: api) } label: {
                    ProviderRow(name: "Calendar link", detail: "Any .ics link. Read only.", color: .slate, enabled: true)
                }
                .buttonStyle(.plain).accessibilityIdentifier("add.ics")
                Text("Each account gets its own colour. You can change it later.")
                    .font(CalFont.hint).foregroundStyle(Color.calInk2)
                    .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.l)
            }
        }
    }
}

private struct ProviderRow: View {
    let name: String
    let detail: String
    let color: AccountColor
    let enabled: Bool
    var isFirst = false
    var body: some View {
        ListRow(name, sub: detail, color: color, isFirst: isFirst) {
            if enabled { CalGlyph(.forward, size: 14).foregroundStyle(Color.calInk2) }
            else { Text("Later").calLabel(Color.calInk3) }
        }
        .opacity(enabled ? 1 : 0.55)
        .accessibilityAddTraits(enabled ? [] : .isStaticText)
    }
}

/// Sign-in again from the list (banner "Fix").
struct ReconnectSheet: View {
    let account: Account
    let api: any HubAPI
    let auth: any WebAuthSession
    let onClose: (Bool) -> Void
    @State private var model: ConnectViewModel

    init(account: Account, api: any HubAPI, auth: any WebAuthSession, onClose: @escaping (Bool) -> Void) {
        self.account = account; self.api = api; self.auth = auth; self.onClose = onClose
        _model = State(initialValue: ConnectViewModel(kind: .reconnect(accountId: account.id), api: api, auth: auth))
    }

    var body: some View {
        VStack(spacing: 0) {
            ScreenNav("Sign in again", leading: .close, onLeading: { onClose(false) })
            ConnectingView(model: model) { onClose($0 != nil) }
        }
        .background(Color.calBg)
        .presentationBackground(Color.calBg)
        .presentationCornerRadius(0)
        .presentationDragIndicator(.hidden)
    }
}

// MARK: Connecting

struct ConnectingView: View {
    let model: ConnectViewModel
    /// Called with the connected account, or nil when the user leaves without one.
    let onClose: (Account?) -> Void

    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            switch model.phase {
            case .idle, .starting, .signingIn:
                busy(title: model.isReconnect ? "Waiting for Google" : "Opening Google",
                     message: "Sign in on the page that opens. Come back here when you\u{2019}re done.")
            case .finishing:
                busy(title: "Finishing sign in",
                     message: "Your server is trading the code with Google and storing the token, encrypted.")
            case .connected(let a):
                done(a)
            case .cancelled:
                Color.clear.onAppear { onClose(nil) }
            case .failed(let f):
                AmberBanner(f.title, message: f.message, kind: .error).padding(.top, Spacing.s)
                Spacer()
                FooterBar {
                    CalButton("Try again") { Task { await model.run() } }.accessibilityIdentifier("connect.retry")
                    CalButton("Close", kind: .outline) { onClose(nil) }
                }
            }
        }
        .task { await model.run() }
        .onChange(of: model.phase) { _, phase in
            if case .connected(let a) = phase, model.isReconnect { onClose(a) }
        }
    }

    private func busy(title: String, message: String) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Text(title).font(CalFont.display).foregroundStyle(Color.calInk)
                .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s)
            Text(message).font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calInk2)
                .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s)
            ProgressRule().padding(.top, Spacing.xl)
            Spacer()
        }
        .accessibilityIdentifier("connect.busy")
    }

    private func done(_ a: Account) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("Connected").font(CalFont.display).foregroundStyle(Color.calInk)
                .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s)
            if a.statusKind == .syncing {
                Text("Your events are loading in the background. You can start using the agenda now.")
                    .font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calInk2)
                    .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s)
            }
            ListRow(a.email, sub: "Google", color: AccountColor(key: a.color), isFirst: true) { StatusLabel("Done", kind: .ok) }
                .padding(.top, Spacing.xl)
            Spacer()
            FooterBar { CalButton("Done") { onClose(a) }.accessibilityIdentifier("connect.done") }
        }
    }
}

// MARK: Detail

struct AccountDetailView: View {
    @State private var model: AccountDetailViewModel
    @State private var reconnect: ConnectViewModel?
    @State private var confirmRemove = false
    @Environment(\.dismiss) private var dismiss
    private let api: any HubAPI
    private let auth: any WebAuthSession
    private let onChange: (Account?) -> Void
    private let onRemoved: (String) -> Void

    init(account: Account, api: any HubAPI, auth: any WebAuthSession,
         onChange: @escaping (Account?) -> Void, onRemoved: @escaping (String) -> Void) {
        _model = State(initialValue: AccountDetailViewModel(account: account, api: api))
        self.api = api; self.auth = auth; self.onChange = onChange; self.onRemoved = onRemoved
    }

    var body: some View {
        let a = model.account
        VStack(spacing: 0) {
            ScreenNav(a.displayName.isEmpty ? a.email : a.displayName, leading: .back, onLeading: { dismiss() })
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    HStack(spacing: Spacing.m) {
                        StatusLabel(StatusWords.word(a), kind: statusKind(a))
                        CalTag(a.provider == "google" ? "Google" : a.provider == "ics" ? "Read only link" : a.provider.capitalized)
                        Spacer(minLength: 0)
                    }
                    .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s)
                    Text(a.email).font(CalFont.sub).foregroundStyle(Color.calInk2)
                        .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s)

                    if a.statusKind == .needsReauth {
                        AmberBanner(a.email, message: "needs you to sign in again.", actionTitle: "Fix") { reconnect = signIn() }
                            .padding(.top, Spacing.l)
                    }

                    Text("Colour").calLabel().padding(.horizontal, Spacing.gutter).padding(.top, 32).padding(.bottom, 10)
                    colourPicker.padding(.horizontal, Spacing.gutter)

                    Text("Calendars").calLabel().padding(.horizontal, Spacing.gutter).padding(.top, 32).padding(.bottom, 10)
                    if model.calendars.isEmpty {
                        Text("Calendars appear here once this account has synced.")
                            .font(CalFont.sub).foregroundStyle(Color.calInk2)
                            .padding(.horizontal, Spacing.gutter).padding(.vertical, Spacing.m)
                    } else {
                        ForEach(Array(model.calendars.enumerated()), id: \.element.id) { n, c in
                            ListRow(c.name, sub: c.isPrimary ? "Default for new events" : nil, color: nil,
                                    height: Spacing.rowHeight, isFirst: n == 0) { EmptyView() }
                        }
                    }

                    Text("Sync").calLabel().padding(.horizontal, Spacing.gutter).padding(.top, 32).padding(.bottom, 10)
                    KeyValueRow("Last sync", StatusWords.lastSync(a).replacingOccurrences(of: "Synced ", with: ""))
                    if let d = a.statusDetail, !d.isEmpty { KeyValueRow("Detail", d, isError: a.statusKind == .error) }

                    if let e = model.errorMessage {
                        Text(e).font(CalFont.sub).foregroundStyle(Color.calRed)
                            .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.m)
                    }
                    Color.clear.frame(height: Spacing.xl)
                }
            }
        }
        .background(Color.calBg)
        .toolbar(.hidden, for: .navigationBar)
        .safeAreaInset(edge: .bottom) {
            FooterBar {
                CalButton("Sign in again", kind: .outline) { reconnect = signIn() }.accessibilityIdentifier("detail.reconnect")
                CalButton("Remove account", kind: .danger) { confirmRemove = true }
                    .accessibilityIdentifier("detail.remove").disabled(model.busy)
            }
        }
        .task { await model.load() }
        .onChange(of: model.account) { _, new in onChange(new) }
        .onChange(of: model.removed) { _, gone in
            if gone { onRemoved(model.account.id); dismiss() }
        }
        .sheet(item: $reconnect) { vm in
            VStack(spacing: 0) {
                ScreenNav("Sign in again", leading: .close, onLeading: { reconnect = nil })
                ConnectingView(model: vm) { account in
                    reconnect = nil
                    if account != nil { Task { await model.reconnected() } }
                }
            }
            .background(Color.calBg)
            .presentationBackground(Color.calBg)
        .presentationCornerRadius(0)
        .presentationDragIndicator(.hidden)
        }
        .confirmationDialog("Remove \(a.email)?", isPresented: $confirmRemove, titleVisibility: .visible) {
            Button("Remove account", role: .destructive) { Task { await model.remove() } }
            Button("Keep it", role: .cancel) {}
        } message: {
            Text("Its calendars and events disappear from this app and from your server, and the saved sign-in is deleted. Nothing changes in Google. You can add it again later.")
        }
        .toastOverlay($model.toast)
    }

    private func statusKind(_ a: Account) -> StatusLabel.Kind {
        switch a.statusKind {
        case .active: .ok
        case .syncing: .working
        case .needsReauth, .error: .bad
        case .paused: .none
        }
    }

    private func signIn() -> ConnectViewModel {
        ConnectViewModel(kind: .reconnect(accountId: model.account.id), api: api, auth: auth)
    }

    private var colourPicker: some View {
        HStack(spacing: Spacing.xs) {
            ForEach(AccountColor.allCases) { c in
                let selected = c.rawValue == model.account.color
                Button { Task { await model.setColor(c.rawValue) } } label: {
                    Rectangle().fill(c.color).frame(width: 36, height: 36)
                        .overlay(Rectangle().strokeBorder(Color.calInk, lineWidth: selected ? 1 : 0).padding(-4))
                        .frame(width: Spacing.hit, height: Spacing.hit)
                }
                .accessibilityLabel(c.rawValue.capitalized)
                .accessibilityAddTraits(selected ? .isSelected : [])
            }
        }
    }
}

extension ConnectViewModel: Identifiable {
    public nonisolated var id: ObjectIdentifier { ObjectIdentifier(self) }
}


// MARK: Read-only link (a-ics)

struct IcsLinkView: View {
    @Bindable var model: IcsLinkViewModel
    let onClose: (Account?) -> Void
    @FocusState private var focus: Bool

    var body: some View {
        VStack(spacing: 0) {
            if model.isChecking { ProgressRule().accessibilityLabel("Checking the link") }
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    if case .added(let a) = model.phase { added(a) } else { form }
                }
            }
            .scrollDismissesKeyboard(.interactively)
        }
        .safeAreaInset(edge: .bottom) {
            if case .added(let a) = model.phase {
                FooterBar { CalButton("Done") { onClose(a) }.accessibilityIdentifier("ics.done") }
            } else {
                FooterBar {
                    CalButton(model.isChecking ? "Checking\u{2026}" : "Add calendar") { Task { await model.add() } }
                        .disabled(!model.canAdd).accessibilityIdentifier("ics.add")
                }
            }
        }
        .onAppear { focus = true }
    }

    @ViewBuilder private var form: some View {
        Text("Paste the address of a calendar someone shared with you. Your server reads it, so it must be reachable from there.")
            .font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calInk2)
            .padding(.horizontal, Spacing.gutter).padding(.bottom, Spacing.l)
        if let banner = model.banner {
            AmberBanner("Nothing was added.", message: banner, kind: .error).padding(.bottom, Spacing.l)
                .accessibilityIdentifier("ics.banner")
        }
        LabeledField("Link", hint: model.urlError == nil ? "https:// or webcal://, ending in .ics" : nil, error: model.urlError, isFocused: focus) {
            TextField("https://example.com/calendar.ics", text: $model.url)
                .textInputAutocapitalization(.never).autocorrectionDisabled().keyboardType(.URL)
                .focused($focus).submitLabel(.go)
                .onSubmit { Task { await model.add() } }
                .accessibilityIdentifier("ics.url")
        }
        .padding(.horizontal, Spacing.gutter)
        LabeledField("Name (optional)", hint: "Defaults to the calendar\u{2019}s own name.") {
            TextField("Name", text: $model.name).accessibilityIdentifier("ics.name")
        }
        .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.l)
        Text("Events from a link are read only. You can look at them, but not change, reply to or delete them. The link is checked first, and nothing is saved if it fails.")
            .font(CalFont.hint).foregroundStyle(Color.calInk2)
            .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.l)
    }

    @ViewBuilder private func added(_ a: Account) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            Text("Added").font(CalFont.title).foregroundStyle(Color.calInk).padding(.horizontal, Spacing.gutter).padding(.bottom, Spacing.l)
            KeyValueRow("Calendar", a.displayName.isEmpty ? a.email : a.displayName)
            KeyValueRow("Source", a.email)
            KeyValueRow("Access", "Read only")
            Text("Your server checks the link about every 15 minutes.")
                .font(CalFont.hint).foregroundStyle(Color.calInk2)
                .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.l)
        }
    }
}
