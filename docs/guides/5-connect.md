# 5. Pair and connect Google

1. On the iPhone, make sure Tailscale is connected.
2. Open the app. On the pairing screen enter the hub address `https://<your-address>` and paste the token you created in
   [step 2](2-run-the-hub.md) (it starts with `cal_`). The app checks that the hub is reachable, the certificate is valid,
   the token works and the versions match. All four should pass.
3. **Settings, Accounts, Add Google account.** A sheet opens Google's sign-in. Pick your account, click **Advanced**, then
   **Go to Calendar (unsafe)** on the "hasn't verified this app" page. On the permission screen, **tick the box for your
   calendars** (Google lets you untick it) and continue. You return to the app.
4. Repeat for each Google account.

The first sync takes seconds to a few minutes for a calendar with years of events; events appear as each calendar
finishes. After that the hub polls about every minute while the app is open and every five minutes otherwise.

**Check:** your events are on the Home screen, and **Settings, Server** shows the hub as connected. Create a test event in
the app and see it appear in Google Calendar on the web, then change something in Google and watch it arrive.

If something is off: [troubleshooting](../ops/troubleshooting.md). To walk through every feature on a real account:
[SMOKE.md](../ops/SMOKE.md).
