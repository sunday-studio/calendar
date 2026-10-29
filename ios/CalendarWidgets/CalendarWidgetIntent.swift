import AppIntents
import CalendarCore
import SurfacesCore
import WidgetKit

/// A visible calendar the widget can be limited to.
struct CalendarEntity: AppEntity {
    static let typeDisplayRepresentation = TypeDisplayRepresentation(name: "Calendar")
    static let defaultQuery = CalendarEntityQuery()
    let id: String
    let name: String
    var displayRepresentation: DisplayRepresentation { DisplayRepresentation(title: "\(name)") }
}

struct CalendarEntityQuery: EntityQuery {
    private func visible() async -> [CalendarEntity] {
        guard let db = try? AppDatabase.openShared(), let cals = try? db.savedCalendars() else { return [] }
        return cals.filter(\.visible).map { CalendarEntity(id: $0.id, name: $0.name) }
    }
    func entities(for identifiers: [String]) async throws -> [CalendarEntity] { await visible().filter { identifiers.contains($0.id) } }
    func suggestedEntities() async throws -> [CalendarEntity] { await visible() }
}

enum LargeLayoutOption: String, AppEnum {
    case agenda, day
    static let typeDisplayRepresentation = TypeDisplayRepresentation(name: "Large layout")
    static let caseDisplayRepresentations: [LargeLayoutOption: DisplayRepresentation] = [
        .agenda: "Agenda", .day: "Day"]
}

enum MediumLayoutOption: String, AppEnum {
    case list, nextList
    static let typeDisplayRepresentation = TypeDisplayRepresentation(name: "Medium layout")
    static let caseDisplayRepresentations: [MediumLayoutOption: DisplayRepresentation] = [
        .list: "List", .nextList: "Next + list"]
}

/// One configuration for the Medium, Large, date and lock widgets (spec: widget configuration).
struct CalendarWidgetIntent: WidgetConfigurationIntent {
    static let title: LocalizedStringResource = "Calendar"
    static let description = IntentDescription("Choose what the widget shows.")

    @Parameter(title: "Large layout", default: .agenda) var largeLayout: LargeLayoutOption
    @Parameter(title: "Medium layout", default: .list) var mediumLayout: MediumLayoutOption
    @Parameter(title: "Calendars") var calendars: [CalendarEntity]?
    @Parameter(title: "Show declined", default: false) var showDeclined: Bool
    @Parameter(title: "Show titles on lock screen", default: false) var showLockTitles: Bool

    var config: WidgetConfig {
        WidgetConfig(largeLayout: largeLayout == .day ? .day : .agenda, mediumLayout: mediumLayout == .nextList ? .nextList : .list,
                     calendarIds: (calendars?.isEmpty ?? true) ? nil : Set(calendars!.map(\.id)),
                     showDeclined: showDeclined, showLockTitles: showLockTitles)
    }
}
