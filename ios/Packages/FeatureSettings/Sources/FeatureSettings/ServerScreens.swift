import CalendarCore
import CalendarUI
import SwiftUI

// Server and sync (y-status), Sync log (y-log), diagnostic checklist (y-diag).

struct ServerScreen: View {
    let model: StatusViewModel
    let env: AppEnvironment
    let openAccounts: () -> Void
    @State private var toast: String?
    private var host: String { HubConfigStore().load()?.baseURL.host ?? "your server" }

    var body: some View {
        SettingsPage(title: "Server and sync") {
            if let b = model.banner {
                AmberBanner(b.title, message: b.detail, kind: b.kind == .error ? .error : .attention).padding(.top, Spacing.s)
            }
            GroupLabel("Server", first: model.banner == nil)
            VStack(spacing: 0) {
                HStack(alignment: .firstTextBaseline) {
                    VStack(alignment: .leading, spacing: 6) {
                        Text("Address").calLabel()
                        Text(host).font(CalFont.footer).foregroundStyle(Color.calInk).lineLimit(1)
                    }
                    Spacer(minLength: Spacing.s)
                    StatusLabel(model.load == .loaded ? "Reachable" : "Unreachable", kind: model.load == .loaded ? .ok : .bad)
                }
                .padding(.horizontal, Spacing.gutter).padding(.vertical, 14)
                .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
                .overlay(alignment: .top) { Rectangle().fill(Color.calLine).frame(height: 1) }
                if !model.hubVersion.isEmpty { KeyValueRow("Version", model.hubVersion) }
                if !model.databaseSize.isEmpty { KeyValueRow("Database", model.databaseSize) }
            }
            if case .unreachable = model.load {
                NavigationLink { DiagnosticScreen(env: env) } label: { NavRow("Check the connection", isFirst: false) }.buttonStyle(.plain)
            }
            GroupLabel("Accounts")
            ForEach(Array(model.accounts.enumerated()), id: \.element.id) { n, a in
                SettingsLink(.syncLog(a.id)) {
                    ListRow(a.email, sub: a.detail, color: nil, isFirst: n == 0) {
                        StatusLabel(a.word, kind: a.needsAttention ? .bad : a.word == "Syncing" ? .working : .ok)
                    }
                }
            }
            if model.accounts.isEmpty { Text("No accounts yet.").font(CalFont.sub).foregroundStyle(Color.calInk2).padding(.horizontal, Spacing.gutter) }
            if !model.warnings.isEmpty {
                GroupLabel("Secrets")
                ForEach(Array(model.warnings.enumerated()), id: \.offset) { _, w in KeyValueRow("Heads up", w) }
            }
            Hint("Your server checks each account about once a minute. Tap an account to see its sync log.")
        }
        .safeAreaInset(edge: .bottom) {
            FooterBar {
                CalButton(model.isSyncing ? "Syncing\u{2026}" : "Sync now", kind: .outline) {
                    Task { await model.syncNow(); toast = "Synced \(model.accounts.count) account\(model.accounts.count == 1 ? "" : "s")" }
                }
                .disabled(model.isSyncing)
            }
        }
        .overlay(alignment: .bottom) {
            if let t = toast {
                ToastView(t).padding(.bottom, 96).task { try? await Task.sleep(for: .seconds(3)); toast = nil }
            }
        }
        .task { await model.refresh() }
    }
}

struct SyncLogScreen: View {
    let source: any StatusSource
    let accountID: String?
    @State private var rows: [SyncLogEntry] = []
    @State private var failed = false

    var body: some View {
        SettingsPage(title: "Sync log", sub: nil) {
            if failed { AmberBanner("Can\u{2019}t load the log.", message: "Your server didn\u{2019}t answer.", kind: .error) }
            ForEach(Array(rows.enumerated()), id: \.element.id) { n, r in
                ListRow(r.title, sub: r.sub, color: nil, height: Spacing.rowHeightTall, isFirst: n == 0) {
                    Text(r.time).font(CalFont.time).foregroundStyle(Color.calInk2)
                }
                .foregroundStyle(r.isError ? Color.calRed : Color.calInk)
            }
            Hint("Checks run about once a minute. Quiet checks are grouped.")
        }
        .task {
            do {
                let runs = try await source.syncRuns(limit: 100, accountId: accountID)
                rows = SyncLogEntry.group(runs, tz: .current)
            } catch { failed = true }
        }
    }
}

/// One row of the sync log; consecutive quiet checks collapse into one.
struct SyncLogEntry: Identifiable, Equatable {
    var id: String
    var time: String
    var title: String
    var sub: String
    var isError: Bool

