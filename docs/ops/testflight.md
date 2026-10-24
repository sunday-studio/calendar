# TestFlight / Release builds on your own hub

Why: the free Personal Team cannot do Push or Time Sensitive Notifications and its installs expire
after 7 days. A paid-team Release build (TestFlight, or ad hoc) has neither limit. The Debug/personal flow is unchanged.

## Team id: one variable, no file edits
`DEVELOPMENT_TEAM` in `ios/project.yml` is `${CALENDAR_TEAM_ID}`. The Makefile defaults it to the Personal Team.

| Goal | Command |
| --- | --- |
| Personal install (free team, no push entitlements) | `make ios-gen-personal` (unchanged) |
| Personal team with full entitlements | `make ios-gen` |
| Paid team project in Xcode | `CALENDAR_TEAM_ID=<paid id> make ios-gen` |
| Release archive + upload | `TEAM_ID=<paid id> make ios-archive` |

Always generate through `make` (a bare `xcodegen generate` leaves the variable unresolved). If you export
`CALENDAR_TEAM_ID` in your shell profile, `ios-gen-personal` will use it too; unset it for the personal flow.
Your paid team id is in developer.apple.com > Membership details (10 characters).

## Order of steps
1. **Sign the paid team into Xcode**: Settings > Accounts > add your Apple ID; the paid team then appears next to
   the Personal Team. Run `CALENDAR_TEAM_ID=<paid id> make ios-gen`, open the project, check Signing & Capabilities.
2. **Identifiers.** Automatic signing (`-allowProvisioningUpdates`) registers these for you on first archive; or
   create them by hand at developer.apple.com > Identifiers:
   - `dev.casprine.calendar` (app), `dev.casprine.calendar.widgets`, `dev.casprine.calendar.notification-service`
   - App Group `group.dev.casprine.calendar`, enabled on all three ids
   - App id capabilities: Push Notifications, App Groups, Time Sensitive Notifications. Keychain sharing is implicit
     (`$(AppIdentifierPrefix)dev.casprine.calendar.shared`). Live Activities need no switch.
   - Changing prefix/group: update `project.yml` entitlements and `AppGroup` in CalendarCore.
3. **APNs key**: Keys > "+" > Apple Push Notifications service (.p8, downloadable once). Note Key ID and Team ID.
   Give them to the hub as `CALENDAR_APNS_KEY_FILE`, `CALENDAR_APNS_KEY_ID`, `CALENDAR_APNS_TEAM_ID` (the paid team id)
   and `CALENDAR_APNS_TOPIC=dev.casprine.calendar`; see `docs/ops/deploy.md` and `deploy/calendard.env.example`
   (also uncomment the `apns_key` secret in `deploy/compose.yaml`). `hub doctor` must show `apns key ok`.
   A team-scoped key works for sandbox and production.
4. **App Store Connect**: My Apps > "+" > New App, iOS, bundle id `dev.casprine.calendar`, any SKU.
   Privacy: **Data Not Collected** (the app only talks to your hub; `PrivacyInfo.xcprivacy` in the app and both
   extensions declares no tracking). Export compliance is answered in the build (`ITSAppUsesNonExemptEncryption = false`).
5. **Archive and upload**:

       TEAM_ID=<paid id> BUILD_NUMBER=2 make ios-archive

   The script generates the project for that team, archives Release for `generic/platform=iOS`, exports with
   `ios/ExportOptions.appstore.plist` (method `app-store-connect`, destination `upload`, teamID filled in from
   `TEAM_ID` in a temp copy) and uploads. `NO_UPLOAD=1` exports `ios/build/export/Calendar.ipa` instead (then use
   Transporter). Raise `BUILD_NUMBER` for every upload (it overrides `CURRENT_PROJECT_VERSION`; marketing version
   is `MARKETING_VERSION` in `project.yml`). The script never reads or prints credentials; Xcode's signed-in account
   is used. Afterwards run `make ios-gen-personal` if you want the personal-team project back.
6. **Internal tester**: App Store Connect > Users and Access: your Apple ID is already a user. TestFlight >
   Internal Testing > "+" group > add yourself and the build (after processing, ~10-30 min).
7. **Install**: TestFlight app on the phone > Calendar > Install. Builds last 90 days; upload a new one before then.
8. **Pair**: open the app, enter your hub address `https://<mac-mini>.<tailnet>.ts.net` and pair. Then Settings >
   Alerts to allow notifications.

### Ad hoc instead of TestFlight
Register the phone UDID (Devices) in the paid team, then `TEAM_ID=<paid id> make ios-archive-adhoc`. Output:
`ios/build/export-adhoc/Calendar.ipa` (`ios/ExportOptions.plist`, method `release-testing`). Install through Xcode >
Window > Devices and Simulators.

## What changes in a TestFlight/ad hoc build
- The distribution profile turns `aps-environment: development` into `production`, and the app registers its
  token with environment `production` (`PushEnvironment.current` is `.sandbox` only under `#if DEBUG`).
- Hub side: **no setting needed.** The environment is stored per device at registration
  (`server/internal/push/apns.go` holds both gateways and picks `api.sandbox.push.apple.com` or `api.push.apple.com`
  per token). Debug and TestFlight installs can coexist. Re-pair or reopen the app once on the new build so the
  production token is registered; old sandbox tokens simply stop working.
- Do not run a Release build from Xcode with a development profile: it would register a sandbox token as production.
- Release is HTTPS only (the Debug-only `NSAppTransportSecurity` key is deleted by the post-build script): the hub
  must be served over Tailscale HTTPS (`tailscale serve`), not plain http. `NSLocalNetworkUsageDescription` is present.

## Entitlements/Info checks (Release)
All three targets share the same entitlements: App Group, keychain group, `aps-environment`, time-sensitive.
Signing is only disabled for the simulator SDK (`CODE_SIGNING_ALLOWED[sdk=iphonesimulator*]`).
Fonts are stand-ins (DECISIONS Q4).
