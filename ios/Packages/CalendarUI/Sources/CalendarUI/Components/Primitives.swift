import SwiftUI

/// Small square swatch for an account/calendar colour. `hollow` for past/declined.
public struct AccountDot: View {
    let color: AccountColor
    var hollow: Bool
    var size: CGFloat

    public init(_ color: AccountColor, hollow: Bool = false, size: CGFloat = 9) {
        self.color = color
        self.hollow = hollow
        self.size = size
    }

    public var body: some View {
        Rectangle()
            .fill(hollow ? Color.clear : color.color)
            .overlay { if hollow { Rectangle().strokeBorder(color.color, lineWidth: 1.5) } }
            .frame(width: size, height: size)
            .accessibilityHidden(true)
    }
}

/// Diagonal hatching (135 degrees) for elapsed and unavailable time on timelines (canvas `.past`):
/// 1pt lines every 7pt in the hairline colour. Not used on agenda rows.
public struct HatchedShade: View {
    var spacing: CGFloat
    var lineWidth: CGFloat

    public init(spacing: CGFloat = 7, lineWidth: CGFloat = 1) {
        self.spacing = spacing
        self.lineWidth = lineWidth
    }

    public var body: some View {
        let ink = Theme.line.color.opacity(0.8)
        Canvas { ctx, size in
            var path = Path()
            let h = size.height
            var x = -h
            while x < size.width {
                path.move(to: CGPoint(x: x, y: 0))
                path.addLine(to: CGPoint(x: x + h, y: h))
                x += spacing
            }
            ctx.stroke(path, with: .color(ink), lineWidth: lineWidth)
        }
        .accessibilityHidden(true)
    }
}

/// The "now" line (canvas `.now`): 1pt amber with an optional time tag (mono Medium 11, ink on amber) at x=8.
public struct BrassLine: View {
    let label: String?
    public init(label: String? = nil) { self.label = label }

    public var body: some View {
        ZStack(alignment: .leading) {
            Rectangle().fill(Color.calAccent).frame(height: 1)
            if let label {
                Text(label).font(CalFont.font(.monoMedium, size: 11, relativeTo: .caption2)).monospacedDigit()
                    .foregroundStyle(Theme.onAmber.color)
                    .padding(.horizontal, 5).frame(height: 18)
                    .background(Color.calAccent)
                    .padding(.leading, 8)
            }
        }
        .frame(height: label == nil ? 1 : 18)
        .accessibilityElement(children: .ignore)
        .accessibilityLabel(label.map { "Now, \($0)" } ?? "Now")
    }
}

/// Section label (canvas `.lb`): mono 11, +0.14em, UPPERCASE, dim. "Today", "Tomorrow", "Earlier today", "Tue 13 Oct".
/// `detail`, `count` and `isToday` are accepted for source compatibility and ignored: the canvas label is the title only.
public struct DaySectionHeader: View {
    let title: String

    public init(title: String, detail: String? = nil, count: Int? = nil, isToday: Bool = false) {
        self.title = title
    }

    public var body: some View {
        Text(title).calLabel()
            .padding(.horizontal, Spacing.gutter)
            .padding(.top, 32).padding(.bottom, 10)
            .frame(maxWidth: .infinity, alignment: .leading)
            .accessibilityAddTraits(.isHeader)
    }
}

/// Banner (canvas `.bn`): surface, hairlines top and bottom, 3pt bar at the left (amber, or red), one sentence with
/// the subject in Medium, action text on the right. "[work email] needs you to sign in again. Its events are paused." · Fix
public struct AmberBanner: View {
    public enum Kind: Sendable { case attention, error, neutral }

    let title: String
    let message: String?
    let actionTitle: String?
    let action: (() -> Void)?
    let kind: Kind

    public init(_ title: String, message: String? = nil, actionTitle: String? = nil, action: (() -> Void)? = nil,
                kind: Kind = .attention) {
        self.title = title
        self.message = message
        self.actionTitle = actionTitle
        self.action = action
        self.kind = kind
    }

