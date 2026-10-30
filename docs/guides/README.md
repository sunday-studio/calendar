# Set it up, step by step

Do these in order. Each guide ends with a check, so you know it worked before you move on. About an hour the first time,
most of it waiting on Google and Apple.

| Step | Guide | You end up with |
|---|---|---|
| 1 | [Create the Google OAuth client](1-google-cloud.md) | A client id and secret |
| 2 | [Run the hub](2-run-the-hub.md) | A hub running in Docker on your server |
| 3 | [Put HTTPS in front of it](3-https.md) | `https://<your-host>` that your phone can reach |
| 4 | [Build the app and install it](4-install-the-app.md) | The app on your iPhone |
| 5 | [Pair and connect Google](5-connect.md) | Your calendars on the phone |
| 6 | [Keep it running](6-keep-it-running.md) | Backups, updates and a restore you have tried |

You need: a server with Docker and Compose v2 (amd64 or arm64), an iPhone on iOS 26, a Mac with Xcode 26 to build the
app, a Google account, and an Apple developer account (the free one cannot do push notifications).

Want to look around first, with no accounts at all? `scripts/run-local.sh demo` runs a hub with fake data
([local-run.md](../ops/local-run.md)).
