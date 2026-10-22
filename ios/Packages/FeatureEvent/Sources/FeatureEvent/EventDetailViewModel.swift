import CalendarCore
import Foundation
import Observation

/// Actions that need the write service (T14/T15); the detail screen only reports the intent.
public enum EventAction: Equatable, Sendable {
    case edit, duplicate, move
    case delete(EventScope)
    case rsvp(String) // accepted | tentative | declined
}

@MainActor @Observable
public final class EventDetailViewModel {
    public enum Phase: Equatable, Sendable {
        case loading
        case loaded
        case failed(String)
    }

    public static let readOnlyCopy = "This event can\u{2019}t be edited here"

    public let id: String
    public private(set) var phase: Phase = .loading
    public private(set) var detail: EventDetail?
    /// Shown while loading so the title and time appear instantly.
    public let preview: AgendaItem?
    /// Showing the saved copy because the refresh failed.
    public private(set) var refreshFailed = false
    public private(set) var isRefreshing = false
    public private(set) var now: Date
    /// RSVP in flight / failed (board r-fail).
    public private(set) var isSending = false
    /// Calendars and accounts, to name the eyebrow when the hub's detail has no names.
    public private(set) var context: (calendars: [HubCalendar], accounts: [Account]) = ([], [])
    public func setContext(calendars: [HubCalendar], accounts: [Account]) { context = (calendars, accounts) }
    public private(set) var replyError: String?
    public private(set) var pendingChoice: String?

    private let repo: any EventDetailRepository
    private let clock: AppClock
    public let format: EventTimeFormat

    public init(id: String, preview: AgendaItem? = nil, repo: any EventDetailRepository, clock: AppClock = .system,
                tz: TimeZone = .current, locale: Locale = .current) {
        self.id = id; self.preview = preview; self.repo = repo; self.clock = clock
        now = clock.now()
        format = EventTimeFormat(tz: tz, locale: locale)
    }

    // MARK: Derived

    public var item: AgendaItem? { detail?.item ?? preview }
    public var timeText: EventTimeText? { item.map { format.text(for: $0) } }
    public var relative: String? { item.flatMap { EventTimeFormat.relative($0, now: now) } }
    public var readOnly: Bool { item.map { !$0.canEdit } ?? false }
    public var readOnlyBanner: String? { readOnly ? Self.readOnlyCopy : nil }
    public var people: [GuestRow] { detail.map(PeopleLogic.rows) ?? [] }
    public var peopleSummary: String { PeopleLogic.summary(people) }
    public var recurrenceText: String? {
        if let r = RecurrenceText.summary(detail?.recurrence) { return r }
        return item?.recurring == true ? "Repeats" : nil
    }
    public var descriptionText: AttributedString? {
        guard let d = detail?.description, !DescriptionText.plain(d).isEmpty else { return nil }
        return DescriptionText.linkified(d)
    }

    /// Structured join link, else a recognised meeting link inside the notes.
    public var join: (join: Join, fromNotes: Bool)? {
        if let j = item?.join { return (j, false) }
        if let j = DescriptionText.firstMeetingLink(detail?.description) { return (j, true) }
        return nil
    }

    /// "Sun 11 Oct \u{00B7} 10:30 to 11:30", "Sun 11 Oct \u{00B7} All day", "Thu 15 Oct to Fri 16 Oct".
    public var whenLine: String? { item.map { format.whenLine(for: $0) } }

    /// Why this event cannot be edited: a link feed, an invitation (guest), or a read-only calendar.
    public var readOnlyExplanation: String {
        let isLink = item.flatMap { i in context.accounts.first { $0.id == i.accountId }?.provider == "ics" } ?? false
        if isLink { return "Added from a link. Calendar can show this event but not change it, reply to it or delete it." }
        if item?.canRSVP == true { return "You are a guest. You can reply, but only the organiser can change or delete this event." }
        return "This calendar is read only. Calendar can show this event but not change it."
    }

    /// Eyebrow: "WORK \u{00B7} NAME@EXAMPLE.COM" (uppercased by the label style).
    public var eyebrow: String {
        let cal = detail?.calendarName ?? item.flatMap { i in context.calendars.first { $0.id == i.calendarId }?.name }
        let acct = detail?.accountName ?? item.flatMap { i in context.accounts.first { $0.id == i.accountId }?.email }
        // A read-only link (ICS) is named by what it is, not by its feed host (canvas d-ro: "WORK, READ ONLY LINK").
        if readOnly, let i = item, context.accounts.first(where: { $0.id == i.accountId })?.provider == "ics" {
            return [cal, "Read only link"].compactMap { $0 }.filter { !$0.isEmpty }.joined(separator: ", ")
        }
        let parts = [cal, acct].compactMap { $0 }.filter { !$0.isEmpty }
        return parts.isEmpty ? "Calendar" : parts.joined(separator: " \u{00B7} ")
    }

