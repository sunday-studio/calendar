import CalendarCore
import CalendarUI
import SwiftUI

/// Move to another calendar (board d-move). Waits for the provider; cross-account moves warn first.
public struct MoveCalendarView: View {
    private let item: AgendaItem
    private let calendars: [HubCalendar]
    private let accountNames: [String: String]
    private let writer: any EventWriting
    private let onClose: (_ moved: Bool) -> Void
    @State private var selection: String
    @State private var isSaving = false
    @State private var error: String?

    public init(item: AgendaItem, calendars: [HubCalendar], accountNames: [String: String], writer: any EventWriting,
                onClose: @escaping (Bool) -> Void) {
        self.item = item; self.accountNames = accountNames; self.writer = writer; self.onClose = onClose
        // A repeating event can't leave its account (hub rule).
        let writable = calendars.filter { $0.access == "owner" || $0.access == "writer" }
        self.calendars = item.recurring ? writable.filter { $0.accountId == item.accountId } : writable
        _selection = State(initialValue: item.calendarId)
    }

    private var current: HubCalendar? { calendars.first { $0.id == item.calendarId } }
    private var target: HubCalendar? { calendars.first { $0.id == selection } }
    private var crossAccount: Bool { target.map { $0.accountId != item.accountId } ?? false }
    private var canMove: Bool { selection != item.calendarId && !isSaving }

    public var body: some View {
        VStack(spacing: 0) {
            ScreenNav("Move to", leading: .close, onLeading: { if !isSaving { onClose(false) } })
            if isSaving { ProgressRule(0.6) }
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    Text("\(item.title) is in \(current?.name ?? "its calendar") now.")
                        .font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calInk2)
                        .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s)
                    if let error {
                        AmberBanner("Couldn\u{2019}t move it.", message: error, kind: .error).padding(.top, Spacing.m)
                    } else if crossAccount {
                        AmberBanner("Moving to another account makes a copy there and removes this one.",
                                    message: "Guests may get a new invitation.", kind: .neutral).padding(.top, Spacing.m)
                    }
                    let groups = Dictionary(grouping: calendars, by: \.accountId)
                    ForEach(groups.keys.sorted(), id: \.self) { acct in
                        Text(accountNames[acct] ?? "Account").calLabel()
                            .padding(.horizontal, Spacing.gutter).padding(.top, 32).padding(.bottom, 10)
                        ForEach(Array((groups[acct] ?? []).enumerated()), id: \.element.id) { n, c in
                            Button { selection = c.id; error = nil } label: {
                                HStack(spacing: Spacing.l) {
                                    Rectangle().fill(AccountColor(key: c.colorOverride ?? c.color).color).frame(width: 14, height: 14)
                                    Text(c.name).font(CalFont.heading).foregroundStyle(Color.calInk)
                                    Spacer(minLength: 0)
                                    RadioMark(isOn: selection == c.id)
                                }
                                .padding(.horizontal, Spacing.gutter)
                                .frame(maxWidth: .infinity, alignment: .leading)
                                .frame(height: 56)
                                .overlay(alignment: .top) { Rectangle().fill(Color.calLine).frame(height: 1) }
                                .contentShape(Rectangle())
                            }
                            .buttonStyle(.plain)
                            .accessibilityAddTraits(selection == c.id ? .isSelected : [])
                        }
                    }
                    Color.clear.frame(height: Spacing.xxl)
                }
            }
            .opacity(isSaving ? 0.5 : 1).allowsHitTesting(!isSaving)
        }
        .safeAreaInset(edge: .bottom) {
            FooterBar { CalButton("Move", action: move).disabled(!canMove).accessibilityIdentifier("move.confirm") }
        }
        .background(Color.calBg)
        .presentationBackground(Color.calBg)
        .accessibilityIdentifier("event.move")
    }

    private func move() {
        isSaving = true; error = nil
        Task {
            switch await writer.move(item, toCalendar: selection, key: nil, mode: .confirmed) {
            case .saved: isSaving = false; Haptics.shared.play(.saved); onClose(true)
            case .failed(let f): isSaving = false; error = EventEditorViewModel.message(for: f)
            }
        }
    }
}