    private var tint: Color {
        switch kind {
        case .error: .calRed
        case .neutral: .calBorder
        case .attention: .calAmber
        }
    }

    public var body: some View {
        HStack(spacing: Spacing.m) {
            (Text(title).font(CalFont.font(.sansMedium, size: 15, relativeTo: .subheadline))
                + Text(message.map { " " + $0 } ?? "").font(CalFont.status))
                .foregroundStyle(Color.calInk)
                .frame(maxWidth: .infinity, alignment: .leading)
            if let actionTitle, let action {
                Button(action: action) {
                    Text(actionTitle).font(CalFont.font(.sansMedium, size: 15, relativeTo: .subheadline)).foregroundStyle(tint)
                        .frame(minWidth: Spacing.hit, minHeight: Spacing.hit)
                }
                .buttonStyle(.plain)
            }
        }
        .padding(.horizontal, Spacing.gutter).padding(.vertical, 6)
        .frame(maxWidth: .infinity, minHeight: 52)
        .background(Color.calSurface)
        .overlay(alignment: .leading) { Rectangle().fill(tint).frame(width: 3).padding(.vertical, 12) }
        .overlay(alignment: .top) { Rectangle().fill(Color.calLine).frame(height: 1) }
        .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
        .accessibilityElement(children: .contain)
    }
}

/// Empty state: the quiet card plus at most one outlined button ("New event"), as in the canvas.
public struct EmptyState: View {
    let title: String
    let message: String
    let actionTitle: String?
    let action: (() -> Void)?

    public init(title: String, message: String, actionTitle: String? = nil, action: (() -> Void)? = nil) {
        self.title = title
        self.message = message
        self.actionTitle = actionTitle
        self.action = action
    }

    public var body: some View {
        VStack(spacing: 0) {
            QuietCard(title: title, footer: message)
            if let actionTitle, let action {
                CalButton(actionTitle, kind: .outline, action: action)
                    .padding(.horizontal, Spacing.gutter).padding(.top, 36)
            }
        }
        .accessibilityElement(children: .contain)
    }
}

/// Loading placeholder shaped like a row (canvas `.sk`: flat 14pt bars on the raised surface). Static under Reduce Motion.
public struct SkeletonRow: View {
    @Environment(\.accessibilityReduceMotion) private var reduceMotion
    @State private var phase: CGFloat = 0

    public init() {}

    public var body: some View {
        HStack(spacing: Spacing.l) {
            bar(width: 40).frame(width: Spacing.timeColumn, alignment: .leading)
            bar(width: 180)
            Spacer(minLength: 0)
        }
        .padding(.horizontal, Spacing.gutter)
        .frame(maxWidth: .infinity, minHeight: Spacing.rowHeight, alignment: .leading)
        .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
        .opacity(reduceMotion ? 1 : 0.6 + 0.4 * Double(phase))
        .onAppear {
            guard !reduceMotion else { return }
            withAnimation(.easeInOut(duration: 1.1).repeatForever(autoreverses: true)) { phase = 1 }
        }
        .accessibilityElement(children: .ignore)
        .accessibilityLabel("Loading")
    }

    private func bar(width: CGFloat) -> some View {
        Rectangle().fill(Color.calSunk).frame(width: width, height: 14)
    }
}

/// Toast (canvas `.ts`): inset 12, ink fill with bg text, action in Medium. Position it ~96pt above the bottom.
public struct ToastView: View {
    let message: String
    let detail: String?
    let actionTitle: String?
    let action: (() -> Void)?

    /// `detail`: optional second line (canvas v-fail: "1:1 Maya went back to 15:00." / "[work email] refused the change.").
    public init(_ message: String, detail: String? = nil, actionTitle: String? = nil, action: (() -> Void)? = nil) {
        self.message = message
        self.detail = detail
        self.actionTitle = actionTitle
        self.action = action
    }

