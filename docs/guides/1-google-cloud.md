# 1. Create the Google OAuth client

The hub signs in to your calendars through a Google Cloud project that you own. It is free, takes about 15 minutes and
needs no review for personal use.

1. Open [console.cloud.google.com](https://console.cloud.google.com), click the project picker, then **New project**.
   Name it `calendar-hub` and select it.
2. **APIs & Services**, **Library**. Search for "Google Calendar API" and click **Enable**.
3. **OAuth consent screen** (newer consoles call it "Google Auth Platform"), **Get started**.
   - App name `Calendar`, your email as the support and developer contact.
   - Audience: **External**.
4. **Data access**, **Add or remove scopes**. Add `https://www.googleapis.com/auth/calendar`, `openid` and
   `.../auth/userinfo.email`.
5. **Audience**, **Publish app**, so the status reads **In production**.
   Do not leave it in *Testing*: Google expires refresh tokens after 7 days there, and every account would ask you to
   sign in again every week.
6. **Clients**, **Create client**. Type **Web application**, name `calendar-hub`. Under **Authorized redirect URIs** add

   ```text
   https://<your-hub-address>/auth/google/callback
   ```

   `<your-hub-address>` is the HTTPS address you set up in [step 3](3-https.md), for example
   `calendar.your-tailnet.ts.net`. If you do not know it yet, come back and add it later; Google lets you add URIs any
   time. It must match what the hub is told in `CALENDAR_PUBLIC_URL`, character for character: `https`, no trailing slash.
7. Click **Create**. Copy the **client id** and the **client secret** somewhere safe. You use them in step 2.

**You will not verify the app**, and you do not need to. The one consequence: each time you sign an account in, Google
shows "Google hasn't verified this app". Click **Advanced**, then **Go to Calendar (unsafe)**, then allow. Unverified apps
are limited to 100 users, far above what you need.

**Work or school accounts** may be blocked by the organisation's admin ("access_blocked"). Ask them to allow your client
id, or share that calendar to a personal account.

**Check:** you have a client id ending in `.apps.googleusercontent.com` and a secret starting with `GOCSPX-`.