    static func group(_ runs: [SyncRun], tz: TimeZone) -> [SyncLogEntry] {
        let f = DateFormatter(); f.dateFormat = "HH:mm"; f.timeZone = tz
        var out: [SyncLogEntry] = []
        var quiet: [SyncRun] = []
        func flush() {
            guard let first = quiet.first, let last = quiet.last else { return }
            if quiet.count == 1 {
                out.append(.init(id: "r\(first.id)", time: f.string(from: first.startedAt), title: "Checked, 0 changes",
                                 sub: "\(first.durationMs) ms", isError: false))
            } else {
                out.append(.init(id: "q\(first.id)", time: f.string(from: first.startedAt), title: "Checked \(quiet.count) times, 0 changes",
                                 sub: "\(f.string(from: last.startedAt)) to \(f.string(from: first.startedAt))", isError: false))
            }
            quiet = []
        }
        for r in runs {   // newest first
            let failed = r.error != nil || r.errorKind != nil
            let changes = r.added + r.changed + r.removed
            if !failed && changes == 0 { quiet.append(r); continue }
            flush()
            if failed {
                out.append(.init(id: "r\(r.id)", time: f.string(from: r.startedAt), title: "Sync failed", sub: r.error ?? r.errorKind ?? "Error", isError: true))
            } else {
                let parts = [(r.changed, "moved"), (r.added, "added"), (r.removed, "removed")].filter { $0.0 > 0 }.map { "\($0.0) \($0.1)" }
                out.append(.init(id: "r\(r.id)", time: f.string(from: r.startedAt), title: changes == 1 ? "1 change" : "\(changes) changes",
                                 sub: parts.joined(separator: ", ") + " \u{00B7} \(r.durationMs) ms", isError: false))
            }
        }
        flush()
        return out
    }
}

// MARK: Diagnostic

@MainActor @Observable
final class DiagnosticModel {
    struct Step: Identifiable { var id: String; var title: String; var status: String; var kind: StatusLabel.Kind }
    private(set) var steps = [
        Step(id: "net", title: "This phone has internet", status: "Not tried", kind: .none),
        Step(id: "ts", title: "Tailscale is connected", status: "Not tried", kind: .none),
        Step(id: "srv", title: "Server answers", status: "Not tried", kind: .none),
    ]
    private(set) var advice: String?
    private(set) var tailscaleFailed = false
    private(set) var failed = false
    private(set) var lastGood: Date?
    private let api: any HubAPI
    private let host: String

    init(api: any HubAPI, host: String, lastGood: Date?) { self.api = api; self.host = host; self.lastGood = lastGood }

    private func set(_ id: String, _ status: String, _ kind: StatusLabel.Kind) {
        if let i = steps.firstIndex(where: { $0.id == id }) { steps[i].status = status; steps[i].kind = kind }
    }

    func run() async {
        for s in steps { set(s.id, "Not tried", .none) }
        advice = nil; tailscaleFailed = false; failed = false
        set("net", "Working", .working)
        guard NetworkMonitor.shared.isOnline else {
            failed = true; set("net", "No", .bad); advice = "Connect this phone to Wi-Fi or mobile data, then try again."; return
        }
        set("net", "Yes", .ok)
        set("ts", "Working", .working)
        do {
            _ = try await api.hello()
            set("ts", host.hasSuffix(".ts.net") ? "On" : "Not needed", .ok)
            set("srv", "Yes", .ok)
            advice = "Everything answers. Pull down on Home to refresh."
        } catch APIClientError.unauthorized {
            failed = true; set("ts", "On", .ok); set("srv", "Token refused", .bad)
            advice = "The server doesn\u{2019}t accept this phone\u{2019}s token. Pair the phone again."
        } catch {
            if host.hasSuffix(".ts.net") {
                failed = true; set("ts", "Off", .bad); tailscaleFailed = true
                advice = "Turn Tailscale on for this phone, then try again."
            } else {
                failed = true; set("ts", "Not needed", .ok); set("srv", "No answer", .bad)
                advice = "Check that the hub is running on your server."
            }
        }
    }
}

struct DiagnosticScreen: View {
    let env: AppEnvironment
    @State private var model: DiagnosticModel
    @Environment(\.openURL) private var openURL
    private let host: String

    init(env: AppEnvironment) {
        self.env = env
        let cfg = HubConfigStore().load()
        host = cfg?.baseURL.host ?? "your server"
        _model = State(initialValue: DiagnosticModel(api: cfg.map { APIClient(config: $0) } ?? APIClient(baseURL: URL(string: "https://invalid.local")!, token: ""),
                                                      host: cfg?.baseURL.host ?? "", lastGood: nil))
    }

    var body: some View {
        SettingsPage(title: "Server and sync") {
            if model.failed { AmberBanner("\(host) didn\u{2019}t answer.", message: nil, kind: .error).padding(.top, Spacing.s) }
            GroupLabel("Checking")
            ForEach(Array(model.steps.enumerated()), id: \.element.id) { n, s in
                ChecklistRow(s.title, status: s.status, kind: s.kind, isFirst: n == 0)
            }
            if let a = model.advice {
                Text(a).font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calInk2)
                    .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.l)
            }
        }
        .safeAreaInset(edge: .bottom) {
            FooterBar {
                if model.tailscaleFailed, let u = URL(string: "tailscale://") {
                    CalButton("Open Tailscale") { openURL(u) }
                }
                CalButton("Try again", kind: .outline) { Task { await model.run() } }
            }
        }
        .task { await model.run() }
    }
}
