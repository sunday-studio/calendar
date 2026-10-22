import CalendarCore
import CalendarUI
import SwiftUI
import UIKit

// API tokens (canvas y-tokens, y-newtoken). Reached from Settings > Server and sync.

struct TokensScreen: View {
    @State var model: TokensViewModel
    let api: any HubTokensAPI
    @State private var showNew = false
    @State private var toast: String?

    var body: some View {
        SettingsPage(title: "API tokens", sub: "Tokens let this phone and your scripts use your calendar through your server.",
                     trailing: model.phase == .loaded ? "New" : nil, onTrailing: { showNew = true }) {
            switch model.phase {
            case .loading:
                VStack(spacing: 0) { SkeletonRow(); SkeletonRow() }
            case .needsAdmin:
                AmberBanner("This token can\u{2019}t manage tokens.", message: "Use a token with admin access to see and make tokens.", kind: .neutral)
                    .padding(.top, Spacing.m).accessibilityIdentifier("tokens.needsAdmin")
            case .failed(let m):
                AmberBanner("Can\u{2019}t load tokens.", message: m, actionTitle: "Retry", action: { Task { await model.load() } }, kind: .error)
                    .padding(.top, Spacing.m)
            case .loaded:
                loaded
            }
        }
        .task { await model.load() }
        .refreshable { await model.load() }
        .fullScreenCover(isPresented: $showNew) {
            NewTokenScreen(model: NewTokenViewModel(api: api, copy: { Self.copyToPasteboard($0) })) {
                showNew = false
                Task { await model.load() }
            }
        }
        .overlay(alignment: .bottom) {
            if let t = toast {
                ToastView(t).padding(.bottom, 32).task { try? await Task.sleep(for: .seconds(3)); toast = nil }
            }
        }
    }

    @ViewBuilder private var loaded: some View {
        if let m = model.message {
            AmberBanner("Not changed.", message: m, kind: .error).padding(.top, Spacing.m)
        }
        GroupLabel("Active", first: true)
        if model.active.isEmpty {
            Text("No tokens yet.").font(CalFont.sub).foregroundStyle(Color.calInk2).padding(.horizontal, Spacing.gutter)
        }
        ForEach(Array(model.active.enumerated()), id: \.element.id) { n, r in
            NavigationLink {
                TokenDetailScreen(model: model, row: r)
            } label: {
                ListRow(r.name, sub: "\(TokensViewModel.kindWords(r.token.kind)) \u{00B7} \(r.sub)", isFirst: n == 0) {
                    if r.isMine { CalTag("Yours") } else { CalGlyph(.forward, size: 12).foregroundStyle(Color.calInk2) }
                }
            }
            .buttonStyle(.plain)
            .accessibilityIdentifier("token.row.\(r.name)")
        }
        if !model.revoked.isEmpty {
            GroupLabel("Revoked")
            ForEach(Array(model.revoked.enumerated()), id: \.element.id) { n, r in
                ListRow(r.name, sub: r.sub, isFirst: n == 0) { EmptyView() }.opacity(0.55)
            }
        }
        Hint("Revoking a token signs that device or script out straight away.")
    }

    @MainActor static func copyToPasteboard(_ s: String) {
        // Local to this device and gone after two minutes: a secret should not sit in Universal Clipboard.
        UIPasteboard.general.setItems([[UIPasteboard.typeAutomatic: s]], options: [.localOnly: true, .expirationDate: Date().addingTimeInterval(120)])
    }
}

struct TokenDetailScreen: View {
    let model: TokensViewModel
    let row: TokensViewModel.Row
    @State private var confirm = false
    @State private var busy = false
    @Environment(\.dismiss) private var dismiss

    var body: some View {
        SettingsPage(title: row.name) {
            VStack(spacing: 0) {
                KeyValueRow("Access", TokensViewModel.scopeWords(row.token.scope))
                KeyValueRow("Kind", TokensViewModel.kindWords(row.token.kind))
                KeyValueRow("Starts with", row.token.prefix)
                KeyValueRow("Last used", TokensViewModel.usedWords(row.token, now: Date()).replacingOccurrences(of: "used ", with: "").capitalized(first: true))
            }
            .overlay(alignment: .top) { Rectangle().fill(Color.calLine).frame(height: 1) }
            .padding(.top, Spacing.m)
            if let m = model.message { AmberBanner("Not changed.", message: m, kind: .error).padding(.top, Spacing.l) }
            if row.isMine {
                Hint("This is the token this phone uses, so it can\u{2019}t be revoked here.")
            } else {
                CalButton("Revoke token", kind: .danger) { confirm = true }
                    .disabled(busy).padding(.horizontal, Spacing.gutter).padding(.top, Spacing.xl)
                    .accessibilityIdentifier("token.revoke")
            }
        }
        .calSheet(isPresented: $confirm) {
            CalSheet(title: "Revoke \u{201C}\(row.name)\u{201D}?", message: "Revoking signs that device or script out straight away.") {
                SheetRow("Revoke token", isDestructive: true, isFirst: true) {
                    confirm = false
                    busy = true
                    Task {
                        let gone = await model.revoke(row.id)
                        busy = false
                        if gone { dismiss() }
                    }
                }
                .accessibilityIdentifier("token.revoke.confirm")
                SheetRow("Keep it") { confirm = false }
            }
        }
    }
}