    /// "Maya, Sam and 2 more" (first two names), "Maya and Sam", "Maya".
    public var peopleNames: String {
        let names = people.filter { !$0.isSelf }.map { $0.name.split(separator: " ").first.map(String.init) ?? $0.name }
        switch names.count {
        case 0: return people.isEmpty ? "" : "Just you"
        case 1: return names[0]
        case 2: return "\(names[0]) and \(names[1])"
        default: return "\(names[0]), \(names[1]) and \(names.count - 2) more"
        }
    }

    /// "4 yes \u{00B7} 1 pending"
    public var peopleCounts: String {
        let yes = people.filter { $0.response == .going }.count
        let pending = people.filter { $0.response == .noReply }.count
        let no = people.filter { $0.response == .declined }.count
        return [yes > 0 ? "\(yes) yes" : nil, pending > 0 ? "\(pending) pending" : nil, no > 0 ? "\(no) no" : nil]
            .compactMap { $0 }.joined(separator: " \u{00B7} ")
    }

    /// "10 minutes before", "At the start", "1 hour before"
    public var reminderText: String? { detail?.reminderMin.map(ReminderText.text) }

    /// Name of the event's calendar (hub detail, else the local calendar list).
    public var calendarName: String? {
        detail?.calendarName ?? item.flatMap { i in context.calendars.first { $0.id == i.calendarId }?.name }
    }

    public var notesText: String {
        guard let d = detail?.description else { return "None" }
        let p = DescriptionText.plain(d)
        return p.isEmpty ? "None" : p
    }

    public var isRecurring: Bool { item?.recurring == true }

    public var calendarLine: String? {
        guard let d = detail else { return nil }
        let parts = [d.calendarName, d.accountName].compactMap { $0 }.filter { !$0.isEmpty }
        return parts.isEmpty ? nil : parts.joined(separator: " \u{00B7} ")
    }

    /// Reply bar only while a reply is pending and replying is allowed.
    public var showsReplyBar: Bool {
        if isSending || replyError != nil { return item?.canRSVP == true }
        guard let i = item else { return false }
        return i.canRSVP && (i.myResponse == nil || i.myResponse == "needsAction")
    }

    public var mapsURL: URL? {
        guard let loc = item?.location, !loc.isEmpty else { return nil }
        return MapsLink.url(for: loc)
    }

    // MARK: Writes

    /// Sends the reply and waits for the provider. Returns true when stored.
    @discardableResult
    public func reply(_ response: RSVPResponse, note: String?, scope: EventScope, writer: any EventWriting) async -> Bool {
        guard let item, !isSending else { return false }
        isSending = true; replyError = nil; pendingChoice = response.rawValue
        defer { isSending = false }
        switch await writer.rsvp(item, response: response, note: note?.isEmpty == true ? nil : note, scope: scope, key: nil, mode: .confirmed) {
        case .saved(let d):
            if let d { detail = d } else if var cur = detail { cur.item.myResponse = Self.wire(response); detail = cur }
            pendingChoice = nil
            return true
        case .failed(let f):
            replyError = f == .offline ? "Can\u{2019}t reach your server. Nothing changed for the organiser." : EventEditorViewModel.message(for: f)
            return false
        }
    }

    static func wire(_ r: RSVPResponse) -> String { r == .accepted ? "accepted" : r == .declined ? "declined" : "tentative" }

    public var myResponseWord: String? {
        switch item?.myResponse { case "accepted": "Yes"; case "tentative": "Maybe"; case "declined": "No"; default: nil }
    }

    // MARK: Loading

    /// Cached copy first (instant), then a network refresh. Safe to call again for pull-to-refresh.
    public func load() async {
        now = clock.now()
        if detail == nil, let c = try? await repo.cached(id: id) {
            detail = c; phase = .loaded
        }
        await refresh()
    }

    public func refresh() async {
        guard !isRefreshing else { return }
        isRefreshing = true
        defer { isRefreshing = false }
        do {
            let d = try await repo.refresh(id: id)
            detail = d; phase = .loaded; refreshFailed = false
        } catch {
            if detail != nil { refreshFailed = true }
            else { phase = .failed(Self.message(error)) }
        }
        now = clock.now()
    }

    public func retry() async {
        phase = .loading
        await refresh()
    }

    static func message(_ error: Error) -> String {
        if let e = error as? APIClientError {
            switch e {
            case .transport, .tls: return "Can\u{2019}t reach your hub. Check your connection and try again."
            case .notFound: return "This event is no longer on your calendar."
            default: break
            }
        }
        return "Couldn\u{2019}t load this event. Try again."
    }
}
