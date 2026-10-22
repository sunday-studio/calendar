import CalendarCore
import CalendarUI
import SwiftUI
import UIKit

/// Event detail (boards d-detail, d-nolink, d-people, d-recur, d-actions, d-del, d-delrec, d-ro, d-join).
/// Edit, move, duplicate, delete and RSVP report an `EventAction`; the host performs the write.
public struct EventDetailView: View {
    @State private var model: EventDetailViewModel
    @State private var showActions = false
    @State private var showDelete = false
    @State private var showJoin = false
    @State private var showPeople = false
    @State private var showShare = false
    @State private var deleteScope: EventScope = .this
    @State private var replyStep: ReplyStep?
    @State private var replyNote = ""
    @State private var replyScope: EventScope = .all
    @State private var changingReply = false
    @State private var toast: String?
    private let writer: (any EventWriting)?
    private let onEdit: (EventDetail) -> Void
    private let onDuplicate: (EventDetail) -> Void
    private let onMove: (EventDetail) -> Void
    @Environment(\.openURL) private var openURL
    @Environment(\.dismiss) private var dismiss

    private enum ReplyStep: Equatable { case note(RSVPResponse), series(RSVPResponse) }

    /// `writer` performs RSVP and delete (waiting for the provider); `onEdit` opens the editor.
    public init(model: EventDetailViewModel, writer: (any EventWriting)? = nil, onEdit: @escaping (EventDetail) -> Void = { _ in },
                onDuplicate: @escaping (EventDetail) -> Void = { _ in }, onMove: @escaping (EventDetail) -> Void = { _ in }) {
        _model = State(initialValue: model)
        self.writer = writer
        self.onEdit = onEdit; self.onDuplicate = onDuplicate; self.onMove = onMove
    }

    public var body: some View {
        VStack(spacing: 0) {
            ScreenNav("", leading: .back, trailing: model.readOnly || model.item == nil ? nil : "More",
                      onLeading: { dismiss() }, onTrailing: { showActions = true })
            Group {
                if case .failed(let message) = model.phase, model.item == nil {
                    failed(message)
                } else if model.item == nil {
                    Color.clear.frame(maxWidth: .infinity, maxHeight: .infinity).accessibilityLabel("Loading event")
                } else {
                    content
                }
            }
        }
        .background(Color.calBg)
        .toolbar(.hidden, for: .navigationBar)
        .navigationDestination(isPresented: $showPeople) { PeopleView(model: model) }
        .sheet(isPresented: $showShare) { ActivityView(items: [shareText]).presentationDetents([.medium, .large]) }
        .onChange(of: showActions) { _, on in if on { Haptics.shared.play(.sheetOpen) } }
        .onChange(of: showDelete) { _, on in if on { Haptics.shared.play(.sheetOpen) } }
        .onChange(of: showJoin) { _, on in if on { Haptics.shared.play(.sheetOpen) } }
        .onChange(of: replyStep) { _, new in if new != nil { Haptics.shared.play(.sheetOpen) } }
        .calSheet(isPresented: $showActions) { actionsSheet }
        .calSheet(isPresented: $showDelete) { deleteSheet }
        .calSheet(isPresented: $showJoin) { joinSheet }
        .calSheet(isPresented: Binding(get: { replyStep != nil }, set: { if !$0 { replyStep = nil } })) { replySheet }
        .overlay(alignment: .bottom) {
            if let t = toast { ToastView(t, actionTitle: "OK") { toast = nil }.padding(.bottom, 60) }
        }
        .task { await model.load() }
        .accessibilityIdentifier("event.detail")
    }

    // MARK: Content

