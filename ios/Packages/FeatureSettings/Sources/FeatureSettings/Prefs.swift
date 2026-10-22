import CalendarCore
import SwiftUI

/// On-device preferences (not synced to the hub). Keys are stable: widgets and the notification extension may read them.
public enum Prefs {
    public static let theme = DisplayPrefs.Key.theme                    // system | dark | light
    public static let haptics = "pref.haptics"                // Bool, default true (same key as CalendarCore.Haptics)
    public static let flipAnimation = DisplayPrefs.Key.flipAnimation    // Bool, default true
    public static let showDeclined = DisplayPrefs.Key.showDeclined      // Bool, default false
    public static let showWeekNumbers = DisplayPrefs.Key.showWeekNumbers
    public static let time24 = DisplayPrefs.Key.time24                  // Bool, default true
    public static let alertJoinButton = AlertPrefs.Key.joinButton
    public static let alertMovedCancelled = AlertPrefs.Key.movedCancelled
    public static let alertNeedsSignIn = AlertPrefs.Key.needsSignIn
    public static let alertInvitations = AlertPrefs.Key.invitations

    public static func scheme(_ raw: String) -> ColorScheme? {
        switch raw { case "dark": .dark; case "light": .light; default: nil }
    }
}
