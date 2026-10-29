import Foundation
import Observation

/// One app-wide toast (bottom 96, ~6 s). Screens post, Home draws it, so it survives a screen being popped
/// ("Lunch deleted · Undo").
@MainActor @Observable
public final class ToastCenter {
    public struct Toast: Identifiable {
        public let id = UUID()
        public var message: String
        public var detail: String?
        public var actionTitle: String?
        public var action: (@MainActor () -> Void)?
    }

    public static let shared = ToastCenter()
    public private(set) var current: Toast?
    private var clear: Task<Void, Never>?

    public func show(_ message: String, detail: String? = nil, actionTitle: String? = nil, seconds: Double? = nil, action: (@MainActor () -> Void)? = nil) {
        let t = Toast(message: message, detail: detail, actionTitle: actionTitle, action: action)
        current = t
        clear?.cancel()
        clear = Task { [weak self] in
            // Short: a plain note 2.5s, one with a button (Undo, Try again) 4s.
            try? await Task.sleep(for: .seconds(seconds ?? (actionTitle == nil ? 2.5 : 4)))
            if !Task.isCancelled, self?.current?.id == t.id { self?.current = nil }
        }
    }

    public func dismiss() { clear?.cancel(); current = nil }
}
