import CalendarCore
import Foundation
import Observation

public struct SettingsDependencies: Sendable {
    public var settings: any SettingsRepository
    public var calendars: any CalendarRepository
    public init(settings: any SettingsRepository, calendars: any CalendarRepository) {
        self.settings = settings; self.calendars = calendars
    }
    public init(env: AppEnvironment) { self.init(settings: env.settings, calendars: env.calendars) }
}

/// Edits a draft copy of the hub settings; `save()` validates the same rules the hub enforces, then PUTs.
@MainActor @Observable
public final class SettingsViewModel {
    public enum Field: String, Sendable { case weekStart, workingHours, workingDays, defaultCalendar, secondaryTimeZone }

    public var draft = Settings()
    public private(set) var original: Settings?
    public private(set) var writableCalendars: [HubCalendar] = []
    public private(set) var isSaving = false
    public private(set) var errors: [Field: String] = [:]
    public var errorMessage: String?

    private let deps: SettingsDependencies
    private var tasks: [Task<Void, Never>] = []

    public init(deps: SettingsDependencies) { self.deps = deps }

    public var loaded: Bool { original != nil }
    public var isDirty: Bool { original != nil && draft != original }

    public func start() {
        guard tasks.isEmpty else { return }
        tasks.append(Task { [weak self, deps] in
            for await s in deps.settings.observeSettings() {
                guard let self, let s else { continue }
                let wasDirty = self.isDirty
                self.original = s
                if !wasDirty { self.draft = s }   // never overwrite what the user is editing
            }
        })
        tasks.append(Task { [weak self, deps] in
            for await c in deps.calendars.observeCalendars() { self?.writableCalendars = c.filter(SettingsLogic.isWritable) }
        })
    }

    public func stop() { tasks.forEach { $0.cancel() }; tasks = [] }

    public func revert() { if let original { draft = original }; errors = [:]; errorMessage = nil }

    // MARK: Working hours

    public var workStartMinutes: Int {
        get { SettingsLogic.minutes(draft.workingHours.start) ?? 9 * 60 }
        set { draft.workingHours.start = SettingsLogic.hhmm(newValue) }
    }
    public var workEndMinutes: Int {
        get { SettingsLogic.minutes(draft.workingHours.end) ?? 17 * 60 + 30 }
        set { draft.workingHours.end = SettingsLogic.hhmm(newValue) }
    }

    public func toggleWorkingDay(_ day: Int) {
        guard (0...6).contains(day) else { return }
        var days = Set(draft.workingHours.days)
        if days.contains(day) { days.remove(day) } else { days.insert(day) }
        draft.workingHours.days = days.sorted()
    }

    // MARK: Save

    /// Field errors keyed by field; empty = valid.
    public func validate() -> [Field: String] {
        var e: [Field: String] = [:]
        if !(0...6).contains(draft.weekStart) { e[.weekStart] = "Pick a day of the week." }
        let w = draft.workingHours
        if let a = SettingsLogic.minutes(w.start), let b = SettingsLogic.minutes(w.end) {
            if a >= b { e[.workingHours] = "Work should end after it starts." }
        } else { e[.workingHours] = "Use times like 09:00." }
        if w.days.contains(where: { !(0...6).contains($0) }) || Set(w.days).count != w.days.count {
            e[.workingDays] = "Pick each day once."
        }
        if let id = draft.defaultCalendarId, !writableCalendars.contains(where: { $0.id == id }) {
            e[.defaultCalendar] = "Pick a calendar you can add events to."
        }
        if let tz = draft.secondaryTzid, TimeZone(identifier: tz) == nil { e[.secondaryTimeZone] = "Unknown time zone." }
        return e
    }

    /// Returns true when stored (or nothing to store).
    @discardableResult
    public func save() async -> Bool {
        guard isDirty, !isSaving else { return true }
        errors = validate()
        guard errors.isEmpty else { return false }
        isSaving = true; errorMessage = nil
        defer { isSaving = false }
        do {
            let saved = try await deps.settings.update(draft)
            original = saved; draft = saved
            return true
        } catch {
            if case APIClientError.server(400, let body?) = error, let f = body.error.field, let field = Self.field(f) {
                errors[field] = body.message
            } else {
                errorMessage = CalendarsViewModel.message(error)
            }
            return false
        }
    }

    static func field(_ wire: String) -> Field? {
        switch wire {
        case "weekStart": .weekStart
        case "defaultCalendarId": .defaultCalendar
        case "secondaryTzid": .secondaryTimeZone
        case "workingHours": .workingHours
        default: wire.hasPrefix("workingHours") ? .workingHours : nil
        }
    }
}
