import CalendarCore
import CalendarUI
import SwiftUI
import UIKit

/// Welcome -> form -> checking -> (failed | paired). `onFinish(config, addAccount)` is called when the user leaves the flow.
public struct PairingFlowView: View {
    @State private var model: PairingViewModel
    private let onFinish: (HubConfig, Bool) -> Void
    @Environment(\.accessibilityReduceMotion) private var reduceMotion

    public init(model: PairingViewModel, onFinish: @escaping (HubConfig, Bool) -> Void) {
        _model = State(initialValue: model)
        self.onFinish = onFinish
    }

    public var body: some View {
        ZStack {
            Color.calBg.ignoresSafeArea()
            switch model.step {
            case .welcome: WelcomeView(start: model.begin)
            case .form: PairFormView(model: model)
            case .checking: CheckingView(model: model)
            case .failed(let f): PairFailedView(model: model, failure: f)
            case .paired(let hello):
                PairedView(hello: hello, host: model.pairedConfig?.baseURL.host ?? model.host) { add in
                    if let c = model.pairedConfig { onFinish(c, add) }
                }
            }
        }
        .animation(Motion.animation(reduceMotion: reduceMotion), value: model.step)
    }
}

// MARK: Welcome

private struct WelcomeView: View {
    let start: () -> Void
    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            FlipCells("CALENDAR", size: .xl).padding(.top, 60)
            Text("Your calendars,\non one board.")
                .font(CalFont.welcome).foregroundStyle(Color.calInk).padding(.top, 36)
                .fixedSize(horizontal: false, vertical: true)
            Text("Calendar reads Google, Outlook and iCloud through your own home server. Nothing leaves your tailnet.")
                .font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calInk2)
                .padding(.top, Spacing.l).frame(maxWidth: 340, alignment: .leading)
            Spacer()
        }
        .padding(.horizontal, Spacing.gutter)
        .frame(maxWidth: .infinity, alignment: .leading)
        .safeAreaInset(edge: .bottom) {
            FooterBar {
                CalButton("Connect to your server", action: start).accessibilityIdentifier("welcome.connect")
                Text("Needs Tailscale on this phone").font(CalFont.hint).foregroundStyle(Color.calInk2)
            }
        }
    }
}

// MARK: Form

private struct PairFormView: View {
    @Bindable var model: PairingViewModel
    @FocusState private var focus: Field?
    enum Field { case host, token }

    var body: some View {
        VStack(spacing: 0) {
            ScreenNav("Your server", leading: .back, onLeading: model.back)
            ScrollView {
                VStack(alignment: .leading, spacing: Spacing.l) {
                    Text("Enter the address of your hub and an access token made on the server.")
                        .font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calInk2)
                        .padding(.bottom, Spacing.s)
                    LabeledField("Server address", error: hostError, isFocused: focus == .host) {
                        TextField("https://hub.tail1234.ts.net", text: $model.host)
                            .keyboardType(.URL).textContentType(.URL)
                            .textInputAutocapitalization(.never).autocorrectionDisabled()
                            .focused($focus, equals: .host).submitLabel(.next)
                            .onSubmit { focus = .token }
                            .accessibilityIdentifier("pair.host")
                    }
                    LabeledField("Access token",
                                 hint: "Make one on the server under API tokens. This phone gets its own token you can revoke.",
                                 error: tokenError, isFocused: focus == .token) {
                        SecureField("cal_\u{2026}", text: $model.token)
                            .textInputAutocapitalization(.never).autocorrectionDisabled()
                            .focused($focus, equals: .token).submitLabel(.go)
                            .onSubmit { submit() }
                            .accessibilityIdentifier("pair.token")
                    }
                    Button { model.paste(UIPasteboard.general.string) } label: {
                        Text("Paste from clipboard").font(CalFont.status).foregroundStyle(Color.calAmber)
                            .frame(minHeight: Spacing.hit, alignment: .leading)
                    }
                }
                .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s)
            }
            .scrollDismissesKeyboard(.interactively)
        }
        .safeAreaInset(edge: .bottom) {
            FooterBar { CalButton("Pair this phone", action: submit).accessibilityIdentifier("pair.submit") }
        }
    }

    /// The model reports one message; show it under the field it is about.
    private var hostError: String? {
        guard let e = model.formError, !e.hasPrefix("Access tokens") else { return nil }
        return e
    }
    private var tokenError: String? {
        guard let e = model.formError, e.hasPrefix("Access tokens") else { return nil }
        return e
    }

    private func submit() { focus = nil; Task { await model.submit() } }
}

