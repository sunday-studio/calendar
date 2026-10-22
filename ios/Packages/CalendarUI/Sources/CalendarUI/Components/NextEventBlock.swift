import SwiftUI

/// The one bright block on a screen (canvas `.nx`): the next or current event, in its account colour.
/// Flip cells with the start time on the left; title 30 Medium; footer line with an inline action on the right.
/// Variants: live ("Now · 22 min left", Join stays), no link (outlined Map + walking time), quiet (`quiet(...)`).
public struct NextEventBlock: View {
    public enum Action: Equatable, Sendable {
        case join(String)          // filled: "Join"
        case map(String)           // outlined: "Map"
        case details(String)       // outlined: "Details"
        case none
    }

    let time: String?
    let title: String
    let footer: String
    let color: AccountColor
    let action: Action
    let onAction: (() -> Void)?

    /// Canvas initialiser. `footer`: "in 8 min", "Now · 22 min left", "Kōya · 14 min walk".
    public init(time: String?, title: String, footer: String, color: AccountColor,
                action: Action = .none, onAction: (() -> Void)? = nil) {
        self.time = time; self.title = title; self.footer = footer
        self.color = color; self.action = action; self.onAction = onAction
    }

    private var ink: Color { Theme.onAccountColor.color }

    public var body: some View {
        HStack(alignment: .top, spacing: Spacing.l) {
            if let time { FlipCells(time, size: .regular, onBlock: true) }
            VStack(alignment: .leading, spacing: 10) {
                Text(title).font(CalFont.nextTitle).foregroundStyle(ink)
                    .lineLimit(1).frame(height: 44, alignment: .leading)
                HStack(spacing: Spacing.m) {
                    Text(footer).font(CalFont.footer).foregroundStyle(ink).lineLimit(1)
                    Spacer(minLength: Spacing.s)
                    actionView
                }
            }
        }
        .padding(.horizontal, Spacing.gutter)
        .padding(.vertical, 22)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(color.bright)
        .accessibilityElement(children: .contain)
    }

    @ViewBuilder private var actionView: some View {
        switch action {
        case .join(let label):
            Button { onAction?() } label: {
                Text(label).font(CalFont.blockButton).foregroundStyle(Theme.flipInk.color)
                    .padding(.horizontal, 20).padding(.vertical, 7)
                    .background(Theme.onAccountColor.color)
            }
            .buttonStyle(.plain).frame(minHeight: Spacing.hit)
        case .map(let label), .details(let label):
            Button { onAction?() } label: {
                Text(label).font(CalFont.blockButton).foregroundStyle(ink)
                    .padding(.horizontal, 20).padding(.vertical, 7)
                    .overlay(Rectangle().strokeBorder(ink, lineWidth: 1))
            }
            .buttonStyle(.plain).frame(minHeight: Spacing.hit)
        case .none:
            EmptyView()
        }
    }
}

/// Quiet variant of the Next block (canvas `.nx.q`): surface fill, hairlines, no colour, no cells.
/// "Done for today", "Clear until Thursday", "No calendars yet".
public struct QuietCard: View {
    let time: String?
    let title: String
    let footer: String?

    /// `time`: show flip cells on the left (the "server unreachable" saved-copy card, canvas y-down).
    public init(time: String? = nil, title: String, footer: String? = nil) {
        self.time = time
        self.title = title
        self.footer = footer
    }

    public var body: some View {
        HStack(alignment: .top, spacing: Spacing.l) {
            if let time { FlipCells(time, size: .regular) }
            textBlock
        }
        .padding(.horizontal, Spacing.gutter).padding(.vertical, 22)
        .frame(maxWidth: .infinity, alignment: .leading)
        .background(Color.calSurface)
        .overlay(alignment: .top) { Rectangle().fill(Color.calLine).frame(height: 1) }
        .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
        .accessibilityElement(children: .combine)
    }

    private var textBlock: some View {
        VStack(alignment: .leading, spacing: 10) {
            Text(title).font(CalFont.nextTitle).foregroundStyle(Color.calInk)
                .fixedSize(horizontal: false, vertical: true)
            if let footer {
                Text(footer).font(CalFont.footer).foregroundStyle(Color.calInk2)
                    .fixedSize(horizontal: false, vertical: true)
            }
        }
    }
}