    private var content: some View {
        ScrollView {
            VStack(alignment: .leading, spacing: 0) {
                if model.refreshFailed {
                    AmberBanner("Couldn\u{2019}t refresh.", message: "Showing the saved copy.", actionTitle: "Retry",
                                action: { Task { await model.refresh() } }, kind: .error)
                        .accessibilityIdentifier("event.stale")
                }
                if let e = model.replyError {
                    AmberBanner("Your reply wasn\u{2019}t sent.", message: e, actionTitle: "Retry",
                                action: { if let c = model.pendingChoice { send(RSVPResponse(rawValue: c) ?? .accepted, note: replyNote, scope: replyScope) } },
                                kind: .error)
                        .padding(.top, 24).accessibilityIdentifier("event.replyFailed")
                }
                header
                joinBar
                if let word = model.myResponseWord, model.item?.canRSVP == true, !changingReply {
                    HStack(alignment: .firstTextBaseline) {
                        VStack(alignment: .leading, spacing: 6) { Text("Your reply").calLabel(); Text(word).font(CalFont.footer).foregroundStyle(Color.calInk) }
                        Spacer()
                        Button { changingReply = true } label: { Text("Change").font(CalFont.status).foregroundStyle(Color.calAmber).frame(minHeight: Spacing.hit) }
                    }
                    .padding(.horizontal, Spacing.gutter).padding(.vertical, 8)
                    .padding(.top, 8)
                }
                rows
                if model.isRecurring {
                    Text("Editing or deleting will ask whether the change is for this event, this and following, or all.")
                        .font(CalFont.hint).foregroundStyle(Color.calInk2)
                        .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.l)
                }
                Color.clear.frame(height: Spacing.xxl)
            }
        }
        .refreshable { await model.refresh() }
        .safeAreaInset(edge: .bottom, spacing: 0) {
            if model.showsReplyBar || changingReply, model.item?.canRSVP == true {
                VStack(spacing: 0) {
                    if model.isSending { ProgressRule(0.6) }
                    FooterBar {
                        ReplyBar(selected: model.pendingChoice.map(choice(for:)), isSending: model.isSending) { choose($0) }
                    }
                }
                .accessibilityIdentifier("event.reply")
            }
        }
    }

    private var header: some View {
        VStack(alignment: .leading, spacing: 0) {
            if let item = model.item {
                HStack(spacing: 10) {
                    Rectangle().fill(AccountColor(key: item.color).color).frame(width: 14, height: 14)
                    Text(eyebrowText).calLabel().lineLimit(1)
                    if model.readOnly { CalTag("Read only") }
                    if item.status == "cancelled" { CalTag("Cancelled", amber: true) }
                }
                .padding(.top, 4)
                Text(item.title.isEmpty ? "(No title)" : item.title)
                    .font(CalFont.display).foregroundStyle(Color.calInk).lineSpacing(2)
                    .fixedSize(horizontal: false, vertical: true)
                    .padding(.top, 14)
                    .accessibilityAddTraits(.isHeader)
                    .accessibilityIdentifier("event.title")
            }
            if let w = model.whenLine {
                Text(w).font(CalFont.footer).foregroundStyle(Color.calInk).padding(.top, 10)
                    .accessibilityIdentifier("event.time")
            }
            if let rel = model.relative {
                Text(rel).font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calAmber).padding(.top, 2)
            }
        }
        .padding(.horizontal, Spacing.gutter)
        .frame(maxWidth: .infinity, alignment: .leading)
        .accessibilityElement(children: .combine)
    }

    private var eyebrowText: String {
        model.readOnly && !model.eyebrow.isEmpty ? model.eyebrow : model.eyebrow.isEmpty ? "Event" : model.eyebrow
    }

    @ViewBuilder private var joinBar: some View {
        if let item = model.item {
            if let j = model.join {
                JoinBar(Self.providerTitle(j.join), button: "Join",
                        color: AccountColor(key: item.color)) { showJoin = true }
                    .padding(.top, Spacing.l).accessibilityIdentifier("event.join")
            } else if let loc = item.location, !loc.isEmpty {
                JoinBar(loc, button: "Map", color: AccountColor(key: item.color)) {
                    if let u = model.mapsURL { openURL(u) }
                }
                .padding(.top, Spacing.l).accessibilityIdentifier("event.map")
            }
        }
    }

    @ViewBuilder private var rows: some View {
        VStack(spacing: 0) {
            if let loc = model.item?.location, !loc.isEmpty { KeyValueRow("Where", loc) }
            if model.isRecurring, let r = model.recurrenceText {
                VStack(alignment: .leading, spacing: 6) {
                    Text("Repeats").calLabel()
                    HStack(spacing: Spacing.s) {
                        Text(r).font(CalFont.footer).foregroundStyle(Color.calInk)
                        CalTag("Series")
                    }
                }
                .padding(.horizontal, Spacing.gutter).padding(.vertical, 14)
                .frame(maxWidth: .infinity, alignment: .leading)
                .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
            }
            if !model.people.isEmpty {
                Button { showPeople = true } label: {
                    VStack(alignment: .leading, spacing: 6) {
                        Text("People").calLabel()
                        HStack(alignment: .firstTextBaseline) {
                            Text(model.peopleNames).font(CalFont.footer).foregroundStyle(Color.calInk).lineLimit(1)
                            Spacer(minLength: Spacing.s)
                            Text(model.peopleCounts).font(CalFont.status).foregroundStyle(Color.calInk2).lineLimit(1)
                        }
                    }
                    .padding(.horizontal, Spacing.gutter).padding(.vertical, 14)
                    .frame(maxWidth: .infinity, alignment: .leading)
                    .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
                    .contentShape(Rectangle())
                }
                .buttonStyle(.plain)
                .accessibilityLabel("People, \(model.peopleCounts)")
                .accessibilityIdentifier("event.people")
            }
            if let r = model.reminderText { KeyValueRow("Reminder", r) }
            if model.readOnly {
                VStack(alignment: .leading, spacing: 6) {
                    Text("Calendar").calLabel()
                    Text(model.readOnlyExplanation)
                        .font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calInk2)
                }
                .padding(.horizontal, Spacing.gutter).padding(.vertical, 14)
                .frame(maxWidth: .infinity, alignment: .leading)
                .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
            } else {
                VStack(alignment: .leading, spacing: 6) {
                    Text("Notes").calLabel()
                    Text(model.notesText).font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calInk2)
                        .lineLimit(6)
                }
                .padding(.horizontal, Spacing.gutter).padding(.vertical, 14)
                .frame(maxWidth: .infinity, alignment: .leading)
                .overlay(alignment: .bottom) { Rectangle().fill(Color.calLine).frame(height: 1) }
            }
        }
        .overlay(alignment: .top) { Rectangle().fill(Color.calLine).frame(height: 1) }
        .padding(.top, 8)
    }

    // MARK: Sheets

    private var actionsSheet: some View {
        CalSheet {
            SheetRow("Edit", isFirst: true) { showActions = false; if let d = model.detail { onEdit(d) } }
            SheetRow("Duplicate") { showActions = false; if let d = model.detail { onDuplicate(d) } }
            SheetRow("Move to another calendar", detail: model.calendarName) { showActions = false; if let d = model.detail { onMove(d) } }
            if let j = model.join {
                SheetRow("Copy join link") { UIPasteboard.general.string = j.join.url; showActions = false }
            }
            SheetRow("Share details") { showActions = false; showShare = true }
            SheetRow("Delete", isDestructive: true) {
                showActions = false; deleteScope = .this; showDelete = true
            }
        }
        .accessibilityIdentifier("event.actions")
    }

    private var shareText: String {
        [model.item?.title, model.whenLine, model.item?.location, model.join?.join.url]
            .compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: "\n")
    }

    @ViewBuilder private var deleteSheet: some View {
        let title = model.item?.title ?? "event"
        if model.isRecurring {
            CalSheet(title: "Delete \(title)") {
                SheetRow("This event", radio: deleteScope == .this, isFirst: true) { deleteScope = .this }
                SheetRow("This and following", radio: deleteScope == .following) { deleteScope = .following }
                SheetRow("All events", radio: deleteScope == .all) { deleteScope = .all }
                VStack(spacing: 10) {
                    CalButton("Delete", kind: .danger) { showDelete = false; performDelete(deleteScope) }
                    CalButton("Cancel", kind: .outline) { showDelete = false }
                }
                .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.m)
            }
        } else {
            let guests = !model.people.filter { !$0.isSelf }.isEmpty
            CalSheet(title: "Delete \(title)?",
                     message: "It is removed from your \(model.calendarName.map { $0 + " " } ?? "")calendar in Google."
                        + (guests ? " Guests get a cancellation." : "")) {
                VStack(spacing: 10) {
                    CalButton("Delete event", kind: .danger) { showDelete = false; performDelete(.this) }
                    CalButton("Keep it", kind: .outline) { showDelete = false }
                }
                .padding(.horizontal, Spacing.gutter)
            }
        }
    }

    private var joinSheet: some View {
        let j = model.join?.join
        let provider = j.map { JoinLabel.provider(for: $0) } ?? "the call"
        return CalSheet(title: "Open in \(j.map(Self.providerTitle) ?? provider)?", message: j?.url) {
            SheetRow("Open in the \(provider) app", isFirst: true) { showJoin = false; open(j?.url) }
            SheetRow("Open in the browser") { showJoin = false; open(j?.url) }
            SheetRow("Copy link") { UIPasteboard.general.string = j?.url; showJoin = false }
            CalButton("Cancel", kind: .outline) { showJoin = false }
                .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.m)
        }
    }

    // MARK: Writes

    private func choice(for raw: String) -> ReplyBar.Choice { raw == "accepted" ? .yes : raw == "tentative" ? .maybe : .no }

    private func choose(_ c: ReplyBar.Choice) {
        let r: RSVPResponse = c == .yes ? .accepted : c == .maybe ? .tentative : .declined
        replyNote = ""
        if r != .accepted { replyStep = .note(r) }
        else if model.isRecurring { replyScope = .all; replyStep = .series(r) }
        else { send(r, note: nil, scope: .this) }
    }

    private func send(_ r: RSVPResponse, note: String?, scope: EventScope) {
        guard let writer else { return }
        let previous = model.item?.myResponse
        let title = model.item?.title ?? "event"
        Task {
            if await model.reply(r, note: note, scope: scope, writer: writer) {
                Haptics.shared.play(.rsvpSent)
                changingReply = false
                let word = r == .accepted ? "yes" : r == .tentative ? "maybe" : "no"
                ToastCenter.shared.show("You said \(word) to \(title)", actionTitle: "Undo", seconds: 6) {
                    if let prev = previous, let back = Self.response(for: prev) { Task { if await model.reply(back, note: nil, scope: scope, writer: writer) { Haptics.shared.play(.undoRestored) } } }
                }
            }
        }
    }

    static func response(for wire: String) -> RSVPResponse? {
        switch wire { case "accepted": .accepted; case "tentative": .tentative; case "declined": .declined; default: nil }
    }

    private func performDelete(_ scope: EventScope) {
        guard let writer, let item = model.item else { return }
        let draft = model.detail.flatMap { EventDraft(recreating: $0) }
        Task {
            switch await writer.delete(item, scope: scope, key: nil, mode: .confirmed) {
            case .saved:
                Haptics.shared.play(.saved)
                ToastCenter.shared.show("\(item.title) deleted", actionTitle: "Undo", seconds: 6) {
                    if let draft { Task { if case .saved = await writer.create(draft, key: nil, mode: .confirmed) { Haptics.shared.play(.undoRestored) } } }
                }
                dismiss()
            case .failed(let f):
                toast = EventEditorViewModel.message(for: f)
            }
        }
    }

    private var replySheet: some View {
        let who = model.detail?.organizer?.name ?? model.detail?.organizer?.email ?? "The organiser"
        return Group {
            switch replyStep {
            case .note(let r):
                CalSheet(title: r == .tentative ? "Maybe" : "Can\u{2019}t make it", message: "\(who) gets your reply from \(model.detail?.accountName ?? "your account").") {
                    LabeledField("Note, optional") { TextField("", text: $replyNote).accessibilityLabel("Note, optional") }
                        .padding(.horizontal, Spacing.gutter).padding(.bottom, Spacing.m)
                    VStack(spacing: 10) {
                        CalButton("Send reply") {
                            if model.isRecurring { replyScope = .all; replyStep = .series(r) } else { replyStep = nil; send(r, note: replyNote, scope: .this) }
                        }
                        CalButton("Cancel", kind: .outline) { replyStep = nil }
                    }.padding(.horizontal, Spacing.gutter)
                }
            case .series(let r):
                CalSheet(title: "Reply \(r == .accepted ? "Yes" : r == .tentative ? "Maybe" : "No") to", message: model.recurrenceText) {
                    SheetRow("This event only", radio: replyScope == .this, isFirst: true) { replyScope = .this }
                    SheetRow("All events in the series", radio: replyScope == .all) { replyScope = .all }
                    VStack(spacing: 10) {
                        CalButton("Send reply") { replyStep = nil; send(r, note: replyNote, scope: replyScope) }
                        CalButton("Cancel", kind: .outline) { replyStep = nil }
                    }.padding(.horizontal, Spacing.gutter).padding(.top, Spacing.m)
                }
            case nil: EmptyView()
            }
        }
    }

    static func providerTitle(_ j: Join) -> String {
        switch JoinLabel.provider(for: j) {
        case "Meet": "Google Meet"
        case "Teams": "Microsoft Teams"
        case let p: p
        }
    }

    private func open(_ s: String?) { if let s, let u = URL(string: s) { openURL(u) } }

    private func failed(_ message: String) -> some View {
        VStack(alignment: .leading, spacing: 0) {
            AmberBanner("Couldn\u{2019}t load this event.", message: message, actionTitle: "Retry",
                        action: { Task { await model.retry() } }, kind: .error)
                .padding(.top, Spacing.s)
            Spacer()
        }
        .accessibilityIdentifier("event.error")
    }
}

