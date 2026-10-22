# Scripting the hub

Everything the app does goes through the HTTP API in [`api/openapi.yaml`](../api/openapi.yaml). Scripts use a **script token**.

## Get a token

On the server (or from the app: Settings, API tokens):

```bash
calendard token create --name grafana --kind script --scope read
# id:     01J...
# secret: cal_...   (shown once)
```

Scopes: `read` (GET and drafts), `write` (also create/edit/delete events, settings), `admin` (also tokens, accounts, automations).
With an admin token: `POST /v1/tokens {"name","kind":"script","scope":"read"}`, `DELETE /v1/tokens/{id}`.

## Read

```bash
HUB=https://calendar.<tailnet>.ts.net; T=cal_...
curl -H "Authorization: Bearer $T" "$HUB/v1/agenda?from=2026-10-15T00:00:00Z&to=2026-10-16T00:00:00Z"
curl -H "Authorization: Bearer $T" "$HUB/v1/events?q=dentist&from=2026-10-01T00:00:00Z&to=2026-12-01T00:00:00Z"
curl -H "Authorization: Bearer $T" "$HUB/v1/summary"            # today: count, first start, free stretch, ready-made text
```

## Ask for free time (deterministic, inside working hours)

```bash
curl -X POST -H "Authorization: Bearer $T" -H 'Content-Type: application/json' "$HUB/v1/freetime" \
  -d '{"durationMin":120,"from":"2026-10-12T00:00:00Z","to":"2026-10-17T00:00:00Z"}'
```

## Draft and create an event

```bash
# 1. a draft: nothing is written
curl -X POST -H "Authorization: Bearer $T" -H 'Content-Type: application/json' "$HUB/v1/quick-add" \
  -d '{"text":"lunch with Sam next fri 1pm at Kōya"}' > draft.json
# 2. answer any "ambiguities", then create from draft.event with an Idempotency-Key
curl -X POST -H "Authorization: Bearer $T" -H 'Content-Type: application/json' -H "Idempotency-Key: $(uuidgen)" \
  "$HUB/v1/events" -d "$(jq .event draft.json)"
```

Re-sending the same `Idempotency-Key` with the same body returns the first response instead of creating a second event.
Edits take `If-Match: "<version>"` (the `version` you read) and `?scope=this|following|all` for repeating events.

## Subscribe to an ICS feed (admin)

```bash
curl -X POST -H "Authorization: Bearer $ADMIN" -H 'Content-Type: application/json' "$HUB/v1/accounts/ics" -d '{"url":"webcal://example.com/holidays.ics","name":"Holidays"}'   # read-only; polled every 15 min; 422 {field:"url"} if it cannot be fetched or parsed
curl -X PATCH -H "Authorization: Bearer $ADMIN" -H 'Content-Type: application/json' "$HUB/v1/accounts/<id>" -d '{"url":"https://new.example.com/x.ics"}'   # change the address; feeds on your LAN need CALENDAR_ICS_ALLOW_PRIVATE=1 on the hub
```

## Automations (admin)

```bash
# a daily 07:00 summary on your phone through ntfy
curl -X POST -H "Authorization: Bearer $ADMIN" -H 'Content-Type: application/json' "$HUB/v1/automations" -d '{
  "name":"Morning summary","trigger":{"type":"cron","expr":"0 7 * * *"},
  "action":{"type":"ntfy","topic":"my-calendar"}}'
# 5 minutes before any event on one calendar, to a webhook you sign-check
curl -X POST ... -d '{"name":"Heads up","trigger":{"type":"event.start","offsetMin":-5,"calendarId":"..."},
  "action":{"type":"webhook","url":"http://192.168.1.20:8080/hook","secret":"change-me"}}'
curl -X POST -H "Authorization: Bearer $ADMIN" "$HUB/v1/automations/<id>/test"     # send a sample now
```

### Verifying a webhook

`X-Calendar-Signature` is `sha256=` + hex HMAC-SHA256 of `"<X-Calendar-Timestamp>.<raw body>"` with your secret.

```python
import hmac, hashlib
def valid(secret: bytes, ts: str, body: bytes, header: str) -> bool:
    mac = hmac.new(secret, ts.encode() + b"." + body, hashlib.sha256).hexdigest()
    return hmac.compare_digest("sha256=" + mac, header)
```

Reject requests whose timestamp is more than a few minutes old. The body is the payload documented as `Automation` deliveries: `{id, automation, trigger, firedAt, event?|summary?}`.
