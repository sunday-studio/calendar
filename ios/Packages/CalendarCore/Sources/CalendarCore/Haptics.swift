import Foundation
import UIKit

/// Moments in the app that have a feel. Screens say WHAT happened; `HapticMap` decides how it feels.
public enum HapticTrigger: Sendable, CaseIterable {
    /// A 15-minute step while dragging or resizing a block.
    case snap
    case viewSwitch, dayPage, monthPick
    /// Opening an event or invitation, picking a row in a sheet, tapping Today.
    case rowTap, today
    /// Month grid opens or closes.
    case monthToggle
    /// A dragged block was dropped and the write started.
    case drop
    case primaryButton, fab
    case sheetOpen
    /// Long-press lifts an event.
    case lift
    /// A drag was refused (read only, all-day, offline).
    case blocked
    case saved, rsvpSent, undoRestored
    case conflict, failure
}

public enum HapticFeedback: Equatable, Sendable {
    public enum Impact: Equatable, Sendable { case light, soft, rigid }
    public enum Notice: Equatable, Sendable { case success, warning, error }
    case selection
    case impact(Impact)
    case notification(Notice)
}

/// Pure event -> feedback table (unit-tested).
public enum HapticMap {
    public static func feedback(for t: HapticTrigger) -> HapticFeedback {
        switch t {
        case .snap, .viewSwitch, .dayPage, .monthPick, .rowTap, .today: .selection
        case .monthToggle: .impact(.soft)
        case .drop: .impact(.light)
        case .primaryButton, .fab: .impact(.light)
        case .sheetOpen: .impact(.soft)
        case .lift: .impact(.rigid)
        case .blocked: .notification(.warning)
        case .saved, .rsvpSent, .undoRestored: .notification(.success)
        case .conflict: .notification(.warning)
        case .failure: .notification(.error)
        }
    }
}

/// Plays feedback. Generators are kept and prepared ahead so the first tick has no latency.
/// Silent when Settings > Appearance > Haptics is off; the system's own haptics switch silences UIKit generators itself.
@MainActor
public final class Haptics {
    public static let shared = Haptics()
    public static let preferenceKey = "pref.haptics"

    /// Injectable for tests.
    public var isEnabled: () -> Bool = { UserDefaults.standard.object(forKey: Haptics.preferenceKey) as? Bool ?? true }
    /// Injectable for tests: receives every feedback that would be played.
    public var sink: ((HapticFeedback) -> Void)?

    private let selection = UISelectionFeedbackGenerator()
    private let light = UIImpactFeedbackGenerator(style: .light)
    private let soft = UIImpactFeedbackGenerator(style: .soft)
    private let rigid = UIImpactFeedbackGenerator(style: .rigid)
    private let notice = UINotificationFeedbackGenerator()

    public init() {}

    /// Call when a gesture begins so the next tick is instant.
    public func prepare(for t: HapticTrigger) {
        guard isEnabled() else { return }
        switch HapticMap.feedback(for: t) {
        case .selection: selection.prepare()
        case .impact(.light): light.prepare()
        case .impact(.soft): soft.prepare()
        case .impact(.rigid): rigid.prepare()
        case .notification: notice.prepare()
        }
    }

    public func play(_ t: HapticTrigger) {
        guard isEnabled() else { return }
        let f = HapticMap.feedback(for: t)
        sink?(f)
        switch f {
        case .selection: selection.selectionChanged(); selection.prepare()
        case .impact(.light): light.impactOccurred(); light.prepare()
        case .impact(.soft): soft.impactOccurred(); soft.prepare()
        case .impact(.rigid): rigid.impactOccurred(); rigid.prepare()
        case .notification(let n):
            notice.notificationOccurred(n == .success ? .success : n == .warning ? .warning : .error); notice.prepare()
        }
    }
}
