import CalendarCore
import CalendarUI
import SwiftUI

/// Search (canvas f-open, f-results, f-empty). Pushed on Home's stack; tapping a row opens the event.
public struct SearchScreen: View {
    @State private var model: SearchViewModel
    private let openEvent: (String) -> Void
    private let findFreeTime: (String) -> Void
    private let close: () -> Void
    @FocusState private var focused: Bool

    public init(model: SearchViewModel, openEvent: @escaping (String) -> Void, findFreeTime: @escaping (String) -> Void, close: @escaping () -> Void) {
        _model = State(initialValue: model); self.openEvent = openEvent; self.findFreeTime = findFreeTime; self.close = close
    }

    public var body: some View {
        VStack(spacing: 0) {
            HStack(spacing: 0) {
                Button(action: close) { CalGlyph(.back).foregroundStyle(Color.calInk).frame(width: Spacing.hit, height: Spacing.hit) }
                    .accessibilityLabel("Back").accessibilityIdentifier("search.close")
                TextField("Search events and people", text: $model.query)
                    .font(CalFont.body).foregroundStyle(Color.calInk).tint(Color.calAmber)
                    .focused($focused).submitLabel(.search).autocorrectionDisabled()
                    .onSubmit { Task { await model.submit() } }
                    .padding(.horizontal, 14).frame(height: 44)
                    .background(Color.calSurface)
                    .overlay(Rectangle().strokeBorder(focused ? Color.calInk : Color.calBorder, lineWidth: 1))
                    .accessibilityIdentifier("search.field")
                Color.clear.frame(width: Spacing.s, height: 1)
            }
            .padding(.horizontal, Spacing.s).frame(height: Spacing.navHeight)
            if model.phase == .searching { ProgressRule().accessibilityLabel("Searching") }
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    if model.trimmed.isEmpty { open } else { results }
                }
            }
            .scrollDismissesKeyboard(.immediately)
        }
        .background(Color.calBg)
        .toolbar(.hidden, for: .navigationBar)
        .onAppear { focused = true }
    }

    // f-open
    @ViewBuilder private var open: some View {
        if !model.recents.isEmpty {
            Text("Recent").calLabel().padding(.horizontal, Spacing.gutter).padding(.top, Spacing.m).padding(.bottom, 4)
            ForEach(model.recents, id: \.self) { term in
                Button { model.fill(term) } label: {
                    HStack {
                        Text(term).font(CalFont.body).foregroundStyle(Color.calInk)
                        Spacer()
                        Button { model.removeRecent(term) } label: {
                            CalGlyph(.close, size: 12).foregroundStyle(Color.calInk2).frame(width: Spacing.hit, height: Spacing.hit)
                        }.buttonStyle(.plain).accessibilityLabel("Remove \(term)")
                    }
                    .padding(.leading, Spacing.gutter).padding(.trailing, Spacing.s)
                    .frame(maxWidth: .infinity, minHeight: 52, alignment: .leading)
                    .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
            }
        }
        Text("Search looks across all your connected accounts, on your server.").font(CalFont.hint).foregroundStyle(Color.calInk2)
            .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.l)
    }

    // f-results, f-empty
    @ViewBuilder private var results: some View {
        if case .failed(let m) = model.phase {
            AmberBanner("Search didn\u{2019}t work.", message: m, kind: .error).padding(.top, Spacing.s)
        } else if model.isEmptyResult {
            QuietCard(title: "Nothing found", footer: model.emptyMessage + (model.savedOnly ? " Searching saved events only." : ""))
                .padding(.top, Spacing.s).accessibilityIdentifier("search.empty")
            CalButton("Find free time instead", kind: .outline) { findFreeTime(model.trimmed) }
                .padding(.horizontal, Spacing.gutter).padding(.top, 24)
        } else {
            if model.savedOnly {
                Text("Searching saved events only.").font(CalFont.hint).foregroundStyle(Color.calInk2)
                    .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s)
            }
            if !model.people.isEmpty {
                Text("People").calLabel().padding(.horizontal, Spacing.gutter).padding(.top, Spacing.m).padding(.bottom, 4)
                ForEach(Array(model.people.enumerated()), id: \.element.id) { n, p in
                    Button { model.fill(p.name.isEmpty ? p.email : p.name) } label: { personRow(p, first: n == 0) }.buttonStyle(.plain)
                }
            }
            if !model.upcoming.isEmpty {
                Text("Coming up \u{00B7} \(model.upcoming.count)").calLabel().padding(.horizontal, Spacing.gutter).padding(.top, Spacing.m).padding(.bottom, 4)
                ForEach(Array(model.upcoming.enumerated()), id: \.element.id) { n, r in row(r, first: n == 0) }
            }
            if !model.past.isEmpty {
                Text("Past \u{00B7} \(model.past.count)").calLabel().padding(.horizontal, Spacing.gutter).padding(.top, Spacing.l).padding(.bottom, 4)
                ForEach(Array(model.past.enumerated()), id: \.element.id) { n, r in row(r, first: n == 0).opacity(0.45) }
            }
        }
    }

    private func row(_ r: SearchViewModel.Result, first: Bool) -> some View {
        Button { model.remember(); openEvent(r.id) } label: {
            HStack(spacing: Spacing.m) {
                Text(r.dateWords).font(CalFont.time).foregroundStyle(Color.calInk2).frame(width: 56, alignment: .leading).lineLimit(1).minimumScaleFactor(0.7)
                VStack(alignment: .leading, spacing: 3) {
                    Text(r.title).font(CalFont.heading).foregroundStyle(Color.calInk).lineLimit(1)
                    Text(r.sub).font(CalFont.sub).foregroundStyle(Color.calInk2).lineLimit(1)
                }
                Spacer(minLength: 0)
            }
            .padding(.horizontal, Spacing.gutter).frame(maxWidth: .infinity, minHeight: Spacing.rowHeightTall, alignment: .leading)
            .overlay(alignment: .leading) { AccountBar(AccountColor(key: r.color), height: 32) }
            .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
            .contentShape(Rectangle())
        }
        .buttonStyle(.plain)
        .accessibilityElement(children: .combine)
        .accessibilityIdentifier("search.result")
    }

    private func personRow(_ p: PersonHit, first: Bool) -> some View {
        HStack(spacing: Spacing.m) {
            Text(initials(p)).font(CalFont.label).foregroundStyle(Color.calInk)
                .frame(width: 36, height: 36).overlay(Rectangle().strokeBorder(Color.calBorder, lineWidth: 1))
            VStack(alignment: .leading, spacing: 2) {
                Text(p.name.isEmpty ? p.email : p.name).font(CalFont.heading).foregroundStyle(Color.calInk).lineLimit(1)
                Text(p.events == 1 ? "1 event" : "\(p.events) events").font(CalFont.sub).foregroundStyle(Color.calInk2)
            }
            Spacer(minLength: 0)
        }
        .padding(.horizontal, Spacing.gutter).frame(maxWidth: .infinity, minHeight: 64, alignment: .leading)
        .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
        .contentShape(Rectangle())
    }

    private func initials(_ p: PersonHit) -> String {
        let src = p.name.isEmpty ? p.email : p.name
        let parts = src.split(separator: " ").prefix(2).compactMap(\.first)
        return String(parts).uppercased()
    }
}
