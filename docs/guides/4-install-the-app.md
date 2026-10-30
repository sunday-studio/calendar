# 4. Build the app and install it

There is no App Store build. You build the app yourself with Xcode and install it on your own iPhone.

You need a Mac with Xcode 26, [XcodeGen](https://github.com/yonaskolb/XcodeGen) (`brew install xcodegen`), an iPhone on
iOS 26 with Developer Mode on, and an Apple developer account. The free account cannot do push notifications or
time-sensitive alerts, and its installs expire after 7 days; the paid one has neither limit.

1. Clone the repository and go into it:

   ```bash
   git clone https://github.com/sunday-studio/calendar.git && cd calendar
   ```

2. Replace the author's identifiers with your own. The bundle id, App Group and push topic are tied to the author's
   Apple account, so yours must differ:

   ```bash
   scripts/set-bundle-id.sh com.yourname.calendar
   ```

3. Generate the Xcode project with your team id (Apple developer portal, Membership):

   ```bash
   CALENDAR_TEAM_ID=ABCDE12345 make ios-gen
   open ios/Calendar.xcodeproj
   ```

4. In Xcode, select the **Calendar** scheme and your iPhone as the destination. Under **Signing & Capabilities** for each
   target, check that your team is selected and signing is automatic.
5. Choose **Product, Scheme, Edit Scheme, Run, Build Configuration: Release** for a build meant for daily use, then press
   **Run**. The first launch asks you to trust the developer: iPhone Settings, General, VPN & Device Management.
   A debug build also allows plain `http` to a Tailscale address, which is handy for testing; a release build needs `https`.

## Push notifications (optional)

Without this the app still syncs whenever you open it; with it, changes arrive within moments and alerts work.

1. Apple developer portal, **Keys**, create a key with **Apple Push Notifications service (APNs)** enabled, and download
   the `.p8` file once. Note the **Key ID** and your **Team ID**.
2. Give them to the hub in `calendard.env`; see [push.md](../ops/push.md).

**Check:** the app opens on a welcome screen asking for a hub address. Pairing is the next step.
