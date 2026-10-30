# Push notifications

Optional. Without push the app still syncs whenever you open it and in the background when iOS allows. With push, a
change in Google reaches your phone within moments, and alerts work for moved, cancelled and new-invitation events.

Event titles never travel through Apple. A push carries only an id; the app fetches the wording from your hub.

## What you need

- A paid Apple developer account (the free one cannot use push).
- An APNs key: Apple developer portal, **Keys**, **+**, tick **Apple Push Notifications service (APNs)**, download the
  `.p8` once. Note its **Key ID** and your **Team ID**.
- An app built with your own bundle id ([step 4](../guides/4-install-the-app.md)); the hub's topic must match it.

## Give the key to the hub

Put the key in `calendard.env` as one line of base64:

```bash
base64 -i AuthKey_XXXXXXXXXX.p8 | tr -d '\n'        # copy the output
```

```sh
CALENDAR_APNS_KEY=<the base64 line>
CALENDAR_APNS_KEY_ID=XXXXXXXXXX
CALENDAR_APNS_TEAM_ID=YYYYYYYYYY
CALENDAR_APNS_TOPIC=com.yourname.calendar            # your app's bundle id
```

Then recreate the container and check:

```bash
docker compose -f compose.image.yaml up -d --force-recreate
docker compose -f compose.image.yaml exec calendard calendard doctor      # an "apns key" line, no FAIL
```

If your setup mounts files instead, set `CALENDAR_APNS_KEY_FILE` to the `.p8` path. If both are set, the file wins.

## Sandbox and production

An app installed from Xcode registers for Apple's **sandbox** push service, and one from TestFlight or the App Store for
**production**. The app tells the hub which one each device uses, so one hub serves both.

## If nothing arrives

- iPhone Settings, Notifications, Calendar: allowed, and not in a Focus that silences it.
- In the app, Settings, Alerts: the kind of alert you expect is on.
- The hub log has one line per push (`push sent`) with the outcome. `BadDeviceToken` means the topic or environment does not
  match the build you installed; `InvalidProviderToken` means the key id or team id is wrong.
