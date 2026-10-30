import Foundation

/// On-device display preferences (Settings > Appearance). They are NOT synced to the hub.
/// Keys are stable; `Prefs` in FeatureSettings re-exports them for `@AppStorage`.
public struct DisplayPrefs: Equatable, Sendable {
    public enum Key {
        public static let theme = "pref.theme"
        public static let flipAnimation = "pref.flipAnimation"
        public static let showDeclined = "pref.showDeclined"
        public static let showWeekNumbers = "pref.showWeekNumbers"
        public static let time24 = "pref.time24"
    }

    public var flipAnimation = true
    public var showDeclined = false
    public var showWeekNumbers = false
    public var time24 = true

    public init(flipAnimation: Bool = true, showDeclined: Bool = false, showWeekNumbers: Bool = false, time24: Bool = true) {
        self.flipAnimation = flipAnimation; self.showDeclined = showDeclined
        self.showWeekNumbers = showWeekNumbers; self.time24 = time24
    }

    public static func load(_ d: UserDefaults = .standard) -> DisplayPrefs {
        DisplayPrefs(flipAnimation: d.object(forKey: Key.flipAnimation) as? Bool ?? true,
                     showDeclined: d.object(forKey: Key.showDeclined) as? Bool ?? false,
                     showWeekNumbers: d.object(forKey: Key.showWeekNumbers) as? Bool ?? false,
                     time24: d.object(forKey: Key.time24) as? Bool ?? true)
    }

    /// `base` with the clock forced to 24 or 12 hours; every formatter built from it follows the preference.
    public func locale(base: Locale = .current) -> Locale {
        var c = Locale.Components(locale: base)
        c.hourCycle = time24 ? .zeroToTwentyThree : .oneToTwelve
        return Locale(components: c)
    }

    /// The list Home shows: declined events only when the preference asks for them.
    public func visible(_ items: [AgendaItem]) -> [AgendaItem] {
        showDeclined ? items : items.filter { $0.myResponse != "declined" }
    }

    /// Flip cells animate unless the preference or the system Reduce Motion setting says otherwise.
    public func animatesFlips(reduceMotion: Bool) -> Bool { flipAnimation && !reduceMotion }

    /// ISO-8601 week number of `day` ("Week 42").
    public static func weekNumber(of day: DayKey) -> Int {
        var cal = Calendar(identifier: .iso8601)
        cal.timeZone = TimeZone(identifier: "UTC")!
        let p = day.rawValue.split(separator: "-").compactMap { Int($0) }
        guard p.count == 3, let d = cal.date(from: DateComponents(year: p[0], month: p[1], day: p[2], hour: 12)) else { return 0 }
        return cal.component(.weekOfYear, from: d)
    }
}

/// Alert preferences the notification service extension must also see, so they live in the App Group suite
/// (the extension cannot read the app's standard defaults). The app mirrors them whenever defaults change.
public struct AlertPrefs: Equatable, Sendable {
    public enum Key {
        public static let joinButton = "pref.alert.joinButton"
        public static let movedCancelled = "pref.alert.movedCancelled"
        public static let needsSignIn = "pref.alert.needsSignIn"
        public static let invitations = "pref.alert.invitations"
    }

    public var joinButton = true
    public var movedCancelled = true
    public var needsSignIn = true
    public var invitations = true

    public init(joinButton: Bool = true, movedCancelled: Bool = true, needsSignIn: Bool = true, invitations: Bool = true) {
        self.joinButton = joinButton; self.movedCancelled = movedCancelled
        self.needsSignIn = needsSignIn; self.invitations = invitations
    }

    public static func load(_ d: UserDefaults) -> AlertPrefs {
        AlertPrefs(joinButton: d.object(forKey: Key.joinButton) as? Bool ?? true,
                   movedCancelled: d.object(forKey: Key.movedCancelled) as? Bool ?? true,
                   needsSignIn: d.object(forKey: Key.needsSignIn) as? Bool ?? true,
                   invitations: d.object(forKey: Key.invitations) as? Bool ?? true)
    }

    /// The suite shared with extensions; nil when the group is unavailable (unsigned builds).
    public static var sharedSuite: UserDefaults? { UserDefaults(suiteName: appGroupIdentifier) }

    /// What the extension reads: the shared copy, defaults if the app never mirrored.
    public static func loadShared() -> AlertPrefs { sharedSuite.map(load) ?? AlertPrefs() }

    public func save(to d: UserDefaults) {
        d.set(joinButton, forKey: Key.joinButton); d.set(movedCancelled, forKey: Key.movedCancelled)
        d.set(needsSignIn, forKey: Key.needsSignIn); d.set(invitations, forKey: Key.invitations)
    }

    /// App side: copy the standard defaults into the shared suite (no-op when nothing changed).
    @discardableResult
    public static func mirror(from standard: UserDefaults = .standard, to shared: UserDefaults? = sharedSuite) -> AlertPrefs {
        let p = load(standard)
        if let shared, load(shared) != p { p.save(to: shared) }
        return p
    }
}