struct NewTokenScreen: View {
    @State var model: NewTokenViewModel
    let onClose: () -> Void
    @FocusState private var focus: Bool
    @State private var toast: String?

    private var accessIndex: Binding<Int> { Binding(get: { model.canWrite ? 1 : 0 }, set: { model.canWrite = $0 == 1 }) }
    private var kindIndex: Binding<Int> { Binding(get: { model.kind == .script ? 0 : 1 }, set: { model.kind = $0 == 0 ? .script : .device }) }

    var body: some View {
        VStack(spacing: 0) {
            ScreenNav("New token", leading: .close, onLeading: onClose)
            if model.phase == .creating { ProgressRule().accessibilityLabel("Making the token") }
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    if let secret = model.secret { reveal(secret) } else { form }
                }
            }
            .scrollDismissesKeyboard(.interactively)
        }
        .background(Color.calBg)
        .safeAreaInset(edge: .bottom) {
            FooterBar {
                if model.secret != nil {
                    CalButton("Copy token") { model.copyToken(); toast = "Copied" }.accessibilityIdentifier("newtoken.copy")
                    CalButton("Done", kind: .outline) { model.done(); onClose() }.accessibilityIdentifier("newtoken.done")
                } else {
                    CalButton(model.phase == .creating ? "Making\u{2026}" : "Create") { Task { await model.create() } }
                        .disabled(!model.canCreate).accessibilityIdentifier("newtoken.create")
                }
            }
        }
        .overlay(alignment: .bottom) {
            if let t = toast { ToastView(t).padding(.bottom, 150).task { try? await Task.sleep(for: .seconds(2)); toast = nil } }
        }
        .interactiveDismissDisabled(model.secret != nil)
        .onAppear { focus = true }
    }

    @ViewBuilder private var form: some View {
        if let e = model.error { AmberBanner("Not made.", message: e, kind: .error).padding(.bottom, Spacing.l) }
        LabeledField("Name", hint: "For example \u{201C}Home script\u{201D}.", isFocused: focus) {
            TextField("Name", text: $model.name).focused($focus).submitLabel(.done)
                .accessibilityIdentifier("newtoken.name")
        }
        .padding(.horizontal, Spacing.gutter)
        Text("Access").calLabel().padding(.horizontal, Spacing.gutter).padding(.top, Spacing.xl).padding(.bottom, 10)
        SegmentedControl(["Read only", "Read and write"], selection: accessIndex).padding(.horizontal, Spacing.gutter)
        Text("For").calLabel().padding(.horizontal, Spacing.gutter).padding(.top, Spacing.xl).padding(.bottom, 10)
        SegmentedControl(["A script", "A device"], selection: kindIndex).padding(.horizontal, Spacing.gutter)
        Hint(model.canWrite ? "This token can add, change and delete events." : "This token can read your calendar and nothing else.")
    }

    @ViewBuilder private func reveal(_ secret: String) -> some View {
        VStack(alignment: .leading, spacing: 6) {
            Text("Token, shown once").calLabel()
            Text(secret).font(CalFont.font(.mono, size: 18, relativeTo: .body)).foregroundStyle(Color.calInk)
                .textSelection(.enabled).fixedSize(horizontal: false, vertical: true)
                .accessibilityIdentifier("newtoken.secret")
        }
        .padding(.horizontal, Spacing.gutter).padding(.vertical, 14)
        .frame(maxWidth: .infinity, alignment: .leading)
        .overlay(alignment: .top) { Rectangle().fill(Color.calLine).frame(height: 1) }
        .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
        Hint("Copy it now. Calendar cannot show it again.")
    }
}

private extension String {
    func capitalized(first: Bool) -> String { prefix(1).uppercased() + dropFirst() }
}