/// People (board d-people): Organiser, Going, Not answered, Declined.
struct PeopleView: View {
    let model: EventDetailViewModel
    @Environment(\.dismiss) private var dismiss

    private var groups: [(title: String, rows: [GuestRow])] {
        let p = model.people
        let org = p.filter(\.isOrganizer)
        let going = p.filter { !$0.isOrganizer && ($0.response == .going || $0.response == .maybe) }
        let pending = p.filter { !$0.isOrganizer && $0.response == .noReply }
        let no = p.filter { !$0.isOrganizer && $0.response == .declined }
        return [("Organiser", org), ("Going \u{00B7} \(going.count)", going),
                ("Not answered \u{00B7} \(pending.count)", pending), ("Declined \u{00B7} \(no.count)", no)]
    }

    var body: some View {
        VStack(spacing: 0) {
            ScreenNav("People", leading: .back, onLeading: { dismiss() })
            ScrollView {
                VStack(alignment: .leading, spacing: 0) {
                    Text("\(model.item?.title ?? "") \u{00B7} \(model.people.count) invited")
                        .font(CalFont.font(.sans, size: 16, relativeTo: .body)).foregroundStyle(Color.calInk2)
                        .padding(.horizontal, Spacing.gutter).padding(.top, Spacing.s)
                    ForEach(Array(groups.enumerated()), id: \.offset) { _, g in
                        if g.title != "Organiser" || !g.rows.isEmpty {
                            Text(g.title).calLabel().padding(.horizontal, Spacing.gutter).padding(.top, 32).padding(.bottom, 10)
                            ForEach(Array(g.rows.enumerated()), id: \.element.id) { n, r in
                                CalendarUI.PersonRow(r.isSelf ? "You" : r.name, status: status(r), tone: r.response == .noReply ? .amber : .dim,
                                                     isFirst: n == 0)
                            }
                        }
                    }
                    Color.clear.frame(height: Spacing.xxl)
                }
            }
        }
        .background(Color.calBg)
        .toolbar(.hidden, for: .navigationBar)
        .accessibilityIdentifier("event.peopleList")
    }

    private func status(_ r: GuestRow) -> String {
        if r.isOrganizer { return "Organiser" }
        switch r.response {
        case .going: return "Yes"
        case .maybe: return "Maybe"
        case .declined: return "No"
        case .noReply: return "Pending"
        }
    }
}

struct ActivityView: UIViewControllerRepresentable {
    let items: [Any]
    func makeUIViewController(context: Context) -> UIActivityViewController { UIActivityViewController(activityItems: items, applicationActivities: nil) }
    func updateUIViewController(_ vc: UIActivityViewController, context: Context) {}
}
