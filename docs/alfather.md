# alfather and the hub's MCP server (M8, B5)

Two directions, both built against a fake because alfather's real tool names are not known yet.

## 1. alfather as the quick-add parser (hub is the MCP client)

`POST /v1/quick-add` asks alfather first when it is configured, otherwise the rule parser answers.

| Env | Meaning |
|---|---|
| `CALENDAR_ALFATHER_URL` | alfather's MCP endpoint (streamable HTTP). **Unset = feature off.** |
| `CALENDAR_ALFATHER_PARSE_TOOL` | tool to call, default `parse_event` |
| `CALENDAR_ALFATHER_TOKEN` | optional bearer token sent to alfather |
| `CALENDAR_MCP_DISABLE=1` | do not serve the hub's own `/mcp` |

**Assumed tool contract** (change the env or `internal/alfather` once the real one is known):

- Input arguments: `{text, now, tz, calendars:[{id,name,isDefault}], defaults:{calendarId, durationMin}}`. `now` is RFC 3339 in `tz`.
- Output: one JSON object, either as `structuredContent` or as the first text block, with the shape of `Draft` in `api/openapi.yaml`: `{confidence, event (EventInput), people, ambiguities, spans}`. `source` is set by the hub.
- Checked by the hub: title present, `when` valid, `event.calendarId` one of the offered calendars (empty = default), confidence 0..1, at most 200 guests. Spans outside the text are dropped. Anything else invalid = the answer is rejected.
- Budget: 3 s for connect + call. On timeout, connection error, tool error, or invalid output the hub answers with the rule parser, `source: "rules"`, `degraded: true`. The app uses `degraded` for the "alfather isn't answering" banner (design board q-down) and still offers the rule-based guess. Nothing is ever created by this call.

## 2. The hub as an MCP server (agents use your calendar)

`POST /mcp`, streamable HTTP (stateless, JSON responses), same bearer tokens, MCP SDK `github.com/modelcontextprotocol/go-sdk` v1.8.0.

| Tool | Scope | Notes |
|---|---|---|
| `agenda(from,to,calendars?)` | read | window <= 62 days, <= 300 items |
| `get_event(id)`, `search(q,from?,to?,limit?)`, `calendars()` | read | |
| `freetime(duration,from,to,day_part?,ignore_working_hours?,limit?)` | read | duration in minutes |
| `draft_event(input)`, `draft_update_event(id,patch,scope?)`, `draft_delete_event(id,scope?)`, `draft_rsvp(id,response,note?,scope?)` | read | write nothing; return `confirm_token` |
| `create_event`, `update_event`, `delete_event`, `rsvp_event` — each `{confirm_token}` | write | listed only for write tokens |

**Approval flow.** A `confirm_token` is single use, valid 10 minutes, bound to the token that drafted it and to the exact payload. It only works after a person approves it: `GET /v1/mcp/confirmations/{token}` (show the draft) and `POST /v1/mcp/confirmations/{token}/approve`, both with a **device** token of write scope. Script tokens and the drafting token get 403, so an agent cannot approve itself. Refusals (tool result with `isError`, `{error:{code}}`): `confirmation_required`, `confirmation_invalid`, `confirmation_pending`, `confirmation_expired`, `confirmation_used`. A token is spent even if the write then fails; update/delete/rsvp drafts remember the event `version` and fail with `precondition_failed` if it changed.

**Which token to give an agent.** `script` + `read` = reads and drafts only. `script` + `write` = also runs what you approved. Never `admin`.

**Untrusted text.** Titles, locations, descriptions and names are returned only as string values in data fields (control/format characters stripped, capped), with a `notice` field. Calls are audit-logged (table `mcp_calls`; counts in `GET /v1/status` under `mcp`); no event text is logged.

## 3. What alfather's owner must tell us (H9)

1. The real endpoint URL and transport (streamable HTTP? SSE? stdio only?), and how it authenticates.
2. The real tool name(s) for "parse a sentence into an event" and for "answer a free-time question" (the app's "Ask alfather" screen currently has no hub call).
3. Whether it takes the input above and returns a `Draft`, or something else we must map (give a sample call and reply).
4. How alfather will call this hub: the `/mcp` URL (tailnet) and which token scope you will give it. Does it need resources/prompts rather than tools?
5. Its latency: if it regularly needs more than 3 s, say so (the budget is a constant in `internal/alfather`).
