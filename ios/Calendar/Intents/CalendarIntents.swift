import AppIntents
import CalendarCore
import CalendarUI
import FeatureAssist
import Foundation
import SwiftUI

// Add event / What's next / Join next meeting. They run in the app process, read the shared database (App Group)
// and talk to the hub with the token from the shared Keychain. Not paired = a plain sentence, never a crash.

private enum IntentContext {
    static func services() -> AppServices.Bundle? { AppServices.shared.current() }
    static func tz(_ env: AppEnvironment) -> TimeZone { (try? env.db.userTimeZone()) ?? .current }
    static let notPaired: IntentDialog = "Open Calendar and connect your server first."
}

struct AddEventIntent: AppIntent {
    static let title: LocalizedStringResource = "Add event"
    static let description = IntentDescription("Say or type an event, check it, and add it to your calendar.")
    static let openAppWhenRun = false

    @Parameter(title: "Event", requestValueDialog: "What should I add?")
    var text: String

    static var parameterSummary: some ParameterSummary { Summary("Add \(\.$text)") }

    func perform() async throws -> some IntentResult & ProvidesDialog {
        guard let config = HubConfigStore().load(), let b = IntentContext.services() else { return .result(dialog: IntentContext.notPaired) }
        let api = APIClient(config: config)
        let tz = IntentContext.tz(b.env)
        let draft: QuickAddDraft
        do { draft = try await api.quickAdd(text: text, now: Date(), tzid: tz.identifier) }
        catch { return .result(dialog: "I couldn\u{2019}t reach your server. Nothing was added.") }

        // Never guess: ask each fixed-choice question out loud; anything free-form goes to the app.
        var answers: [String: String] = [:]
        for q in IntentAdd.required(draft) {
            guard let labels = IntentAdd.choices(q) else {
                return .result(dialog: "I need a clearer time for that. Try saying the day and time, like \u{201C}Lunch with Sam Friday at 1pm\u{201D}.")
            }
            let picked = try await $text.requestDisambiguation(among: labels, dialog: IntentDialog(stringLiteral: q.question))
            IntentAdd.apply(label: picked, to: q, answers: &answers)
        }
        let event = QuickAddViewModel.resolve(draft, answers: answers, tz: tz)
        if let issue = event.validate().first { return .result(dialog: IntentDialog(stringLiteral: issue.message + " Nothing was added.")) }

        try await requestConfirmation(result: .result(dialog: IntentDialog(stringLiteral: "Add \(IntentAdd.summary(event, tz: tz))?")))
        do {
            _ = try await api.createEvent(event, key: UUID().uuidString)
            _ = await b.env.sync.silentPush()
            return .result(dialog: IntentDialog(stringLiteral: "Added \(event.title)."))
        } catch {
            return .result(dialog: IntentDialog(stringLiteral: QuickAddViewModel.words(WriteFailure.classify(error))))
        }
    }
}

struct WhatsNextIntent: AppIntent {
    static let title: LocalizedStringResource = "What\u{2019}s next?"
    static let description = IntentDescription("Hear or see your next event.")
    static let openAppWhenRun = false

    @MainActor
    func perform() async throws -> some IntentResult & ProvidesDialog & ShowsSnippetView {
        guard let b = IntentContext.services() else { return .result(dialog: IntentContext.notPaired, view: NextSnippet(title: "Not connected", detail: "Open Calendar to connect your server.")) }
        let now = Date()
        let next = try? await b.env.agenda.nextEvent(now: now)
        let tz = IntentContext.tz(b.env)
        let sentence = NextMeeting.sentence(next: next, now: now, tz: tz)
        let time = DraftFormat.formatter("jmm", tz, .current)
        return .result(dialog: IntentDialog(stringLiteral: sentence),
                       view: NextSnippet(title: next?.title ?? "Nothing next",
                                         detail: next?.start.map { "\(time.string(from: $0)) \u{00B7} \(NextMeeting.untilWords($0.timeIntervalSince(now)))" } ?? "Clear for the rest of today"))
    }
}

struct JoinNextMeetingIntent: AppIntent {
    static let title: LocalizedStringResource = "Join next meeting"
    static let description = IntentDescription("Open the call link of the meeting that is on now or about to start.")
    static let openAppWhenRun = false

    func perform() async throws -> some IntentResult & ProvidesDialog & OpensIntent {
        guard let b = IntentContext.services() else { return .result(opensIntent: OpenURLIntent(URL(string: "calendar://today")!), dialog: IntentContext.notPaired) }
        let tz = IntentContext.tz(b.env)
        let now = Date()
        let today = DayKey(now, in: tz)
        let items = (try? await b.env.agenda.items(from: today, to: today.adding(days: 1))) ?? []
        guard let meeting = NextMeeting.pick(items, now: now), let link = meeting.join, let url = URL(string: link.url) else {
            return .result(opensIntent: OpenURLIntent(URL(string: "calendar://today")!), dialog: "There is no meeting with a call link starting soon.")
        }
        return .result(opensIntent: OpenURLIntent(url), dialog: IntentDialog(stringLiteral: "Joining \(meeting.title)."))
    }
}

struct NextSnippet: View {
    let title: String
    let detail: String
    var body: some View {
        VStack(alignment: .leading, spacing: 4) {
            Text(title).font(CalFont.heading).foregroundStyle(Color.calInk)
            Text(detail).font(CalFont.sub).foregroundStyle(Color.calInk2)
        }
        .padding(16).frame(maxWidth: .infinity, alignment: .leading)
    }
}

struct CalendarShortcuts: AppShortcutsProvider {
    static var appShortcuts: [AppShortcut] {
        AppShortcut(intent: AddEventIntent(), phrases: ["Add an event in \(.applicationName)", "New event in \(.applicationName)"],
                    shortTitle: "Add event", systemImageName: "plus")
        AppShortcut(intent: WhatsNextIntent(), phrases: ["What\u{2019}s next in \(.applicationName)", "What is next in \(.applicationName)"],
                    shortTitle: "What\u{2019}s next?", systemImageName: "clock")
        AppShortcut(intent: JoinNextMeetingIntent(), phrases: ["Join my next meeting in \(.applicationName)", "Join the call in \(.applicationName)"],
                    shortTitle: "Join next meeting", systemImageName: "video")
    }
}