// MARK: Checking / failed

private struct StepList: View {
    let steps: [CheckStep]
    var body: some View {
        VStack(spacing: 0) {
            ForEach(Array(steps.enumerated()), id: \.element.id) { n, s in
                ChecklistRow(s.title, sub: s.detailText, status: s.statusText, kind: s.statusKind, isFirst: n == 0)
                    .accessibilityIdentifier("check.\(s.id.rawValue)")
            }
        }
    }
}

private struct CheckingView: View {
    let model: PairingViewModel
    private var progress: Double {
        let done = model.steps.filter { if case .ok = $0.state { true } else { false } }.count
        let running = model.steps.contains { $0.state == .running } ? 0.5 : 0
        return (Double(done) + running) / Double(max(model.steps.count, 1))
    }
    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            ScreenNav("Your server", leading: .none)
            Text("Checking").font(CalFont.display).foregroundStyle(Color.calInk)
                .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s)
            Text(model.host).font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calInk2)
                .lineLimit(1).truncationMode(.middle)
                .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s)
            ProgressRule(progress).padding(.top, Spacing.xl)
            StepList(steps: model.steps).padding(.top, Spacing.s)
            Spacer()
        }
        .safeAreaInset(edge: .bottom) {
            FooterBar { CalButton("Cancel", kind: .outline, action: model.cancel).accessibilityIdentifier("pair.cancel") }
        }
    }
}

private struct PairFailedView: View {
    let model: PairingViewModel
    let failure: PairFailure
    var body: some View {
        VStack(spacing: 0) {
            ScreenNav("Your server", leading: .back, onLeading: model.editAddress)
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    AmberBanner(failure.headline, message: failure.detail, kind: .error).padding(.top, Spacing.s)
                        .accessibilityIdentifier("pair.failure")
                    if !failure.hints.isEmpty {
                        Text("Check these").calLabel()
                            .padding(.horizontal, Spacing.gutter).padding(.top, 32).padding(.bottom, 10)
                        ForEach(Array(failure.hints.enumerated()), id: \.offset) { n, h in
                            ListRow(h.title, sub: h.detail, color: nil, height: Spacing.rowHeightTall, isFirst: n == 0) { EmptyView() }
                        }
                    }
                }
            }
        }
        .safeAreaInset(edge: .bottom) {
            FooterBar {
                CalButton("Try again") { Task { await model.submit() } }.accessibilityIdentifier("pair.retry")
                if !failure.isLocal { CalButton("Edit address", kind: .outline, action: model.editAddress) }
            }
        }
    }
}

// MARK: Paired

private struct PairedView: View {
    let hello: Hello
    let host: String
    let finish: (Bool) -> Void
    var body: some View {
        VStack(alignment: .leading, spacing: 0) {
            FlipCells("PAIRED", size: .xl).padding(.top, 60)
            Text("This phone is\non your board.")
                .font(CalFont.welcome).foregroundStyle(Color.calInk).padding(.top, 36)
                .fixedSize(horizontal: false, vertical: true)
            VStack(spacing: 0) {
                kv("Server", host).accessibilityIdentifier("paired.server")
                kv("Version", "\(hello.server) \(hello.version)")
                kv("Token", "Calendar \u{00B7} this phone")
            }
            .padding(.top, 32)
            Spacer()
        }
        .padding(.horizontal, Spacing.gutter)
        .frame(maxWidth: .infinity, alignment: .leading)
        .safeAreaInset(edge: .bottom) {
            FooterBar {
                CalButton("Add your first account") { finish(true) }.accessibilityIdentifier("paired.add")
                CalButton("Later", kind: .outline) { finish(false) }
            }
        }
    }

    private func kv(_ key: String, _ value: String) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 0) {
            Text(key).calLabel().frame(width: 90, alignment: .leading)
            Text(value).font(CalFont.body).foregroundStyle(Color.calInk).lineLimit(1).truncationMode(.middle)
            Spacer(minLength: 0)
        }
        .padding(.vertical, 14)
        .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
        .accessibilityElement(children: .combine)
    }
}