    public var body: some View {
        HStack(spacing: Spacing.m) {
            VStack(alignment: .leading, spacing: 2) {
                Text(message).font(detail == nil ? CalFont.font(.sans, size: 16, relativeTo: .body) : CalFont.font(.sansMedium, size: 16, relativeTo: .body))
                if let detail { Text(detail).font(CalFont.font(.sans, size: 16, relativeTo: .body)) }
            }
            .foregroundStyle(Theme.inverseInk.color)
            Spacer(minLength: Spacing.s)
            if let actionTitle, let action {
                Button(action: action) {
                    Text(actionTitle).font(CalFont.font(.sansMedium, size: 16, relativeTo: .body))
                        .foregroundStyle(Theme.inverseInk.color)
                        .frame(minHeight: Spacing.hit)
                }
            }
        }
        .padding(.horizontal, 16).padding(.vertical, 6)
        .frame(maxWidth: .infinity, minHeight: 52)
        .background(Theme.inverse.color)
        .padding(.horizontal, 12)
        .accessibilityElement(children: .contain)
        .accessibilityAddTraits(.updatesFrequently)
    }
}

/// Canvas button (`.bt`): full width, h52, square, 17 Medium. `primary` ink fill; `outline` 1pt border;
/// `amber` and `danger` 1pt coloured outline. Disabled = 35%. `small`: h40, auto width, 15pt.
public struct CalButton: View {
    public enum Kind: Sendable { case primary, outline, amber, danger }
    public enum Size: Sendable { case regular, small }

    let title: String
    let kind: Kind
    let size: Size
    let action: () -> Void
    @Environment(\.isEnabled) private var isEnabled

    public init(_ title: String, kind: Kind = .primary, size: Size = .regular, action: @escaping () -> Void) {
        self.title = title
        self.kind = kind
        self.size = size
        self.action = action
    }

    private var fg: Color {
        switch kind {
        case .primary: .calBg
        case .outline: .calInk
        case .amber: .calAmber
        case .danger: .calRed
        }
    }

    private var stroke: Color? {
        switch kind {
        case .primary: nil
        case .outline: .calControl
        case .amber: .calAmber
        case .danger: .calRed
        }
    }

    public var body: some View {
        Button(action: action) {
            Text(title)
                .font(size == .small ? CalFont.font(.sansMedium, size: 15, relativeTo: .subheadline) : CalFont.bodyMedium)
                .foregroundStyle(fg)
                .padding(.horizontal, size == .small ? 18 : 0)
                .frame(maxWidth: size == .small ? nil : .infinity)
                .frame(height: size == .small ? 40 : Spacing.buttonHeight)
                .background(kind == .primary ? Color.calInk : Color.clear)
                .overlay { if let stroke { Rectangle().strokeBorder(stroke, lineWidth: 1) } }
        }
        .buttonStyle(.plain)
        .frame(minHeight: Spacing.hit)
        .opacity(isEnabled ? 1 : 0.35)
    }
}

public struct PrimaryButton: View {
    let title: String
    let action: () -> Void
    public init(_ title: String, action: @escaping () -> Void) { self.title = title; self.action = action }
    public var body: some View { CalButton(title, kind: .primary, action: action) }
}

public struct SecondaryButton: View {
    let title: String
    let action: () -> Void
    public init(_ title: String, action: @escaping () -> Void) { self.title = title; self.action = action }
    public var body: some View { CalButton(title, kind: .outline, action: action) }
}

/// Pinned footer (canvas `.ft2`): bg fill, hairline on top, padding 16/24, buttons stacked with gap 10.
/// Use with `.safeAreaInset(edge: .bottom) { FooterBar { ... } }` (the home-indicator inset supplies the extra bottom space).
public struct FooterBar<Content: View>: View {
    let content: Content
    public init(@ViewBuilder _ content: () -> Content) { self.content = content() }

    public var body: some View {
        VStack(spacing: 10) { content }
            .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.l).padding(.bottom, Spacing.l)
            .frame(maxWidth: .infinity)
            .background(Color.calBg.ignoresSafeArea(edges: .bottom))
            .overlay(alignment: .top) { Rectangle().fill(Color.calLine).frame(height: 1) }
    }
}
